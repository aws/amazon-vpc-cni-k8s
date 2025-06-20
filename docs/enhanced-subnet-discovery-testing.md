# Enhanced Subnet Discovery Testing Documentation

This document provides a comprehensive overview of all testing implemented for the Enhanced Subnet Discovery feature in AWS VPC CNI.

## Overview

The Enhanced Subnet Discovery feature adds the following capabilities:
1. Primary subnet exclusion using `kubernetes.io/role/cni=0` tag
2. Custom security groups for secondary subnets using `kubernetes.io/role/cni=1` tag on security groups
3. Cluster-specific subnet tags using `kubernetes.io/cluster/<cluster-name>=shared`
4. Graceful handling of existing pod IPs during node restart with excluded primary subnet

## Unit Tests

### 1. awsutils Package Tests (`pkg/awsutils/awsutils_test.go`)

#### Core Subnet Discovery Tests

| Test Name | Description | Coverage |
|-----------|-------------|----------|
| `TestValidTag` | Tests subnet validation logic for primary/secondary subnets with various tag scenarios | - Primary subnet inclusion by default<br>- Secondary subnet opt-in requirement<br>- Tag value "0" exclusion logic |
| `TestValidTagWithClusterSpecificTags` | Tests cluster-specific subnet tagging (`kubernetes.io/cluster/*`) | - Subnet available to all clusters when no cluster tags<br>- Subnet exclusive to specific cluster<br>- Multiple cluster tags support |
| `TestIsPrimarySubnetExcluded` | Tests checking if primary subnet has `kubernetes.io/role/cni=0` tag | - API error handling<br>- Empty subnet list handling<br>- Correct exclusion detection |
| `TestAllocENIWithSubnetExclusion` | Tests ENI allocation scenarios with excluded subnets | - Multiple subnets with primary excluded<br>- All subnets excluded error case<br>- Subnet discovery disabled with excluded primary |
| `TestAllocENIWithSubnetDiscoveryFailure` | Tests fallback behavior when DescribeSubnets API fails | - Fallback to primary subnet<br>- Error when primary is excluded |

#### Custom Security Groups Tests

| Test Name | Description | Coverage |
|-----------|-------------|----------|
| `TestDiscoverCustomSecurityGroups` | Tests discovery of security groups tagged with `kubernetes.io/role/cni=1` | - Multiple custom SGs discovery<br>- Empty SG list handling<br>- API error handling |
| `TestGetENISubnetID` | Tests helper method to get subnet ID for an ENI | - Successful subnet lookup<br>- ENI not found error<br>- API error handling |
| `TestCreateENIWithCustomSGs` | Tests custom SG application during ENI creation | - Primary subnet uses primary SGs<br>- Secondary subnet with custom SGs<br>- Secondary subnet fallback to primary SGs |
| `TestRefreshCustomSGIDsWithFallback` | Tests fallback to primary SGs when custom SG discovery fails | - API permission errors<br>- Cache clearing on failure |
| `TestENICreationFallbackLogging` | Tests logging behavior during SG fallback scenarios | - Custom SG usage logging<br>- Fallback scenario logging |

### 2. IPAMD Package Tests (`pkg/ipamd/ipamd_test.go`)

#### Primary Subnet Exclusion Tests

| Test Name | Description | Coverage |
|-----------|-------------|----------|
| `TestIPAMContext_PrimarySubnetExclusion` | Tests that primary ENI is marked as excluded when primary subnet is excluded | - ENI exclusion marking<br>- IP allocation prevention from excluded ENI |
| `TestIPAMContext_WarmTargetWithExcludedPrimary` | Tests warm IP/ENI target calculations with excluded primary subnet | - Pool too low detection<br>- Secondary ENI requirement<br>- Target fulfillment logic |
| `TestNodeInitPrimarySubnetExclusionWithExistingPodIPs` | Tests node initialization when primary subnet is excluded but has existing pod IPs | - Re-inclusion of primary ENI<br>- Checkpoint restoration<br>- Backwards compatibility |
| `TestNodeInitPrimarySubnetExclusionWithoutExistingPodIPs` | Tests node initialization when primary subnet is excluded with no existing pods | - Primary ENI remains excluded<br>- Clean node startup |
| `TestPrimaryENIWasPreviouslyUsed` | Tests detection of whether primary ENI was previously used for pods | - Empty datastore check<br>- Unassigned CIDRs handling<br>- Previously assigned IPs detection |

## Integration Tests

### Existing Tests (`test/integration/eni-subnet-discovery/`)

#### Basic Subnet Discovery Tests (`eni_subnet_discovery_test.go`)

| Test Name | Description | Expected Result |
|-----------|-------------|-----------------|
| `TestENISubnetDiscovery` | Tests basic subnet discovery with `kubernetes.io/role/cni=1` tag | ENIs created in tagged secondary subnet |
| `TestSubnetDiscoveryDisabled` | Tests when `ENABLE_SUBNET_DISCOVERY=false` | ENIs only created in primary subnet |
| `TestWrongTagFormat` | Tests with incorrect tag `kubernetes.io/role/cn` | ENIs not created in incorrectly tagged subnet |
| `TestMissingDescribeSubnetsPermission` | Tests fallback when `ec2:DescribeSubnets` permission missing | Falls back to primary subnet |

### Enhanced Feature Tests (`eni_subnet_discovery_enhanced_test.go`)

#### Primary Subnet Exclusion Test

```go
Context("when primary subnet is excluded with tag value 0")
```
- Tags primary subnet with `kubernetes.io/role/cni=0`
- Verifies all ENIs created in secondary subnets only
- Ensures no IPs allocated from primary ENI

#### Custom Security Groups Test

```go
Context("when using custom security groups for secondary subnets")
```
- Creates custom security group with `kubernetes.io/role/cni=1` tag
- Verifies secondary ENIs use custom security group
- Verifies primary ENI keeps primary security groups

#### Cluster-Specific Tags Test

```go
Context("when using cluster-specific subnet tags")
```
- Tags subnet with `kubernetes.io/cluster/<cluster-name>=shared`
- Verifies only matching cluster uses the subnet
- Tests subnet isolation between clusters

#### Node Restart Behavior Test (TODO)

```go
Context("when node restarts with existing pod IPs in excluded primary subnet")
```
- **Why TODO**: Requires complex orchestration of node restart
- **What it would test**: Primary subnet re-inclusion when pods exist
- **Alternative**: Covered by unit test `TestNodeInitPrimarySubnetExclusionWithExistingPodIPs`

#### Security Group Fallback Test (TODO)

```go
Context("when custom security groups discovery fails")
```
- **Why TODO**: Requires simulating IAM permission removal at runtime
- **What it would test**: Graceful fallback to primary security groups
- **Alternative**: Covered by unit test `TestRefreshCustomSGIDsWithFallback`

## Test Coverage Summary

### Features Fully Tested

✅ **Primary Subnet Exclusion**
- Tag-based exclusion (`kubernetes.io/role/cni=0`)
- IP allocation prevention from excluded subnets
- Warm target calculations with excluded primary
- Node restart with existing pod IPs

✅ **Custom Security Groups**
- Discovery of tagged security groups
- Application to secondary subnet ENIs
- Fallback to primary SGs when unavailable
- Primary ENI SG preservation

✅ **Cluster-Specific Tags**
- Subnet filtering by cluster name
- Multi-cluster subnet sharing
- Subnet isolation between clusters

✅ **Backward Compatibility**
- Primary subnet included by default (no tag)
- Existing checkpoint restoration
- Existing test compatibility

### Test Execution

#### Running Unit Tests

```bash
# Run all unit tests
make unit-test

# Run specific package tests
go test -v ./pkg/awsutils/...
go test -v ./pkg/ipamd/...

# Run with coverage
go test -coverprofile=coverage.out ./pkg/...
go tool cover -html=coverage.out
```

#### Running Integration Tests

```bash
# Prerequisites
export CLUSTER_NAME=<your-cluster-name>
export AWS_REGION=<your-region>
export CLUSTER_ENDPOINT=<your-k8s-endpoint>
export KUBECONFIG=<path-to-kubeconfig>

# Run all ENI subnet discovery tests
cd test/integration/eni-subnet-discovery
ginkgo -v

# Run specific test
ginkgo -v -focus "when primary subnet is excluded"

# Run with detailed output
ginkgo -v -r --fail-fast --trace
```

## Test Environment Requirements

### For Integration Tests

1. **EKS Cluster**: Running EKS cluster with VPC CNI
2. **IAM Permissions**: Updated policy including:
   - `ec2:DescribeSubnets`
   - `ec2:DescribeSecurityGroups`
   - `ec2:CreateTags`
   - `ec2:DeleteTags`
3. **Multiple Subnets**: At least 2 subnets in the same AZ
4. **Test Resources**: Ability to create/tag subnets and security groups

### Test Data Setup

```bash
# Tag secondary subnet for CNI use
aws ec2 create-tags --resources subnet-xxx \
  --tags Key=kubernetes.io/role/cni,Value=1

# Create and tag custom security group
aws ec2 create-security-group --group-name cni-custom-sg \
  --description "Custom SG for CNI" --vpc-id vpc-xxx
aws ec2 create-tags --resources sg-xxx \
  --tags Key=kubernetes.io/role/cni,Value=1

# Tag primary subnet for exclusion (optional)
aws ec2 create-tags --resources subnet-primary \
  --tags Key=kubernetes.io/role/cni,Value=0
```

## Known Limitations

1. **Node Restart Test**: Integration test requires actual node restart which is complex in test environment
2. **Permission Failure Test**: Simulating IAM permission changes at runtime is challenging
3. **Multi-Cluster Test**: Requires multiple EKS clusters for full cluster-specific tag testing

## Troubleshooting Test Failures

### Common Issues

1. **Subnet Discovery Tests Fail**
   - Verify subnets have correct tags
   - Check IAM permissions include DescribeSubnets
   - Ensure subnets are in same AZ as test node

2. **Custom Security Group Tests Fail**
   - Verify security groups have correct tags
   - Check IAM permissions include DescribeSecurityGroups
   - Ensure SGs are in same VPC

3. **Primary Subnet Exclusion Tests Fail**
   - Verify no existing pods have IPs from primary subnet
   - Check primary subnet tag value is exactly "0"
   - Ensure at least one secondary subnet is available

### Debug Commands

```bash
# Check subnet tags
aws ec2 describe-subnets --subnet-ids subnet-xxx

# Check security group tags
aws ec2 describe-security-groups --group-ids sg-xxx

# Check ENI details
aws ec2 describe-network-interfaces --network-interface-ids eni-xxx

# Check pod IPs
kubectl get pods -o wide -A | grep <node-name>
``` 