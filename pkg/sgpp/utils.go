package sgpp

import (
	"os"
)

const vlanInterfacePrefix = "vlan"

// BuildHostVethNamePrefix computes the name prefix for host-side veth pairs for SGPP pods
// for the "standard" mode, we use the same hostVethNamePrefix as normal pods, which is "eni" by default, but can be overwritten as well.
// for the "strict" mode, we use dedicated "vlan" hostVethNamePrefix, which is to opt-out SNAT support and opt-out calico's workload management.
func BuildHostVethNamePrefix(hostVethNamePrefix string, podSGEnforcingMode EnforcingMode) string {
	switch podSGEnforcingMode {
	case EnforcingModeStrict:
		return vlanInterfacePrefix
	case EnforcingModeStandard:
		return hostVethNamePrefix
	default:
		return vlanInterfacePrefix
	}
}

// LoadEnforcingModeFromEnv tries to load the enforcing mode from environment variable and fall-back to DefaultEnforcingMode.
func LoadEnforcingModeFromEnv() EnforcingMode {
	envVal, _ := os.LookupEnv(envEnforcingMode)
	switch envVal {
	case string(EnforcingModeStrict):
		return EnforcingModeStrict
	case string(EnforcingModeStandard):
		return EnforcingModeStandard
	default:
		return DefaultEnforcingMode
	}
}
