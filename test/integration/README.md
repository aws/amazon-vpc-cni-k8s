## CNI Integration Test Suites

This package contains automated integration tests suites for `amazon-vpc-cni-k8s`.

### Prerequisites
The integration tests require:
- At least 2 nodes in a node group.
- Nodes in the nodegroup shouldn't have existing pods.
- Ginkgo installed on your environment. To install, run `go install github.com/onsi/ginkgo/v2/ginkgo`.
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

To run the test switch to the integration folder. For instance, running the cni integration test from root of the project.
```bash
cd test/integration/cni
```
Run Ginkgo test suite
```bash
ginkgo -v --fail-on-pending -- \
 --cluster-kubeconfig=$KUBECONFIG \
 --cluster-name=$CLUSTER_NAME \
 --aws-region=$AWS_REGION \
 --aws-vpc-id=$VPC_ID \
 --ng-name-label-key=$NG_NAME_LABEL_KEY \
 --ng-name-label-val=$NG_NAME_LABEL_VAL
```

### cni-metrics-helper

> #### Prerequisites:
>
> This test expects CNIMetricsHelperPolicy to be present in the test account. Create the policy with below permissions in the test account:

```
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "cloudwatch:PutMetricData"
            ],
            "Resource": "*"
        }
    ]
}
```

The CNI Metrics Helper Integration test uses helm to install the cni-metrics-helper. The helm charts are present in local test directory and if needed can be published to a repository.

In order to test a custom image you need pass the following tags along with the tags discussed above.
```
--cni-metrics-helper-image-repo=<image-repository>
--cni-metrics-helper-image-tag=<image-tag>
```

*IMPORTANT*: The CNI Metric test is suitable for release testing of new CNI Metrics Helper manifest only if the manifest and the local helm charts are in sync.

### IPv6

`ipv6` test suite helps validate basic ipv6 traffic flows and will also verify network setup (on the node) in ipv6 mode.

*IMPORTANT*: Should use an IPv6 cluster with Prefix Delegation enabled. VPC CNI only supports IPv6 mode with Prefix Delegation.

### Custom Networking tests (custom_networking)

Custom networking tests validate use of the `AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG` environment variable.

Test info:
  - Pass `custom-networking-cidr-range` flag with *allowed* VPC CIDR that does not conflict with an existing one. So if existing VPC CIDR is `192.168.0.0/16`, you can use `custom-networking-cidr-range=100.64.0.0/16`. You can go to your cluster VPC to check existing/allowed CIDRs.

### SNAT tests (snat)

SNAT tests cover pod source NAT behavior with various deployment scenarios.

Test info:
  - EKS Cluster should have at least one private subnet and at least one public subnet. These tests modify the SNAT related variables in `aws-node` pod, validate the IP table SNAT rules, and check for Internet Connectivity.

### Calico tests (calico)

`calico` helps validate compatibility with calico network policies. It does so by running the Calico Stars policy demo.

### Security Groups For Pods tests (pod_eni)

`pod_eni` test suite validates Security Group for Pods implementation from VPC CNI perspective.

Test info:
  - Requires at least one Nitro-based instance.
  - EKS Cluster should be v1.16+. This tests creates an additional Trunk ENI on all Nitro-based instances present in the cluster.

### Custom Networking and Security Groups for Pods tests (custom_networking_sgpp)

`custom_networking_sgpp` test suite validates the combination of Custom Networking and Security Groups for Pods.

Test info:
  - Pass `custom-networking-cidr-range` flag with *allowed* VPC CIDR that does not conflict with an existing one. So if existing VPC CIDR is `192.168.0.0/16`, you can use `custom-networking-cidr-range=100.64.0.0/16`. You can go to your cluster VPC to check existing/allowed CIDRs.
  - Requires at least one Nitro-based instance.
  - EKS Cluster should be v1.16+. This tests creates an additional Trunk ENI on all Nitro-based instances present in the cluster.

### Multus tests (multus)
These tests require multus to be deployed to your cluster using the [manifest](https://raw.githubusercontent.com/aws/amazon-vpc-cni-k8s/master/config/multus/v3.9.2-eksbuild.1/aws-k8s-multus.yaml) file. Instead test can be triggered by running `run-multus-tests.sh` located under scripts directory. This script installs the multus manifest first and then runs the the ginkgo test suite.
You can optionally provide multus tag to install the manifest. If not provided then it will use the default tag

```
KUBE_CONFIG_PATH=/Users/cgadgil/.kube/config CLUSTER_NAME=eks-MultusInfra REGION=us-west-2 SKIP_MAKE_TEST_BINARIES=true ./scripts/run-multus-tests.sh v3.7.2-eksbuild.2

Running tests with the following variables
KUBE_CONFIG_PATH:  /Users/cgadgil/.kube/config
CLUSTER_NAME: eks-MultusInfra
REGION: us-west-2
ENDPOINT:
skipping making ginkgo test binaries
loading cluster details eks-MultusInfra
Installing latest multus manifest with tag: v3.7.2-eksbuild.2
customresourcedefinition.apiextensions.k8s.io/network-attachment-definitions.k8s.cni.cncf.io unchanged
clusterrole.rbac.authorization.k8s.io/multus unchanged
clusterrolebinding.rbac.authorization.k8s.io/multus unchanged
serviceaccount/multus unchanged
configmap/multus-cni-config unchanged
daemonset.apps/kube-multus-ds unchanged
Running multus ginkgo tests
Running Suite: Multus Setup Suite
=================================
Random Seed: 1647974995
Will run 1 of 1 specs

STEP: Check if Multus Daemonset is Ready
...
...

Ran 1 of 1 Specs in 6.340 seconds
SUCCESS! -- 1 Passed | 0 Failed | 0 Pending | 0 Skipped
PASS

Ginkgo ran 1 suite in 14.379316223s
Test Suite Passed
all tests ran successfully in 0 minutes and 27 seconds
```

### Running release tests with scripts/run-cni-release-tests.sh
`run-cni-release-tests.sh` will run cni, ipamd, and cni-metrics-helper (integration tests)[https://github.com/aws/amazon-vpc-cni-k8s/tree/master/test/integration]. The script _does not_ create a test cluster, instead it will run the test on cluster specified via variables required in the script. The tests are run on the vpc-cni version installed on the cluster(it does not upgrade/install any specific vpc-cni version). See script `update-cni-images.sh` to update the test cluster with required cni version before running the tests.

## Development of New Integration Tests

This section is written to give a high level overview for the process of developing integration tests in the VPC CNI repo.

### Test helpers

- `amazon-vpc-cni-k8s/test/framework` : This is the main folder that has the modules that will be used in writing tests. It has a number of other folder like `controller` , `helm`, `resources` and `utils` that provide different use cases. A very useful folder is `resources` (explained in detail below).

- `amazon-vpc-cni-k8s/test/framework/resources` It has the following sub-folders with their functionality listed:

- `agent`: Used to test traffic by creating multiple server pods using a deployment and multiple client pods using a Job. Each client Pod tests connectivity to each Server Pod.

- `aws`: Used for aws functionality related to autoscaling node groups, cloudformation, cloudwatch, ec2, eks, iam and more.

- `k8s`: The k8s folder has modules that comprise of the building blocks for writing the tests. It has the following subfolders:
  - `manifest`: This folder has modules for building pods, services, jobs, deployments, containers and more. These are responsible for determining the structure, say for instance the build configuration of the pod.
  - `resources`: This folder has modules for actual creation of the k8s elements built by the modules in the manifest folder. Not just creation/deletion, it has other useful functionalities related to the k8s element in consideration. For instance, getting status and logs for the element or functionalities like exec for a pod k8s element.
  - `utils`: Has helper utilities related to node, daemonset and containers.

### Organization of test folders

The test folders are located at `amazon-vpc-cni-k8s/tree/master/test/integration` It has the following sub-folders:
 - calico
 - cni
 - custom-networking
 - ipamd
 - ipv6
 - metrics-helper
 - pod-eni
 - snat

The ginkgo test for any component has generally two main components:
- `ginkgo suite file`: Every ginkgo suite file will have `RegisterFailHandler` and `RunSpecs`. A Ginkgo test signals failure by calling Ginkgo’s Fail function passed to RegisterFailHandler. RunSpec tells Ginkgo to start the test suite. Running ginkgo inside the sub-folder containing the test suite should trigger the ```RunSpecs``` function in the suite.

- `ginkgo test files`: By default, test files in the same folder as ginkgo suite file will be run on the trigger of the `RunSpecs` function in the ginkgo test suite.

### Adding new test folder

Say for instance, the cni test and suite files in the cni folder has functionality related to CNI component in VPC CNI as you would expect. If you want to add a test that does not belong to any of the modules in the integration folder, you will have to create a new folder structure as below
- `integration`
  - ```new_component_test_xyz```
       - ```new_component_test_xyz/new_component_xyz_suite_test.go```)
       - ```new_component_test_xyz/xyz_test_1.go```
       - ```new_component_test_xyz/xyz_test_2.go```)
       - ```...```
  - ```cni```
  - ```...```

### Structure of sample test suite:
```cni/pod_networking_suite_test.go```

#### Logic Components

- ```BeforeSuite``` : All common steps that should be performed before the suite are added here. In the sample BeforeSuite below, we can  see a few prerequistes for the tests that run under the suite, like namespace creation and setting of env variables like WARM_IP_TARGET.
- ```AfterSuite``` : All common steps that should be performed after the suite are added here. In the sample AfterSuite below, we can  see cleanup to be followed after running the tests under the suite like namespace deletion and resetting of env variables.

```go
package cni

import (
        // fmt is imported for printing
	"fmt"
        // testing is imported as it is the original go testing module used by ginkgo
	"testing"
        // The below folders are similar to the ones discussed above
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
        //ginkgo and the assertion library: gomega are imported below
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
        //v1 imported for Node libraries
	v1 "k8s.io/api/core/v1"
)

//Global variables for the suite are defined here
const InstanceTypeNodeLabelKey = "beta.kubernetes.io/instance-type"

var f *framework.Framework
...

//The function below is the starter function for running tests
//for each suite and attaching a fail handler for the same
func TestCNIPodNetworking(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CNI Pod Networking Suite")
}

//The following function has checks and setup needed before running the suite.
var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

        // The Sequence of By and Expect are provided by the omega package and
        // ensure the correct functionality by providing assertions at every step

	By("creating test namespace")
	f.K8sResourceManagers.NamespaceManager().
		CreateNamespace(utils.DefaultTestNamespace)

	...
        ...

	// Set the WARM_ENI_TARGET to 0 to prevent all pods being scheduled on secondary ENI
	k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
		"aws-node", map[string]string{"WARM_IP_TARGET": "3", "WARM_ENI_TARGET": "0"})
})

//The following function has checks and setup needed after running the suite.
var _ = AfterSuite(func() {
	By("deleting test namespace")
	f.K8sResourceManagers.NamespaceManager().
		DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)

	k8sUtils.UpdateEnvVarOnDaemonSetAndWaitUntilReady(f, "aws-node", "kube-system",
		"aws-node", map[string]string{
			AWS_VPC_ENI_MTU:            "9001",
			AWS_VPC_K8S_CNI_VETHPREFIX: "eni",
		},
		map[string]struct{}{
			"WARM_IP_TARGET":  {},
			"WARM_ENI_TARGET": {},
		})
})
```

### Structure of sample test corresponding to a suite:
```cni/pod_traffic_test_PD_enabled.go```

#### Logic Components

- ```It```: Individual spec specified by It. It is the innermost component that holds the core testing logic. The other components listed below are hierarchically arranged before and after It in order to provision/deprovision the setup required to run the individual spec (It). In the sample test below, our It tests for 99+% traffic success rate between client and server pods.
- ```Describe``` : This block is used describe the individual behaviors of code. In the sample test below, we try to describe a behaviour of pod traffic with PD (Prefix delegation) enabled.
- ```Context``` : Context block is used to execute the behavior used by Describe block under different scenarios. We can have different Context or scenarios for our sample test below, like testing TCP pod traffic or UDP pod traffic.
- ```JustBeforeEach``` : Executed immediately before each test, however following the execution order from outside blocks to inside blocks before an It(spec) in case of multipe JustBeforeEach blocks. We can see that in the JustBeforeEach function below, we setup server deployment and enable PD just before we run the It.
- ```JustAfterEach``` : Executed immediately after each test, however following the execution order from inside blocks to outside blocks after an It(spec) in case of multipe JustAfterEach blocks. We can see that in the JustAfterEach function below, we reset PD to false after running It.
- ```BeforeEach``` : Executed (not immediately) before each test, however following the execution order from outside blocks to inside blocks before an It(spec) in case of multipe BeforeEach blocks.
- ```AfterEach``` : Executed (not immediately) after each test, however following the execution order from inside blocks to outside blocks after an It(spec) in case of multipe AfterEach blocks.

Each of the above components are arranged hierarchically in a way that makes most sense for abstracting the common logic from the rest of the code.

Every ```BeforeEach``` precedes every ```JustBeforeEach``` in execution before execution of an It.
Every ```JustAfterEach``` precedes every ```AfterEach``` in execution after execution of an It.

```go
package cni

import (
        // Imports similar to above test suite found here
	...
)

// This blocks is used describe the individual behaviors of code
var _ = Describe("Test pod networking with prefix delegation enabled", func() {
	var (
	    // List of global variables used for the tests below
		// The Pod labels for client and server in order to retrieve the
		// client and server Pods belonging to a Deployment/Jobs
		labelKey                = "app"
		serverPodLabelVal       = "server-pod"
		clientPodLabelVal       = "client-pod"
		serverDeploymentBuilder *manifest.DeploymentBuilder
		// Value for the Environment variable ENABLE_PREFIX_DELEGATION
		enableIPv4PrefixDelegation string
	)

	JustBeforeEach(func() {
		By("creating deployment")
		serverDeploymentBuilder = manifest.NewDefaultDeploymentBuilder().
			Name("traffic-server").
			NodeSelector(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)

		By("Set PD")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
			utils.AwsNodeNamespace, utils.AwsNodeName,
			map[string]string{"ENABLE_PREFIX_DELEGATION": enableIPv4PrefixDelegation})
	})

	JustAfterEach(func() {
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
			utils.AwsNodeNamespace, utils.AwsNodeName,
			map[string]string{"ENABLE_PREFIX_DELEGATION": "false"})
	})

    // Context block is used to execute the behavior used
    // by Describe block under different scenarios
	Context("when testing TCP traffic between client and server pods", func() {
		BeforeEach(func() {
			enableIPv4PrefixDelegation = "true"
		})

        // Below is example of individual spec specified by It
		It("should have 99+% success rate", func() {
			trafficTester := agent.TrafficTest{
				Framework:                      f,
				TrafficServerDeploymentBuilder: serverDeploymentBuilder,
				...
				ClientPodLabelKey:              labelKey,
				ClientPodLabelVal:              clientPodLabelVal,
			}

			successRate, err := trafficTester.TestTraffic()
			Expect(err).ToNot(HaveOccurred())
			Expect(successRate).Should(BeNumerically(">=", float64(99)))
		})
	})

	// Similarly we can also test for UDP traffic between
    // client and server pods in another context here
})
```

More info can be found here https://github.com/onsi/ginkgo

### Troubleshooting Test Failure

Everytime you run a ginkgo test suite, you will get stats on number of tests passed/failed/pending/skipped as follows:

```
<PASS/FAIL>! -- <> Passed | <> Failed | <> Pending | <> Skipped
```

In case of an error, the error message will be printed in ginkgo error stack. For instance, in case kubeconfig is not correctly set, you will get an error message similar to below:

```
  Unexpected error:
      <*errors.fundamental | xxxx>: {
          msg: "kubeconfig must be set!",
          stack: [xxx,xxx,...],
      }
      kubeconfig must be set!
  occurred

  ...
  Test Panicked
  runtime error: invalid memory address or nil pointer dereference
 ..

  Full Stack Trace
  ..
Test Suite Failed
```

For additional [Debugging Help](./Troubleshooting.md)
