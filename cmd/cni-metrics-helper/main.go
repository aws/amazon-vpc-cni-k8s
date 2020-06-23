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
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/spf13/pflag"

	"github.com/aws/amazon-vpc-cni-k8s/cmd/cni-metrics-helper/metrics"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/publisher"
)

var log = logger.DefaultLogger()

type options struct {
	submitCW bool
	help     bool
}

func main() {
	options := &options{}
	flags := pflag.NewFlagSet("", pflag.ExitOnError)
	flags.AddGoFlagSet(flag.CommandLine)

	flags.BoolVar(&options.submitCW, "cloudwatch", true, "a bool")

	flags.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flags.PrintDefaults()
	}

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
		if strings.Compare(cwENV, "yes") == 0 || strings.Compare(cwENV, "true") == 0 {
			options.submitCW = true
		}
		if strings.Compare(cwENV, "no") == 0 || strings.Compare(cwENV, "false") == 0 {
			options.submitCW = false
		}
	}

	log.Infof("Starting CNIMetricsHelper. Sending metrics to CloudWatch: %v", options.submitCW)

	kubeClient, err := k8sapi.CreateKubeClient()
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	discoverController := k8sapi.NewController(kubeClient)
	go discoverController.DiscoverCNIK8SPods()

	var cw publisher.Publisher

	if options.submitCW {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cw, err = publisher.New(ctx)
		if err != nil {
			log.Fatalf("Failed to create publisher: %v", err)
		}
		go cw.Start()
		defer cw.Stop()
	}

	var cniMetric = metrics.CNIMetricsNew(kubeClient, cw, discoverController, options.submitCW)

	// metric loop
	var pullInterval = 30 // seconds
	for range time.Tick(time.Duration(pullInterval) * time.Second) {
		log.Info("Collecting metrics ...")
		metrics.Handler(cniMetric)
	}
}
