# ENI Subnet Discovery Test Enhancements

## Overview

This document describes the enhanced integration tests for the improved subnet discovery functionality in VPC CNI.

## Existing Tests (eni_subnet_discovery_test.go)

These tests will continue to pass with backward compatibility:

1. **Basic subnet discovery** - Tests ENI creation in secondary subnet when tagged with `kubernetes.io/role/cni=1`
2. **Subnet discovery disabled** - Verifies ENIs stay in primary subnet when feature is disabled
3. **Wrong tag test** - Verifies ENIs don't use subnets with incorrect tag (`kubernetes.io/role/cn`)
4. **Missing permissions** - Tests fallback to primary subnet when DescribeSubnets permission is missing

## New Tests (eni_subnet_discovery_enhanced_test.go)

### 1. Primary Subnet Exclusion Test
- **Test**: Tag primary subnet with `kubernetes.io/role/cni=0`
- **Expected**: All ENIs created in secondary subnet only
- **Validates**: Primary subnet can be excluded from ENI creation

### 2. Custom Security Groups Test
- **Test**: Create and tag security group with `kubernetes.io/role/cni=1`
- **Expected**: Secondary subnet ENIs use custom security group
- **Validates**: Custom security groups work for secondary subnets

### 3. Cluster-Specific Tags Test
- **Test**: Tag subnet with `kubernetes.io/cluster/<name>=shared`
- **Expected**: Only subnets with matching cluster tag are used
- **Validates**: Multi-cluster subnet isolation works

### 4. Node Restart Behavior Test (TODO)
- **Test**: Restart node with existing pod IPs in excluded primary subnet
- **Expected**: Primary ENI re-included for existing pods
- **Validates**: Backward compatibility during upgrades

### 5. Security Group Fallback Test (TODO)
- **Test**: Remove DescribeSecurityGroups permission
- **Expected**: Secondary ENIs use primary security groups
- **Validates**: Graceful fallback when custom SG discovery fails

## Running the Tests

```bash
# Run all ENI subnet discovery tests
ginkgo -v test/integration/eni-subnet-discovery/

# Run only enhanced tests
ginkgo -v test/integration/eni-subnet-discovery/ -focus="ENI Subnet Discovery Enhanced Tests"

# Run with specific cluster name for cluster tag test
CLUSTER_NAME=my-cluster ginkgo -v test/integration/eni-subnet-discovery/
```

## Key Behaviors Tested

1. **Backward Compatibility**: Primary subnet included by default (no tag)
2. **Explicit Exclusion**: Primary subnet excluded with tag value "0"
3. **Secondary Subnet Opt-in**: Secondary subnets require tag value != "0"
4. **Custom Security Groups**: Secondary subnets can use different security groups
5. **Cluster Isolation**: Subnets can be restricted to specific clusters
6. **Graceful Fallbacks**: System handles missing permissions gracefully 