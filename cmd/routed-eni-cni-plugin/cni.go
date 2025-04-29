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
	"time"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	cniSpecVersion "github.com/containernetworking/cni/pkg/version"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/aws/amazon-vpc-cni-k8s/cmd/routed-eni-cni-plugin/driver"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/grpcwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/rpcwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/sgpp"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/typeswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/cniutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	pb "github.com/aws/amazon-vpc-cni-k8s/rpc"
)

const ipamdAddress = "127.0.0.1:50051"

const npAgentAddress = "127.0.0.1:50052"

const dummyInterfacePrefix = "dummy"

const npAgentConnTimeout = 2

const multiNICPodAnnotation = "k8s.amazonaws.com/nicConfig"
const multiNICAttachment = "multi-nic-attachment"
const containerVethNamePrefix = "mNicIf"

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

	PluginLogLevel string        `json:"pluginLogLevel"`
	RuntimeConfig  RuntimeConfig `json:"runtimeConfig"`
}

type RuntimeConfig struct {
	PodAnnotations map[string]string `json:"io.kubernetes.cri.pod-annotations"`
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

	// Derive pod MTU. Note that the value has already been validated.
	mtu := networkutils.GetPodMTU(conf.MTU)
	log.Debugf("MTU value set is %d:", mtu)

	// Derive if Pod requires multiple NIC connected
	requiresMultiNICAttachment := false

	if value, ok := conf.RuntimeConfig.PodAnnotations[multiNICPodAnnotation]; ok {
		if value == multiNICAttachment {
			requiresMultiNICAttachment = true
		} else {
			log.Debugf("Multi-NIC annotation mismatch: found=%q, required=%q. Falling back to default configuration.",
				conf.RuntimeConfig.PodAnnotations[multiNICPodAnnotation], multiNICAttachment)
		}
	}

	log.Debugf("pod requires multi-nic attachment: %t", requiresMultiNICAttachment)

	// Set up a connection to the ipamD server.
	conn, err := grpcClient.Dial(ipamdAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
			RequiresMultiNICAttachment: requiresMultiNICAttachment,
		})

	if err != nil {
		log.Errorf("Error received from AddNetwork grpc call for containerID %s: %v", args.ContainerID, err)
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

	// AddNetwork guarantees that Gateway string is a valid IPNet
	gw := net.ParseIP(r.PodENISubnetGW)

	var vethMetadata []driver.VirtualInterfaceMetadata
	var podIPs []*current.IPConfig
	var podInterfaces []*current.Interface
	var dummyInterface *current.Interface

	for index, ip := range r.IPAddress {

		var hostVethName string

		addr := parseIPAddress(ip)
		if r.PodVlanId != 0 {
			hostVethNamePrefix := sgpp.BuildHostVethNamePrefix(conf.VethPrefix, conf.PodSGEnforcingMode)
			hostVethName = networkutils.GeneratePodHostVethName(hostVethNamePrefix, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME), index)
		} else {
			hostVethName = networkutils.GeneratePodHostVethName(conf.VethPrefix, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME), index)
		}

		containerVethName := networkutils.GenerateContainerVethName(args.IfName, containerVethNamePrefix, index)

		vethMetadata = append(vethMetadata, driver.VirtualInterfaceMetadata{
			IPAddress:         addr,
			DeviceNumber:      int(ip.DeviceNumber),
			RouteTable:        int(ip.RouteTableId),
			HostVethName:      hostVethName,
			ContainerVethName: containerVethName,
		})
		// CNI stores the route table ID in the MAC field of the interface. The Route table ID is on the host side
		// and is used during cleanup to remove the ip rules when IPAMD is not reachable.
		podInterfaces = append(podInterfaces,
			&current.Interface{Name: hostVethName},
			&current.Interface{Name: containerVethName, Sandbox: args.Netns, Mac: fmt.Sprint(ip.RouteTableId)},
		)

		// This index always points to the container interface so that we get the IP address corresponding to the interface
		containerInterfaceIndex := len(podInterfaces) - 1

		podIPs = append(podIPs, &current.IPConfig{
			Interface: &containerInterfaceIndex,
			Address:   *addr,
			Gateway:   gw,
		})
	}

	// The dummy interface is purely virtual and is stored in the prevResult struct to assist in cleanup during the DEL command.
	dummyInterfaceName := networkutils.GeneratePodHostVethName(dummyInterfacePrefix, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME), 0)

	// Non-zero value means pods are using branch ENI
	if r.PodVlanId != 0 {
		// Since we always return 1 IP, it should be okay to directly address index 0
		err = driverClient.SetupBranchENIPodNetwork(vethMetadata[0], args.Netns, int(r.PodVlanId), r.PodENIMAC,
			r.PodENISubnetGW, int(r.ParentIfIndex), mtu, conf.PodSGEnforcingMode, log)
		// For branch ENI mode, the pod VLAN ID is packed in Interface.Mac
		dummyInterface = &current.Interface{Name: dummyInterfaceName, Mac: fmt.Sprint(r.PodVlanId)}
	} else {
		err = driverClient.SetupPodNetwork(vethMetadata, args.Netns, mtu, log)
		// For non-branch ENI, the pod VLAN ID value of 0 is packed in Interface.Mac, while the interface device number is packed in Interface.Sandbox
		dummyInterface = &current.Interface{Name: dummyInterfaceName, Mac: fmt.Sprint(0), Sandbox: fmt.Sprint(vethMetadata[0].DeviceNumber)}
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
			log.Errorf("Error received from DelNetwork grpc call for container %s: %v", args.ContainerID, delErr)
		} else if !r.Success {
			log.Errorf("Failed to release IP of container %s", args.ContainerID)
		}
		return errors.Wrap(err, "add command: failed to setup network")
	}

	result := &current.Result{
		IPs:        podIPs,
		Interfaces: podInterfaces,
	}

	// dummy interface is appended to PrevResult for use during cleanup
	// The interfaces field should only include host,container and dummy interfaces in the list.
	// Revisit the prevResult cleanup logic if this changes
	result.Interfaces = append(result.Interfaces, dummyInterface)

	// Set up a connection to the network policy agent
	// NP container might have been removed if network policies are not being used
	// If NETWORK_POLICY_ENFORCING_MODE is not set, we will not configure anything related to NP
	if r.NetworkPolicyMode == "" {
		log.Infof("NETWORK_POLICY_ENFORCING_MODE is not set")
		return cniTypes.PrintResult(result, conf.CNIVersion)
	}
	ctx, cancel := context.WithTimeout(context.Background(), npAgentConnTimeout*time.Second) // Set timeout
	defer cancel()
	npConn, err := grpcClient.DialContext(ctx, npAgentAddress, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		log.Errorf("Failed to connect to network policy agent: %v", err)
		return errors.New("add cmd: failed to setup network policy")
	}
	defer npConn.Close()

	//Make a GRPC call for network policy agent
	npc := rpcClient.NewNPBackendClient(npConn)

	npr, err := npc.EnforceNpToPod(context.Background(),
		&pb.EnforceNpRequest{
			K8S_POD_NAME:        string(k8sArgs.K8S_POD_NAME),
			K8S_POD_NAMESPACE:   string(k8sArgs.K8S_POD_NAMESPACE),
			NETWORK_POLICY_MODE: r.NetworkPolicyMode,
			InterfaceCount:      int32(len(r.IPAddress)),
		})

	// No need to cleanup IP and network, kubelet will send delete.
	if err != nil || !npr.Success {
		log.Errorf("Failed to setup default network policy for Pod Name %s and NameSpace %s: GRPC returned - %v Network policy agent returned - %v",
			string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), err, npr)
		return errors.New("add cmd: failed to setup network policy")
	}

	log.Debugf("Network Policy agent for EnforceNpToPod returned Success : %v", npr.Success)
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
	// During del we cannot depend on the annotation as annotation change doesn't restart a POD
	// We can add it to the dummy interface and use that during the deletion flow
	// Or Pass the number of interfaces so that it is returned by prev result.
	// OR store this information in ipamd datastore so that it always returns what is assigned.

	// For pods using branch ENI, try to delete using previous result
	handled, err := tryDelWithPrevResult(driverClient, conf, k8sArgs, args.IfName, args.Netns, log)
	if err != nil {
		return errors.Wrap(err, "del cmd: failed to delete with prevResult")
	}
	if handled {
		log.Infof("Handled CNI del request with prevResult: ContainerID(%s) Netns(%s) IfName(%s) PodNamespace(%s) PodName(%s)",
			args.ContainerID, args.Netns, args.IfName, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))
		return nil
	}

	// notify local IP address manager to free secondary IP
	// Set up a connection to the server.
	conn, err := grpcClient.Dial(ipamdAddress, grpc.WithInsecure())
	if err != nil {
		log.Errorf("Failed to connect to backend server for container %s: %v",
			args.ContainerID, err)

		// When IPAMD is unreachable, try to teardown pod network using previous result. This action prevents rules from leaking while IPAMD is unreachable.
		// Note that no error is returned to kubelet as there is no guarantee that kubelet will retry delete, and returning an error would prevent container runtime
		// from cleaning up resources. When IPAMD again becomes responsive, it is responsible for reclaiming IP.
		if teardownPodNetworkWithPrevResult(driverClient, conf, k8sArgs, args.IfName, log) {
			log.Infof("Handled pod teardown using prevResult: ContainerID(%s) Netns(%s) IfName(%s) PodNamespace(%s) PodName(%s)",
				args.ContainerID, args.Netns, args.IfName, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))
		} else {
			log.Infof("Could not teardown pod using prevResult: ContainerID(%s) Netns(%s) IfName(%s) PodNamespace(%s) PodName(%s)",
				args.ContainerID, args.Netns, args.IfName, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))
		}
		return nil
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
		log.Errorf("Error received from DelNetwork gRPC call for container %s: %v", args.ContainerID, err)

		// DelNetworkRequest may return a connection error, so try to delete using PrevResult whenever an error is returned. As with the case above, do
		// not return error to kubelet, as there is no guarantee that delete is retried.
		if teardownPodNetworkWithPrevResult(driverClient, conf, k8sArgs, args.IfName, log) {
			log.Infof("Handled pod teardown using prevResult: ContainerID(%s) Netns(%s) IfName(%s) PodNamespace(%s) PodName(%s)",
				args.ContainerID, args.Netns, args.IfName, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))
		} else {
			log.Infof("Could not teardown pod using prevResult: ContainerID(%s) Netns(%s) IfName(%s) PodNamespace(%s) PodName(%s)",
				args.ContainerID, args.Netns, args.IfName, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME))
		}
		return nil
	}

	if !r.Success {
		log.Errorf("Failed to process delete request for container %s: Success == false",
			args.ContainerID)
		return errors.New("del cmd: failed to process delete request")
	}

	log.Infof("Received del network response from ipamd for pod %s namespace %s sandbox %s: %+v", string(k8sArgs.K8S_POD_NAME),
		string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_INFRA_CONTAINER_ID), r)

	var vethMetadata []driver.VirtualInterfaceMetadata

	for _, ip := range r.IPAddress {
		addr := parseIPAddress(ip)

		if addr != nil {
			vethMetadata = append(vethMetadata, driver.VirtualInterfaceMetadata{
				IPAddress:    addr,
				DeviceNumber: int(ip.DeviceNumber),
				RouteTable:   int(ip.RouteTableId),
			})
		}
	}

	if len(vethMetadata) > 0 {

		// vlanID != 0 means pod using security group
		if r.PodVlanId != 0 {
			if isNetnsEmpty(args.Netns) {
				log.Infof("Ignoring TeardownPodENI as Netns is empty for SG pod:%s namespace: %s containerID:%s", k8sArgs.K8S_POD_NAME, k8sArgs.K8S_POD_NAMESPACE, k8sArgs.K8S_POD_INFRA_CONTAINER_ID)
				return nil
			}
			err = driverClient.TeardownBranchENIPodNetwork(vethMetadata[0], int(r.PodVlanId), conf.PodSGEnforcingMode, log)
		} else {
			err = driverClient.TeardownPodNetwork(vethMetadata, log)
		}

		if err != nil {
			log.Errorf("Failed on TeardownPodNetwork for container ID %s: %v",
				args.ContainerID, err)
			return errors.Wrap(err, "del cmd: failed on tear down pod network")
		}
	} else {
		log.Warnf("Container %s did not have a valid IP %+v", args.ContainerID, r.IPAddress)
	}

	if r.NetworkPolicyMode == "" {
		log.Infof("NETWORK_POLICY_ENFORCING_MODE is not set")
		return nil
	}
	// Set up a connection to the network policy agent
	ctx, cancel := context.WithTimeout(context.Background(), npAgentConnTimeout*time.Second) // Set timeout
	defer cancel()
	npConn, err := grpcClient.DialContext(ctx, npAgentAddress, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		log.Errorf("Failed to connect to network policy agent: %v. Network Policy agent might not be running", err)
		return errors.Wrap(err, "del cmd: failed to connect to network policy agent")
	}
	defer npConn.Close()
	//Make a GRPC call for network policy agent
	npc := rpcClient.NewNPBackendClient(npConn)

	npr, err := npc.DeletePodNp(context.Background(),
		&pb.DeleteNpRequest{
			K8S_POD_NAME:      string(k8sArgs.K8S_POD_NAME),
			K8S_POD_NAMESPACE: string(k8sArgs.K8S_POD_NAMESPACE),
		})

	// NP agent will never return an error if its not able to delete ebpf probes
	if err != nil || !npr.Success {
		log.Errorf("Failed to delete pod network policy for Pod Name %s and NameSpace %s: GRPC returned - %v Network policy agent returned - %v",
			string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE), err, npr)
	}

	log.Debugf("Network Policy agent for DeletePodNp returned Success : %v", npr.Success)
	return nil
}

func getContainerNetworkMetadata(prevResult *current.Result, contVethName string) (net.IPNet, *current.Interface, error) {
	containerIfaceIndex, containerInterface, found := cniutils.FindInterfaceByName(prevResult.Interfaces, contVethName)
	if !found {
		return net.IPNet{}, nil, errors.Errorf("cannot find contVethName %s in prevResult", contVethName)
	}
	containerIPs := cniutils.FindIPConfigsByIfaceIndex(prevResult.IPs, containerIfaceIndex)
	if len(containerIPs) != 1 {
		return net.IPNet{}, nil, errors.Errorf("found %d containerIPs for %v in prevResult", len(containerIPs), contVethName)
	}
	return containerIPs[0].Address, containerInterface, nil
}

// tryDelWithPrevResult will try to process CNI delete request without IPAMD.
// returns true if the del request is handled.
func tryDelWithPrevResult(driverClient driver.NetworkAPIs, conf *NetConf, k8sArgs K8sArgs, contVethName string, netNS string, log logger.Logger) (bool, error) {
	// prevResult might not be available, if we are still using older cni spec < 0.4.0.
	if conf.PrevResult == nil {
		return false, nil
	}

	prevResult, err := current.NewResultFromResult(conf.PrevResult)
	if err != nil {
		log.Info("PrevResult not available for pod or parsing failed")
		return false, nil
	}

	dummyIfaceName := networkutils.GeneratePodHostVethName(dummyInterfacePrefix, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME), 0)
	_, dummyIface, found := cniutils.FindInterfaceByName(prevResult.Interfaces, dummyIfaceName)
	if !found {
		return false, nil
	}
	podVlanID, err := strconv.Atoi(dummyIface.Mac)
	if err != nil {
		return false, errors.Errorf("malformed vlanID in prevResult: %s", dummyIface.Mac)
	}
	// For non-branch ENI pods, deletion requires handshake with IPAMD
	if podVlanID == 0 {
		return false, nil
	}
	if isNetnsEmpty(netNS) {
		log.Infof("Ignoring TeardownPodENI as Netns is empty for SG pod: %s namespace: %s containerID: %s", k8sArgs.K8S_POD_NAME, k8sArgs.K8S_POD_NAMESPACE, k8sArgs.K8S_POD_INFRA_CONTAINER_ID)
		return true, nil
	}

	containerIP, _, err := getContainerNetworkMetadata(prevResult, contVethName)
	if err != nil {
		return false, err
	}

	vethMetadata := driver.VirtualInterfaceMetadata{
		IPAddress: &containerIP,
	}

	if err := driverClient.TeardownBranchENIPodNetwork(vethMetadata, podVlanID, conf.PodSGEnforcingMode, log); err != nil {
		return true, err
	}
	return true, nil
}

// teardownPodNetworkWithPrevResult will try to process CNI delete for non-branch ENIs without IPAMD.
// Returns true if pod network is torn down
func teardownPodNetworkWithPrevResult(driverClient driver.NetworkAPIs, conf *NetConf, k8sArgs K8sArgs, contVethName string, log logger.Logger) bool {
	// For non-branch ENI, prevResult is only available in v1.12.1+
	if conf.PrevResult == nil {
		log.Infof("PrevResult not available for pod. Pod may have already been deleted.")
		return false
	}

	prevResult, err := current.NewResultFromResult(conf.PrevResult)
	if err != nil {
		log.Infof("PrevResult not available for pod. Pod may have already been deleted.")
		return false
	}
	dummyIfaceName := networkutils.GeneratePodHostVethName(dummyInterfacePrefix, string(k8sArgs.K8S_POD_NAMESPACE), string(k8sArgs.K8S_POD_NAME), 0)
	_, dummyIface, found := cniutils.FindInterfaceByName(prevResult.Interfaces, dummyIfaceName)
	if !found {
		return false
	}
	// For non-branch ENI, VLAN ID of 0 is encoded in Mac and device number is encoded in Sandbox
	podVlanID, err := strconv.Atoi(dummyIface.Mac)
	if err != nil || podVlanID != 0 {
		log.Errorf("Invalid VLAN ID for non-branch ENI pod: %s", dummyIface.Mac)
		return false
	}
	deviceNumber, err := strconv.Atoi(dummyIface.Sandbox)
	if err != nil {
		log.Errorf("Invalid device number for pod: %s", dummyIface.Sandbox)
		return false
	}

	// Since we are always using dummy interface to store the device number of network card 0 IP
	// RT ID for NC-0 is also stored in the container interface entry. So we have a path for migration where
	// getting the device number from dummy interface can be deprecated entirely. This is currently done to keep it backwards compatible
	routeTableId := deviceNumber + 1
	// The number of interfaces attached to the pod is taken as the length of the interfaces array - 1 (for dummy interface) divided by 2 (for host and container interface)
	var interfacesAttached = (len(prevResult.Interfaces) - 1) / 2
	var vethMetadata []driver.VirtualInterfaceMetadata
	for v := range interfacesAttached {
		containerInterfaceName := networkutils.GenerateContainerVethName(contVethName, containerVethNamePrefix, v)
		containerIP, containerInterface, err := getContainerNetworkMetadata(prevResult, containerInterfaceName)
		if err != nil {
			log.Errorf("container interface name %s does not exist %v", containerInterfaceName, err)
			continue
		}

		// If this property is set, that means the container metadata has the route table ID which we can use
		// If this is not set, it is a pod launched before this change was introduced.
		// So it is only managing network card 0 at that time and device number + 1 is the route table ID which we calculate from device number
		if containerInterface.Mac != "" {
			routeTableId, err = strconv.Atoi(containerInterface.Mac)
			if err != nil {
				log.Errorf("error getting route table number of the interface %s", containerInterface.Mac)
				return false
			}
		}

		vethMetadata = append(vethMetadata, driver.VirtualInterfaceMetadata{
			IPAddress:  &containerIP,
			RouteTable: routeTableId,
		})
	}

	if err := driverClient.TeardownPodNetwork(vethMetadata, log); err != nil {
		log.Errorf("Failed to teardown pod network: %v", err)
		return false
	}
	return true
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

func parseIPAddress(v *pb.IPAddress) *net.IPNet {

	if v.IPv4Addr != "" {
		return &net.IPNet{
			IP:   net.ParseIP(v.IPv4Addr),
			Mask: net.CIDRMask(32, 32),
		}
	} else if v.IPv6Addr != "" {
		return &net.IPNet{
			IP:   net.ParseIP(v.IPv6Addr),
			Mask: net.CIDRMask(128, 128),
		}
	}

	return nil
}
