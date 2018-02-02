// Copyright 2017-2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package vpcipresource

import (
	"encoding/json"
	"strconv"

	log "github.com/cihub/seelog"
	"github.com/pkg/errors"

	//"k8s.io/apimachinery/pkg/api/errors"
	//metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	//"k8s.io/client-go/rest"
)

// vpcIPResource is used to specify node leve VPC IP as a K8S extended resource
type vpcIPResource struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

const (
	k8sJSONHeader      = "application/json-patch+json"
	k8sResourcePrefix  = "/status/capacity/"
	vpcIPResourceDef   = "vpc.amazonaws.com~1ipv4"
	k8sNodeSubResource = "status"
	k8sAddOp           = "add"
)

// Interface is the interface for vpcipresource
type Interface interface {
	Update(client kubernetes.Interface, nodeName string, ipCount int) error
}

// VPCIPResource is the obect for VPC IP resource
type VPCIPResource struct{}

// NewVPCIPResource creates a new VPCIPResource object
func NewVPCIPResource() Interface {
	return &VPCIPResource{}
}

// Update updates VPC IP resource
func (r *VPCIPResource) Update(client kubernetes.Interface, nodeName string, ipCount int) error {
	dataJSON := []vpcIPResource{{
		Op:    k8sAddOp,
		Path:  k8sResourcePrefix + vpcIPResourceDef,
		Value: strconv.Itoa(ipCount),
	}}
	data, err := json.Marshal(dataJSON)
	if err != nil {
		return errors.Wrap(err, "failed to marshal vpc ip resource")
	}

	_, err = client.CoreV1().Nodes().Patch(nodeName, k8sJSONHeader, data, k8sNodeSubResource)

	if err != nil {
		return errors.Wrap(err, "failed to update vpc ip resource")
	}

	log.Infof("Updated node(%s)'s resource(%s) to %d", nodeName, vpcIPResourceDef, ipCount)
	return nil
}

// GetVPICIPLimit return IP address limit based on EC2 instance type
func GetVPICIPLimit(instanceType string) (int, error) {
	eniLimit, ok := InstanceENIsAvailable[instanceType]
	if !ok {
		log.Errorf("Failed to get eni limit due to unknown instance type %s", instanceType)
		return 0, errors.Errorf("vpc ip resource(eni limit): unknown instance type %s", instanceType)
	}

	ipLimit, ok := InstanceIPsAvailable[instanceType]
	if !ok {
		log.Errorf("Failed to get eni IP limit due to unknown instance type %s", instanceType)
		return 0, errors.Errorf("vpc ip resource(eni ip limit): unknown instance thype %s", instanceType)
	}

	return eniLimit * (ipLimit - 1), nil
}
