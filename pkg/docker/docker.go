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

func GetRunningContainers() ([]ContainerInfo, error) {
	var containerInfos []ContainerInfo

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
		containerInfos = append(containerInfos,
			ContainerInfo{
				ID:     container.ID,
				Name:   container.Names[0],
				K8SUID: container.Labels["io.kubernetes.pod.uid"]})
	}

	log.Info("GetRunningContainers: Discovered running docker: ", containerInfos)
	return containerInfos, nil
}
