// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
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

	"github.com/aws/amazon-vpc-cni-k8s/pkg/eniconfig"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)

const binaryName = "ipamd"

var version string

func main() {
	os.Exit(_main())
}

func _main() int {
	//Do not add anything before initializing logger
	logConfig := logger.Configuration{
		BinaryName: binaryName,
	}
	log := logger.New(&logConfig)

	log.Infof("Starting L-IPAMD %s  ...", version)

	kubeClient, err := k8sapi.CreateKubeClient()
	if err != nil {
		log.Errorf("Failed to create client: %v", err)
		return 1
	}

	discoverController := k8sapi.NewController(kubeClient)
	go discoverController.DiscoverLocalK8SPods()

	eniConfigController := eniconfig.NewENIConfigController()
	if ipamd.UseCustomNetworkCfg() {
		go eniConfigController.Start()
	}

	ipamContext, err := ipamd.New(discoverController, eniConfigController)

	if err != nil {
		log.Errorf("Initialization failure: %v", err)
		return 1
	}

	// Pool manager
	go ipamContext.StartNodeIPPoolManager()

	// Prometheus metrics
	go ipamContext.ServeMetrics()

	// CNI introspection endpoints
	go ipamContext.ServeIntrospection()

	// Start the RPC listener
	err = ipamContext.RunRPCHandler()
	if err != nil {
		log.Errorf("Failed to set up gRPC handler: %v", err)
		return 1
	}
	return 0
}
