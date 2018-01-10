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

package dependencygraph

import (
	"strings"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/credentials"
	log "github.com/cihub/seelog"
	"github.com/pkg/errors"
)

var (
	// UnableTransitionExecutionCredentialsNotResolved is the error where a container needs to wait for
	// credentials before it can process by agent
	UnableTransitionExecutionCredentialsNotResolved = errors.New("dependency graph: container execution credentials not available")
	// UnableTransitionTransitionDependencyNotResolved is the error where a dependent container isn't in expected state
	UnableTransitionTransitionDependencyNotResolved = errors.New("dependency graph: dependent container not in expected state")
	// UnableTransitionContainerPassedDesiredStatus is the error where the container status is bigger than desired status
	UnableTransitionContainerPassedDesiredStatus = errors.New("container transition: container status is equal or greater than desired status")
	unableTransitionVolumesDependencyNotResolved = errors.New("dependency graph: container volume dependency not resolved")
	unableTranstionLinksDependencyNotResolved    = errors.New("dependency graph: container links dependency not resolved")
)

// Because a container may depend on another container being created
// (volumes-from) or running (links) it makes sense to abstract it out
// to each container having dependencies on another container being in any
// particular state set. For now, these are resolved here and support only
// volume/link (created/run)

// ValidDependencies takes a task and verifies that it is possible to allow all
// containers within it to reach the desired status by proceeding in some
// order.  ValidDependencies is called during DockerTaskEngine.AddTask to
// verify that a startup order can exist.
func ValidDependencies(task *api.Task) bool {
	unresolved := make([]*api.Container, len(task.Containers))
	resolved := make([]*api.Container, 0, len(task.Containers))

	copy(unresolved, task.Containers)

OuterLoop:
	for len(unresolved) > 0 {
		for i, tryResolve := range unresolved {
			if dependenciesCanBeResolved(tryResolve, resolved) {
				resolved = append(resolved, tryResolve)
				unresolved = append(unresolved[:i], unresolved[i+1:]...)
				// Break out of the inner loop now that we modified the slice
				// we're looping over
				continue OuterLoop
			}
		}
		log.Warnf("Could not resolve some containers: [%v] for task %v", unresolved, task)
		return false
	}

	return true
}

// DependenciesCanBeResolved verifies that it's possible to transition a `target`
// given a group of already handled containers, `by`. Essentially, it asks "is
// `target` resolved by `by`". It assumes that everything in `by` has reached
// DesiredStatus and that `target` is also trying to get there
//
// This function is used for verifying that a state should be resolvable, not
// for actually deciding what to do. `DependenciesAreResolved` should be used for
// that purpose instead.
func dependenciesCanBeResolved(target *api.Container, by []*api.Container) bool {
	nameMap := make(map[string]*api.Container)
	for _, cont := range by {
		nameMap[cont.Name] = cont
	}
	neededVolumeContainers := make([]string, len(target.VolumesFrom))
	for i, volume := range target.VolumesFrom {
		neededVolumeContainers[i] = volume.SourceContainer
	}

	return verifyStatusResolvable(target, nameMap, neededVolumeContainers, volumeCanResolve) &&
		verifyStatusResolvable(target, nameMap, linksToContainerNames(target.Links), linkCanResolve) &&
		verifyStatusResolvable(target, nameMap, target.SteadyStateDependencies, onSteadyStateCanResolve)
}

// DependenciesAreResolved validates that the `target` container can be
// transitioned given the current known state of the containers in `by`. If
// this function returns true, `target` should be technically able to launch
// without issues.
// Transitions are between known statuses (whether the container can move to
// the next known status), not desired statuses; the desired status typically
// is either RUNNING or STOPPED.
func DependenciesAreResolved(target *api.Container,
	by []*api.Container,
	id string,
	manager credentials.Manager) error {
	if !executionCredentialsResolved(target, id, manager) {
		return UnableTransitionExecutionCredentialsNotResolved
	}

	nameMap := make(map[string]*api.Container)
	for _, cont := range by {
		nameMap[cont.Name] = cont
	}
	neededVolumeContainers := make([]string, len(target.VolumesFrom))
	for i, volume := range target.VolumesFrom {
		neededVolumeContainers[i] = volume.SourceContainer
	}

	if !verifyStatusResolvable(target, nameMap, neededVolumeContainers, volumeIsResolved) {
		return unableTransitionVolumesDependencyNotResolved
	}

	if !verifyStatusResolvable(target, nameMap, linksToContainerNames(target.Links), linkIsResolved) {
		return unableTranstionLinksDependencyNotResolved
	}

	if !verifyStatusResolvable(target, nameMap, target.SteadyStateDependencies, onSteadyStateIsResolved) ||
		!verifyTransitionDependenciesResolved(target, nameMap) {
		return UnableTransitionTransitionDependencyNotResolved
	}

	return nil
}

func linksToContainerNames(links []string) []string {
	names := make([]string, 0, len(links))
	for _, link := range links {
		name := strings.Split(link, ":")[0]
		names = append(names, name)
	}
	return names
}

func executionCredentialsResolved(target *api.Container, id string, manager credentials.Manager) bool {
	if target.GetKnownStatus() >= api.ContainerPulled ||
		!target.ShouldPullWithExecutionRole() ||
		target.GetDesiredStatus() >= api.ContainerStopped {
		return true
	}

	_, ok := manager.GetTaskCredentials(id)
	return ok
}

// verifyStatusResolvable validates that `target` can be resolved given that
// target depends on `dependencies` (which are container names) and there are
// `existingContainers` (map from name to container). The `resolves` function
// passed should return true if the named container is resolved.
func verifyStatusResolvable(target *api.Container, existingContainers map[string]*api.Container, dependencies []string, resolves func(*api.Container, *api.Container) bool) bool {
	targetGoal := target.GetDesiredStatus()
	if targetGoal != target.GetSteadyStateStatus() && targetGoal != api.ContainerCreated {
		// A container can always stop, die, or reach whatever other state it
		// wants regardless of what dependencies it has
		return true
	}

	for _, dependency := range dependencies {
		maybeResolves, exists := existingContainers[dependency]
		if !exists {
			return false
		}
		if !resolves(target, maybeResolves) {
			return false
		}
	}
	return true
}

func verifyTransitionDependenciesResolved(target *api.Container, existingContainers map[string]*api.Container) bool {
	targetGoal := target.GetDesiredStatus()
	if targetGoal >= api.ContainerStopped {
		// A container can always stop, die, or reach whatever other state it
		// wants regardless of what dependencies it has
		return true
	}

	for _, containerDependency := range target.TransitionDependencySet.ContainerDependencies {
		maybeResolves, exists := existingContainers[containerDependency.ContainerName]
		if !exists {
			return false
		}
		if !resolvesContainerTransitionDependency(target, maybeResolves, containerDependency) {
			return false
		}
	}
	return true
}

func resolvesContainerTransitionDependency(target *api.Container, resource *api.Container, dependency api.ContainerDependency) bool {
	targetDesired := target.GetDesiredStatus()
	if targetDesired < dependency.DependentStatus {
		// not trying to reach dependent status
		return true
	}
	targetKnown := target.GetKnownStatus()
	if targetKnown >= dependency.DependentStatus {
		// already satisfied
		return true
	}
	targetNext := targetKnown + 1
	if targetNext < dependency.DependentStatus {
		// next status is not the dependent status, so proceed
		return true
	}
	resourceKnown := resource.GetKnownStatus()
	return resourceKnown >= dependency.SatisfiedStatus
}

func linkCanResolve(target *api.Container, link *api.Container) bool {
	targetDesiredStatus := target.GetDesiredStatus()
	linkDesiredStatus := link.GetDesiredStatus()
	if targetDesiredStatus == api.ContainerCreated {
		// The 'target' container desires to be moved to 'Created' state.
		// Allow this only if the desired status of the linked container is
		// 'Created' or if the linked container is in 'steady state'
		return linkDesiredStatus == api.ContainerCreated || linkDesiredStatus == link.GetSteadyStateStatus()
	} else if targetDesiredStatus == target.GetSteadyStateStatus() {
		// The 'target' container desires to be moved to its 'steady' state.
		// Allow this only if the linked container is in 'steady state' as well
		return linkDesiredStatus == link.GetSteadyStateStatus()
	}
	log.Errorf("Failed to resolve the desired status of the link [%v] for the target [%v]", link, target)
	return false
}

func linkIsResolved(target *api.Container, link *api.Container) bool {
	targetDesiredStatus := target.GetDesiredStatus()
	if targetDesiredStatus == api.ContainerCreated {
		// The 'target' container desires to be moved to 'Created' state.
		// Allow this only if the known status of the linked container is
		// 'Created' or if the linked container is in 'steady state'
		linkKnownStatus := link.GetKnownStatus()
		return linkKnownStatus == api.ContainerCreated || link.IsKnownSteadyState()
	} else if targetDesiredStatus == target.GetSteadyStateStatus() {
		// The 'target' container desires to be moved to its 'steady' state.
		// Allow this only if the linked container is in 'steady state' as well
		return link.IsKnownSteadyState()
	}
	log.Errorf("Failed to resolve if the link [%v] has been resolved for the target [%v]", link, target)
	return false
}

func volumeCanResolve(target *api.Container, volume *api.Container) bool {
	targetDesiredStatus := target.GetDesiredStatus()
	if targetDesiredStatus != api.ContainerCreated && targetDesiredStatus != target.GetSteadyStateStatus() {
		// The 'target' container doesn't desire to move to either 'Created' or the 'steady' state,
		// which is not allowed
		log.Errorf("Failed to resolve the desired status of the volume [%v] for the target [%v]", volume, target)
		return false
	}

	// The 'target' container desires to be moved to 'Created' or the 'steady' state.
	// Allow this only if the known status of the source volume container is
	// any of 'Created', 'steady state' or 'Stopped'
	volumeDesiredStatus := volume.GetDesiredStatus()
	return volumeDesiredStatus == api.ContainerCreated ||
		volumeDesiredStatus == volume.GetSteadyStateStatus() ||
		volumeDesiredStatus == api.ContainerStopped
}

func volumeIsResolved(target *api.Container, volume *api.Container) bool {
	targetDesiredStatus := target.GetDesiredStatus()
	if targetDesiredStatus != api.ContainerCreated && targetDesiredStatus != api.ContainerRunning {
		// The 'target' container doesn't desire to be moved to 'Created' or the 'steady' state.
		// Do not allow it.
		log.Errorf("Failed to resolve if the volume [%v] has been resolved for the target [%v]", volume, target)
		return false
	}

	// The 'target' container desires to be moved to 'Created' or the 'steady' state.
	// Allow this only if the known status of the source volume container is
	// any of 'Created', 'steady state' or 'Stopped'
	knownStatus := volume.GetKnownStatus()
	return knownStatus == api.ContainerCreated ||
		knownStatus == volume.GetSteadyStateStatus() ||
		knownStatus == api.ContainerStopped
}

func onSteadyStateCanResolve(target *api.Container, run *api.Container) bool {
	return target.GetDesiredStatus() >= api.ContainerCreated &&
		run.GetDesiredStatus() >= run.GetSteadyStateStatus()
}

// onSteadyStateIsResolved defines a relationship where a target cannot be
// created until 'dependency' has reached the steady state. Transitions include pulling.
func onSteadyStateIsResolved(target *api.Container, run *api.Container) bool {
	return target.GetDesiredStatus() >= api.ContainerCreated &&
		run.GetKnownStatus() >= run.GetSteadyStateStatus()
}
