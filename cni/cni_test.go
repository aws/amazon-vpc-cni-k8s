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
	"encoding/json"
	"net"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"

	"github.com/aws/amazon-vpc-cni-k8s/rpc"
)

const (
	containerID  = "test-container"
	netNS        = "/proc/ns/1234"
	ifName       = "eth0"
	cniVersion   = "1.0"
	cniName      = "aws-cni"
	cniType      = "aws-cni"
	podNamespace = "test-namespace"
	podName      = "test-pod"
	ipAddr       = "10.0.1.15"
	devNum       = 4
)

type mockCNIBackendClient struct {
	an func(ctx context.Context, in *rpc.AddNetworkRequest, opts ...grpc.CallOption) (*rpc.AddNetworkReply, error)
	dn func(ctx context.Context, in *rpc.DelNetworkRequest, opts ...grpc.CallOption) (*rpc.DelNetworkReply, error)
}

func (m *mockCNIBackendClient) AddNetwork(ctx context.Context, in *rpc.AddNetworkRequest, opts ...grpc.CallOption) (*rpc.AddNetworkReply, error) {
	return m.an(ctx, in, opts...)
}
func (m *mockCNIBackendClient) DelNetwork(ctx context.Context, in *rpc.DelNetworkRequest, opts ...grpc.CallOption) (*rpc.DelNetworkReply, error) {
	return m.dn(ctx, in, opts...)
}

type mockDriver struct {
	sn func(hostVethName string, contVethName string, netnsPath string, addr *net.IPNet, table int) error
	tn func(addr *net.IPNet, table int) error
}

func (m *mockDriver) SetupNS(hostVethName string, contVethName string, netnsPath string, addr *net.IPNet, table int) error {
	return m.sn(hostVethName, contVethName, netnsPath, addr, table)
}
func (m *mockDriver) TeardownNS(addr *net.IPNet, table int) error {
	return m.tn(addr, table)
}

func newMockDriver() *mockDriver {
	return &mockDriver{
		func(hostVethName string, contVethName string, netnsPath string, addr *net.IPNet, table int) error {
			return nil
		},
		func(addr *net.IPNet, table int) error { return nil },
	}
}

func setupCmdArgs() *skel.CmdArgs {
	netconf := &cniPluginConf{
		CNIVersion: cniVersion,
		Name:       cniName,
		Type:       cniType,
	}
	stdinData, _ := json.Marshal(netconf)

	cmdArgs := &skel.CmdArgs{
		ContainerID: containerID,
		Netns:       netNS,
		IfName:      ifName,
		StdinData:   stdinData,
	}
	return cmdArgs
}

func TestCmdAdd(t *testing.T) {
	cmdArgs := setupCmdArgs()
	mock := &mockCNIBackendClient{func(ctx context.Context, in *rpc.AddNetworkRequest, opts ...grpc.CallOption) (*rpc.AddNetworkReply, error) {
		return &rpc.AddNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum}, nil
	}, nil}

	add(cmdArgs, mock, newMockDriver())
	// TODO(tvi): assert.Nil(t, err)
}

func TestCmdAddNetworkErr(t *testing.T) {
	cmdArgs := setupCmdArgs()
	mock := &mockCNIBackendClient{func(ctx context.Context, in *rpc.AddNetworkRequest, opts ...grpc.CallOption) (*rpc.AddNetworkReply, error) {
		return nil, errors.New("Error on AddNetworkReply")
	}, nil}
	err := add(cmdArgs, mock, newMockDriver())
	assert.Error(t, err)
}

func TestCmdAddErrSetupPodNetwork(t *testing.T) {
	cmdArgs := setupCmdArgs()
	mock := &mockCNIBackendClient{func(ctx context.Context, in *rpc.AddNetworkRequest, opts ...grpc.CallOption) (*rpc.AddNetworkReply, error) {
		return &rpc.AddNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum}, nil
	}, nil}
	mockDriver := &mockDriver{func(hostVethName string, contVethName string, netnsPath string, addr *net.IPNet, table int) error {
		return errors.New("Error on SetupPodNetwork")
	}, nil}
	err := add(cmdArgs, mock, mockDriver)
	assert.Error(t, err)
}

func TestCmdDel(t *testing.T) {
	cmdArgs := setupCmdArgs()
	mock := &mockCNIBackendClient{nil, func(ctx context.Context, in *rpc.DelNetworkRequest, opts ...grpc.CallOption) (*rpc.DelNetworkReply, error) {
		return &rpc.DelNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum}, nil
	}}
	err := del(cmdArgs, mock, newMockDriver())
	assert.Nil(t, err)
}

func TestCmdDelErrDelNetwork(t *testing.T) {
	cmdArgs := setupCmdArgs()
	mock := &mockCNIBackendClient{nil, func(ctx context.Context, in *rpc.DelNetworkRequest, opts ...grpc.CallOption) (*rpc.DelNetworkReply, error) {
		return &rpc.DelNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum}, errors.New("Error on DelNetwork")
	}}
	err := del(cmdArgs, mock, newMockDriver())
	assert.Error(t, err)
}

func TestCmdDelErrTeardown(t *testing.T) {
	cmdArgs := setupCmdArgs()
	mock := &mockCNIBackendClient{nil, func(ctx context.Context, in *rpc.DelNetworkRequest, opts ...grpc.CallOption) (*rpc.DelNetworkReply, error) {
		return &rpc.DelNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum}, nil
	}}
	mockDriver := &mockDriver{nil, func(addr *net.IPNet, table int) error { return errors.New("Error on teardown") }}
	err := del(cmdArgs, mock, mockDriver)
	assert.Error(t, err)
}
