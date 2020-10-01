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

// Package cri is used to get running sandboxes from the CRI socket
package cri

import (
	"context"
	"os"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"google.golang.org/grpc"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"
)

const (
	criSocketPath    = "unix:///var/run/cri.sock"
	dockerSocketPath = "unix:///var/run/dockershim.sock"
)

// SandboxInfo provides container information
type SandboxInfo struct {
	ID string
	IP string
}

// APIs is the CRI interface
type APIs interface {
	GetRunningPodSandboxes(log logger.Logger) ([]*SandboxInfo, error)
}

// Client is an empty struct
type Client struct{}

// New creates a new CRI client
func New() *Client {
	return &Client{}
}

//GetRunningPodSandboxes get running sandboxIDs
func (c *Client) GetRunningPodSandboxes(log logger.Logger) ([]*SandboxInfo, error) {
	ctx := context.TODO()

	socketPath := dockerSocketPath
	if info, err := os.Stat("/var/run/cri.sock"); err == nil && !info.IsDir() {
		socketPath = criSocketPath
	}
	log.Debugf("Getting running pod sandboxes from %q", socketPath)
	conn, err := grpc.Dial(socketPath, grpc.WithInsecure(), grpc.WithNoProxy(), grpc.WithBlock())
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := runtimeapi.NewRuntimeServiceClient(conn)

	// List all ready sandboxes from the CRI
	sandboxes, err := client.ListPodSandbox(ctx, &runtimeapi.ListPodSandboxRequest{
		Filter: &runtimeapi.PodSandboxFilter{
			State: &runtimeapi.PodSandboxStateValue{
				State: runtimeapi.PodSandboxState_SANDBOX_READY,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	sandboxInfos := make([]*SandboxInfo, 0, len(sandboxes.GetItems()))
	for _, sandbox := range sandboxes.GetItems() {
		status, err := client.PodSandboxStatus(ctx, &runtimeapi.PodSandboxStatusRequest{
			PodSandboxId: sandbox.GetId(),
		})
		if err != nil {
			return nil, err
		}

		if state := status.GetStatus().GetState(); state != runtimeapi.PodSandboxState_SANDBOX_READY {
			log.Debugf("Ignoring sandbox %s in unready state %s", sandbox.Id, state)
			continue
		}

		if netmode := status.GetStatus().GetLinux().GetNamespaces().GetOptions().GetNetwork(); netmode != runtimeapi.NamespaceMode_POD {
			log.Debugf("Ignoring sandbox %s with non-pod netns mode %s", sandbox.Id, netmode)
			continue
		}

		ips := []string{status.GetStatus().GetNetwork().GetIp()}
		for _, ip := range status.GetStatus().GetNetwork().GetAdditionalIps() {
			ips = append(ips, ip.GetIp())
		}

		for _, ip := range ips {
			info := SandboxInfo{
				ID: sandbox.GetId(),
				IP: ip,
			}
			sandboxInfos = append(sandboxInfos, &info)
		}
	}
	return sandboxInfos, nil
}
