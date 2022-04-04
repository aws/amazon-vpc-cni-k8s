package sgpp

type EnforcingMode string

const (
	EnforcingModeStrict   EnforcingMode = "strict"
	EnforcingModeStandard EnforcingMode = "standard"
)

const (
	// DefaultEnforcingMode is the default enforcing mode if not specified explicitly.
	DefaultEnforcingMode EnforcingMode = EnforcingModeStrict
	// environment variable knob to decide EnforcingMode for SGPP feature.
	envEnforcingMode = "POD_SECURITY_GROUP_ENFORCING_MODE"
)
