package cri

import (
	"context"
	"errors"
	"google.golang.org/grpc"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"
	"os"

	log "github.com/cihub/seelog"
)

const (
	criSocketPath = "unix:///var/run/cri.sock"
	dockerSocketPath = "unix:///var/run/dockershim.sock"
)

// SandboxInfo provides container information
type SandboxInfo struct {
	ID        string
	Namespace string
	Name      string
	K8SUID    string
}

type APIs interface {
	GetRunningPodSandboxes() (map[string]*SandboxInfo, error)
}

type Client struct{}

func New() *Client {
	return &Client{}
}

func (c *Client) GetRunningPodSandboxes() (map[string]*SandboxInfo, error) {
	socketPath := dockerSocketPath
	if info, err := os.Stat("/var/run/cri.sock"); err == nil && !info.IsDir() {
		socketPath = criSocketPath
	}
	log.Debugf("Getting running pod sandboxes from %q", socketPath)
	conn, err := grpc.Dial(socketPath, grpc.WithInsecure())
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
	sandboxes, err := client.ListPodSandbox(context.Background(), &runtimeapi.ListPodSandboxRequest{Filter: filter})
	if err != nil {
		return nil, err
	}

	sandboxInfos := make(map[string]*SandboxInfo)
	for _, sandbox := range sandboxes.Items {
		if sandbox.Metadata == nil {
			continue
		}
		uid := sandbox.Metadata.Uid

		// Verify each pod only has one active sandbox. Kubelet will clean this
		// up if it happens, so we should abort and wait until it does.
		if other, ok := sandboxInfos[uid]; ok {
			log.Errorf("GetRunningPodSandboxes: More than one sandbox with the same pod UID %s", uid)
			log.Errorf("  Sandbox %s: namespace=%s name=%s", other.ID, other.Namespace, other.Name)
			log.Errorf("  Sandbox %s: namespace=%s name=%s", sandbox.Id, sandbox.Metadata.Namespace, sandbox.Metadata.Name)
			return nil, errors.New("UID conflict in container runtime")
		}

		sandboxInfos[uid] = &SandboxInfo{
			ID:        sandbox.Id,
			Namespace: sandbox.Metadata.Namespace,
			Name:      sandbox.Metadata.Name,
			K8SUID:    uid}
	}
	return sandboxInfos, nil
}
