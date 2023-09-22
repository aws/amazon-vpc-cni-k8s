// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.
package eniconfig

import (
	"context"
	"os"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	testclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	eniconfigscheme "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
)

func TestMyENIConfig(t *testing.T) {
	testNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}

	testENIConfigAZ1 := &v1alpha1.ENIConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "az1",
		},
		Spec: v1alpha1.ENIConfigSpec{
			SecurityGroups: []string{"SG1"},
			Subnet:         "SB1",
		},
	}

	testENIConfigAZ2 := &v1alpha1.ENIConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "az2",
		},
		Spec: v1alpha1.ENIConfigSpec{
			SecurityGroups: []string{"SG2"},
			Subnet:         "SB2",
		},
	}

	testENIConfigCustom := &v1alpha1.ENIConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "custom",
		},
		Spec: v1alpha1.ENIConfigSpec{
			SecurityGroups: []string{"SG1"},
			Subnet:         "SB1",
		},
	}

	type env struct {
		nodes                  []*corev1.Node
		eniconfigs             []*v1alpha1.ENIConfig
		Labels                 map[string]string
		Annotations            map[string]string
		eniConfigLabelKey      string
		eniConfigAnnotationKey string
	}

	tests := []struct {
		name    string
		env     env
		want    *v1alpha1.ENIConfigSpec
		wantErr error
	}{
		{
			name: "Matching ENIConfig available - Using Default Labels",
			env: env{
				nodes:      []*corev1.Node{testNode},
				eniconfigs: []*v1alpha1.ENIConfig{testENIConfigAZ1},
				Labels: map[string]string{
					"k8s.amazonaws.com/eniConfig": "az1",
				},
			},
			want:    &testENIConfigAZ1.Spec,
			wantErr: nil,
		},
		{
			name: "No Matching ENIConfig available - Using Default Labels",
			env: env{
				nodes:      []*corev1.Node{testNode},
				eniconfigs: []*v1alpha1.ENIConfig{testENIConfigAZ2},
				Labels: map[string]string{
					"k8s.amazonaws.com/eniConfig": "az1",
				},
			},
			want:    nil,
			wantErr: errors.New("eniconfig: eniconfig is not available"),
		},
		{
			name: "Matching ENIConfig available - Using Custom Label Key exposed via ENI_CONFIG_LABEL_DEF",
			env: env{
				nodes:      []*corev1.Node{testNode},
				eniconfigs: []*v1alpha1.ENIConfig{testENIConfigAZ1},
				Labels: map[string]string{
					"topology.kubernetes.io/zone": "az1",
				},
				eniConfigLabelKey: "topology.kubernetes.io/zone",
			},
			want:    &testENIConfigAZ1.Spec,
			wantErr: nil,
		},
		{
			name: "No Matching ENIConfig available - Using Custom Label Key exposed via ENI_CONFIG_LABEL_DEF",
			env: env{
				nodes:      []*corev1.Node{testNode},
				eniconfigs: []*v1alpha1.ENIConfig{testENIConfigAZ1},
				Labels: map[string]string{
					"topology.kubernetes.io/zone": "az2",
				},
				eniConfigLabelKey: "topology.kubernetes.io/zone",
			},
			want:    nil,
			wantErr: errors.New("eniconfig: eniconfig is not available"),
		},
		{
			name: "Matching ENIConfig available - Using Default Annotation",
			env: env{
				nodes:      []*corev1.Node{testNode},
				eniconfigs: []*v1alpha1.ENIConfig{testENIConfigAZ1},
				Labels: map[string]string{
					"topology.kubernetes.io/zone": "az2",
				},
				Annotations: map[string]string{
					"k8s.amazonaws.com/eniConfig": "az1",
				},
			},
			want:    &testENIConfigAZ1.Spec,
			wantErr: nil,
		},
		{
			name: "No Matching ENIConfig available - Using Default Annotation",
			env: env{
				nodes:      []*corev1.Node{testNode},
				eniconfigs: []*v1alpha1.ENIConfig{testENIConfigAZ2},
				Labels: map[string]string{
					"topology.kubernetes.io/zone": "az2",
				},
				Annotations: map[string]string{
					"k8s.amazonaws.com/eniConfig": "az1",
				},
			},
			want:    nil,
			wantErr: errors.New("eniconfig: eniconfig is not available"),
		},
		{
			name: "Matching ENIConfig available - Using Custom Annotation Key exposed via ENI_CONFIG_ANNOTATION_DEF",
			env: env{
				nodes:      []*corev1.Node{testNode},
				eniconfigs: []*v1alpha1.ENIConfig{testENIConfigAZ1},
				Labels: map[string]string{
					"topology.kubernetes.io/zone": "az2",
				},
				Annotations: map[string]string{
					"k8s.amazonaws.com/myENIConfig": "az1",
				},
				eniConfigAnnotationKey: "k8s.amazonaws.com/myENIConfig",
			},
			want:    &testENIConfigAZ1.Spec,
			wantErr: nil,
		},
		{
			name: "Matching ENIConfig available - Using external label",
			env: env{
				nodes:      []*corev1.Node{testNode},
				eniconfigs: []*v1alpha1.ENIConfig{testENIConfigAZ1, testENIConfigCustom},
				Labels: map[string]string{
					"vpc.amazonaws.com/externalEniConfig": "custom",
					"topology.kubernetes.io/zone":         "az2",
				},
				eniConfigLabelKey: "topology.kubernetes.io/zone",
			},
			want:    &testENIConfigCustom.Spec,
			wantErr: nil,
		},
		{
			name: "No Matching ENIConfig available - Using Custom Label Key exposed via ENI_CONFIG_ANNOTATION_DEF",
			env: env{
				nodes:      []*corev1.Node{testNode},
				eniconfigs: []*v1alpha1.ENIConfig{testENIConfigAZ2},
				Labels: map[string]string{
					"topology.kubernetes.io/zone": "az2",
				},
				Annotations: map[string]string{
					"k8s.amazonaws.com/myENIConfig": "az1",
				},
				eniConfigAnnotationKey: "k8s.amazonaws.com/myENIConfig",
			},
			want:    nil,
			wantErr: errors.New("eniconfig: eniconfig is not available"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			k8sSchema := runtime.NewScheme()
			clientgoscheme.AddToScheme(k8sSchema)
			eniconfigscheme.AddToScheme(k8sSchema)
			k8sClient := testclient.NewClientBuilder().WithScheme(k8sSchema).WithRuntimeObjects().Build()

			for _, node := range tt.env.nodes {
				_ = os.Setenv("MY_NODE_NAME", node.Name)
				node.Labels = tt.env.Labels
				node.Annotations = tt.env.Annotations
				if tt.env.eniConfigAnnotationKey != "" {
					_ = os.Setenv(envEniConfigAnnotationDef, tt.env.eniConfigAnnotationKey)
				}
				if tt.env.eniConfigLabelKey != "" {
					_ = os.Setenv(envEniConfigLabelDef, tt.env.eniConfigLabelKey)
				}
				err := k8sClient.Create(ctx, node.DeepCopy())
				assert.NoError(t, err)
			}

			for _, eniconfig := range tt.env.eniconfigs {
				err := k8sClient.Create(ctx, eniconfig.DeepCopy())
				assert.NoError(t, err)
			}

			myENIConfig, err := MyENIConfig(ctx, k8sClient)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, myENIConfig, tt.want)
			}
		})
	}
}

func TestGetEniConfigAnnotationDefDefault(t *testing.T) {
	_ = os.Unsetenv(envEniConfigAnnotationDef)
	eniConfigAnnotationDef := getEniConfigAnnotationDef()
	assert.Equal(t, eniConfigAnnotationDef, defaultEniConfigAnnotationDef)
}

func TestGetEniConfigAnnotationlDefCustom(t *testing.T) {
	_ = os.Setenv(envEniConfigAnnotationDef, "k8s.amazonaws.com/eniConfigCustom")
	eniConfigAnnotationDef := getEniConfigAnnotationDef()
	assert.Equal(t, eniConfigAnnotationDef, "k8s.amazonaws.com/eniConfigCustom")
}

func TestGetEniConfigLabelDefDefault(t *testing.T) {
	_ = os.Unsetenv(envEniConfigLabelDef)
	eniConfigLabelDef := getEniConfigLabelDef()
	assert.Equal(t, eniConfigLabelDef, defaultEniConfigLabelDef)
}

func TestGetEniConfigLabelDefCustom(t *testing.T) {
	_ = os.Setenv(envEniConfigLabelDef, "k8s.amazonaws.com/eniConfigCustom")
	eniConfigLabelDef := getEniConfigLabelDef()
	assert.Equal(t, eniConfigLabelDef, "k8s.amazonaws.com/eniConfigCustom")
}
