// +build !integration
// Copyright 2014-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package dockerclient

import (
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/engine/dockeriface"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockeriface/mocks"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

const expectedEndpoint = "expectedEndpoint"

func TestGetDefaultClientSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	expectedClient := mock_dockeriface.NewMockClient(ctrl)
	newVersionedClient = func(endpoint, version string) (dockeriface.Client, error) {
		mockClient := mock_dockeriface.NewMockClient(ctrl)
		if version == string(getDefaultVersion()) {
			mockClient = expectedClient
		}
		mockClient.EXPECT().Version().Return(&docker.Env{}, nil).AnyTimes()
		mockClient.EXPECT().Ping().AnyTimes()

		return mockClient, nil
	}

	factory := NewFactory(expectedEndpoint)
	actualClient, err := factory.GetDefaultClient()
	assert.Nil(t, err)
	assert.Equal(t, expectedClient, actualClient)
}

func TestFindSupportedAPIVersions(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	agentVersions := getAgentVersions()
	allVersions := getKnownAPIVersions()

	// Set up the mocks and expectations
	mockClients := make(map[string]*mock_dockeriface.MockClient)

	// Ensure that agent pings all known versions of Docker API
	for i := 0; i < len(allVersions); i++ {
		mockClients[string(allVersions[i])] = mock_dockeriface.NewMockClient(ctrl)
		mockClients[string(allVersions[i])].EXPECT().Version().Return(&docker.Env{}, nil).AnyTimes()
		mockClients[string(allVersions[i])].EXPECT().Ping().AnyTimes()
	}

	// Define the function for the mock client
	// For simplicity, we will pretend all versions of docker are available
	newVersionedClient = func(endpoint, version string) (dockeriface.Client, error) {
		return mockClients[version], nil
	}

	factory := NewFactory(expectedEndpoint)
	actualVersions := factory.FindSupportedAPIVersions()

	assert.Equal(t, len(agentVersions), len(actualVersions))
	for i := 0; i < len(actualVersions); i++ {
		assert.Equal(t, agentVersions[i], actualVersions[i])
	}
}

func TestVerifyAgentVersions(t *testing.T) {
	var isKnown = func(v1 DockerVersion) bool {
		for _, v2 := range getAgentVersions() {
			if v1 == v2 {
				return true
			}
		}
		return false
	}

	// Validate that agentVersions is a subset of allVersions
	for _, agentVersion := range getAgentVersions() {
		assert.True(t, isKnown(agentVersion))
	}
}

func TestFindSupportedAPIVersionsFromMinAPIVersions(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	agentVersions := getAgentVersions()
	allVersions := getKnownAPIVersions()

	// Set up the mocks and expectations
	mockClients := make(map[string]*mock_dockeriface.MockClient)

	// Ensure that agent pings all known versions of Docker API
	for i := 0; i < len(allVersions); i++ {
		mockClients[string(allVersions[i])] = mock_dockeriface.NewMockClient(ctrl)
		mockClients[string(allVersions[i])].EXPECT().Version().Return(&docker.Env{"MinAPIVersion=1.12", "ApiVersion=1.30"}, nil).AnyTimes()
		mockClients[string(allVersions[i])].EXPECT().Ping().AnyTimes()
	}

	// Define the function for the mock client
	// For simplicity, we will pretend all versions of docker are available
	newVersionedClient = func(endpoint, version string) (dockeriface.Client, error) {
		return mockClients[version], nil
	}

	factory := NewFactory(expectedEndpoint)
	actualVersions := factory.FindSupportedAPIVersions()

	assert.Equal(t, len(agentVersions), len(actualVersions))
	for i := 0; i < len(actualVersions); i++ {
		assert.Equal(t, agentVersions[i], actualVersions[i])
	}
}

func TestCompareDockerVersionsWithMinAPIVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	minAPIVersion := "1.12"
	apiVersion := "1.27"
	versions := []string{"1.11", "1.29"}
	rightVersion := "1.25"

	for _, version := range versions {
		_, err := getDockerClientForVersion("endpoint", version, minAPIVersion, apiVersion)
		assert.EqualError(t, err, "version detection using MinAPIVersion: unsupported version: "+version)
	}

	mockClients := make(map[string]*mock_dockeriface.MockClient)
	newVersionedClient = func(endpoint, version string) (dockeriface.Client, error) {
		mockClients[version] = mock_dockeriface.NewMockClient(ctrl)
		mockClients[version].EXPECT().Ping()
		return mockClients[version], nil
	}
	client, _ := getDockerClientForVersion("endpoint", rightVersion, minAPIVersion, apiVersion)
	assert.Equal(t, mockClients[rightVersion], client)
}
