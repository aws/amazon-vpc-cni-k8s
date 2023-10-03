# Multiple Secondary Subnets within the Same Availability Zone (AZ) - VPC CNI Custom Networking

## Background
This document addresses the common challenge raised by the community, which involves adding multiple secondary CIDR subnets to the same Availability Zone (AZ) using VPC CNI custom networking. 
This issue is encountered by organizations seeking to optimize their network configurations in AWS, allowing for more flexible resource allocation and improved network segmentation within the same AZ.

### Issue Description:
The challenge at hand is enabling support for multiple secondary subnets within the same Availability Zone (AZ) using VPC CNI custom networking. 
This requirement arises from a common scenario where organizations initially allocate smaller secondary CIDR subnets to each AZ. 
As resources and workloads expand, they encounter the need to add additional secondary subnets to the same AZ to accommodate more IP addresses effectively. 
This flexibility in adding more subnets within the same AZ allows organizations to scale their network configurations as their needs evolve.

For more detailed information on this issue, you can refer to the GitHub discussion [here](https://github.com/aws/containers-roadmap/issues/1709)

### Solution:
To address this challenge until the ENIConfig Custom Resource Definition (CRD) supports a list of subnets, 
we have developed a practical workaround. 
This workaround is designed to be adaptable to any AWS environment utilizing Amazon Elastic Kubernetes Service (EKS). 
It encompasses the modification of the VPC CNI configuration and the creation of custom ENIConfig objects. 
These custom ENIConfig objects facilitate the inclusion of multiple secondary subnets within the same Availability Zone (AZ), offering a temporary but effective solution for accommodating evolving network needs.

The solution code has been developed in Terraform, but it can be adapted to various Infrastructure as Code (IaC) templates as needed. 
Below is the Terraform code snippets that addresses this challenge:

#### Step 1: Create ENI Configurations
To implement this step, you can utilize variables or Terraform module outputs as inputs for the local variables defined below. 
This process involves the creation of VPC CNI ENIConfig objects to accommodate secondary CIDR subnets. 
In the example provided, we allocate two secondary CIDR subnets per Availability Zone (AZ), resulting in a total of six subnets spread across three AZs.

```hcl
local {
  # Define the secondary CIDR subnets for each AZ. 
  # In this example, two secondary subnets are allocated for each AZ.
  # You can customize these values based on your network configuration.
  secondary_subnet_ids = ["subnet1-aza", "subnet1-azb", "subnet1-azc", "subnet2-aza", "subnet2-azb", "subnet3-azc"]

  # List of Availability Zones (AZs) where subnets are distributed.
  azs = ["us-west-2a", "us-west-2b", "us-west-2c"]

  # Specify the security group IDs for ENIConfig.
  security_group_ids = ["cluster_primary_security_group_id", "node_security_group_id"]
}

resource "kubectl_manifest" "eni_config" {
  # Create an ENIConfig resource for each secondary subnet defined in local.secondary_subnet_ids.
  for_each = {
    for idx, subnet in local.secondary_subnet_ids : idx => subnet
  }

  # Define the YAML manifest for the ENIConfig resource.
  yaml_body = yamlencode({
    apiVersion = "crd.k8s.amazonaws.com/v1alpha1"
    kind       = "ENIConfig"
    metadata = {
      # Generate a unique name for each ENIConfig based on the index and AZ.
      name = "eni-config-${format("%02d", each.key + 1)}-${element(local.azs, each.key)}"
    }
    spec = {
      securityGroups = local.security_group_ids  # Assign security group IDs to the ENIConfig.
      subnet = each.value  # Associate the ENIConfig with the specific subnet.
    }
  })
}

```

#### Output: ENI Config Names
The following is the output object names of the ENI Configurations created for multiple secondary subnets within the same Availability Zone (AZ) using custom networking:

```shell
eni-config-01-us-west-2a
eni-config-02-us-west-2b
eni-config-03-us-west-2c
eni-config-04-us-west-2a
eni-config-05-us-west-2b
eni-config-06-us-west-2c
```

These ENI Configurations enable the allocation of Elastic Network Interfaces (ENIs) and provide the flexibility needed for scaling and optimizing network resources within your Amazon Elastic Kubernetes Service (EKS) cluster.

### Step 2: Create custom VPC CNI config and Managed Node Groups
To enable support for multiple subnets within the same Availability Zone (AZ), you need to perform the following steps:

- **Configure EKS VPC CNI Add-On:** Modify the EKS VPC CNI add-on configuration by setting the `ENI_CONFIG_LABEL_DEF` value to `k8s.amazonaws.com/eniConfig` within the cluster_addons section of your EKS configuration. 
This step ensures that EKS is aware of the custom ENIConfigurations.

- **Create Managed Node Groups:** Create managed node groups for your EKS cluster, and associate the appropriate ENIConfig labels with each node group. 
This association allows you to allocate pod IPs from the specified ENIConfig objects. 
Customize the labels according to your specific requirements.

Below is an example Terraform code snippet demonstrating how to configure the EKS VPC CNI add-on and create managed node groups with ENIConfig labels:

```hcl
module "eks" {
  source                         = "terraform-aws-modules/eks/aws"
  version                        = "~> 19.16"
  cluster_name                   = "my-eks-cluster"

  # Rest of the EKS configuration ...

  # Configure EKS Managed Add-Ons with VPC CNI Custom Network Configuration
  cluster_addons = {
    vpc-cni = {
      configuration_values = jsonencode({
        env = {
          AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG = "true"
          ENI_CONFIG_LABEL_DEF               = "k8s.amazonaws.com/eniConfig"
          ENABLE_PREFIX_DELEGATION           = "true"
        }
      })
    }
  }

  # Define EKS Managed Node Groups
  eks_managed_node_groups = {
    core = {
      instance_types = ["m5.large"]
      min_size       = 3
      max_size       = 3
      desired_size   = 3

      # Specify private subnets to allocate IPs to the Nodes from RFC 1918
      private_subnets = ["subnet-1a", "subnet-2b", "subnet-3c"]

      labels = {
        WorkerType                    = "ON_DEMAND"
        NodeGroupType                 = "core"
        # Configure Secondary CIDR Subnet ENIConfig labels for this Node group
        # Allocate 6 secondary subnet CIDRs to this Node group to allocate IPs to the Pods
        # Carrier-Grade NAT (CGN) Range IPs ENIConfig
        "k8s.amazonaws.com/eniConfig" = "eni-config-01-us-west-2a"
        "k8s.amazonaws.com/eniConfig" = "eni-config-02-us-west-2b"
        "k8s.amazonaws.com/eniConfig" = "eni-config-03-us-west-2c"
        "k8s.amazonaws.com/eniConfig" = "eni-config-04-us-west-2a"
        "k8s.amazonaws.com/eniConfig" = "eni-config-05-us-west-2b"
        "k8s.amazonaws.com/eniConfig" = "eni-config-06-us-west-2c"
      }
    },

    spark = {
      instance_types = ["m5.large"]
      min_size       = 2
      max_size       = 2
      desired_size   = 2

      # Specify private subnets to allocate IPs to the Nodes
      # Single AZ node group 
      subnet_ids = ["subnet-1a"]

      labels = {
        WorkerType                    = "ON_DEMAND"
        NodeGroupType                 = "spark"
        # Configure Secondary CIDR Subnet ENIConfig labels for this Node group
        # Allocate 6 secondary subnet CIDRs to this Node group to allocate IPs to the Pods
        # Carrier-Grade NAT (CGN) Range IPs ENIConfig
        "k8s.amazonaws.com/eniConfig" = "eni-config-04-us-west-2a"
        "k8s.amazonaws.com/eniConfig" = "eni-config-07-us-west-2a"
      }
    }
  }
}

```

With this solution, you can now support multiple secondary subnets within the same AZ using VPC CNI custom networking in your Amazon EKS cluster, 
allowing for greater flexibility in resource allocation and workload segregation.

