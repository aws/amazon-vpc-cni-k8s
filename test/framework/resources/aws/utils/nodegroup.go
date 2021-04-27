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

package utils

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
)

const CreateNodeGroupCFNTemplateURL = "https://raw.githubusercontent.com/awslabs/amazon-eks-ami/master/amazon-eks-nodegroup.yaml"

type NodeGroupProperties struct {
	// Required to verify the node is up and ready
	NgLabelKey string
	NgLabelVal string
	// ASG Size
	AsgSize       int
	NodeGroupName string
	// If custom networking is set then max pod
	// will be set on Kubelet extra arguments
	IsCustomNetworkingEnabled bool
	// Subnet where the node group will be created
	Subnet       []string
	InstanceType string
	KeyPairName  string
}

type ClusterVPCConfig struct {
	PublicSubnetList   []string
	AvailZones         []string
	PublicRouteTableID string
}

type AWSAuthMapRole struct {
	Groups   []string `yaml:"groups"`
	RoleArn  string   `yaml:"rolearn"`
	UserName string   `yaml:"username"`
}

func CreateAndWaitTillSelfManagedNGReady(f *framework.Framework, properties NodeGroupProperties) error {
	// Create self managed node group stack
	resp, err := http.Get(CreateNodeGroupCFNTemplateURL)
	if err != nil {
		return fmt.Errorf("failed to load template from URL %s: %v",
			CreateNodeGroupCFNTemplateURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("non OK status code on getting node group URL %s: %d",
			CreateNodeGroupCFNTemplateURL, resp.StatusCode)
	}
	defer resp.Body.Close()

	templateBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}
	template := string(templateBytes)

	describeClusterOutput, err := f.CloudServices.EKS().DescribeCluster(f.Options.ClusterName)
	if err != nil {
		return fmt.Errorf("failed to describe cluster %s: %v", f.Options.ClusterName, err)
	}

	var bootstrapArgs = fmt.Sprintf("--apiserver-endpoint %s --b64-cluster-ca %s",
		*describeClusterOutput.Cluster.Endpoint, *describeClusterOutput.Cluster.CertificateAuthority.Data)
	var kubeletExtraArgs = fmt.Sprintf("--node-labels=%s=%s", properties.NgLabelKey, properties.NgLabelVal)

	if properties.IsCustomNetworkingEnabled {
		limit := awsutils.InstanceNetworkingLimits[properties.InstanceType]
		maxPods := (limit.ENILimit-1)*(limit.IPv4Limit-1) + 2

		bootstrapArgs += " --use-max-pods false"
		kubeletExtraArgs += fmt.Sprintf(" --max-pods=%d", maxPods)
	}

	asgSizeString := strconv.Itoa(properties.AsgSize)

	createNgStackParams := []*cloudformation.Parameter{
		{
			ParameterKey:   aws.String("ClusterName"),
			ParameterValue: aws.String(f.Options.ClusterName),
		},
		{
			ParameterKey:   aws.String("VpcId"),
			ParameterValue: aws.String(f.Options.AWSVPCID),
		},
		{
			ParameterKey:   aws.String("Subnets"),
			ParameterValue: aws.String(strings.Join(properties.Subnet, ",")),
		},
		{
			ParameterKey:   aws.String("ClusterControlPlaneSecurityGroup"),
			ParameterValue: describeClusterOutput.Cluster.ResourcesVpcConfig.SecurityGroupIds[0],
		},
		{
			ParameterKey:   aws.String("NodeGroupName"),
			ParameterValue: aws.String(properties.NodeGroupName),
		},
		{
			ParameterKey:   aws.String("NodeAutoScalingGroupMinSize"),
			ParameterValue: aws.String(asgSizeString),
		},
		{
			ParameterKey:   aws.String("NodeAutoScalingGroupDesiredCapacity"),
			ParameterValue: aws.String(asgSizeString),
		},
		{
			ParameterKey:   aws.String("NodeAutoScalingGroupMaxSize"),
			ParameterValue: aws.String(asgSizeString),
		},
		{
			ParameterKey:   aws.String("NodeInstanceType"),
			ParameterValue: aws.String(properties.InstanceType),
		},
		{
			ParameterKey:   aws.String("BootstrapArguments"),
			ParameterValue: aws.String(fmt.Sprintf("%s --kubelet-extra-args '%s'", bootstrapArgs, kubeletExtraArgs)),
		},
		{
			ParameterKey:   aws.String("KeyName"),
			ParameterValue: aws.String(properties.KeyPairName),
		},
	}

	describeStackOutput, err := f.CloudServices.CloudFormation().
		WaitTillStackCreated(properties.NodeGroupName, createNgStackParams, template)
	if err != nil {
		return fmt.Errorf("failed to create node group cfn stack: %v", err)
	}

	var nodeInstanceRole string
	for _, stackOutput := range describeStackOutput.Stacks[0].Outputs {
		if *stackOutput.OutputKey == "NodeInstanceRole" {
			nodeInstanceRole = *stackOutput.OutputValue
		}
	}

	if nodeInstanceRole == "" {
		return fmt.Errorf("failed to find node instance role in stack %+v", describeStackOutput)
	}

	// Update the AWS Auth Config with the Node Instance Role
	awsAuth, err := f.K8sResourceManagers.ConfigMapManager().
		GetConfigMap("kube-system", "aws-auth")
	if err != nil {
		return fmt.Errorf("failed to find aws-auth configmap: %v", err)
	}

	updatedAWSAuth := awsAuth.DeepCopy()
	authMapRole := []AWSAuthMapRole{
		{
			Groups:   []string{"system:bootstrappers", "system:nodes"},
			RoleArn:  nodeInstanceRole,
			UserName: "system:node:{{EC2PrivateDNSName}}",
		},
	}
	yamlBytes, err := yaml.Marshal(authMapRole)

	updatedAWSAuth.Data["mapRoles"] = updatedAWSAuth.Data["mapRoles"] + string(yamlBytes)

	err = f.K8sResourceManagers.ConfigMapManager().UpdateConfigMap(awsAuth, updatedAWSAuth)
	if err != nil {
		return fmt.Errorf("failed to update the auth config with new node's instance role: %v", err)
	}

	// Wait till the node group have joined the cluster and are ready
	err = f.K8sResourceManagers.NodeManager().
		WaitTillNodesReady(properties.NgLabelKey, properties.NgLabelVal, properties.AsgSize)
	if err != nil {
		return fmt.Errorf("faield to list nodegroup with label key %s:%v: %v",
			properties.NgLabelKey, properties.NgLabelVal, err)
	}

	return nil
}

func DeleteAndWaitTillSelfManagedNGStackDeleted(f *framework.Framework, properties NodeGroupProperties) error {
	err := f.CloudServices.CloudFormation().
		WaitTillStackDeleted(properties.NodeGroupName)
	if err != nil {
		return fmt.Errorf("failed to delete node group cfn stack: %v", err)
	}

	return nil
}

func GetClusterVPCConfig(f *framework.Framework) (*ClusterVPCConfig, error) {
	clusterConfig := &ClusterVPCConfig{
		PublicSubnetList: []string{},
		AvailZones:       []string{},
	}

	describeClusterOutput, err := f.CloudServices.EKS().DescribeCluster(f.Options.ClusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to describe cluster %s: %v", f.Options.ClusterName, err)
	}

	for _, subnet := range describeClusterOutput.Cluster.ResourcesVpcConfig.SubnetIds {
		describeRouteOutput, err := f.CloudServices.EC2().DescribeRouteTables(*subnet)
		if err != nil {
			return nil, fmt.Errorf("failed to describe subnet %s: %v", *subnet, err)
		}
		for _, route := range describeRouteOutput.RouteTables[0].Routes {
			if route.GatewayId != nil && strings.Contains(*route.GatewayId, "igw-") {
				clusterConfig.PublicSubnetList = append(clusterConfig.PublicSubnetList, *subnet)
				clusterConfig.PublicRouteTableID = *describeRouteOutput.RouteTables[0].RouteTableId
			}
		}
	}

	uniqueAZ := map[string]bool{}
	for _, subnet := range clusterConfig.PublicSubnetList {
		describeSubnet, err := f.CloudServices.EC2().DescribeSubnet(subnet)
		if err != nil {
			return nil, fmt.Errorf("failed to descrieb the subnet %s: %v", subnet, err)
		}
		if ok := uniqueAZ[*describeSubnet.Subnets[0].AvailabilityZone]; !ok {
			uniqueAZ[*describeSubnet.Subnets[0].AvailabilityZone] = true
			clusterConfig.AvailZones =
				append(clusterConfig.AvailZones, *describeSubnet.Subnets[0].AvailabilityZone)
		}
	}

	return clusterConfig, nil
}
