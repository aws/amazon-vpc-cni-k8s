// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package main

import (
	"os"

	"github.com/aws/amazon-ecs-cni-plugins/pkg/logger"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd"
	log "github.com/cihub/seelog"
)

const (
	defaultLogFilePath = "/host/var/log/aws-routed-eni/ipamd.log"
	version            = "0.1"
)

func main() {
	defer log.Flush()
	logger.SetupLogger(logger.GetLogFileLocation(defaultLogFilePath))

	log.Infof("Starting L-IPAMD %s  ...", version)
	aws_k8s_agent, err := ipamd.New()

	if err != nil {
		log.Error("initialization failure", err)
		os.Exit(1)
	}

	go aws_k8s_agent.StartNodeIPPoolManager()
	aws_k8s_agent.RunRPCHandler()
}
