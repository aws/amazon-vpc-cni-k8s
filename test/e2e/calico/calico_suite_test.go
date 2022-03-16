package calico

import (
	"context"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
)

var (
	f                *framework.Framework
	err              error
	uiNamespace      = "management-ui"
	clientNamespace  = "client"
	starsNamespace   = "stars"
	uiLabel          = map[string]string{"role": "management-ui"}
	clientLabel      = map[string]string{"role": "client"}
	feLabel          = map[string]string{"role": "frontend"}
	beLabel          = map[string]string{"role": "backend"}
	nodeArchKey      = "kubernetes.io/arch"
	nodeArchARMValue = "arm64"
	nodeArchAMDValue = "amd64"
	uiPod            v1.Pod
	clientPod        v1.Pod
	fePod            v1.Pod
	bePod            v1.Pod
)

func TestCalicoPoliciesWithVPCCNI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Calico with VPC CNI e2e Test Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)
	By("installing Calico operator")

	tigeraVersion := f.Options.CalicoVersion
	err := f.InstallationManager.InstallTigeraOperator(tigeraVersion)
	Expect(err).ToNot(HaveOccurred())

	By("Patching ARM64 node unschedulable")
	err = updateNodesSchedulability(nodeArchKey, nodeArchARMValue, true)
	Expect(err).ToNot(HaveOccurred())

	By("installing Calico Start Policy Tests Resources")
	err = f.K8sResourceManagers.NamespaceManager().CreateNamespaceWithLabels(uiNamespace, map[string]string{"role": "management-ui"})
	Expect(err).ToNot(HaveOccurred())
	err = f.K8sResourceManagers.NamespaceManager().CreateNamespaceWithLabels(clientNamespace, map[string]string{"role": "client"})
	Expect(err).ToNot(HaveOccurred())
	err = f.K8sResourceManagers.NamespaceManager().CreateNamespace(starsNamespace)
	Expect(err).ToNot(HaveOccurred())

	uiContainer := manifest.NewBaseContainer().
		Name("management-ui").
		Image("calico/star-collect:v0.1.0").
		ImagePullPolicy(v1.PullAlways).
		Port(v1.ContainerPort{ContainerPort: 9001}).
		Build()
	uiDeployment := manifest.NewCalicoStarDeploymentBuilder().
		Namespace(uiNamespace).
		Name("management-ui").
		Container(uiContainer).
		Replicas(1).
		PodLabel("role", "management-ui").
		NodeSelector(nodeArchKey, nodeArchAMDValue).
		Labels(map[string]string{"role": "management-ui"}).
		Build()
	_, err = f.K8sResourceManagers.DeploymentManager().CreateAndWaitTillDeploymentIsReady(uiDeployment, utils.DefaultDeploymentReadyTimeout)
	Expect(err).ToNot(HaveOccurred())

	clientContainer := manifest.NewBaseContainer().
		Name("client").
		Image("calico/star-probe:v0.1.0").
		ImagePullPolicy(v1.PullAlways).
		Command([]string{"probe", "--urls=http://frontend.stars:80/status,http://backend.stars:6379/status"}).
		Port(v1.ContainerPort{ContainerPort: 9000}).
		Build()
	clientDeployment := manifest.NewCalicoStarDeploymentBuilder().
		Namespace(clientNamespace).
		Name("client").
		Container(clientContainer).
		Replicas(1).
		PodLabel("role", "client").
		NodeSelector(nodeArchKey, nodeArchAMDValue).
		Labels(map[string]string{"role": "client"}).
		Build()
	_, err = f.K8sResourceManagers.DeploymentManager().CreateAndWaitTillDeploymentIsReady(clientDeployment, utils.DefaultDeploymentReadyTimeout)
	Expect(err).ToNot(HaveOccurred())

	feContainer := manifest.NewBaseContainer().
		Name("frontend").
		Image("calico/star-probe:v0.1.0").
		ImagePullPolicy(v1.PullAlways).
		Command([]string{
			"probe",
			"--http-port=80",
			"--urls=http://frontend.stars:80/status,http://backend.stars:6379/status,http://client.client:9000/status",
		}).
		Port(v1.ContainerPort{ContainerPort: 80}).
		Build()
	feDeployment := manifest.NewCalicoStarDeploymentBuilder().
		Namespace(starsNamespace).
		Name("frontend").
		Container(feContainer).
		Replicas(1).
		PodLabel("role", "frontend").
		NodeSelector(nodeArchKey, nodeArchAMDValue).
		Labels(map[string]string{"role": "frontend"}).
		Build()
	_, err = f.K8sResourceManagers.DeploymentManager().CreateAndWaitTillDeploymentIsReady(feDeployment, utils.DefaultDeploymentReadyTimeout)
	Expect(err).ToNot(HaveOccurred())

	beContainer := manifest.NewBaseContainer().
		Name("backend").
		Image("calico/star-probe:v0.1.0").
		ImagePullPolicy(v1.PullAlways).
		Command([]string{
			"probe",
			"--http-port=6379",
			"--urls=http://frontend.stars:80/status,http://backend.stars:6379/status,http://client.client:9000/status",
		}).
		Port(v1.ContainerPort{ContainerPort: 6379}).
		Build()
	beDeployment := manifest.NewCalicoStarDeploymentBuilder().
		Namespace(starsNamespace).
		Name("backend").
		Container(beContainer).
		Replicas(1).
		PodLabel("role", "backend").
		NodeSelector(nodeArchKey, nodeArchAMDValue).
		Labels(map[string]string{"role": "backend"}).
		Build()
	_, err = f.K8sResourceManagers.DeploymentManager().CreateAndWaitTillDeploymentIsReady(beDeployment, utils.DefaultDeploymentReadyTimeout)
	Expect(err).ToNot(HaveOccurred())

	ui := manifest.NewHTTPService().
		Name("management-ui").
		Namespace("management-ui").
		ServiceType(v1.ServiceTypeNodePort).
		NodePort(30002).
		Port(9001).
		Selector("role", "management-ui").
		Build()
	_, err = f.K8sResourceManagers.ServiceManager().CreateService(context.Background(), ui)
	Expect(err).NotTo(HaveOccurred())

	client := manifest.NewHTTPService().
		Name("client").
		Namespace("client").
		Port(9000).
		Selector("role", "client").
		Build()
	_, err = f.K8sResourceManagers.ServiceManager().CreateService(context.Background(), client)
	Expect(err).NotTo(HaveOccurred())

	frontend := manifest.NewHTTPService().
		Name("frontend").
		Namespace("stars").
		Port(80).
		Selector("role", "frontend").
		Build()
	_, err = f.K8sResourceManagers.ServiceManager().CreateService(context.Background(), frontend)
	Expect(err).NotTo(HaveOccurred())

	backend := manifest.NewHTTPService().
		Name("backend").
		Namespace("stars").
		Port(6379).
		Selector("role", "backend").
		Build()
	_, err = f.K8sResourceManagers.ServiceManager().CreateService(context.Background(), backend)
	Expect(err).NotTo(HaveOccurred())

	uiPods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelectorMap(uiLabel)
	Expect(err).ToNot(HaveOccurred())
	clientPods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelectorMap(clientLabel)
	Expect(err).ToNot(HaveOccurred())
	fePods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelectorMap(feLabel)
	Expect(err).NotTo(HaveOccurred())
	bePods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelectorMap(beLabel)
	Expect(err).NotTo(HaveOccurred())
	uiPod = uiPods.Items[0]
	clientPod = clientPods.Items[0]
	fePod = fePods.Items[0]
	bePod = bePods.Items[0]

	By("Installing netcat in all STAR containers for connectivity tests")
	err = installNetcatToolInContainer(uiPod.Name, uiPod.Namespace)
	Expect(err).NotTo(HaveOccurred())
	err = installNetcatToolInContainer(clientPod.Name, clientPod.Namespace)
	Expect(err).NotTo(HaveOccurred())
	err = installNetcatToolInContainer(fePod.Name, fePod.Namespace)
	Expect(err).NotTo(HaveOccurred())
	err = installNetcatToolInContainer(bePod.Name, bePod.Namespace)
	Expect(err).NotTo(HaveOccurred())

	assignPodsMetadataForTests()
})

var _ = AfterSuite(func() {
	By("Remove All Star Resources")
	f.K8sResourceManagers.NamespaceManager().DeleteAndWaitTillNamespaceDeleted(uiNamespace)
	f.K8sResourceManagers.NamespaceManager().DeleteAndWaitTillNamespaceDeleted(clientNamespace)
	f.K8sResourceManagers.NamespaceManager().DeleteAndWaitTillNamespaceDeleted(starsNamespace)
	f.K8sResourceManagers.NetworkPolicyManager().DeleteNetworkPolicy(&networkPolicyDenyStars)
	f.K8sResourceManagers.NetworkPolicyManager().DeleteNetworkPolicy(&networkPolicyDenyClient)
	f.K8sResourceManagers.NetworkPolicyManager().DeleteNetworkPolicy(&networkPolicyAllowUIStars)
	f.K8sResourceManagers.NetworkPolicyManager().DeleteNetworkPolicy(&networkPolicyAllowUIClient)
	f.K8sResourceManagers.NetworkPolicyManager().DeleteNetworkPolicy(&networkPolicyAllowFE)
	f.K8sResourceManagers.NetworkPolicyManager().DeleteNetworkPolicy(&networkPolicyAllowClient)

	By("Helm Uninstall Calico Installation")
	f.InstallationManager.UninstallTigeraOperator()

	By("Restore ARM64 Nodes Schedulability")
	updateNodesSchedulability(nodeArchKey, nodeArchARMValue, false)
})

func installNetcatToolInContainer(name string, namespace string) error {
	_, _, err := f.K8sResourceManagers.PodManager().PodExec(
		namespace,
		name,
		[]string{"apt-get", "update"})

	_, _, err = f.K8sResourceManagers.PodManager().PodExec(
		namespace,
		name,
		[]string{"apt-get", "install", "netcat", "-y"})
	return err
}

func assignPodsMetadataForTests() {
	uiPodName = uiPod.Name
	clientPodName = clientPod.Name
	fePodName = fePod.Name
	bePodName = bePod.Name

	uiPodNamespace = uiPod.Namespace
	clientPodNamespace = clientPod.Namespace
	fePodNamespace = fePod.Namespace
	bePodNamespace = bePod.Namespace

	clientIP = clientPod.Status.PodIP
	clientPort = int(clientPod.Spec.Containers[0].Ports[0].ContainerPort)
	feIP = fePod.Status.PodIP
	fePort = int(fePod.Spec.Containers[0].Ports[0].ContainerPort)
	beIP = bePod.Status.PodIP
	bePort = int(bePod.Spec.Containers[0].Ports[0].ContainerPort)
}

func updateNodesSchedulability(key string, value string, unschedule bool) error {
	nodes, err := f.K8sResourceManagers.NodeManager().GetNodes(key, value)
	if err != nil {
		return err
	}

	for _, node := range nodes.Items {
		newNode := node.DeepCopy()
		newNode.Spec.Unschedulable = unschedule

		if err = f.K8sResourceManagers.NodeManager().UpdateNode(&node, newNode); err != nil {
			return err
		}
	}
	return err
}
