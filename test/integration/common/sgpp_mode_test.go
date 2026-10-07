package common

import (
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/sgpp"
)

func TestResolveSGPPTestConfig(t *testing.T) {
	tests := []struct {
		name       string
		env        map[string]string
		wantMode   sgpp.EnforcingMode
		wantPrefix string
	}{
		{
			name:       "defaults to strict mode and vlan prefix",
			env:        map[string]string{},
			wantMode:   sgpp.EnforcingModeStrict,
			wantPrefix: "vlan",
		},
		{
			name: "standard mode uses default veth prefix",
			env: map[string]string{
				podSGEnforcingModeEnv: string(sgpp.EnforcingModeStandard),
			},
			wantMode:   sgpp.EnforcingModeStandard,
			wantPrefix: defaultVethPrefix,
		},
		{
			name: "standard mode uses configured veth prefix",
			env: map[string]string{
				podSGEnforcingModeEnv: string(sgpp.EnforcingModeStandard),
				vethPrefixEnv:         "abcd",
			},
			wantMode:   sgpp.EnforcingModeStandard,
			wantPrefix: "abcd",
		},
		{
			name: "strict mode ignores configured veth prefix",
			env: map[string]string{
				podSGEnforcingModeEnv: string(sgpp.EnforcingModeStrict),
				vethPrefixEnv:         "abcd",
			},
			wantMode:   sgpp.EnforcingModeStrict,
			wantPrefix: "vlan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveSGPPTestConfig(tt.env)
			if err != nil {
				t.Fatalf("ResolveSGPPTestConfig() returned error: %v", err)
			}
			if got.EnforcingMode != tt.wantMode {
				t.Errorf("enforcing mode = %q, want %q", got.EnforcingMode, tt.wantMode)
			}
			if got.HostVethPrefix != tt.wantPrefix {
				t.Errorf("host veth prefix = %q, want %q", got.HostVethPrefix, tt.wantPrefix)
			}
		})
	}
}

func TestResolveSGPPTestConfigRejectsInvalidMode(t *testing.T) {
	_, err := ResolveSGPPTestConfig(map[string]string{
		podSGEnforcingModeEnv: "invalid",
	})
	if err == nil {
		t.Fatal("ResolveSGPPTestConfig() returned no error")
	}
}
