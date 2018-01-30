// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package containermetadata

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"

	docker "github.com/fsouza/go-dockerclient"
)

const (
	// MetadataInitial is the initial state of the metadata file which
	// contains metadata provided by the ECS Agent
	MetadataInitialText = "INITIAL"
	// MetadataReady is the final state of the metadata file which indicates
	// it has acquired all the data it needs (Currently from the Agent and Docker)
	MetadataReadyText = "READY"
)

const (
	MetadataInitial MetadataStatus = iota
	MetadataReady
)

// MetadataStatus specifies the current update status of the metadata file.
// The purpose of this status is for users to check if the metadata file has
// reached the stage they need before they read the rest of the file to avoid
// race conditions (Since the final stage will need to be after the container
// starts up
// In the future the metadata may require multiple stages of update and these
// statuses should amended/appended accordingly.
type MetadataStatus int32

func (status MetadataStatus) MarshalText() (text []byte, err error) {
	switch status {
	case MetadataInitial:
		text = []byte(MetadataInitialText)
	case MetadataReady:
		text = []byte(MetadataReadyText)
	default:
		return nil, fmt.Errorf("failed marshalling MetadataStatus %v", status)
	}
	return
}

func (status *MetadataStatus) UnmarshalText(text []byte) error {
	t := string(text)
	switch t {
	case MetadataInitialText:
		*status = MetadataInitial
	case MetadataReadyText:
		*status = MetadataReady
	default:
		return fmt.Errorf("failed unmarshalling MetadataStatus %s", text)
	}
	return nil
}

// DockerMetadataClient is a wrapper for the docker interface functions we need
// We use this as a dummy type to be able to pass in engine.DockerClient to
// our functions without creating import cycles
// We make it exported because we need to use it for testing (Using the MockDockerClient
// in engine package leads to import cycles)
// The problems described above are indications engine.DockerClient needs to be moved
// outside the engine package
type DockerMetadataClient interface {
	InspectContainer(string, time.Duration) (*docker.Container, error)
}

// Network is a struct that keeps track of metadata of a network interface
type Network struct {
	NetworkMode   string   `json:"NetworkMode,omitempty"`
	IPv4Addresses []string `json:"IPv4Addresses,omitempty"`
	IPv6Addresses []string `json:"IPv6Addresses,omitempty"`
}

// NetworkMetadata keeps track of the data we parse from the Network Settings
// in docker containers. While most information is redundant with the internal
// Network struct, we keeps this wrapper in case we wish to add data specifically
// from the NetworkSettings
type NetworkMetadata struct {
	networks []Network
}

// DockerContainerMetadata keeps track of all metadata acquired from Docker inspection
// Has redundancies with engine.DockerContainerMetadata but this packages all
// docker metadata we want in the service so we can change features easily
type DockerContainerMetadata struct {
	containerID         string
	dockerContainerName string
	imageID             string
	imageName           string
	networkMode         string
	ports               []api.PortBinding
	networkInfo         NetworkMetadata
}

// TaskMetadata keeps track of all metadata associated with a task
// provided by AWS, does not depend on the creation of the container
type TaskMetadata struct {
	containerName string
	taskARN       string
}

// Metadata packages all acquired metadata and is used to format it
// into JSON to write to the metadata file. We have it flattened, rather
// than simply containing the previous three structures to simplify JSON
// parsing and avoid exposing those structs in the final metadata file.
type Metadata struct {
	cluster                 string
	taskMetadata            TaskMetadata
	dockerContainerMetadata DockerContainerMetadata
	containerInstanceARN    string
	metadataStatus          MetadataStatus
}

// metadataSerializer is an intermediate struct that converts the information
// in Metadata into information to encode into JSOn
type metadataSerializer struct {
	Cluster              string            `json:"Cluster,omitempty"`
	ContainerInstanceARN string            `json:"ContainerInstanceARN,omitempty"`
	TaskARN              string            `json:"TaskARN,omitempty"`
	ContainerID          string            `json:"ContainerID,omitempty"`
	ContainerName        string            `json:"ContainerName,omitempty"`
	DockerContainerName  string            `json:"DockerContainerName,omitempty"`
	ImageID              string            `json:"ImageID,omitempty"`
	ImageName            string            `json:"ImageName,omitempty"`
	Ports                []api.PortBinding `json:"PortMappings,omitempty"`
	Networks             []Network         `json:"Networks,omitempty"`
	MetadataFileStatus   MetadataStatus    `json:"MetadataFileStatus,omitempty"`
}

func (m Metadata) MarshalJSON() ([]byte, error) {
	return json.Marshal(
		metadataSerializer{
			Cluster:              m.cluster,
			ContainerInstanceARN: m.containerInstanceARN,
			TaskARN:              m.taskMetadata.taskARN,
			ContainerID:          m.dockerContainerMetadata.containerID,
			ContainerName:        m.taskMetadata.containerName,
			DockerContainerName:  m.dockerContainerMetadata.dockerContainerName,
			ImageID:              m.dockerContainerMetadata.imageID,
			ImageName:            m.dockerContainerMetadata.imageName,
			Ports:                m.dockerContainerMetadata.ports,
			Networks:             m.dockerContainerMetadata.networkInfo.networks,
			MetadataFileStatus:   m.metadataStatus,
		})
}
