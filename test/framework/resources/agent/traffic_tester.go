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

package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo/v2"
	appsV1 "k8s.io/api/apps/v1"
	batchV1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
)

// TrafficTest is used to execute a traffic test for TCP/UDP traffic
type TrafficTest struct {
	Framework *framework.Framework
	// The deployment builder is needed instead of deployment, in order
	// to inject the server container to the deployment
	TrafficServerDeploymentBuilder *manifest.DeploymentBuilder
	// Port on which the server should listen for traffic
	ServerPort int
	// TCP/UPD are the supported protocols
	ServerProtocol string
	// The number of client pods to create for testing connection to
	// each server Pod
	ClientCount int
	// Server count is the number of server pods to be created
	ServerCount int
	// Server Pod Label Key/Val is required in order to get the list of
	// pods belonging to the server deployment
	ServerPodLabelKey string
	ServerPodLabelVal string
	// Client Pod Label Key/Val is required in order to get the list of
	// pods belonging to the client job
	ClientPodLabelKey string
	ClientPodLabelVal string
	// If supplied the function will be used to validate the pods are as expected
	// For instance, to validate server/client pods are using Branch ENI
	ValidateServerPods func(list v1.PodList) error
	ValidateClientPods func(list v1.PodList) error
	// Boolean that indicates if IPv6 mode is enabled
	IsV6Enabled bool
}

// Tests traffic by creating multiple server pods using a deployment and multiple client pods
// using a Job. Each client Pod tests connectivity to each Server Pod.
func (t *TrafficTest) TestTraffic() (float64, error) {
	// Server listens on a given TCP/UDP Port
	serverDeployment, err := t.startTrafficServer()
	if err != nil {
		return 0, fmt.Errorf("failed to start server deployment: %v", err)
	}

	fmt.Fprintln(GinkgoWriter, "successfully created server deployment")

	// The Metric Server Aggregates all metrics from all the client Pod so we
	// don't have to query each client Pod to get the metric
	metricServerPod, err := t.startMetricServerPod()
	if err != nil {
		return 0, fmt.Errorf("failed to start metric server pod: %v", err)
	}

	fmt.Fprintln(GinkgoWriter, "successfully created metric server pod")

	// Get the list of Server Pod in order to get the IP Address of the Servers
	podList, err := t.Framework.K8sResourceManagers.PodManager().
		GetPodsWithLabelSelector(t.ServerPodLabelKey, t.ServerPodLabelVal)
	if err != nil {
		return 0, fmt.Errorf("failed to get the list of pods for server deployment: %v", err)
	}

	fmt.Fprintln(GinkgoWriter, "successfully fetched list of pod belonging to deployment")

	// Using this Validation Injector you can validate the Server Pods
	if t.ValidateServerPods != nil {
		err = t.ValidateServerPods(podList)
		if err != nil {
			return 0, fmt.Errorf("pod list %v validation failed %v", podList, err)
		}
		fmt.Fprintln(GinkgoWriter, "successfully validated the server pod list")
	}

	var serverIPs []string
	for _, pod := range podList.Items {
		podIP := pod.Status.PodIP
		if t.IsV6Enabled {
			podIP = fmt.Sprintf("[%s]", pod.Status.PodIP)
		}
		serverIPs = append(serverIPs, podIP)
	}

	// To the Client Job pass the list of Server IPs, so each client Pod tests connectivity to each
	// server
	clientJob, err := t.startTrafficClient(strings.Join(serverIPs, ","), metricServerPod.Status.PodIP)
	if err != nil {
		return 0, fmt.Errorf("failed to start client jobs: %v", err)
	}

	fmt.Fprintln(GinkgoWriter, "successfully created traffic client")

	// Get List of client Pods for validation
	podList, err = t.Framework.K8sResourceManagers.PodManager().
		GetPodsWithLabelSelector(t.ClientPodLabelKey, t.ClientPodLabelVal)
	if err != nil {
		return 0, fmt.Errorf("failed to get the list of pods for client job: %v", err)
	}

	if t.ValidateClientPods != nil {
		err = t.ValidateClientPods(podList)
		if err != nil {
			return 0, fmt.Errorf("pod list %v validation failed %v", podList, err)
		}
		fmt.Fprintln(GinkgoWriter, "successfully validated the server pod list")
	}

	metricServerIP := metricServerPod.Status.PodIP
	if t.IsV6Enabled {
		metricServerIP = fmt.Sprintf("[%s]", metricServerPod.Status.PodIP)
	}
	// Get the aggregated response from the metric server for calculating the connection success rate
	testInputs, err := t.getTestStatusFromMetricServer(metricServerIP)
	if err != nil {
		return 0, fmt.Errorf("failed to get test status from metric server: %v", err)
	}

	fmt.Fprintln(GinkgoWriter, "successfully fetched the metrics from metric server")

	successRate := t.calculateSuccessRate(testInputs)
	if successRate != float64(100) {
		fmt.Fprintf(GinkgoWriter, "SuccessRate: %v, Input List: %v", successRate, testInputs)
	} else {
		fmt.Fprintf(GinkgoWriter, "SuccessRate: %v", successRate)
	}

	// Clean up all the resources
	err = t.Framework.K8sResourceManagers.JobManager().DeleteAndWaitTillJobIsDeleted(clientJob)
	if err != nil {
		return 0, fmt.Errorf("failed to delete client job: %v", err)
	}

	err = t.Framework.K8sResourceManagers.PodManager().DeleteAndWaitTillPodDeleted(metricServerPod)
	if err != nil {
		return 0, fmt.Errorf("failed to delete metric server pod: %v", err)
	}

	err = t.Framework.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(serverDeployment)
	if err != nil {
		return 0, fmt.Errorf("failed to delete server deployment: %v", err)
	}

	return successRate, nil
}

func (t *TrafficTest) startTrafficServer() (*appsV1.Deployment, error) {
	serverContainer := manifest.NewTestHelperContainer(t.Framework.Options.TestImageRegistry).
		Name("server").
		Command([]string{"./traffic-server"}).
		Args([]string{
			fmt.Sprintf("-server-port=%d", t.ServerPort),
			fmt.Sprintf("-server-mode=%s", t.ServerProtocol),
		}).
		Build()

	serverDeployment := t.TrafficServerDeploymentBuilder.
		Container(serverContainer).
		Replicas(t.ServerCount).
		PodLabel(t.ServerPodLabelKey, t.ServerPodLabelVal).
		Build()

	return t.Framework.K8sResourceManagers.DeploymentManager().
		CreateAndWaitTillDeploymentIsReady(serverDeployment, utils.DefaultDeploymentReadyTimeout)
}

func (t *TrafficTest) startTrafficClient(serverAddList string, metricServerIP string) (*batchV1.Job, error) {
	trafficClientContainer := manifest.NewTestHelperContainer(t.Framework.Options.TestImageRegistry).
		Name("client-regular-pods").
		Command([]string{"./traffic-client"}).
		Args([]string{
			fmt.Sprintf("-server-list-csv=%s", serverAddList),
			fmt.Sprintf("-server-port=%d", t.ServerPort),
			fmt.Sprintf("-server-listen-mode=%s", t.ServerProtocol),
			fmt.Sprintf("-metric-aggregator-addr=http://%s:8080/submit/metric"+
				"/connectivity", metricServerIP),
		}).
		Build()

	clientJob := manifest.NewDefaultJobBuilder().
		Name("traffic-client-regular-pods").
		Parallelism(t.ClientCount).
		Container(trafficClientContainer).
		PodLabels(t.ClientPodLabelKey, t.ClientPodLabelVal).
		Build()

	return t.Framework.K8sResourceManagers.JobManager().CreateAndWaitTillJobCompleted(clientJob)
}

func (t *TrafficTest) startMetricServerPod() (*v1.Pod, error) {
	metricContainer := manifest.NewTestHelperContainer(t.Framework.Options.TestImageRegistry).
		Name("metric-container").
		Command([]string{"./metric-server"}).
		Build()

	metricServerPod := manifest.NewDefaultPodBuilder().
		Name("metric-pod").
		Container(metricContainer).
		Build()

	return t.Framework.K8sResourceManagers.PodManager().CreateAndWaitTillRunning(metricServerPod)
}

func (t *TrafficTest) getTestStatusFromMetricServer(metricPodIP string) ([]input.TestStatus, error) {
	getMetricContainer := manifest.NewCurlContainer().
		Name("get-metric-container").
		Command([]string{"curl"}).
		Args([]string{fmt.Sprintf("http://%s:8080/get/metric/connectivity", metricPodIP), "--silent"}).
		Build()

	getMetricPod := manifest.NewDefaultPodBuilder().
		Name("get-metric-pod").
		Container(getMetricContainer).
		Build()

	getMetricPod, err := t.Framework.K8sResourceManagers.PodManager().
		CreateAndWaitTillPodCompleted(getMetricPod)
	if err != nil {
		return nil, err
	}

	logs, err := t.Framework.K8sResourceManagers.PodManager().
		PodLogs(getMetricPod.Namespace, getMetricPod.Name)
	if err != nil {
		return nil, err
	}

	err = t.Framework.K8sResourceManagers.PodManager().DeleteAndWaitTillPodDeleted(getMetricPod)
	if err != nil {
		return nil, err
	}

	var testStatues []input.TestStatus
	err = json.Unmarshal([]byte(logs), &testStatues)
	return testStatues, err
}

func (t *TrafficTest) calculateSuccessRate(testStatuses []input.TestStatus) float64 {
	expectedResults := t.ServerCount * t.ClientCount

	var successCount int
	for _, testStatus := range testStatuses {
		successCount += testStatus.SuccessCount
	}

	return float64(successCount) / float64(expectedResults) * 100
}
