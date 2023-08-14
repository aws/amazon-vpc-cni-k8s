package sgpp

import (
	"os"
	"testing"

	eniconfigscheme "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha2"
	rcscheme "github.com/aws/amazon-vpc-resource-controller-k8s/apis/vpcresources/v1alpha1"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	testclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type testMocks struct {
	ctrl      *gomock.Controller
	k8sClient client.Client
}

func setup(t *testing.T) *testMocks {
	ctrl := gomock.NewController(t)
	k8sSchema := runtime.NewScheme()
	clientgoscheme.AddToScheme(k8sSchema)
	eniconfigscheme.AddToScheme(k8sSchema)
	rcscheme.AddToScheme(k8sSchema)

	return &testMocks{
		ctrl:      ctrl,
		k8sClient: testclient.NewClientBuilder().WithScheme(k8sSchema).WithRuntimeObjects().Build(),
	}
}

func TestBuildHostVethNamePrefix(t *testing.T) {
	type args struct {
		hostVethNamePrefix string
		podSGEnforcingMode EnforcingMode
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "standard mode should use configured vethNamePrefix",
			args: args{
				hostVethNamePrefix: "eni",
				podSGEnforcingMode: EnforcingModeStandard,
			},
			want: "eni",
		},
		{
			name: "strict mode should use vlan vethNamePrefix",
			args: args{
				hostVethNamePrefix: "eni",
				podSGEnforcingMode: EnforcingModeStrict,
			},
			want: "vlan",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildHostVethNamePrefix(tt.args.hostVethNamePrefix, tt.args.podSGEnforcingMode)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadEnforcingModeFromEnv(t *testing.T) {
	type fields struct {
		envVars map[string]string
	}
	tests := []struct {
		name   string
		fields fields
		want   EnforcingMode
	}{
		{
			name: "use strict mode when POD_SECURITY_GROUP_ENFORCING_MODE set to strict",
			fields: fields{
				envVars: map[string]string{
					"POD_SECURITY_GROUP_ENFORCING_MODE": "strict",
				},
			},
			want: EnforcingModeStrict,
		},
		{
			name: "use standard mode when POD_SECURITY_GROUP_ENFORCING_MODE set to standard",
			fields: fields{
				envVars: map[string]string{
					"POD_SECURITY_GROUP_ENFORCING_MODE": "standard",
				},
			},
			want: EnforcingModeStandard,
		},
		{
			name:   "default to strict mode when POD_SECURITY_GROUP_ENFORCING_MODE not set",
			fields: fields{},
			want:   EnforcingModeStrict,
		},
		{
			name: "default to strict mode when POD_SECURITY_GROUP_ENFORCING_MODE incorrectly configured",
			fields: fields{
				envVars: map[string]string{
					"POD_SECURITY_GROUP_ENFORCING_MODE": "unknown",
				},
			},
			want: EnforcingModeStrict,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalEnvVars := make(map[string]string)
			for k := range tt.fields.envVars {
				originalV, _ := os.LookupEnv(k)
				originalEnvVars[k] = originalV
			}
			defer func() {
				for k, v := range originalEnvVars {
					if len(v) != 0 {
						os.Setenv(k, v)
					} else {
						os.Unsetenv(k)
					}
				}
			}()

			for k, v := range tt.fields.envVars {
				os.Setenv(k, v)
			}

			got := LoadEnforcingModeFromEnv()
			assert.Equal(t, tt.want, got)
		})
	}
}
