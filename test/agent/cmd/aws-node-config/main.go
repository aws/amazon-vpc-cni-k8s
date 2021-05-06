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
	"flag"
	"fmt"
	"log"
	"net"
	"strings"
)

const (
	MTU_CHECK         = "mtu"
	VETH_PREFIX_CHECK = "veth"
)

func main() {
	validate := ""
	prefix := "eni"
	vethCount := 1
	mtuVal := 9001

	flag.StringVar(&validate, "validate", "", "name of the test, valid values: mtu, veth")
	flag.StringVar(&prefix, "prefix", "eni", "veth prefix to be used, specify when validating veth")
	flag.IntVar(&vethCount, "veth-count", 1, "Number of expected veth links to have the new prefix. specify when validating veth")
	flag.IntVar(&mtuVal, "mtu-val", 9001, "Expected MTU values, specify when validating mtu")
	flag.Parse()

	switch validate {
	case MTU_CHECK:
		validateMTUValue(mtuVal)
	case VETH_PREFIX_CHECK:
		validateVethPairCount(prefix, vethCount)
	default:
		fmt.Println("Invalid test-name choice")
	}
}

func validateVethPairCount(prefix string, expected int) {
	interfaces, err := net.Interfaces()
	if err != nil {
		log.Fatal(err)
	}

	actual := 0
	for _, ni := range interfaces {
		if strings.HasPrefix(ni.Name, prefix) {
			actual++
		}
	}

	if actual >= expected {
		log.Println("Veth Check: Passed")
	} else {
		log.Println("Veth Check: Failed")
	}
}

func validateMTUValue(expected int) {
	interfaces, err := net.Interfaces()
	if err != nil {
		log.Fatal(err)
	}

	ok := false
	for _, ni := range interfaces {
		if strings.HasPrefix(ni.Name, "eth") {
			if ni.MTU != expected {
				log.Fatal("MTU Check: Failed")
			} else {
				ok = true
			}
		}
	}
	if !ok {
		log.Println("MTU Check: Failed")
		log.Println("Could not find eth interface, check ifconfig on the node")
	} else {
		log.Println("MTU Check: Passed")
	}
}
