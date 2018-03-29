// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package cni

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"runtime"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	"github.com/aws/amazon-vpc-cni-k8s/cni/driver"
	pb "github.com/aws/amazon-vpc-cni-k8s/rpc"
)

const (
	ipamdAddress = "localhost:50051"
)

// cniPluginConf stores the common network config for CNI plugin
type cniPluginConf struct {
	CNIVersion string `json:"cniVersion,omitempty"`
	Name       string `json:"name"` // plugin name
	Type       string `json:"type"` // plugin type

	// VethPrefix is the prefix to use when constructing the host-side veth device name.
	// It should be no more than four characters, and defaults to 'eni'.
	VethPrefix string `json:"vethPrefix"`
}

// K8sArgs is the valid CNI_ARGS used for Kubernetes.
type K8sArgs struct {
	types.CommonArgs
	IP                         net.IP
	K8S_POD_NAME               types.UnmarshallableString
	K8S_POD_NAMESPACE          types.UnmarshallableString
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString
}

// MakeVethName returns a name to be used on the host-side veth device.
// Note: the maximum length for linux interface name is 15
func (k8s K8sArgs) MakeVethName(prefix string) string {
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%s.%s", k8s.K8S_POD_NAMESPACE, k8s.K8S_POD_NAME)))
	return fmt.Sprintf("%s%s", prefix, hex.EncodeToString(h.Sum(nil))[:11])
}

func init() {
	// This is to ensure that all the namespace operations
	// are performed for a single thread.
	runtime.LockOSThread()
}

// CmdAdd is a callback functions that gets called by skel.PluginMain
// in response to ADD method.
func CmdAdd(args *skel.CmdArgs) error {
	// Set up a connection to the ipamd server.
	conn, err := grpc.Dial(ipamdAddress, grpc.WithInsecure())
	if err != nil {
		return errors.Wrap(err, "add cmd: failed to connect to backend server")
	}
	defer conn.Close()

	c := pb.NewCNIBackendClient(conn)
	return add(args, c, driver.NewLinuxDriver())
}

func add(args *skel.CmdArgs, c pb.CNIBackendClient, driverClient driver.NetworkDriver) error {
	conf := cniPluginConf{}
	if err := json.Unmarshal(args.StdinData, &conf); err != nil {
		return errors.Wrap(err, "add cmd: error loading config from args")
	}

	k8sArgs := K8sArgs{}
	if err := types.LoadArgs(args.Args, &k8sArgs); err != nil {
		return errors.Wrap(err, "add cmd: failed to load k8s config from arg")
	}

	// Default the host-side veth prefix to 'eni'.
	if conf.VethPrefix == "" {
		conf.VethPrefix = "eni"
	}
	if len(conf.VethPrefix) > 4 {
		return errors.New("conf.VethPrefix must be less than 4 characters long")
	}

	r, err := c.AddNetwork(context.Background(), &pb.AddNetworkRequest{
		Netns:                      args.Netns,
		IfName:                     args.IfName,
		K8S_POD_NAME:               string(k8sArgs.K8S_POD_NAME),
		K8S_POD_NAMESPACE:          string(k8sArgs.K8S_POD_NAMESPACE),
		K8S_POD_INFRA_CONTAINER_ID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
	})

	if err != nil || !r.Success {
		return fmt.Errorf("add cmd: failed to assign an IP address to container")
	}

	log.Infof("Received add network response for %#v: %s, table %d", k8sArgs, r.IPv4Addr, r.DeviceNumber)
	addr := &net.IPNet{
		IP:   net.ParseIP(r.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}

	if err = driverClient.SetupNS(k8sArgs.MakeVethName(conf.VethPrefix), args.IfName, args.Netns, addr, int(r.DeviceNumber)); err != nil {
		return errors.Wrap(err, "add command: failed to setup network")
	}

	result := &current.Result{
		IPs: []*current.IPConfig{
			{
				Version: "4",
				Address: *addr,
			},
		},
	}

	return types.PrintResult(result, conf.CNIVersion)
}

// CmdDel is a callback functions that gets called by skel.PluginMain
// in response to DEL method.
func CmdDel(args *skel.CmdArgs) error {
	// notify local IP address manager to free secondary IP
	// Set up a connection to the server.
	conn, err := grpc.Dial(ipamdAddress, grpc.WithInsecure())
	if err != nil {
		return errors.Wrap(err, "del cmd: failed to connect to backend server")
	}
	defer conn.Close()

	c := pb.NewCNIBackendClient(conn)
	return del(args, c, driver.NewLinuxDriver())
}

func del(args *skel.CmdArgs, c pb.CNIBackendClient, driverClient driver.NetworkDriver) error {
	conf := cniPluginConf{}
	if err := json.Unmarshal(args.StdinData, &conf); err != nil {
		return errors.Wrap(err, "del cmd: failed to load cniPluginConf from args")
	}

	k8sArgs := K8sArgs{}
	if err := types.LoadArgs(args.Args, &k8sArgs); err != nil {
		return errors.Wrap(err, "del cmd: failed to load k8s config from args")
	}

	r, err := c.DelNetwork(context.Background(), &pb.DelNetworkRequest{
		K8S_POD_NAME:               string(k8sArgs.K8S_POD_NAME),
		K8S_POD_NAMESPACE:          string(k8sArgs.K8S_POD_NAMESPACE),
		K8S_POD_INFRA_CONTAINER_ID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
		IPv4Addr:                   k8sArgs.IP.String(),
	})
	if err != nil || !r.Success {
		return errors.Wrap(err, "del cmd: failed to process delete request")
	}

	addr := &net.IPNet{
		IP:   net.ParseIP(r.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}

	return driverClient.TeardownNS(addr, int(r.DeviceNumber))
}
