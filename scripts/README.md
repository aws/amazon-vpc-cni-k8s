## Integration Test Scripts

This package contains shell scripts and libraries used for running integration tests.
This README covers the prerequisites and instructions for running the scripts.

### run-integration-test.sh

`run-integration-test.sh` can run various integration test suites against the current revision in the invoking directory. 

#### Prerequisites:
1. Valid AWS credentials for an account capable of creating EKS clusters
2. Docker installed and able to publish to an ECR repository in your account that can store test images
(run `aws ecr get-login-password --region $REGION | docker login --username AWS --password-stdin ${ACCOUNT_ID}.dkr.ecr.us-west-2.amazonaws.com` to log into Docker)
3. Repositories in your ECR named `amazon-vpc-cni` and `amazon-vpc-init`
4. For performance tests, an S3 bucket in your account to store test results. The name is passed in `PERFORMANCE_TEST_S3_BUCKET_NAME` 

#### Tests
The following tests are valid to run, and setting the respective environment variable to true will run them:
1. CNI Integration Tests - `RUN_CNI_INTEGRATION_TESTS`
2. Conformance Tests - `RUN_CONFORMANCE`
3. Performance Tests - `RUN_PERFORMANCE_TESTS`
4. KOPS Tests - `RUN_KOPS_TEST`
5. Bottlerocket Tests - `RUN_BOTTLEROCKET_TEST`
6. CNI Soap Tests with Ubuntu AMI - `RUN_UBUNTU_TEST`

Example for running performance tests:
```
RUN_CNI_INTEGRATION_TESTS=false RUN_PERFORMANCE_TESTS=true PERFORMANCE_TEST_S3_BUCKET_NAME=cniperftests ./scripts/run-integration-tests.sh
```

#### Other
`run-integration-test.sh` will create a new cluster by default based on the test(s) being run.
1. For KOPS tests, the `kops` binary will be used to create the cluster.
2. For others, the cluster will be created from a template in [scripts/test/config](https://github.com/aws/amazon-vpc-cni-k8s/tree/master/scripts/test/config) 

Note that some tests create clusters with ARM and AMDx86 node groups, so test cases must be able to pass on both. Specifically, images that test cases pull must be able to run on both architectures.

#### Manually running performance tests
The following steps cover how to manually run the performance tests:

1. Copy `scripts/test/config/perf-cluster.yml` to wherever you are driving the test from.
2. Get the AMI ID to use from `aws ssm get-parameter --name /aws/service/eks/optimized-ami/${EKS_CLUSTER_VERSION}/amazon-linux-2/recommended/image_id --region us-west-2 --query "Parameter.Value" --output text`
3. Set the following values in the template:
    - Replace `CLUSTER_NAME_PLACEHOLDER` with a cluster name of your choice.
    - Replace `managedNodeGroups.ami` with the AMI value derived above for your EKS version of choice.
    - Replace `max-pods` and `managedNodeGroups.instanceType` with your values of choice. 
4. Create the cluster with `eksctl create cluster -f $CLUSTER_CONFIG`, where `CLUSTER_CONFIG` is the template you modified above.
5. Deploy the cluster autoscaler with: `kubectl create -f scripts/test/config/cluster-autoscaler-autodiscover.yml`
6. Deploy the metrics server with: `kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml`
7. Apply the latest CNI manifest with `kubectl apply -f config/master/aws-vpc-cni.yaml`
8. Modify the init/main container image in the `aws-node` daemonset with your image of choice.
9. Deploy a performance deployment, i.e. `kubectl apply -f testdata/deploy-130-pods.yaml`
10. Collect statistics
