package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-iptables/iptables"
)

func main() {
	var testIPTableRules bool
	var testExternalDomainConnectivity bool
	var randomizedSNATValue string
	var numOfCidrs int
	var url string

	flag.BoolVar(&testIPTableRules, "testIPTableRules", false, "bool flag when set to true tests validate if IPTable has required rules")
	flag.StringVar(&randomizedSNATValue, "randomizedSNATValue", "prng", "value for AWS_VPC_K8S_CNI_RANDOMIZESNAT")
	flag.IntVar(&numOfCidrs, "numOfCidrs", 1, "Number of CIDR blocks in customer VPC")
	flag.BoolVar(&testExternalDomainConnectivity, "testExternalDomainConnectivity", false, "bool flag when set to true tests if the pod has internet access")
	flag.StringVar(&url, "url", "https://aws.amazon.com/", "url to check for connectivity")

	flag.Parse()

	if testIPTableRules {
		err := validateIPTableRules(randomizedSNATValue, numOfCidrs)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Randomized SNAT test passed for AWS_VPC_K8S_CNI_RANDOMIZESNAT: %s\n", randomizedSNATValue)
	}

	if testExternalDomainConnectivity {
		err := validateExternalDomainConnectivity(url)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("External Domain Connectivity test passed")
	}
}

func validateExternalDomainConnectivity(url string) error {
	timeout := time.Duration(120 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("%s returned response code: %d", url, resp.StatusCode)
	}
	return nil
}

func validateIPTableRules(randomizedSNATValue string, numOfCidrs int) error {
	// Check IPTable rules corresponding to AWS_VPC_K8S_CNI_RANDOMIZESNAT
	expectedString := "random-fully"
	iptables, err := iptables.New()
	if err != nil {
		return err
	}

	if !iptables.HasRandomFully() || randomizedSNATValue == "hashrandom" {
		expectedString = "random"
	}

	containsExpectedString := false
	rule := ""

	for i := 0; i <= numOfCidrs; i++ {
		curr := fmt.Sprintf("AWS-SNAT-CHAIN-%d", i)
		fmt.Printf("Checking: %s\n", curr)
		chains, err := iptables.List("nat", curr)
		if err != nil {
			return err
		}

		for _, chain := range chains {
			if strings.Contains(chain, expectedString) {
				rule = chain
				containsExpectedString = true
				break
			}
		}

		if containsExpectedString {
			break
		}
	}

	if randomizedSNATValue == "none" && containsExpectedString {
		return fmt.Errorf("failed: found unexpected %s for SNAT rule: %s", expectedString, rule)
	} else if randomizedSNATValue != "none" && !containsExpectedString {
		return fmt.Errorf("failed: did not find expected %s for any of the SNAT rules", expectedString)
	}
	return nil
}
