// +build integration
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

package app

import (
	"context"
	"os"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/sighandlers/exitcodes"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/stretchr/testify/assert"
)

func TestNewAgent(t *testing.T) {
	ctx := context.TODO()
	os.Setenv("AWS_DEFAULT_REGION", "us-west-2")
	defer os.Unsetenv("AWS_DEFAULT_REGION")

	agent, err := newAgent(ctx, true, aws.Bool(true))

	assert.NoError(t, err)
	// printVersion should ensure that agent's cfg is set with
	// valid values and not panic
	assert.Equal(t, exitcodes.ExitSuccess, agent.printVersion())
}
