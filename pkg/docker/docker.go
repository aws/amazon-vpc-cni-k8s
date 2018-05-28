package docker

import (
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"golang.org/x/net/context"

	log "github.com/cihub/seelog"
)

// ContainerInfo provides container information
type ContainerInfo struct {
	ID     string
	Name   string
	K8SUID string
}

// APIs provides Docker API
type APIs interface {
	GetRunningContainers() ([]*ContainerInfo, error)
}

type Client struct{}

func New() *Client {
	return &Client{}
}

func (c *Client) GetRunningContainers() ([]*ContainerInfo, error) {
	var containerInfos []*ContainerInfo

	cli, err := client.NewEnvClient()
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	containers, err := cli.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		return nil, err
	}

	for _, container := range containers {
		log.Infof("GetRunningContainers: Discovered running docker: %s %s %s",
			container.ID, container.Names[0], container.Labels["io.kubernetes.pod.uid"])
		containerInfos = append(containerInfos,
			&ContainerInfo{
				ID:     container.ID,
				Name:   container.Names[0],
				K8SUID: container.Labels["io.kubernetes.pod.uid"]})
	}

	return containerInfos, nil
}
