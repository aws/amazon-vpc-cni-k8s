#!/bin/bash
# Copyright 2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the
# "License"). You may not use this file except in compliance
#  with the License. A copy of the License is located at
#
#     http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is
# distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
# CONDITIONS OF ANY KIND, either express or implied. See the
# License for the specific language governing permissions and
# limitations under the License.
#
# This script wraps the mockgen tool and inserts licensing information.

mockgen.sh github.com/aws/amazon-ecs-agent/agent/engine TaskEngine,DockerClient,ImageManager mocks/engine_mocks.go

sed -e "s/engine\.//g" \
	-e 's|\s*engine "github.com/aws/amazon-ecs-agent/agent/engine"|REMOVETHISLINE|' \
	-e "s/mock_engine/engine/" \
	-e '/REMOVETHISLINE/d' \
	mocks/engine_mocks.go > engine_mocks.go

rm mocks/engine_mocks.go
