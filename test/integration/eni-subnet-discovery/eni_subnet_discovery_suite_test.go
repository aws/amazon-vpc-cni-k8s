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

package eni_subnet_discovery

import (
	"context"
	"flag"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/apparentlymart/go-cidr/cidr"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	awsUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/utils"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/integration/common"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCustomNetworking(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CNI ENI Subnet Selection Test Suite")
}

var (
	f                      *framework.Framework
	clusterVPCConfig       *awsUtils.ClusterVPCConfig
	cidrRangeString        string
	cidrRange              *net.IPNet
	cidrBlockAssociationID string
	createdSubnet          string
	rtAssociationID        string
	primaryInstance        ec2types.Instance
	useIPv6                bool
	specSubnetIDs          []string
)

// Parse test specific variable from flag
func init() {
	flag.StringVar(&cidrRangeString, "secondary-cidr-range", "100.64.0.0/16", "second cidr range to be associated with the VPC")
	flag.BoolVar(&useIPv6, "use-ipv6", false, "Use IPv6 for subnet discovery tests")
}

var primaryNodeName string

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	nodeList, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey,
		f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())

	numOfNodes := len(nodeList.Items)
	Expect(numOfNodes).Should(BeNumerically(">", 1))

	// Nominate the first untainted node as the one to run deployment against
	By("finding the first untainted node for the deployment")
	var primaryNode *corev1.Node
	for _, n := range nodeList.Items {
		if len(n.Spec.Taints) == 0 {
			primaryNode = &n
			break
		}
	}
	Expect(primaryNode).To(Not(BeNil()), "expected to find a non-tainted node")

	instanceID := k8sUtils.GetInstanceIDFromNode(*primaryNode)
	primaryInstance, err = f.CloudServices.EC2().DescribeInstance(context.TODO(), instanceID)
	Expect(err).ToNot(HaveOccurred())

	// Pods that are not DaemonSet-owned (e.g. CoreDNS) can hold IPs on the primary node's
	// secondary ENIs, which blocks the ENI drain below. Move them to other nodes, then
	// uncordon right away: ipamd does not attach or detach ENIs on an unschedulable node.
	primaryNodeName = primaryNode.Name
	By(fmt.Sprintf("evicting non-DaemonSet pods from %s", primaryNodeName))
	setNodeUnschedulable(primaryNodeName, true)
	evictNonDaemonSetPods(primaryNodeName)
	setNodeUnschedulable(primaryNodeName, false)

	// Adjust default CIDR if IPv6 is enabled and no custom CIDR provided
	if useIPv6 && cidrRangeString == "100.64.0.0/16" {
		cidrRangeString = "2600:1f13:000::/56" // AWS IPv6 example range
	}

	_, cidrRange, err = net.ParseCIDR(cidrRangeString)
	Expect(err).ToNot(HaveOccurred())

	// Validate CIDR matches IP version
	if useIPv6 {
		Expect(cidrRange.IP.To4()).To(BeNil(), "IPv6 mode requires IPv6 CIDR")
	} else {
		Expect(cidrRange.IP.To4()).ToNot(BeNil(), "IPv4 mode requires IPv4 CIDR")
	}

	By("creating test namespace")
	_ = f.K8sResourceManagers.NamespaceManager().CreateNamespace(utils.DefaultTestNamespace)

	By("getting the cluster VPC Config")
	clusterVPCConfig, err = awsUtils.GetClusterVPCConfig(f)
	Expect(err).ToNot(HaveOccurred())

	By("associating cidr range to the VPC")
	if useIPv6 {
		// The current framework only supports IPv4 CIDR association
		// For IPv6, we assume the VPC already has IPv6 enabled
		// In a production environment, you would extend the framework to support IPv6 association
		By("IPv6 mode: assuming VPC already has IPv6 enabled")

		// Verify VPC has IPv6 and get an existing association ID for cleanup
		vpcInfo, err := f.CloudServices.EC2().DescribeVPC(context.TODO(), f.Options.AWSVPCID)
		Expect(err).ToNot(HaveOccurred())

		hasIPv6 := false
		for _, assoc := range vpcInfo.Vpcs[0].Ipv6CidrBlockAssociationSet {
			if assoc.Ipv6CidrBlockState != nil && assoc.Ipv6CidrBlockState.State == "associated" {
				hasIPv6 = true
				// We won't disassociate existing IPv6, so set empty ID
				cidrBlockAssociationID = ""
				break
			}
		}

		if !hasIPv6 {
			Skip("IPv6 tests require a VPC with IPv6 already enabled")
		}
	} else {
		// IPv4: check if the CIDR is already associated (from a previous leaked run)
		vpcInfo, err := f.CloudServices.EC2().DescribeVPC(context.TODO(), f.Options.AWSVPCID)
		Expect(err).ToNot(HaveOccurred())

		alreadyAssociated := false
		for _, assoc := range vpcInfo.Vpcs[0].CidrBlockAssociationSet {
			if assoc.CidrBlock != nil && *assoc.CidrBlock == cidrRange.String() {
				state := assoc.CidrBlockState.State
				if state == "associated" {
					By(fmt.Sprintf("CIDR %s already associated (id: %s), reusing", cidrRange.String(), *assoc.AssociationId))
					cidrBlockAssociationID = *assoc.AssociationId
					alreadyAssociated = true
					break
				} else if state == "disassociating" {
					By(fmt.Sprintf("CIDR %s is disassociating, polling until complete", cidrRange.String()))
					Eventually(func() bool {
						info, err := f.CloudServices.EC2().DescribeVPC(context.TODO(), f.Options.AWSVPCID)
						if err != nil {
							return false
						}
						for _, a := range info.Vpcs[0].CidrBlockAssociationSet {
							if a.CidrBlock != nil && *a.CidrBlock == cidrRange.String() {
								return a.CidrBlockState.State != "disassociating"
							}
						}
						return true // CIDR no longer listed, disassociation complete
					}, 5*time.Minute, 10*time.Second).Should(BeTrue(), "CIDR disassociation should complete")
				}
			}
		}

		if !alreadyAssociated {
			association, err := f.CloudServices.EC2().AssociateVPCCIDRBlock(context.TODO(), f.Options.AWSVPCID, cidrRange.String())
			Expect(err).ToNot(HaveOccurred())
			cidrBlockAssociationID = *association.CidrBlockAssociation.AssociationId
		}
	}

	By(fmt.Sprintf("creating the subnet in %s", *primaryInstance.Placement.AvailabilityZone))

	var subnetCidr *net.IPNet
	if useIPv6 {
		// For IPv6, calculate the appropriate number of bits to get a /64 subnet
		prefixLen, _ := cidrRange.Mask.Size()
		if prefixLen > 64 {
			Fail(fmt.Sprintf("IPv6 parent CIDR prefix length must be <= 64, got /%d", prefixLen))
		}
		// Calculate how many bits we need to extend to reach /64
		bitsToExtend := 64 - prefixLen
		subnetCidr, err = cidr.Subnet(cidrRange, bitsToExtend, 0)
		Expect(err).ToNot(HaveOccurred())
	} else {
		// IPv4: Subnet must be greater than /19
		subnetCidr, err = cidr.Subnet(cidrRange, 2, 0)
		Expect(err).ToNot(HaveOccurred())
	}

	// Clean up stale subnets from previous failed runs in the secondary CIDR range
	By("cleaning up any stale subnets from previous runs")
	cleanupStaleSubnetsInCIDR()

	createSubnetOutput, err := f.CloudServices.EC2().
		CreateSubnet(context.TODO(), subnetCidr.String(), f.Options.AWSVPCID, *primaryInstance.Placement.AvailabilityZone)
	Expect(err).ToNot(HaveOccurred())

	createdSubnet = *createSubnetOutput.Subnet.SubnetId

	By("tagging the subnet for test ownership")
	tagSubnetForOwnership(createdSubnet)

	By("associating the route table with the newly created subnet")
	rtAssociationID, err = f.CloudServices.EC2().AssociateRouteTableToSubnet(context.TODO(), clusterVPCConfig.PublicRouteTableID, createdSubnet)
	Expect(err).ToNot(HaveOccurred())

	By("try detaching all ENIs by setting WARM_ENI_TARGET to 0")
	k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
		utils.AwsNodeName, map[string]string{"WARM_ENI_TARGET": "0"})

	By("waiting for the CNI to delete unused ENIs")
	waitForSecondaryENIsDrained()
})

var _ = AfterSuite(func() {
	var errs prometheus.MultiError

	// DeferCleanup runs after this body, even if an assertion fails: the restore
	// cannot mask the aggregated cleanup errors below (its helper asserts
	// internally), the failed aggregate cannot skip the restore, and it still runs
	// after the subnet delete so the CNI drains ENIs instead of warming a new one
	// into the subnet being deleted.
	DeferCleanup(func() {
		if primaryNodeName != "" {
			By(fmt.Sprintf("ensuring %s is uncordoned", primaryNodeName))
			setNodeUnschedulable(primaryNodeName, false)
		}
	})
	DeferCleanup(func() {
		By("restoring WARM_ENI_TARGET to 1")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
			utils.AwsNodeName, map[string]string{"WARM_ENI_TARGET": "1"})
	})

	By("deleting test namespace")
	errs.Append(f.K8sResourceManagers.NamespaceManager().
		DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace))

	if rtAssociationID != "" {
		By(fmt.Sprintf("disassociating route table association %s", rtAssociationID))
		errs.Append(common.EnsureRouteTableDisassociated(f, rtAssociationID))
	}

	for _, subnetID := range specSubnetIDs {
		By(fmt.Sprintf("deleting the spec subnet %s", subnetID))
		errs.Append(common.EnsureSubnetDeleted(f, subnetID))
	}

	// WARM_ENI_TARGET stays 0 (set in BeforeSuite) until the subnet is gone; the
	// bounded delete poll absorbs the ENI drain, no fixed sleep needed.
	if createdSubnet != "" {
		By(fmt.Sprintf("deleting the subnet %s", createdSubnet))
		errs.Append(common.EnsureSubnetDeleted(f, createdSubnet))
	}

	if cidrBlockAssociationID != "" {
		By("disassociating the CIDR range from the VPC")
		errs.Append(common.EnsureVPCCIDRDisassociated(f, cidrBlockAssociationID, cidrRange.String()))
	}

	// Fail the suite on a cleanup error rather than swallowing it, so leaks surface in CI.
	Expect(errs.MaybeUnwrap()).ToNot(HaveOccurred(), "cleanup operations failed")
})

func createSpecSubnet(cidrBlock string) string {
	out, err := f.CloudServices.EC2().
		CreateSubnet(context.TODO(), cidrBlock, f.Options.AWSVPCID, *primaryInstance.Placement.AvailabilityZone)
	Expect(err).ToNot(HaveOccurred())
	subnetID := *out.Subnet.SubnetId
	specSubnetIDs = append(specSubnetIDs, subnetID)
	tagSubnetForOwnership(subnetID)
	return subnetID
}

// tagSubnetForOwnership retries until the new subnet is visible to EC2 (CreateTags can
// return InvalidSubnetID.NotFound right after CreateSubnet), so callers can tag it next.
func tagSubnetForOwnership(subnetID string) {
	Eventually(func() error {
		_, err := f.CloudServices.EC2().CreateTags(context.TODO(), []string{subnetID}, []ec2types.Tag{
			{Key: aws.String(staleSubnetTag), Value: aws.String("true")},
		})
		return err
	}, 2*time.Minute, 5*time.Second).Should(Succeed(), "subnet %s should become taggable", subnetID)
}

func setNodeUnschedulable(nodeName string, unschedulable bool) {
	Eventually(func() error {
		node := &corev1.Node{}
		if err := f.K8sClient.Get(context.TODO(), client.ObjectKey{Name: nodeName}, node); err != nil {
			return err
		}
		patch := client.MergeFrom(node.DeepCopy())
		node.Spec.Unschedulable = unschedulable
		return f.K8sClient.Patch(context.TODO(), node, patch)
	}, time.Minute, 5*time.Second).Should(Succeed())
}

// evictablePodsOnNode returns running pods on the node that would hold a secondary ENI IP
// and are not recreated on the node by a DaemonSet.
func evictablePodsOnNode(nodeName string) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := f.K8sClient.List(context.TODO(), podList); err != nil {
		return nil, err
	}
	var pods []corev1.Pod
	for _, pod := range podList.Items {
		if pod.Spec.NodeName != nodeName || pod.Spec.HostNetwork ||
			pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		if _, mirror := pod.Annotations[corev1.MirrorPodAnnotationKey]; mirror {
			continue
		}
		if owner := metav1.GetControllerOf(&pod); owner != nil && owner.Kind == "DaemonSet" {
			continue
		}
		pods = append(pods, pod)
	}
	return pods, nil
}

func evictNonDaemonSetPods(nodeName string) {
	Eventually(func() ([]string, error) {
		pods, err := evictablePodsOnNode(nodeName)
		if err != nil {
			return nil, err
		}
		var remaining []string
		for i := range pods {
			pod := &pods[i]
			remaining = append(remaining, pod.Namespace+"/"+pod.Name)
			if pod.DeletionTimestamp != nil {
				continue
			}
			eviction := &policyv1.Eviction{ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace}}
			if err := f.K8sClient.SubResource("eviction").Create(context.TODO(), pod, eviction); err != nil && !apierrors.IsNotFound(err) {
				GinkgoWriter.Printf("eviction of %s/%s not yet allowed: %v\n", pod.Namespace, pod.Name, err)
			}
		}
		return remaining, nil
	}, 5*time.Minute, 10*time.Second).Should(BeEmpty(), "non-DaemonSet pods should be evicted from %s", nodeName)
}

func deleteSpecSubnet(subnetID string) {
	if err := common.EnsureSubnetDeleted(f, subnetID); err != nil {
		GinkgoWriter.Printf("Warning: %v; AfterSuite will retry\n", err)
	}
}

// staleSubnetTag is applied to all subnets created by this test suite so that
// cleanupStaleSubnetsInCIDR only removes resources it owns.
const staleSubnetTag = "eni-subnet-discovery-test"

// cleanupStaleSubnetsInCIDR removes subnets in the secondary CIDR range that were
// leaked from previous failed test runs. Only subnets tagged by this suite are removed,
// preventing accidental deletion of unrelated infrastructure in the VPC.
func cleanupStaleSubnetsInCIDR() {
	ctx := context.TODO()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(f.Options.AWSRegion))
	if err != nil {
		GinkgoWriter.Printf("Warning: failed to create EC2 client for cleanup: %v\n", err)
		return
	}
	ec2Client := ec2.NewFromConfig(cfg)

	// Only find subnets tagged by this test suite
	result, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{f.Options.AWSVPCID}},
			{Name: aws.String("tag-key"), Values: []string{staleSubnetTag}},
		},
	})
	if err != nil {
		GinkgoWriter.Printf("Warning: failed to describe subnets for cleanup: %v\n", err)
		return
	}

	for _, subnet := range result.Subnets {
		if subnet.CidrBlock == nil || subnet.SubnetId == nil {
			continue
		}
		// Double-check the subnet falls within our secondary CIDR range
		_, subnetNet, err := net.ParseCIDR(*subnet.CidrBlock)
		if err != nil || !cidrRange.Contains(subnetNet.IP) {
			continue
		}

		GinkgoWriter.Printf("Found stale test subnet %s (%s), cleaning up\n", *subnet.SubnetId, *subnet.CidrBlock)

		// Detach and delete any ENIs in this subnet
		eniResult, err := ec2Client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
			Filters: []ec2types.Filter{
				{Name: aws.String("subnet-id"), Values: []string{*subnet.SubnetId}},
			},
		})
		if err == nil {
			for _, eni := range eniResult.NetworkInterfaces {
				if eni.Attachment != nil && eni.Attachment.AttachmentId != nil {
					GinkgoWriter.Printf("  Detaching ENI %s\n", *eni.NetworkInterfaceId)
					_, _ = ec2Client.DetachNetworkInterface(ctx, &ec2.DetachNetworkInterfaceInput{
						AttachmentId: eni.Attachment.AttachmentId,
						Force:        aws.Bool(true),
					})
					time.Sleep(5 * time.Second)
				}
				GinkgoWriter.Printf("  Deleting ENI %s\n", *eni.NetworkInterfaceId)
				_, _ = ec2Client.DeleteNetworkInterface(ctx, &ec2.DeleteNetworkInterfaceInput{
					NetworkInterfaceId: eni.NetworkInterfaceId,
				})
			}
			if len(eniResult.NetworkInterfaces) > 0 {
				time.Sleep(10 * time.Second)
			}
		}

		if err := f.CloudServices.EC2().DeleteSubnet(ctx, *subnet.SubnetId); err != nil {
			GinkgoWriter.Printf("  Warning: failed to delete stale subnet %s: %v\n", *subnet.SubnetId, err)
		} else {
			GinkgoWriter.Printf("  Deleted stale subnet %s\n", *subnet.SubnetId)
		}
	}
}
