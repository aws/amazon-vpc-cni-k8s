package cni

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"

	"github.com/aws/aws-k8s-tester/e2e/framework"
	"github.com/aws/aws-k8s-tester/e2e/utils"
	"github.com/aws/aws-sdk-go/service/ec2"
	log "github.com/cihub/seelog"
	"github.com/davecgh/go-spew/spew"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetInstanceLimits gets instance ENI and IP limits
func GetInstanceLimits(f *framework.Framework, nodeName string) (int, int, error) {
	filterName := "private-dns-name"
	describeInstancesInput := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   &filterName,
				Values: []*string{&nodeName},
			},
		},
	}
	instance, err := f.Cloud.EC2().DescribeInstances(describeInstancesInput)
	if err != nil {
		return 0, 0, err
	}
	if len(instance.Reservations) < 1 {
		return 0, 0, errors.New("No instance reservations found")
	}
	if len(instance.Reservations[0].Instances) < 1 {
		return 0, 0, errors.New("No instances found")
	}
	instanceType := *(instance.Reservations[0].Instances[0].InstanceType)
	availableENIs := awsutils.InstanceENIsAvailable[instanceType]
	availableIPs := awsutils.InstanceIPsAvailable[instanceType] - 1

	log.Debugf("node (%s) has instance type (%s) and (%d) ENIs and (%d) IPs available",
		nodeName, instanceType, availableENIs, availableIPs)

	return availableENIs, availableIPs, nil
}

// TestENIInfo checks if the ENIInfo values from the aws-node (CNI) metrics endpoint are as expected
// TODO: Future upgrade can get the port from aws-node's INTROSPECTION_BIND_ADDRESS
func TestENIInfo(ctx context.Context, f *framework.Framework, internalIP string, eniLimit int, ipLimit int, expectedENICount int) {
	var resp *http.Response
	var eniInfos datastore.ENIInfos

	// If the expected ENI count is greater than the max ENIs, check for the max ENI count
	if expectedENICount > eniLimit {
		log.Debugf("checking for (%d) ENIs since (%d) exceeds the eniLimit", eniLimit, expectedENICount)
		expectedENICount = eniLimit
	}

	By("getting ENIs from EC2")
	enis := utils.GetExpectedNodeENIs(ctx, f, internalIP, expectedENICount)
	if len(enis) != expectedENICount {
		log.Debug(spew.Sprintf("found %d ENIs via EC2:\n%+v", len(enis), enis))
	}

	By("checking number of ENIs via EC2")
	Expect(len(enis)).To(Equal(expectedENICount))

	By("checking number of IPs via EC2")
	for _, eni := range enis {
		log.Debugf("%s has %d secondary IPs via EC2", *(eni.NetworkInterfaceId), len(eni.PrivateIpAddresses)-1)
		// 1 to include the primary IP
		Expect(len(eni.PrivateIpAddresses)).To(Equal(ipLimit + 1))
	}

	// Check metrics endpoint
	port := "61679"

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "aws-node",
			Namespace: "kube-system",
		},
	}

	_, err := f.ResourceManager.WaitServiceHasEndpointIP(ctx, svc, internalIP)
	Expect(err).ShouldNot(HaveOccurred())

	By("Getting ENIs from metrics endpoint")
	enisPath := fmt.Sprintf("http://%s:%s/v1/enis", internalIP, port)
	for i := 0; i < 5; i++ {
		resp, err = http.Get(enisPath)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		Expect(err).ShouldNot(HaveOccurred())

		body, err := ioutil.ReadAll(resp.Body)
		Expect(err).ShouldNot(HaveOccurred())

		json.Unmarshal(body, &eniInfos)

		if len(eniInfos.ENIIPPools) == expectedENICount {
			break
		}
		time.Sleep(time.Second * 10)
	}
	// Log ENIIPools if the number of ENIs is not the expected count
	if len(eniInfos.ENIIPPools) != expectedENICount {
		log.Debug(spew.Sprintf("found %d ENIs via metrics endpoint:\n%+v", len(eniInfos.ENIIPPools), eniInfos.ENIIPPools))
	}

	By("checking number of ENIs via metrics endpoint")
	Expect(len(eniInfos.ENIIPPools)).To(Equal(expectedENICount))

	By("checking number of IPs via metrics endpoint")
	for k, v := range eniInfos.ENIIPPools {
		log.Debugf("%s has %d secondary IPs via metrics endpoint", k, len(v.IPv4Addresses))
		Expect(len(v.IPv4Addresses)).To(Equal(ipLimit))
	}
}

// GetTestpodCount returns the number of testpod pods running on the node. This is useful when tests are
// run in parallel
func GetTestpodCount(f *framework.Framework, nodeName string, ns *corev1.Namespace) (int, error) {
	var err error
	var count int
	var podList *corev1.PodList

	// Find nodes running coredns
	listOptions := metav1.ListOptions{
		LabelSelector: "app=testpod",
	}
	podList, err = f.ClientSet.CoreV1().Pods(ns.Name).List(listOptions)
	if err != nil {
		return 0, err
	}
	for _, pod := range podList.Items {
		if nodeName == pod.Spec.NodeName {
			count++
		}
	}
	if count != 0 {
		log.Debugf("found (%d) testpod pods running on node (%s)", count, nodeName)
	}
	return count, nil
}
