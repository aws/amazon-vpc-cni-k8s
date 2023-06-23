package k8sapi

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	eniconfigscheme "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
)

func TestGetNode(t *testing.T) {
	var err error
	ctx := context.Background()
	k8sSchema := runtime.NewScheme()
	err = clientgoscheme.AddToScheme(k8sSchema)
	assert.NoError(t, err)

	err = eniconfigscheme.AddToScheme(k8sSchema)
	assert.NoError(t, err)

	fakeNode := &corev1.Node{
		ObjectMeta: v1.ObjectMeta{
			Name: "testNode",
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(k8sSchema).WithObjects(fakeNode).Build()
	err = os.Setenv("MY_NODE_NAME", "testNode")
	assert.NoError(t, err)

	node, err := GetNode(ctx, k8sClient)
	assert.NoError(t, err)
	assert.Equal(t, node.Name, "testNode")

	err = os.Setenv("MY_NODE_NAME", "dummyNode")
	assert.NoError(t, err)

	_, err = GetNode(ctx, k8sClient)
	assert.Error(t, err)
}
