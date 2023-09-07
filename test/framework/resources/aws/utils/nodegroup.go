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
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/vpc"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
)

const (
	// Docker will be default, if not specified
	CONTAINERD                 = "containerd"
	CreateNodeGroupCFNTemplate = "/testdata/amazon-eks-nodegroup.yaml"
	NodeImageIdSSMParam        = "/aws/service/eks/optimized-ami/%s/amazon-linux-2/recommended/image_id"
)

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

	// optional: specify container runtime
	ContainerRuntime string

	NodeImageId string
}

type ClusterVPCConfig struct {
	PublicSubnetList   []string
	AvailZones         []string
	PublicRouteTableID string
	PrivateSubnetList  []string
}

type AWSAuthMapRole struct {
	Groups   []string `yaml:"groups"`
	RoleArn  string   `yaml:"rolearn"`
	UserName string   `yaml:"username"`
}

// Create self managed node group stack
func CreateAndWaitTillSelfManagedNGReady(f *framework.Framework, properties NodeGroupProperties) error {
	templatePath := utils.GetProjectRoot() + CreateNodeGroupCFNTemplate
	templateBytes, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("failed to read from %s, %v", templatePath, err)
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
		limit, _ := vpc.GetInstance(properties.InstanceType)
		maxPods := (limit.ENILimit-1)*(limit.IPv4Limit-1) + 2

		bootstrapArgs += " --use-max-pods false"
		kubeletExtraArgs += fmt.Sprintf(" --max-pods=%d", maxPods)
	}

	containerRuntime := properties.ContainerRuntime
	if containerRuntime != "" {
		bootstrapArgs += fmt.Sprintf(" --container-runtime %s", containerRuntime)
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
			ParameterKey:   aws.String("NodeImageIdSSMParam"),
			ParameterValue: aws.String(fmt.Sprintf(NodeImageIdSSMParam, f.Options.NgK8SVersion)),
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
		{
			ParameterKey:   aws.String("DisableIMDSv1"),
			ParameterValue: aws.String("true"),
		},
	}

	if properties.NodeImageId != "" {
		createNgStackParams = append(createNgStackParams, &cloudformation.Parameter{
			ParameterKey:   aws.String("NodeImageId"),
			ParameterValue: aws.String(properties.NodeImageId),
		})
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
		return fmt.Errorf("failed to list nodegroup with label key %s:%v: %v",
			properties.NgLabelKey, properties.NgLabelVal, err)
	}

	return nil
}

func DeleteAndWaitTillSelfManagedNGStackDeleted(f *framework.Framework, properties NodeGroupProperties) error {
	err := f.CloudServices.CloudFormation().WaitTillStackDeleted(properties.NodeGroupName)
	if err != nil {
		return fmt.Errorf("failed to delete node group cfn stack: %v", err)
	}
	return nil
}

func GetClusterVPCConfig(f *framework.Framework) (*ClusterVPCConfig, error) {
	clusterConfig := &ClusterVPCConfig{
		PublicSubnetList:  []string{},
		AvailZones:        []string{},
		PrivateSubnetList: []string{},
	}

	if len(f.Options.PublicSubnets) > 0 {
		clusterConfig.PublicSubnetList = strings.Split(f.Options.PublicSubnets, ",")
	}
	if len(f.Options.PrivateSubnets) > 0 {
		clusterConfig.PrivateSubnetList = strings.Split(f.Options.PrivateSubnets, ",")
	}
	if len(f.Options.AvailabilityZones) > 0 {
		clusterConfig.AvailZones = strings.Split(f.Options.AvailabilityZones, ",")
	}
	if f.Options.PublicRouteTableID != "" {
		clusterConfig.PublicRouteTableID = f.Options.PublicRouteTableID
	}

	// user provided the info so we don't need to look it up
	if clusterConfig.PublicRouteTableID != "" && len(clusterConfig.PublicSubnetList) > 0 && len(clusterConfig.AvailZones) > 0 {
		return clusterConfig, nil
	}

	if clusterConfig.PublicRouteTableID != "" || len(clusterConfig.PublicSubnetList) > 0 ||
		len(clusterConfig.PrivateSubnetList) > 0 || len(clusterConfig.AvailZones) > 0 {
		return nil, fmt.Errorf("partial configuration, if supplying config via flags you need to provide at least public route table ID, public subnet list and availibility zone list")
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

		isPublic := false
		for _, route := range describeRouteOutput.RouteTables[0].Routes {
			if route.GatewayId != nil && strings.Contains(*route.GatewayId, "igw-") {
				isPublic = true
				clusterConfig.PublicSubnetList = append(clusterConfig.PublicSubnetList, *subnet)
				clusterConfig.PublicRouteTableID = *describeRouteOutput.RouteTables[0].RouteTableId
			}
		}
		if !isPublic {
			clusterConfig.PrivateSubnetList = append(clusterConfig.PrivateSubnetList, *subnet)
		}
	}

	uniqueAZ := map[string]bool{}
	for _, subnet := range clusterConfig.PublicSubnetList {
		describeSubnet, err := f.CloudServices.EC2().DescribeSubnet(subnet)
		if err != nil {
			return nil, fmt.Errorf("failed to describe the subnet %s: %v", subnet, err)
		}
		if ok := uniqueAZ[*describeSubnet.Subnets[0].AvailabilityZone]; !ok {
			uniqueAZ[*describeSubnet.Subnets[0].AvailabilityZone] = true
			clusterConfig.AvailZones =
				append(clusterConfig.AvailZones, *describeSubnet.Subnets[0].AvailabilityZone)
		}
	}

	return clusterConfig, nil
}

func TerminateInstances(f *framework.Framework) error {
	nodeList, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
	if err != nil {
		return fmt.Errorf("failed to get list of nodes created: %v", err)
	}

	var instanceIDs []string
	for _, node := range nodeList.Items {
		instanceIDs = append(instanceIDs, k8sUtils.GetInstanceIDFromNode(node))
	}

	err = f.CloudServices.EC2().TerminateInstance(instanceIDs)
	if err != nil {
		return fmt.Errorf("failed to terminate instances: %v", err)
	}

	// Wait for instances to be replaced
	time.Sleep(time.Minute * 8)
	return nil
}
