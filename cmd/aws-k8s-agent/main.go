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

// The aws-node ipam daemon binary
package main

import (
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"os"
)

var version string

func main() {
	os.Exit(_main())
}

func _main() int {
	//Do not add anything before initializing logger
	logConfig := logger.Configuration{
		LogLevel:    logger.GetLogLevel(),
		LogLocation: logger.GetLogLocation(),
	}
	log := logger.New(&logConfig)

	log.Infof("Starting L-IPAMD %s  ...", version)

	//Check API Server Connectivity
	if k8sapi.CheckAPIServerConnectivity() != nil{
		return 1
	}

	standaloneK8SClient, k8sClient, err := k8sapi.CreateKubeClients()
	if err != nil{
		return 1
	}

	ipamContext, err := ipamd.New(standaloneK8SClient, k8sClient)

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
	err = ipamContext.RunRPCHandler(version)
	if err != nil {
		log.Errorf("Failed to set up gRPC handler: %v", err)
		return 1
	}
	return 0
}
