// Copyright 2014-2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

// Portions copyright 2012-2015 Docker, Inc.  Please see LICENSE for applicable
// license terms and NOTICE for applicable notices.
package dockerauth

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/aws/amazon-ecs-agent/agent/api"

	"github.com/cihub/seelog"
	docker "github.com/fsouza/go-dockerclient"
)

func NewDockerAuthProvider(authType string, authData json.RawMessage) DockerAuthProvider {
	return &dockerAuthProvider{
		authMap: parseAuthData(authType, authData),
	}
}

type dockerAuthProvider struct {
	authMap dockerAuths
}

// map from registry url (minus schema) to auth information
type dockerAuths map[string]docker.AuthConfiguration

type dockercfgConfigEntry struct {
	Auth string `json:"auth"`
}

type dockercfgData map[string]dockercfgConfigEntry

// GetAuthconfig retrieves the correct auth configuration for the given repository
func (authProvider *dockerAuthProvider) GetAuthconfig(image string, authData *api.ECRAuthData) (docker.AuthConfiguration, error) {
	// Ignore 'tag', not used in auth determination
	repository, _ := docker.ParseRepositoryTag(image)
	authDataMap := authProvider.authMap

	// Ignore repo/image name for some auth checks (see use of 'image' below for where it's not ignored.
	indexName, _ := splitReposName(repository)

	if isDockerhubHostname(indexName) {
		return authDataMap[dockerRegistryKey], nil
	}

	// Try to find the longest match that at least matches the hostname
	// This is to support e.g. 'registry.tld: auth1, registry.tld/username:
	// auth2' for multiple different auths on the same registry.
	longestKey := ""
	authConfigKey := indexName

	// Take a direct match of the index hostname as a sane default
	if _, found := authDataMap[authConfigKey]; found && len(authConfigKey) > len(longestKey) {
		longestKey = authConfigKey
	}

	for registry := range authDataMap {
		nameParts := strings.SplitN(registry, "/", 2)
		hostname := nameParts[0]

		// Only ever take a new key if the hostname matches in normal cases
		if authConfigKey == hostname {
			if longestKey == "" {
				longestKey = registry
			} else if len(registry) > len(longestKey) && strings.HasPrefix(image, registry) {
				// If we have a longer match, that indicates a username / namespace appended
				longestKey = registry
			}
		}
	}
	if longestKey != "" {
		return authDataMap[longestKey], nil
	}
	return docker.AuthConfiguration{}, nil
}

// Normalize all auth types into a uniform 'dockerAuths' type.
// On error, any appropriate information will be logged and an empty dockerAuths will be returned
func parseAuthData(authType string, authData json.RawMessage) dockerAuths {
	intermediateAuthData := make(dockerAuths)
	switch authType {
	case "docker":
		err := json.Unmarshal(authData, &intermediateAuthData)
		if err != nil {
			seelog.Warn("Could not parse 'docker' type auth config")
			return dockerAuths{}
		}
	case "dockercfg":
		var base64dAuthInfo dockercfgData
		err := json.Unmarshal(authData, &base64dAuthInfo)
		if err != nil {
			seelog.Warn("Could not parse 'dockercfg' type auth config")
			return dockerAuths{}
		}

		for registry, auth := range base64dAuthInfo {
			data, err := base64.StdEncoding.DecodeString(auth.Auth)
			if err != nil {
				seelog.Warnf("Malformed auth data for registry %v", registry)
				continue
			}

			usernamePass := strings.SplitN(string(data), ":", 2)
			if len(usernamePass) != 2 {
				seelog.Warnf("Malformed auth data for registry %v; must contain ':'", registry)
				continue
			}
			intermediateAuthData[registry] = docker.AuthConfiguration{
				Username: usernamePass[0],
				Password: usernamePass[1],
			}
		}
	case "":
		// not set; no warn
		return dockerAuths{}
	default:
		seelog.Warnf("Unknown auth configuration: %v", authType)
		return dockerAuths{}
	}

	// Normalize intermediate registry keys into not having a schema
	output := make(dockerAuths)
	for key, val := range intermediateAuthData {
		output[stripRegistrySchema(key)] = val
	}
	return output
}

// stripRegistrySchema normalizes the registy url by removing http/https from it.
// because registries only support those two schemas, this function can be quite naive.
func stripRegistrySchema(registry string) string {
	if strings.HasPrefix(registry, "http://") {
		return strings.TrimPrefix(registry, "http://")
	} else if strings.HasPrefix(registry, "https://") {
		return strings.TrimPrefix(registry, "https://")
	}
	return registry
}

// https://github.com/docker/docker/blob/729c9a97822ebee2c978a322d37060454af6bc66/cliconfig/config.go#L25
// `docker login` still uses this, including the /v1/ for me, as of the 1.9.0 RCs
const dockerRegistryKey = "index.docker.io/v1/"

var dockerRegistryHosts = []string{"docker.io", "index.docker.io", "registry-1.docker.io"}

func isDockerhubHostname(hostname string) bool {
	for _, dockerHostname := range dockerRegistryHosts {
		if hostname == dockerHostname {
			return true
		}
	}
	return false
}

// This is taken from Docker's codebase in whole or in part, Copyright Docker Inc.
// https://github.com/docker/docker/blob/v1.8.3/registry/config.go#L290
const IndexName = "docker.io"

func splitReposName(reposName string) (string, string) {
	nameParts := strings.SplitN(reposName, "/", 2)
	var indexName, remoteName string
	if len(nameParts) == 1 || (!strings.Contains(nameParts[0], ".") &&
		!strings.Contains(nameParts[0], ":") && nameParts[0] != "localhost") {
		// This is a Docker Index repos (ex: samalba/hipache or ubuntu)
		// 'docker.io'
		indexName = IndexName
		remoteName = reposName
	} else {
		indexName = nameParts[0]
		remoteName = nameParts[1]
	}
	return indexName, remoteName
}
