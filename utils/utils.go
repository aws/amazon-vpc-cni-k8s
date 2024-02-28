package utils

import (
	"os"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

// Parse environment variable and return boolean representation of string, or default value if environment variable is unset
func GetBoolAsStringEnvVar(env string, defaultVal bool) bool {
	if val, ok := os.LookupEnv(env); ok {
		parsedVal, err := strconv.ParseBool(val)
		if err == nil {
			return parsedVal
		}
		log.Errorf("Failed to parse variable %s with value %s as boolean", env, val)
		return defaultVal
	}
	// Environment variable is not set, so return default value
	return defaultVal
}

// Parse environment variable and return integer representation of string, or default value if environment variable is unset
func GetIntFromStringEnvVar(env string, defaultVal int) (int, error, string) {
	if val, ok := os.LookupEnv(env); ok {
		parsedVal, err := strconv.Atoi(val)
		if err == nil {
			return parsedVal, nil, val
		}
		log.Errorf("Failed to parse variable %s with value %s as integer", env, val)
		return -1, err, val
	}
	// Environment variable is not set, so return default value
	return defaultVal, nil, ""
}

// If environment variable is set, return set value, otherwise return default value
func GetEnv(env, defaultVal string) string {
	if val, ok := os.LookupEnv(env); ok {
		return val
	}
	return defaultVal
}

// NetworkPolicyEnforcingMode is the mode of network policy enforcement
type NetworkPolicyEnforcingMode string

const (
	// None : no network policy enforcement
	None NetworkPolicyEnforcingMode = "none"
	// Strict : strict network policy enforcement
	Strict NetworkPolicyEnforcingMode = "strict"
	// Standard :standard network policy enforcement
	Standard NetworkPolicyEnforcingMode = "standard"
)

// IsValidNetworkPolicyEnforcingMode checks if the input string matches any of the enum values
func IsValidNetworkPolicyEnforcingMode(input string) bool {
	switch strings.ToLower(input) {
	case string(None), string(Strict), string(Standard):
		return true
	default:
		return false
	}
}

// IsStrictMode checks if strict mode is enabled
func IsStrictMode(input string) bool {
	return strings.ToLower(input) == string(Strict)
}
