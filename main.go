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
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	log "github.com/cihub/seelog"
)

const (
	defaultLogFilePath = "/host/var/log/aws-routed-eni/ipamd.log"
	version            = "0.2.3"
)

func main() {
	maxPods := flag.Bool("max-pods", false, "Display the table showing how many pods can be run on each instance type")
	versionFlag := flag.Bool("version", false, "Print the version number and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Println(version)
		os.Exit(0)
	}

	if *maxPods {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "    ")
		err := enc.Encode(awsutils.MaxPods())
		if err != nil {
			log.Errorf("Error serializing instance IPs", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	logger.SetupLogger(logger.GetLogFileLocation(defaultLogFilePath))

	log.Infof("Starting L-IPAMD %s  ...", version)

	kubeClient, err := k8sapi.CreateKubeClient("", "")
	if err != nil {
		log.Errorf("Failed to create client: %v", err)
		log.Flush()
		os.Exit(1)
	}

	discoverController := k8sapi.NewController(kubeClient)
	go discoverController.DiscoverK8SPods()

	aws_k8s_agent, err := ipamd.New(discoverController)

	if err != nil {
		log.Error("initialization failure", err)
		log.Flush()
		os.Exit(1)
	}

	go aws_k8s_agent.StartNodeIPPoolManager()
	go aws_k8s_agent.SetupHTTP()
	aws_k8s_agent.RunRPCHandler()
}
