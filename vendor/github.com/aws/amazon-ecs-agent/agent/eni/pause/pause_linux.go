// +build linux

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

package pause

import (
	"fmt"

	"github.com/aws/amazon-ecs-agent/agent/acs/update_handler/os"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	docker "github.com/fsouza/go-dockerclient"

	log "github.com/cihub/seelog"
	"github.com/pkg/errors"
)

// LoadImage helps load the pause container image for the agent
func (*loader) LoadImage(cfg *config.Config, dockerClient engine.DockerClient) (*docker.Image, error) {
	log.Debugf("Loading pause container tarball: %s", cfg.PauseContainerTarballPath)
	if err := loadFromFile(cfg.PauseContainerTarballPath, dockerClient, os.Default); err != nil {
		return nil, err
	}

	return getPauseContainerImage(
		config.DefaultPauseContainerImageName, config.DefaultPauseContainerTag, dockerClient)
}

func loadFromFile(path string, dockerClient engine.DockerClient, fs os.FileSystem) error {
	pauseContainerReader, err := fs.Open(path)
	if err != nil {
		if err.Error() == noSuchFile {
			return NewNoSuchFileError(errors.Wrapf(err,
				"pause container load: failed to read pause container image: %s", path))
		}
		return errors.Wrapf(err,
			"pause container load: failed to read pause container image: %s", path)
	}
	if err := dockerClient.LoadImage(pauseContainerReader, engine.LoadImageTimeout); err != nil {
		return errors.Wrapf(err,
			"pause container load: failed to load pause container image: %s", path)
	}

	return nil

}

func getPauseContainerImage(name string, tag string, dockerClient engine.DockerClient) (*docker.Image, error) {
	imageName := fmt.Sprintf("%s:%s", name, tag)
	log.Debugf("Inspecting pause container image: %s", imageName)

	image, err := dockerClient.InspectImage(imageName)
	if err != nil {
		return nil, errors.Wrapf(err,
			"pause container load: failed to inspect image: %s", imageName)
	}

	return image, nil
}
