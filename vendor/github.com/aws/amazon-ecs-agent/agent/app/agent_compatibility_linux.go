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

package app

import (
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/engine"

	"github.com/cihub/seelog"
	"github.com/pkg/errors"
)

// checkCompatibility (for linux) ensures that the running task engine is capable for running with the TaskCPUMemLimit
// flag enabled. It will disable the feature if it determines that it is incompatible and the feature isn't explicitly
// enabled.
func (agent *ecsAgent) checkCompatibility(engine engine.TaskEngine) error {

	// We don't need to do these checks if CPUMemLimit is disabled
	if !agent.cfg.TaskCPUMemLimit.Enabled() {
		return nil
	}

	tasks, err := engine.ListTasks()
	if err != nil {
		return err
	}

	// Loop over each task to determine if the state is compatible with the running version of agent and its options
	compatible := true
	for _, task := range tasks {
		if !task.MemoryCPULimitsEnabled {
			seelog.Warnf("App: task from state file is not compatible with TaskCPUMemLimit setting, because it is not using ecs' cgroup hierarchy: %s", task.Arn)
			compatible = false
			break
		}
	}

	if compatible {
		return nil
	}

	if agent.cfg.TaskCPUMemLimit == config.ExplicitlyEnabled {
		return errors.New("App: unable to load old tasks because TaskCPUMemLimits setting is incompatible with old state.")
	}

	seelog.Warn("App: disabling TaskCPUMemLimit.")
	agent.cfg.TaskCPUMemLimit = config.ExplicitlyDisabled

	return nil
}
