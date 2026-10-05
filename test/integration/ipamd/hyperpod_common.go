// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package ipamd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

const (
	// HyperPod owns the ENI at device index 0 on network card 0, so one slot is never available to the CNI
	hyperPodReservedENIs = 1
	// HyperPod node providerID: aws:///<az>/sagemaker/cluster/hyperpod-<clusterID>-<instanceID>
	hyperPodProviderIDMarker = "/sagemaker/cluster/"
	hyperPodNodeIDPrefix     = "hyperpod-"
)

// isHyperPod is true when the primary node is a SageMaker HyperPod node
var isHyperPod bool

// hyperPodNode is the primary node, used by the HyperPod specs
var hyperPodNode corev1.Node

func isHyperPodNode(node corev1.Node) bool {
	return strings.Contains(node.Spec.ProviderID, hyperPodProviderIDMarker)
}

// describeInstanceForNode describes the EC2 instance backing the node. HyperPod instances live in the SageMaker
// service account and are not visible to DescribeInstances, so the instance is built from the ENIs the CNI manages.
func describeInstanceForNode(node corev1.Node) (types.Instance, error) {
	if !isHyperPodNode(node) {
		return f.CloudServices.EC2().DescribeInstance(context.TODO(), k8sUtils.GetInstanceIDFromNode(node))
	}
	// The last providerID segment is hyperpod-<clusterID>-<instanceID>
	nodeID := strings.TrimPrefix(k8sUtils.GetInstanceIDFromNode(node), hyperPodNodeIDPrefix)
	parts := strings.SplitN(nodeID, "-", 2)
	Expect(parts).To(HaveLen(2), "unexpected HyperPod providerID %s", node.Spec.ProviderID)
	// HyperPod instance types carry an "ml." prefix, e.g. ml.c5.xlarge
	instanceType := strings.TrimPrefix(node.Labels["node.kubernetes.io/instance-type"], "ml.")
	return f.CloudServices.EC2().DescribeHyperPodInstance(context.TODO(), parts[1], instanceType)
}

// describeHyperPodInstance refreshes the primary HyperPod instance
func describeHyperPodInstance() types.Instance {
	instance, err := describeInstanceForNode(hyperPodNode)
	Expect(err).ToNot(HaveOccurred())
	return instance
}

func describeInstanceType() types.InstanceTypeInfo {
	instanceInfo, err := f.CloudServices.EC2().DescribeInstanceType(context.TODO(), string(primaryInstance.InstanceType))
	Expect(err).ToNot(HaveOccurred())
	return instanceInfo[0]
}

// usableENILimit returns the number of ENIs the CNI can attach to the HyperPod node
func usableENILimit() int {
	return int(*describeInstanceType().NetworkInfo.MaximumNetworkInterfaces) - hyperPodReservedENIs
}

// secondaryIPsPerENI returns the number of secondary IPv4 addresses per ENI
func secondaryIPsPerENI() int {
	return int(*describeInstanceType().NetworkInfo.Ipv4AddressesPerInterface) - 1
}

// nodeMaxPods returns capacity.pods of the HyperPod node. ipamd stops adding IPs and ENIs once the node has this many
// IPs. HyperPod nodes may advertise only as many pods as one ENI holds.
func nodeMaxPods() int {
	maxPods, ok := hyperPodNode.Status.Capacity.Pods().AsInt64()
	Expect(ok).To(BeTrue())
	return int(maxPods)
}

// maxENIsForPods returns the number of ENIs ipamd can attach before the node's IPs reach capacity.pods
func maxENIsForPods() int {
	return min(usableENILimit(), ceil(nodeMaxPods(), secondaryIPsPerENI()))
}

// needsReservedSlot returns true if capacity.pods is more than the usable ENIs can hold, so a full node makes ipamd
// want another ENI. Only then can the reserved slot be exercised.
func needsReservedSlot() bool {
	return nodeMaxPods() > usableENILimit()*secondaryIPsPerENI()
}

// getPodsOnNode returns the non-terminated pods on the HyperPod node
func getPodsOnNode() []corev1.Pod {
	// The framework client is cached without a spec.nodeName index, so filter by node here
	podList := &corev1.PodList{}
	Expect(f.K8sClient.List(context.TODO(), podList)).To(Succeed())
	var pods []corev1.Pod
	for _, pod := range podList.Items {
		if pod.Spec.NodeName == hyperPodNode.Name && pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
			pods = append(pods, pod)
		}
	}
	return pods
}

// assignedIPs returns the number of pods on the HyperPod node that hold a VPC IP. HyperPod runs pod-network
// DaemonSets (e.g. hyperpod-observability), so the node is never empty and the warm targets are on top of these IPs.
func assignedIPs() int {
	count := 0
	for _, pod := range getPodsOnNode() {
		if !pod.Spec.HostNetwork {
			count++
		}
	}
	return count
}

// expectedENIs returns the number of ENIs ipamd should attach for WARM_ENI_TARGET and MAX_ENI. MAX_ENI caps the ENIs
// attached to the instance, which includes the reserved ENI.
func expectedENIs(warmENITarget, maxENI int) int {
	limit := maxENIsForPods()
	if maxENI > 0 {
		limit = min(limit, maxENI-hyperPodReservedENIs)
	}
	// The primary ENI is never freed
	return max(min(warmENITarget+ceil(assignedIPs(), secondaryIPsPerENI()), limit), 1)
}

// expectedIPs returns the number of secondary IPs ipamd should allocate for WARM_IP_TARGET and MINIMUM_IP_TARGET
func expectedIPs(warmIPTarget, minIPTarget int) int {
	return min(max(minIPTarget, assignedIPs()+warmIPTarget), nodeMaxPods())
}

// expectedPrefixes returns the number of /28 prefixes ipamd should allocate. WARM_IP_TARGET and MINIMUM_IP_TARGET take
// precedence over WARM_PREFIX_TARGET. secondaryIPs is the number of secondary IPs on the node: pods that existed before
// prefix delegation was enabled keep their secondary IPs, and those count toward the targets. ipamd always keeps at
// least one prefix with prefix delegation enabled.
func expectedPrefixes(warmIPTarget, minIPTarget, warmPrefixTarget, secondaryIPs int) int {
	assigned := assignedIPs()
	if warmIPTarget > 0 || minIPTarget > 0 {
		prefixes := ceil(max(minIPTarget, assigned+warmIPTarget)-secondaryIPs, 16)
		return min(max(prefixes, 1), ceil(nodeMaxPods(), 16))
	}
	// capacity.pods does not cap WARM_PREFIX_TARGET
	return warmPrefixTarget + ceil(max(assigned-secondaryIPs, 0), 16)
}

// countSecondaryIPsAndPrefixes returns the secondary IPs and prefixes on all ENIs of the HyperPod node
func countSecondaryIPsAndPrefixes(instance types.Instance) (int, int) {
	ips, prefixes := 0, 0
	for _, ni := range instance.NetworkInterfaces {
		ips += len(ni.PrivateIpAddresses) - 1
		prefixes += len(ni.Ipv4Prefixes)
	}
	return ips, prefixes
}

// getIpamdMetric returns the value of an ipamd metric on the HyperPod node, or 0 if the metric has not been emitted.
// labels is the exact label set, e.g. `{fn="increaseIPPoolAllocENI"}`, or empty for an unlabeled metric.
func getIpamdMetric(metric string, labels string) float64 {
	jobName := fmt.Sprintf("ipamd-metrics-%d", time.Now().UnixNano())
	curlContainer := manifest.NewCurlContainer(f.Options.TestImageRegistry).
		Command([]string{"curl"}).
		Args([]string{"--silent", "--fail", "127.0.0.1:61678/metrics"}).
		Build()
	curlJob := manifest.NewDefaultJobBuilder().
		Container(curlContainer).
		Name(jobName).
		NodeName(hyperPodNode.Name).
		Parallelism(1).
		HostNetwork(true).
		Build()

	curlJob, err := f.K8sResourceManagers.JobManager().CreateAndWaitTillJobCompleted(curlJob)
	Expect(err).ToNot(HaveOccurred())
	defer func() {
		Expect(f.K8sResourceManagers.JobManager().DeleteAndWaitTillJobIsDeleted(curlJob)).To(Succeed())
	}()

	pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector("job-name", jobName)
	Expect(err).ToNot(HaveOccurred())
	Expect(pods.Items).ToNot(BeEmpty())
	logs, err := f.K8sResourceManagers.PodManager().PodLogs(pods.Items[0].Namespace, pods.Items[0].Name)
	Expect(err).ToNot(HaveOccurred())

	for _, line := range strings.Split(logs, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == metric+labels {
			value, err := strconv.ParseFloat(fields[1], 64)
			Expect(err).ToNot(HaveOccurred())
			return value
		}
	}
	return 0
}

// verifyNoFailedENIAllocations checks that ipamd does not fail any ENI allocation on the HyperPod node during the
// window. A failed allocation means ipamd tried to attach an ENI with no free slot.
func verifyNoFailedENIAllocations(window time.Duration) {
	const metric, labels = "awscni_ipamd_error_count", `{fn="increaseIPPoolAllocENI"}`

	By(fmt.Sprintf("verifying no ENI allocation fails in %v", window))
	before := getIpamdMetric(metric, labels)
	time.Sleep(window)
	Expect(getIpamdMetric(metric, labels)).To(Equal(before), "ipamd tried to attach an ENI with no free slot")
}
