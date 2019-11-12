package cri

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"

	log "github.com/cihub/seelog"
)

const (
	// TODO: Parameterize?
	criSocketPath = "unix:///var/run/cri.sock"
)

// ContainerInfo provides container information
type ContainerInfo struct {
	ID     string
	Name   string
	K8SUID string
}

type APIs interface {
	GetRunningContainers() (map[string]*ContainerInfo, error)
}

type Client struct{}

func New() *Client {
	return &Client{}
}

func (c *Client) GetRunningContainers() (map[string]*ContainerInfo, error) {
	conn, err := grpc.Dial(criSocketPath, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := runtimeapi.NewRuntimeServiceClient(conn)

	// TODO: Filter on ready state?
	sandboxes, err := client.ListPodSandbox(context.Background(), &runtimeapi.ListPodSandboxRequest{})
	if err != nil {
		return nil, err
	}

	containerInfos := make(map[string]*ContainerInfo)
	for _, sandbox := range sandboxes.Items {
		uid := sandbox.Metadata.Uid
		_, ok := containerInfos[uid]
		if !ok {
			containerInfos[uid] = &ContainerInfo{
				ID:     sandbox.Id,
				Name:   sandbox.Metadata.Name,
				K8SUID: uid}
			continue
		}

		// TODO: Are these checks relevant for CRI?
		if sandbox.Metadata.Name != containerInfos[uid].Name {
			log.Infof("GetRunningContainers: same uid matched by container:%s, %s container id %s",
				containerInfos[uid].Name, sandbox.Metadata.Name, sandbox.Id)
			continue
		}

		if sandbox.Metadata.Name == containerInfos[uid].Name {
			log.Errorf("GetRunningContainers: Conflict container id %s for container %s",
				sandbox.Id, containerInfos[uid].Name)
			return nil, errors.New("conflict docker runtime info")
		}
	}
	return containerInfos, nil
}
