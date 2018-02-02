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

package args

import "flag"

const (
	versionUsage             = "Print the agent version information and exit"
	logLevelUsage            = "Loglevel: [<crit>|<error>|<warn>|<info>|<debug>]"
	ecsAttributesUsage       = "Print the Agent's ECS Attributes based on its environment"
	acceptInsecureCertUsage  = "Disable SSL certificate verification. We do not recommend setting this option"
	licenseUsage             = "Print the LICENSE and NOTICE files and exit"
	blacholeEC2MetadataUsage = "Blackhole the EC2 Metadata requests. Setting this option can cause the ECS Agent to fail to work properly.  We do not recommend setting this option"
	windowsServiceUsage      = "Run the ECS agent as a Windows Service"

	versionFlagName              = "version"
	logLevelFlagName             = "loglevel"
	ecsAttributesFlagName        = "ecs-attributes"
	acceptInsecureCertFlagName   = "k"
	licenseFlagName              = "license"
	blackholeEC2MetadataFlagName = "blackhole-ec2-metadata"
	windowsServiceFlagName       = "windows-service"
)

// Args wraps various ECS Agent arguments
type Args struct {
	// Version indicates if the version information should be printed
	Version *bool
	// LogLevel represents the ECS Agent's log level
	LogLevel *string
	// AcceptInsecureCert indicates if SSL certificate verification
	// should be disabled
	AcceptInsecureCert *bool
	// License indicates if the license information should be printed
	License *bool
	// BlackholeEC2Metadata indicates if EC2 Metadata requests should be
	// blackholed
	BlackholeEC2Metadata *bool
	// ECSAttributes indicates that the agent should print its attributes
	ECSAttributes *bool
	// WindowsService indicates that the agent should run as a Windows service
	WindowsService *bool
}

// New creates a new Args object from the argument list
func New(arguments []string) (*Args, error) {
	flagset := flag.NewFlagSet("Amazon ECS Agent", flag.ContinueOnError)

	args := &Args{
		Version:              flagset.Bool(versionFlagName, false, versionUsage),
		LogLevel:             flagset.String(logLevelFlagName, "", logLevelUsage),
		AcceptInsecureCert:   flagset.Bool(acceptInsecureCertFlagName, false, acceptInsecureCertUsage),
		License:              flagset.Bool(licenseFlagName, false, licenseUsage),
		BlackholeEC2Metadata: flagset.Bool(blackholeEC2MetadataFlagName, false, blacholeEC2MetadataUsage),
		ECSAttributes:        flagset.Bool(ecsAttributesFlagName, false, ecsAttributesUsage),
		WindowsService:       flagset.Bool(windowsServiceFlagName, false, windowsServiceUsage),
	}

	err := flagset.Parse(arguments)
	if err != nil {
		return nil, err
	}

	return args, nil
}
