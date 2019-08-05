package node_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nodecontroller "github.com/aws/amazon-vpc-cni-k8s/pkg/controller/node"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/controller/node/mocks"
)

const expectedRequeueTime = 5 * time.Second

func newReconcilerWithFakeClient(ctrl *gomock.Controller, cl client.Client) *nodecontroller.ReconcileNode {
	s := scheme.Scheme

	mgr := mocks.NewMockManager(ctrl)
	mgr.EXPECT().GetClient().Return(cl)
	mgr.EXPECT().GetScheme().Return(s)

	return nodecontroller.NewReconciler(mgr)
}

func updateNodeAnnotation(t *testing.T, cl client.Client, reconciler reconcile.Reconciler, nodeName string, configName string, toDelete bool) {
	node := corev1.Node{
		TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
	}

	// Fetch existing object
	key, _ := client.ObjectKeyFromObject(&node)
	_ = cl.Get(context.TODO(), key, &node)

	accessor, err := meta.Accessor(&node)
	assert.NoError(t, err)

	eniAnnotations := make(map[string]string)
	eniConfigAnnotationDef := nodecontroller.GetEniConfigAnnotationDef()

	if !toDelete {
		eniAnnotations[eniConfigAnnotationDef] = configName
	}
	accessor.SetAnnotations(eniAnnotations)

	err = cl.Create(context.TODO(), &node)
	if err != nil && errors.IsAlreadyExists(err) {
		err = cl.Update(context.TODO(), &node)
	}
	assert.NoError(t, err)

	// Mock request to simulate Reconcile() being called on the watched resource
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: nodeName,
		},
	}
	res, err := reconciler.Reconcile(req)
	assert.NoError(t, err)
	assert.Equal(t, expectedRequeueTime, res.RequeueAfter)
}

func updateNodeLabel(t *testing.T, cl client.Client, reconciler reconcile.Reconciler, nodeName string, configName string, toDelete bool) {
	node := corev1.Node{
		TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
	}

	// Fetch existing object
	key, _ := client.ObjectKeyFromObject(&node)
	_ = cl.Get(context.TODO(), key, &node)

	accessor, err := meta.Accessor(&node)
	assert.NoError(t, err)
	eniLabels := make(map[string]string)
	eniConfigLabelDef := nodecontroller.GetEniConfigLabelDef()

	if !toDelete {
		eniLabels[eniConfigLabelDef] = configName
	}
	accessor.SetLabels(eniLabels)

	err = cl.Create(context.TODO(), &node)
	if err != nil && errors.IsAlreadyExists(err) {
		err = cl.Update(context.TODO(), &node)
	}
	assert.NoError(t, err)

	// Mock request to simulate Reconcile() being called on the watched resource
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: nodeName,
		},
	}
	res, err := reconciler.Reconcile(req)
	assert.NoError(t, err)
	assert.Equal(t, expectedRequeueTime, res.RequeueAfter)
}

func TestGetMyEniAnnotation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	myNodeName := "testMyNodeWithAnnotation"
	myENIConfig := "testMyENIConfig"
	_ = os.Setenv("MY_NODE_NAME", myNodeName)

	cl := fake.NewFakeClient()
	reconciler := newReconcilerWithFakeClient(ctrl, cl)

	// Node not found yet
	assert.Equal(t, nodecontroller.EniConfigDefault, reconciler.GetMyENI())

	// Set node label
	updateNodeAnnotation(t, cl, reconciler, myNodeName, myENIConfig, false)
	assert.Equal(t, myENIConfig, reconciler.GetMyENI())

	// Delete node's myENIConfig label, then the value should fallback to default
	updateNodeAnnotation(t, cl, reconciler, myNodeName, myENIConfig, true)
	assert.Equal(t, nodecontroller.EniConfigDefault, reconciler.GetMyENI())
}

func TestGetMyEniLabel(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	myNodeName := "testMyNodeWithLabel"
	myENIConfig := "testMyENIConfig"
	_ = os.Setenv("MY_NODE_NAME", myNodeName)

	cl := fake.NewFakeClient()
	reconciler := newReconcilerWithFakeClient(ctrl, cl)

	// Node not found yet
	assert.Equal(t, nodecontroller.EniConfigDefault, reconciler.GetMyENI())

	// Set node label
	updateNodeLabel(t, cl, reconciler, myNodeName, myENIConfig, false)
	assert.Equal(t, myENIConfig, reconciler.GetMyENI())

	// Delete node's myENIConfig label, then the value should fallback to default
	updateNodeLabel(t, cl, reconciler, myNodeName, myENIConfig, true)
	assert.Equal(t, nodecontroller.EniConfigDefault, reconciler.GetMyENI())
}


func TestGetMyEniPrecedence(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	myNodeName := "testMyNodeWithLabelAndAnnotation"
	myENIConfigAnnotation := "testMyENIConfigAnnotation"
	myENIConfigLabel := "testMyENIConfigLabel"
	_ = os.Setenv("MY_NODE_NAME", myNodeName)

	cl := fake.NewFakeClient()
	reconciler := newReconcilerWithFakeClient(ctrl, cl)

	// Set node label
	updateNodeLabel(t, cl, reconciler, myNodeName, myENIConfigLabel, false)
	assert.Equal(t, myENIConfigLabel, reconciler.GetMyENI())

	// Set node annotation
	updateNodeAnnotation(t, cl, reconciler, myNodeName, myENIConfigAnnotation, false)
	assert.Equal(t, myENIConfigAnnotation, reconciler.GetMyENI())

	// Delete node label
	updateNodeLabel(t, cl, reconciler, myNodeName, myENIConfigLabel, true)
	assert.Equal(t, myENIConfigAnnotation, reconciler.GetMyENI())

	// Delete node annotation, then the value should fallback to default
	updateNodeAnnotation(t, cl, reconciler, myNodeName, myENIConfigAnnotation, true)
	assert.Equal(t, nodecontroller.EniConfigDefault, reconciler.GetMyENI())
}

func TestGetEniConfigAnnotationDefDefault(t *testing.T) {
	_ = os.Unsetenv(nodecontroller.EnvEniConfigAnnotationDef)
	eniConfigAnnotationDef := nodecontroller.GetEniConfigAnnotationDef()
	assert.Equal(t, eniConfigAnnotationDef, nodecontroller.DefaultEniConfigAnnotationDef)
}

func TestGetEniConfigAnnotationlDefCustom(t *testing.T) {
	_ = os.Setenv(nodecontroller.EnvEniConfigAnnotationDef, "k8s.amazonaws.com/eniConfigCustom")
	eniConfigAnnotationDef := nodecontroller.GetEniConfigAnnotationDef()
	assert.Equal(t, eniConfigAnnotationDef, "k8s.amazonaws.com/eniConfigCustom")
}

func TestGetEniConfigLabelDefDefault(t *testing.T) {
	_ = os.Unsetenv(nodecontroller.EnvEniConfigLabelDef)
	eniConfigLabelDef := nodecontroller.GetEniConfigLabelDef()
	assert.Equal(t, eniConfigLabelDef, nodecontroller.DefaultEniConfigLabelDef)
}

func TestGetEniConfigLabelDefCustom(t *testing.T) {
	_ = os.Setenv(nodecontroller.EnvEniConfigLabelDef, "k8s.amazonaws.com/eniConfigCustom")
	eniConfigLabelDef := nodecontroller.GetEniConfigLabelDef()
	assert.Equal(t, eniConfigLabelDef, "k8s.amazonaws.com/eniConfigCustom")
}
