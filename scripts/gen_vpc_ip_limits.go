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
	"strconv"
	"text/template"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/vpc"

	"github.com/aws/aws-sdk-go/aws"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

const ipLimitFileName = "pkg/vpc/vpc_ip_resource_limit.go"
const eniMaxPodsFileName = "misc/eni-max-pods.txt"

var log = logger.DefaultLogger()

// Helper to calculate the --max-pods to match the ENIs and IPs on the instance
func printPodLimit(instanceType string, l vpc.InstanceTypeLimits) string {
	maxPods := l.ENILimit*(l.IPv4Limit-1) + 2
	return fmt.Sprintf("%s %d", instanceType, maxPods)
}

func main() {
	// Get instance types limits across all regions
	regions := describeRegions()
	eniLimitMap := make(map[string]vpc.InstanceTypeLimits)
	for _, region := range regions {
		describeInstanceTypes(region, eniLimitMap)
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
	f, err := os.Create(ipLimitFileName)
	if err != nil {
		log.Fatalf("Failed to create file: %v\n", err)
	}
	err = limitsTemplate.Execute(f, struct {
		ENILimits map[string]vpc.InstanceTypeLimits
		Regions   []string
	}{
		ENILimits: eniLimitMap,
		Regions:   regions,
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
		ENIPods []string
		Regions []string
	}{
		ENIPods: eniPods,
		Regions: regions,
	})
	if err != nil {
		log.Fatalf("Failed to generate template: %v\n", err)
	}
	log.Infof("Generated %s", eniMaxPodsFileName)
}

// Helper function to call the EC2 DescribeRegions API, returning sorted region names
// Note that the credentials being used may not be opted-in to all regions
func describeRegions() []string {
	// Get session
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))
	_, err := sess.Config.Credentials.Get()
	if err != nil {
		log.Fatalf("Failed to get session credentials: %v", err)
	}
	svc := ec2.New(sess)
	output, err := svc.DescribeRegions(&ec2.DescribeRegionsInput{})
	if err != nil {
		log.Fatalf("Failed to call EC2 DescribeRegions: %v", err)
	}
	var regionNames []string
	for _, region := range output.Regions {
		regionNames = append(regionNames, *region.RegionName)
	}
	sort.Strings(regionNames)
	return regionNames
}

// Helper function to call the EC2 DescribeInstanceTypes API for a region and merge the respective instance-type limits into eniLimitMap
func describeInstanceTypes(region string, eniLimitMap map[string]vpc.InstanceTypeLimits) {
	log.Infof("Describing instance types in region=%s", region)

	// Get session
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config:            *aws.NewConfig().WithRegion(region),
	}))
	_, err := sess.Config.Credentials.Get()
	if err != nil {
		log.Fatalf("Failed to get session credentials: %v", err)
	}
	svc := ec2.New(sess)
	describeInstanceTypesInput := &ec2.DescribeInstanceTypesInput{}

	for {
		output, err := svc.DescribeInstanceTypes(describeInstanceTypesInput)
		if err != nil {
			log.Fatalf("Failed to call EC2 DescribeInstanceTypes: %v", err)
		}
		// We just want the type name, ENI and IP limits
		for _, info := range output.InstanceTypes {
			// Ignore any missing values
			instanceType := aws.StringValue(info.InstanceType)
			// only one network card is supported, so use the MaximumNetworkInterfaces from the default card if more than one are present
			var eniLimit int
			if len(info.NetworkInfo.NetworkCards) > 1 {
				eniLimit = int(aws.Int64Value(info.NetworkInfo.NetworkCards[*info.NetworkInfo.DefaultNetworkCardIndex].MaximumNetworkInterfaces))
			} else {
				eniLimit = int(aws.Int64Value(info.NetworkInfo.MaximumNetworkInterfaces))
			}
			ipv4Limit := int(aws.Int64Value(info.NetworkInfo.Ipv4AddressesPerInterface))
			isBareMetalInstance := aws.BoolValue(info.BareMetal)
			hypervisorType := aws.StringValue(info.Hypervisor)
			if hypervisorType == "" {
				hypervisorType = "unknown"
			}
			networkCards := make([]vpc.NetworkCard, aws.Int64Value(info.NetworkInfo.MaximumNetworkCards))
			defaultNetworkCardIndex := int(aws.Int64Value(info.NetworkInfo.DefaultNetworkCardIndex))
			for idx := 0; idx < len(networkCards); idx += 1 {
				networkCards[idx] = vpc.NetworkCard{
					MaximumNetworkInterfaces: *info.NetworkInfo.NetworkCards[idx].MaximumNetworkInterfaces,
					NetworkCardIndex:         *info.NetworkInfo.NetworkCards[idx].NetworkCardIndex,
				}
			}
			if instanceType != "" && eniLimit > 0 && ipv4Limit > 0 {
				limits := vpc.InstanceTypeLimits{ENILimit: eniLimit, IPv4Limit: ipv4Limit, NetworkCards: networkCards, HypervisorType: strconv.Quote(hypervisorType),
					IsBareMetal: isBareMetalInstance, DefaultNetworkCardIndex: defaultNetworkCardIndex}
				if existingLimits, contains := eniLimitMap[instanceType]; contains && !reflect.DeepEqual(existingLimits, limits) {
					// this should never happen
					log.Fatalf("A previous region has different limits for instanceType=%s than region=%s", instanceType, region)
				}
				eniLimitMap[instanceType] = limits
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
}

// addManualLimits has the list of faulty or missing instance types
// Instance types added here are missing the NetworkCard info due to not being publicly available. Only supporting
// NetworkCard for instances currently accessible from the EC2 API to match customer accessibility.
func addManualLimits(limitMap map[string]vpc.InstanceTypeLimits) map[string]vpc.InstanceTypeLimits {
	manuallyAddedLimits := map[string]vpc.InstanceTypeLimits{
		"cr1.8xlarge": {
			ENILimit:       8,
			IPv4Limit:      30,
			HypervisorType: strconv.Quote("unknown"),
			IsBareMetal:    false,
		},
		"hs1.8xlarge": {
			ENILimit:       8,
			IPv4Limit:      30,
			HypervisorType: strconv.Quote("unknown"),
			IsBareMetal:    false,
		},
		"u-12tb1.metal": {
			ENILimit:       5,
			IPv4Limit:      30,
			HypervisorType: strconv.Quote("unknown"),
			IsBareMetal:    true,
		},
		"u-18tb1.metal": {
			ENILimit:       15,
			IPv4Limit:      50,
			HypervisorType: strconv.Quote("unknown"),
			IsBareMetal:    true,
		},
		"u-24tb1.metal": {
			ENILimit:       15,
			IPv4Limit:      50,
			HypervisorType: strconv.Quote("unknown"),
			IsBareMetal:    true,
		},
		"u-6tb1.metal": {
			ENILimit:       5,
			IPv4Limit:      30,
			HypervisorType: strconv.Quote("unknown"),
			IsBareMetal:    true,
		},
		"u-9tb1.metal": {
			ENILimit:       5,
			IPv4Limit:      30,
			HypervisorType: strconv.Quote("unknown"),
			IsBareMetal:    true,
		},
		"c5a.metal": {
			ENILimit:       15,
			IPv4Limit:      50,
			HypervisorType: strconv.Quote("unknown"),
			IsBareMetal:    true,
		},
		"c5ad.metal": {
			ENILimit:       15,
			IPv4Limit:      50,
			HypervisorType: strconv.Quote("unknown"),
			IsBareMetal:    true,
		},
		"p4de.24xlarge": {
			ENILimit:       15,
			IPv4Limit:      50,
			HypervisorType: strconv.Quote("nitro"),
			IsBareMetal:    false,
		},
		"c7g.metal": {
			ENILimit:       15,
			IPv4Limit:      50,
			HypervisorType: strconv.Quote("nitro"),
			IsBareMetal:    true,
		},
		"bmn-sf1.metal": {
			ENILimit:       15,
			IPv4Limit:      50,
			HypervisorType: strconv.Quote("unknown"),
			IsBareMetal:    true,
		},
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
//
// The regions queried were:
{{- range $region := .Regions}}
{{ printf "// - %s" $region }}
{{- end }}
package vpc

// InstanceNetworkingLimits contains a mapping from instance type to networking limits for the type. Documentation found at
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html#AvailableIpPerENI
var instanceNetworkingLimits = map[string]InstanceTypeLimits{
{{- range $key, $value := .ENILimits}}
	"{{$key}}":	   {
		ENILimit: {{.ENILimit}}, 
		IPv4Limit: {{.IPv4Limit}}, 
		DefaultNetworkCardIndex: {{.DefaultNetworkCardIndex}},
		NetworkCards: []NetworkCard{
			{{- range .NetworkCards}}
				{
					MaximumNetworkInterfaces: {{.MaximumNetworkInterfaces}},
					NetworkCardIndex: {{.NetworkCardIndex}},
				},
			{{end}}
		},
		HypervisorType: {{.HypervisorType}},
		IsBareMetal: {{.IsBareMetal}},
	},
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
# The regions queried were:
{{- range $region := .Regions}}
{{ printf "# - %s" $region }}
{{- end }}
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
