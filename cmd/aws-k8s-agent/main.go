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
	"os"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/eventrecorder"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/version"
	"github.com/aws/amazon-vpc-cni-k8s/utils"
	metrics "github.com/aws/amazon-vpc-cni-k8s/utils/prometheusmetrics"
)

const (
	appName = "aws-node"
	// metricsPort is the port for prometheus metrics
	metricsPort = 61678

	// Environment variable to disable the metrics endpoint on 61678
	envDisableMetrics = "DISABLE_METRICS"

	// Environment variable to disable the IPAMD introspection endpoint on 61679
	envDisableIntrospection = "DISABLE_INTROSPECTION"
)

func main() {
	os.Exit(_main())
}

func _main() int {
	// Do not add anything before initializing logger
	log := logger.Get()

	log.Infof("Starting L-IPAMD %s  ...", version.Version)
	version.RegisterMetric()

	// Check API Server Connectivity
	if err := k8sapi.CheckAPIServerConnectivity(); err != nil {
		log.Errorf("Failed to check API server connectivity: %s", err)
		return 1
	}

	// Create Kubernetes client for API server requests
	k8sClient, err := k8sapi.CreateKubeClient(appName)
	if err != nil {
		log.Errorf("Failed to create kube client: %s", err)
		return 1
	}

	// Create EventRecorder for use by IPAMD
	if err := eventrecorder.Init(k8sClient); err != nil {
		log.Errorf("Failed to create event recorder: %s", err)
		return 1
	}

	ipamContext, err := ipamd.New(k8sClient)
	if err != nil {
		log.Errorf("Initialization failure: %v", err)
		return 1
	}

	// Pool manager
	go ipamContext.StartNodeIPPoolManager()

	// Pod IP allocation reconcile loop to clear up dangling pod IPs.
	go ipamContext.PodIPAllocationReconcileLoop()

	if !utils.GetBoolAsStringEnvVar(envDisableMetrics, false) {
		// Prometheus metrics
		go metrics.ServeMetrics(metricsPort)
	}

	// CNI introspection endpoints
	if !utils.GetBoolAsStringEnvVar(envDisableIntrospection, false) {
		go ipamContext.ServeIntrospection()
	}

	// Start the RPC listener
	err = ipamContext.RunRPCHandler(version.Version)
	if err != nil {
		log.Errorf("Failed to set up gRPC handler: %v", err)
		return 1
	}
	return 0
}
