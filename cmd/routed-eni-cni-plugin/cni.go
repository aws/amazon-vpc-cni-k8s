// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
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
	"os"
	"runtime"
	"strings"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	cniSpecVersion "github.com/containernetworking/cni/pkg/version"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/aws/amazon-vpc-cni-k8s/cmd/routed-eni-cni-plugin/driver"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/grpcwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/rpcwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/typeswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	pb "github.com/aws/amazon-vpc-cni-k8s/rpc"
)

const ipamdAddress = "127.0.0.1:50051"

var version string

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

	// MTU for eth0
	MTU string `json:"mtu"`

	PluginLogFile string `json:"pluginLogFile"`

	PluginLogLevel string `json:"pluginLogLevel"`
}

// K8sArgs is the valid CNI_ARGS used for Kubernetes
type K8sArgs struct {
	types.CommonArgs

	// K8S_POD_NAME is pod's name
	K8S_POD_NAME types.UnmarshallableString

	// K8S_POD_NAMESPACE is pod's namespace
	K8S_POD_NAMESPACE types.UnmarshallableString

	// K8S_POD_INFRA_CONTAINER_ID is pod's sandbox id
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString
}

func init() {
	// This is to ensure that all the namespace operations are performed for
	// a single thread
	runtime.LockOSThread()
}

// LoadNetConf converts inputs (i.e. stdin) to NetConf
func LoadNetConf(bytes []byte) (*NetConf, logger.Logger, error) {
	conf := &NetConf{}
	if err := json.Unmarshal(bytes, conf); err != nil {
		return nil, nil, errors.Wrap(err, "add cmd: error loading config from args")
	}

	//logConfig
	logConfig := logger.Configuration{
		BinaryName:  conf.Name,
		LogLevel:    conf.PluginLogLevel,
		LogLocation: conf.PluginLogFile,
	}
	log := logger.New(&logConfig)

	// MTU
	if conf.MTU == "" {
		log.Debug("MTU not set, defaulting to 9001")
		conf.MTU = "9001"
	}

	// Default the host-side veth prefix to 'eni'.
	if conf.VethPrefix == "" {
		conf.VethPrefix = "eni"
	}
	if len(conf.VethPrefix) > 4 {
		return nil, nil, errors.New("conf.VethPrefix can be at most 4 characters long")
	}

	return conf, log, nil
}

func cmdAdd(args *skel.CmdArgs) error {
	return add(args, typeswrapper.New(), grpcwrapper.New(), rpcwrapper.New(), driver.New())
}

func add(args *skel.CmdArgs, cniTypes typeswrapper.CNITYPES, grpcClient grpcwrapper.GRPC,
	rpcClient rpcwrapper.RPC, driverClient driver.NetworkAPIs) error {

	conf, log, err := LoadNetConf(args.StdinData)
	if err != nil {
		return errors.Wrap(err, "add cmd: error loading config from args")
	}

	log.Infof("Received CNI add request: ContainerID(%s) Netns(%s) IfName(%s) Args(%s) Path(%s) argsStdinData(%s)",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path, args.StdinData)

	k8sArgs := K8sArgs{}
	if err := cniTypes.LoadArgs(args.Args, &k8sArgs); err != nil {
		log.Errorf("Failed to load k8s config from arg: %v", err)
		return errors.Wrap(err, "add cmd: failed to load k8s config from arg")
	}

	mtu := networkutils.GetEthernetMTU(conf.MTU)
	log.Debugf("MTU value set is %d:", mtu)

	// Set up a connection to the ipamD server.
	conn, err := grpcClient.Dial(ipamdAddress, grpc.WithInsecure())
	if err != nil {
		log.Errorf("Failed to connect to backend server for pod %s namespace %s sandbox %s: %v",
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
			IfName:                     args.IfName})

	if err != nil {
		log.Errorf("Error received from AddNetwork grpc call for pod %s namespace %s sandbox %s: %v",
			string(k8sArgs.K8S_POD_NAME),
			string(k8sArgs.K8S_POD_NAMESPACE),
			string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
			err)
		return errors.Wrap(err, "add cmd: Error received from AddNetwork gRPC call")
	}

	if !r.Success {
		log.Errorf("Failed to assign an IP address to pod %s, namespace %s sandbox %s",
			string(k8sArgs.K8S_POD_NAME),
			string(k8sArgs.K8S_POD_NAMESPACE),
			string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID))
		return errors.New("add cmd: failed to assign an IP address to container")
	}

	log.Infof("Received add network response for pod %s namespace %s sandbox %s: %s, table %d, external-SNAT: %v, vpcCIDR: %v",
		string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
		r.IPv4Addr, r.DeviceNumber, r.UseExternalSNAT, r.VPCcidrs)

	addr := &net.IPNet{
		IP:   net.ParseIP(r.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}

	// build hostVethName
	// Note: the maximum length for linux interface name is 15
	hostVethName := generateHostVethName(conf.VethPrefix, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))

	err = driverClient.SetupNS(hostVethName, args.IfName, args.Netns, addr, int(r.DeviceNumber), r.VPCcidrs, r.UseExternalSNAT, mtu, log)

	if err != nil {
		log.Errorf("Failed SetupPodNetwork for pod %s namespace %s sandbox %s: %v",
			string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID), err)

		// return allocated IP back to IP pool
		r, delErr := c.DelNetwork(context.Background(),
			&pb.DelNetworkRequest{
				K8S_POD_NAME:               string(k8sArgs.K8S_POD_NAME),
				K8S_POD_NAMESPACE:          string(k8sArgs.K8S_POD_NAMESPACE),
				K8S_POD_INFRA_CONTAINER_ID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
				Reason:                     "SetupNSFailed"})

		if delErr != nil {
			log.Errorf("Error received from DelNetwork grpc call for pod %s namespace %s sandbox %s: %v",
				string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID), delErr)
		}

		if !r.Success {
			log.Errorf("Failed to release IP of pod %s namespace %s sandbox %s: %v",
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

	return cniTypes.PrintResult(result, conf.CNIVersion)
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

	_, log, err := LoadNetConf(args.StdinData)
	if err != nil {
		return errors.Wrap(err, "add cmd: error loading config from args")
	}

	log.Infof("Received CNI del request: ContainerID(%s) Netns(%s) IfName(%s) Args(%s) Path(%s) argsStdinData(%s)",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path, args.StdinData)

	k8sArgs := K8sArgs{}
	if err := cniTypes.LoadArgs(args.Args, &k8sArgs); err != nil {
		log.Errorf("Failed to load k8s config from args: %v", err)
		return errors.Wrap(err, "del cmd: failed to load k8s config from args")
	}

	// notify local IP address manager to free secondary IP
	// Set up a connection to the server.
	conn, err := grpcClient.Dial(ipamdAddress, grpc.WithInsecure())
	if err != nil {
		log.Errorf("Failed to connect to backend server for pod %s namespace %s sandbox %s: %v",
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
			Reason:                     "PodDeleted"})

	if err != nil {
		if strings.Contains(err.Error(), datastore.ErrUnknownPod.Error()) {
			// Plugins should generally complete a DEL action without error even if some resources are missing. For example,
			// an IPAM plugin should generally release an IP allocation and return success even if the container network
			// namespace no longer exists, unless that network namespace is critical for IPAM management
			log.Infof("Pod %s in namespace %s not found", string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE))
			return nil
		} else {
			log.Errorf("Error received from DelNetwork gRPC call for pod %s namespace %s sandbox %s: %v",
				string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID), err)
			return errors.Wrap(err, "del cmd: error received from DelNetwork gRPC call")
		}
	}

	if !r.Success {
		log.Errorf("Failed to process delete request for pod %s namespace %s sandbox %s: Success == false",
			string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID))
		return errors.New("del cmd: failed to process delete request")
	}

	deletedPodIP := net.ParseIP(r.IPv4Addr)
	if deletedPodIP != nil {
		addr := &net.IPNet{
			IP:   deletedPodIP,
			Mask: net.IPv4Mask(255, 255, 255, 255),
		}
		err = driverClient.TeardownNS(addr, int(r.DeviceNumber), log)
		if err != nil {
			log.Errorf("Failed on TeardownPodNetwork for pod %s namespace %s sandbox %s: %v",
				string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID), err)
			return errors.Wrap(err, "del cmd: failed on tear down pod network")
		}
	} else {
		log.Warnf("Pod %s in namespace %s did not have a valid IP %s", string(k8sArgs.K8S_POD_NAME),
			string(k8sArgs.K8S_POD_NAMESPACE), r.IPv4Addr)
	}
	return nil
}

func main() {
	log := logger.DefaultLogger()
	log.Infof("CNI Plugin version: %s ...", version)
	about := fmt.Sprintf("AWS CNI %s", version)
	exitCode := 0
	if e := skel.PluginMainWithError(cmdAdd, nil, cmdDel, cniSpecVersion.All, about); e != nil {
		if err := e.Print(); err != nil {
			log.Errorf("Failed to write error to stdout: %v", err)
		}
		exitCode = 1
	}
	os.Exit(exitCode)
}
