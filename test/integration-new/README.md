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
 --ng-name-label-key=$NG_NAME_LABEL_KEY \
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

### IPv6

`ipv6` test suite helps validate basic ipv6 traffic flows and will also verify network setup (on the node) in ipv6 mode.

*IMPORTANT*: Should use an IPv6 cluster with Prefix Delegation enabled. VPC CNI only supports IPv6 mode with Prefix Delegation.

### Future Work
Currently the package is named as `integration-new` because we already have `integration` directory with existing Ginkgo test cases with a separate `go.mod`. Once the older package is completely deprecated we will rename this package to `integration`.




## Development of New Integration Tests

This section is written to give a high level overview for the process of developing integration tests in the VPC CNI repo. 

### Test helpers

- ```amazon-vpc-cni-k8s/test/framework``` : This is the main folder that has the modules that will be used in writing tests. It has number of other folder like ```controller``` , ```helm```, ```resources``` and ```utils``` providing different use cases. A very useful folder is ```resources``` and is explained in detail below. 

- ```amazon-vpc-cni-k8s/test/framework/resources``` It has the following sub-folders with their functionality listed:

- ```agent```: Used to test traffic by creating multiple server pods using a deployment and multiple client pods using a Job. Each client Pod tests connectivity to each Server Pod.

- ```aws```: Used for aws functionality related to autoscaling node groups, cloudformation, cloudwatch, ec2, eks, iam and more.  

- ```k8s```: The k8s folder has modules that comprise of the building blocks for writing the tests. It has the following subfolders:
  - ```manifest```: This folder has modules for building pods, services, jobs, deployments, containers and more. These are responsible for determining the structure, say for instance the build configuration of the pod. 
  - ```resources```: This folder has modules for actual creation of the k8s elements built by the modules in the manifest folder. Not just creation/deletion, it has other useful functionalities related to the k8s element in considersation. For instance, getting status and logs for the element or functionalities like exec for a pod k8s element. 
  - ```utils```: Has helper utilities related to node, daemonset and containers. 


### Organization of test folders

Currently the test folders are located at ```amazon-vpc-cni-k8s/tree/master/test/integration-new``` It has the following sub-folders ```cni```, ```ipamd```, ```ipv6``` and ```metrics-helper```.

The ginkgo test for any component has generally two main components:
- ```ginkgo suite file```: Every ginkgo suite file will have ```RegisterFailHandler``` and ```RunSpecs```. A Ginkgo test signals failure by calling Ginkgo’s Fail function passed to RegisterFailHandler. RunSpec tells Ginkgo to start the test suite. Running ginkgo inside the sub-folder conatining the test suite, should trigger the ```RunSpecs``` function in the suite.

- ```ginkgo test files```: By default, test files in the same folder as ginkgo suite file will be run on the trigger of the ```RunSpecs``` function in the ginkgo test suite. 

### Adding new test folder

Say for instance, The cni test and suite files in the cni folder has functionality related to CNI component in VPC CNI as you would expect. If you want to add a test that does not belong to any of the modules in the integration-new folder, you will have to create a new folder structure as below
- ```integration-new``` 
  - ```new_component_test_xyz``` 
       - ```new_component_test_xyz/new_component_xyz_suite_test.go```)
       - ```new_component_test_xyz/xyz_test_1.go``` 
       - ```new_component_test_xyz/xyz_test_2.go```) 
       - ```...```
  - ```cni``` 
  - ```...```

### Structure of sample test suite 

#### Logic Components

- ```BeforeSuite``` : All common steps that should be performed before the suite are added here.
- ```AfterSuite``` : All common steps that should be performed after the suite are added here.

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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
        //v1 imported for Node libraries
	v1 "k8s.io/api/core/v1"
)

//Global variables for the suite are defined here
const InstanceTypeNodeLabelKey = "beta.kubernetes.io/instance-type"

var f *framework.Framework
var maxIPPerInterface int
var primaryNode v1.Node
var secondaryNode v1.Node
var instanceSecurityGroupID string
var vpcCIDRs []string

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

	By(fmt.Sprintf("getting the node with the node label key %s and value %s",
		f.Options.NgNameLabelKey, f.Options.NgNameLabelVal))
	nodes, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())

	By("verifying more than 1 nodes are present for the test")
	Expect(len(nodes.Items)).Should(BeNumerically(">", 1))

	// Set the primary and secondary node for testing
	primaryNode = nodes.Items[0]
	secondaryNode = nodes.Items[1]

	// Get the node security group
	instanceID := k8sUtils.GetInstanceIDFromNode(primaryNode)
	primaryInstance, err := f.CloudServices.EC2().DescribeInstance(instanceID)
	Expect(err).ToNot(HaveOccurred())

	// This won't work if the first SG is only associated with the primary instance.
	// Need a robust substring in the SGP name to identify node SGP
	instanceSecurityGroupID = *primaryInstance.NetworkInterfaces[0].Groups[0].GroupId

	By("getting the instance type from node label " + InstanceTypeNodeLabelKey)
	instanceType := primaryNode.Labels[InstanceTypeNodeLabelKey]

	By("getting the network interface details from ec2")
	instanceOutput, err := f.CloudServices.EC2().DescribeInstanceType(instanceType)
	Expect(err).ToNot(HaveOccurred())

	// Pods often get stuck due insufficient capacity, so adding some buffer to the maxIPPerInterface
	maxIPPerInterface = int(*instanceOutput[0].NetworkInfo.Ipv4AddressesPerInterface) - 5

	By("describing the VPC to get the VPC CIDRs")
	describeVPCOutput, err := f.CloudServices.EC2().DescribeVPC(f.Options.AWSVPCID)
	Expect(err).ToNot(HaveOccurred())

	for _, cidrBlockAssociationSet := range describeVPCOutput.Vpcs[0].CidrBlockAssociationSet {
		vpcCIDRs = append(vpcCIDRs, *cidrBlockAssociationSet.CidrBlock)
	}

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

### Structure of sample test corresponding to a suite


#### Logic Components

- ```It```: Individual spec specified by It. It is the innermost component that holds the core testing logic. The other components listed below are hierarchically arranged before and after It in order to provision/deprovision the setup required to run the individual spec (It).
- ```Describe``` : This blocks is used describe the individual behaviors of code.
- ```Context``` : Context block is used to execute the behavior used by Describe block under different scenarios 
- ```JustBeforeEach``` : Executed immediately before each test, however following the execution order from outside blocks to inside blocks before an It(spec) in case of multipe JustBeforeEach blocks.
- ```JustAfterEach``` : Executed immediately after each test, however following the execution order from inside blocks to outside blocks after an It(spec) in case of multipe JustAfterEach blocks.
- ```BeforeEach``` : Executed (not immediately) before each test, however following the execution order from outside blocks to inside blocks before an It(spec) in case of multipe BeforeEach blocks.
- ```AfterEach``` : Executed (not immediately) after each test, however following the execution order from inside blocks to outside blocks after an It(spec) in case of multipe AfterEach blocks.

Each of the above components are arranged hierarchically in a way that makes most sense for abstracting the common logic from the rest of the code. 

Every ```BeforeEach``` precedes every ```JustBeforeEach``` before execution of an It.
Every ```JustAfterEach``` precedes every ```AfterEach``` after execution of an It.


Below is a sample test structure and may largely vary based on requirement. Some of the functions used below are just an example for illustration of functionality. Additions or deletions to the below snippet may be required for any new test.


```go

package cni

import (
        // The below folders are similar to the ones discussed above
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/agent"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
        //ginkgo and the assertion library: gomega are imported below
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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
		By("creating test namespace")
		f.K8sResourceManagers.NamespaceManager().
			CreateNamespace(utils.DefaultTestNamespace)

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
		By("deleting test namespace")
		f.K8sResourceManagers.NamespaceManager().
			DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)

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
				ServerPort:                     2273,
				ServerProtocol:                 "tcp",
				ClientCount:                    20,
				ServerCount:                    20,
				ServerPodLabelKey:              labelKey,
				ServerPodLabelVal:              serverPodLabelVal,
				ClientPodLabelKey:              labelKey,
				ClientPodLabelVal:              clientPodLabelVal,
			}

			successRate, err := trafficTester.TestTraffic()
			Expect(err).ToNot(HaveOccurred())
			Expect(successRate).Should(BeNumerically(">=", float64(99)))
		})
	})

	Context("when testing UDP traffic between client and server pods", func() {
		BeforeEach(func() {
			enableIPv4PrefixDelegation = "true"
		})
                
		It("should have 99+% success rate", func() {
			trafficTester := agent.TrafficTest{
				Framework:                      f,
				TrafficServerDeploymentBuilder: serverDeploymentBuilder,
				ServerPort:                     2273,
				ServerProtocol:                 "udp",
				ClientCount:                    20,
				ServerCount:                    20,
				ServerPodLabelKey:              labelKey,
				ServerPodLabelVal:              serverPodLabelVal,
				ClientPodLabelKey:              labelKey,
				ClientPodLabelVal:              clientPodLabelVal,
			}

			successRate, err := trafficTester.TestTraffic()
			Expect(err).ToNot(HaveOccurred())
			Expect(successRate).Should(BeNumerically(">=", float64(99)))
		})
	})
})
```

More info can be found here https://github.com/onsi/ginkgo
