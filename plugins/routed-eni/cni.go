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

package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"runtime"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	log "github.com/cihub/seelog"

	"github.com/pkg/errors"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	cniSpecVersion "github.com/containernetworking/cni/pkg/version"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/grpcwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/rpcwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/typeswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/plugins/routed-eni/driver"
	pb "github.com/aws/amazon-vpc-cni-k8s/rpc"
)

const (
	ipamDAddress       = "localhost:50051"
	defaultLogFilePath = "/var/log/aws-routed-eni/plugin.log"
)

var (
	version string
)

// NetConf stores the common network config for the CNI plugin
type NetConf struct {
	// CNIVersion is the plugin version
	CNIVersion string `json:"cniVersion,omitempty"`

	// Name is the plugin name
	Name string `json:"name"`

	// Type is the plugin type
	Type string `json:"type"`

	// VethPrefix is the prefix to use when constructing the host-side
	// veth device name. It should be no more than four characters, and
	// defaults to 'eni'.
	VethPrefix string `json:"vethPrefix"`
}

// K8sArgs is the valid CNI_ARGS used for Kubernetes
type K8sArgs struct {
	types.CommonArgs

	// IP is pod's ip address
	IP net.IP

	// K8S_POD_NAME is pod's name
	K8S_POD_NAME types.UnmarshallableString

	// K8S_POD_NAMESPACE is pod's namespace
	K8S_POD_NAMESPACE types.UnmarshallableString

	// K8S_POD_INFRA_CONTAINER_ID is pod's container id
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString
}

func init() {
	// This is to ensure that all the namespace operations are performed for
	// a single thread
	runtime.LockOSThread()
}

func cmdAdd(args *skel.CmdArgs) error {
	return add(args, typeswrapper.New(), grpcwrapper.New(), rpcwrapper.New(), driver.New())
}

func add(args *skel.CmdArgs, cniTypes typeswrapper.CNITYPES, grpcClient grpcwrapper.GRPC,
	rpcClient rpcwrapper.RPC, driverClient driver.NetworkAPIs) error {
	log.Infof("Received CNI add request: ContainerID(%s) Netns(%s) IfName(%s) Args(%s) Path(%s) argsStdinData(%s)",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path, args.StdinData)

	conf := NetConf{}
	if err := json.Unmarshal(args.StdinData, &conf); err != nil {
		log.Errorf("Error loading config from args: %v", err)
		return errors.Wrap(err, "add cmd: error loading config from args")
	}

	k8sArgs := K8sArgs{}
	if err := cniTypes.LoadArgs(args.Args, &k8sArgs); err != nil {
		log.Errorf("Failed to load k8s config from arg: %v", err)
		return errors.Wrap(err, "add cmd: failed to load k8s config from arg")
	}

	// Default the host-side veth prefix to 'eni'.
	if conf.VethPrefix == "" {
		conf.VethPrefix = "eni"
	}
	if len(conf.VethPrefix) > 4 {
		return errors.New("conf.VethPrefix must be less than 4 characters long")
	}

	cniVersion := conf.CNIVersion

	// Set up a connection to the ipamD server.
	conn, err := grpcClient.Dial(ipamDAddress, grpc.WithInsecure())
	if err != nil {
		log.Errorf("Failed to connect to backend server for pod %s namespace %s container %s: %v",
			string(k8sArgs.K8S_POD_NAME),
			string(k8sArgs.K8S_POD_NAMESPACE),
			string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
			err)
		return errors.Wrap(err, "add cmd: failed to connect to backend server")
	}
	defer conn.Close()

	c := rpcClient.NewCNIBackendClient(conn)

	r, err := c.AddNetwork(context.Background(),
		&pb.AddNetworkRequest{
			Netns:                      args.Netns,
			K8S_POD_NAME:               string(k8sArgs.K8S_POD_NAME),
			K8S_POD_NAMESPACE:          string(k8sArgs.K8S_POD_NAMESPACE),
			K8S_POD_INFRA_CONTAINER_ID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
			IfName: args.IfName})

	if err != nil {
		log.Errorf("Error received from AddNetwork grpc call for pod %s namespace %s container %s: %v",
			string(k8sArgs.K8S_POD_NAME),
			string(k8sArgs.K8S_POD_NAMESPACE),
			string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
			err)
		return err
	}

	if !r.Success {
		log.Errorf("Failed to assign an IP address to pod %s, namespace %s container %s",
			string(k8sArgs.K8S_POD_NAME),
			string(k8sArgs.K8S_POD_NAMESPACE),
			string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID))
		return fmt.Errorf("add cmd: failed to assign an IP address to container")
	}

	log.Infof("Received add network response for pod %s namespace %s container %s: %s, table %d ",
		string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
		r.IPv4Addr, r.DeviceNumber)

	addr := &net.IPNet{
		IP:   net.ParseIP(r.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}

	// build hostVethName
	// Note: the maximum length for linux interface name is 15
	hostVethName := generateHostVethName(conf.VethPrefix, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))

	err = driverClient.SetupNS(hostVethName, args.IfName, args.Netns, addr, int(r.DeviceNumber))

	if err != nil {
		log.Errorf("Failed SetupPodNetwork for pod %s namespace %s container %s: %v",
			string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID), err)

		// return allocated IP back to IP pool
		r, delErr := c.DelNetwork(context.Background(),
			&pb.DelNetworkRequest{
				K8S_POD_NAME:               string(k8sArgs.K8S_POD_NAME),
				K8S_POD_NAMESPACE:          string(k8sArgs.K8S_POD_NAMESPACE),
				K8S_POD_INFRA_CONTAINER_ID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
				IPv4Addr:                   r.IPv4Addr,
				Reason:                     "SetupNSFailed"})

		if delErr != nil {
			log.Errorf("Error received from DelNetwork grpc call for pod %s namespace %s container %s: %v",
				string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID), delErr)
		}

		if !r.Success {
			log.Errorf("Failed to release IP of pod %s namespace %s container %s: %v",
				string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID), delErr)
		}
		return errors.Wrap(err, "add command: failed to setup network")
	}

	ips := []*current.IPConfig{
		{
			Version: "4",
			Address: *addr,
		},
	}

	result := &current.Result{
		IPs: ips,
	}

	return cniTypes.PrintResult(result, cniVersion)
}

// generateHostVethName returns a name to be used on the host-side veth device.
func generateHostVethName(prefix, namespace, podname string) string {
	h := sha1.New()
	h.Write([]byte(fmt.Sprintf("%s.%s", namespace, podname)))
	return fmt.Sprintf("%s%s", prefix, hex.EncodeToString(h.Sum(nil))[:11])
}

func cmdDel(args *skel.CmdArgs) error {
	return del(args, typeswrapper.New(), grpcwrapper.New(), rpcwrapper.New(), driver.New())
}

func del(args *skel.CmdArgs, cniTypes typeswrapper.CNITYPES, grpcClient grpcwrapper.GRPC, rpcClient rpcwrapper.RPC,
	driverClient driver.NetworkAPIs) error {

	log.Infof("Received CNI del request: ContainerID(%s) Netns(%s) IfName(%s) Args(%s) Path(%s) argsStdinData(%s)",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path, args.StdinData)

	conf := NetConf{}
	if err := json.Unmarshal(args.StdinData, &conf); err != nil {
		log.Errorf("Failed to load netconf from args %v", err)
		return errors.Wrap(err, "del cmd: failed to load netconf from args")
	}

	k8sArgs := K8sArgs{}
	if err := cniTypes.LoadArgs(args.Args, &k8sArgs); err != nil {
		log.Errorf("Failed to load k8s config from args: %v", err)
		return errors.Wrap(err, "del cmd: failed to load k8s config from args")
	}

	// notify local IP address manager to free secondary IP
	// Set up a connection to the server.
	conn, err := grpcClient.Dial(ipamDAddress, grpc.WithInsecure())
	if err != nil {
		log.Errorf("Failed to connect to backend server for pod %s namespace %s container %s: %v",
			string(k8sArgs.K8S_POD_NAME),
			string(k8sArgs.K8S_POD_NAMESPACE),
			string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
			err)

		return errors.Wrap(err, "del cmd: failed to connect to backend server")
	}
	defer conn.Close()

	c := rpcClient.NewCNIBackendClient(conn)

	r, err := c.DelNetwork(context.Background(),
		&pb.DelNetworkRequest{
			K8S_POD_NAME:               string(k8sArgs.K8S_POD_NAME),
			K8S_POD_NAMESPACE:          string(k8sArgs.K8S_POD_NAMESPACE),
			K8S_POD_INFRA_CONTAINER_ID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
			IPv4Addr:                   k8sArgs.IP.String(),
			Reason:                     "PodDeleted"})

	if err != nil {
		log.Errorf("Error received from DelNetwork grpc call for pod %s namespace %s container %s: %v",
			string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID), err)
		return err
	}

	if !r.Success {
		log.Errorf("Failed to process delete request for pod %s namespace %s container %s: %v",
			string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID), err)
		return errors.Wrap(err, "del cmd: failed to process delete request")
	}

	addr := &net.IPNet{
		IP:   net.ParseIP(r.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}

	err = driverClient.TeardownNS(addr, int(r.DeviceNumber))

	if err != nil {
		log.Errorf("Failed on TeardownPodNetwork for pod %s namespace %s container %s: %v",
			string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID), err)
		return err
	}
	return nil
}

func main() {
	defer log.Flush()
	logger.SetupLogger(logger.GetLogFileLocation(defaultLogFilePath))

	log.Infof("Starting CNI Plugin %s  ...", version)

	skel.PluginMain(cmdAdd, cmdDel, cniSpecVersion.All)
}
