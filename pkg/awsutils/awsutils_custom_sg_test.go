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

package awsutils

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

// TestDiscoverCustomSecurityGroups tests the discoverCustomSecurityGroups method
func TestDiscoverCustomSecurityGroups(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockMetadata := testMetadata(nil)

	tests := []struct {
		name           string
		describeOutput *ec2.DescribeSecurityGroupsOutput
		describeError  error
		expectedSGIDs  []string
		expectError    bool
		errorContains  string
	}{
		{
			name: "successful discovery with multiple SGs",
			describeOutput: &ec2.DescribeSecurityGroupsOutput{
				SecurityGroups: []ec2types.SecurityGroup{
					{
						GroupId: aws.String("sg-custom1"),
						Tags: []ec2types.Tag{
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("1"),
							},
						},
					},
					{
						GroupId: aws.String("sg-custom2"),
						Tags: []ec2types.Tag{
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("1"),
							},
						},
					},
				},
			},
			describeError: nil,
			expectedSGIDs: []string{"sg-custom1", "sg-custom2"},
			expectError:   false,
		},
		{
			name: "empty security group list",
			describeOutput: &ec2.DescribeSecurityGroupsOutput{
				SecurityGroups: []ec2types.SecurityGroup{},
			},
			describeError: nil,
			expectedSGIDs: []string{},
			expectError:   false,
		},
		{
			name:           "describe API error",
			describeOutput: nil,
			describeError:  errors.New("API error"),
			expectedSGIDs:  nil,
			expectError:    true,
			errorContains:  "unable to describe security groups",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &EC2InstanceMetadataCache{
				ec2SVC: mockEC2,
				imds:   TypedIMDS{mockMetadata},
				vpcID:  vpcID,
			}

			mockEC2.EXPECT().DescribeSecurityGroups(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
			).Return(tt.describeOutput, tt.describeError)

			sgIDs, err := cache.discoverCustomSecurityGroups()

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
				if len(tt.expectedSGIDs) > 0 {
					assert.ElementsMatch(t, tt.expectedSGIDs, sgIDs)
				} else {
					assert.Empty(t, sgIDs)
				}
			}
		})
	}
}

// TestGetENISubnetID tests the getENISubnetID helper method
func TestGetENISubnetID(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockMetadata := testMetadata(nil)

	tests := []struct {
		name             string
		eniID            string
		describeOutput   *ec2.DescribeNetworkInterfacesOutput
		describeError    error
		expectedSubnetID string
		expectError      bool
		errorContains    string
	}{
		{
			name:  "successful subnet lookup",
			eniID: "eni-12345678",
			describeOutput: &ec2.DescribeNetworkInterfacesOutput{
				NetworkInterfaces: []ec2types.NetworkInterface{
					{
						NetworkInterfaceId: aws.String("eni-12345678"),
						SubnetId:           aws.String("subnet-secondary"),
					},
				},
			},
			describeError:    nil,
			expectedSubnetID: "subnet-secondary",
			expectError:      false,
		},
		{
			name:  "no matching ENI found",
			eniID: "eni-nonexistent",
			describeOutput: &ec2.DescribeNetworkInterfacesOutput{
				NetworkInterfaces: []ec2types.NetworkInterface{},
			},
			describeError: nil,
			expectError:   true,
			errorContains: "no interfaces found",
		},
		{
			name:           "describe API error",
			eniID:          "eni-12345678",
			describeOutput: nil,
			describeError:  errors.New("API error"),
			expectError:    true,
			errorContains:  "unable to describe network interface",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &EC2InstanceMetadataCache{
				ec2SVC: mockEC2,
				imds:   TypedIMDS{mockMetadata},
			}

			mockEC2.EXPECT().DescribeNetworkInterfaces(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
			).Return(tt.describeOutput, tt.describeError)

			subnetID, err := cache.getENISubnetID(tt.eniID)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedSubnetID, subnetID)
			}
		})
	}
}

// TestCreateENIWithCustomSGs tests the custom SG application in createENI
func TestCreateENIWithCustomSGs(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockMetadata := testMetadata(nil)

	tests := []struct {
		name               string
		isPrimarySubnet    bool
		customSGs          []string
		expectedGroups     []string
		subnets            []ec2types.Subnet
		useSubnetDiscovery bool
	}{
		{
			name:            "primary subnet uses primary SGs",
			isPrimarySubnet: true,
			customSGs:       []string{"sg-custom1", "sg-custom2"},
			expectedGroups:  []string{sg1, sg2}, // primary ENI security groups
			subnets: []ec2types.Subnet{
				{
					SubnetId: aws.String(subnetID),
					Tags: []ec2types.Tag{
						{
							Key:   aws.String("kubernetes.io/role/cni"),
							Value: aws.String("1"),
						},
					},
				},
			},
			useSubnetDiscovery: true,
		},
		{
			name:            "secondary subnet with custom SGs",
			isPrimarySubnet: false,
			customSGs:       []string{"sg-custom1", "sg-custom2"},
			expectedGroups:  []string{"sg-custom1", "sg-custom2"}, // custom security groups
			subnets: []ec2types.Subnet{
				{
					SubnetId: aws.String("subnet-secondary"),
					Tags: []ec2types.Tag{
						{
							Key:   aws.String("kubernetes.io/role/cni"),
							Value: aws.String("1"),
						},
					},
				},
			},
			useSubnetDiscovery: true,
		},
		{
			name:            "secondary subnet without custom SGs",
			isPrimarySubnet: false,
			customSGs:       []string{},
			expectedGroups:  []string{sg1, sg2}, // falls back to primary ENI security groups
			subnets: []ec2types.Subnet{
				{
					SubnetId: aws.String("subnet-secondary"),
					Tags: []ec2types.Tag{
						{
							Key:   aws.String("kubernetes.io/role/cni"),
							Value: aws.String("1"),
						},
					},
				},
			},
			useSubnetDiscovery: true,
		},
	}

	// Define the initial security group IDs
	initialSGIDs := []string{sg1, sg2}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &EC2InstanceMetadataCache{
				ec2SVC:             mockEC2,
				imds:               TypedIMDS{mockMetadata},
				useSubnetDiscovery: tt.useSubnetDiscovery,
				securityGroups:     StringSet{}, // Create a new StringSet to avoid copying mutex
				subnetID:           subnetID,
			}

			// Initialize security groups and custom SG cache
			cache.securityGroups.Set(initialSGIDs)
			cache.customSecurityGroups.Set(tt.customSGs)

			// Mock the subnet discovery
			subnetResult := &ec2.DescribeSubnetsOutput{Subnets: tt.subnets}
			mockEC2.EXPECT().DescribeSubnets(gomock.Any(), gomock.Any(), gomock.Any()).Return(subnetResult, nil)

			// Mock free device number detection
			ec2ENIs := make([]ec2types.InstanceNetworkInterface, 0)
			deviceNum1 := int32(0)
			ec2ENI := ec2types.InstanceNetworkInterface{Attachment: &ec2types.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNum1}}
			ec2ENIs = append(ec2ENIs, ec2ENI)
			result := &ec2.DescribeInstancesOutput{
				Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{{NetworkInterfaces: ec2ENIs}}}},
			}
			mockEC2.EXPECT().DescribeInstances(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)

			// Mock the CreateNetworkInterface call and capture the input
			var capturedInput *ec2.CreateNetworkInterfaceInput
			cureniID := eniID
			eni := ec2.CreateNetworkInterfaceOutput{NetworkInterface: &ec2types.NetworkInterface{NetworkInterfaceId: &cureniID}}
			mockEC2.EXPECT().CreateNetworkInterface(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
			).DoAndReturn(func(_ context.Context, input *ec2.CreateNetworkInterfaceInput, _ ...func(*ec2.Options)) (*ec2.CreateNetworkInterfaceOutput, error) {
				capturedInput = input
				return &eni, nil
			})

			// Mock AttachNetworkInterface
			attachmentID := "eni-attach-123"
			attachResult := &ec2.AttachNetworkInterfaceOutput{AttachmentId: &attachmentID}
			mockEC2.EXPECT().AttachNetworkInterface(gomock.Any(), gomock.Any(), gomock.Any()).Return(attachResult, nil)
			mockEC2.EXPECT().ModifyNetworkInterfaceAttribute(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

			// Call the function under test
			createdENI, err := cache.AllocENI(false, nil, "", 5)

			// Verify results
			assert.NoError(t, err)
			assert.NotNil(t, createdENI)

			// Check that the correct security groups were used
			assert.NotNil(t, capturedInput)
			assert.NotNil(t, capturedInput.Groups)

			// Convert []string to set for easier comparison
			expectedGroupSet := StringSet{}
			expectedGroupSet.Set(tt.expectedGroups)

			// Convert the actual groups to set
			actualGroupSet := StringSet{}
			actualGroupSet.Set(capturedInput.Groups)

			// Compare sets (order-independent)
			assert.Equal(t, expectedGroupSet.SortedList(), actualGroupSet.SortedList())
		})
	}
}
