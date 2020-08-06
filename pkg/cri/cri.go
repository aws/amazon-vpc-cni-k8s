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

package cri

import (
	"context"
	"errors"
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
	// ID of the PodSandbox
	ID string
	// IP address of the PodSandbox
	IP string
	// Pod namespace of the sandbox
	Namespace string
	// Pod name of the sandbox
	Name string
	// Pod UID of the sandbox
	K8SUID string
}

type APIs interface {
	GetRunningPodSandboxes(log logger.Logger) (map[string]*SandboxInfo, error)
}

type Client struct{}

func New() *Client {
	return &Client{}
}

//GetRunningPodSandboxes get running sandboxIDs
func (c *Client) GetRunningPodSandboxes(log logger.Logger) (map[string]*SandboxInfo, error) {
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
	filter := &runtimeapi.PodSandboxFilter{
		State: &runtimeapi.PodSandboxStateValue{
			State: runtimeapi.PodSandboxState_SANDBOX_READY,
		},
	}
	sandboxes, err := client.ListPodSandbox(ctx, &runtimeapi.ListPodSandboxRequest{Filter: filter})
	if err != nil {
		return nil, err
	}

	sandboxInfos := make(map[string]*SandboxInfo)
	for _, sandbox := range sandboxes.GetItems() {
		if sandbox.Metadata == nil {
			continue
		}
		uid := sandbox.Metadata.Uid

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

		// Verify each pod only has one active sandbox. Kubelet will clean this
		// up if it happens, so we should abort and wait until it does.
		if other, ok := sandboxInfos[uid]; ok {
			log.Errorf("GetRunningPodSandboxes: More than one sandbox with the same pod UID %s", uid)
			log.Errorf("  Sandbox %s: namespace=%s name=%s", other.ID, other.Namespace, other.Name)
			log.Errorf("  Sandbox %s: namespace=%s name=%s", sandbox.Id, sandbox.Metadata.Namespace, sandbox.Metadata.Name)
			return nil, errors.New("UID conflict in container runtime")
		}

		// no dual-stack support here so we only get the IPv4 address
		ip := status.GetStatus().GetNetwork().GetIp()

		sandboxInfos[uid] = &SandboxInfo{
			ID:        sandbox.Id,
			IP:        ip,
			Namespace: sandbox.Metadata.Namespace,
			Name:      sandbox.Metadata.Name,
			K8SUID:    uid}
	}
	return sandboxInfos, nil
}
