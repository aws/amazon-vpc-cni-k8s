package common

import (
	"fmt"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/sgpp"
)

const (
	podSGEnforcingModeEnv = "POD_SECURITY_GROUP_ENFORCING_MODE"
	vethPrefixEnv         = "AWS_VPC_K8S_CNI_VETHPREFIX"
	defaultVethPrefix     = "eni"
)

// SGPPTestConfig captures the mode-dependent host networking expectations.
type SGPPTestConfig struct {
	EnforcingMode  sgpp.EnforcingMode
	HostVethPrefix string
}

// ResolveSGPPTestConfig derives SGPP test expectations from the live aws-node environment.
func ResolveSGPPTestConfig(awsNodeEnv map[string]string) (SGPPTestConfig, error) {
	enforcingMode := sgpp.DefaultEnforcingMode
	if configuredMode := awsNodeEnv[podSGEnforcingModeEnv]; configuredMode != "" {
		enforcingMode = sgpp.EnforcingMode(configuredMode)
	}
	if enforcingMode != sgpp.EnforcingModeStrict && enforcingMode != sgpp.EnforcingModeStandard {
		return SGPPTestConfig{}, fmt.Errorf("unsupported %s value %q",
			podSGEnforcingModeEnv, enforcingMode)
	}

	configuredVethPrefix := awsNodeEnv[vethPrefixEnv]
	if configuredVethPrefix == "" {
		configuredVethPrefix = defaultVethPrefix
	}

	return SGPPTestConfig{
		EnforcingMode:  enforcingMode,
		HostVethPrefix: sgpp.BuildHostVethNamePrefix(configuredVethPrefix, enforcingMode),
	}, nil
}
