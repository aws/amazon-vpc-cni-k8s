package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"regexp"
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

	currChain := "AWS-SNAT-CHAIN-0"
	lastChain := fmt.Sprintf("AWS-SNAT-CHAIN-%d", numOfCidrs)

	exists, err := iptables.ChainExists("nat", "AWS-SNAT-CHAIN-1")
	if err != nil {
		return err
	}
	// If AWS-SNAT-CHAIN-1 exists, we run the old logic
	if exists {
		i := 0
		for i < numOfCidrs {
			rules, err := iptables.List("nat", currChain)
			if err != nil {
				return err
			}
			i = i + 1
			nextChain := fmt.Sprintf("AWS-SNAT-CHAIN-%d", i)
			foundNextChain := false
			for _, rule := range rules {
				target := fmt.Sprintf("-j %s", nextChain)
				if strings.Contains(rule, target) {
					currChain = nextChain
					foundNextChain = true
					break
				}
			}
			if !foundNextChain {
				return fmt.Errorf("failed: AWS-SNAT chain broken for %s", currChain)
			}
		}
	} else {
		lastChain = "AWS-SNAT-CHAIN-0"
		rules, err := iptables.List("nat", currChain)
		if err != nil {
			return err
		}
		// One rule per cidr + SNAT rule + chain creation rule
		if len(rules) != numOfCidrs+2 {
			return fmt.Errorf("failed: AWS-SNAT chain does not contain the correct amount of rules")
		}
	}

	// Fetch rules from lastChain
	rules, err := iptables.List("nat", lastChain)
	if err != nil {
		return err
	}

	// Check for rule with following pattern
	match := fmt.Sprintf(".*-j SNAT.*%s", expectedString)
	r, _ := regexp.Compile(match)
	for _, rule := range rules {
		if r.Match([]byte(rule)) {
			containsExpectedString = true
			break
		}
	}

	if randomizedSNATValue == "none" && containsExpectedString {
		return fmt.Errorf("failed: found unexpected %s for SNAT rule", expectedString)
	} else if randomizedSNATValue != "none" && !containsExpectedString {
		return fmt.Errorf("failed: did not find expected %s for any of the SNAT rule", expectedString)
	}
	return nil
}
