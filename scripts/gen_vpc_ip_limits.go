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

// This program generates vpc_ip_resource_limit.go
// It can be invoked by running `go run`
package main

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"

	"github.com/aws/aws-sdk-go/aws"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

const ipLimitFileName = "pkg/awsutils/vpc_ip_resource_limit.go"
const eniMaxPodsFileName = "misc/eni-max-pods.txt"

// Helper to quote the type in order to print the map correctly
func printMapLine(instanceType string, l awsutils.InstanceTypeLimits) string {
	indentedInstanceType := fmt.Sprintf("\"%s\":", instanceType)
	return strings.Replace(fmt.Sprintf("%-17s%# v", indentedInstanceType, l), "awsutils.InstanceTypeLimits", "", 1)
}

// Helper to calculate the --max-pods to match the ENIs and IPs on the instance
func printPodLimit(instanceType string, l awsutils.InstanceTypeLimits) string {
	maxPods := l.ENILimit*(l.IPv4Limit-1) + 2
	return fmt.Sprintf("%s %d", instanceType, maxPods)
}

// Helper function to call the EC2 DescribeInstanceTypes API and generate the IP limit file.
func main() {
	log := logger.DefaultLogger()

	// Get session
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))
	_, err := sess.Config.Credentials.Get()
	if err != nil {
		log.Fatalf("Failed to get session credentials: %v", err)
	}
	svc := ec2.New(sess)
	describeInstanceTypesInput := &ec2.DescribeInstanceTypesInput{}

	eniLimitMap := make(map[string]awsutils.InstanceTypeLimits)
	for {
		output, err := svc.DescribeInstanceTypes(describeInstanceTypesInput)
		if err != nil {
			log.Fatalf("Failed to call EC2 DescribeInstanceTypes: %v", err)
		}
		// We just want the type name, ENI and IP limits
		for _, info := range output.InstanceTypes {
			// Ignore any missing values
			instanceType := aws.StringValue(info.InstanceType)
			eniLimit := int(aws.Int64Value(info.NetworkInfo.MaximumNetworkInterfaces))
			ipv4Limit := int(aws.Int64Value(info.NetworkInfo.Ipv4AddressesPerInterface))
			if instanceType != "" && eniLimit > 0 && ipv4Limit > 0 {
				eniLimitMap[instanceType] = awsutils.InstanceTypeLimits{ENILimit: eniLimit, IPv4Limit: ipv4Limit}
			}
		}
		// Paginate to the next request
		if output.NextToken == nil {
			break
		}
		describeInstanceTypesInput = &ec2.DescribeInstanceTypesInput{
			NextToken: output.NextToken,
		}
	}

	// Override faulty values and add missing instance types
	eniLimitMap = addManualLimits(eniLimitMap)

	// Sort the keys
	instanceTypes := make([]string, 0)
	for k := range eniLimitMap {
		instanceTypes = append(instanceTypes, k)
	}
	sort.Strings(instanceTypes)

	// Generate instance ENI limits
	eniLimits := make([]string, 0)
	for _, it := range instanceTypes {
		eniLimits = append(eniLimits, printMapLine(it, eniLimitMap[it]))
	}
	f, err := os.Create(ipLimitFileName)
	if err != nil {
		log.Fatalf("Failed to create file: %v\n", err)
	}
	err = limitsTemplate.Execute(f, struct {
		Timestamp string
		ENILimits []string
	}{
		Timestamp: time.Now().Format(time.RFC3339),
		ENILimits: eniLimits,
	})
	if err != nil {
		log.Fatalf("Failed to generate template: %v\n", err)
	}
	log.Infof("Generated %s", ipLimitFileName)

	// Generate --max-pods file for awslabs/amazon-eks-ami
	eniPods := make([]string, 0)
	for _, it := range instanceTypes {
		eniPods = append(eniPods, printPodLimit(it, eniLimitMap[it]))
	}
	f, err = os.Create(eniMaxPodsFileName)
	if err != nil {
		log.Fatalf("Failed to create file: %s\n", err)
	}
	err = eksMaxPodsTemplate.Execute(f, struct {
		Timestamp string
		ENIPods   []string
	}{
		Timestamp: time.Now().Format(time.RFC3339),
		ENIPods:   eniPods,
	})
	if err != nil {
		log.Fatalf("Failed to generate template: %v\n", err)
	}
	log.Infof("Generated %s", eniMaxPodsFileName)
}

// addManualLimits has the list of faulty or missing instance types
func addManualLimits(limitMap map[string]awsutils.InstanceTypeLimits) map[string]awsutils.InstanceTypeLimits {
	manuallyAddedLimits := map[string]awsutils.InstanceTypeLimits{
		"cr1.8xlarge":   {ENILimit: 8, IPv4Limit: 30},
		"hs1.8xlarge":   {ENILimit: 8, IPv4Limit: 30},
		"u-12tb1.metal": {ENILimit: 5, IPv4Limit: 30},
		"u-18tb1.metal": {ENILimit: 15, IPv4Limit: 50},
		"u-24tb1.metal": {ENILimit: 15, IPv4Limit: 50},
		"u-6tb1.metal":  {ENILimit: 5, IPv4Limit: 30},
		"u-9tb1.metal":  {ENILimit: 5, IPv4Limit: 30},
		"c5a.metal":     {ENILimit: 15, IPv4Limit: 50},
		"c5ad.metal":    {ENILimit: 15, IPv4Limit: 50},
		"p4d.24xlarge":  {ENILimit: 15, IPv4Limit: 50},
	}
	for instanceType, instanceLimits := range manuallyAddedLimits {
		val, ok := limitMap[instanceType]
		if ok {
			if reflect.DeepEqual(val, instanceLimits) {
				fmt.Printf("Delete %q: %v is already correct in the API\n", instanceType, val)
			} else {
				fmt.Printf("Replacing API value %v with override %v for %q\n", val, instanceLimits, instanceType)
			}
		} else {
			fmt.Printf("Adding %q: %v since it is missing from the API\n", instanceType, instanceLimits)
		}
		limitMap[instanceType] = instanceLimits
	}
	return limitMap
}

var limitsTemplate = template.Must(template.New("").Parse(`// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
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

// Code generated by go generate; DO NOT EDIT.
// This file was generated at {{ .Timestamp }}

package awsutils

// InstanceNetworkingLimits contains a mapping from instance type to networking limits for the type. Documentation found at
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html#AvailableIpPerENI
var InstanceNetworkingLimits = map[string]InstanceTypeLimits{
{{- range $mapLine := .ENILimits}}
	{{ printf "%s" $mapLine }},
{{- end }}
}
`))

var eksMaxPodsTemplate = template.Must(template.New("").Parse(`# Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"). You may
# not use this file except in compliance with the License. A copy of the
# License is located at
#
#     http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is distributed
# on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
# express or implied. See the License for the specific language governing
# permissions and limitations under the License.
#
# This file was generated at {{ .Timestamp }}
#
# Mapping is calculated from AWS EC2 API using the following formula:
# * First IP on each ENI is not used for pods
# * +2 for the pods that use host-networking (AWS CNI and kube-proxy)
#
#   # of ENI * (# of IPv4 per ENI - 1) + 2
#
# https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html#AvailableIpPerENI
#
{{- range $instanceLimit := .ENIPods}}
{{ printf "%s" $instanceLimit }}
{{- end }}
`))
