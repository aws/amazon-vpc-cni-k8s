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
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
)

type EKS interface {
	DescribeCluster(ctx context.Context, clusterName string) (*eks.DescribeClusterOutput, error)
	CreateAddon(ctx context.Context, addonInput AddonInput) (*eks.CreateAddonOutput, error)
	DescribeAddonVersions(ctx context.Context, addonInput AddonInput) (*eks.DescribeAddonVersionsOutput, error)
	DescribeAddon(ctx context.Context, addonInput AddonInput) (*eks.DescribeAddonOutput, error)
	DeleteAddon(ctx context.Context, addOnInput AddonInput) (*eks.DeleteAddonOutput, error)
	GetLatestVersion(ctx context.Context, addonInput AddonInput) (string, error)
}

type defaultEKS struct {
	client *eks.Client
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

func NewEKS(cfg aws.Config, endpoint string) (EKS, error) {
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL: endpoint,
		}, nil
	})
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithEndpointResolverWithOptions(customResolver),
		config.WithRegion(cfg.Region),
	)

	if err != nil {
		return &defaultEKS{}, err
	}

	return &defaultEKS{
		client: eks.NewFromConfig(cfg),
	}, nil
}

func (d *defaultEKS) CreateAddon(ctx context.Context, addonInput AddonInput) (*eks.CreateAddonOutput, error) {
	createAddonInput := &eks.CreateAddonInput{
		AddonName:   aws.String(addonInput.AddonName),
		ClusterName: aws.String(addonInput.ClusterName),
	}
	if addonInput.AddonVersion != "" {
		createAddonInput.AddonVersion = aws.String(addonInput.AddonVersion)
		createAddonInput.ResolveConflicts = types.ResolveConflictsOverwrite
	}
	return d.client.CreateAddon(ctx, createAddonInput)
}

func (d *defaultEKS) DeleteAddon(ctx context.Context, addonInput AddonInput) (*eks.DeleteAddonOutput, error) {
	deleteAddonInput := &eks.DeleteAddonInput{
		AddonName:   aws.String(addonInput.AddonName),
		ClusterName: aws.String(addonInput.ClusterName),
	}
	return d.client.DeleteAddon(ctx, deleteAddonInput)
}

func (d *defaultEKS) DescribeAddonVersions(ctx context.Context, addonInput AddonInput) (*eks.DescribeAddonVersionsOutput, error) {
	describeAddonVersionsInput := &eks.DescribeAddonVersionsInput{
		AddonName:         aws.String(addonInput.AddonName),
		KubernetesVersion: aws.String(addonInput.K8sVersion),
	}
	return d.client.DescribeAddonVersions(ctx, describeAddonVersionsInput)
}

func (d *defaultEKS) DescribeAddon(ctx context.Context, addonInput AddonInput) (*eks.DescribeAddonOutput, error) {
	describeAddonInput := &eks.DescribeAddonInput{
		AddonName:   aws.String(addonInput.AddonName),
		ClusterName: aws.String(addonInput.ClusterName),
	}
	return d.client.DescribeAddon(ctx, describeAddonInput)
}

func (d *defaultEKS) DescribeCluster(ctx context.Context, clusterName string) (*eks.DescribeClusterOutput, error) {
	describeClusterInput := &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	}
	return d.client.DescribeCluster(ctx, describeClusterInput)
}

func (d *defaultEKS) GetLatestVersion(ctx context.Context, addonInput AddonInput) (string, error) {
	addonOutput, err := d.DescribeAddonVersions(ctx, addonInput)
	if err != nil {
		return "", err
	}
	return aws.ToString(addonOutput.Addons[0].AddonVersions[0].AddonVersion), nil
}
