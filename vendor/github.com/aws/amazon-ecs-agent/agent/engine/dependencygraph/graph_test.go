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

package dependencygraph

import (
	"testing"

	"fmt"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/stretchr/testify/assert"
)

func volumeStrToVol(vols []string) []api.VolumeFrom {
	ret := make([]api.VolumeFrom, len(vols))
	for i, v := range vols {
		ret[i] = api.VolumeFrom{SourceContainer: v, ReadOnly: false}
	}
	return ret
}

func steadyStateContainer(name string, links, volumes []string, desiredState api.ContainerStatus, steadyState api.ContainerStatus) *api.Container {
	container := api.NewContainerWithSteadyState(steadyState)
	container.Name = name
	container.Links = links
	container.VolumesFrom = volumeStrToVol(volumes)
	container.DesiredStatusUnsafe = desiredState
	return container
}

func createdContainer(name string, links, volumes []string, steadyState api.ContainerStatus) *api.Container {
	container := api.NewContainerWithSteadyState(steadyState)
	container.Name = name
	container.Links = links
	container.VolumesFrom = volumeStrToVol(volumes)
	container.DesiredStatusUnsafe = api.ContainerCreated
	return container
}

func TestValidDependencies(t *testing.T) {
	// Empty task
	task := &api.Task{}
	resolveable := ValidDependencies(task)
	assert.True(t, resolveable, "The zero dependency graph should resolve")

	task = &api.Task{
		Containers: []*api.Container{
			{
				Name:                "redis",
				DesiredStatusUnsafe: api.ContainerRunning,
			},
		},
	}
	resolveable = ValidDependencies(task)
	assert.True(t, resolveable, "One container should resolve trivially")

	// Webserver stack
	php := steadyStateContainer("php", []string{"db"}, []string{}, api.ContainerRunning, api.ContainerRunning)
	db := steadyStateContainer("db", []string{}, []string{"dbdatavolume"}, api.ContainerRunning, api.ContainerRunning)
	dbdata := createdContainer("dbdatavolume", []string{}, []string{}, api.ContainerRunning)
	webserver := steadyStateContainer("webserver", []string{"php"}, []string{"htmldata"}, api.ContainerRunning, api.ContainerRunning)
	htmldata := steadyStateContainer("htmldata", []string{}, []string{"sharedcssfiles"}, api.ContainerRunning, api.ContainerRunning)
	sharedcssfiles := createdContainer("sharedcssfiles", []string{}, []string{}, api.ContainerRunning)

	task = &api.Task{
		Containers: []*api.Container{
			php, db, dbdata, webserver, htmldata, sharedcssfiles,
		},
	}

	resolveable = ValidDependencies(task)
	assert.True(t, resolveable, "The webserver group should resolve just fine")
}

func TestValidDependenciesWithCycles(t *testing.T) {
	// Unresolveable: cycle
	task := &api.Task{
		Containers: []*api.Container{
			steadyStateContainer("a", []string{"b"}, []string{}, api.ContainerRunning, api.ContainerRunning),
			steadyStateContainer("b", []string{"a"}, []string{}, api.ContainerRunning, api.ContainerRunning),
		},
	}
	resolveable := ValidDependencies(task)
	assert.False(t, resolveable, "Cycle should not be resolveable")
}

func TestValidDependenciesWithUnresolvedReference(t *testing.T) {
	// Unresolveable, reference doesn't exist
	task := &api.Task{
		Containers: []*api.Container{
			steadyStateContainer("php", []string{"db"}, []string{}, api.ContainerRunning, api.ContainerRunning),
		},
	}
	resolveable := ValidDependencies(task)
	assert.False(t, resolveable, "Nonexistent reference shouldn't resolve")
}

func TestDependenciesAreResolvedWhenSteadyStateIsRunning(t *testing.T) {
	task := &api.Task{
		Containers: []*api.Container{
			{
				Name:                "redis",
				DesiredStatusUnsafe: api.ContainerRunning,
			},
		},
	}
	err := DependenciesAreResolved(task.Containers[0], task.Containers, "", nil)
	assert.NoError(t, err, "One container should resolve trivially")

	// Webserver stack
	php := steadyStateContainer("php", []string{"db"}, []string{}, api.ContainerRunning, api.ContainerRunning)
	db := steadyStateContainer("db", []string{}, []string{"dbdatavolume"}, api.ContainerRunning, api.ContainerRunning)
	dbdata := createdContainer("dbdatavolume", []string{}, []string{}, api.ContainerRunning)
	webserver := steadyStateContainer("webserver", []string{"php"}, []string{"htmldata"}, api.ContainerRunning, api.ContainerRunning)
	htmldata := steadyStateContainer("htmldata", []string{}, []string{"sharedcssfiles"}, api.ContainerRunning, api.ContainerRunning)
	sharedcssfiles := createdContainer("sharedcssfiles", []string{}, []string{}, api.ContainerRunning)

	task = &api.Task{
		Containers: []*api.Container{
			php, db, dbdata, webserver, htmldata, sharedcssfiles,
		},
	}

	err = DependenciesAreResolved(php, task.Containers, "", nil)
	assert.Error(t, err, "Shouldn't be resolved; db isn't running")

	err = DependenciesAreResolved(db, task.Containers, "", nil)
	assert.Error(t, err, "Shouldn't be resolved; dbdatavolume isn't created")

	err = DependenciesAreResolved(dbdata, task.Containers, "", nil)
	assert.NoError(t, err, "data volume with no deps should resolve")

	dbdata.KnownStatusUnsafe = api.ContainerCreated
	err = DependenciesAreResolved(php, task.Containers, "", nil)
	assert.Error(t, err, "Php shouldn't run, db is not created")

	db.KnownStatusUnsafe = api.ContainerCreated
	err = DependenciesAreResolved(php, task.Containers, "", nil)
	assert.Error(t, err, "Php shouldn't run, db is not running")

	err = DependenciesAreResolved(db, task.Containers, "", nil)
	assert.NoError(t, err, "db should be resolved, dbdata volume is Created")
	db.KnownStatusUnsafe = api.ContainerRunning

	err = DependenciesAreResolved(php, task.Containers, "", nil)
	assert.NoError(t, err, "Php should resolve")
}

func TestRunDependencies(t *testing.T) {
	c1 := &api.Container{
		Name:              "a",
		KnownStatusUnsafe: api.ContainerStatusNone,
	}
	c2 := &api.Container{
		Name:                    "b",
		KnownStatusUnsafe:       api.ContainerStatusNone,
		DesiredStatusUnsafe:     api.ContainerCreated,
		SteadyStateDependencies: []string{"a"},
	}
	task := &api.Task{Containers: []*api.Container{c1, c2}}

	assert.Error(t, DependenciesAreResolved(c2, task.Containers, "", nil), "Dependencies should not be resolved")
	task.Containers[1].SetDesiredStatus(api.ContainerRunning)
	assert.Error(t, DependenciesAreResolved(c2, task.Containers, "", nil), "Dependencies should not be resolved")

	task.Containers[0].KnownStatusUnsafe = api.ContainerRunning
	assert.NoError(t, DependenciesAreResolved(c2, task.Containers, "", nil), "Dependencies should be resolved")

	task.Containers[1].SetDesiredStatus(api.ContainerCreated)
	assert.NoError(t, DependenciesAreResolved(c1, task.Containers, "", nil), "Dependencies should be resolved")
}

func TestRunDependenciesWhenSteadyStateIsResourcesProvisionedForOneContainer(t *testing.T) {
	// Webserver stack
	php := steadyStateContainer("php", []string{"db"}, []string{}, api.ContainerRunning, api.ContainerRunning)
	db := steadyStateContainer("db", []string{}, []string{"dbdatavolume"}, api.ContainerRunning, api.ContainerRunning)
	dbdata := createdContainer("dbdatavolume", []string{}, []string{}, api.ContainerRunning)
	webserver := steadyStateContainer("webserver", []string{"php"}, []string{"htmldata"}, api.ContainerRunning, api.ContainerRunning)
	htmldata := steadyStateContainer("htmldata", []string{}, []string{"sharedcssfiles"}, api.ContainerRunning, api.ContainerRunning)
	sharedcssfiles := createdContainer("sharedcssfiles", []string{}, []string{}, api.ContainerRunning)
	// The Pause container, being added to the webserver stack
	pause := steadyStateContainer("pause", []string{}, []string{}, api.ContainerResourcesProvisioned, api.ContainerResourcesProvisioned)

	task := &api.Task{
		Containers: []*api.Container{
			php, db, dbdata, webserver, htmldata, sharedcssfiles, pause,
		},
	}

	// Add a dependency on the pause container for all containers in the webserver stack
	for _, container := range task.Containers {
		if container.Name == "pause" {
			continue
		}
		container.SteadyStateDependencies = []string{"pause"}
		err := DependenciesAreResolved(container, task.Containers, "", nil)
		assert.Error(t, err, "Shouldn't be resolved; pause isn't running")
	}

	err := DependenciesAreResolved(pause, task.Containers, "", nil)
	assert.NoError(t, err, "Pause container's dependencies should be resolved")

	// Transition pause container to RUNNING
	pause.KnownStatusUnsafe = api.ContainerRunning
	// Transition dependencies in webserver stack to CREATED/RUNNING state
	dbdata.KnownStatusUnsafe = api.ContainerCreated
	db.KnownStatusUnsafe = api.ContainerRunning
	for _, container := range task.Containers {
		if container.Name == "pause" {
			continue
		}
		// Assert that dependencies remain unresolved until the pause container reaches
		// RESOURCES_PROVISIONED
		err = DependenciesAreResolved(container, task.Containers, "", nil)
		assert.Error(t, err, "Shouldn't be resolved; pause isn't running")
	}
	pause.KnownStatusUnsafe = api.ContainerResourcesProvisioned
	// Dependecies should be resolved now that the 'pause' container has
	// transitioned into RESOURCES_PROVISIONED
	err = DependenciesAreResolved(php, task.Containers, "", nil)
	assert.NoError(t, err, "Php should resolve")
}

func TestVolumeCanResolve(t *testing.T) {
	testcases := []struct {
		TargetDesired api.ContainerStatus
		VolumeDesired api.ContainerStatus
		Resolvable    bool
	}{
		{
			TargetDesired: api.ContainerCreated,
			VolumeDesired: api.ContainerStatusNone,
			Resolvable:    false,
		},
		{
			TargetDesired: api.ContainerCreated,
			VolumeDesired: api.ContainerCreated,
			Resolvable:    true,
		},
		{
			TargetDesired: api.ContainerCreated,
			VolumeDesired: api.ContainerRunning,
			Resolvable:    true,
		},
		{
			TargetDesired: api.ContainerCreated,
			VolumeDesired: api.ContainerStopped,
			Resolvable:    true,
		},
		{
			TargetDesired: api.ContainerCreated,
			VolumeDesired: api.ContainerZombie,
			Resolvable:    false,
		},
		{
			TargetDesired: api.ContainerRunning,
			VolumeDesired: api.ContainerStatusNone,
			Resolvable:    false,
		},
		{
			TargetDesired: api.ContainerRunning,
			VolumeDesired: api.ContainerCreated,
			Resolvable:    true,
		},
		{
			TargetDesired: api.ContainerRunning,
			VolumeDesired: api.ContainerRunning,
			Resolvable:    true,
		},
		{
			TargetDesired: api.ContainerRunning,
			VolumeDesired: api.ContainerStopped,
			Resolvable:    true,
		},
		{
			TargetDesired: api.ContainerRunning,
			VolumeDesired: api.ContainerZombie,
			Resolvable:    false,
		},
		{
			TargetDesired: api.ContainerStatusNone,
			Resolvable:    false,
		},
		{
			TargetDesired: api.ContainerStopped,
			Resolvable:    false,
		},
		{
			TargetDesired: api.ContainerZombie,
			Resolvable:    false,
		},
	}
	for _, tc := range testcases {
		t.Run(fmt.Sprintf("T:%s+V:%s", tc.TargetDesired.String(), tc.VolumeDesired.String()),
			assertCanResolve(volumeCanResolve, tc.TargetDesired, tc.VolumeDesired, tc.Resolvable))
	}
}

func TestVolumeIsResolved(t *testing.T) {
	testcases := []struct {
		TargetDesired api.ContainerStatus
		VolumeKnown   api.ContainerStatus
		Resolved      bool
	}{
		{
			TargetDesired: api.ContainerCreated,
			VolumeKnown:   api.ContainerStatusNone,
			Resolved:      false,
		},
		{
			TargetDesired: api.ContainerCreated,
			VolumeKnown:   api.ContainerCreated,
			Resolved:      true,
		},
		{
			TargetDesired: api.ContainerCreated,
			VolumeKnown:   api.ContainerRunning,
			Resolved:      true,
		},
		{
			TargetDesired: api.ContainerCreated,
			VolumeKnown:   api.ContainerStopped,
			Resolved:      true,
		},
		{
			TargetDesired: api.ContainerCreated,
			VolumeKnown:   api.ContainerZombie,
			Resolved:      false,
		},
		{
			TargetDesired: api.ContainerRunning,
			VolumeKnown:   api.ContainerStatusNone,
			Resolved:      false,
		},
		{
			TargetDesired: api.ContainerRunning,
			VolumeKnown:   api.ContainerCreated,
			Resolved:      true,
		},
		{
			TargetDesired: api.ContainerRunning,
			VolumeKnown:   api.ContainerRunning,
			Resolved:      true,
		},
		{
			TargetDesired: api.ContainerRunning,
			VolumeKnown:   api.ContainerStopped,
			Resolved:      true,
		},
		{
			TargetDesired: api.ContainerRunning,
			VolumeKnown:   api.ContainerZombie,
			Resolved:      false,
		},
		{
			TargetDesired: api.ContainerStatusNone,
			Resolved:      false,
		},
		{
			TargetDesired: api.ContainerStopped,
			Resolved:      false,
		},
		{
			TargetDesired: api.ContainerZombie,
			Resolved:      false,
		},
	}
	for _, tc := range testcases {
		t.Run(fmt.Sprintf("T:%s+V:%s", tc.TargetDesired.String(), tc.VolumeKnown.String()),
			assertResolved(volumeIsResolved, tc.TargetDesired, tc.VolumeKnown, tc.Resolved))
	}
}

func TestOnSteadyStateIsResolved(t *testing.T) {
	testcases := []struct {
		TargetDesired api.ContainerStatus
		RunKnown      api.ContainerStatus
		Resolved      bool
	}{
		{
			TargetDesired: api.ContainerStatusNone,
			Resolved:      false,
		},
		{
			TargetDesired: api.ContainerPulled,
			Resolved:      false,
		},
		{
			TargetDesired: api.ContainerCreated,
			RunKnown:      api.ContainerCreated,
			Resolved:      false,
		},
		{
			TargetDesired: api.ContainerCreated,
			RunKnown:      api.ContainerRunning,
			Resolved:      true,
		},
		{
			TargetDesired: api.ContainerCreated,
			RunKnown:      api.ContainerStopped,
			Resolved:      true,
		},
	}
	for _, tc := range testcases {
		t.Run(fmt.Sprintf("T:%s+R:%s", tc.TargetDesired.String(), tc.RunKnown.String()),
			assertResolved(onSteadyStateIsResolved, tc.TargetDesired, tc.RunKnown, tc.Resolved))
	}
}

func assertCanResolve(f func(target *api.Container, dep *api.Container) bool, targetDesired, depKnown api.ContainerStatus, expectedResolvable bool) func(t *testing.T) {
	return func(t *testing.T) {
		target := &api.Container{
			DesiredStatusUnsafe: targetDesired,
		}
		dep := &api.Container{
			DesiredStatusUnsafe: depKnown,
		}
		resolvable := f(target, dep)
		assert.Equal(t, expectedResolvable, resolvable)
	}
}

func assertResolved(f func(target *api.Container, dep *api.Container) bool, targetDesired, depKnown api.ContainerStatus, expectedResolved bool) func(t *testing.T) {
	return func(t *testing.T) {
		target := &api.Container{
			DesiredStatusUnsafe: targetDesired,
		}
		dep := &api.Container{
			KnownStatusUnsafe: depKnown,
		}
		resolved := f(target, dep)
		assert.Equal(t, expectedResolved, resolved)
	}
}

func TestTransitionDependenciesResolved(t *testing.T) {
	testcases := []struct {
		Name             string
		TargetKnown      api.ContainerStatus
		TargetDesired    api.ContainerStatus
		DependencyKnown  api.ContainerStatus
		DependentStatus  api.ContainerStatus
		SatisfiedStatus  api.ContainerStatus
		ExpectedResolved bool
	}{
		{
			Name:             "Nothing running, pull depends on running",
			TargetKnown:      api.ContainerStatusNone,
			TargetDesired:    api.ContainerRunning,
			DependencyKnown:  api.ContainerStatusNone,
			DependentStatus:  api.ContainerPulled,
			SatisfiedStatus:  api.ContainerRunning,
			ExpectedResolved: false,
		},
		{
			Name:             "Nothing running, pull depends on resources provisioned",
			TargetKnown:      api.ContainerStatusNone,
			TargetDesired:    api.ContainerRunning,
			DependencyKnown:  api.ContainerStatusNone,
			DependentStatus:  api.ContainerPulled,
			SatisfiedStatus:  api.ContainerResourcesProvisioned,
			ExpectedResolved: false,
		},
		{
			Name:             "Nothing running, create depends on running",
			TargetKnown:      api.ContainerStatusNone,
			TargetDesired:    api.ContainerRunning,
			DependencyKnown:  api.ContainerStatusNone,
			DependentStatus:  api.ContainerCreated,
			SatisfiedStatus:  api.ContainerRunning,
			ExpectedResolved: true,
		},
		{
			Name:             "Dependency created, pull depends on running",
			TargetKnown:      api.ContainerStatusNone,
			TargetDesired:    api.ContainerRunning,
			DependencyKnown:  api.ContainerCreated,
			DependentStatus:  api.ContainerPulled,
			SatisfiedStatus:  api.ContainerRunning,
			ExpectedResolved: false,
		},
		{
			Name:             "Dependency created, pull depends on resources provisioned",
			TargetKnown:      api.ContainerStatusNone,
			TargetDesired:    api.ContainerRunning,
			DependencyKnown:  api.ContainerCreated,
			DependentStatus:  api.ContainerPulled,
			SatisfiedStatus:  api.ContainerResourcesProvisioned,
			ExpectedResolved: false,
		},
		{
			Name:             "Dependency running, pull depends on running",
			TargetKnown:      api.ContainerStatusNone,
			TargetDesired:    api.ContainerRunning,
			DependencyKnown:  api.ContainerRunning,
			DependentStatus:  api.ContainerPulled,
			SatisfiedStatus:  api.ContainerRunning,
			ExpectedResolved: true,
		},
		{
			Name:             "Dependency running, pull depends on resources provisioned",
			TargetKnown:      api.ContainerStatusNone,
			TargetDesired:    api.ContainerRunning,
			DependencyKnown:  api.ContainerRunning,
			DependentStatus:  api.ContainerPulled,
			SatisfiedStatus:  api.ContainerResourcesProvisioned,
			ExpectedResolved: false,
		},
		{
			Name:             "Dependency resources provisioned, pull depends on resources provisioned",
			TargetKnown:      api.ContainerStatusNone,
			TargetDesired:    api.ContainerRunning,
			DependencyKnown:  api.ContainerResourcesProvisioned,
			DependentStatus:  api.ContainerPulled,
			SatisfiedStatus:  api.ContainerResourcesProvisioned,
			ExpectedResolved: true,
		},
		{
			Name:             "Dependency running, create depends on created",
			TargetKnown:      api.ContainerPulled,
			TargetDesired:    api.ContainerRunning,
			DependencyKnown:  api.ContainerRunning,
			DependentStatus:  api.ContainerCreated,
			SatisfiedStatus:  api.ContainerCreated,
			ExpectedResolved: true,
		},
		{
			Name:             "Target running, create depends on running",
			TargetKnown:      api.ContainerRunning,
			TargetDesired:    api.ContainerRunning,
			DependencyKnown:  api.ContainerRunning,
			DependentStatus:  api.ContainerRunning,
			SatisfiedStatus:  api.ContainerCreated,
			ExpectedResolved: true,
		},
		// Note: Not all possible situations are tested here.  The only situations tested here are ones that are
		// expected to reasonably happen at the time this code was written.  Other behavior is not expected to occur,
		// so it is not tested.
	}
	for _, tc := range testcases {
		t.Run(tc.Name, func(t *testing.T) {
			containerDependency := api.ContainerDependency{
				DependentStatus: tc.DependentStatus,
				SatisfiedStatus: tc.SatisfiedStatus,
			}
			target := &api.Container{
				KnownStatusUnsafe:   tc.TargetKnown,
				DesiredStatusUnsafe: tc.TargetDesired,
			}
			dep := &api.Container{
				KnownStatusUnsafe: tc.DependencyKnown,
			}
			resolved := resolvesContainerTransitionDependency(target, dep, containerDependency)
			assert.Equal(t, tc.ExpectedResolved, resolved)
		})
	}
}
