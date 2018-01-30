// +build !linux

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
	"runtime"

	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/pkg/errors"
)

// LoadImage returns UnsupportedPlatformError on the unsupported platform
func (*loader) LoadImage(cfg *config.Config, dockerClient engine.DockerClient) (*docker.Image, error) {
	return nil, NewUnsupportedPlatformError(errors.Errorf(
		"pause container load: unsupported platform: %s/%s",
		runtime.GOOS, runtime.GOARCH))
}
