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

package stats

import (
	"errors"
	"time"

	ecsengine "github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/stats/resolver"
	"github.com/cihub/seelog"
	"golang.org/x/net/context"
)

const (
	// SleepBetweenUsageDataCollection is the sleep duration between collecting usage data for a container.
	SleepBetweenUsageDataCollection = 1 * time.Second

	// ContainerStatsBufferLength is the number of usage metrics stored in memory for a container. It is calculated as
	// Number of usage metrics gathered in a second (1) * 60 * Time duration in minutes to store the data for (2)
	ContainerStatsBufferLength = 120
)

func newStatsContainer(dockerID string, client ecsengine.DockerClient, resolver resolver.ContainerMetadataResolver) *StatsContainer {
	ctx, cancel := context.WithCancel(context.Background())
	return &StatsContainer{
		containerMetadata: &ContainerMetadata{
			DockerID: dockerID,
		},
		ctx:      ctx,
		cancel:   cancel,
		client:   client,
		resolver: resolver,
	}
}

func (container *StatsContainer) StartStatsCollection() {
	// Create the queue to store utilization data from docker stats
	container.statsQueue = NewQueue(ContainerStatsBufferLength)
	container.statsQueue.Reset()
	go container.collect()
}

func (container *StatsContainer) StopStatsCollection() {
	container.cancel()
}

func (container *StatsContainer) collect() {
	dockerID := container.containerMetadata.DockerID
	for {
		select {
		case <-container.ctx.Done():
			seelog.Debugf("Stopping stats collection for container %s", dockerID)
			return
		default:
			err := container.processStatsStream()
			if err != nil {
				// Currenlty, the only error that we get here is if go-dockerclient is unable
				// to decode the stats payload properly. Other errors such as
				// 'NoSuchContainer', 'InactivityTimeoutExceeded' etc are silently consumed.
				// We rely on state reconciliation with docker task engine at this point of
				// time to stop collecting metrics.
				seelog.Debugf("Error querying stats for container %s: %v", dockerID, err)
			}
			// We were disconnected from the stats stream.
			// Check if the container is terminal. If it is, stop collecting metrics.
			// We might sometimes miss events from docker task  engine and this helps
			// in reconciling the state.
			terminal, err := container.terminal()
			if err != nil {
				// Error determining if the container is terminal. This means that the container
				// id could not be resolved to a container that is being tracked by the
				// docker task engine. If the docker task engine has already removed
				// the container from its state, there's no point in stats engine tracking the
				// container. So, clean-up anyway.
				seelog.Warnf("Error determining if the container %s is terminal, stopping stats collection: %v", dockerID, err)
				container.StopStatsCollection()
			} else if terminal {
				seelog.Infof("Container %s is terminal, stopping stats collection", dockerID)
				container.StopStatsCollection()
			}
		}
	}
}

func (container *StatsContainer) processStatsStream() error {
	dockerID := container.containerMetadata.DockerID
	seelog.Debugf("Collecting stats for container %s", dockerID)
	if container.client == nil {
		return errors.New("container processStatsStream: Client is not set.")
	}
	dockerStats, err := container.client.Stats(dockerID, container.ctx)
	if err != nil {
		return err
	}
	for rawStat := range dockerStats {
		stat, err := dockerStatsToContainerStats(rawStat)
		if err == nil {
			container.statsQueue.Add(stat)
		} else {
			seelog.Warnf("Error converting stats for container %s: %v", dockerID, err)
		}
	}
	return nil
}

func (container *StatsContainer) terminal() (bool, error) {
	dockerContainer, err := container.resolver.ResolveContainer(container.containerMetadata.DockerID)
	if err != nil {
		return false, err
	}
	return dockerContainer.Container.KnownTerminal(), nil
}
