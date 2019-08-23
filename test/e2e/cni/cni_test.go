package cni_test

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/e2e/cni"

	"github.com/aws/aws-k8s-tester/e2e/framework"
	"github.com/aws/aws-k8s-tester/e2e/resources"
	"github.com/aws/aws-k8s-tester/e2e/utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	err                    error
	testerNodeName         string
	testPodErrPercentLimit float64
	awsNodeErrLimit        int
	internalIP             string
	desired                int
	i                      int
	ipLimit                int
	eniLimit               int
	coreDNSCount           int
	testpodCount           int32
	podCount               int32
	timeout                time.Duration

	f                *framework.Framework
	prom             *resources.Prom
	promResources    *resources.Resources
	testpodResources *resources.Resources
	nginxResources   *resources.Resources
	testResources    []*resources.Resources

	nodes   []corev1.Node
	promAPI promv1.API
)

var _ = Describe("Testing Amazon VPC CNI", func() {
	f = framework.New()
	ctx := context.Background()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cni-test"}}
	kubeSystem := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}}
	awsNodeDS := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "aws-node", Namespace: kubeSystem.Name}}
	serviceAccount := "cni-tester"
	testpodSleepDuration := 60 * time.Second

	Context("Testing with 1 node per test", func() {
		BeforeEach(func() {
			// cni-e2e tester node and two test nodes
			desired = 3

			testResources = []*resources.Resources{}
			timeout = 2 * time.Minute
			testpodCount = int32(6)
			// TODO: What values are we OK with?
			testPodErrPercentLimit = 0.01
			awsNodeErrLimit = 5
		})
		It("Should annotate the aws-node daemonset to scrape prometheus metrics", func() {
			annotations := map[string]string{
				"prometheus.io/scrape": "true",
				"prometheus.io/port":   "61678",
			}
			utils.AddAnnotationsToDaemonSet(ctx, f, kubeSystem, awsNodeDS, annotations)
		})
		It("Should get test and tester pods", func() {
			By(fmt.Sprintf("checking that there are at least %d nodes available", desired))
			// Check that there are at least as many nodes as desired
			// TODO: make sure they're ready
			nodeList, err := f.ClientSet.CoreV1().Nodes().List(metav1.ListOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(nodeList.Items)).To(BeNumerically(">=", desired))

			// Get name of node cni-e2e pod is running on
			testerNodeName, err = utils.GetTesterPodNodeName(f, ns.Name, "cni-e2e")
			Expect(err).ShouldNot(HaveOccurred())

			// Get nodes that the cni-e2e pod is not running on
			nodes, err = utils.GetTestNodes(f, testerNodeName)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(nodes)).To(BeNumerically(">=", desired-1))
		})

		Context("Should successfully run with WARM_IP_TARGET=0 (default), WARM_ENI_TARGET=1 (default), and MAX_ENI=-1(default)", func() {
			BeforeEach(func() {
				// Update aws-node env vars
				envs := []corev1.EnvVar{
					{Name: "INTROSPECTION_BIND_ADDRESS", Value: ":61679"},
					{Name: "WARM_IP_TARGET", Value: "0"},
					{Name: "WARM_ENI_TARGET", Value: "1"},
					{Name: "MAX_ENI", Value: "-1"},
				}
				i = 0
				utils.UpdateDaemonSetEnvVars(ctx, f, kubeSystem, awsNodeDS, envs)

				internalIP, eniLimit, ipLimit, coreDNSCount = setup(ctx, f, ns, serviceAccount, awsNodeDS, i)
				// podCount is the remaining number of secondary IPs on the first ENI
				podCount = int32(ipLimit - coreDNSCount)
			})
			It("Should have the expected aws-node prometheus metrics and be below the error rate", func() {
				setUpPrometheus(ctx, ns, serviceAccount, testerNodeName)
				testAWSNodePromMetrics(time.Now(), internalIP, eniLimit, ipLimit, awsNodeErrLimit)
			})
			It("Should have the expected testpod prometheus metrics and be below the error rate", func() {
				setUpPrometheus(ctx, ns, serviceAccount, testerNodeName)

				By("creating testpod resources")
				testpodResources = resources.NewTestpodResources(ns.Name, serviceAccount, nodes[i].Name, testpodCount)
				testResources = append(testResources, testpodResources)
				testpodResources.ExpectDeploySuccessful(ctx, f, timeout, ns)

				// Sleep for metrics to exist
				time.Sleep(testpodSleepDuration)
				testTestpodPromMetrics(time.Now(), testPodErrPercentLimit)
			})
			// With WARM_IP_TARGET=0 (default), WARM_ENI_TARGET=1 (default), and MAX_ENI=-1(default), the Amazon VPC CNI wants
			// one ENI to have no used secondary IPs.
			It("Should successfully scale ENIs with WARM_IP_TARGET=0 (default), WARM_ENI_TARGET=1 (default), and MAX_ENI=-1(default)", func() {
				// Adding anywhere from 1 pod up to the limit for an ENI adds another ENI up to the max ENIs allowed for the instance type
				nginxResources = resources.NewNginxResources(ns.Name, serviceAccount, nodes[i].Name, podCount)
				testResources = append(testResources, nginxResources)
				By(fmt.Sprintf("deployment (%s) with %d pods should have 2 ENIs", nginxResources.Deployment.Name, podCount))
				nginxResources.ExpectDeploySuccessful(ctx, f, timeout, ns)
				cni.TestENIInfo(ctx, f, internalIP, eniLimit, ipLimit, 2)

				// And adding 1 more secondary IP adds an IP on the second ENI, resulting in a third ENI
				By(fmt.Sprintf("scaling deployment (%s) to %d pods to get 3 ENIs", nginxResources.Deployment.Name, podCount+1))
				nginxResources.ExpectDeploymentScaleSuccessful(ctx, f, timeout, ns, podCount+1)
				cni.TestENIInfo(ctx, f, internalIP, eniLimit, ipLimit, 3)
			})
		})
		Context("Should successfully run with WARM_IP_TARGET=0 (default), WARM_ENI_TARGET=2, and MAX_ENI=-1(default)", func() {
			BeforeEach(func() {
				// Update aws-node env vars
				envs := []corev1.EnvVar{
					{Name: "INTROSPECTION_BIND_ADDRESS", Value: ":61679"},
					{Name: "WARM_IP_TARGET", Value: "0"},
					{Name: "WARM_ENI_TARGET", Value: "2"},
					{Name: "MAX_ENI", Value: "-1"},
				}
				i = 1
				utils.UpdateDaemonSetEnvVars(ctx, f, kubeSystem, awsNodeDS, envs)

				internalIP, eniLimit, ipLimit, coreDNSCount = setup(ctx, f, ns, serviceAccount, awsNodeDS, i)
				// podCount is the remaining number of secondary IPs on the first ENI
				podCount = int32(ipLimit - coreDNSCount)
			})
			It("Should have the expected aws-node prometheus metrics and be below the error rate", func() {
				setUpPrometheus(ctx, ns, serviceAccount, testerNodeName)
				testAWSNodePromMetrics(time.Now(), internalIP, eniLimit, ipLimit, awsNodeErrLimit)
			})
			It("Should have the expected testpod prometheus metrics and be below the error rate", func() {
				setUpPrometheus(ctx, ns, serviceAccount, testerNodeName)

				By("creating testpod resources")
				testpodResources = resources.NewTestpodResources(ns.Name, serviceAccount, nodes[i].Name, testpodCount)
				testResources = append(testResources, testpodResources)
				testpodResources.ExpectDeploySuccessful(ctx, f, timeout, ns)

				// Sleep for metrics to exist
				time.Sleep(testpodSleepDuration)
				testTestpodPromMetrics(time.Now(), testPodErrPercentLimit)
			})
			// With WARM_IP_TARGET=0 (default), WARM_ENI_TARGET=2, and MAX_ENI=-1(default), the Amazon VPC CNI wants
			// two ENIs to have no used secondary IPs.
			It("Should successfully scale ENIs with WARM_IP_TARGET=0 (default), WARM_ENI_TARGET=2 (default), and MAX_ENI=-1(default)", func() {
				// Adding anywhere from 1 pod up to the limit for an ENI adds two ENIs up to the max ENIs allowed for the instance type.
				nginxResources = resources.NewNginxResources(ns.Name, serviceAccount, nodes[i].Name, podCount)
				testResources = append(testResources, nginxResources)
				By(fmt.Sprintf("deployment (%s) with %d pods should have 2 ENIs", nginxResources.Deployment.Name, podCount))
				nginxResources.ExpectDeploySuccessful(ctx, f, timeout, ns)
				cni.TestENIInfo(ctx, f, internalIP, eniLimit, ipLimit, 3)

				// And adding 1 more secondary IP adds an IP on the second ENI, resulting in a third ENI
				By(fmt.Sprintf("scaling deployment (%s) to %d pods to get 3 ENIs", nginxResources.Deployment.Name, podCount+1))
				nginxResources.ExpectDeploymentScaleSuccessful(ctx, f, timeout, ns, podCount+1)
				cni.TestENIInfo(ctx, f, internalIP, eniLimit, ipLimit, 4)
			})
		})
		AfterEach(func() {
			for _, testResource := range testResources {
				testResource.ExpectCleanupSuccessful(ctx, f, ns)
			}
		})
	})
})

func setUpPrometheus(ctx context.Context, ns *corev1.Namespace, serviceAccount string, testerNodeName string) {
	By("Creating prometheus resources")
	promResources = resources.NewPromResources(ns.Name, serviceAccount, testerNodeName, 1)
	testResources = append(testResources, promResources)
	promResources.ExpectDeploySuccessful(ctx, f, 3*time.Minute, ns)

	promAPI, err = resources.NewPromAPI(f, ns)
	Expect(err).NotTo(HaveOccurred())
	// Wait for promAPI to be ready
	time.Sleep(time.Second * 5)

	prom = &resources.Prom{API: promAPI}
}

// setup creates test deployments, gets instance limits, the node's internal IP address, and checks prometheus metrics
// Returns internalIP, eniLimit, ipLimit, and coreDNS count
func setup(ctx context.Context, f *framework.Framework, ns *corev1.Namespace, serviceAccount string, ds *appsv1.DaemonSet, i int) (string, int, int, int) {
	By("waiting for aws-node daemonset to be ready")
	_, err := f.ResourceManager.WaitDaemonSetReady(ctx, ds)
	Expect(err).ToNot(HaveOccurred())

	eniLimit, ipLimit, err := cni.GetInstanceLimits(f, nodes[i].Name)
	Expect(err).ToNot(HaveOccurred())

	internalIP, err := utils.GetNodeInternalIP(nodes[i])
	Expect(err).ToNot(HaveOccurred())

	count, err := utils.NodeCoreDNSCount(f, nodes[i].Name)
	Expect(err).ToNot(HaveOccurred())

	return internalIP, eniLimit, ipLimit, count
}

// TODO: change these to helper functions so code isn't repeated.
func testTestpodPromMetrics(testTime time.Time, errPercentLimit float64) {
	By("checking prometheus testpod number of events received", func() {
		out, err := prom.Query("cni_test_received_total", testTime)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).NotTo(BeNil())
	})
	By("checking prometheus testpod dnsRequestFailurePercent", func() {
		out, err := prom.QueryPercent("cni_test_dns_request_total", "cni_test_dns_request_failure", testTime)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).NotTo(BeNil())
		Expect(out).To(BeNumerically("<", errPercentLimit))
	})
	By("checking prometheus testpod externalHTTPRequestsFailurePercent", func() {
		out, err := prom.QueryPercent("cni_test_external_http_request_total", "cni_test_external_http_request_failure", testTime)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).NotTo(BeNil())
		Expect(out).To(BeNumerically("<", errPercentLimit))
	})
	By("checking prometheus testpod svcClusterIPRequestFailurePercent", func() {
		out, err := prom.QueryPercent("cni_test_cluster_ip_request_total", "cni_test_cluster_ip_request_failure", testTime)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).NotTo(BeNil())
		Expect(out).To(BeNumerically("<", errPercentLimit))
	})
	By("checking prometheus testpod svcPodIPRequestsFailurePercent", func() {
		out, err := prom.QueryPercent("cni_test_pod_ip_request_total", "cni_test_pod_ip_request_failure", testTime)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).NotTo(BeNil())
		Expect(out).To(BeNumerically("<", errPercentLimit))
	})
	By("checking prometheus testpod cniTestRequestFailurePercent", func() {
		out, err := prom.QueryPercent("cni_test_request_total", "cni_test_request_failure", testTime)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).NotTo(BeNil())
		Expect(out).To(BeNumerically("<", errPercentLimit))
	})
}

// TODO: add more metrics
func testAWSNodePromMetrics(testTime time.Time, internalIP string, eniLimit int, ipLimit int, errLimit int) {
	instanceName := fmt.Sprintf("%s:61678", internalIP)
	By(fmt.Sprintf("checking prometheus awscni_eni_max (%s)", instanceName), func() {
		out, err := prom.Query(fmt.Sprintf("awscni_eni_max{instance='%s'}", instanceName), testTime)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).NotTo(BeNil())
		Expect(out).To(BeNumerically("==", eniLimit))
	})
	By(fmt.Sprintf("checking prometheus awscni_ip_max (%s)", instanceName), func() {
		out, err := prom.Query(fmt.Sprintf("awscni_ip_max{instance='%s'}", instanceName), testTime)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).NotTo(BeNil())
		Expect(out).To(BeNumerically("==", eniLimit*ipLimit))
	})
	time.Sleep(5 * time.Second)
	By(fmt.Sprintf("checking prometheus awscni_aws_api_error_count (%s)", instanceName), func() {
		out, err := prom.Query(fmt.Sprintf("awscni_aws_api_error_count{instance='%s'}", instanceName), testTime)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).NotTo(BeNil())
		Expect(out).To(BeNumerically("<=", errLimit))
	})
}
