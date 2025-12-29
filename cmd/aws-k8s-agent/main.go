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
	"context"
	"os"
	"strconv"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/eventrecorder"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/version"
	"github.com/aws/amazon-vpc-cni-k8s/utils"
	metrics "github.com/aws/amazon-vpc-cni-k8s/utils/prometheusmetrics"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	appName = "aws-node"
	// metricsPort is the port for prometheus metrics
	metricsPort = 61678

	// Environment variable to disable the metrics endpoint on 61678
	envDisableMetrics = "DISABLE_METRICS"

	// Environment variable to disable the IPAMD introspection endpoint on 61679
	envDisableIntrospection = "DISABLE_INTROSPECTION"

	restCfgTimeout = 5 * time.Second
	pollInterval   = 5 * time.Second
	pollTimeout    = 30 * time.Second
)

func main() {
	os.Exit(_main())
}

// startBackgroundAPIServerCheck checks API connectivity in the background
func startBackgroundAPIServerCheck(ipamContext *ipamd.IPAMContext) {
	go func() {
		log := logger.Get()
		log.Info("Starting background API server connectivity check...")

		// Create a new client for API server check
		restCfg, err := k8sapi.GetRestConfig()
		if err != nil {
			log.Errorf("Failed to get REST config for background API check: %v", err)
			return
		}
		restCfg.Timeout = restCfgTimeout
		clientSet, err := kubernetes.NewForConfig(restCfg)
		if err != nil {
			log.Errorf("Failed to create k8s client for background API check: %v", err)
			return
		}

		// Keep checking until connection is established
		wait.PollUntilContextCancel(context.Background(), pollInterval, true, func(ctx context.Context) (bool, error) {
			version, err := clientSet.Discovery().ServerVersion()
			if err == nil {
				log.Infof("API server connectivity established in background! Cluster Version is: %s", version.GitVersion)

				// Update IPAM context with new API server connectivity
				ipamContext.SetAPIServerConnectivity(true)

				// Exit the goroutine after successful connection
				log.Info("Background API server check completed successfully")
				return true, nil
			}

			log.Debugf("Still waiting for API server connectivity in background: %v", err)
			return false, nil
		})
	}()
}

func _main() int {
	// Start measuring full startup duration
	startupStartTime := time.Now()

	// Do not add anything before initializing logger
	log := logger.Get()

	// Initialize controller-runtime logger
	logConfig := logger.LoadLogConfig()
	ctrl.SetLogger(logConfig.NewControllerRuntimeLogger())

	log.Infof("Starting L-IPAMD %s  ...", version.Version)
	version.RegisterMetric()

	enabledPodEni := ipamd.EnablePodENI()
	enabledCustomNetwork := ipamd.UseCustomNetworkCfg()
	enabledPodAnnotation := ipamd.EnablePodIPAnnotation()
	withApiServer := false
	// Check API Server Connectivity
	if enabledPodEni || enabledCustomNetwork || enabledPodAnnotation {
		log.Info("SGP, custom networking or pod annotation feature is in use, waiting for API server connectivity to start IPAMD")
		if err := k8sapi.CheckAPIServerConnectivity(); err != nil {
			log.Errorf("Failed to check API server connectivity: %s", err)
			// Record failed startup
			metrics.IpamdStartupDuration.WithLabelValues("false", strconv.FormatBool(withApiServer), "api_server_connectivity").Observe(time.Since(startupStartTime).Seconds())
			return 1
		} else {
			log.Info("API server connectivity established.")
			withApiServer = true
		}
	} else {
		log.Infof("Waiting to connect API server for upto %s...", pollTimeout)
		// Try a quick check first
		if err := k8sapi.CheckAPIServerConnectivityWithTimeout(pollInterval, pollTimeout); err != nil {
			log.Warn("Proceeding without API server connectivity, will run background API server connectivity check")
			withApiServer = false
		} else {
			log.Info("API server connectivity established.")
			withApiServer = true
		}
	}
	// Create Kubernetes client for API server requests
	k8sClient, err := k8sapi.CreateKubeClient(appName)
	if err != nil {
		log.Errorf("Failed to create kube client: %s", err)
	}
	// Create EventRecorder for use by IPAMD
	if err := eventrecorder.Init(k8sClient, withApiServer); err != nil {
		log.Errorf("Failed to create event recorder: %s", err)
		log.Warn("Skipping event recorder initialization")
	}
	// Measure node initialization duration
	IPAMDNodeInitStartTime := time.Now()
	ipamContext, err := ipamd.New(k8sClient, withApiServer)
	IPAMDNodeInitDuration := time.Since(IPAMDNodeInitStartTime).Seconds()

	if err != nil {
		log.Errorf("Initialization failure: %v", err)
		// Record failed IPAMD initialization and failed startup
		metrics.IpamdNodeInitDuration.WithLabelValues("false").Observe(IPAMDNodeInitDuration)
		metrics.IpamdStartupDuration.WithLabelValues("false", strconv.FormatBool(withApiServer), "node_initialization").Observe(time.Since(startupStartTime).Seconds())
		return 1
	}

	// Record successful AWS initialization
	metrics.IpamdNodeInitDuration.WithLabelValues("true").Observe(IPAMDNodeInitDuration)

	// If not connected to API server yet, start background checks
	if !withApiServer {
		startBackgroundAPIServerCheck(ipamContext)
	}

	// Pool manager
	go ipamContext.StartNodeIPPoolManager(context.Background())

	if !utils.GetBoolAsStringEnvVar(envDisableMetrics, false) {
		// Prometheus metrics
		go metrics.ServeMetrics(metricsPort)
	}

	// CNI introspection endpoints
	if !utils.GetBoolAsStringEnvVar(envDisableIntrospection, false) {
		go ipamContext.ServeIntrospection()
	}

	// Record successful startup duration before the blocking RPC handler call
	metrics.IpamdStartupDuration.WithLabelValues("true", strconv.FormatBool(withApiServer), "").Observe(time.Since(startupStartTime).Seconds())

	// Start the RPC listener (this is a blocking call)
	err = ipamContext.RunRPCHandler(version.Version)
	if err != nil {
		log.Errorf("Failed to set up gRPC handler: %v", err)
		return 1
	}
	return 0
}
