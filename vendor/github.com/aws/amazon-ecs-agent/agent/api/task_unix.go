// +build !windows

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

package api

import (
	"path/filepath"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/config"
	docker "github.com/fsouza/go-dockerclient"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

const (
	//memorySwappinessDefault is the expected default value for this platform. This is used in task_windows.go
	//and is maintained here for unix default. Also used for testing
	memorySwappinessDefault = 0

	defaultCPUPeriod = 100 * time.Millisecond // 100ms
	// With a 100ms CPU period, we can express 0.01 vCPU to 10 vCPUs
	maxTaskVCPULimit = 10
	// Reference: http://docs.aws.amazon.com/AmazonECS/latest/APIReference/API_ContainerDefinition.html
	minimumCPUShare = 2

	minimumCPUPercent = 0
	bytesPerMegabyte  = 1024 * 1024
)

func (task *Task) adjustForPlatform(cfg *config.Config) {
	task.memoryCPULimitsEnabledLock.Lock()
	defer task.memoryCPULimitsEnabledLock.Unlock()
	task.MemoryCPULimitsEnabled = cfg.TaskCPUMemLimit.Enabled()
}

func getCanonicalPath(path string) string { return path }

// BuildCgroupRoot helps build the task cgroup prefix
// Example: /ecs/task-id
func (task *Task) BuildCgroupRoot() (string, error) {
	taskID, err := task.GetID()
	if err != nil {
		return "", errors.Wrapf(err, "task build cgroup root: unable to get task-id from task ARN: %s", task.Arn)
	}

	return filepath.Join(config.DefaultTaskCgroupPrefix, taskID), nil
}

// BuildLinuxResourceSpec returns a linuxResources object for the task cgroup
func (task *Task) BuildLinuxResourceSpec() (specs.LinuxResources, error) {
	linuxResourceSpec := specs.LinuxResources{}

	// If task level CPU limits are requested, set CPU quota + CPU period
	// Else set CPU shares
	if task.CPU > 0 {
		linuxCPUSpec, err := task.buildExplicitLinuxCPUSpec()
		if err != nil {
			return specs.LinuxResources{}, err
		}
		linuxResourceSpec.CPU = &linuxCPUSpec
	} else {
		linuxCPUSpec := task.buildImplicitLinuxCPUSpec()
		linuxResourceSpec.CPU = &linuxCPUSpec
	}

	// Validate and build task memory spec
	// NOTE: task memory specifications are optional
	if task.Memory > 0 {
		linuxMemorySpec, err := task.buildLinuxMemorySpec()
		if err != nil {
			return specs.LinuxResources{}, err
		}
		linuxResourceSpec.Memory = &linuxMemorySpec
	}

	return linuxResourceSpec, nil
}

// buildExplicitLinuxCPUSpec builds CPU spec when task CPU limits are
// explicitly requested
func (task *Task) buildExplicitLinuxCPUSpec() (specs.LinuxCPU, error) {
	if task.CPU > maxTaskVCPULimit {
		return specs.LinuxCPU{},
			errors.Errorf("task CPU spec builder: unsupported CPU limits, requested=%f, max-supported=%d",
				task.CPU, maxTaskVCPULimit)
	}
	taskCPUPeriod := uint64(defaultCPUPeriod / time.Microsecond)
	taskCPUQuota := int64(task.CPU * float64(taskCPUPeriod))

	// TODO: DefaultCPUPeriod only permits 10VCPUs.
	// Adaptive calculation of CPUPeriod required for further support
	// (samuelkarp) The largest available EC2 instance in terms of CPU count is a x1.32xlarge,
	// with 128 vCPUs. If we assume a fixed evaluation period of 100ms (100000us),
	// we'd need a quota of 12800000us, which is longer than the maximum of 1000000.
	// For 128 vCPUs, we'd probably need something like a 1ms (1000us - the minimum)
	// evaluation period, an 128000us quota in order to stay within the min/max limits.
	return specs.LinuxCPU{
		Quota:  &taskCPUQuota,
		Period: &taskCPUPeriod,
	}, nil
}

// buildImplicitLinuxCPUSpec builds the implicit task CPU spec when
// task CPU and memory limit feature is enabled
func (task *Task) buildImplicitLinuxCPUSpec() specs.LinuxCPU {
	// If task level CPU limits are missing,
	// aggregate container CPU shares when present
	var taskCPUShares uint64
	for _, container := range task.Containers {
		if container.CPU > 0 {
			taskCPUShares += uint64(container.CPU)
		}
	}

	// If there are are no CPU limits at task or container level,
	// default task CPU shares
	if taskCPUShares == 0 {
		// Set default CPU shares
		taskCPUShares = minimumCPUShare
	}

	return specs.LinuxCPU{
		Shares: &taskCPUShares,
	}
}

// buildLinuxMemorySpec validates and builds the task memory spec
func (task *Task) buildLinuxMemorySpec() (specs.LinuxMemory, error) {
	// If task memory limit is not present, cgroup parent memory is not set
	// If task memory limit is set, ensure that no container
	// of this task has a greater request
	for _, container := range task.Containers {
		containerMemoryLimit := int64(container.Memory)
		if containerMemoryLimit > task.Memory {
			return specs.LinuxMemory{},
				errors.Errorf("task memory spec builder: container memory limit(%d) greater than task memory limit(%d)",
					containerMemoryLimit, task.Memory)
		}
	}

	// Kernel expects memory to be expressed in bytes
	memoryBytes := task.Memory * bytesPerMegabyte
	return specs.LinuxMemory{
		Limit: &memoryBytes,
	}, nil
}

// platformHostConfigOverride to override platform specific feature sets
func (task *Task) platformHostConfigOverride(hostConfig *docker.HostConfig) error {
	// Override cgroup parent
	return task.overrideCgroupParent(hostConfig)
}

// overrideCgroupParent updates hostconfig with cgroup parent when task cgroups
// are enabled
func (task *Task) overrideCgroupParent(hostConfig *docker.HostConfig) error {
	task.memoryCPULimitsEnabledLock.RLock()
	defer task.memoryCPULimitsEnabledLock.RUnlock()
	if task.MemoryCPULimitsEnabled {
		cgroupRoot, err := task.BuildCgroupRoot()
		if err != nil {
			return errors.Wrapf(err, "task cgroup override: unable to obtain cgroup root for task: %s", task.Arn)
		}
		hostConfig.CgroupParent = cgroupRoot
	}
	return nil
}
