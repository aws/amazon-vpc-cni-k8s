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
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
)

const (
	MTU_CHECK         = "mtu"
	VETH_PREFIX_CHECK = "veth"
)

type MTU_CONFIG struct {
	ExpectedVal int
}

func main() {
	validate := ""
	config := ""

	flag.StringVar(&validate, "validate", "", "name of the test, valid values: mtu, veth")
	flag.StringVar(&config, "config", "", "Base64 encoded json object for the given test")
	flag.Parse()

	decoded, err := base64.StdEncoding.DecodeString(config)
	if err != nil {
		log.Fatalf("failed to descode base64 string %s: %v", config, err)
	}

	switch validate {
	case MTU_CHECK:
		validateMTUValue(decoded)
	case VETH_PREFIX_CHECK:
		validateVethPairCount(decoded)
	default:
		fmt.Println("Invalid test-name choice")
	}
}

func validateVethPairCount(config []byte) {
	var podNetworkingValidationInput input.PodNetworkingValidationInput
	err := json.Unmarshal(config, &podNetworkingValidationInput)
	if err != nil {
		log.Fatalf("failed to unmarshall json string %s: %v", config, err)
	}

	log.Printf("podNetworkingValidationInput: %+v\n", podNetworkingValidationInput)

	if podNetworkingValidationInput.VethPrefix == "" {
		podNetworkingValidationInput.VethPrefix = "eni"
	}

	actual := 0
	vethPairs := make(map[string]bool, len(podNetworkingValidationInput.PodList))

	for _, pod := range podNetworkingValidationInput.PodList {
		// Get the veth pair for pod in host network namespace
		hostVethName := getHostVethPairName(pod, podNetworkingValidationInput.VethPrefix)
		log.Printf("hostVethName: %v\n", hostVethName)
		vethPairs[hostVethName] = false
	}

	interfaces, err := net.Interfaces()
	if err != nil {
		log.Fatal(err)
	}

	for _, ni := range interfaces {
		if checked, ok := vethPairs[ni.Name]; ok {
			if !checked {
				actual++
			}
		}
	}

	if actual == len(podNetworkingValidationInput.PodList) {
		log.Println("Veth Check: Passed")
	} else {
		log.Println("Veth Check: Failed")
		log.Println("Missing Veth pairs")
		for name, val := range vethPairs {
			if !val {
				log.Println(name)
			}
		}
	}
}

func validateMTUValue(config []byte) {
	mtuConfig := MTU_CONFIG{}
	err := json.Unmarshal(config, &mtuConfig)

	if err != nil {
		log.Fatalf("failed to unmarshall json string %s: %v", config, err)
	}

	log.Printf("MTU_CONFIG: %+v\n", mtuConfig)

	if mtuConfig.ExpectedVal == 0 {
		mtuConfig.ExpectedVal = 9001
	}

	interfaces, err := net.Interfaces()
	if err != nil {
		log.Fatal(err)
	}

	ok := false
	failedInterfaces := make([]string, 0)
	for _, ni := range interfaces {
		if strings.HasPrefix(ni.Name, "eth") {
			if ni.MTU != mtuConfig.ExpectedVal {
				failedInterfaces = append(failedInterfaces, ni.Name)
			}
			ok = true
		}
	}
	if !ok {
		log.Println("MTU Check: Failed")
		log.Println("Could not find eth interface, check ifconfig on the node")
	} else {
		if len(failedInterfaces) != 0 {
			log.Println("MTU Check: Failed")
			log.Printf("Failed Interfaces: %+v\n", failedInterfaces)
		} else {
			log.Println("MTU Check: Passed")
		}
	}
}

func getHostVethPairName(input input.Pod, vethPrefix string) string {
	h := sha1.New()
	h.Write([]byte(fmt.Sprintf("%s.%s", input.PodNamespace, input.PodName)))
	return fmt.Sprintf("%s%s", vethPrefix, hex.EncodeToString(h.Sum(nil))[:11])
}
