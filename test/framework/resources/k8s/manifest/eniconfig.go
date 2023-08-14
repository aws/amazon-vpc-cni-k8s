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

package manifest

import (
	"fmt"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ENIConfigBuilder struct {
	name          string
	subnetID      string
	securityGroup []string
}

func NewENIConfigBuilder() *ENIConfigBuilder {
	return &ENIConfigBuilder{
		name: "eniConfig-test",
	}
}

func (e *ENIConfigBuilder) Name(name string) *ENIConfigBuilder {
	e.name = name
	return e
}

func (e *ENIConfigBuilder) SubnetID(subnetID string) *ENIConfigBuilder {
	e.subnetID = subnetID
	return e
}

func (e *ENIConfigBuilder) SecurityGroup(securityGroup []string) *ENIConfigBuilder {
	e.securityGroup = securityGroup
	return e
}

func (e *ENIConfigBuilder) Build() (*v1alpha2.ENIConfig, error) {
	if e.subnetID == "" {
		return nil, fmt.Errorf("subnet id is a required field")
	}

	if e.securityGroup == nil {
		return &v1alpha2.ENIConfig{
			ObjectMeta: v1.ObjectMeta{
				Name: e.name,
			},
			Spec: v1alpha2.ENIConfigSpec{
				Subnet: e.subnetID,
			},
		}, nil
	} else {
		return &v1alpha2.ENIConfig{
			ObjectMeta: v1.ObjectMeta{
				Name: e.name,
			},
			Spec: v1alpha2.ENIConfigSpec{
				SecurityGroups: e.securityGroup,
				Subnet:         e.subnetID,
			},
		}, nil
	}
}
