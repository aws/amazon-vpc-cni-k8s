package integration

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubernetes/test/e2e/framework"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
)

const (
	defaultHost            = "http://127.0.0.1:8080"
	integrationTestAppName = "integration-test-app"
)

func init() {
	RegisterFlags(flag.CommandLine)
}

type LocalTestContextType struct {
	AssetsDir string
}

var LocalTestContext LocalTestContextType

func RegisterFlags(flags *flag.FlagSet) {
	// Flags also used by the upstream test framework
	flags.StringVar(&framework.TestContext.KubeConfig, clientcmd.RecommendedConfigPathFlag, os.Getenv(clientcmd.RecommendedConfigPathEnvVar), "Path to kubeconfig containing embedded authinfo.")
	flags.StringVar(&framework.TestContext.KubeContext, clientcmd.FlagContext, "", "kubeconfig context to use/override. If unset, will use value from 'current-context'")
	flags.StringVar(&framework.TestContext.KubectlPath, "kubectl-path", "kubectl", "The kubectl binary to use. For development, you might use 'cluster/kubectl.sh' here.")
	flags.StringVar(&framework.TestContext.Host, "host", "", fmt.Sprintf("The host, or apiserver, to connect to. Will default to %s if this argument and --kubeconfig are not set", defaultHost))
	flag.StringVar(&framework.TestContext.Provider, "provider", "", "The name of the Kubernetes provider (gce, gke, local, skeleton (the fallback if not set), etc.)")

	// Required to prevent metrics from being annoyingly dumped to stdout
	flag.StringVar(&framework.TestContext.GatherMetricsAfterTest, "gather-metrics-at-teardown", "false", "If set to 'true' framework will gather metrics from all components after each test. If set to 'master' only master component metrics would be gathered.")
	flag.StringVar(&framework.TestContext.GatherKubeSystemResourceUsageData, "gather-resource-usage", "false", "If set to 'true' or 'all' framework will be monitoring resource usage of system all add-ons in (some) e2e tests, if set to 'master' framework will be monitoring master node only, if set to 'none' of 'false' monitoring will be turned off.")

	// Custom flags
	flags.StringVar(&LocalTestContext.AssetsDir, "assets", "assets", "The directory that holds assets used by the integration test.")

	// Configure ginkgo as done by framework.RegisterCommonFlags
	// Turn on verbose by default to get spec names
	config.DefaultReporterConfig.Verbose = true

	// Turn on EmitSpecProgress to get spec progress (especially on interrupt)
	config.GinkgoConfig.EmitSpecProgress = true

	// Randomize specs as well as suites
	config.GinkgoConfig.RandomizeAllSpecs = true
}

func TestIntegration(t *testing.T) {
	flag.Parse()
	framework.AfterReadingAllFlags(&framework.TestContext)
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Amazon VPC CNI Integration Tests")
}

var _ = ginkgo.BeforeSuite(func() {
	fmt.Printf("Using KUBECONFIG=\"%s\"\n", framework.TestContext.KubeConfig)
})

var _ = ginkgo.Describe("[cni-integration]", func() {
	var f *framework.Framework
	f = framework.NewDefaultFramework("cni-integration")

	ginkgo.It("should enable pod-pod communication", func() {
		serverPod := newTestPod()
		pods := f.ClientSet.CoreV1().Pods(f.Namespace.Name)
		ctx := context.Background()
		serverPod, err := pods.Create(ctx, serverPod, metav1.CreateOptions{})
		framework.ExpectNoError(err, "creating pod")
		framework.ExpectNoError(e2epod.WaitForPodRunningInNamespace(f.ClientSet, serverPod), "waiting for pod running")
		serverPod, err = pods.Get(ctx, serverPod.Name, metav1.GetOptions{})
		framework.ExpectNoError(err, "getting pod")

		framework.ExpectNoError(
			checkConnectivityToHost(f, "", "client-pod", serverPod.Status.PodIP, 8080, 30))
		err = f.ClientSet.CoreV1().Pods(f.Namespace.Name).Delete(ctx, "server-pod", metav1.DeleteOptions{})
		framework.ExpectNoError(err, "deleting pod")
	})

	ginkgo.It("should enable pod-node communication", func() {
		nodeList, err := e2enode.GetReadySchedulableNodes(f.ClientSet)
		framework.ExpectNoError(err)
		internalIP := getNodeInternalIP(&nodeList.Items[0])
		fmt.Printf("Obtained node internal IP as %s", internalIP)
		framework.ExpectNoError(
			checkConnectivityToHost(f, "", "client-pod", internalIP, 8080, 30))
	})

})

func newTestPod() *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "server-pod",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:    "c",
					Image:   framework.BusyBoxImage,
					Ports: []v1.ContainerPort{
						{
							ContainerPort: 8080,
							HostPort: 8080,
						},
					},
					Command: []string{"sleep", "60"},
				},
			},
			RestartPolicy: v1.RestartPolicyNever,
		},
	}
}


// checkConnectivityToHost launches a pod to test connectivity to the specified
// host. An error will be returned if the host is not reachable from the pod.
//
// An empty nodeName will use the schedule to choose where the pod is executed.
func checkConnectivityToHost(f *framework.Framework, nodeName, podName, host string, port, timeout int) error {
	command := []string{
		"nc",
		"-vz",
		"-w", strconv.Itoa(timeout),
		host,
		strconv.Itoa(port),
	}

	pod := e2epod.NewAgnhostPod(f.Namespace.Name, podName, nil, nil, nil)
	pod.Spec.Containers[0].Command = command
	pod.Spec.Containers[0].Args = nil // otherwise 'pause` is magically an argument to nc, which causes all hell to break loose
	pod.Spec.NodeName = nodeName
	pod.Spec.RestartPolicy = v1.RestartPolicyNever

	podClient := f.ClientSet.CoreV1().Pods(f.Namespace.Name)
	_, err := podClient.Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	err = e2epod.WaitForPodSuccessInNamespace(f.ClientSet, podName, f.Namespace.Name)

	if err != nil {
		logs, logErr := e2epod.GetPodLogs(f.ClientSet, f.Namespace.Name, pod.Name, pod.Spec.Containers[0].Name)
		if logErr != nil {
			framework.Logf("Warning: Failed to get logs from pod %q: %v", pod.Name, logErr)
		} else {
			framework.Logf("pod %s/%s logs:\n%s", f.Namespace.Name, pod.Name, logs)
		}
	}

	return err
}

// Returns the internal IP of the node or "<none>" if none is found.
func getNodeInternalIP(node *v1.Node) string {
	for _, address := range node.Status.Addresses {
		if address.Type == v1.NodeInternalIP {
			return address.Address
		}
	}

	return "<none>"
}
