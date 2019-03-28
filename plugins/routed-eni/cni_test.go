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
	"encoding/json"
	"errors"
	"net"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/grpcwrapper/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/rpcwrapper/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/typeswrapper/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/plugins/routed-eni/driver/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/rpc"
	"github.com/aws/amazon-vpc-cni-k8s/rpc/mocks"
	"google.golang.org/grpc"
)

const (
	containerID  = "test-container"
	netNS        = "/proc/ns/1234"
	ifName       = "eth0"
	cniVersion   = "1.0"
	cniName      = "aws-cni"
	cniType      = "aws-cni"
	ipAddr       = "10.0.1.15"
	devNum       = 4
)

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

type RPCCONN interface {
	Close() error
}

type rpcConn struct{}

func NewRPCCONN() RPCCONN {
	return &rpcConn{}
}

func (*rpcConn) Close() error {
	return nil
}

func TestCmdAdd(t *testing.T) {
	ctrl, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork := setup(t)
	defer ctrl.Finish()

	netconf := &NetConf{CNIVersion: cniVersion,
		Name: cniName,
		Type: cniType}
	stdinData, _ := json.Marshal(netconf)

	cmdArgs := &skel.CmdArgs{ContainerID: containerID,
		Netns:     netNS,
		IfName:    ifName,
		StdinData: stdinData}

	mocksTypes.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil)

	conn, _ := grpc.Dial(ipamDAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(conn, nil)
	mockC := mock_rpc.NewMockCNIBackendClient(ctrl)
	mocksRPC.EXPECT().NewCNIBackendClient(conn).Return(mockC)

	addNetworkReply := &rpc.AddNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum}
	mockC.EXPECT().AddNetwork(gomock.Any(), gomock.Any()).Return(addNetworkReply, nil)

	addr := &net.IPNet{
		IP:   net.ParseIP(addNetworkReply.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}

	mocksNetwork.EXPECT().SetupNS(gomock.Any(), cmdArgs.IfName, cmdArgs.Netns,
		addr, int(addNetworkReply.DeviceNumber), gomock.Any(), gomock.Any()).Return(nil)

	mocksTypes.EXPECT().PrintResult(gomock.Any(), gomock.Any()).Return(nil)

	add(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
}

func TestCmdAddNetworkErr(t *testing.T) {
	ctrl, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork := setup(t)
	defer ctrl.Finish()

	netconf := &NetConf{CNIVersion: cniVersion,
		Name: cniName,
		Type: cniType}
	stdinData, _ := json.Marshal(netconf)

	cmdArgs := &skel.CmdArgs{ContainerID: containerID,
		Netns:     netNS,
		IfName:    ifName,
		StdinData: stdinData}

	mocksTypes.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil)

	conn, _ := grpc.Dial(ipamDAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(conn, nil)
	mockC := mock_rpc.NewMockCNIBackendClient(ctrl)
	mocksRPC.EXPECT().NewCNIBackendClient(conn).Return(mockC)

	addNetworkReply := &rpc.AddNetworkReply{Success: false, IPv4Addr: ipAddr, DeviceNumber: devNum}
	mockC.EXPECT().AddNetwork(gomock.Any(), gomock.Any()).Return(addNetworkReply, errors.New("Error on AddNetworkReply"))

	err := add(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)

	assert.Error(t, err)
}

func TestCmdAddErrSetupPodNetwork(t *testing.T) {
	ctrl, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork := setup(t)
	defer ctrl.Finish()

	netconf := &NetConf{CNIVersion: cniVersion,
		Name: cniName,
		Type: cniType}
	stdinData, _ := json.Marshal(netconf)

	cmdArgs := &skel.CmdArgs{ContainerID: containerID,
		Netns:     netNS,
		IfName:    ifName,
		StdinData: stdinData}

	mocksTypes.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil)

	conn, _ := grpc.Dial(ipamDAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(conn, nil)
	mockC := mock_rpc.NewMockCNIBackendClient(ctrl)
	mocksRPC.EXPECT().NewCNIBackendClient(conn).Return(mockC)

	addNetworkReply := &rpc.AddNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum}
	mockC.EXPECT().AddNetwork(gomock.Any(), gomock.Any()).Return(addNetworkReply, nil)

	addr := &net.IPNet{
		IP:   net.ParseIP(addNetworkReply.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}

	mocksNetwork.EXPECT().SetupNS(gomock.Any(), cmdArgs.IfName, cmdArgs.Netns,
		addr, int(addNetworkReply.DeviceNumber), gomock.Any(), gomock.Any()).Return(errors.New("error on SetupPodNetwork"))

	// when SetupPodNetwork fails, expect to return IP back to datastore
	delNetworkReply := &rpc.DelNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum}
	mockC.EXPECT().DelNetwork(gomock.Any(), gomock.Any()).Return(delNetworkReply, nil)

	err := add(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)

	assert.Error(t, err)
}

func TestCmdDel(t *testing.T) {
	ctrl, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork := setup(t)
	defer ctrl.Finish()

	netconf := &NetConf{CNIVersion: cniVersion,
		Name: cniName,
		Type: cniType}
	stdinData, _ := json.Marshal(netconf)

	cmdArgs := &skel.CmdArgs{ContainerID: containerID,
		Netns:     netNS,
		IfName:    ifName,
		StdinData: stdinData}

	mocksTypes.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil)

	conn, _ := grpc.Dial(ipamDAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(conn, nil)
	mockC := mock_rpc.NewMockCNIBackendClient(ctrl)
	mocksRPC.EXPECT().NewCNIBackendClient(conn).Return(mockC)

	delNetworkReply := &rpc.DelNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum}

	mockC.EXPECT().DelNetwork(gomock.Any(), gomock.Any()).Return(delNetworkReply, nil)

	addr := &net.IPNet{
		IP:   net.ParseIP(delNetworkReply.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}

	mocksNetwork.EXPECT().TeardownNS(addr, int(delNetworkReply.DeviceNumber)).Return(nil)

	del(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
}

func TestCmdDelErrDelNetwork(t *testing.T) {
	ctrl, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork := setup(t)
	defer ctrl.Finish()

	netconf := &NetConf{CNIVersion: cniVersion,
		Name: cniName,
		Type: cniType}
	stdinData, _ := json.Marshal(netconf)

	cmdArgs := &skel.CmdArgs{ContainerID: containerID,
		Netns:     netNS,
		IfName:    ifName,
		StdinData: stdinData}

	mocksTypes.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil)

	conn, _ := grpc.Dial(ipamDAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(conn, nil)
	mockC := mock_rpc.NewMockCNIBackendClient(ctrl)
	mocksRPC.EXPECT().NewCNIBackendClient(conn).Return(mockC)

	delNetworkReply := &rpc.DelNetworkReply{Success: false, IPv4Addr: ipAddr, DeviceNumber: devNum}

	mockC.EXPECT().DelNetwork(gomock.Any(), gomock.Any()).Return(delNetworkReply, errors.New("error on DelNetwork"))

	del(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
}

func TestCmdDelErrTeardown(t *testing.T) {
	ctrl, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork := setup(t)
	defer ctrl.Finish()

	netconf := &NetConf{CNIVersion: cniVersion,
		Name: cniName,
		Type: cniType}
	stdinData, _ := json.Marshal(netconf)

	cmdArgs := &skel.CmdArgs{ContainerID: containerID,
		Netns:     netNS,
		IfName:    ifName,
		StdinData: stdinData}

	mocksTypes.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil)

	conn, _ := grpc.Dial(ipamDAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(conn, nil)
	mockC := mock_rpc.NewMockCNIBackendClient(ctrl)
	mocksRPC.EXPECT().NewCNIBackendClient(conn).Return(mockC)

	delNetworkReply := &rpc.DelNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum}

	mockC.EXPECT().DelNetwork(gomock.Any(), gomock.Any()).Return(delNetworkReply, nil)

	addr := &net.IPNet{
		IP:   net.ParseIP(delNetworkReply.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}

	mocksNetwork.EXPECT().TeardownNS(addr, int(delNetworkReply.DeviceNumber)).Return(errors.New("error on teardown"))

	del(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
}
