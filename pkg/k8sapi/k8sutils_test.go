package k8sapi

import (
	"context"
	"os"
	"testing"

	eniconfigscheme "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha2"
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
