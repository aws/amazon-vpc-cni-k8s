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
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"github.com/aws/amazon-vpc-cni-k8s/test/agent/cmd/networking/tester"
	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
)

// TODO: Instead of passing the list of pods, get the pods from API Server so this agent can run as DS
// TODO: Export metrics via Prometheus for debugging and analysis purposes
func main() {
	var podNetworkingValidationInput input.PodNetworkingValidationInput
	var podNetworkingValidationInputString string
	var shouldTestSetup bool
	var shouldTestCleanup bool
	// per pod security group
	var ppsg bool

	flag.StringVar(&podNetworkingValidationInputString, "pod-networking-validation-input", "", "json string containing the array of pods whose networking needs to be validated")
	flag.BoolVar(&shouldTestCleanup, "test-cleanup", false, "bool flag when set to true tests that networking is teared down after pod has been deleted")
	flag.BoolVar(&shouldTestSetup, "test-setup", false, "bool flag when set to true tests the networking is setup correctly after pod is running")
	flag.BoolVar(&ppsg, "test-ppsg", false, "bool flag when set to true tests the networking setup for pods using security groups")

	flag.Parse()

	if shouldTestCleanup && shouldTestSetup {
		log.Fatal("can only test setup or cleanup at one time")
	}

	err := json.Unmarshal([]byte(podNetworkingValidationInputString), &podNetworkingValidationInput)
	if err != nil {
		log.Fatalf("failed to unmarshall json string %s: %v", podNetworkingValidationInputString, err)
	}

	log.Printf("list of pod against which test will be run %v", podNetworkingValidationInput.PodList)

	if ppsg && shouldTestSetup {
		log.Print("testing networking is setup for pods using security groups")
		err := tester.TestNetworkingSetupForPodsUsingSecurityGroup(podNetworkingValidationInput)
		if err != nil {
			log.Fatalf("found 1 or more pod setup validation failure: %v", err)
		}
	} else if ppsg && shouldTestCleanup {
		log.Print("testing networking is teared down for pods using security groups")
		err := tester.TestNetworkTearedDownForPodsUsingSecurityGroup(podNetworkingValidationInput)
		if err != nil {
			log.Fatalf("found 1 or more pod setup validation failure: %v", err)
		}
	} else if !ppsg && shouldTestSetup {
		log.Print("testing networking is setup for regular pods")
		err := tester.TestNetworkingSetupForRegularPod(podNetworkingValidationInput)
		if err != nil {
			log.Fatalf("found 1 or more pod setup validation failure: %v", err)
		}
	} else {
		log.Print("testing network is teared down for regular pods")
		err := tester.TestNetworkTearedDownForRegularPods(podNetworkingValidationInput)
		if len(err) > 0 {
			var errs bytes.Buffer
			for _, e := range err {
				if errs.Len() > 0 {
					fmt.Fprint(&errs, " / ")
				}
				fmt.Fprint(&errs, e.Error())
			}
			log.Fatalf("found 1 or more pod teardown validation failure: %s", errs.String())
		}
	}
}
