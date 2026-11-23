// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//      http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package awsutils

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aws/amazon-vpc-cni-k8s/utils/eksinsight"
)

const (
	eniConfigSubnet = "subnet-0eee000000000001"
	secondSubnet    = "subnet-0fff000000000002"
)

// noAddresses is EC2 refusing because the subnet has no addresses left.
func noAddresses() error {
	return &smithy.GenericAPIError{Code: "InsufficientFreeAddressesInSubnet", Message: "test"}
}

// freshRefusals gives the test its own record, which is what New hands the cache in
// production.
func freshRefusals(t *testing.T) *eksinsight.Refusals {
	t.Helper()
	return eksinsight.NewRefusals(nil)
}

// cniTagged is a subnet the CNI is allowed to create ENIs in.
func cniTagged(id string) ec2types.Subnet {
	count := int32(100)
	return ec2types.Subnet{
		AvailableIpAddressCount: &count,
		SubnetId:                aws.String(id),
		Tags: []ec2types.Tag{{
			Key:   aws.String("kubernetes.io/role/cni"),
			Value: aws.String("1"),
		}},
	}
}

// Under custom networking the ENI goes in the ENIConfig subnet, so that is the subnet to
// note. The node's own subnet was never asked.
func TestCustomNetworkingRefusalNotesTheENIConfigSubnet(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()
	refusals := freshRefusals(t)

	mockEC2.EXPECT().CreateNetworkInterface(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, noAddresses())

	cache := &EC2InstanceMetadataCache{
		ec2SVC:              mockEC2,
		instanceType:        "c5n.18xlarge",
		useCustomNetworking: true,
		subnetID:            subnetID,
		refusals:            refusals,
	}

	_, err := cache.createENI(context.Background(), nil, eniConfigSubnet, 5)
	require.Error(t, err)
	assert.Equal(t, []string{eniConfigSubnet}, refusals.Recent(0))
}

func TestCustomNetworkingSuccessClearsTheRefusal(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()
	refusals := freshRefusals(t)

	refusals.RefusedIf(noAddresses(), eniConfigSubnet)
	require.Equal(t, []string{eniConfigSubnet}, refusals.Recent(0))

	created := ec2.CreateNetworkInterfaceOutput{
		NetworkInterface: &ec2types.NetworkInterface{NetworkInterfaceId: aws.String(eniID)},
	}
	mockEC2.EXPECT().CreateNetworkInterface(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&created, nil)

	cache := &EC2InstanceMetadataCache{
		ec2SVC:              mockEC2,
		instanceType:        "c5n.18xlarge",
		useCustomNetworking: true,
		subnetID:            subnetID,
		refusals:            refusals,
	}

	_, err := cache.createENI(context.Background(), nil, eniConfigSubnet, 5)
	require.NoError(t, err)
	assert.Empty(t, refusals.Recent(0))
}

// Subnet discovery tries every candidate subnet and returns only the last error, so each
// attempt is noted as it happens.
func TestEverySubnetTriedDuringDiscoveryIsNoted(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()
	refusals := freshRefusals(t)

	subnets := &ec2.DescribeSubnetsOutput{
		Subnets: []ec2types.Subnet{cniTagged(eniConfigSubnet), cniTagged(secondSubnet)},
	}
	mockEC2.EXPECT().DescribeSubnets(gomock.Any(), gomock.Any(), gomock.Any()).Return(subnets, nil)
	mockEC2.EXPECT().CreateNetworkInterface(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, noAddresses()).Times(2)

	cache := &EC2InstanceMetadataCache{
		ec2SVC:             mockEC2,
		instanceType:       "c5n.18xlarge",
		useSubnetDiscovery: true,
		subnetID:           subnetID,
		refusals:           refusals,
	}

	_, err := cache.createENI(context.Background(), nil, "", 5)
	require.Error(t, err)
	assert.ElementsMatch(t, []string{eniConfigSubnet, secondSubnet}, refusals.Recent(0))
}

// Most awsutils tests build the cache as a struct literal and never set a recorder, so both
// the refused and the supplied path have to work with a nil one.
func TestACacheWithNoRecorderRecordsNothing(t *testing.T) {
	t.Run("refused", func(t *testing.T) {
		ctrl, mockEC2 := setup(t)
		defer ctrl.Finish()

		mockEC2.EXPECT().CreateNetworkInterface(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, noAddresses())

		cache := &EC2InstanceMetadataCache{
			ec2SVC:              mockEC2,
			instanceType:        "c5n.18xlarge",
			useCustomNetworking: true,
			subnetID:            subnetID,
		}

		_, err := cache.createENI(context.Background(), nil, eniConfigSubnet, 5)
		assert.Error(t, err)
	})

	t.Run("supplied", func(t *testing.T) {
		ctrl, mockEC2 := setup(t)
		defer ctrl.Finish()

		created := ec2.CreateNetworkInterfaceOutput{
			NetworkInterface: &ec2types.NetworkInterface{NetworkInterfaceId: aws.String(eniID)},
		}
		mockEC2.EXPECT().CreateNetworkInterface(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&created, nil)

		cache := &EC2InstanceMetadataCache{
			ec2SVC:              mockEC2,
			instanceType:        "c5n.18xlarge",
			useCustomNetworking: true,
			subnetID:            subnetID,
		}

		_, err := cache.createENI(context.Background(), nil, eniConfigSubnet, 5)
		assert.NoError(t, err)
	})
}
