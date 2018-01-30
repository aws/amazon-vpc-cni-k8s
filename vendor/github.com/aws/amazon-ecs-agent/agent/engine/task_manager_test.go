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

package engine

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/credentials"
	"github.com/aws/amazon-ecs-agent/agent/engine/dependencygraph"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate/mocks"
	"github.com/aws/amazon-ecs-agent/agent/engine/testdata"
	"github.com/aws/amazon-ecs-agent/agent/eventstream"
	"github.com/aws/amazon-ecs-agent/agent/resources/mock_resources"
	"github.com/aws/amazon-ecs-agent/agent/statechange"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/aws/amazon-ecs-agent/agent/statemanager/mocks"
	"github.com/aws/amazon-ecs-agent/agent/utils/ttime/mocks"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/stretchr/testify/assert"

	"github.com/golang/mock/gomock"
	"golang.org/x/net/context"
)

func TestHandleEventError(t *testing.T) {
	testCases := []struct {
		Name                         string
		EventStatus                  api.ContainerStatus
		CurrentKnownStatus           api.ContainerStatus
		Error                        engineError
		ExpectedKnownStatusSet       bool
		ExpectedKnownStatus          api.ContainerStatus
		ExpectedDesiredStatusStopped bool
		ExpectedOK                   bool
	}{
		{
			Name:               "StopTimedOut",
			EventStatus:        api.ContainerStopped,
			CurrentKnownStatus: api.ContainerRunning,
			Error:              &DockerTimeoutError{},
			ExpectedKnownStatusSet: true,
			ExpectedKnownStatus:    api.ContainerRunning,
			ExpectedOK:             false,
		},
		{
			Name:               "StopErrorRetriable",
			EventStatus:        api.ContainerStopped,
			CurrentKnownStatus: api.ContainerRunning,
			Error: &CannotStopContainerError{
				fromError: errors.New(""),
			},
			ExpectedKnownStatusSet: true,
			ExpectedKnownStatus:    api.ContainerRunning,
			ExpectedOK:             false,
		},
		{
			Name:               "StopErrorUnretriable",
			EventStatus:        api.ContainerStopped,
			CurrentKnownStatus: api.ContainerRunning,
			Error: &CannotStopContainerError{
				fromError: &docker.ContainerNotRunning{},
			},
			ExpectedKnownStatusSet:       true,
			ExpectedKnownStatus:          api.ContainerStopped,
			ExpectedDesiredStatusStopped: true,
			ExpectedOK:                   true,
		},
		{
			Name:        "PullError",
			Error:       &DockerTimeoutError{},
			EventStatus: api.ContainerPulled,
			ExpectedOK:  true,
		},
		{
			Name:               "Other",
			EventStatus:        api.ContainerRunning,
			CurrentKnownStatus: api.ContainerPulled,
			Error:              &ContainerVanishedError{},
			ExpectedKnownStatusSet:       true,
			ExpectedKnownStatus:          api.ContainerPulled,
			ExpectedDesiredStatusStopped: true,
			ExpectedOK:                   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			containerChange := dockerContainerChange{
				container: &api.Container{},
				event: DockerContainerChangeEvent{
					Status: tc.EventStatus,
					DockerContainerMetadata: DockerContainerMetadata{
						Error: tc.Error,
					},
				},
			}
			mtask := managedTask{}
			ok := mtask.handleEventError(containerChange, tc.CurrentKnownStatus)
			assert.Equal(t, tc.ExpectedOK, ok, "to proceed")
			if tc.ExpectedKnownStatusSet {
				assert.Equal(t, tc.ExpectedKnownStatus, containerChange.container.GetKnownStatus())
			}
			if tc.ExpectedDesiredStatusStopped {
				assert.Equal(t, api.ContainerStopped, containerChange.container.GetDesiredStatus())
			}
			assert.Equal(t, tc.Error.ErrorName(), containerChange.container.ApplyingError.ErrorName())
		})
	}
}

func TestContainerNextState(t *testing.T) {
	testCases := []struct {
		containerCurrentStatus       api.ContainerStatus
		containerDesiredStatus       api.ContainerStatus
		expectedContainerStatus      api.ContainerStatus
		expectedTransitionActionable bool
		reason                       error
	}{
		// NONE -> RUNNING transition is allowed and actionable, when desired is Running
		// The expected next status is Pulled
		{api.ContainerStatusNone, api.ContainerRunning, api.ContainerPulled, true, nil},
		// NONE -> RESOURCES_PROVISIONED transition is allowed and actionable, when desired
		// is Running. The exptected next status is Pulled
		{api.ContainerStatusNone, api.ContainerResourcesProvisioned, api.ContainerPulled, true, nil},
		// NONE -> NONE transition is not be allowed and is not actionable,
		// when desired is Running
		{api.ContainerStatusNone, api.ContainerStatusNone, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},
		// NONE -> STOPPED transition will result in STOPPED and is allowed, but not
		// actionable, when desired is STOPPED
		{api.ContainerStatusNone, api.ContainerStopped, api.ContainerStopped, false, nil},
		// PULLED -> RUNNING transition is allowed and actionable, when desired is Running
		// The exptected next status is Created
		{api.ContainerPulled, api.ContainerRunning, api.ContainerCreated, true, nil},
		// PULLED -> RESOURCES_PROVISIONED transition is allowed and actionable, when desired
		// is Running. The exptected next status is Created
		{api.ContainerPulled, api.ContainerResourcesProvisioned, api.ContainerCreated, true, nil},
		// PULLED -> PULLED transition is not allowed and not actionable,
		// when desired is Running
		{api.ContainerPulled, api.ContainerPulled, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},
		// PULLED -> NONE transition is not allowed and not actionable,
		// when desired is Running
		{api.ContainerPulled, api.ContainerStatusNone, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},
		// PULLED -> STOPPED transition will result in STOPPED and is allowed, but not
		// actionable, when desired is STOPPED
		{api.ContainerPulled, api.ContainerStopped, api.ContainerStopped, false, nil},
		// CREATED -> RUNNING transition is allowed and actionable, when desired is Running
		// The expected next status is Running
		{api.ContainerCreated, api.ContainerRunning, api.ContainerRunning, true, nil},
		// CREATED -> RESOURCES_PROVISIONED transition is allowed and actionable, when desired
		// is Running. The exptected next status is Running
		{api.ContainerCreated, api.ContainerResourcesProvisioned, api.ContainerRunning, true, nil},
		// CREATED -> CREATED transition is not allowed and not actionable,
		// when desired is Running
		{api.ContainerCreated, api.ContainerCreated, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},
		// CREATED -> NONE transition is not allowed and not actionable,
		// when desired is Running
		{api.ContainerCreated, api.ContainerStatusNone, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},
		// CREATED -> PULLED transition is not allowed and not actionable,
		// when desired is Running
		{api.ContainerCreated, api.ContainerPulled, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},
		// CREATED -> STOPPED transition will result in STOPPED and is allowed, but not
		// actionable, when desired is STOPPED
		{api.ContainerCreated, api.ContainerStopped, api.ContainerStopped, false, nil},
		// RUNNING -> STOPPED transition is allowed and actionable, when desired is Running
		// The expected next status is STOPPED
		{api.ContainerRunning, api.ContainerStopped, api.ContainerStopped, true, nil},
		// RUNNING -> RUNNING transition is not allowed and not actionable,
		// when desired is Running
		{api.ContainerRunning, api.ContainerRunning, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},
		// RUNNING -> RESOURCES_PROVISIONED is allowed when steady state status is
		// RESOURCES_PROVISIONED and desired is RESOURCES_PROVISIONED
		{api.ContainerRunning, api.ContainerResourcesProvisioned, api.ContainerResourcesProvisioned, true, nil},
		// RUNNING -> NONE transition is not allowed and not actionable,
		// when desired is Running
		{api.ContainerRunning, api.ContainerStatusNone, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},
		// RUNNING -> PULLED transition is not allowed and not actionable,
		// when desired is Running
		{api.ContainerRunning, api.ContainerPulled, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},
		// RUNNING -> CREATED transition is not allowed and not actionable,
		// when desired is Running
		{api.ContainerRunning, api.ContainerCreated, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},

		// RESOURCES_PROVISIONED -> RESOURCES_PROVISIONED transition is not allowed and not actionable,
		// when desired is Running
		{api.ContainerResourcesProvisioned, api.ContainerResourcesProvisioned, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},
		// RESOURCES_PROVISIONED -> RUNNING transition is not allowed and not actionable,
		// when desired is Running
		{api.ContainerResourcesProvisioned, api.ContainerRunning, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},
		// RESOURCES_PROVISIONED -> STOPPED transition is allowed and actionable, when desired
		// is Running. The exptected next status is STOPPED
		{api.ContainerResourcesProvisioned, api.ContainerStopped, api.ContainerStopped, true, nil},
		// RESOURCES_PROVISIONED -> NONE transition is not allowed and not actionable,
		// when desired is Running
		{api.ContainerResourcesProvisioned, api.ContainerStatusNone, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},
		// RESOURCES_PROVISIONED -> PULLED transition is not allowed and not actionable,
		// when desired is Running
		{api.ContainerResourcesProvisioned, api.ContainerPulled, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},
		// RESOURCES_PROVISIONED -> CREATED transition is not allowed and not actionable,
		// when desired is Running
		{api.ContainerResourcesProvisioned, api.ContainerCreated, api.ContainerStatusNone, false, dependencygraph.UnableTransitionContainerPassedDesiredStatus},
	}

	steadyStates := []api.ContainerStatus{api.ContainerRunning, api.ContainerResourcesProvisioned}

	for _, tc := range testCases {
		for _, steadyState := range steadyStates {
			t.Run(fmt.Sprintf("%s to %s Transition with Steady State %s",
				tc.containerCurrentStatus.String(), tc.containerDesiredStatus.String(), steadyState.String()), func(t *testing.T) {
				if tc.containerDesiredStatus == api.ContainerResourcesProvisioned &&
					steadyState < tc.containerDesiredStatus {
					t.Skipf("Skipping because of unassumable steady state [%s] and desired state [%s]",
						steadyState.String(), tc.containerDesiredStatus.String())
				}
				container := api.NewContainerWithSteadyState(steadyState)
				container.DesiredStatusUnsafe = tc.containerDesiredStatus
				container.KnownStatusUnsafe = tc.containerCurrentStatus
				task := &managedTask{
					Task: &api.Task{
						Containers: []*api.Container{
							container,
						},
						DesiredStatusUnsafe: api.TaskRunning,
					},
					engine: &DockerTaskEngine{},
				}
				transition := task.containerNextState(container)
				t.Logf("%s %v %v", transition.nextState, transition.actionRequired, transition.reason)
				assert.Equal(t, tc.expectedContainerStatus, transition.nextState, "Mismatch for expected next state")
				assert.Equal(t, tc.expectedTransitionActionable, transition.actionRequired, "Mismatch for expected actionable flag")
				assert.Equal(t, tc.reason, transition.reason, "Mismatch for expected reason")
			})
		}
	}
}

func TestContainerNextStateWithTransitionDependencies(t *testing.T) {
	testCases := []struct {
		name                         string
		containerCurrentStatus       api.ContainerStatus
		containerDesiredStatus       api.ContainerStatus
		containerDependentStatus     api.ContainerStatus
		dependencyCurrentStatus      api.ContainerStatus
		dependencySatisfiedStatus    api.ContainerStatus
		expectedContainerStatus      api.ContainerStatus
		expectedTransitionActionable bool
		reason                       error
	}{
		// NONE -> RUNNING transition is not allowed and not actionable, when pull depends on create and dependency is None
		{
			name: "pull depends on created, dependency is none",
			containerCurrentStatus:       api.ContainerStatusNone,
			containerDesiredStatus:       api.ContainerRunning,
			containerDependentStatus:     api.ContainerPulled,
			dependencyCurrentStatus:      api.ContainerStatusNone,
			dependencySatisfiedStatus:    api.ContainerCreated,
			expectedContainerStatus:      api.ContainerStatusNone,
			expectedTransitionActionable: false,
			reason: dependencygraph.UnableTransitionTransitionDependencyNotResolved,
		},
		// NONE -> RUNNING transition is not allowed and not actionable, when desired is Running and dependency is Created
		{
			name: "pull depends on running, dependency is created",
			containerCurrentStatus:       api.ContainerStatusNone,
			containerDesiredStatus:       api.ContainerRunning,
			containerDependentStatus:     api.ContainerPulled,
			dependencyCurrentStatus:      api.ContainerCreated,
			dependencySatisfiedStatus:    api.ContainerRunning,
			expectedContainerStatus:      api.ContainerStatusNone,
			expectedTransitionActionable: false,
			reason: dependencygraph.UnableTransitionTransitionDependencyNotResolved,
		},
		// NONE -> RUNNING transition is allowed and actionable, when desired is Running and dependency is Running
		// The expected next status is Pulled
		{
			name: "pull depends on running, dependency is running, next status is pulled",
			containerCurrentStatus:       api.ContainerStatusNone,
			containerDesiredStatus:       api.ContainerRunning,
			containerDependentStatus:     api.ContainerPulled,
			dependencyCurrentStatus:      api.ContainerRunning,
			dependencySatisfiedStatus:    api.ContainerRunning,
			expectedContainerStatus:      api.ContainerPulled,
			expectedTransitionActionable: true,
		},
		// NONE -> RUNNING transition is allowed and actionable, when desired is Running and dependency is Stopped
		// The expected next status is Pulled
		{
			name: "pull depends on running, dependency is stopped, next status is pulled",
			containerCurrentStatus:       api.ContainerStatusNone,
			containerDesiredStatus:       api.ContainerRunning,
			containerDependentStatus:     api.ContainerPulled,
			dependencyCurrentStatus:      api.ContainerStopped,
			dependencySatisfiedStatus:    api.ContainerRunning,
			expectedContainerStatus:      api.ContainerPulled,
			expectedTransitionActionable: true,
		},
		// NONE -> RUNNING transition is allowed and actionable, when desired is Running and dependency is None and
		// dependent status is Running
		{
			name: "create depends on running, dependency is none, next status is pulled",
			containerCurrentStatus:       api.ContainerStatusNone,
			containerDesiredStatus:       api.ContainerRunning,
			containerDependentStatus:     api.ContainerCreated,
			dependencyCurrentStatus:      api.ContainerStatusNone,
			dependencySatisfiedStatus:    api.ContainerRunning,
			expectedContainerStatus:      api.ContainerPulled,
			expectedTransitionActionable: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dependencyName := "dependency"
			container := &api.Container{
				DesiredStatusUnsafe: tc.containerDesiredStatus,
				KnownStatusUnsafe:   tc.containerCurrentStatus,
				TransitionDependencySet: api.TransitionDependencySet{
					ContainerDependencies: []api.ContainerDependency{{
						ContainerName:   dependencyName,
						DependentStatus: tc.containerDependentStatus,
						SatisfiedStatus: tc.dependencySatisfiedStatus,
					}},
				},
			}
			dependency := &api.Container{
				Name:              dependencyName,
				KnownStatusUnsafe: tc.dependencyCurrentStatus,
			}
			task := &managedTask{
				Task: &api.Task{
					Containers: []*api.Container{
						container,
						dependency,
					},
					DesiredStatusUnsafe: api.TaskRunning,
				},
				engine: &DockerTaskEngine{},
			}
			transition := task.containerNextState(container)
			assert.Equal(t, tc.expectedContainerStatus, transition.nextState,
				"Expected next state [%s] != Retrieved next state [%s]",
				tc.expectedContainerStatus.String(), transition.nextState.String())
			assert.Equal(t, tc.expectedTransitionActionable, transition.actionRequired, "transition actionable")
			assert.Equal(t, tc.reason, transition.reason, "transition possible")
		})
	}
}

func TestContainerNextStateWithDependencies(t *testing.T) {
	testCases := []struct {
		containerCurrentStatus       api.ContainerStatus
		containerDesiredStatus       api.ContainerStatus
		dependencyCurrentStatus      api.ContainerStatus
		expectedContainerStatus      api.ContainerStatus
		expectedTransitionActionable bool
		reason                       error
	}{
		// NONE -> RUNNING transition is not allowed and not actionable, when desired is Running and dependency is None
		{api.ContainerStatusNone, api.ContainerRunning, api.ContainerStatusNone, api.ContainerStatusNone, false, dependencygraph.UnableTransitionTransitionDependencyNotResolved},
		// NONE -> RUNNING transition is not allowed and not actionable, when desired is Running and dependency is Created
		{api.ContainerStatusNone, api.ContainerRunning, api.ContainerCreated, api.ContainerStatusNone, false, dependencygraph.UnableTransitionTransitionDependencyNotResolved},
		// NONE -> RUNNING transition is allowed and actionable, when desired is Running and dependency is Running
		// The expected next status is Pulled
		{api.ContainerStatusNone, api.ContainerRunning, api.ContainerRunning, api.ContainerPulled, true, nil},
		// NONE -> RUNNING transition is allowed and actionable, when desired is Running and dependency is Stopped
		// The expected next status is Pulled
		{api.ContainerStatusNone, api.ContainerRunning, api.ContainerStopped, api.ContainerPulled, true, nil},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s to %s Transition",
			tc.containerCurrentStatus.String(), tc.containerDesiredStatus.String()), func(t *testing.T) {
			dependencyName := "dependency"
			container := &api.Container{
				DesiredStatusUnsafe:     tc.containerDesiredStatus,
				KnownStatusUnsafe:       tc.containerCurrentStatus,
				SteadyStateDependencies: []string{dependencyName},
			}
			dependency := &api.Container{
				Name:              dependencyName,
				KnownStatusUnsafe: tc.dependencyCurrentStatus,
			}
			task := &managedTask{
				Task: &api.Task{
					Containers: []*api.Container{
						container,
						dependency,
					},
					DesiredStatusUnsafe: api.TaskRunning,
				},
				engine: &DockerTaskEngine{},
			}
			transition := task.containerNextState(container)
			assert.Equal(t, tc.expectedContainerStatus, transition.nextState,
				"Expected next state [%s] != Retrieved next state [%s]",
				tc.expectedContainerStatus.String(), transition.nextState.String())
			assert.Equal(t, tc.expectedTransitionActionable, transition.actionRequired, "transition actionable")
			assert.Equal(t, tc.reason, transition.reason, "transition possible")
		})
	}
}

func TestContainerNextStateWithPullCredentials(t *testing.T) {
	testCases := []struct {
		containerCurrentStatus       api.ContainerStatus
		containerDesiredStatus       api.ContainerStatus
		expectedContainerStatus      api.ContainerStatus
		credentialsID                string
		useExecutionRole             bool
		expectedTransitionActionable bool
		expectedTransitionReason     error
	}{
		// NONE -> RUNNING transition is not allowed when container is waiting for credentials
		{api.ContainerStatusNone, api.ContainerRunning, api.ContainerStatusNone, "not_existed", true, false, dependencygraph.UnableTransitionExecutionCredentialsNotResolved},
		// NONE -> RUNNING transition is allowed when the required execution credentials existed
		{api.ContainerStatusNone, api.ContainerRunning, api.ContainerPulled, "existed", true, true, nil},
		// PULLED -> RUNNING transition is allowed even the credentials is required
		{api.ContainerPulled, api.ContainerRunning, api.ContainerCreated, "not_existed", true, true, nil},
		// NONE -> STOPPED transition is allowed even the credentials is required
		{api.ContainerStatusNone, api.ContainerStopped, api.ContainerStopped, "not_existed", true, false, nil},
		// NONE -> RUNNING transition is allowed when the container doesn't use execution credentials
		{api.ContainerStatusNone, api.ContainerRunning, api.ContainerPulled, "not_existed", false, true, nil},
	}

	taskEngine := &DockerTaskEngine{
		credentialsManager: credentials.NewManager(),
	}

	err := taskEngine.credentialsManager.SetTaskCredentials(credentials.TaskIAMRoleCredentials{
		ARN: "taskarn",
		IAMRoleCredentials: credentials.IAMRoleCredentials{
			CredentialsID:   "existed",
			SessionToken:    "token",
			AccessKeyID:     "id",
			SecretAccessKey: "accesskey",
		},
	})
	assert.NoError(t, err, "setting task credentials failed")

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s to %s transition with useExecutionRole %v and credentials %s", tc.containerCurrentStatus.String(), tc.containerDesiredStatus.String(), tc.useExecutionRole, tc.credentialsID), func(t *testing.T) {
			container := &api.Container{
				DesiredStatusUnsafe: tc.containerDesiredStatus,
				KnownStatusUnsafe:   tc.containerCurrentStatus,
				RegistryAuthentication: &api.RegistryAuthenticationData{
					Type: "ecr",
					ECRAuthData: &api.ECRAuthData{
						UseExecutionRole: tc.useExecutionRole,
					},
				},
			}

			task := &managedTask{
				Task: &api.Task{
					Containers: []*api.Container{
						container,
					},
					ExecutionCredentialsID: tc.credentialsID,
					DesiredStatusUnsafe:    api.TaskRunning,
				},
				engine: taskEngine,
			}

			transition := task.containerNextState(container)
			assert.Equal(t, tc.expectedContainerStatus, transition.nextState, "Mismatch container status")
			assert.Equal(t, tc.expectedTransitionReason, transition.reason, "Mismatch transition possible")
			assert.Equal(t, tc.expectedTransitionActionable, transition.actionRequired, "Mismatch transition actionalbe")
		})
	}
}

func TestStartContainerTransitionsWhenForwardTransitionPossible(t *testing.T) {
	steadyStates := []api.ContainerStatus{api.ContainerRunning, api.ContainerResourcesProvisioned}
	for _, steadyState := range steadyStates {
		t.Run(fmt.Sprintf("Steady State is %s", steadyState.String()), func(t *testing.T) {
			firstContainerName := "container1"
			firstContainer := api.NewContainerWithSteadyState(steadyState)
			firstContainer.KnownStatusUnsafe = api.ContainerCreated
			firstContainer.DesiredStatusUnsafe = api.ContainerRunning
			firstContainer.Name = firstContainerName

			secondContainerName := "container2"
			secondContainer := api.NewContainerWithSteadyState(steadyState)
			secondContainer.KnownStatusUnsafe = api.ContainerPulled
			secondContainer.DesiredStatusUnsafe = api.ContainerRunning
			secondContainer.Name = secondContainerName

			task := &managedTask{
				Task: &api.Task{
					Containers: []*api.Container{
						firstContainer,
						secondContainer,
					},
					DesiredStatusUnsafe: api.TaskRunning,
				},
				engine: &DockerTaskEngine{},
			}

			pauseContainerName := "pause"
			waitForAssertions := sync.WaitGroup{}
			if steadyState == api.ContainerResourcesProvisioned {
				pauseContainer := api.NewContainerWithSteadyState(steadyState)
				pauseContainer.KnownStatusUnsafe = api.ContainerRunning
				pauseContainer.DesiredStatusUnsafe = api.ContainerResourcesProvisioned
				pauseContainer.Name = pauseContainerName
				task.Containers = append(task.Containers, pauseContainer)
				waitForAssertions.Add(1)
			}

			waitForAssertions.Add(2)
			canTransition, transitions, _ := task.startContainerTransitions(
				func(cont *api.Container, nextStatus api.ContainerStatus) {
					if cont.Name == firstContainerName {
						assert.Equal(t, nextStatus, api.ContainerRunning, "Mismatch for first container next status")
					} else if cont.Name == secondContainerName {
						assert.Equal(t, nextStatus, api.ContainerCreated, "Mismatch for second container next status")
					} else if cont.Name == pauseContainerName {
						assert.Equal(t, nextStatus, api.ContainerResourcesProvisioned, "Mismatch for pause container next status")
					}
					waitForAssertions.Done()
				})
			waitForAssertions.Wait()
			assert.True(t, canTransition, "Mismatch for canTransition")
			assert.NotEmpty(t, transitions)
			if steadyState == api.ContainerResourcesProvisioned {
				assert.Len(t, transitions, 3)
				pauseContainerTransition, ok := transitions[pauseContainerName]
				assert.True(t, ok, "Expected pause container transition to be in the transitions map")
				assert.Equal(t, pauseContainerTransition, api.ContainerResourcesProvisioned, "Mismatch for pause container transition state")
			} else {
				assert.Len(t, transitions, 2)
			}
			firstContainerTransition, ok := transitions[firstContainerName]
			assert.True(t, ok, "Expected first container transition to be in the transitions map")
			assert.Equal(t, firstContainerTransition, api.ContainerRunning, "Mismatch for first container transition state")
			secondContainerTransition, ok := transitions[secondContainerName]
			assert.True(t, ok, "Expected second container transition to be in the transitions map")
			assert.Equal(t, secondContainerTransition, api.ContainerCreated, "Mismatch for second container transition state")
		})
	}
}

func TestStartContainerTransitionsWhenForwardTransitionIsNotPossible(t *testing.T) {
	firstContainerName := "container1"
	firstContainer := &api.Container{
		KnownStatusUnsafe:   api.ContainerRunning,
		DesiredStatusUnsafe: api.ContainerRunning,
		Name:                firstContainerName,
	}
	secondContainerName := "container2"
	secondContainer := &api.Container{
		KnownStatusUnsafe:   api.ContainerRunning,
		DesiredStatusUnsafe: api.ContainerRunning,
		Name:                secondContainerName,
	}
	pauseContainerName := "pause"
	pauseContainer := api.NewContainerWithSteadyState(api.ContainerResourcesProvisioned)
	pauseContainer.KnownStatusUnsafe = api.ContainerResourcesProvisioned
	pauseContainer.DesiredStatusUnsafe = api.ContainerResourcesProvisioned
	pauseContainer.Name = pauseContainerName
	task := &managedTask{
		Task: &api.Task{
			Containers: []*api.Container{
				firstContainer,
				secondContainer,
				pauseContainer,
			},
			DesiredStatusUnsafe: api.TaskRunning,
		},
		engine: &DockerTaskEngine{},
	}

	canTransition, transitions, _ := task.startContainerTransitions(
		func(cont *api.Container, nextStatus api.ContainerStatus) {
			t.Error("Transition function should not be called when no transitions are possible")
		})
	assert.False(t, canTransition)
	assert.Empty(t, transitions)
}

func TestStartContainerTransitionsInvokesHandleContainerChange(t *testing.T) {
	eventStreamName := "TESTTASKENGINE"

	// Create a container with the intent to do
	// CREATERD -> STOPPED transition. This triggers
	// `managedTask.handleContainerChange()` and generates the following
	// events:
	// 1. container state change event for Submit* API
	// 2. task state change event for Submit* API
	// 3. container state change event for the internal event stream
	firstContainerName := "container1"
	firstContainer := &api.Container{
		KnownStatusUnsafe:   api.ContainerCreated,
		DesiredStatusUnsafe: api.ContainerStopped,
		Name:                firstContainerName,
	}

	containerChangeEventStream := eventstream.NewEventStream(eventStreamName, context.Background())
	containerChangeEventStream.StartListening()

	stateChangeEvents := make(chan statechange.Event)

	task := &managedTask{
		Task: &api.Task{
			Containers: []*api.Container{
				firstContainer,
			},
			DesiredStatusUnsafe: api.TaskRunning,
		},
		engine: &DockerTaskEngine{
			containerChangeEventStream: containerChangeEventStream,
			stateChangeEvents:          stateChangeEvents,
		},
	}

	eventsGenerated := sync.WaitGroup{}
	eventsGenerated.Add(2)
	containerChangeEventStream.Subscribe(eventStreamName, func(events ...interface{}) error {
		assert.NotNil(t, events)
		assert.Len(t, events, 1)
		event := events[0]
		containerChangeEvent, ok := event.(DockerContainerChangeEvent)
		assert.True(t, ok)
		assert.Equal(t, containerChangeEvent.Status, api.ContainerStopped)
		eventsGenerated.Done()
		return nil
	})
	defer containerChangeEventStream.Unsubscribe(eventStreamName)

	// account for container and task state change events for Submit* API
	go func() {
		<-stateChangeEvents
		<-stateChangeEvents
		eventsGenerated.Done()
	}()

	canTransition, transitions, _ := task.startContainerTransitions(
		func(cont *api.Container, nextStatus api.ContainerStatus) {
			t.Error("Invalid code path. The transition function should not be invoked when transitioning container from CREATED -> STOPPED")
		})
	assert.True(t, canTransition)
	assert.Empty(t, transitions)
	eventsGenerated.Wait()
}

func TestWaitForContainerTransitionsForNonTerminalTask(t *testing.T) {
	acsMessages := make(chan acsTransition)
	dockerMessages := make(chan dockerContainerChange)
	task := &managedTask{
		acsMessages:    acsMessages,
		dockerMessages: dockerMessages,
		Task: &api.Task{
			Containers: []*api.Container{},
		},
	}

	transitionChange := make(chan bool, 2)
	transitionChangeContainer := make(chan string, 2)

	firstContainerName := "container1"
	secondContainerName := "container2"

	// populate the transitions map with transitions for two
	// containers. We expect two sets of events to be consumed
	// by `waitForContainerTransitions`
	transitions := make(map[string]api.ContainerStatus)
	transitions[firstContainerName] = api.ContainerRunning
	transitions[secondContainerName] = api.ContainerRunning

	go func() {
		// Send "transitions completed" messages. These are being
		// sent out of order for no particular reason. We should be
		// resilient to the ordering of these messages anyway.
		transitionChange <- true
		transitionChangeContainer <- secondContainerName
		transitionChange <- true
		transitionChangeContainer <- firstContainerName
	}()

	// waitForContainerTransitions will block until it recieves events
	// sent by the go routine defined above
	task.waitForContainerTransitions(transitions, transitionChange, transitionChangeContainer)
}

// TestWaitForContainerTransitionsForTerminalTask verifies that the
// `waitForContainerTransitions` method doesn't wait for any container
// transitions when the task's desired status is STOPPED and if all
// containers in the task are in PULLED state
func TestWaitForContainerTransitionsForTerminalTask(t *testing.T) {
	acsMessages := make(chan acsTransition)
	dockerMessages := make(chan dockerContainerChange)
	task := &managedTask{
		acsMessages:    acsMessages,
		dockerMessages: dockerMessages,
		Task: &api.Task{
			Containers:        []*api.Container{},
			KnownStatusUnsafe: api.TaskStopped,
		},
	}

	transitionChange := make(chan bool, 2)
	transitionChangeContainer := make(chan string, 2)

	firstContainerName := "container1"
	secondContainerName := "container2"
	transitions := make(map[string]api.ContainerStatus)
	transitions[firstContainerName] = api.ContainerPulled
	transitions[secondContainerName] = api.ContainerPulled

	// Event though there are two keys in the transitions map, send
	// only one event. This tests that `waitForContainerTransitions` doesn't
	// block to recieve two events and will still progress
	go func() {
		transitionChange <- true
		transitionChangeContainer <- secondContainerName
	}()
	task.waitForContainerTransitions(transitions, transitionChange, transitionChangeContainer)
}

func TestOnContainersUnableToTransitionStateForDesiredStoppedTask(t *testing.T) {
	stateChangeEvents := make(chan statechange.Event)
	task := &managedTask{
		Task: &api.Task{
			Containers:          []*api.Container{},
			DesiredStatusUnsafe: api.TaskStopped,
		},
		engine: &DockerTaskEngine{
			stateChangeEvents: stateChangeEvents,
		},
	}
	eventsGenerated := sync.WaitGroup{}
	eventsGenerated.Add(1)

	go func() {
		event := <-stateChangeEvents
		assert.Equal(t, event.(api.TaskStateChange).Reason, taskUnableToTransitionToStoppedReason)
		eventsGenerated.Done()
	}()

	task.onContainersUnableToTransitionState()
	eventsGenerated.Wait()

	assert.Equal(t, task.GetDesiredStatus(), api.TaskStopped)
}

func TestOnContainersUnableToTransitionStateForDesiredRunningTask(t *testing.T) {
	firstContainerName := "container1"
	firstContainer := &api.Container{
		KnownStatusUnsafe:   api.ContainerCreated,
		DesiredStatusUnsafe: api.ContainerRunning,
		Name:                firstContainerName,
	}
	task := &managedTask{
		Task: &api.Task{
			Containers: []*api.Container{
				firstContainer,
			},
			DesiredStatusUnsafe: api.TaskRunning,
		},
	}

	task.onContainersUnableToTransitionState()
	assert.Equal(t, task.GetDesiredStatus(), api.TaskStopped)
	assert.Equal(t, task.Containers[0].GetDesiredStatus(), api.ContainerStopped)
}

// TODO: Test progressContainers workflow

func TestHandleStoppedToSteadyStateTransition(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStateManager := mock_statemanager.NewMockStateManager(ctrl)
	defer ctrl.Finish()

	taskEngine := &DockerTaskEngine{
		saver: mockStateManager,
	}
	firstContainerName := "container1"
	firstContainer := &api.Container{
		KnownStatusUnsafe: api.ContainerStopped,
		Name:              firstContainerName,
	}
	secondContainerName := "container2"
	secondContainer := &api.Container{
		KnownStatusUnsafe:   api.ContainerRunning,
		DesiredStatusUnsafe: api.ContainerRunning,
		Name:                secondContainerName,
	}
	mTask := &managedTask{
		Task: &api.Task{
			Containers: []*api.Container{
				firstContainer,
				secondContainer,
			},
			Arn: "task1",
		},
		engine:         taskEngine,
		acsMessages:    make(chan acsTransition),
		dockerMessages: make(chan dockerContainerChange),
	}
	taskEngine.managedTasks = make(map[string]*managedTask)
	taskEngine.managedTasks["task1"] = mTask

	var waitForTransitionFunctionInvocation sync.WaitGroup
	waitForTransitionFunctionInvocation.Add(1)
	transitionFunction := func(task *api.Task, container *api.Container) DockerContainerMetadata {
		assert.Equal(t, firstContainerName, container.Name,
			"Mismatch in container reference in transition function")
		waitForTransitionFunctionInvocation.Done()
		return DockerContainerMetadata{}
	}

	taskEngine.containerStatusToTransitionFunction = map[api.ContainerStatus]transitionApplyFunc{
		api.ContainerStopped: transitionFunction,
	}

	// Recieved RUNNING event, known status is not STOPPED, expect this to
	// be a noop. Assertions in transitionFunction asserts that as well
	mTask.handleStoppedToRunningContainerTransition(
		api.ContainerRunning, secondContainer)

	// Start building preconditions and assertions for STOPPED -> RUNNING
	// transition that will be triggered by next invocation of
	// handleStoppedToRunningContainerTransition

	// We expect state manager Save to be invoked on container transition
	// for the next transition
	mockStateManager.EXPECT().Save()
	// This wait group ensures that a docker message is generated as a
	// result of the transition function
	var waitForDockerMessageAssertions sync.WaitGroup
	waitForDockerMessageAssertions.Add(1)
	go func() {
		dockerMessage := <-mTask.dockerMessages
		assert.Equal(t, api.ContainerStopped, dockerMessage.event.Status,
			"Mismatch in event status")
		assert.Equal(t, firstContainerName, dockerMessage.container.Name,
			"Mismatch in container reference in event")
		waitForDockerMessageAssertions.Done()
	}()
	// Recieved RUNNING, known status is STOPPED, expect this to invoke
	// transition function once
	mTask.handleStoppedToRunningContainerTransition(
		api.ContainerRunning, firstContainer)

	// Wait for wait groups to be done
	waitForTransitionFunctionInvocation.Wait()
	waitForDockerMessageAssertions.Wait()

	// We now have an empty transition function map. Any further transitions
	// should be noops
	delete(taskEngine.containerStatusToTransitionFunction, api.ContainerStopped)
	// Simulate getting RUNNING event for a STOPPED container 10 times.
	// All of these should be noops. 10 is chosen arbitrarily. Any number > 0
	// should be fine here
	for i := 0; i < 10; i++ {
		mTask.handleStoppedToRunningContainerTransition(
			api.ContainerRunning, firstContainer)
	}
}

func TestCleanupTask(t *testing.T) {
	cfg := getTestConfig()
	ctrl := gomock.NewController(t)
	mockTime := mock_ttime.NewMockTime(ctrl)
	mockState := mock_dockerstate.NewMockTaskEngineState(ctrl)
	mockClient := NewMockDockerClient(ctrl)
	mockImageManager := NewMockImageManager(ctrl)
	defer ctrl.Finish()

	taskEngine := &DockerTaskEngine{
		cfg:          &cfg,
		saver:        statemanager.NewNoopStateManager(),
		state:        mockState,
		client:       mockClient,
		imageManager: mockImageManager,
	}
	mTask := &managedTask{
		Task:           testdata.LoadTask("sleep5"),
		_time:          mockTime,
		engine:         taskEngine,
		acsMessages:    make(chan acsTransition),
		dockerMessages: make(chan dockerContainerChange),
	}
	mTask.SetKnownStatus(api.TaskStopped)
	mTask.SetSentStatus(api.TaskStopped)
	container := mTask.Containers[0]
	dockerContainer := &api.DockerContainer{
		DockerName: "dockerContainer",
	}

	// Expectations for triggering cleanup
	now := mTask.GetKnownStatusTime()
	taskStoppedDuration := 1 * time.Minute
	mockTime.EXPECT().Now().Return(now).AnyTimes()
	cleanupTimeTrigger := make(chan time.Time)
	mockTime.EXPECT().After(gomock.Any()).Return(cleanupTimeTrigger)
	go func() {
		cleanupTimeTrigger <- now
	}()

	// Expectations to verify that the task gets removed
	mockState.EXPECT().ContainerMapByArn(mTask.Arn).Return(map[string]*api.DockerContainer{container.Name: dockerContainer}, true)
	mockClient.EXPECT().RemoveContainer(dockerContainer.DockerName, gomock.Any()).Return(nil)
	mockImageManager.EXPECT().RemoveContainerReferenceFromImageState(container).Return(nil)
	mockState.EXPECT().RemoveTask(mTask.Task)
	mTask.cleanupTask(taskStoppedDuration)
}

func TestCleanupTaskWaitsForStoppedSent(t *testing.T) {
	cfg := getTestConfig()
	ctrl := gomock.NewController(t)
	mockTime := mock_ttime.NewMockTime(ctrl)
	mockState := mock_dockerstate.NewMockTaskEngineState(ctrl)
	mockClient := NewMockDockerClient(ctrl)
	mockImageManager := NewMockImageManager(ctrl)
	defer ctrl.Finish()

	taskEngine := &DockerTaskEngine{
		cfg:          &cfg,
		saver:        statemanager.NewNoopStateManager(),
		state:        mockState,
		client:       mockClient,
		imageManager: mockImageManager,
	}
	mTask := &managedTask{
		Task:           testdata.LoadTask("sleep5"),
		_time:          mockTime,
		engine:         taskEngine,
		acsMessages:    make(chan acsTransition),
		dockerMessages: make(chan dockerContainerChange),
	}
	mTask.SetKnownStatus(api.TaskStopped)
	mTask.SetSentStatus(api.TaskRunning)
	container := mTask.Containers[0]
	dockerContainer := &api.DockerContainer{
		DockerName: "dockerContainer",
	}

	// Expectations for triggering cleanup
	now := mTask.GetKnownStatusTime()
	taskStoppedDuration := 1 * time.Minute
	mockTime.EXPECT().Now().Return(now).AnyTimes()
	cleanupTimeTrigger := make(chan time.Time)
	mockTime.EXPECT().After(gomock.Any()).Return(cleanupTimeTrigger)
	go func() {
		cleanupTimeTrigger <- now
	}()
	timesCalled := 0
	callsExpected := 3
	mockTime.EXPECT().Sleep(gomock.Any()).AnyTimes().Do(func(_ interface{}) {
		timesCalled++
		if timesCalled == callsExpected {
			mTask.SetSentStatus(api.TaskStopped)
		} else if timesCalled > callsExpected {
			t.Errorf("Sleep called too many times, called %d but expected %d", timesCalled, callsExpected)
		}
	})
	assert.Equal(t, api.TaskRunning, mTask.GetSentStatus())

	// Expectations to verify that the task gets removed
	mockState.EXPECT().ContainerMapByArn(mTask.Arn).Return(map[string]*api.DockerContainer{container.Name: dockerContainer}, true)
	mockClient.EXPECT().RemoveContainer(dockerContainer.DockerName, gomock.Any()).Return(nil)
	mockImageManager.EXPECT().RemoveContainerReferenceFromImageState(container).Return(nil)
	mockState.EXPECT().RemoveTask(mTask.Task)
	mTask.cleanupTask(taskStoppedDuration)
	assert.Equal(t, api.TaskStopped, mTask.GetSentStatus())
}

func TestCleanupTaskGivesUpIfWaitingTooLong(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockTime := mock_ttime.NewMockTime(ctrl)
	mockState := mock_dockerstate.NewMockTaskEngineState(ctrl)
	mockClient := NewMockDockerClient(ctrl)
	mockImageManager := NewMockImageManager(ctrl)
	defer ctrl.Finish()

	cfg := getTestConfig()
	taskEngine := &DockerTaskEngine{
		cfg:          &cfg,
		saver:        statemanager.NewNoopStateManager(),
		state:        mockState,
		client:       mockClient,
		imageManager: mockImageManager,
	}
	mTask := &managedTask{
		Task:           testdata.LoadTask("sleep5"),
		_time:          mockTime,
		engine:         taskEngine,
		acsMessages:    make(chan acsTransition),
		dockerMessages: make(chan dockerContainerChange),
	}
	mTask.SetKnownStatus(api.TaskStopped)
	mTask.SetSentStatus(api.TaskRunning)

	// Expectations for triggering cleanup
	now := mTask.GetKnownStatusTime()
	taskStoppedDuration := 1 * time.Minute
	mockTime.EXPECT().Now().Return(now).AnyTimes()
	cleanupTimeTrigger := make(chan time.Time)
	mockTime.EXPECT().After(gomock.Any()).Return(cleanupTimeTrigger)
	go func() {
		cleanupTimeTrigger <- now
	}()
	_maxStoppedWaitTimes = 10
	defer func() {
		// reset
		_maxStoppedWaitTimes = int(maxStoppedWaitTimes)
	}()
	mockTime.EXPECT().Sleep(gomock.Any()).Times(_maxStoppedWaitTimes)
	assert.Equal(t, api.TaskRunning, mTask.GetSentStatus())

	// No cleanup expected
	mTask.cleanupTask(taskStoppedDuration)
	assert.Equal(t, api.TaskRunning, mTask.GetSentStatus())
}

func TestCleanupTaskENIs(t *testing.T) {
	cfg := getTestConfig()
	ctrl := gomock.NewController(t)
	mockTime := mock_ttime.NewMockTime(ctrl)
	mockState := mock_dockerstate.NewMockTaskEngineState(ctrl)
	mockClient := NewMockDockerClient(ctrl)
	mockImageManager := NewMockImageManager(ctrl)
	defer ctrl.Finish()

	taskEngine := &DockerTaskEngine{
		cfg:          &cfg,
		saver:        statemanager.NewNoopStateManager(),
		state:        mockState,
		client:       mockClient,
		imageManager: mockImageManager,
	}
	mTask := &managedTask{
		Task:           testdata.LoadTask("sleep5"),
		_time:          mockTime,
		engine:         taskEngine,
		acsMessages:    make(chan acsTransition),
		dockerMessages: make(chan dockerContainerChange),
	}
	mTask.SetTaskENI(&api.ENI{
		ID: "TestCleanupTaskENIs",
		IPV4Addresses: []*api.ENIIPV4Address{
			{
				Primary: true,
				Address: ipv4,
			},
		},
		MacAddress: mac,
		IPV6Addresses: []*api.ENIIPV6Address{
			{
				Address: ipv6,
			},
		},
	})

	mTask.SetKnownStatus(api.TaskStopped)
	mTask.SetSentStatus(api.TaskStopped)
	container := mTask.Containers[0]
	dockerContainer := &api.DockerContainer{
		DockerName: "dockerContainer",
	}

	// Expectations for triggering cleanup
	now := mTask.GetKnownStatusTime()
	taskStoppedDuration := 1 * time.Minute
	mockTime.EXPECT().Now().Return(now).AnyTimes()
	cleanupTimeTrigger := make(chan time.Time)
	mockTime.EXPECT().After(gomock.Any()).Return(cleanupTimeTrigger)
	go func() {
		cleanupTimeTrigger <- now
	}()

	// Expectations to verify that the task gets removed
	mockState.EXPECT().ContainerMapByArn(mTask.Arn).Return(map[string]*api.DockerContainer{container.Name: dockerContainer}, true)
	mockClient.EXPECT().RemoveContainer(dockerContainer.DockerName, gomock.Any()).Return(nil)
	mockImageManager.EXPECT().RemoveContainerReferenceFromImageState(container).Return(nil)
	mockState.EXPECT().RemoveTask(mTask.Task)
	mockState.EXPECT().RemoveENIAttachment(mac)
	mTask.cleanupTask(taskStoppedDuration)
}

func TestTaskWaitForExecutionCredentials(t *testing.T) {
	tcs := []struct {
		errs   []error
		result bool
		msg    string
	}{
		{
			errs: []error{
				dependencygraph.UnableTransitionExecutionCredentialsNotResolved,
				dependencygraph.UnableTransitionContainerPassedDesiredStatus,
				fmt.Errorf("other error"),
			},
			result: true,
			msg:    "managed task should wait for credentials if the credentials dependency is not resolved",
		},
		{
			result: false,
			msg:    "managed task does not need to wait for credentials if there is no error",
		},
		{
			errs: []error{
				dependencygraph.UnableTransitionContainerPassedDesiredStatus,
				dependencygraph.UnableTransitionTransitionDependencyNotResolved,
				fmt.Errorf("other errors"),
			},
			result: false,
			msg:    "managed task does not need to wait for credentials if there is no credentials dependency error",
		},
	}

	for _, tc := range tcs {
		t.Run(fmt.Sprintf("%v", tc.errs), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockTime := mock_ttime.NewMockTime(ctrl)
			mockTimer := mock_ttime.NewMockTimer(ctrl)
			task := &managedTask{
				Task: &api.Task{
					KnownStatusUnsafe:   api.TaskRunning,
					DesiredStatusUnsafe: api.TaskRunning,
				},
				_time:       mockTime,
				acsMessages: make(chan acsTransition),
			}
			if tc.result {
				mockTime.EXPECT().AfterFunc(gomock.Any(), gomock.Any()).Return(mockTimer)
				mockTimer.EXPECT().Stop()
				go func() { task.acsMessages <- acsTransition{desiredStatus: api.TaskRunning} }()
			}

			assert.Equal(t, tc.result, task.waitForExecutionCredentialsFromACS(tc.errs), tc.msg)
		})
	}
}

func TestCleanupTaskWithInvalidInterval(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockTime := mock_ttime.NewMockTime(ctrl)
	mockState := mock_dockerstate.NewMockTaskEngineState(ctrl)
	mockClient := NewMockDockerClient(ctrl)
	mockImageManager := NewMockImageManager(ctrl)
	defer ctrl.Finish()

	cfg := getTestConfig()
	taskEngine := &DockerTaskEngine{
		cfg:          &cfg,
		saver:        statemanager.NewNoopStateManager(),
		state:        mockState,
		client:       mockClient,
		imageManager: mockImageManager,
	}
	mTask := &managedTask{
		Task:           testdata.LoadTask("sleep5"),
		_time:          mockTime,
		engine:         taskEngine,
		acsMessages:    make(chan acsTransition),
		dockerMessages: make(chan dockerContainerChange),
	}

	mTask.SetKnownStatus(api.TaskStopped)
	mTask.SetSentStatus(api.TaskStopped)
	container := mTask.Containers[0]
	dockerContainer := &api.DockerContainer{
		DockerName: "dockerContainer",
	}

	// Expectations for triggering cleanup
	now := mTask.GetKnownStatusTime()
	taskStoppedDuration := -1 * time.Minute
	mockTime.EXPECT().Now().Return(now).AnyTimes()
	cleanupTimeTrigger := make(chan time.Time)
	mockTime.EXPECT().After(gomock.Any()).Return(cleanupTimeTrigger)
	go func() {
		cleanupTimeTrigger <- now
	}()

	// Expectations to verify that the task gets removed
	mockState.EXPECT().ContainerMapByArn(mTask.Arn).Return(map[string]*api.DockerContainer{container.Name: dockerContainer}, true)
	mockClient.EXPECT().RemoveContainer(dockerContainer.DockerName, gomock.Any()).Return(nil)
	mockImageManager.EXPECT().RemoveContainerReferenceFromImageState(container).Return(nil)
	mockState.EXPECT().RemoveTask(mTask.Task)
	mTask.cleanupTask(taskStoppedDuration)
}

func TestCleanupTaskWithResourceHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockTime := mock_ttime.NewMockTime(ctrl)
	mockState := mock_dockerstate.NewMockTaskEngineState(ctrl)
	mockClient := NewMockDockerClient(ctrl)
	mockImageManager := NewMockImageManager(ctrl)
	mockResource := mock_resources.NewMockResource(ctrl)
	defer ctrl.Finish()

	cfg := getTestConfig()
	cfg.TaskCPUMemLimit = config.ExplicitlyEnabled

	taskEngine := &DockerTaskEngine{
		cfg:          &cfg,
		saver:        statemanager.NewNoopStateManager(),
		state:        mockState,
		client:       mockClient,
		imageManager: mockImageManager,
	}
	mTask := &managedTask{
		Task:           testdata.LoadTask("sleep5TaskCgroup"),
		_time:          mockTime,
		engine:         taskEngine,
		acsMessages:    make(chan acsTransition),
		dockerMessages: make(chan dockerContainerChange),
		resource:       mockResource,
	}
	mTask.SetKnownStatus(api.TaskStopped)
	mTask.SetSentStatus(api.TaskStopped)
	container := mTask.Containers[0]
	dockerContainer := &api.DockerContainer{
		DockerName: "dockerContainer",
	}

	// Expectations for triggering cleanup
	now := mTask.GetKnownStatusTime()
	taskStoppedDuration := 1 * time.Minute
	mockTime.EXPECT().Now().Return(now).AnyTimes()
	cleanupTimeTrigger := make(chan time.Time)
	mockTime.EXPECT().After(gomock.Any()).Return(cleanupTimeTrigger)
	go func() {
		cleanupTimeTrigger <- now
	}()

	// Expectations to verify that the task gets removed
	mockState.EXPECT().ContainerMapByArn(mTask.Arn).Return(map[string]*api.DockerContainer{container.Name: dockerContainer}, true)
	mockClient.EXPECT().RemoveContainer(dockerContainer.DockerName, gomock.Any()).Return(nil)
	mockImageManager.EXPECT().RemoveContainerReferenceFromImageState(container).Return(nil)
	mockState.EXPECT().RemoveTask(mTask.Task)
	mockResource.EXPECT().Cleanup(gomock.Any()).Return(nil)
	mTask.cleanupTask(taskStoppedDuration)
}

func TestCleanupTaskWithResourceErrorPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockTime := mock_ttime.NewMockTime(ctrl)
	mockState := mock_dockerstate.NewMockTaskEngineState(ctrl)
	mockClient := NewMockDockerClient(ctrl)
	mockImageManager := NewMockImageManager(ctrl)
	mockResource := mock_resources.NewMockResource(ctrl)
	defer ctrl.Finish()

	cfg := getTestConfig()
	cfg.TaskCPUMemLimit = config.ExplicitlyEnabled

	taskEngine := &DockerTaskEngine{
		cfg:          &cfg,
		saver:        statemanager.NewNoopStateManager(),
		state:        mockState,
		client:       mockClient,
		imageManager: mockImageManager,
	}
	mTask := &managedTask{
		Task:           testdata.LoadTask("sleep5TaskCgroup"),
		_time:          mockTime,
		engine:         taskEngine,
		acsMessages:    make(chan acsTransition),
		dockerMessages: make(chan dockerContainerChange),
		resource:       mockResource,
	}
	mTask.SetKnownStatus(api.TaskStopped)
	mTask.SetSentStatus(api.TaskStopped)
	container := mTask.Containers[0]
	dockerContainer := &api.DockerContainer{
		DockerName: "dockerContainer",
	}

	// Expectations for triggering cleanup
	now := mTask.GetKnownStatusTime()
	taskStoppedDuration := 1 * time.Minute
	mockTime.EXPECT().Now().Return(now).AnyTimes()
	cleanupTimeTrigger := make(chan time.Time)
	mockTime.EXPECT().After(gomock.Any()).Return(cleanupTimeTrigger)
	go func() {
		cleanupTimeTrigger <- now
	}()

	// Expectations to verify that the task gets removed
	mockState.EXPECT().ContainerMapByArn(mTask.Arn).Return(map[string]*api.DockerContainer{container.Name: dockerContainer}, true)
	mockClient.EXPECT().RemoveContainer(dockerContainer.DockerName, gomock.Any()).Return(nil)
	mockImageManager.EXPECT().RemoveContainerReferenceFromImageState(container).Return(nil)
	mockState.EXPECT().RemoveTask(mTask.Task)
	mockResource.EXPECT().Cleanup(gomock.Any()).Return(errors.New("resource cleanup error"))
	mTask.cleanupTask(taskStoppedDuration)
}

func getTestConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.TaskCPUMemLimit = config.ExplicitlyDisabled
	return cfg
}
