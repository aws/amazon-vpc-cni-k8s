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

// +build ignore

// This program generates vpc_ip_resource_limit.go
// It can be invoked by running `go run`
package main

import (
	"os"
	"sort"
	"text/template"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"

	"github.com/aws/aws-sdk-go/aws"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

const ipLimitFileName = "pkg/awsutils/vpc_ip_resource_limit.go"

type ENILimit struct {
	InstanceType string
	ENILimit     int64
	IPLimit      int64
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

	eniLimitMap := make(map[string]ENILimit)
	for {
		output, err := svc.DescribeInstanceTypes(describeInstanceTypesInput)
		if err != nil {
			log.Fatalf("Failed to call EC2 DescibeInstanceTypes: %v", err)
		}
		// We just want the type name, ENI and IP limits
		for _, info := range output.InstanceTypes {
			// Ignore any missing values
			instanceType := aws.StringValue(info.InstanceType)
			eniLimit := aws.Int64Value(info.NetworkInfo.MaximumNetworkInterfaces)
			ipLimit := aws.Int64Value(info.NetworkInfo.Ipv4AddressesPerInterface)
			if instanceType != "" && eniLimit > 0 && ipLimit > 0 {
				eniLimitMap[instanceType] = newENILimit(instanceType, eniLimit, ipLimit)
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
	eniLimits := make([]ENILimit, 0)
	for _, it := range instanceTypes {
		eniLimits = append(eniLimits, eniLimitMap[it])
	}

	// Generate the file
	f, err := os.Create(ipLimitFileName)
	if err != nil {
		log.Fatalf("Failed to create file: %v, ", err)
	}
	limitsTemplate.Execute(f, struct {
		Timestamp string
		ENILimits []ENILimit
	}{
		Timestamp: time.Now().Format(time.RFC3339),
		ENILimits: eniLimits,
	})
}

// addManualLimits has the list of faulty or missing instance types
func addManualLimits(limitMap map[string]ENILimit) map[string]ENILimit {
	manuallyAddedLimits := []ENILimit{
		{"g4dn.16xlarge", 15, 50}, // Wrong value in the API
		{"cr1.8xlarge", 8, 30},
		{"g4dn.metal", 15, 50},
		{"hs1.8xlarge", 8, 30},
		{"u-12tb1.metal", 5, 30},
		{"u-18tb1.metal", 15, 50},
		{"u-24tb1.metal", 15, 50},
		{"u-6tb1.metal", 5, 30},
		{"u-9tb1.metal", 5, 30},
		{"c6g.medium", 2, 4},
		{"c6g.large", 3, 10},
		{"c6g.xlarge", 4, 15},
		{"c6g.2xlarge", 4, 15},
		{"c6g.4xlarge", 8, 30},
		{"c6g.8xlarge", 8, 30},
		{"c6g.12xlarge", 8, 30},
		{"c6g.16xlarge", 15, 50},
		{"c6g.metal", 15, 50},
		{"r6g.medium", 2, 4},
		{"r6g.large", 3, 10},
		{"r6g.xlarge", 4, 15},
		{"r6g.2xlarge", 4, 15},
		{"r6g.4xlarge", 8, 30},
		{"r6g.8xlarge", 8, 30},
		{"r6g.12xlarge", 8, 30},
		{"r6g.16xlarge", 15, 50},
		{"r6g.metal", 15, 50},
	}
	for _, eniLimit := range manuallyAddedLimits {
		limitMap[eniLimit.InstanceType] = newENILimit(eniLimit.InstanceType, eniLimit.ENILimit, eniLimit.IPLimit)
	}
	return limitMap
}

// Helper to quote the type in order to print the map correctly
func newENILimit(instanceType string, eniLimit int64, ipLimit int64) ENILimit {
	return ENILimit{
		InstanceType: "\"" + instanceType + "\":",
		ENILimit:     eniLimit,
		IPLimit:      ipLimit,
	}
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

// InstanceENIsAvailable contains a mapping of instance types to the number of ENIs available which is described at
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html#AvailableIpPerENI
var InstanceENIsAvailable = map[string]int{
{{- range $it := .ENILimits}}
	{{ printf "%-17s%d" $it.InstanceType $it.ENILimit }},
{{- end }}
}

// InstanceIPsAvailable contains a mapping of instance types to the number of IPs per ENI
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html#AvailableIpPerENI
var InstanceIPsAvailable = map[string]int{
{{- range $it := .ENILimits}}
	{{ printf "%-17s%d" $it.InstanceType $it.IPLimit }},
{{- end }}
}
`))
