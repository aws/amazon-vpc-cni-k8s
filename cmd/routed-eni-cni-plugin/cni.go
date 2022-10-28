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

// AWS VPC CNI plugin binary
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/cniutils"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/sgpp"

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

const dummyInterfacePrefix = "dummy"

var version string

// NetConf stores the common network config for the CNI plugin
type NetConf struct {
	types.NetConf

	// VethPrefix is the prefix to use when constructing the host-side
	// veth device name. It should be no more than four characters, and
	// defaults to 'eni'.
	VethPrefix string `json:"vethPrefix"`

	// MTU for eth0
	MTU string `json:"mtu"`

	// PodSGEnforcingMode is the enforcing mode for Security groups for pods feature
	PodSGEnforcingMode sgpp.EnforcingMode `json:"podSGEnforcingMode"`

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
	// Default config
	conf := NetConf{
		MTU:                "9001",
		VethPrefix:         "eni",
		PodSGEnforcingMode: sgpp.DefaultEnforcingMode,
	}

	if err := json.Unmarshal(bytes, &conf); err != nil {
		return nil, nil, errors.Wrap(err, "add cmd: error loading config from args")
	}

	if conf.RawPrevResult != nil {
		if err := cniSpecVersion.ParsePrevResult(&conf.NetConf); err != nil {
			return nil, nil, fmt.Errorf("could not parse prevResult: %v", err)
		}
	}

	logConfig := logger.Configuration{
		LogLevel:    conf.PluginLogLevel,
		LogLocation: conf.PluginLogFile,
	}
	log := logger.New(&logConfig)

	if len(conf.VethPrefix) > 4 {
		return nil, nil, errors.New("conf.VethPrefix can be at most 4 characters long")
	}
	return &conf, log, nil
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

	log.Debugf("Prev Result: %v\n", conf.PrevResult)

	var k8sArgs K8sArgs
	if err := cniTypes.LoadArgs(args.Args, &k8sArgs); err != nil {
		log.Errorf("Failed to load k8s config from arg: %v", err)
		return errors.Wrap(err, "add cmd: failed to load k8s config from arg")
	}

	mtu := networkutils.GetEthernetMTU(conf.MTU)
	log.Debugf("MTU value set is %d:", mtu)

	// Set up a connection to the ipamD server.
	conn, err := grpcClient.Dial(ipamdAddress, grpc.WithInsecure())
	if err != nil {
		log.Errorf("Failed to connect to backend server for container %s: %v",
			args.ContainerID, err)
		return errors.Wrap(err, "add cmd: failed to connect to backend server")
	}
	defer conn.Close()

	c := rpcClient.NewCNIBackendClient(conn)

	r, err := c.AddNetwork(context.Background(),
		&pb.AddNetworkRequest{
			ClientVersion:              version,
			K8S_POD_NAME:               string(k8sArgs.K8S_POD_NAME),
			K8S_POD_NAMESPACE:          string(k8sArgs.K8S_POD_NAMESPACE),
			K8S_POD_INFRA_CONTAINER_ID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
			Netns:                      args.Netns,
			ContainerID:                args.ContainerID,
			NetworkName:                conf.Name,
			IfName:                     args.IfName,
		})

	if err != nil {
		log.Errorf("Error received from AddNetwork grpc call for containerID %s: %v",
			args.ContainerID,
			err)
		return errors.Wrap(err, "add cmd: Error received from AddNetwork gRPC call")
	}

	if !r.Success {
		log.Errorf("Failed to assign an IP address to container %s",
			args.ContainerID)
		return errors.New("add cmd: failed to assign an IP address to container")
	}

	log.Infof("Received add network response from ipamd for container %s interface %s: %+v",
		args.ContainerID, args.IfName, r)

	// We will let the values in result struct guide us in terms of IP Address Family configured.
	var v4Addr, v6Addr, addr *net.IPNet
	var addrFamily string

	// We don't support dual stack mode currently so it has to be either v4 or v6 mode.
	if r.IPv4Addr != "" {
		v4Addr = &net.IPNet{
			IP:   net.ParseIP(r.IPv4Addr),
			Mask: net.CIDRMask(32, 32),
		}
		addrFamily = "4"
		addr = v4Addr
	} else if r.IPv6Addr != "" {
		v6Addr = &net.IPNet{
			IP:   net.ParseIP(r.IPv6Addr),
			Mask: net.CIDRMask(128, 128),
		}
		addrFamily = "6"
		addr = v6Addr
	}

	var hostVethName string
	var dummyInterface *current.Interface

	// The dummy interface is purely virtual and is stored in the prevResult struct to assist in cleanup during the DEL command.
	dummyInterfaceName := networkutils.GeneratePodHostVethName(dummyInterfacePrefix, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))

	// Non-zero value means pods are using branch ENI
	if r.PodVlanId != 0 {
		hostVethNamePrefix := sgpp.BuildHostVethNamePrefix(conf.VethPrefix, conf.PodSGEnforcingMode)
		hostVethName = networkutils.GeneratePodHostVethName(hostVethNamePrefix, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))
		err = driverClient.SetupBranchENIPodNetwork(hostVethName, args.IfName, args.Netns, v4Addr, v6Addr, int(r.PodVlanId), r.PodENIMAC,
			r.PodENISubnetGW, int(r.ParentIfIndex), mtu, conf.PodSGEnforcingMode, log)
		// For branch ENI mode, the pod VLAN ID is packed in Interface.Mac
		dummyInterface = &current.Interface{Name: dummyInterfaceName, Mac: fmt.Sprint(r.PodVlanId)}
	} else {
		// build hostVethName
		// Note: the maximum length for linux interface name is 15
		hostVethName = networkutils.GeneratePodHostVethName(conf.VethPrefix, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))
		err = driverClient.SetupPodNetwork(hostVethName, args.IfName, args.Netns, v4Addr, v6Addr, int(r.DeviceNumber), mtu, log)
		// For non-branch ENI, the pod VLAN ID value of 0 is packed in Interface.Mac, while the interface device number is packed in Interface.Sandbox
		dummyInterface = &current.Interface{Name: dummyInterfaceName, Mac: fmt.Sprint(0), Sandbox: fmt.Sprint(r.DeviceNumber)}
	}
	log.Debugf("Using dummy interface: %v", dummyInterface)

	if err != nil {
		log.Errorf("Failed SetupPodNetwork for container %s: %v",
			args.ContainerID, err)

		// return allocated IP back to IP pool
		r, delErr := c.DelNetwork(context.Background(), &pb.DelNetworkRequest{
			ClientVersion:              version,
			K8S_POD_NAME:               string(k8sArgs.K8S_POD_NAME),
			K8S_POD_NAMESPACE:          string(k8sArgs.K8S_POD_NAMESPACE),
			K8S_POD_INFRA_CONTAINER_ID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
			ContainerID:                args.ContainerID,
			IfName:                     args.IfName,
			NetworkName:                conf.Name,
			Reason:                     "SetupNSFailed",
		})

		if delErr != nil {
			log.Errorf("Error received from DelNetwork grpc call for container %s: %v",
				args.ContainerID, delErr)
		} else if !r.Success {
			log.Errorf("Failed to release IP of container %s", args.ContainerID)
		}
		return errors.Wrap(err, "add command: failed to setup network")
	}

	containerInterfaceIndex := 1
	ips := []*current.IPConfig{
		{
			Version:   addrFamily,
			Address:   *addr,
			Interface: &containerInterfaceIndex,
		},
	}

	hostInterface := &current.Interface{Name: hostVethName}
	containerInterface := &current.Interface{Name: args.IfName, Sandbox: args.Netns}

	result := &current.Result{
		IPs: ips,
		Interfaces: []*current.Interface{
			hostInterface,
			containerInterface,
		},
	}

	// dummy interface is appended to PrevResult for use during cleanup
	result.Interfaces = append(result.Interfaces, dummyInterface)

	return cniTypes.PrintResult(result, conf.CNIVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	return del(args, typeswrapper.New(), grpcwrapper.New(), rpcwrapper.New(), driver.New())
}

func del(args *skel.CmdArgs, cniTypes typeswrapper.CNITYPES, grpcClient grpcwrapper.GRPC, rpcClient rpcwrapper.RPC,
	driverClient driver.NetworkAPIs) error {

	conf, log, err := LoadNetConf(args.StdinData)
	log.Debugf("Prev Result: %v\n", conf.PrevResult)

	if err != nil {
		return errors.Wrap(err, "del cmd: error loading config from args")
	}

	log.Infof("Received CNI del request: ContainerID(%s) Netns(%s) IfName(%s) Args(%s) Path(%s) argsStdinData(%s)",
		args.ContainerID, args.Netns, args.IfName, args.Args, args.Path, args.StdinData)

	var k8sArgs K8sArgs
	if err := cniTypes.LoadArgs(args.Args, &k8sArgs); err != nil {
		log.Errorf("Failed to load k8s config from args: %v", err)
		return errors.Wrap(err, "del cmd: failed to load k8s config from args")
	}

	// Try to delete allocated network resources with data stored in PrevResult.
	handled, skipIpamd, err := tryDelWithPrevResult(driverClient, conf, k8sArgs, args.IfName, args.Netns, log)
	// On error, return immediately if pod was created with branch ENI. Otherwise, let IPAMD handle cleanup. Note that we cannot
	// check conf.PodSGEnforcingMode as the pod we are deleting may have been added before the mode switch.
	if err != nil && skipIpamd {
		return errors.Wrap(err, "del cmd: failed to delete with prevResult")
	}
	if handled {
		log.Infof("Handled CNI del request with prevResult: ContainerID(%s) Netns(%s) IfName(%s) PodNamespace(%s) PodName(%s)",
			args.ContainerID, args.Netns, args.IfName, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))
		// When deleting a pod with a branch ENI configured, IPAMD does not need to be notified, so we can return early.
		if skipIpamd {
			return nil
		}
	}

	// Notify local IP address manager to free secondary IP
	// Set up a connection to the server.
	conn, err := grpcClient.Dial(ipamdAddress, grpc.WithInsecure())
	if err != nil {
		log.Errorf("Failed to connect to backend server for container %s: %v",
			args.ContainerID, err)

		return errors.Wrap(err, "del cmd: failed to connect to backend server")
	}
	defer conn.Close()

	c := rpcClient.NewCNIBackendClient(conn)

	r, err := c.DelNetwork(context.Background(), &pb.DelNetworkRequest{
		ClientVersion:              version,
		K8S_POD_NAME:               string(k8sArgs.K8S_POD_NAME),
		K8S_POD_NAMESPACE:          string(k8sArgs.K8S_POD_NAMESPACE),
		K8S_POD_INFRA_CONTAINER_ID: string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID),
		NetworkName:                conf.Name,
		ContainerID:                args.ContainerID,
		IfName:                     args.IfName,
		Reason:                     "PodDeleted",
	})

	if err != nil {
		if strings.Contains(err.Error(), datastore.ErrUnknownPod.Error()) {
			// Plugins should generally complete a DEL action without error even if some resources are missing. For example,
			// an IPAM plugin should generally release an IP allocation and return success even if the container network
			// namespace no longer exists, unless that network namespace is critical for IPAM management
			log.Infof("Container %s not found", args.ContainerID)
			return nil
		}
		log.Errorf("Error received from DelNetwork gRPC call for container %s: %v",
			args.ContainerID, err)
		return errors.Wrap(err, "del cmd: error received from DelNetwork gRPC call")
	}

	if !r.Success {
		log.Errorf("Failed to process delete request for container %s: Success == false",
			args.ContainerID)
		return errors.New("del cmd: failed to process delete request")
	}

	log.Infof("Received del network response from ipamd for pod %s namespace %s sandbox %s: %+v", string(k8sArgs.K8S_POD_NAME),
		string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID), r)

	// If CNI was able to cleanup network resources using PrevResult, we can return early.
	if handled {
		return nil
	}

	var deletedPodIP net.IP
	var maskLen int
	if r.IPv4Addr != "" {
		deletedPodIP = net.ParseIP(r.IPv4Addr)
		maskLen = 32
	} else if r.IPv6Addr != "" {
		deletedPodIP = net.ParseIP(r.IPv6Addr)
		maskLen = 128
	}

	if deletedPodIP != nil {
		addr := &net.IPNet{
			IP:   deletedPodIP,
			Mask: net.CIDRMask(maskLen, maskLen),
		}

		// vlanID != 0 means pod using security group
		if r.PodVlanId != 0 {
			if isNetnsEmpty(args.Netns) {
				log.Infof("Ignoring TeardownPodENI as Netns is empty for SG pod:%s namespace: %s containerID:%s", k8sArgs.K8S_POD_NAME, k8sArgs.K8S_POD_NAMESPACE, k8sArgs.K8S_POD_INFRA_CONTAINER_ID)
				return nil
			}
			err = driverClient.TeardownBranchENIPodNetwork(addr, int(r.PodVlanId), conf.PodSGEnforcingMode, log)
		} else {
			err = driverClient.TeardownPodNetwork(addr, int(r.DeviceNumber), log)
		}

		if err != nil {
			log.Errorf("Failed on TeardownPodNetwork for container ID %s: %v",
				args.ContainerID, err)
			return errors.Wrap(err, "del cmd: failed on tear down pod network")
		}
	} else {
		log.Warnf("Container %s did not have a valid IP %s", args.ContainerID, r.IPv4Addr)
	}
	return nil
}

// tryDelWithPrevResult will try to process CNI delete request without IPAMD.
// returns true if the del request is handled.
func tryDelWithPrevResult(driverClient driver.NetworkAPIs, conf *NetConf, k8sArgs K8sArgs, contVethName string, netNS string, log logger.Logger) (bool, bool, error) {
	// prevResult might not be available, if we are still using older cni spec < 0.4.0.
	prevResult, ok := conf.PrevResult.(*current.Result)
	if !ok {
		log.Infof("no previous result found")
		return false, false, nil
	}

	dummyIfaceName := networkutils.GeneratePodHostVethName(dummyInterfacePrefix, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))
	_, dummyIface, found := cniutils.FindInterfaceByName(prevResult.Interfaces, dummyIfaceName)
	if !found {
		log.Infof("interface %s not found in prevResult", dummyIfaceName)
		return false, false, nil
	}
	// Pod VLAN ID is encoded in Interface.Mac
	podVlanID, err := strconv.Atoi(dummyIface.Mac)
	// Note that a VLAN ID of 0 when conf.PodSGEnforcingMode is set does not necessarily mean an error, as the pod may have been created before the mode switch.
	if err != nil {
		return true, false, errors.Errorf("malformed vlanID in prevResult: %s", dummyIface.Mac)
	}
	branchEni := podVlanID != 0

	containerIfaceIndex, _, found := cniutils.FindInterfaceByName(prevResult.Interfaces, contVethName)
	if !found {
		return false, branchEni, errors.Errorf("cannot find contVethName %s in prevResult", contVethName)
	}
	containerIPs := cniutils.FindIPConfigsByIfaceIndex(prevResult.IPs, containerIfaceIndex)
	if len(containerIPs) != 1 {
		return false, branchEni, errors.Errorf("found %d containerIP for %v in prevResult", len(containerIPs), contVethName)
	}
	containerIP := containerIPs[0].Address

	if branchEni {
		if isNetnsEmpty(netNS) {
			log.Infof("Ignoring TeardownPodENI as Netns is empty for SG pod: %s namespace: %s containerID: %s", k8sArgs.K8S_POD_NAME, k8sArgs.K8S_POD_NAMESPACE, k8sArgs.K8S_POD_INFRA_CONTAINER_ID)
			return true, branchEni, nil
		}
		err = driverClient.TeardownBranchENIPodNetwork(&containerIP, podVlanID, conf.PodSGEnforcingMode, log)
	} else {
		// deviceNumber is stored in interface Sandbox
		var deviceNumber int
		deviceNumber, err = strconv.Atoi(dummyIface.Sandbox)
		if err != nil {
			return false, branchEni, errors.Errorf("malformed deviceNumber in prevResult: %s", dummyIface.Sandbox)
		}
		err = driverClient.TeardownPodNetwork(&containerIP, deviceNumber, log)
	}

	if err != nil {
		return true, branchEni, err
	}
	return true, branchEni, nil
}

// Scope usage of this function to only SG pods scenario
// Don't process deletes when NetNS is empty
// as it implies that veth for this request is already deleted
// ref: https://github.com/kubernetes/kubernetes/issues/44100#issuecomment-329780382
func isNetnsEmpty(Netns string) bool {
	return Netns == ""
}

func main() {
	log := logger.DefaultLogger()
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
