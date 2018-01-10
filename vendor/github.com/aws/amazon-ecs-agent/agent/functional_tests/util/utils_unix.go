// +build !windows,functional

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

package util

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/ec2"
	"github.com/aws/amazon-ecs-agent/agent/ecs_client/model/ecs"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	docker "github.com/fsouza/go-dockerclient"
)

const (
	defaultExecDriverPath = "/var/run/docker/execdriver"
	logdir                = "/log"
	datadir               = "/data"
	ExecDriverDir         = "/var/lib/docker/execdriver"
	defaultCgroupPath     = "/cgroup"
	cacheDirectory        = "/var/cache/ecs"
	configDirectory       = "/etc/ecs"
	readOnly              = ":ro"
	dockerEndpoint        = "/var/run/docker.sock"
)

var ECS *ecs.ECS
var Cluster string

func init() {
	var ecsconfig aws.Config
	if region := os.Getenv("AWS_REGION"); region != "" {
		ecsconfig.Region = &region
	}
	if region := os.Getenv("AWS_DEFAULT_REGION"); region != "" {
		ecsconfig.Region = &region
	}
	if ecsconfig.Region == nil {
		if iid, err := ec2.NewEC2MetadataClient(nil).InstanceIdentityDocument(); err == nil {
			ecsconfig.Region = &iid.Region
		}
	}
	if envEndpoint := os.Getenv("ECS_BACKEND_HOST"); envEndpoint != "" {
		ecsconfig.Endpoint = &envEndpoint
	}

	ECS = ecs.New(session.New(&ecsconfig))
	Cluster = "ecs-functional-tests"
	if envCluster := os.Getenv("ECS_CLUSTER"); envCluster != "" {
		Cluster = envCluster
	}
	ECS.CreateCluster(&ecs.CreateClusterInput{
		ClusterName: aws.String(Cluster),
	})
}

// RunAgent launches the agent and returns an object which may be used to reference it.
// It will wait until the agent is correctly registered before returning.
// 'version' may be a docker image (e.g. amazon/amazon-ecs-agent:v1.0.0) with
// tag that may be used to run the agent. It defaults to
// 'amazon/amazon-ecs-agent:make', the version created locally by running
// 'make'
func RunAgent(t *testing.T, options *AgentOptions) *TestAgent {
	agent := &TestAgent{t: t}
	agentImage := "amazon/amazon-ecs-agent:make"
	if envImage := os.Getenv("ECS_AGENT_IMAGE"); envImage != "" {
		agentImage = envImage
	}
	agent.Image = agentImage

	dockerClient, err := docker.NewClientFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	agent.DockerClient = dockerClient

	_, err = dockerClient.InspectImage(agentImage)
	if err != nil {
		err = dockerClient.PullImage(docker.PullImageOptions{Repository: agentImage}, docker.AuthConfiguration{})
		if err != nil {
			t.Fatal("Could not launch agent", err)
		}
	}

	tmpdirOverride := os.Getenv("ECS_FTEST_TMP")

	agentTempdir, err := ioutil.TempDir(tmpdirOverride, "ecs_integ_testdata")
	if err != nil {
		t.Fatal("Could not create temp dir for test")
	}
	logdir := filepath.Join(agentTempdir, "log")
	datadir := filepath.Join(agentTempdir, "data")
	os.Mkdir(logdir, 0755)
	os.Mkdir(datadir, 0755)
	agent.TestDir = agentTempdir
	agent.Options = options
	if options == nil {
		agent.Options = &AgentOptions{}
	}
	t.Logf("Created directory %s to store test data in", agentTempdir)

	err = agent.StartAgent()
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func (agent *TestAgent) StopAgent() error {
	return agent.DockerClient.StopContainer(agent.DockerID, 10)
}

func (agent *TestAgent) StartAgent() error {
	agent.t.Logf("Launching agent with image: %s\n", agent.Image)
	dockerConfig := &docker.Config{
		Image: agent.Image,
		ExposedPorts: map[docker.Port]struct{}{
			"51678/tcp": {},
		},
		Env: []string{
			"ECS_CLUSTER=" + Cluster,
			"ECS_DATADIR=/data",
			"ECS_HOST_DATA_DIR=" + agent.TestDir,
			"ECS_LOGLEVEL=debug",
			"ECS_LOGFILE=/log/integ_agent.log",
			"ECS_BACKEND_HOST=" + os.Getenv("ECS_BACKEND_HOST"),
			"AWS_ACCESS_KEY_ID=" + os.Getenv("AWS_ACCESS_KEY_ID"),
			"AWS_DEFAULT_REGION=" + *ECS.Config.Region,
			"AWS_SECRET_ACCESS_KEY=" + os.Getenv("AWS_SECRET_ACCESS_KEY"),
			"ECS_ENGINE_TASK_CLEANUP_WAIT_DURATION=" + os.Getenv("ECS_ENGINE_TASK_CLEANUP_WAIT_DURATION"),
		},
		Cmd: strings.Split(os.Getenv("ECS_FTEST_AGENT_ARGS"), " "),
	}

	// Append ECS_INSTANCE_ATTRIBUTES to dockerConfig
	if attr := os.Getenv("ECS_INSTANCE_ATTRIBUTES"); attr != "" {
		dockerConfig.Env = append(dockerConfig.Env, "ECS_INSTANCE_ATTRIBUTES="+attr)
	}

	binds := agent.getBindMounts()

	hostConfig := &docker.HostConfig{
		Binds: binds,
		PortBindings: map[docker.Port][]docker.PortBinding{
			"51678/tcp": {{HostIP: "0.0.0.0"}},
		},
		Links: agent.Options.ContainerLinks,
	}

	if os.Getenv("ECS_FTEST_FORCE_NET_HOST") != "" {
		hostConfig.NetworkMode = "host"
	}

	if agent.Options != nil {
		// Override the default docker environment variable
		for key, value := range agent.Options.ExtraEnvironment {
			envVarExists := false
			for i, str := range dockerConfig.Env {
				if strings.HasPrefix(str, key+"=") {
					dockerConfig.Env[i] = key + "=" + value
					envVarExists = true
					break
				}
			}
			if !envVarExists {
				dockerConfig.Env = append(dockerConfig.Env, key+"="+value)
			}
		}

		for key, value := range agent.Options.PortBindings {
			hostConfig.PortBindings[key] = []docker.PortBinding{{HostIP: value["HostIP"], HostPort: value["HostPort"]}}
			dockerConfig.ExposedPorts[key] = struct{}{}
		}
	}

	agentContainer, err := agent.DockerClient.CreateContainer(docker.CreateContainerOptions{
		Config:     dockerConfig,
		HostConfig: hostConfig,
	})
	if err != nil {
		agent.t.Fatal("Could not create agent container", err)
	}
	agent.DockerID = agentContainer.ID
	agent.t.Logf("Agent started as docker container: %s\n", agentContainer.ID)

	err = agent.DockerClient.StartContainer(agentContainer.ID, nil)
	if err != nil {
		return errors.New("Could not start agent container " + err.Error())
	}

	containerMetadata, err := agent.DockerClient.InspectContainer(agentContainer.ID)
	if err != nil {
		return errors.New("Could not inspect agent container: " + err.Error())
	}
	if containerMetadata.HostConfig.NetworkMode == "host" {
		agent.IntrospectionURL = "http://localhost:51678"
	} else {
		agent.IntrospectionURL = "http://localhost:" + containerMetadata.NetworkSettings.Ports["51678/tcp"][0].HostPort
	}

	return agent.verifyIntrospectionAPI()
}

// getBindMounts actually constructs volume binds for container's host config
// It also additionally checks for environment variables:
// * CGROUP_PATH: the cgroup path
// * EXECDRIVER_PATH: the path of metrics
func (agent *TestAgent) getBindMounts() []string {
	var binds []string
	cgroupPath := utils.DefaultIfBlank(os.Getenv("CGROUP_PATH"), defaultCgroupPath)
	cgroupBind := cgroupPath + ":" + cgroupPath + readOnly
	binds = append(binds, cgroupBind)

	execdriverPath := utils.DefaultIfBlank(os.Getenv("EXECDRIVER_PATH"), defaultExecDriverPath)
	execdriverBind := execdriverPath + ":" + ExecDriverDir + readOnly
	binds = append(binds, execdriverBind)

	hostLogDir := filepath.Join(agent.TestDir, "log")
	hostDataDir := filepath.Join(agent.TestDir, "data")
	hostConfigDir := filepath.Join(agent.TestDir, "config")
	hostCacheDir := filepath.Join(agent.TestDir, "cache")
	agent.Logdir = hostLogDir

	binds = append(binds, hostLogDir+":"+logdir)
	binds = append(binds, hostDataDir+":"+datadir)
	binds = append(binds, dockerEndpoint+":"+dockerEndpoint)
	binds = append(binds, hostConfigDir+":"+configDirectory)
	binds = append(binds, hostCacheDir+":"+cacheDirectory)

	return binds
}

func (agent *TestAgent) Cleanup() {
	agent.platformIndependentCleanup()
}
