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

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd"
	log "github.com/cihub/seelog"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/eniconfig"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
)

const (
	defaultLogFilePath = "/host/var/log/aws-routed-eni/ipamd.log"
)

var (
	version string
)

func main() {
	os.Exit(_main())
}

func _main() int {
	defer log.Flush()
	logger.SetupLogger(logger.GetLogFileLocation(defaultLogFilePath))

	log.Infof("Starting L-IPAMD %s  ...", version)

	kubeClient, err := k8sapi.CreateKubeClient()
	if err != nil {
		log.Errorf("Failed to create client: %v", err)
		return 1
	}

	discoverController := k8sapi.NewController(kubeClient)
	go discoverController.DiscoverK8SPods()

	eniConfigController := eniconfig.NewENIConfigController()
	if ipamd.UseCustomNetworkCfg() {
		go eniConfigController.Start()
	}

	awsK8sAgent, err := ipamd.New(discoverController, eniConfigController)

	if err != nil {
		log.Error("Initialization failure ", err)
		return 1
	}

	// Pool manager
	go awsK8sAgent.StartNodeIPPoolManager()

	// Prometheus metrics
	go awsK8sAgent.ServeMetrics()

	// CNI introspection endpoints
	go awsK8sAgent.ServeIntrospection()

	err = awsK8sAgent.RunRPCHandler()
	if err != nil {
		log.Error("Failed to set up gRPC handler ", err)
		return 1
	}

	return 0
}
