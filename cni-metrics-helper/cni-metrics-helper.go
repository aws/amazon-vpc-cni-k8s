package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/spf13/pflag"

	"github.com/aws/amazon-vpc-cni-k8s/cni-metrics-helper/metrics"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/publisher"
)

type options struct {
	kubeconfig   string
	pullInterval int
	pullCNI      bool
	submitCW     bool
	help         bool
}

func main() {

	options := &options{}
	flags := pflag.NewFlagSet("", pflag.ExitOnError)
	// add glog flags
	flags.AddGoFlagSet(flag.CommandLine)
	flags.Lookup("logtostderr").Value.Set("true")
	flags.Lookup("logtostderr").DefValue = "true"
	flags.Lookup("logtostderr").NoOptDefVal = "true"
	flags.BoolVar(&options.submitCW, "cloudwatch", true, "a bool")

	flags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flags.PrintDefaults()
	}

	err := flags.Parse(os.Args)
	if err != nil {
		glog.Fatalf("Error on parsing parameters: %s", err)
	}

	flag.CommandLine.Parse([]string{})

	if options.help {
		flags.Usage()
		os.Exit(1)
	}

	cwENV, found := os.LookupEnv("USE_CLOUDWATCH")
	if found {
		if strings.Compare(cwENV, "yes") == 0 {
			options.submitCW = true
		}
		if strings.Compare(cwENV, "no") == 0 {
			options.submitCW = false
		}
	}

	glog.Infof("Starting CNIMetricsHelper, cloudwatch: %v...", options.submitCW)

	kubeClient, err := k8sapi.CreateKubeClient("", "")
	if err != nil {
		glog.Errorf("Failed to create client: %v", err)
		os.Exit(1)
	}

	discoverController := k8sapi.NewController(kubeClient)
	go discoverController.DiscoverK8SPods()

	var cw publisher.Publisher

	if options.submitCW {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cw, err = publisher.New(ctx)
		if err != nil {
			glog.Errorf("Failed to create publisher: %v", err)
			os.Exit(1)
		}
		go cw.Start()
		defer cw.Stop()
	}

	var cniMetric *metrics.CNIMetricsTarget
	cniMetric = metrics.CNIMetricsNew(kubeClient, cw, discoverController, options.submitCW)

	// metric loop
	var pullInterval = 30 // seconds
	for range time.Tick(time.Duration(pullInterval) * time.Second) {
		glog.Info("Collecting metrics ...")

		metrics.Hdlr(cniMetric)
	}
}
