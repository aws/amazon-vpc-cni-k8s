// Copyright 2014-2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

/*
Package config handles loading configuration data, warning on missing data,
and setting sane defaults.

Configuration Sources

Configuration data is loaded from two sources currently: the environment and
a json config file.

Environment Variables:

The environment variables from which configuration values are loaded are
documented in the README file which can be found at
https://github.com/aws/amazon-ecs-agent#environment-variables.

Config file:

The config file will be loaded from the path stored in the environment key
ECS_AGENT_CONFIG_FILE_PATH. It must be a JSON file of the format described
by the "Config" struct below.
*/
package config
