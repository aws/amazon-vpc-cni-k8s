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

package config

// Constant values used in aws-node
// TODO: consolidate all constants in the project
const (
	// Cluster name ENV
	ClusterNameEnv = "CLUSTER_NAME"
	// ENI tags
	ClusterNameTagKeyFormat = "kubernetes.io/cluster/%s"
	ClusterNameTagValue     = "owned"

	ClusterNameTagKey = "cluster.k8s.amazonaws.com/name"
	ENIInstanceIDTag  = "node.k8s.amazonaws.com/instance_id"
	ENINodeNameTagKey = "node.k8s.amazonaws.com/nodename"

	// ENI owner tag
	ENIOwnerTagKey   = "eks:eni:owner"
	ENIOwnerTagValue = "amazon-vpc-cni"
)
