package soak_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	awssdk "github.com/aws/aws-sdk-go/aws"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	appsV1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultSoakTestNSName = "cni-automation-soak"
	stateFileHostDir      = "/var/run/aws-node"
	stateFileMountPath    = "/var/run/aws-node"
	stateFilePathname     = "/var/run/aws-node/ipam.json"
)

var _ = Describe("launch Pod on single node", Serial, func() {
	var (
		nominatedNode corev1.Node
		sandBoxNS     *corev1.Namespace
	)

	BeforeEach(func(ctx context.Context) {
		By("nominate node for testing", func() {
			nodeList, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
			Expect(err).NotTo(HaveOccurred())
			numOfNodes := len(nodeList.Items)
			Expect(numOfNodes).Should(BeNumerically(">", 1))
			nominatedNode = nodeList.Items[numOfNodes-1]
			f.Logger.Info("node nominated", "nodeName", nominatedNode.Name)
		})

		By("setup sandbox namespace", func() {
			ns := &corev1.Namespace{
				ObjectMeta: metaV1.ObjectMeta{
					Name: defaultSoakTestNSName,
				},
			}
			err := f.K8sClient.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())
			sandBoxNS = ns
		})
	})

	AfterEach(func(ctx context.Context) {
		if sandBoxNS != nil {
			By("teardown sandbox namespace", func() {
				err := f.K8sClient.Delete(ctx, sandBoxNS)
				Expect(err).NotTo(HaveOccurred())
				err = f.K8sResourceManagers.NamespaceManager().WaitUntilNamespaceDeleted(ctx, sandBoxNS)
				Expect(err).NotTo(HaveOccurred())
			})
		}
	})

	Describe("state file based checkpoint", func() {
		// we create a Pod with state file mount to inspect the content of state file
		// this is future-proof as we will switch CNI to a minimal base image without shell tools.
		var stateFileInspectorPod *corev1.Pod

		BeforeEach(func(ctx context.Context) {
			By("setup state file inspector pod", func() {
				volume := corev1.Volume{
					Name: "run-dir",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: stateFileHostDir,
						},
					},
				}
				volumeMount := corev1.VolumeMount{
					Name:      "run-dir",
					MountPath: stateFileMountPath,
				}
				pod := manifest.NewDefaultPodBuilder().
					Namespace(sandBoxNS.Name).
					Name("inspector").
					Container(manifest.NewBusyBoxContainerBuilder(f.Options.TestImageRegistry).Build()).
					NodeName(nominatedNode.Name).
					MountVolume([]corev1.Volume{volume}, []corev1.VolumeMount{volumeMount}).
					Build()
				err := f.K8sClient.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred())
				stateFileInspectorPod = pod
				err = f.K8sResourceManagers.PodManager().WaitUntilPodRunning(ctx, stateFileInspectorPod)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		AfterEach(func(ctx context.Context) {
			if stateFileInspectorPod != nil {
				By("teardown state file inspector pod", func() {
					err := f.K8sClient.Delete(ctx, stateFileInspectorPod)
					Expect(err).NotTo(HaveOccurred())
					err = f.K8sResourceManagers.PodManager().WaitUntilPodDeleted(ctx, stateFileInspectorPod)
					Expect(err).NotTo(HaveOccurred())
				})
			}
		})

		// This test will set up pod and teardown pods by scale a busybox deployment in calculated steps.
		// It expects the state file is eventually consist with pod state from APIServer.
		It("should remain consistent with pods on node when setup/teardown normal pods", func(ctx context.Context) {
			// TODO: eliminate the need of MAX_POD_PER_NODE env by automatically detect maxPod from instance type and CNI configuration
			envMaxPodPerNode := os.Getenv("MAX_POD_PER_NODE")
			if envMaxPodPerNode == "" {
				Skip("MAX_POD_PER_NODE env not set")
			}
			maxPodPerNode, err := strconv.ParseInt(envMaxPodPerNode, 10, 64)
			Expect(err).NotTo(HaveOccurred())

			podsOnNominatedNode, err := listPodsWithNodeName(ctx, f, nominatedNode.Name)
			Expect(err).NotTo(HaveOccurred())
			availablePodCount := int(maxPodPerNode) - len(podsOnNominatedNode)
			var deploymentReplicaSteps []int
			for count := availablePodCount; count > 0; {
				deploymentReplicaSteps = append(deploymentReplicaSteps, count)
				count = count / 2
			}
			deploymentReplicaSteps = append(deploymentReplicaSteps, 0)
			f.Logger.Info("planned deployment steps", "deploymentReplicaSteps", deploymentReplicaSteps)

			var busyBoxDP *appsV1.Deployment
			By("create deployment with 0 replicas", func() {
				busyBoxDP = manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
					Namespace(sandBoxNS.Name).
					Name("busybox").
					NodeName(nominatedNode.Name).
					Replicas(0).
					Build()
				err := f.K8sClient.Create(ctx, busyBoxDP)
				Expect(err).NotTo(HaveOccurred())
				_, err = f.K8sResourceManagers.DeploymentManager().WaitUntilDeploymentReady(ctx, busyBoxDP)
				Expect(err).NotTo(HaveOccurred())
			})

			Eventually(validateStateFileConsistency).WithContext(ctx).WithArguments(f, nominatedNode, stateFileInspectorPod).WithTimeout(1 * time.Minute).ShouldNot(HaveOccurred())
			for _, replica := range deploymentReplicaSteps {
				By(fmt.Sprintf("scale deployment to %d replicas", replica), func() {
					err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
						busyBoxDPKey := utils.NamespacedName(busyBoxDP)
						if err := f.K8sClient.Get(ctx, busyBoxDPKey, busyBoxDP); err != nil {
							return err
						}
						oldBusyBoxDP := busyBoxDP.DeepCopy()
						busyBoxDP.Spec.Replicas = awssdk.Int32(int32(replica))
						return f.K8sClient.Patch(ctx, busyBoxDP, client.MergeFromWithOptions(oldBusyBoxDP, client.MergeFromWithOptimisticLock{}))
					})
					Expect(err).NotTo(HaveOccurred())
					_, err = f.K8sResourceManagers.DeploymentManager().WaitUntilDeploymentReady(ctx, busyBoxDP)
					Expect(err).NotTo(HaveOccurred())
					Eventually(validateStateFileConsistency).WithContext(ctx).WithArguments(f, nominatedNode, stateFileInspectorPod).WithTimeout(1 * time.Minute).ShouldNot(HaveOccurred())
				})
			}

			By("delete deployment", func() {
				err := f.K8sClient.Delete(ctx, busyBoxDP)
				Expect(err).NotTo(HaveOccurred())
				err = f.K8sResourceManagers.DeploymentManager().WaitUntilDeploymentDeleted(ctx, busyBoxDP)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})

// validateStateFileConsistency validates the state file from inspectorPod matches pod state on node.
func validateStateFileConsistency(ctx context.Context, f *framework.Framework, nominatedNode corev1.Node, inspectorPod *corev1.Pod) error {
	podsOnNominatedNode, err := listPodsWithNodeName(ctx, f, nominatedNode.Name)
	if err != nil {
		return err
	}
	stateFileContent, err := readFileFromPod(ctx, f, inspectorPod, stateFilePathname)
	if err != nil {
		return err
	}
	var checkpointData datastore.CheckpointData
	if err := json.Unmarshal([]byte(stateFileContent), &checkpointData); err != nil {
		return err
	}

	podIPByPodKey := make(map[string]string)
	for _, pod := range podsOnNominatedNode {
		if !pod.Spec.HostNetwork {
			podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
			podIPByPodKey[podKey] = pod.Status.PodIP
		}
	}
	f.Logger.Info("check state file consistency", "podIPByPodKey", podIPByPodKey, "checkpointData", checkpointData)
	if len(podIPByPodKey) != len(checkpointData.Allocations) {
		return errors.Errorf("allocation count don't match: %v/%v", len(podIPByPodKey), len(checkpointData.Allocations))
	}
	for podKey, podIP := range podIPByPodKey {
		foundPodIPAllocation := false
		for _, allocation := range checkpointData.Allocations {
			if allocation.IPv4 == podIP || allocation.IPv6 == podIP {
				if allocation.Metadata.K8SPodName != "" {
					podKeyFromMetadata := fmt.Sprintf("%s/%s", allocation.Metadata.K8SPodNamespace, allocation.Metadata.K8SPodName)
					if podKey != podKeyFromMetadata {
						return errors.Errorf("allocation metadata don't match for podIP %v: %v/%v", podIP, podKey, podKeyFromMetadata)
					}
				}

				foundPodIPAllocation = true
				break
			}
		}
		if !foundPodIPAllocation {
			return errors.Errorf("allocation not found for pod %v: %v", podKey, podIP)
		}
	}
	return nil
}

func listPodsWithNodeName(ctx context.Context, f *framework.Framework, nodeName string) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}
	nodeNameSelector := fields.Set{"spec.nodeName": nodeName}.AsSelector()
	if err := f.K8sClient.List(ctx, podList, client.MatchingFieldsSelector{Selector: nodeNameSelector}); err != nil {
		return nil, err
	}
	return podList.Items, nil
}

func readFileFromPod(_ context.Context, f *framework.Framework, pod *corev1.Pod, filepath string) (string, error) {
	command := []string{"cat", filepath}
	stdOut, stdErr, err := f.K8sResourceManagers.PodManager().PodExec(pod.Namespace, pod.Name, command)
	if err != nil {
		return "", err
	}
	if stdErr != "" {
		return "", errors.New(stdErr)
	}
	return stdOut, nil
}
