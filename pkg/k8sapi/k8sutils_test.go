package k8sapi

import (
	"context"
	"os"
	"sync"
	"testing"

	eniconfigscheme "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetNode(t *testing.T) {
	ctx := context.Background()
	k8sSchema := runtime.NewScheme()
	corev1.AddToScheme(k8sSchema)
	eniconfigscheme.AddToScheme(k8sSchema)

	fakeNode := &corev1.Node{
		ObjectMeta: v1.ObjectMeta{
			Name: "testNode",
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(k8sSchema).WithObjects(fakeNode).Build()
	os.Setenv("MY_NODE_NAME", "testNode")
	node, err := GetNode(ctx, k8sClient)
	assert.NoError(t, err)
	assert.Equal(t, node.Name, "testNode")

	os.Setenv("MY_NODE_NAME", "dummyNode")
	_, err = GetNode(ctx, k8sClient)
	assert.Error(t, err)
}

func TestKubeClientStopContextIdempotent(t *testing.T) {
	// ctrl.SetupSignalHandler panics on a second call ("close of closed channel").
	// Recreating the kube client when the API server becomes available must not
	// invoke it again. Use a stand-in that panics on a second call to prove we
	// only set up the stop context once.
	//
	// sync.Once must not be copied (go vet: copies lock value). Reset package
	// state with a fresh Once instead of saving/restoring the previous value.
	calls := 0
	oldSetup := setupStopContext
	oldCtx := kubeClientStopCtx
	t.Cleanup(func() {
		setupStopContext = oldSetup
		kubeClientStopCtx = oldCtx
		kubeClientStopOnce = sync.Once{}
	})

	kubeClientStopOnce = sync.Once{}
	kubeClientStopCtx = nil
	setupStopContext = func() context.Context {
		calls++
		if calls > 1 {
			panic("close of closed channel")
		}
		return context.Background()
	}

	ctx1 := kubeClientStopContext()
	ctx2 := kubeClientStopContext()
	assert.Equal(t, ctx1, ctx2)
	assert.Equal(t, 1, calls)
}
