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
	"encoding/json"
	"errors"
	"net"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"

	mock_driver "github.com/aws/amazon-vpc-cni-k8s/cmd/routed-eni-cni-plugin/driver/mocks"
	mock_grpcwrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/grpcwrapper/mocks"
	mock_rpcwrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/rpcwrapper/mocks"
	mock_typeswrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/typeswrapper/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/rpc"
	mock_rpc "github.com/aws/amazon-vpc-cni-k8s/rpc/mocks"
)

const (
	containerID    = "test-container"
	netNS          = "/proc/ns/1234"
	ifName         = "eth0"
	cniVersion     = "1.0"
	cniName        = "aws-cni"
	pluginLogLevel = "Debug"
	pluginLogFile  = "/var/log/aws-routed-eni/plugin.log"
	cniType        = "aws-cni"
	ipAddr         = "10.0.1.15"
	ip6Addr        = "2001:db8::1234"
	devNum         = 4
)

var (
	ipAddrBytes  = net.ParseIP(ipAddr)
	ip6AddrBytes = net.ParseIP(ip6Addr)
)

var netConf = &NetConf{
	NetConf: types.NetConf{
		CNIVersion: cniVersion,
		Name:       cniName,
		Type:       cniType,
	},
	PluginLogLevel: pluginLogLevel,
	PluginLogFile:  pluginLogFile,
}

func setup(t *testing.T) (*gomock.Controller,
	*mock_typeswrapper.MockCNITYPES,
	*mock_grpcwrapper.MockGRPC,
	*mock_rpcwrapper.MockRPC,
	*mock_driver.MockNetworkAPIs) {
	ctrl := gomock.NewController(t)
	return ctrl,
		mock_typeswrapper.NewMockCNITYPES(ctrl),
		mock_grpcwrapper.NewMockGRPC(ctrl),
		mock_rpcwrapper.NewMockRPC(ctrl),
		mock_driver.NewMockNetworkAPIs(ctrl)
}

func TestCmdAdd(t *testing.T) {
	ctrl, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork := setup(t)
	defer ctrl.Finish()

	stdinData, _ := json.Marshal(netConf)

	cmdArgs := &skel.CmdArgs{ContainerID: containerID,
		Netns:     netNS,
		IfName:    ifName,
		StdinData: stdinData}

	mocksTypes.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil)

	conn, _ := grpc.Dial(ipamdAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(conn, nil)
	mockC := mock_rpc.NewMockCNIBackendClient(ctrl)
	mocksRPC.EXPECT().NewCNIBackendClient(conn).Return(mockC)

	addNetworkReply := &rpc.AddNetworkReply{Success: true, Ipv4Addr: ipAddr, Ipv6Addr: ip6Addr, DeviceNumber: devNum}
	mockC.EXPECT().AddNetwork(gomock.Any(), gomock.Any()).Return(addNetworkReply, nil)

	addrs := []*net.IP{&ipAddrBytes, &ip6AddrBytes}

	mocksNetwork.EXPECT().SetupNS(gomock.Any(), cmdArgs.IfName, cmdArgs.Netns,
		addrs, int(addNetworkReply.DeviceNumber), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	mocksTypes.EXPECT().PrintResult(gomock.Any(), gomock.Any()).Return(nil)

	err := add(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
	assert.Nil(t, err)
}

func TestCmdAddNetworkErr(t *testing.T) {
	ctrl, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork := setup(t)
	defer ctrl.Finish()

	stdinData, _ := json.Marshal(netConf)

	cmdArgs := &skel.CmdArgs{ContainerID: containerID,
		Netns:     netNS,
		IfName:    ifName,
		StdinData: stdinData}

	mocksTypes.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil)

	conn, _ := grpc.Dial(ipamdAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(conn, nil)
	mockC := mock_rpc.NewMockCNIBackendClient(ctrl)
	mocksRPC.EXPECT().NewCNIBackendClient(conn).Return(mockC)

	addNetworkReply := &rpc.AddNetworkReply{Success: false, Ipv4Addr: ipAddr, DeviceNumber: devNum}
	mockC.EXPECT().AddNetwork(gomock.Any(), gomock.Any()).Return(addNetworkReply, errors.New("Error on AddNetworkReply"))

	err := add(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)

	assert.Error(t, err)
}

func TestCmdAddErrSetupPodNetwork(t *testing.T) {
	ctrl, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork := setup(t)
	defer ctrl.Finish()

	stdinData, _ := json.Marshal(netConf)

	cmdArgs := &skel.CmdArgs{ContainerID: containerID,
		Netns:     netNS,
		IfName:    ifName,
		StdinData: stdinData}

	mocksTypes.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil)

	conn, _ := grpc.Dial(ipamdAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(conn, nil)
	mockC := mock_rpc.NewMockCNIBackendClient(ctrl)
	mocksRPC.EXPECT().NewCNIBackendClient(conn).Return(mockC)

	addNetworkReply := &rpc.AddNetworkReply{Success: true, Ipv4Addr: ipAddr, DeviceNumber: devNum}
	mockC.EXPECT().AddNetwork(gomock.Any(), gomock.Any()).Return(addNetworkReply, nil)

	addrs := []*net.IP{&ipAddrBytes}

	mocksNetwork.EXPECT().SetupNS(gomock.Any(), cmdArgs.IfName, cmdArgs.Netns,
		addrs, int(addNetworkReply.DeviceNumber), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("error on SetupPodNetwork"))

	// when SetupPodNetwork fails, expect to return IP back to datastore
	delNetworkReply := &rpc.DelNetworkReply{Success: true, Ipv4Addr: ipAddr, DeviceNumber: devNum}
	mockC.EXPECT().DelNetwork(gomock.Any(), gomock.Any()).Return(delNetworkReply, nil)

	err := add(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)

	assert.Error(t, err)
}

func TestCmdDel(t *testing.T) {
	ctrl, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork := setup(t)
	defer ctrl.Finish()

	stdinData, _ := json.Marshal(netConf)

	cmdArgs := &skel.CmdArgs{ContainerID: containerID,
		Netns:     netNS,
		IfName:    ifName,
		StdinData: stdinData}

	mocksTypes.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil)

	conn, _ := grpc.Dial(ipamdAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(conn, nil)
	mockC := mock_rpc.NewMockCNIBackendClient(ctrl)
	mocksRPC.EXPECT().NewCNIBackendClient(conn).Return(mockC)

	delNetworkReply := &rpc.DelNetworkReply{Success: true, Ipv4Addr: ipAddr, DeviceNumber: devNum}

	mockC.EXPECT().DelNetwork(gomock.Any(), gomock.Any()).Return(delNetworkReply, nil)

	addrs := []*net.IP{&ipAddrBytes}

	mocksNetwork.EXPECT().TeardownNS(addrs, int(delNetworkReply.DeviceNumber), gomock.Any()).Return(nil)

	err := del(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
	assert.Nil(t, err)
}

func TestCmdDelErrDelNetwork(t *testing.T) {
	ctrl, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork := setup(t)
	defer ctrl.Finish()

	stdinData, _ := json.Marshal(netConf)

	cmdArgs := &skel.CmdArgs{ContainerID: containerID,
		Netns:     netNS,
		IfName:    ifName,
		StdinData: stdinData}

	mocksTypes.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil)

	conn, _ := grpc.Dial(ipamdAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(conn, nil)
	mockC := mock_rpc.NewMockCNIBackendClient(ctrl)
	mocksRPC.EXPECT().NewCNIBackendClient(conn).Return(mockC)

	delNetworkReply := &rpc.DelNetworkReply{Success: false, Ipv4Addr: ipAddr, DeviceNumber: devNum}

	mockC.EXPECT().DelNetwork(gomock.Any(), gomock.Any()).Return(delNetworkReply, errors.New("error on DelNetwork"))

	err := del(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
	assert.Error(t, err)
}

func TestCmdDelErrTeardown(t *testing.T) {
	ctrl, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork := setup(t)
	defer ctrl.Finish()

	stdinData, _ := json.Marshal(netConf)

	cmdArgs := &skel.CmdArgs{ContainerID: containerID,
		Netns:     netNS,
		IfName:    ifName,
		StdinData: stdinData}

	mocksTypes.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil)

	conn, _ := grpc.Dial(ipamdAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(conn, nil)
	mockC := mock_rpc.NewMockCNIBackendClient(ctrl)
	mocksRPC.EXPECT().NewCNIBackendClient(conn).Return(mockC)

	delNetworkReply := &rpc.DelNetworkReply{Success: true, Ipv4Addr: ipAddr, DeviceNumber: devNum}

	mockC.EXPECT().DelNetwork(gomock.Any(), gomock.Any()).Return(delNetworkReply, nil)

	addrs := []*net.IP{&ipAddrBytes}

	mocksNetwork.EXPECT().TeardownNS(addrs, int(delNetworkReply.DeviceNumber), gomock.Any()).Return(errors.New("error on teardown"))

	err := del(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
	assert.Error(t, err)
}
