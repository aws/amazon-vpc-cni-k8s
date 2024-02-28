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

	"github.com/aws/amazon-vpc-cni-k8s/pkg/sgpp"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/aws-sdk-go/aws"
	current "github.com/containernetworking/cni/pkg/types/100"

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
	cniVersion     = "1.1"
	cniName        = "aws-cni"
	pluginLogLevel = "Debug"
	pluginLogFile  = "/var/log/aws-routed-eni/plugin.log"
	cniType        = "aws-cni"
	ipAddr         = "10.0.1.15"
	devNum         = 4
)

var netConf = &NetConf{
	NetConf: types.NetConf{
		CNIVersion: cniVersion,
		Name:       cniName,
		Type:       cniType,
	},
	PodSGEnforcingMode: sgpp.DefaultEnforcingMode,
	PluginLogLevel:     pluginLogLevel,
	PluginLogFile:      pluginLogFile,
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

	addNetworkReply := &rpc.AddNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum, NetworkPolicyMode: "none"}
	mockC.EXPECT().AddNetwork(gomock.Any(), gomock.Any()).Return(addNetworkReply, nil)

	v4Addr := &net.IPNet{
		IP:   net.ParseIP(addNetworkReply.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	mocksNetwork.EXPECT().SetupPodNetwork(gomock.Any(), cmdArgs.IfName, cmdArgs.Netns,
		v4Addr, nil, int(addNetworkReply.DeviceNumber), gomock.Any(), gomock.Any()).Return(nil)

	mocksTypes.EXPECT().PrintResult(gomock.Any(), gomock.Any()).Return(nil)

	err := add(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
	assert.Nil(t, err)
}

func TestCmdAddWithNPenabled(t *testing.T) {
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

	npConn, _ := grpc.Dial(npAgentAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(npConn, nil)
	mockNP := mock_rpc.NewMockNPBackendClient(ctrl)
	mocksRPC.EXPECT().NewNPBackendClient(npConn).Return(mockNP)

	addNetworkReply := &rpc.AddNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum, NetworkPolicyMode: "strict"}
	mockC.EXPECT().AddNetwork(gomock.Any(), gomock.Any()).Return(addNetworkReply, nil)

	enforceNpReply := &rpc.EnforceNpReply{Success: true}
	mockNP.EXPECT().EnforceNpToPod(gomock.Any(), gomock.Any()).Return(enforceNpReply, nil)

	v4Addr := &net.IPNet{
		IP:   net.ParseIP(addNetworkReply.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	mocksNetwork.EXPECT().SetupPodNetwork(gomock.Any(), cmdArgs.IfName, cmdArgs.Netns,
		v4Addr, nil, int(addNetworkReply.DeviceNumber), gomock.Any(), gomock.Any()).Return(nil)

	mocksTypes.EXPECT().PrintResult(gomock.Any(), gomock.Any()).Return(nil)

	err := add(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
	assert.Nil(t, err)
}

func TestCmdAddWithNPenabledWithErr(t *testing.T) {
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

	npConn, _ := grpc.Dial(npAgentAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(npConn, nil)
	mockNP := mock_rpc.NewMockNPBackendClient(ctrl)
	mocksRPC.EXPECT().NewNPBackendClient(npConn).Return(mockNP)

	addNetworkReply := &rpc.AddNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum, NetworkPolicyMode: "strict"}
	mockC.EXPECT().AddNetwork(gomock.Any(), gomock.Any()).Return(addNetworkReply, nil)

	enforceNpReply := &rpc.EnforceNpReply{Success: false}
	mockNP.EXPECT().EnforceNpToPod(gomock.Any(), gomock.Any()).Return(enforceNpReply, errors.New("Error on EnforceNpReply"))

	v4Addr := &net.IPNet{
		IP:   net.ParseIP(addNetworkReply.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	mocksNetwork.EXPECT().SetupPodNetwork(gomock.Any(), cmdArgs.IfName, cmdArgs.Netns,
		v4Addr, nil, int(addNetworkReply.DeviceNumber), gomock.Any(), gomock.Any()).Return(nil)

	err := add(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
	assert.Error(t, err)
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

	addNetworkReply := &rpc.AddNetworkReply{Success: false, IPv4Addr: ipAddr, DeviceNumber: devNum, NetworkPolicyMode: "none"}
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

	addNetworkReply := &rpc.AddNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum, NetworkPolicyMode: "none"}
	mockC.EXPECT().AddNetwork(gomock.Any(), gomock.Any()).Return(addNetworkReply, nil)

	addr := &net.IPNet{
		IP:   net.ParseIP(addNetworkReply.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}

	mocksNetwork.EXPECT().SetupPodNetwork(gomock.Any(), cmdArgs.IfName, cmdArgs.Netns,
		addr, nil, int(addNetworkReply.DeviceNumber), gomock.Any(), gomock.Any()).Return(errors.New("error on SetupPodNetwork"))

	// when SetupPodNetwork fails, expect to return IP back to datastore
	delNetworkReply := &rpc.DelNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum}
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

	delNetworkReply := &rpc.DelNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum}

	mockC.EXPECT().DelNetwork(gomock.Any(), gomock.Any()).Return(delNetworkReply, nil)

	addr := &net.IPNet{
		IP:   net.ParseIP(delNetworkReply.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}

	mocksNetwork.EXPECT().TeardownPodNetwork(addr, int(delNetworkReply.DeviceNumber), gomock.Any()).Return(nil)

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

	delNetworkReply := &rpc.DelNetworkReply{Success: false, IPv4Addr: ipAddr, DeviceNumber: devNum}

	mockC.EXPECT().DelNetwork(gomock.Any(), gomock.Any()).Return(delNetworkReply, errors.New("error on DelNetwork"))

	// On DelNetwork fail, the CNI must not return an error to kubelet as deletes are best-effort.
	err := del(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
	assert.Nil(t, err)
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

	delNetworkReply := &rpc.DelNetworkReply{Success: true, IPv4Addr: ipAddr, DeviceNumber: devNum}

	mockC.EXPECT().DelNetwork(gomock.Any(), gomock.Any()).Return(delNetworkReply, nil)

	addr := &net.IPNet{
		IP:   net.ParseIP(delNetworkReply.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}

	mocksNetwork.EXPECT().TeardownPodNetwork(addr, int(delNetworkReply.DeviceNumber), gomock.Any()).Return(errors.New("error on teardown"))

	err := del(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
	assert.Error(t, err)
}

func TestCmdAddForPodENINetwork(t *testing.T) {
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

	addNetworkReply := &rpc.AddNetworkReply{Success: true, IPv4Addr: ipAddr, PodENISubnetGW: "10.0.0.1", PodVlanId: 1,
		PodENIMAC: "eniHardwareAddr", ParentIfIndex: 2, NetworkPolicyMode: "none"}
	mockC.EXPECT().AddNetwork(gomock.Any(), gomock.Any()).Return(addNetworkReply, nil)

	addr := &net.IPNet{
		IP:   net.ParseIP(addNetworkReply.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	mocksNetwork.EXPECT().SetupBranchENIPodNetwork(gomock.Any(), cmdArgs.IfName, cmdArgs.Netns, addr, nil, 1, "eniHardwareAddr",
		"10.0.0.1", 2, gomock.Any(), sgpp.EnforcingModeStrict, gomock.Any()).Return(nil)

	mocksTypes.EXPECT().PrintResult(gomock.Any(), gomock.Any()).Return(nil)

	err := add(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
	assert.Nil(t, err)
}

func TestCmdDelForPodENINetwork(t *testing.T) {
	ctrl, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork := setup(t)
	defer ctrl.Finish()

	stdinData, _ := json.Marshal(netConf)

	cmdArgs := &skel.CmdArgs{
		ContainerID: containerID,
		Netns:       netNS,
		IfName:      ifName,
		StdinData:   stdinData}

	mocksTypes.EXPECT().LoadArgs(gomock.Any(), gomock.Any()).Return(nil)

	conn, _ := grpc.Dial(ipamdAddress, grpc.WithInsecure())

	mocksGRPC.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(conn, nil)
	mockC := mock_rpc.NewMockCNIBackendClient(ctrl)
	mocksRPC.EXPECT().NewCNIBackendClient(conn).Return(mockC)

	delNetworkReply := &rpc.DelNetworkReply{Success: true, IPv4Addr: ipAddr, PodVlanId: 1}

	mockC.EXPECT().DelNetwork(gomock.Any(), gomock.Any()).Return(delNetworkReply, nil)

	addr := &net.IPNet{
		IP:   net.ParseIP(delNetworkReply.IPv4Addr),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	mocksNetwork.EXPECT().TeardownBranchENIPodNetwork(addr, 1, sgpp.EnforcingModeStrict, gomock.Any()).Return(nil)

	err := del(cmdArgs, mocksTypes, mocksGRPC, mocksRPC, mocksNetwork)
	assert.Nil(t, err)
}

func Test_tryDelWithPrevResult(t *testing.T) {
	type teardownBranchENIPodNetworkCall struct {
		containerAddr      *net.IPNet
		vlanID             int
		podSGEnforcingMode sgpp.EnforcingMode
		err                error
	}
	type fields struct {
		teardownBranchENIPodNetworkCalls []teardownBranchENIPodNetworkCall
	}
	type args struct {
		conf         *NetConf
		k8sArgs      K8sArgs
		contVethName string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    bool
		wantErr error
	}{
		{
			name: "successfully deleted with information from prevResult - with enforcing mode standard",
			fields: fields{
				teardownBranchENIPodNetworkCalls: []teardownBranchENIPodNetworkCall{
					{
						containerAddr: &net.IPNet{
							IP:   net.ParseIP("192.168.1.1"),
							Mask: net.CIDRMask(32, 32),
						},
						vlanID:             7,
						podSGEnforcingMode: sgpp.EnforcingModeStandard,
					},
				},
			},
			args: args{
				conf: &NetConf{
					NetConf: types.NetConf{
						PrevResult: &current.Result{
							Interfaces: []*current.Interface{
								{
									Name: "enicc21c2d7785",
								},
								{
									Name:    "eth0",
									Sandbox: "/proc/42/ns/net",
								},
								{
									Name: "dummycc21c2d7785",
									Mac:  "7",
								},
							},
							IPs: []*current.IPConfig{
								{
									Address: net.IPNet{
										IP:   net.ParseIP("192.168.1.1"),
										Mask: net.CIDRMask(32, 32),
									},
									Interface: aws.Int(1),
								},
							},
						},
					},
					PodSGEnforcingMode: sgpp.EnforcingModeStandard,
				},
				k8sArgs: K8sArgs{
					K8S_POD_NAMESPACE: "default",
					K8S_POD_NAME:      "sample-pod",
				},
				contVethName: "eth0",
			},
			want: true,
		},
		{
			name: "successfully deleted with information from prevResult - with enforcing mode strict",
			fields: fields{
				teardownBranchENIPodNetworkCalls: []teardownBranchENIPodNetworkCall{
					{
						containerAddr: &net.IPNet{
							IP:   net.ParseIP("192.168.1.1"),
							Mask: net.CIDRMask(32, 32),
						},
						vlanID:             7,
						podSGEnforcingMode: sgpp.EnforcingModeStrict,
					},
				},
			},
			args: args{
				conf: &NetConf{
					NetConf: types.NetConf{
						PrevResult: &current.Result{
							Interfaces: []*current.Interface{
								{
									Name: "enicc21c2d7785",
								},
								{
									Name:    "eth0",
									Sandbox: "/proc/42/ns/net",
								},
								{
									Name: "dummycc21c2d7785",
									Mac:  "7",
								},
							},
							IPs: []*current.IPConfig{
								{
									Address: net.IPNet{
										IP:   net.ParseIP("192.168.1.1"),
										Mask: net.CIDRMask(32, 32),
									},
									Interface: aws.Int(1),
								},
							},
						},
					},
					PodSGEnforcingMode: sgpp.EnforcingModeStrict,
				},
				k8sArgs: K8sArgs{
					K8S_POD_NAMESPACE: "default",
					K8S_POD_NAME:      "sample-pod",
				},
				contVethName: "eth0",
			},
			want: true,
		},
		{
			name: "failed to delete due to teardownBranchENIPodNetworkCall failed",
			fields: fields{
				teardownBranchENIPodNetworkCalls: []teardownBranchENIPodNetworkCall{
					{
						containerAddr: &net.IPNet{
							IP:   net.ParseIP("192.168.1.1"),
							Mask: net.CIDRMask(32, 32),
						},
						vlanID:             7,
						podSGEnforcingMode: sgpp.EnforcingModeStandard,
						err:                errors.New("some error"),
					},
				},
			},
			args: args{
				conf: &NetConf{
					NetConf: types.NetConf{
						PrevResult: &current.Result{
							Interfaces: []*current.Interface{
								{
									Name: "enicc21c2d7785",
								},
								{
									Name:    "eth0",
									Sandbox: "/proc/42/ns/net",
								},
								{
									Name: "dummycc21c2d7785",
									Mac:  "7",
								},
							},
							IPs: []*current.IPConfig{
								{
									Address: net.IPNet{
										IP:   net.ParseIP("192.168.1.1"),
										Mask: net.CIDRMask(32, 32),
									},
									Interface: aws.Int(1),
								},
							},
						},
					},
					PodSGEnforcingMode: sgpp.EnforcingModeStandard,
				},
				k8sArgs: K8sArgs{
					K8S_POD_NAMESPACE: "default",
					K8S_POD_NAME:      "sample-pod",
				},
				contVethName: "eth0",
			},
			wantErr: errors.New("some error"),
		},
		{
			name:   "dummy interface don't exists",
			fields: fields{},
			args: args{
				conf: &NetConf{
					NetConf: types.NetConf{
						PrevResult: &current.Result{
							Interfaces: []*current.Interface{
								{
									Name: "enicc21c2d7785",
								},
								{
									Name:    "eth0",
									Sandbox: "/proc/42/ns/net",
								},
							},
							IPs: []*current.IPConfig{
								{
									Address: net.IPNet{
										IP:   net.ParseIP("192.168.1.1"),
										Mask: net.CIDRMask(32, 32),
									},
									Interface: aws.Int(1),
								},
							},
						},
					},
					PodSGEnforcingMode: sgpp.EnforcingModeStandard,
				},
				k8sArgs: K8sArgs{
					K8S_POD_NAMESPACE: "default",
					K8S_POD_NAME:      "sample-pod",
				},
				contVethName: "eth0",
			},
			want: false,
		},
		{
			name:   "malformed vlanID in prevResult - xxx",
			fields: fields{},
			args: args{
				conf: &NetConf{
					NetConf: types.NetConf{
						PrevResult: &current.Result{
							Interfaces: []*current.Interface{
								{
									Name: "enicc21c2d7785",
								},
								{
									Name:    "eth0",
									Sandbox: "/proc/42/ns/net",
								},
								{
									Name: "dummycc21c2d7785",
									Mac:  "xxx",
								},
							},
							IPs: []*current.IPConfig{
								{
									Address: net.IPNet{
										IP:   net.ParseIP("192.168.1.1"),
										Mask: net.CIDRMask(32, 32),
									},
									Interface: aws.Int(1),
								},
							},
						},
					},
					PodSGEnforcingMode: sgpp.EnforcingModeStandard,
				},
				k8sArgs: K8sArgs{
					K8S_POD_NAMESPACE: "default",
					K8S_POD_NAME:      "sample-pod",
				},
				contVethName: "eth0",
			},
			wantErr: errors.New("malformed vlanID in prevResult: xxx"),
		},
		{
			name:   "vlanID: 0",
			fields: fields{},
			args: args{
				conf: &NetConf{
					NetConf: types.NetConf{
						PrevResult: &current.Result{
							Interfaces: []*current.Interface{
								{
									Name: "enicc21c2d7785",
								},
								{
									Name:    "eth0",
									Sandbox: "/proc/42/ns/net",
								},
								{
									Name: "dummycc21c2d7785",
									Mac:  "0",
								},
							},
							IPs: []*current.IPConfig{
								{
									Address: net.IPNet{
										IP:   net.ParseIP("192.168.1.1"),
										Mask: net.CIDRMask(32, 32),
									},
									Interface: aws.Int(1),
								},
							},
						},
					},
				},
				k8sArgs: K8sArgs{
					K8S_POD_NAMESPACE: "default",
					K8S_POD_NAME:      "sample-pod",
				},
				contVethName: "eth0",
			},
			want: false,
		},
		{
			name:   "confVeth don't exists",
			fields: fields{},
			args: args{
				conf: &NetConf{
					NetConf: types.NetConf{
						PrevResult: &current.Result{
							Interfaces: []*current.Interface{
								{
									Name: "enicc21c2d7785",
								},
								{
									Name: "dummycc21c2d7785",
									Mac:  "7",
								},
							},
							IPs: []*current.IPConfig{
								{
									Address: net.IPNet{
										IP:   net.ParseIP("192.168.1.1"),
										Mask: net.CIDRMask(32, 32),
									},
									Interface: aws.Int(1),
								},
							},
						},
					},
					PodSGEnforcingMode: sgpp.EnforcingModeStandard,
				},
				k8sArgs: K8sArgs{
					K8S_POD_NAMESPACE: "default",
					K8S_POD_NAME:      "sample-pod",
				},
				contVethName: "eth0",
			},
			wantErr: errors.New("cannot find contVethName eth0 in prevResult"),
		},
		{
			name:   "container IP don't exists",
			fields: fields{},
			args: args{
				conf: &NetConf{
					NetConf: types.NetConf{
						PrevResult: &current.Result{
							Interfaces: []*current.Interface{
								{
									Name: "enicc21c2d7785",
								},
								{
									Name:    "eth0",
									Sandbox: "/proc/42/ns/net",
								},
								{
									Name: "dummycc21c2d7785",
									Mac:  "7",
								},
							},
							IPs: []*current.IPConfig{},
						},
					},
					PodSGEnforcingMode: sgpp.EnforcingModeStandard,
				},
				k8sArgs: K8sArgs{
					K8S_POD_NAMESPACE: "default",
					K8S_POD_NAME:      "sample-pod",
				},
				contVethName: "eth0",
			},
			wantErr: errors.New("found 0 containerIPs for eth0 in prevResult"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testLogCfg := logger.Configuration{
				LogLevel:    "Debug",
				LogLocation: "stdout",
			}
			testLogger := logger.New(&testLogCfg)

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			driverClient := mock_driver.NewMockNetworkAPIs(ctrl)
			for _, call := range tt.fields.teardownBranchENIPodNetworkCalls {
				driverClient.EXPECT().TeardownBranchENIPodNetwork(call.containerAddr, call.vlanID, call.podSGEnforcingMode, gomock.Any()).Return(call.err)
			}

			got, err := tryDelWithPrevResult(driverClient, tt.args.conf, tt.args.k8sArgs, tt.args.contVethName, "/proc/1/ns", testLogger)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func Test_teardownPodNetworkWithPrevResult(t *testing.T) {
	type teardownPodNetworkCall struct {
		containerAddr *net.IPNet
		deviceNumber  int
		err           error
	}
	type fields struct {
		teardownPodNetworkCalls []teardownPodNetworkCall
	}
	type args struct {
		conf         *NetConf
		k8sArgs      K8sArgs
		contVethName string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		handled bool
	}{
		{
			name: "successfully deleted with information from prevResult",
			fields: fields{
				teardownPodNetworkCalls: []teardownPodNetworkCall{
					{
						containerAddr: &net.IPNet{
							IP:   net.ParseIP("192.168.1.1"),
							Mask: net.CIDRMask(32, 32),
						},
						deviceNumber: 5,
					},
				},
			},
			args: args{
				conf: &NetConf{
					NetConf: types.NetConf{
						PrevResult: &current.Result{
							Interfaces: []*current.Interface{
								{
									Name: "enicc21c2d7785",
								},
								{
									Name:    "eth0",
									Sandbox: "/proc/42/ns/net",
								},
								{
									Name:    "dummycc21c2d7785",
									Mac:     "0",
									Sandbox: "5",
								},
							},
							IPs: []*current.IPConfig{
								{
									Address: net.IPNet{
										IP:   net.ParseIP("192.168.1.1"),
										Mask: net.CIDRMask(32, 32),
									},
									Interface: aws.Int(1),
								},
							},
						},
					},
				},
				k8sArgs: K8sArgs{
					K8S_POD_NAMESPACE: "default",
					K8S_POD_NAME:      "sample-pod",
				},
				contVethName: "eth0",
			},
			handled: true,
		},
		{
			name: "failed to delete due to teardownPodNetworkCall failed",
			fields: fields{
				teardownPodNetworkCalls: []teardownPodNetworkCall{
					{
						containerAddr: &net.IPNet{
							IP:   net.ParseIP("192.168.1.1"),
							Mask: net.CIDRMask(32, 32),
						},
						deviceNumber: 5,
						err:          errors.New("some error"),
					},
				},
			},
			args: args{
				conf: &NetConf{
					NetConf: types.NetConf{
						PrevResult: &current.Result{
							Interfaces: []*current.Interface{
								{
									Name: "enicc21c2d7785",
								},
								{
									Name:    "eth0",
									Sandbox: "/proc/42/ns/net",
								},
								{
									Name:    "dummycc21c2d7785",
									Mac:     "0",
									Sandbox: "5",
								},
							},
							IPs: []*current.IPConfig{
								{
									Address: net.IPNet{
										IP:   net.ParseIP("192.168.1.1"),
										Mask: net.CIDRMask(32, 32),
									},
									Interface: aws.Int(1),
								},
							},
						},
					},
				},
				k8sArgs: K8sArgs{
					K8S_POD_NAMESPACE: "default",
					K8S_POD_NAME:      "sample-pod",
				},
				contVethName: "eth0",
			},
			handled: false,
		},
		{
			name:   "dummy interface does not exist",
			fields: fields{},
			args: args{
				conf: &NetConf{
					NetConf: types.NetConf{
						PrevResult: &current.Result{
							Interfaces: []*current.Interface{
								{
									Name: "enicc21c2d7785",
								},
								{
									Name:    "eth0",
									Sandbox: "/proc/42/ns/net",
								},
							},
							IPs: []*current.IPConfig{
								{
									Address: net.IPNet{
										IP:   net.ParseIP("192.168.1.1"),
										Mask: net.CIDRMask(32, 32),
									},
									Interface: aws.Int(1),
								},
							},
						},
					},
				},
				k8sArgs: K8sArgs{
					K8S_POD_NAMESPACE: "default",
					K8S_POD_NAME:      "sample-pod",
				},
				contVethName: "eth0",
			},
			handled: false,
		},
		{
			name:   "malformed vlanID in prevResult - xxx",
			fields: fields{},
			args: args{
				conf: &NetConf{
					NetConf: types.NetConf{
						PrevResult: &current.Result{
							Interfaces: []*current.Interface{
								{
									Name: "enicc21c2d7785",
								},
								{
									Name:    "eth0",
									Sandbox: "/proc/42/ns/net",
								},
								{
									Name:    "dummycc21c2d7785",
									Mac:     "xxx",
									Sandbox: "5",
								},
							},
							IPs: []*current.IPConfig{
								{
									Address: net.IPNet{
										IP:   net.ParseIP("192.168.1.1"),
										Mask: net.CIDRMask(32, 32),
									},
									Interface: aws.Int(1),
								},
							},
						},
					},
				},
				k8sArgs: K8sArgs{
					K8S_POD_NAMESPACE: "default",
					K8S_POD_NAME:      "sample-pod",
				},
				contVethName: "eth0",
			},
			handled: false,
		},
		{
			name:   "vlanID != 0",
			fields: fields{},
			args: args{
				conf: &NetConf{
					NetConf: types.NetConf{
						PrevResult: &current.Result{
							Interfaces: []*current.Interface{
								{
									Name: "enicc21c2d7785",
								},
								{
									Name:    "eth0",
									Sandbox: "/proc/42/ns/net",
								},
								{
									Name: "dummycc21c2d7785",
									Mac:  "7",
								},
							},
							IPs: []*current.IPConfig{
								{
									Address: net.IPNet{
										IP:   net.ParseIP("192.168.1.1"),
										Mask: net.CIDRMask(32, 32),
									},
									Interface: aws.Int(1),
								},
							},
						},
					},
				},
				k8sArgs: K8sArgs{
					K8S_POD_NAMESPACE: "default",
					K8S_POD_NAME:      "sample-pod",
				},
				contVethName: "eth0",
			},
			handled: false,
		},
		{
			name:   "missing device number",
			fields: fields{},
			args: args{
				conf: &NetConf{
					NetConf: types.NetConf{
						PrevResult: &current.Result{
							Interfaces: []*current.Interface{
								{
									Name: "enicc21c2d7785",
								},
								{
									Name:    "eth0",
									Sandbox: "/proc/42/ns/net",
								},
								{
									Name: "dummycc21c2d7785",
									Mac:  "0",
								},
							},
							IPs: []*current.IPConfig{
								{
									Address: net.IPNet{
										IP:   net.ParseIP("192.168.1.1"),
										Mask: net.CIDRMask(32, 32),
									},
									Interface: aws.Int(1),
								},
							},
						},
					},
				},
				k8sArgs: K8sArgs{
					K8S_POD_NAMESPACE: "default",
					K8S_POD_NAME:      "sample-pod",
				},
				contVethName: "eth0",
			},
			handled: false,
		},
		// confVeth not existing and container IP not existing are covered by Test_tryDelWithPrevResult
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testLogCfg := logger.Configuration{
				LogLevel:    "Debug",
				LogLocation: "stdout",
			}
			testLogger := logger.New(&testLogCfg)

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			driverClient := mock_driver.NewMockNetworkAPIs(ctrl)
			for _, call := range tt.fields.teardownPodNetworkCalls {
				driverClient.EXPECT().TeardownPodNetwork(call.containerAddr, call.deviceNumber, gomock.Any()).Return(call.err)
			}

			handled := teardownPodNetworkWithPrevResult(driverClient, tt.args.conf, tt.args.k8sArgs, tt.args.contVethName, testLogger)
			assert.Equal(t, tt.handled, handled)
		})
	}
}
