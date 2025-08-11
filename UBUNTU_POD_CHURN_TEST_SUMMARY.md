# Ubuntu Pod Churn Test Implementation

## Overview
Added a comprehensive pod churn test specifically for Ubuntu AMI to validate the stability and reliability of the VPC CNI when running on Ubuntu nodes.

## Files Added/Modified

### 1. `test/integration/cni/pod_churn_ubuntu_test.go`
- **Purpose**: Tests pod churn scenarios on Ubuntu nodes
- **Features**:
  - Rapid pod creation and deletion cycles (10 iterations, 20 pods each)
  - Network connectivity validation between pods
  - IP allocation consistency testing
  - CNI health verification after churn cycles
  - Ubuntu-specific node selector targeting

### 2. `test/README.md`
- **Updated**: Added documentation for the Ubuntu pod churn test
- **Change**: Added note about pod churn tests being included in Ubuntu test suite

## Test Structure

### Main Test Cases:
1. **Rapid Pod Creation/Deletion Cycles**
   - Creates and deletes 20 pods per iteration for 10 iterations
   - Validates pod IP assignment and network connectivity
   - Ensures proper cleanup between iterations

2. **IP Allocation Consistency**
   - Tracks IP addresses across multiple cycles
   - Validates no IP conflicts occur
   - Ensures proper IP cleanup and reallocation

### Key Features:
- **Ubuntu Node Targeting**: Uses `kubernetes.io/os: linux` node selector
- **Network Validation**: Tests TCP connectivity between pods using netcat
- **Resource Cleanup**: Ensures all resources are properly cleaned up
- **Error Handling**: Comprehensive error checking and validation
- **Integration Tags**: Properly tagged for integration test execution

## Test Execution

### Environment Variables:
- Set `RUN_UBUNTU_TEST=true` to enable Ubuntu-specific tests
- Test runs as part of the Ubuntu test suite in CI/CD

### Test Labels:
- `[UBUNTU_CHURN_TEST]` - Main test context label
- Pod labels: `churn-test=ubuntu`, `iteration=N`, `cycle=N`

### Duration:
- Approximately 10-15 minutes depending on cluster performance
- Configurable iterations and pod counts for different test scenarios

## Benefits:
1. **Stability Validation**: Ensures CNI remains stable under high pod churn
2. **Ubuntu-Specific Testing**: Validates CNI behavior on Ubuntu nodes specifically  
3. **IP Management**: Tests IP allocation/deallocation under stress
4. **Network Reliability**: Validates network connectivity throughout churn cycles
5. **Resource Management**: Ensures proper cleanup and resource management

## Integration:
- Integrates with existing test framework
- Uses standard test utilities and manifests
- Follows established patterns from other CNI tests
- Compatible with existing CI/CD pipeline for Ubuntu tests
