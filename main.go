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
	"io"
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

	// Copy the CNI plugin and config. This will mark the node as Ready.
	log.Info("Copying /app/aws-cni to /host/opt/cni/bin/aws-cni")
	err = copyFileContents("/app/aws-cni", "/host/opt/cni/bin/aws-cni")
	if err != nil {
		log.Errorf("Failed to copy aws-cni: %v", err)
		return 1
	}

	log.Info("Copying /app/10-aws.conflist to /host/etc/cni/net.d/10-aws.conflist")
	err = copyFileContents("/app/10-aws.conflist", "/host/etc/cni/net.d/10-aws.conflist")
	if err != nil {
		log.Errorf("Failed to copy 10-aws.conflist: %v", err)
		return 1
	}

	// Start the RPC listener
	err = ipamContext.RunRPCHandler()
	if err != nil {
		log.Errorf("Failed to set up gRPC handler: %v", err)
		return 1
	}
	return 0
}

// copyFileContents copies a file
func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		e := out.Close()
		if err == nil {
			err = e
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	err = out.Sync()
	if err != nil {
		return err
	}
	si, err := os.Stat(src)
	if err != nil {
		return err
	}
	err = os.Chmod(dst, si.Mode())
	if err != nil {
		return err
	}
	log.Debugf("Copied file from %q to %q", src, dst)
	return err
}
