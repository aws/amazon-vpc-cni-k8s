package pod_test

import (
	"context"
	"os"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	podcontroller "github.com/aws/amazon-vpc-cni-k8s/pkg/controller/pod"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/controller/pod/mocks"
)

func newReconcilerWithFakeClient(ctrl *gomock.Controller, cl client.Client) *podcontroller.ReconcilePod {
	s := scheme.Scheme

	mgr := mocks.NewMockManager(ctrl)
	mgr.EXPECT().GetClient().Return(cl)
	mgr.EXPECT().GetScheme().Return(s)

	return podcontroller.NewReconciler(mgr)
}

func createTestPod(namespace string, name string, nodeName string) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
	}
}

func TestReconcilePod_InitPodList(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	myNodeName := "testMyNode"
	_ = os.Setenv("MY_NODE_NAME", myNodeName)

	cl := fake.NewFakeClient(
		createTestPod("default", "myPod", myNodeName),
		createTestPod("default", "anotherPod", "anotherNode"),
		)
	reconciler := newReconcilerWithFakeClient(ctrl, cl)

	err := reconciler.InitPodList()
	assert.NoError(t, err)

	podInfo, err := reconciler.K8SGetLocalPodIPs()
	assert.NoError(t, err)
	assert.Len(t, podInfo, 1)
	assert.Equal(t, "default", podInfo[0].Namespace)
	assert.Equal(t, "myPod", podInfo[0].Name)
}

func TestReconcilePod_Reconcile(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	myNodeName := "testMyNode"
	_ = os.Setenv("MY_NODE_NAME", myNodeName)

	cl := fake.NewFakeClient()
	reconciler := newReconcilerWithFakeClient(ctrl, cl)

	// manually sync reconciler
	err := reconciler.InitPodList()
	assert.NoError(t, err)

	// create pod
	pod := createTestPod("default", "myPod", myNodeName)
	req := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "myPod"}}
	_ = cl.Create(context.TODO(), pod)
	_, err = reconciler.Reconcile(req)
	assert.NoError(t, err)

	podInfo, err := reconciler.K8SGetLocalPodIPs()
	assert.NoError(t, err)
	assert.Len(t, podInfo, 1)
	assert.Equal(t, "default", podInfo[0].Namespace)
	assert.Equal(t, "myPod", podInfo[0].Name)

	// update pod
	pod.Status.PodIP = "10.20.30.40"
	_ = cl.Update(context.TODO(), pod)
	_, err = reconciler.Reconcile(req)
	assert.NoError(t, err)

	podInfo, err = reconciler.K8SGetLocalPodIPs()
	assert.NoError(t, err)
	assert.Len(t, podInfo, 1)
	assert.Equal(t, "10.20.30.40", podInfo[0].IP)

	// delete pod
	_ = cl.Delete(context.TODO(), pod)
	_, err = reconciler.Reconcile(req)
	assert.NoError(t, err)

	podInfo, err = reconciler.K8SGetLocalPodIPs()
	assert.NoError(t, err)
	assert.Len(t, podInfo, 0)
}
