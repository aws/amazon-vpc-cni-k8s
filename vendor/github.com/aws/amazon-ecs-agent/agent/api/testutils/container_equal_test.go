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

package testutils

import (
	"fmt"
	"testing"

	. "github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/stretchr/testify/assert"
)

func TestContainerEqual(t *testing.T) {

	exitCodeContainer := func(p *int) Container {
		c := Container{}
		c.SetKnownExitCode(p)
		return c
	}

	testCases := []struct {
		lhs           Container
		rhs           Container
		shouldBeEqual bool
	}{
		// Equal Pairs
		{Container{Name: "name"}, Container{Name: "name"}, true},
		{Container{Image: "nginx"}, Container{Image: "nginx"}, true},
		{Container{Command: []string{"c"}}, Container{Command: []string{"c"}}, true},
		{Container{CPU: 1}, Container{CPU: 1}, true},
		{Container{Memory: 1}, Container{Memory: 1}, true},
		{Container{Links: []string{"1", "2"}}, Container{Links: []string{"1", "2"}}, true},
		{Container{Links: []string{"1", "2"}}, Container{Links: []string{"2", "1"}}, true},
		{Container{VolumesFrom: []VolumeFrom{{"1", false}, {"2", true}}}, Container{VolumesFrom: []VolumeFrom{{"1", false}, {"2", true}}}, true},
		{Container{VolumesFrom: []VolumeFrom{{"1", false}, {"2", true}}}, Container{VolumesFrom: []VolumeFrom{{"2", true}, {"1", false}}}, true},
		{Container{Ports: []PortBinding{{1, 2, "1", TransportProtocolTCP}}}, Container{Ports: []PortBinding{{1, 2, "1", TransportProtocolTCP}}}, true},
		{Container{Essential: true}, Container{Essential: true}, true},
		{Container{EntryPoint: nil}, Container{EntryPoint: nil}, true},
		{Container{EntryPoint: &[]string{"1", "2"}}, Container{EntryPoint: &[]string{"1", "2"}}, true},
		{Container{Environment: map[string]string{}}, Container{Environment: map[string]string{}}, true},
		{Container{Environment: map[string]string{"a": "b", "c": "d"}}, Container{Environment: map[string]string{"c": "d", "a": "b"}}, true},
		{Container{DesiredStatusUnsafe: ContainerRunning}, Container{DesiredStatusUnsafe: ContainerRunning}, true},
		{Container{AppliedStatus: ContainerRunning}, Container{AppliedStatus: ContainerRunning}, true},
		{Container{KnownStatusUnsafe: ContainerRunning}, Container{KnownStatusUnsafe: ContainerRunning}, true},
		{exitCodeContainer(aws.Int(1)), exitCodeContainer(aws.Int(1)), true},
		{exitCodeContainer(nil), exitCodeContainer(nil), true},
		// Unequal Pairs
		{Container{Name: "name"}, Container{Name: "名前"}, false},
		{Container{Image: "nginx"}, Container{Image: "えんじんえっくす"}, false},
		{Container{Command: []string{"c"}}, Container{Command: []string{"し"}}, false},
		{Container{Command: []string{"c", "b"}}, Container{Command: []string{"b", "c"}}, false},
		{Container{CPU: 1}, Container{CPU: 2e2}, false},
		{Container{Memory: 1}, Container{Memory: 2e2}, false},
		{Container{Links: []string{"1", "2"}}, Container{Links: []string{"1", "二"}}, false},
		{Container{VolumesFrom: []VolumeFrom{{"1", false}, {"2", true}}}, Container{VolumesFrom: []VolumeFrom{{"1", false}, {"二", false}}}, false},
		{Container{Ports: []PortBinding{{1, 2, "1", TransportProtocolTCP}}}, Container{Ports: []PortBinding{{1, 2, "二", TransportProtocolTCP}}}, false},
		{Container{Ports: []PortBinding{{1, 2, "1", TransportProtocolTCP}}}, Container{Ports: []PortBinding{{1, 22, "1", TransportProtocolTCP}}}, false},
		{Container{Ports: []PortBinding{{1, 2, "1", TransportProtocolTCP}}}, Container{Ports: []PortBinding{{1, 2, "1", TransportProtocolUDP}}}, false},
		{Container{Essential: true}, Container{Essential: false}, false},
		{Container{EntryPoint: nil}, Container{EntryPoint: &[]string{"nonnil"}}, false},
		{Container{EntryPoint: &[]string{"1", "2"}}, Container{EntryPoint: &[]string{"2", "1"}}, false},
		{Container{EntryPoint: &[]string{"1", "2"}}, Container{EntryPoint: &[]string{"1", "二"}}, false},
		{Container{Environment: map[string]string{"a": "b", "c": "d"}}, Container{Environment: map[string]string{"し": "d", "a": "b"}}, false},
		{Container{DesiredStatusUnsafe: ContainerRunning}, Container{DesiredStatusUnsafe: ContainerStopped}, false},
		{Container{AppliedStatus: ContainerRunning}, Container{AppliedStatus: ContainerStopped}, false},
		{Container{KnownStatusUnsafe: ContainerRunning}, Container{KnownStatusUnsafe: ContainerStopped}, false},
		{exitCodeContainer(aws.Int(0)), exitCodeContainer(aws.Int(42)), false},
		{exitCodeContainer(nil), exitCodeContainer(aws.Int(12)), false},
	}

	for index, tc := range testCases {
		t.Run(fmt.Sprintf("index %d expected %t", index, tc.shouldBeEqual), func(t *testing.T) {
			assert.Equal(t, ContainersEqual(&tc.lhs, &tc.rhs), tc.shouldBeEqual, "ContainersEqual not working as expected. Check index failure.")
			// Symetric
			assert.Equal(t, ContainersEqual(&tc.rhs, &tc.lhs), tc.shouldBeEqual, "Symetric equality check failed. Check index failure.")
		})
	}
}
