// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.
package vpc

import (
	"errors"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)

type NetworkCard struct {
	// max number of interfaces supported per card
	MaximumNetworkInterfaces int64
	// the index of current card
	NetworkCardIndex   int64
	NetworkPerformance string
}

// InstanceTypeLimits keeps track of limits for an instance type
type InstanceTypeLimits struct {
	ENILimit                int
	IPv4Limit               int
	DefaultNetworkCardIndex int
	NetworkCards            []NetworkCard
	HypervisorType          string
	IsBareMetal             bool
}

var ErrInstanceTypeNotExist = errors.New("instance type does not exist")
var ErrNoInfo = errors.New("no info on instance type due to not being publicly available")

var log = logger.Get()

func New(eniLimit int, ipv4Limit int, defaultNetworkCardIndex int, networkCards []NetworkCard,
	hypervisorType string, isBareMetalInstance bool) InstanceTypeLimits {
	return InstanceTypeLimits{
		ENILimit:                eniLimit,
		IPv4Limit:               ipv4Limit,
		NetworkCards:            networkCards,
		HypervisorType:          hypervisorType,
		IsBareMetal:             isBareMetalInstance,
		DefaultNetworkCardIndex: defaultNetworkCardIndex,
	}
}

func GetENILimit(instanceType string) (int, error) {
	instance, ok := GetInstance(instanceType)
	if !ok {
		log.Errorf("%s: %s", instanceType, ErrInstanceTypeNotExist)
		return -1, ErrInstanceTypeNotExist
	}
	return instance.ENILimit, nil
}

func GetIPv4Limit(instanceType string) (int, error) {
	instance, ok := GetInstance(instanceType)
	if !ok {
		log.Errorf("%s: %s", instanceType, ErrInstanceTypeNotExist)
		return -1, ErrInstanceTypeNotExist
	}
	return instance.IPv4Limit, nil
}

func GetDefaultNetworkCardIndex(instanceType string) (int, error) {
	instance, ok := GetInstance(instanceType)
	if !ok {
		log.Errorf("%s: %s", instanceType, ErrInstanceTypeNotExist)
		return -1, ErrInstanceTypeNotExist
	}
	return instance.DefaultNetworkCardIndex, nil
}

func GetHypervisorType(instanceType string) (string, error) {
	instance, ok := GetInstance(instanceType)
	if !ok {
		log.Errorf("%s: %s", instanceType, ErrInstanceTypeNotExist)
		return "", ErrInstanceTypeNotExist
	}
	return instance.HypervisorType, nil
}

func GetIsBareMetal(instanceType string) (bool, error) {
	instance, ok := GetInstance(instanceType)
	if !ok {
		log.Errorf("%s: %s", instanceType, ErrInstanceTypeNotExist)
		return false, ErrInstanceTypeNotExist
	}
	return instance.IsBareMetal, nil
}

func GetNetworkCards(instanceType string) ([]NetworkCard, error) {
	instance, ok := GetInstance(instanceType)
	if !ok {
		log.Errorf("%s: %s", instanceType, ErrInstanceTypeNotExist)
		return nil, ErrInstanceTypeNotExist
	}
	if len(instance.NetworkCards) < 1 {
		log.Errorf("%s: %s", instanceType, ErrNoInfo)
		return nil, ErrNoInfo
	}
	return instance.NetworkCards, nil
}

func GetInstance(instanceType string) (InstanceTypeLimits, bool) {
	instance, ok := instanceNetworkingLimits[instanceType]
	if !ok {
		return InstanceTypeLimits{}, ok
	}
	return instance, ok
}

func SetInstance(instanceType string, eniLimit int, ipv4Limit int, defaultNetworkCardIndex int, networkCards []NetworkCard, hypervisorType string, isBareMetalInstance bool) {
	instanceNetworkingLimits[instanceType] = New(eniLimit, ipv4Limit, defaultNetworkCardIndex, networkCards,
		hypervisorType, isBareMetalInstance)
}
