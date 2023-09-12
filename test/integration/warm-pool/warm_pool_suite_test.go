package warm_pool

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	"math/rand"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
)

// Warm Pool Test Suite Constants
// Run all tests with these constants or change individual tests to get desired outcome
// Environment variables are used in the tests listed in the (...)
const (
	randDigits      = 10   // exclusive, used in rand.Intn to change scale amount, <= maxPods, (3,6,9)
	scale           = 0.25 // used in set proportional scaling, iterate with a fixed percentage (5)
	iterations      = 2    // run test over a set number of iterations (2,3,4,7,8)
	iterPods        = 1    // iterate with a fixed number of pods (2,7,8)
	numBursts       = 2    // Use Case Test 8, set number of bursts (8)
	retryIfNoChange = 1    // retries x amount of times if randInt/randOp is out of range, if out of range no cluster
	// scaling occurs, if set above 0 will increment some areas of no cluster scaling (3, 4, 6, 8, 9)
	maxPods = 60              // max pods you want to work with for your cluster (all)
	minPods = 0               // tests can be run with a base amount of pods at start (all)
	sleep   = 1 * time.Minute // sleep interval (all)
)

var f *framework.Framework
var err error

// This Results structure defines JSON response from the Prometheus API
type Result struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric struct {
				Name                    string `json:"__name__"`
				AppKubernetesIoInstance string `json:"app_kubernetes_io_instance"`
				AppKubernetesIoName     string `json:"app_kubernetes_io_name"`
				ControllerRevisionHash  string `json:"controller_revision_hash"`
				Instance                string `json:"instance"`
				Job                     string `json:"job"`
				K8SApp                  string `json:"k8s_app"`
				Namespace               string `json:"namespace"`
				Node                    string `json:"node"`
				Pod                     string `json:"pod"`
				PodTemplateGeneration   string `json:"pod_template_generation"`
			}
			Values [][2]interface{} `json:"values"`
		}
	}
}

func TestWarmPool(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "VPC Warm Pool Test Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	By("creating test namespace")
	f.K8sResourceManagers.NamespaceManager().
		CreateNamespace(utils.DefaultTestNamespace)

	nodeList, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey,
		f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())

	numOfNodes := len(nodeList.Items)
	Expect(numOfNodes).Should(BeNumerically(">", 1))
})

var _ = AfterSuite(func() {
	By("deleting test namespace")
	f.K8sResourceManagers.NamespaceManager().
		DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)
})

// Helper Functions //

// Basic Prometheus api call
func callPrometheus(url string) Result {
	command := []string{"curl", "--silent", "-g", url}
	stdout, _, err := f.K8sResourceManagers.PodManager().PodExec(utils.DefaultTestNamespace, "curl-pod",
		command)
	Expect(err).ToNot(HaveOccurred())
	Expect(stdout).ShouldNot(BeEmpty())
	var result Result
	marshallErr := json.Unmarshal([]byte(stdout), &result)
	if marshallErr != nil {
		fmt.Printf("Cannot unmarshall json: %s", marshallErr)
	}
	return result
}

func getPrometheusMetrics(start int64, end int64) {
	By("Fetching metrics via Curl Container")
	// metrics
	warmMetric := "awscni_total_ip_addresses-awscni_assigned_ip_addresses"
	noAddrsMetric := "awscni_no_available_ip_addresses"
	netMetric := "awscni_assigned_ip_addresses"

	duration := strDurationMin(start, end)
	step := "30s"
	// Get the cluster ip of the prometheus-server service
	ctx := context.Background()
	service, _ := f.K8sResourceManagers.ServiceManager().GetService(ctx, "prometheus", "prometheus-server")
	clusterIP := service.Spec.ClusterIP

	// warmMetric
	netWarmUrl := fmt.Sprintf("http://%s/api/v1/query_range?query=%s&start=%v&end=%v&step=%s",
		clusterIP, warmMetric, start, end, step)
	resultNetWarm := callPrometheus(netWarmUrl)

	// noAddrsMetric
	noAddrUrl := fmt.Sprintf("http://%s/api/v1/query_range?query=delta(%s[%sm])&start=%v&end=%v&step=%s",
		clusterIP, noAddrsMetric, duration, start, end, step)
	resultNoAddrs := callPrometheus(noAddrUrl)

	// netMetric
	fmt.Printf("\n %s", netMetric)
	netUrl := fmt.Sprintf("http://%s/api/v1/query_range?query=delta(%s[%sm])&start=%v&end=%v&step=%s",
		clusterIP, netMetric, duration, start, end, step)
	resultNet := callPrometheus(netUrl)

	// display Prometheus Metrics
	displayPrometheusMetrics(resultNetWarm, resultNoAddrs, resultNet)
}

// Displays Prometheus queries
func displayPrometheusMetrics(resultNetWarm Result, resultNoAddrs Result, resultNet Result) {
	netMap := make(map[string]int)
	fmt.Printf("\nMAX Warm Pool over test duration: \n")

	for i := 0; i < len(resultNetWarm.Data.Result); i++ {
		node := resultNetWarm.Data.Result[i].Metric.Node
		var maxArr []int
		for j := 0; j < len(resultNetWarm.Data.Result[i].Values); j++ {
			val, _ := strconv.Atoi(resultNetWarm.Data.Result[i].Values[j][1].(string))
			maxArr = append(maxArr, val)
			if j == len(resultNetWarm.Data.Result[i].Values)-1 {
				netMap[node] = val
			}
		}
		fmt.Printf("%v : %v \n", node, slices.Max(maxArr))
	}
	fmt.Printf("\nNET Warm Pool over test duration: \n")
	for k, v := range netMap {
		fmt.Printf("%v : %v \n", k, v)
	}

	fmt.Printf("\nNo addresses available error over test duration: \n")
	for i := 0; i < len(resultNoAddrs.Data.Result); i++ {
		node := resultNoAddrs.Data.Result[i].Metric.Node
		var maxArr []int
		for j := 0; j < len(resultNoAddrs.Data.Result[i].Values); j++ {
			val := resultNoAddrs.Data.Result[i].Values[j][1].(string)
			floatVal, err := strconv.ParseFloat(val, 64)
			if err != nil {
				Expect(err).ToNot(HaveOccurred())
			}
			maxArr = append(maxArr, int(floatVal))
		}
		fmt.Printf("%v : %v \n", node, slices.Max(maxArr))
	}

	fmt.Printf("\nMAX DELTA over test duration: \n")
	for i := 0; i < len(resultNet.Data.Result); i++ {
		node := resultNet.Data.Result[i].Metric.Node
		var maxArr []int
		for j := 0; j < len(resultNet.Data.Result[i].Values); j++ {
			val := resultNet.Data.Result[i].Values[j][1].(string)
			floatVal, err := strconv.ParseFloat(val, 64)
			if err != nil {
				Expect(err).ToNot(HaveOccurred())
			}
			maxArr = append(maxArr, int(floatVal))
		}
		fmt.Printf("%v : %v \n", node, slices.Max(maxArr))
	}
}

// Gets the duration in minutes for Prometheus queries
func strDurationMin(start int64, end int64) string {
	duration := (end - start) / 60
	durationMin := strconv.FormatInt(duration, 10)
	print("TEST DURATION: ", duration)
	return durationMin
}

// Random operation, if retryIfNoChange is 0 this includes no change being a result, otherwise it will add or subtract
func randOp(replicas int, pods int) (int, string) {
	if retryIfNoChange == 0 {
		op := rand.Intn(3)
		if op == 0 {
			return replicas + pods, "adding"
		}
		if op == 1 {
			return replicas - pods, "subtracting"
		} else {
			return replicas, "no change"
		}
	} else {
		op := rand.Intn(2)
		if op == 0 {
			return replicas + pods, "adding"
		} else {
			return replicas - pods, "subtracting"
		}
	}
}

func createCurlPod() {
	// 1. Create curl container definition with the command as "prometheus command"
	// 2.  Run curl container in pod follow traffic_tester.go
	// 3. Get logs from pod
	// 4. Print logs

	// Run Curl Pod
	By("Starting Curl Container")
	curlContainer := manifest.NewCurlContainer().
		Command([]string{"sleep", "200"}).Build()

	curlPod := manifest.NewDefaultPodBuilder().
		Name("curl-pod").
		Namespace(utils.DefaultTestNamespace).
		HostNetwork(true).
		Container(curlContainer).
		Build()

	testPod, _ := f.K8sResourceManagers.PodManager().
		CreateAndWaitTillPodCompleted(curlPod)

	logs, errLogs := f.K8sResourceManagers.PodManager().
		PodLogs(testPod.Namespace, testPod.Name)
	Expect(errLogs).ToNot(HaveOccurred())
	fmt.Fprintln(GinkgoWriter, logs)
}

func deleteDeployment(deployment *v1.Deployment) {
	By("Deleting the deployment")
	err = f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deployment)
	Expect(err).NotTo(HaveOccurred())
}

func deleteCurlPod() {
	By("Deleting Curl Container")
	curlPod, err := f.K8sResourceManagers.PodManager().GetPod(utils.DefaultTestNamespace, "curl-pod")
	err = f.K8sResourceManagers.PodManager().DeleteAndWaitTillPodDeleted(curlPod)
	Expect(err).NotTo(HaveOccurred())
}

// Tries to get a random op/number combo that actually changes the cluster. If retryIfNoChange is above 0, will
// attempt to get another random integer to add/subtract that is within range. This is not always possible depending on
// what iterations and randDigits is set to, so it is best to set retryIfNoChange to a low number if it is set at all.
// If you want to see periods of no change, set this to 0.
func randOpLoop(replicas int) (int, string, int) {
	result := 0
	op := ""
	randPods := 0
	for i := 0; i < retryIfNoChange+1; i++ {
		randPods = rand.Intn(randDigits)
		result, op = randOp(replicas, randPods)
		if result > minPods && result < maxPods && randPods != 0 {
			return result, op, randPods
		}
	}
	return result, op, randPods
}

func changeReplicas(deploymentSpec *manifest.DeploymentBuilder, pods int) (*v1.Deployment, error) {
	// set replicas in template and create deployment
	deployment, err := f.K8sResourceManagers.DeploymentManager().GetDeployment("busybox", utils.DefaultTestNamespace)
	if err == nil {
		deployment := deploymentSpec.Replicas(pods).Build()
		err = f.K8sResourceManagers.
			DeploymentManager().
			UpdateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout*5)
	} else {
		deployment := deploymentSpec.Build()
		_, err = f.K8sResourceManagers.
			DeploymentManager().
			CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout*5)
	}
	return deployment, err
}

// Check on pod count outside deployment
func busyboxPodCnt() int {
	podCount := 0
	podList, _ := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector("role", "test")
	for _, _ = range podList.Items {
		podCount += 1
	}
	return podCount
}

func checkInRange(result int) int {
	replicas := result
	replicas = max(replicas, minPods)
	replicas = min(replicas, maxPods)
	return replicas
}

// Tries to prevent no scaling in the cluster as rand.Intn is inclusive with 0, so just scale 1 instead.
func incIf(pods int) int {
	if pods == 0 && retryIfNoChange > 0 {
		return 1
	} else {
		return pods
	}
}

func max(x, y int) int {
	if x < y {
		return y
	}
	return x
}

func min(x, y int) int {
	if y < x {
		return y
	}
	return x
}
