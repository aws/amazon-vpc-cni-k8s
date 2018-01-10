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
	"fmt"
	"net/http"

	"github.com/aws/amazon-ecs-agent/agent/ec2"
	"github.com/aws/amazon-ecs-agent/agent/ecscni"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/eni/pause"
	"github.com/aws/amazon-ecs-agent/agent/eni/udevwrapper"
	"github.com/aws/amazon-ecs-agent/agent/eni/watcher"
	"github.com/aws/amazon-ecs-agent/agent/statechange"
	"github.com/cihub/seelog"
	"github.com/pkg/errors"
)

// initPID defines the process identifier for the init process
const initPID = 1

// awsVPCCNIPlugins is a list of CNI plugins required by the ECS Agent
// to configure the ENI for a task
var awsVPCCNIPlugins = []string{ecscni.ECSENIPluginName,
	ecscni.ECSBridgePluginName,
	ecscni.ECSIPAMPluginName,
}

// startWindowsService is not supported on Linux
func (agent *ecsAgent) startWindowsService() int {
	seelog.Error("Windows Services are not supported on Linux")
	return 1
}

// initializeTaskENIDependencies initializes all of the dependencies required by
// the Agent to support the 'awsvpc' networking mode. A non nil error is returned
// if an error is encountered during this process. An additional boolean flag to
// indicate if this error is considered terminal is also returned
func (agent *ecsAgent) initializeTaskENIDependencies(state dockerstate.TaskEngineState, taskEngine engine.TaskEngine) (error, bool) {
	// Check if the Agent process's pid  == 1, which means it's running without an init system
	if agent.os.Getpid() == initPID {
		// This is a terminal error. Bad things happen with invoking the
		// the ENI plugin when there's no init process in the pid namesapce.
		// Specifically, the DHClient processes that are started as children
		// of the Agent will not be reaped leading to the ENI device
		// disappearing until the Agent is killed.
		return errors.New("agent is not started with an init system"), true
	}

	// Set VPC and Subnet IDs for the instance
	if err, ok := agent.setVPCSubnet(); err != nil {
		return err, ok
	}

	// Validate that the CNI plugins exist in the expected path and that
	// they possess the right capabilities
	if err := agent.verifyCNIPluginsCapabilities(); err != nil {
		// An error here is terminal as it means that the plugins
		// do not support the ENI capability
		return err, true
	}

	if agent.cfg.ShouldLoadPauseContainerTarball() {
		// Load the pause container's image from the 'disk'
		if _, err := agent.pauseLoader.LoadImage(agent.cfg, agent.dockerClient); err != nil {
			if pause.IsNoSuchFileError(err) || pause.UnsupportedPlatform(err) {
				// If the pause container's image tarball doesn't exist or if the
				// invocation is done for an unsupported platform, we cannot recover.
				// Return the error as terminal for these cases
				return err, true
			}
			return err, false
		}
	}

	if err := agent.startUdevWatcher(state, taskEngine.StateChangeEvents()); err != nil {
		// If udev watcher was not initialized in this run because of the udev socket
		// file not being available etc, the Agent might be able to retry and succeed
		// on the next run. Hence, returning a false here for terminal bool
		return err, false
	}

	return nil, false
}

// setVPCSubnet sets the vpc and subnet ids for the agent by querying the
// instance metadata service
func (agent *ecsAgent) setVPCSubnet() (error, bool) {
	mac, err := agent.ec2MetadataClient.PrimaryENIMAC()
	if err != nil {
		return fmt.Errorf("unable to get mac address of instance's primary ENI from instance metadata: %v", err), false
	}

	vpcID, err := agent.ec2MetadataClient.VPCID(mac)
	if err != nil {
		if isInstanceLaunchedInVPC(err) {
			return fmt.Errorf("unable to get vpc id from instance metadata: %v", err), true
		}
		return instanceNotLaunchedInVPCError, false
	}

	subnetID, err := agent.ec2MetadataClient.SubnetID(mac)
	if err != nil {
		return fmt.Errorf("unable to get subnet id from instance metadata: %v", err), false
	}
	agent.vpc = vpcID
	agent.subnet = subnetID
	agent.mac = mac
	return nil, false
}

// isInstanceLaunchedInVPC returns false when the http status code is set to
// 'not found' (404) when querying the vpc id from instance metadata
func isInstanceLaunchedInVPC(err error) bool {
	if metadataErr, ok := err.(*ec2.MetadataError); ok &&
		metadataErr.GetStatusCode() == http.StatusNotFound {
		return false
	}

	return true
}

// verifyCNIPluginsCapabilities returns an error if there's an error querying
// capabilities or if the required capability is absent from the capabilities
// of the following plugins:
// a. ecs-eni
// b. ecs-bridge
// c. ecs-ipam
func (agent *ecsAgent) verifyCNIPluginsCapabilities() error {
	// Check if we can get capabilities from each plugin
	for _, plugin := range awsVPCCNIPlugins {
		capabilities, err := agent.cniClient.Capabilities(plugin)
		if err != nil {
			return err
		}
		if !contains(capabilities, ecscni.CapabilityAWSVPCNetworkingMode) {
			return errors.Errorf("plugin '%s' doesn't support the capability: %s",
				plugin, ecscni.CapabilityAWSVPCNetworkingMode)
		}
	}

	return nil
}

// startUdevWatcher starts the udev monitor and the watcher for receiving
// notifications from the monitor
func (agent *ecsAgent) startUdevWatcher(state dockerstate.TaskEngineState, stateChangeEvents chan<- statechange.Event) error {
	seelog.Debug("Setting up ENI Watcher")
	udevMonitor, err := udevwrapper.New()
	if err != nil {
		return errors.Wrapf(err, "unable to create udev monitor")
	}
	// Create Watcher
	eniWatcher := watcher.New(agent.ctx, agent.mac, udevMonitor, state, stateChangeEvents)
	if err := eniWatcher.Init(); err != nil {
		return errors.Wrapf(err, "unable to initialize eni watcher")
	}
	go eniWatcher.Start()
	return nil
}

func contains(capabilities []string, capability string) bool {
	for _, cap := range capabilities {
		if cap == capability {
			return true
		}
	}

	return false
}
