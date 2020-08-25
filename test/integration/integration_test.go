package integration

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubernetes/test/e2e/framework"
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
		serverPod, err := f.ClientSet.CoreV1().Pods(f.Namespace.Name).Create(serverPod)
		framework.ExpectNoError(err, "creating pod")
		framework.ExpectNoError(f.WaitForPodRunning(serverPod.Name), "waiting for pod running")
		serverPod, err = f.ClientSet.CoreV1().Pods(f.Namespace.Name).Get(serverPod.Name, metav1.GetOptions{})
		framework.ExpectNoError(err, "getting pod")

		framework.ExpectNoError(
			framework.CheckConnectivityToHost(f, "", "client-pod", serverPod.Status.PodIP, framework.IPv4PingCommand, 30))
		err = f.ClientSet.CoreV1().Pods(f.Namespace.Name).Delete("server-pod", &metav1.DeleteOptions{})
		framework.ExpectNoError(err, "deleting pod")
	})

	ginkgo.It("should enable pod-node communication", func() {
		nodeList := framework.GetReadySchedulableNodesOrDie(f.ClientSet)
		internalIP, err := framework.GetNodeInternalIP(&nodeList.Items[0])
		framework.ExpectNoError(err, "getting node internal IP")
		framework.ExpectNoError(
			framework.CheckConnectivityToHost(f, "", "client-pod", internalIP, framework.IPv4PingCommand, 30))
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
					Command: []string{"sleep", "infinity"},
				},
			},
			RestartPolicy: v1.RestartPolicyNever,
		},
	}
}
