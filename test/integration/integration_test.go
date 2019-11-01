package integration

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"k8s.io/api/core/v1"
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

	ginkgo.Context("Host ip rule test", func() {
		ginkgo.It("Should test something 1", func() {
			applyTestDeployment()
			scaleTestDeployment(1)
			podName := getFirstPodName()

			go func() {
				KubectlPortForward(f.Namespace.Name, podName, "80")
			}()

			resp, err := http.Get("http://localhost/")
			defer resp.Body.Close()
			Expect(err).Should(BeNil())

			body, err := ioutil.ReadAll(resp.Body)
			Expect(err).Should(BeNil())

			fmt.Printf(string(body))
		})
	})

	ginkgo.It("should enable pod-pod communication", func() {
		serverPod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "server-pod",
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:    "c",
						Image:   framework.BusyBoxImage,
						Command: []string{"sleep", "60"},
					},
				},
				RestartPolicy: v1.RestartPolicyNever,
			},
		}
		serverPod, err := f.ClientSet.CoreV1().Pods(f.Namespace.Name).Create(serverPod)
		framework.ExpectNoError(err, "creating pod")
		framework.ExpectNoError(f.WaitForPodRunning(serverPod.Name), "waiting for pod running")
		serverPod, err = f.ClientSet.CoreV1().Pods(f.Namespace.Name).Get(serverPod.Name, metav1.GetOptions{})
		framework.ExpectNoError(err, "getting pod")

		framework.ExpectNoError(
			framework.CheckConnectivityToHost(f, "", "client-pod", serverPod.Status.PodIP, framework.IPv4PingCommand, 30))
	})

	ginkgo.It("should enable pod-node communication", func() {
		nodeList := framework.GetReadySchedulableNodesOrDie(f.ClientSet)
		internalIP, err := framework.GetNodeInternalIP(&nodeList.Items[0])
		framework.ExpectNoError(err, "getting node internal IP")
		framework.ExpectNoError(
			framework.CheckConnectivityToHost(f, "", "client-pod", internalIP, framework.IPv4PingCommand, 30))
	})

})

func applyTestDeployment() {
	stdout, stderr, err := KubectlApply(fmt.Sprintf("%s/test-deployment.yaml", LocalTestContext.AssetsDir))
	fmt.Printf("%s\n%s", stdout, stderr)
	if err != nil {
		fmt.Printf("error applying test deployment: %s", err.Error())
	}
	Expect(err).Should(BeNil())
}

func scaleTestDeployment(replicas int) {
	stdout, stderr, err := KubectlScale(integrationTestAppName, strconv.Itoa(replicas))
	fmt.Printf("%s\n%s", stdout, stderr)
	if err != nil {
		fmt.Printf("error scaling test deployment: %s\n", err)
	}
	Expect(err).Should(BeNil())
}

func getFirstPodName() string {
	var stdout, stderr bytes.Buffer
	cmd := framework.KubectlCmd("get", "pods", "-lapp=test", "-o=jsonpath='{.items[0].metadata.name}'")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	fmt.Printf("%s\n%s", stdout.String(), stderr.String())
	if err != nil {
		fmt.Printf("kubectl get pods error: %s\n", err.Error())
	}
	Expect(err).Should(BeNil())

	return stdout.String()
}

func KubectlPortForward(namespace, podName, port string, args ...string) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	cmdArgs := []string{
		"port-forward",
		fmt.Sprintf("--namespace=%v", namespace),
		podName,
		fmt.Sprintf("%v", port),
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := framework.KubectlCmd(cmdArgs...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	fmt.Printf("Port Forwarding '%s %s'\n", cmd.Path, strings.Join(cmdArgs, " "))
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func KubectlApply(path string, args ...string) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	cmdArgs := []string{
		"apply",
		"-f",
		path,
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := framework.KubectlCmd(cmdArgs...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	fmt.Printf("Applying '%s %s'\n", cmd.Path, strings.Join(cmdArgs, " "))
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func KubectlScale(deployment, replicas string, args ...string) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	cmdArgs := []string{
		"scale",
		"deployment",
		deployment,
		fmt.Sprintf("--replicas=%s", replicas),
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := framework.KubectlCmd(cmdArgs...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	fmt.Printf("Scaling '%s %s'\n", cmd.Path, strings.Join(cmdArgs, " "))
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
