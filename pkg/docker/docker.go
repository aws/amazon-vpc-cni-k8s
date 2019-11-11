package docker

import (
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"golang.org/x/net/context"

	"github.com/pkg/errors"

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
	GetRunningContainers() (map[string]*ContainerInfo, error)
}

type Client struct{}

func New() *Client {
	return &Client{}
}

func (c *Client) GetRunningContainers() (map[string]*ContainerInfo, error) {
	var containerInfos = make(map[string]*ContainerInfo)

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
		log.Infof("GetRunningContainers: Discovered running docker: %s %s %s State: %s Status: %s ",
			container.ID, container.Names[0], container.Labels["io.kubernetes.pod.uid"], container.State, container.Status)

		containerType, ok := container.Labels["io.kubernetes.docker.type"]
		if !ok {
			log.Infof("GetRunningContainers: skip non pause container")
			continue
		}

		if containerType != "podsandbox" {
			log.Infof("GetRunningContainers: skip container type: %s", containerType)
			continue
		}

		log.Debugf("GetRunningContainers: containerType %s", containerType)

		if container.State != "running" {
			log.Infof("GetRunningContainers: skip container who is not running")
			continue
		}

		uid := container.Labels["io.kubernetes.pod.uid"]
		_, ok = containerInfos[uid]
		if !ok {
			containerInfos[uid] = &ContainerInfo{
				ID:     container.ID,
				Name:   container.Names[0],
				K8SUID: uid}
			continue
		}

		if container.Names[0] != containerInfos[uid].Name {
			log.Infof("GetRunningContainers: same uid matched by container:%s, %s container id %s",
				containerInfos[uid].Name, container.Names[0], container.ID)
			continue
		}

		if container.Names[0] == containerInfos[uid].Name {
			log.Errorf("GetRunningContainers: Conflict container id %s for container %s",
				container.ID, containerInfos[uid].Name)
			return nil, errors.New("conflict docker runtime info")
		}
	}

	return containerInfos, nil
}
