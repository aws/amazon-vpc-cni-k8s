##CNI E2E Test Suites

The package contains e2e tests suites for `amazon-vpc-cni-k8s` .

###Prerequisites
- Custom Networking Test
  - No existing node group should be present the test creates new self managed node group with the reduced MAX_POD value.
  - Need to pass 'custom-networking-cidr-range' flag with VPC CIDR that doesn't conflict with an existing one. So if existing VPC cidr is 192.168.0.0/16 then you can use something like 'custom-networking-cidr-range=192.169.0.0/16'. You can go to your cluster VPC to check existing CIDRs

- Security Group For Pods Test
  - EKS Cluster should be v1.16+. This tests creates an additional Trunk ENI on all Nitro based instance present in the cluster. This could interfere with running integration test that test WARM_ENI_TARGET. For this reasons the test should either be run without any node group present in the cluster or at the very end.
- Snat Test
  - EKS Cluster should have atleast 1 private subnet and atleast 1 public subnet. These tests modifies the SNAT related variabls in aws-node, validates the IP table SNAT rules and checks for Internet Connectivity. 

####Testing
Set the environment variables that will be passed to Ginkgo script. If you want to directly pass the arguments you can skip to next step.
```
CLUSTER_NAME=<eks-cluster-name>
VPC_ID=<vpc-id>
KUBECONFIG=<path-to-kubeconfig>
AWS_REGION=<cluster-region>
# Optional endpooint variable
EKS_ENDPOINT=<eks-endpoint>
```

To run the test switch to the integration folder. For instance running the custom-networking test from root of the project.
```bash
cd test/e2e/custom-networking
```

Run Ginkgo test suite
```bash
ginkgo -v --fail-on-pending -- \
 --cluster-kubeconfig=$KUBECONFIG \
 --cluster-name=$CLUSTER_NAME \
 --aws-region=$AWS_REGION \
 --aws-vpc-id=$VPC_ID \
 --eks-endpoint=$EKS_ENDPOINT
```
