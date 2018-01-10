// +build functional,!windows

// Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

// Package simpletest is an auto-generated set of tests defined by the json
// descriptions in testdata/simpletests.
//
// This file should not be edited; rather you should edit the generator instead
package simpletest

import (
	"os"
	"testing"
	"time"

	. "github.com/aws/amazon-ecs-agent/agent/functional_tests/util"
)

// TestAddAndDropCapabilities checks that adding and dropping Linux capabilities work
func TestAddAndDropCapabilities(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.0.0")

	td, err := GetTaskDefinition("add-drop-capabilities")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestDataVolume Check that basic data volumes work
func TestDataVolume(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.0.0")

	td, err := GetTaskDefinition("datavolume")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestDataVolume2 Verify that more complex datavolumes (including empty and volumes-from) work as expected; see Related
func TestDataVolume2(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">1.0.0")

	td, err := GetTaskDefinition("datavolume2")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestDevices checks that adding devices works
func TestDevices(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.0.0")

	td, err := GetTaskDefinition("devices")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestDisableNetworking Check that disable networking works
func TestDisableNetworking(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.5.0")

	td, err := GetTaskDefinition("network-disabled")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestDnsSearchDomains Check that dns search domains works
func TestDnsSearchDomains(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.5.0")

	td, err := GetTaskDefinition("dns-search-domains")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestDnsServers Check that dns servers works
func TestDnsServers(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.5.0")

	td, err := GetTaskDefinition("dns-servers")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestExtraHosts Check that extra hosts works
func TestExtraHosts(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.5.0")

	td, err := GetTaskDefinition("extra-hosts")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestHostname Check that hostname works
func TestHostname(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.5.0")

	td, err := GetTaskDefinition("hostname")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestInitProcessEnabled checks that enabling init process works
func TestInitProcessEnabled(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.14.5")

	td, err := GetTaskDefinition("init-process")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestLinkVolumeDependencies Tests that the dependency graph of task definitions is resolved correctly
func TestLinkVolumeDependencies(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.0.0")

	td, err := GetTaskDefinition("network-link-2")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestNetworkLink Tests that basic network linking works
func TestNetworkLink(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.0.0")

	td, err := GetTaskDefinition("network-link")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestParallelPull check docker pull in parallel works for docker >= 1.11.1
func TestParallelPull(t *testing.T) {

	// Test only available for docker version >=1.11.1
	RequireDockerVersion(t, ">=1.11.1")

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.0.0")

	td, err := GetTaskDefinition("parallel-pull")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 4)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("1m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	// Make sure the task is running
	for _, testTask := range testTasks {
		err = testTask.WaitRunning(timeout)
		if err != nil {
			t.Errorf("Timed out waiting for task to reach running. Error %v, task %v", err, testTask)
		}
	}

	// Cleanup, stop all the tasks and wait for the containers to be stopped
	for _, testTask := range testTasks {
		err = testTask.Stop()
		if err != nil {
			t.Errorf("Failed to stop task, Error %v, task %v", err, testTask)
		}
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestPrivileged Check that privileged works
func TestPrivileged(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.5.0")

	td, err := GetTaskDefinition("privileged")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestReadonlyRootfs Check that readonly rootfs works
func TestReadonlyRootfs(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.5.0")

	td, err := GetTaskDefinition("readonly-rootfs")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestSecurityOptNoNewPrivileges Check that security-opt=no-new-privileges works
func TestSecurityOptNoNewPrivileges(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.12.1")

	td, err := GetTaskDefinition("security-opt-nonewprivileges")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestSimpleExit Tests that the basic premis of this testing fromwork works (e.g. exit codes go through, etc)
func TestSimpleExit(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.0.0")

	td, err := GetTaskDefinition("simple-exit")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestNofilesULimit Check that nofiles ulimit works
func TestNofilesULimit(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.5.0")

	td, err := GetTaskDefinition("nofiles-ulimit")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestUserNobody Check that user works
func TestUserNobody(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.5.0")

	td, err := GetTaskDefinition("user-nobody")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}

// TestWorkingDir Check that working dir works
func TestWorkingDir(t *testing.T) {

	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" {
		t.Parallel()
	}
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.5.0")

	td, err := GetTaskDefinition("working-dir")
	if err != nil {
		t.Fatalf("Could not register task definition: %v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		t.Fatalf("Could not start task: %v", err)
	}
	timeout, err := time.ParseDuration("2m")
	if err != nil {
		t.Fatalf("Could not parse timeout: %#v", err)
	}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %#v, task %#v", err, testTask)
		}

		if exit, ok := testTask.ContainerExitcode("exit"); !ok || exit != 42 {
			t.Errorf("Expected exit to exit with 42; actually exited (%v) with %v", ok, exit)
		}

		defer agent.SweepTask(testTask)
	}

}
