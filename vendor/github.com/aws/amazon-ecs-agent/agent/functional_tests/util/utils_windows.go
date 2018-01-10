// +build windows, functional

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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/ec2"
	"github.com/aws/amazon-ecs-agent/agent/ecs_client/model/ecs"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	docker "github.com/fsouza/go-dockerclient"
	"golang.org/x/sys/windows/registry"
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
func RunAgent(t *testing.T, options *AgentOptions) *TestAgent {
	agent := &TestAgent{t: t}

	dockerClient, err := docker.NewClientFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	agent.DockerClient = dockerClient

	tmpdirOverride := os.Getenv("ECS_FTEST_TMP")

	agentTempdir, err := ioutil.TempDir(tmpdirOverride, "ecs_integ_testdata")
	if err != nil {
		t.Fatal("Could not create temp dir for test")
	}
	logdir := filepath.Join(agentTempdir, "log")
	datadir := filepath.Join(agentTempdir, "data")
	os.Setenv("ECS_LOGFILE", logdir+"/log.log")
	os.Setenv("ECS_DATADIR", datadir)
	valueName := fmt.Sprintf("test_path_%d", time.Now().UnixNano())
	t.Log("Registry location", valueName)
	os.Setenv("ZZZ_I_KNOW_SETTING_TEST_VALUES_IN_PRODUCTION_IS_NOT_SUPPORTED", valueName)
	os.Setenv("ECS_CLUSTER", Cluster)
	os.Setenv("ECS_ENABLE_TASK_IAM_ROLE", "true")
	os.Setenv("DOCKER_HOST", "npipe:////./pipe/docker_engine")
	os.Setenv("ECS_DISABLE_METRICS", "false")
	os.Setenv("ECS_AUDIT_LOGFILE", logdir+"/audit.log")
	os.Setenv("ECS_LOGLEVEL", "debug")
	os.Setenv("ECS_AVAILABLE_LOGGING_DRIVERS", `["json-file","awslogs"]`)

	t.Log("datadir", datadir)
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
	return agent.Process.Kill()
}

func (agent *TestAgent) StartAgent() error {
	if agent.Options != nil {
		for k, v := range agent.Options.ExtraEnvironment {
			os.Setenv(k, v)
		}
	}
	agentInvoke := exec.Command(".\\agent.exe")
	if TestDirectory := os.Getenv("ECS_WINDOWS_TEST_DIR"); TestDirectory != "" {
		agentInvoke.Dir = TestDirectory
	}
	err := agentInvoke.Start()
	if err != nil {
		agent.t.Logf("Agent start invocation failed with %s", err.Error())
		return err
	}
	agent.Process = agentInvoke.Process
	agent.IntrospectionURL = "http://localhost:51678"
	return agent.verifyIntrospectionAPI()
}

func (agent *TestAgent) Cleanup() {
	if agent.Options != nil {
		for k, _ := range agent.Options.ExtraEnvironment {
			os.Unsetenv(k)
		}
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Amazon\ECS Agent\State File`, registry.ALL_ACCESS)
	if err != nil {
		return
	}
	key.DeleteValue(os.Getenv("ZZZ_I_KNOW_SETTING_TEST_VALUES_IN_PRODUCTION_IS_NOT_SUPPORTED"))
	os.Unsetenv("ZZZ_I_KNOW_SETTING_TEST_VALUES_IN_PRODUCTION_IS_NOT_SUPPORTED")
	agent.platformIndependentCleanup()
}
