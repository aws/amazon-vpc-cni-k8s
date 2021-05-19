## CNI Integration Test Suites

The package contains automated integration tests suites for `amazon-vpc-cni-k8s` .

### Prerequisites
The integration test requires 
- At least 2 nodes in a node group.
- Nodes in the nodegroup shouldn't have existing pods.
- Ginkgo installed on your environment. To install `go get github.com/onsi/ginkgo/ginkgo`
- Supports instance types having at least 3 ENIs and 16+ Secondary IPv4 Addresses across all ENIs.

#### Testing
Set the environment variables that will be passed to Ginkgo script. If you want to directly pass the arguments you can skip to next step.
```
CLUSTER_NAME=<eks-cluster-name>
VPC_ID=<vpc-id>
KUBECONFIG=<path-to-kubeconfig>
AWS_REGION=<cluster-region>
NG_NAME_LABEL_KEY=<ng-name-label-tag-on-ec2>
# Example, NG_NAME_LABEL_KEY="eks.amazonaws.com/nodegroup"
NG_NAME_LABEL_VAL=<ng-name-label-tag-value-on-ec2>
# Example, NG_NAME_LABEL_VAL="nodegroup-name"
```

To run the test switch to the integration folder. For instance running the cni integration test from root of the project.
```bash
cd test/integration-new/cni
```
Run Ginkgo test suite
```bash
ginkgo -v --failOnPending -- \
 --cluster-kubeconfig=$KUBECONFIG \
 --cluster-name=$CLUSTER_NAME \
 --aws-region=$AWS_REGION \
 --aws-vpc-id=$VPC_ID \
 --ng-name-label-val=$NG_NAME_LABEL_KEY \
 --ng-name-label-val=$NG_NAME_LABEL_VAL
```

### cni-metrics-helper
The CNI Metrics Helper Integration test uses helm to install the cni-metrics-helper. The helm charts are present in local test directory and if needed can be published to a repository.

In order to test a custom image you need pass the following tags along with the tags discussed above.
```
--cni-metrics-helper-image-repo=<image-repository>
--cni-metrics-helper-image-tag=<image-tag>
```

*IMPORTANT*: The CNI Metric test is suitable for release testing of new CNI Metrics Helper manifest only if the manifest and the local helm charts are in sync.

### Future Work
Currently the package is named as `integraiton-new` because we already have `integration` directory with existing Ginkgo test cases with a separate `go.mod`. Once the older package is completely deprecated we will rename this package to `integration`.





