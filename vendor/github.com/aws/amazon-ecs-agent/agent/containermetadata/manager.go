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
	"os"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/utils/ioutilwrapper"
	"github.com/aws/amazon-ecs-agent/agent/utils/oswrapper"

	docker "github.com/fsouza/go-dockerclient"
)

const (
	// metadataEnvironmentVariable is the enviornment variable passed to the
	// container for the metadata file path.
	metadataEnvironmentVariable = "ECS_CONTAINER_METADATA_FILE"
	inspectContainerTimeout     = 30 * time.Second
	metadataFile                = "ecs-container-metadata.json"
	metadataPerm                = 0644
)

// Manager is an interface that allows us to abstract away the metadata
// operations
type Manager interface {
	SetContainerInstanceARN(string)
	Create(*docker.Config, *docker.HostConfig, string, string) error
	Update(string, string, string) error
	Clean(string) error
}

// metadataManager implements the Manager interface
type metadataManager struct {
	// client is the Docker API Client that the metadata manager uses. It defaults
	// to 1.21 on Linux and 1.24 on Windows
	client DockerMetadataClient
	// cluster is the cluster where this agent is run
	cluster string
	// dataDir is the directory where the metadata is being written. For Linux
	// this is a container directory
	dataDir string
	// dataDirOnHost is the directory from which dataDir is mounted for Linux
	// version of the agent
	dataDirOnHost string
	// containerInstanceARN is the Container Instance ARN registered for this agent
	containerInstanceARN string
	// osWrap is a wrapper for 'os' package operations
	osWrap oswrapper.OS
	// ioutilWrap is a wrapper for 'ioutil' package operations
	ioutilWrap ioutilwrapper.IOUtil
}

// NewManager creates a metadataManager for a given DockerTaskEngine settings.
func NewManager(client DockerMetadataClient, cfg *config.Config) Manager {
	return &metadataManager{
		client:        client,
		cluster:       cfg.Cluster,
		dataDir:       cfg.DataDir,
		dataDirOnHost: cfg.DataDirOnHost,
		osWrap:        oswrapper.NewOS(),
		ioutilWrap:    ioutilwrapper.NewIOUtil(),
	}
}

// SetContainerInstanceARN sets the metadataManager's ContainerInstanceArn which is not available
// at its creation as this information is not present immediately at the agent's startup
func (manager *metadataManager) SetContainerInstanceARN(containerInstanceARN string) {
	manager.containerInstanceARN = containerInstanceARN
}

// Create creates the metadata file and adds the metadata directory to
// the container's mounted host volumes
// Pointer hostConfig is modified directly so there is risk of concurrency errors.
func (manager *metadataManager) Create(config *docker.Config, hostConfig *docker.HostConfig, taskARN string, containerName string) error {
	// Create task and container directories if they do not yet exist
	metadataDirectoryPath, err := getMetadataFilePath(taskARN, containerName, manager.dataDir)
	// Stop metadata creation if path is malformed for any reason
	if err != nil {
		return fmt.Errorf("container metadata create for task %s container %s: %v", taskARN, containerName, err)
	}

	err = manager.osWrap.MkdirAll(metadataDirectoryPath, os.ModePerm)
	if err != nil {
		return fmt.Errorf("creating metadata directory for task %s: %v", taskARN, err)
	}

	// Acquire the metadata then write it in JSON format to the file
	metadata := manager.parseMetadataAtContainerCreate(taskARN, containerName)
	err = manager.marshalAndWrite(metadata, taskARN, containerName)
	if err != nil {
		return err
	}

	// Add the directory of this container's metadata to the container's mount binds
	// Then add the destination directory as an environment variable in the container $METADATA
	binds, env := createBindsEnv(hostConfig.Binds, config.Env, manager.dataDirOnHost, metadataDirectoryPath)
	config.Env = env
	hostConfig.Binds = binds
	return nil
}

// Update updates the metadata file after container starts and dynamic metadata is available
func (manager *metadataManager) Update(dockerID string, taskARN string, containerName string) error {
	// Get docker container information through api call
	dockerContainer, err := manager.client.InspectContainer(dockerID, inspectContainerTimeout)
	if err != nil {
		return err
	}

	// Ensure we do not update a container that is invalid or is not running
	if dockerContainer == nil || !dockerContainer.State.Running {
		return fmt.Errorf("container metadata update for contiainer %s in task %s: container not running or invalid", containerName, taskARN)
	}

	// Acquire the metadata then write it in JSON format to the file
	metadata := manager.parseMetadata(dockerContainer, taskARN, containerName)
	return manager.marshalAndWrite(metadata, taskARN, containerName)
}

// Clean removes the metadata files of all containers associated with a task
func (manager *metadataManager) Clean(taskARN string) error {
	metadataPath, err := getTaskMetadataDir(taskARN, manager.dataDir)
	if err != nil {
		return fmt.Errorf("clean task metadata: unable to get metadata directory for task %s: %v", taskARN, err)
	}
	return manager.osWrap.RemoveAll(metadataPath)
}

func (manager *metadataManager) marshalAndWrite(metadata Metadata, taskARN string, containerName string) error {
	data, err := json.MarshalIndent(metadata, "", "\t")
	if err != nil {
		return fmt.Errorf("create metadata for container %s in task %s: failed to marshal metadata: %v", containerName, taskARN, err)
	}

	// Write the metadata to file
	return writeToMetadataFile(manager.osWrap, manager.ioutilWrap, data, taskARN, containerName, manager.dataDir)
}
