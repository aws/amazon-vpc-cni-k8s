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
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd"
	log "github.com/sirupsen/logrus"
)

var (
	// Version should be replaced by the makefile
	Version = "0.1.3"
	// GitVersion should be replaced by the makefile
	GitVersion = ""
)

func main() {
	if os.Getenv("CNI_LOGLEVEL") != "" {
		log.SetLevel(log.DebugLevel)
	}
	log.Infof("Starting L-IPAMD %v %v  ...", Version, fmt.Sprintf("(%v)", GitVersion))

	ipamd, err := ipamd.New()
	if err != nil {
		log.Error("Could not start L-IPAMD: ", err)
		os.Exit(1)
	}

	go ipamd.StartNodeIPPoolManager()
	go ipamd.SetupHTTP()
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGTERM)
	go func() {
		sig := <-signalChannel
		switch sig {
		case syscall.SIGTERM:
			log.Infof("Received SIGTERM. Exiting...")
			os.Exit(2)
		}
	}()
	if err = ipamd.RunRPCHandler(); err != nil {
		log.Error("Could not start L-IPAMD rpc: ", err)
		os.Exit(1)
	}
}
