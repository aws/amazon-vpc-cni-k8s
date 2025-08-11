## amazon-vpc-cni-k8s Test Framework
The test framework consists of integration tests using the Ginkgo framework invoked manually and using collection of bash scripts using GitHub Workflow and Prow(not publicly available).

### Types of Tests

#### Pull Request Tests
Runs on each Pull Request, verifies the new code changes don't introduce any regression. Given the entire test suite may span for long duration, run only the integration tests.

#### Nightly Integration Tests
Runs the entire test suite every night using the current GitHub build to catch regression.

#### Canary Tests
Canary tests run frequently, multiple times in a day on live production environment. Given all integration tests run spans for hours we can only run a limited set of tests of most important features along with tests that have dependencies like Load Balancer Service Creation. These test runs are not publicly accessible at this moment.

Ginkgo Focus: [CANARY]

### Smoke Tests
Smoke test provide fail early mechanism by failing the test if basic functionality doesn't work. This can be used as a pre-requisite for the running the much longer Integration tests.

Ginkgo Focus: [SMOKE]

# KOPS
    * set RUN_KOPS_TEST=true
    * WARNING: will occassionally fail/flake tests, try re-running test a couple times to ensure there is a 
    
# Bottlerocket
    * set RUN_BOTTLEROCKET_TEST=true

# Ubuntu
    * set RUN_UBUNTU_TEST=true
    * Includes pod churn tests to validate CNI stability on Ubuntu nodes

## How to Manually delete k8s tester Resources (order of deletion)
Cloudformation - (all except cluster, vpc)
EC2 - load balancers, key pair
VPC - Nat gateways, Elastic IPs(after a minute), internet gateway
Cloudformation - cluster
EC2 - network interfaces, security groups
VPC - subnet, route tables
Cloudformation - cluster, vpc(after cluster deletes)
S3 - delete bucket

#### Work In Progress
- Run Upstream Conformance tests as part of Nightly Integration tests.
- Run all integration tests as part of Nightly Integration tests.
- Run all integration tests on each Pull Request.
