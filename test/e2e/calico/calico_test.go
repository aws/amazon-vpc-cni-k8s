package calico

import (
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	retries       = 3
	interval      = 3
	testTimeOut   = 15.0
	netcatTimeOut = "1"

	uiPodName     string
	clientPodName string
	fePodName     string
	bePodName     string

	uiPodNamespace     string
	clientPodNamespace string
	fePodNamespace     string
	bePodNamespace     string

	clientIP   string
	clientPort int
	feIP       string
	fePort     int
	beIP       string
	bePort     int

	networkPolicyAllowFE = netv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backend-policy",
			Namespace: starsNamespace,
		},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"role": "backend"}},
			Ingress: []netv1.NetworkPolicyIngressRule{
				{
					From: []netv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"role": "frontend"},
							},
						},
					},
					Ports: []netv1.NetworkPolicyPort{
						{
							Port: &intstr.IntOrString{IntVal: 6379},
						},
					},
				},
			},
		},
	}
	networkPolicyAllowClient = netv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "frontend-policy",
			Namespace: starsNamespace,
		},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"role": "frontend"}},
			Ingress: []netv1.NetworkPolicyIngressRule{
				{
					From: []netv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"role": "client"},
							},
						},
					},
					Ports: []netv1.NetworkPolicyPort{
						{
							Port: &intstr.IntOrString{IntVal: 80},
						},
					},
				},
			},
		},
	}
	networkPolicyDenyStars = netv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-deny",
			Namespace: starsNamespace,
		},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{},
			},
		},
	}
	networkPolicyDenyClient = netv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-deny",
			Namespace: clientNamespace,
		},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{},
			},
		},
	}
	networkPolicyAllowUIStars = netv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allow-ui",
			Namespace: starsNamespace,
		},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{}},
			Ingress: []netv1.NetworkPolicyIngressRule{
				{
					From: []netv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"role": "management-ui"},
							},
						},
					},
				},
			},
		},
	}
	networkPolicyAllowUIClient = netv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allow-ui",
			Namespace: clientNamespace,
		},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{}},
			Ingress: []netv1.NetworkPolicyIngressRule{
				{
					From: []netv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"role": "management-ui"},
							},
						},
					},
				},
			},
		},
	}
)

var _ = Describe("Calico Star Network Policy Tests", func() {
	Context("Test All Allowed Network Policy", func() {
		It("Allow Mode: Test UI connecting to client pod", func() {
			err := retryPodExecTest(
				uiPodNamespace,
				uiPodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, clientIP, strconv.Itoa(clientPort)},
			)
			Expect(err).NotTo(HaveOccurred())
		}, testTimeOut)

		It("Allow Mode: Test UI connecting to frontend pod", func() {
			err := retryPodExecTest(
				uiPodNamespace,
				uiPodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, feIP, strconv.Itoa(fePort)},
			)
			Expect(err).NotTo(HaveOccurred())
		}, testTimeOut)

		It("Allow Mode: Test UI connecting to backend pod", func() {
			err := retryPodExecTest(
				uiPodNamespace,
				uiPodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, beIP, strconv.Itoa(bePort)},
			)
			Expect(err).NotTo(HaveOccurred())
		}, testTimeOut)

		It("Allow Mode: Test Client connecting to frontend pod", func() {
			err := retryPodExecTest(
				clientPodNamespace,
				clientPodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, feIP, strconv.Itoa(fePort)},
			)
			Expect(err).NotTo(HaveOccurred())
		}, testTimeOut)

		It("Allow Mode: Test frontend connecting to backend pod", func() {
			err := retryPodExecTest(
				fePodNamespace,
				fePodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, beIP, strconv.Itoa(bePort)},
			)
			Expect(err).NotTo(HaveOccurred())
		}, testTimeOut)
	})

	Context("Test All Denied Network Policy", func() {
		It("Applying ALL DENY network policy to the cluster", func() {
			err := f.K8sResourceManagers.NetworkPolicyManager().CreateNetworkPolicy(&networkPolicyDenyStars)
			Expect(err).NotTo(HaveOccurred())
			err = f.K8sResourceManagers.NetworkPolicyManager().CreateNetworkPolicy(&networkPolicyDenyClient)
			Expect(err).NotTo(HaveOccurred())
		}, testTimeOut)

		It("Deny Mode: Test UI connecting to client pod", func() {
			err := retryPodExecTest(
				uiPodNamespace,
				uiPodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, clientIP, strconv.Itoa(clientPort)},
			)
			Expect(err).To(HaveOccurred())
		}, testTimeOut)

		It("Deny Mode: Test UI connecting to frontend pod", func() {
			err := retryPodExecTest(
				uiPodNamespace,
				uiPodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, feIP, strconv.Itoa(fePort)},
			)
			Expect(err).To(HaveOccurred())
		}, testTimeOut)

		It("Deny Mode: Test UI connecting to backend pod", func() {
			err := retryPodExecTest(
				uiPodNamespace,
				uiPodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, beIP, strconv.Itoa(fePort)},
			)
			Expect(err).To(HaveOccurred())
		}, testTimeOut)

		It("Deny Mode: Test client connecting to frontend pod", func() {
			err := retryPodExecTest(
				clientPodNamespace,
				clientPodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, feIP, strconv.Itoa(bePort)},
			)
			Expect(err).To(HaveOccurred())
		}, testTimeOut)

		It("Deny Mode: Test frontend connecting to backend pod", func() {
			err := retryPodExecTest(
				fePodNamespace,
				fePodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, beIP, strconv.Itoa(bePort)},
			)
			Expect(err).To(HaveOccurred())
		}, testTimeOut)
	})

	Context("Test allowing UI connecting to client", func() {
		It("Applying network policy to allow UI connecting to client", func() {

			err := f.K8sResourceManagers.NetworkPolicyManager().CreateNetworkPolicy(&networkPolicyAllowUIStars)
			Expect(err).NotTo(HaveOccurred())
			err = f.K8sResourceManagers.NetworkPolicyManager().CreateNetworkPolicy(&networkPolicyAllowUIClient)
			Expect(err).NotTo(HaveOccurred())
		}, testTimeOut)
		It("Allow Mode: Test UI connecting to client pod", func() {
			err := retryPodExecTest(
				uiPodNamespace,
				uiPodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, clientIP, strconv.Itoa(clientPort)},
			)
			Expect(err).NotTo(HaveOccurred())
		}, testTimeOut)

		It("Allow Mode: Test UI connecting to frontend pod", func() {
			err := retryPodExecTest(
				uiPodNamespace,
				uiPodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, feIP, strconv.Itoa(fePort)},
			)
			Expect(err).NotTo(HaveOccurred())
		}, testTimeOut)

		It("Allow Mode: Test UI connecting to backend pod", func() {
			err := retryPodExecTest(
				uiPodNamespace,
				uiPodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, beIP, strconv.Itoa(bePort)},
			)
			Expect(err).NotTo(HaveOccurred())
		}, testTimeOut)

		It("Deny Mode: Test client connecting to frontend pod", func() {
			err := retryPodExecTest(
				clientPodNamespace,
				clientPodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, feIP, strconv.Itoa(fePort)},
			)
			Expect(err).To(HaveOccurred())
		}, testTimeOut)
	})

	Context("Allow traffic from frontend to backend", func() {
		It("Deny Mode: Test frontend shouldn't connect to backend pod before updating policy", func() {
			err := retryPodExecTest(
				fePodNamespace,
				fePodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, beIP, strconv.Itoa(bePort)},
			)
			Expect(err).To(HaveOccurred())
		}, testTimeOut)

		It("Applying network policy to allow frontend connecting to backend", func() {
			err := f.K8sResourceManagers.NetworkPolicyManager().CreateNetworkPolicy(&networkPolicyAllowFE)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Allow Mode: Test frontend should connect to backend pod after updating policy", func() {
			err := retryPodExecTest(
				fePodNamespace,
				fePodName,
				[]string{"nc", "-zv", beIP, strconv.Itoa(bePort)},
			)
			Expect(err).NotTo(HaveOccurred())
		}, testTimeOut)
	})

	Context("Allow traffic from client to frontend", func() {
		It("Deny Mode: Test client shouldn't connect to frontend pod before updating policy", func() {
			err := retryPodExecTest(
				clientPodNamespace,
				clientPodName,
				[]string{"nc", "-zv", "-w", netcatTimeOut, feIP, strconv.Itoa(fePort)},
			)
			Expect(err).To(HaveOccurred())
		}, testTimeOut)

		It("Applying network policy to allow client connecting to frontend", func() {
			err := f.K8sResourceManagers.NetworkPolicyManager().CreateNetworkPolicy(&networkPolicyAllowClient)
			Expect(err).NotTo(HaveOccurred())
		}, testTimeOut)

		It("Allow Mode: Test client should connect to frontend pod after updating policy", func() {
			err := retryPodExecTest(
				clientPodNamespace,
				clientPodName,
				[]string{"nc", "-zv", feIP, strconv.Itoa(fePort)},
			)
			Expect(err).NotTo(HaveOccurred())
		}, testTimeOut)
	})
})

func retryPodExecTest(namespace string, name string, cmd []string) error {
	count := retries
	for {
		_, _, err := f.K8sResourceManagers.PodManager().PodExec(namespace, name, cmd)
		if err == nil || count == 0 {
			return err
		}
		count--
		wait := interval * (retries - count)
		time.Sleep(time.Second * time.Duration(wait))
	}
}
