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

// CNI metrics helper binary publishing metrics to CloudWatch
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/spf13/pflag"

	"github.com/aws/amazon-vpc-cni-k8s/cmd/cni-metrics-helper/metrics"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/publisher"
	"github.com/aws/amazon-vpc-cni-k8s/utils/prometheusmetrics"
)

const (
	appName = "cni-metrics-helper"

	// metricsPort is the port for prometheus metrics
	metricsPort = 61681

	// Environment variable to enable the metrics endpoint on 61681
	envEnablePrometheusMetrics = "USE_PROMETHEUS"
)

var (
	prometheusRegistered = false
)

type options struct {
	submitCW         bool
	help             bool
	submitPrometheus bool
}

func prometheusRegister() {
	if !prometheusRegistered {
		prometheusmetrics.PrometheusRegister()
		prometheusRegistered = true
	}
}

func main() {
	// Do not add anything before initializing logger
	logLevel := logger.GetLogLevel()
	logConfig := logger.Configuration{
		LogLevel:    logLevel,
		LogLocation: "stdout",
	}
	log := logger.New(&logConfig)
	options := &options{}
	flags := pflag.NewFlagSet("", pflag.ExitOnError)
	flags.AddGoFlagSet(flag.CommandLine)
	flags.BoolVar(&options.submitCW, "cloudwatch", true, "a bool")
	flags.BoolVar(&options.submitPrometheus, "prometheus metrics", false, "a bool")

	flags.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flags.PrintDefaults()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := flags.Parse(os.Args)
	if err != nil {
		log.Fatalf("Error on parsing parameters: %s", err)
	}

	err = flag.CommandLine.Parse([]string{})
	if err != nil {
		log.Fatalf("Error on parsing commandline: %s", err)
	}

	if options.help {
		flags.Usage()
		os.Exit(1)
	}

	cwENV, found := os.LookupEnv("USE_CLOUDWATCH")
	if found {
		cwENV = strings.ToLower(cwENV)
		if strings.Compare(cwENV, "yes") == 0 || strings.Compare(cwENV, "true") == 0 {
			options.submitCW = true
		}
		if strings.Compare(cwENV, "no") == 0 || strings.Compare(cwENV, "false") == 0 {
			options.submitCW = false
		}
	}

	prometheusENV, found := os.LookupEnv(envEnablePrometheusMetrics)
	if found {
		prometheusENV = strings.ToLower(prometheusENV)
		if strings.Compare(prometheusENV, "yes") == 0 || strings.Compare(prometheusENV, "true") == 0 {
			options.submitPrometheus = true
			prometheusRegister()
		}
		if strings.Compare(prometheusENV, "no") == 0 || strings.Compare(prometheusENV, "false") == 0 {
			options.submitPrometheus = false
		}
	}

	metricUpdateIntervalEnv, found := os.LookupEnv("METRIC_UPDATE_INTERVAL")
	if !found {
		metricUpdateIntervalEnv = "30"
	}
	metricUpdateInterval, err := strconv.Atoi(metricUpdateIntervalEnv)
	if err != nil {
		log.Fatalf("METRIC_UPDATE_INTERVAL (%s) format invalid. Integer required. Expecting seconds: %s", metricUpdateIntervalEnv, err)
		os.Exit(1)
	}

	// Fetch region, if using IRSA it be will auto injected as env variable in pod spec
	// If not found then it will be empty, in which case we will try to fetch it from IMDS (existing approach)
	// This can also mean that Cx is not using IRSA and we shouldn't enforce IRSA requirement
	region, _ := os.LookupEnv("AWS_REGION")

	// should be name/identifier for the cluster if specified
	clusterID, _ := os.LookupEnv("AWS_CLUSTER_ID")

	log.Infof("Starting CNIMetricsHelper. Sending metrics to CloudWatch: %v, Prometheus: %v, LogLevel %s, metricUpdateInterval %d", options.submitCW, options.submitPrometheus, logConfig.LogLevel, metricUpdateInterval)

	clientSet, err := k8sapi.GetKubeClientSet()
	if err != nil {
		log.Fatalf("Error Fetching Kubernetes Client: %s", err)
		os.Exit(1)
	}

	k8sClient, err := k8sapi.CreateKubeClient(appName)
	if err != nil {
		log.Fatalf("Error creating Kubernetes Client: %s", err)
		os.Exit(1)
	}

	var cw publisher.Publisher

	if options.submitCW {
		cw, err = publisher.New(ctx, region, clusterID, log)
		if err != nil {
			log.Fatalf("Failed to create publisher: %v", err)
		}
		publishInterval := metricUpdateInterval * 2
		go cw.Start(publishInterval)
		defer cw.Stop()
	}

	if options.submitPrometheus {
		// Start prometheus server
		go prometheusmetrics.ServeMetrics(metricsPort)
	}

	podWatcher := metrics.NewDefaultPodWatcher(k8sClient, log)
	var cniMetric = metrics.CNIMetricsNew(clientSet, cw, options.submitCW, options.submitPrometheus, log, podWatcher)

	// metric loop
	for range time.Tick(time.Duration(metricUpdateInterval) * time.Second) {
		log.Info("Collecting metrics ...")
		metrics.Handler(ctx, cniMetric)
	}
}
