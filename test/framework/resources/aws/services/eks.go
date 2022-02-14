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

package services

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
)

type EKS interface {
	DescribeCluster(clusterName string) (*eks.DescribeClusterOutput, error)
	CreateAddon(addonInput *AddonInput) (*eks.CreateAddonOutput, error)
	DescribeAddonVersions(AddonInput *AddonInput) (*eks.DescribeAddonVersionsOutput, error)
	DescribeAddon(addonInput *AddonInput) (*eks.DescribeAddonOutput, error)
	DeleteAddon(AddOnInput *AddonInput) (*eks.DeleteAddonOutput, error)
	GetLatestVersion(addonInput *AddonInput) (string, error)
}

type defaultEKS struct {
	eksiface.EKSAPI
}

// Internal Addon Input struct
// subset of eks.AddonInput
// used by ginkgo tests
type AddonInput struct {
	AddonName    string
	ClusterName  string
	AddonVersion string
	K8sVersion   string
}

func NewEKS(session *session.Session, endpoint string) EKS {
	return &defaultEKS{
		EKSAPI: eks.New(session, &aws.Config{
			Endpoint: aws.String(endpoint),
			Region:   session.Config.Region,
		}),
	}
}

func (d defaultEKS) CreateAddon(addonInput *AddonInput) (*eks.CreateAddonOutput, error) {
	createAddonInput := &eks.CreateAddonInput{
		AddonName:   aws.String(addonInput.AddonName),
		ClusterName: aws.String(addonInput.ClusterName),
	}
	if addonInput.AddonVersion != "" {
		createAddonInput.SetAddonVersion(addonInput.AddonVersion)
		createAddonInput.SetResolveConflicts("OVERWRITE")
	}
	return d.EKSAPI.CreateAddon(createAddonInput)
}

func (d defaultEKS) DeleteAddon(addonInput *AddonInput) (*eks.DeleteAddonOutput, error) {
	deleteAddonInput := &eks.DeleteAddonInput{
		AddonName:   aws.String(addonInput.AddonName),
		ClusterName: aws.String(addonInput.ClusterName),
	}
	return d.EKSAPI.DeleteAddon(deleteAddonInput)
}

func (d defaultEKS) DescribeAddonVersions(addonInput *AddonInput) (*eks.DescribeAddonVersionsOutput, error) {
	describeAddonVersionsInput := &eks.DescribeAddonVersionsInput{
		AddonName:         aws.String(addonInput.AddonName),
		KubernetesVersion: aws.String(addonInput.K8sVersion),
	}
	return d.EKSAPI.DescribeAddonVersions(describeAddonVersionsInput)
}

func (d defaultEKS) DescribeAddon(addonInput *AddonInput) (*eks.DescribeAddonOutput, error) {
	describeAddonInput := &eks.DescribeAddonInput{
		AddonName:   aws.String(addonInput.AddonName),
		ClusterName: aws.String(addonInput.ClusterName),
	}
	return d.EKSAPI.DescribeAddon(describeAddonInput)
}

func (d defaultEKS) DescribeCluster(clusterName string) (*eks.DescribeClusterOutput, error) {
	describeClusterInput := &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	}

	return d.EKSAPI.DescribeCluster(describeClusterInput)
}

func (d defaultEKS) GetLatestVersion(addonInput *AddonInput) (string, error) {
	addonOutput, err := d.DescribeAddonVersions(addonInput)
	if err != nil {
		return "", err
	}
	return *addonOutput.Addons[0].AddonVersions[0].AddonVersion, nil
}
