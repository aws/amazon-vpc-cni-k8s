package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	awsConflist = "../../misc/10-aws.conflist"
	devNull     = "/dev/null"
	nodeIP      = "10.0.0.0"
)

var getPrimaryIPMock = func(ipv4 bool) (string, error) {
	if ipv4 {
		return "10.0.0.0", nil
	}
	return "2600::", nil
}

// Validate that generateJSON runs against default conflist without error
func TestGenerateJSON(t *testing.T) {
	err := generateJSON(awsConflist, devNull, getPrimaryIPMock)
	assert.NoError(t, err)
}

// Validate that generateJSON runs without error when bandwidth plugin is added to the default conflist
func TestGenerateJSONPlusBandwidth(t *testing.T) {
	_ = os.Setenv(envEnBandwidthPlugin, "true")
	err := generateJSON(awsConflist, devNull, getPrimaryIPMock)
	assert.NoError(t, err)
}

// Validate that generateJSON runs without error when tuning plugin is added to the default conflist
func TestGenerateJSONPlusTuning(t *testing.T) {
	_ = os.Setenv(envDisablePodV6, "true")
	err := generateJSON(awsConflist, devNull, getPrimaryIPMock)
	assert.NoError(t, err)
}

// Validate that generateJSON runs without error when the bandwidth and tuning plugins are added to the default conflist
func TestGenerateJSONPlusBandwidthAndTuning(t *testing.T) {
	_ = os.Setenv(envEnBandwidthPlugin, "true")
	_ = os.Setenv(envDisablePodV6, "true")
	err := generateJSON(awsConflist, devNull, getPrimaryIPMock)
	assert.NoError(t, err)
}

func TestMTUValidation(t *testing.T) {
	// By default, ENI MTU and pod MTU should be valid
	assert.True(t, validateMTU(envEniMTU))
	assert.True(t, validateMTU(envPodMTU))

	// Non-integer values should fail
	_ = os.Setenv(envEniMTU, "true")
	_ = os.Setenv(envPodMTU, "abc")
	assert.False(t, validateMTU(envEniMTU))
	assert.False(t, validateMTU(envPodMTU))

	// Integer values within IPv4 range should succeed
	_ = os.Setenv(envEniMTU, "5000")
	_ = os.Setenv(envPodMTU, "3000")
	assert.True(t, validateMTU(envEniMTU))
	assert.True(t, validateMTU(envPodMTU))

	// Integer values outside IPv4 range should fail
	_ = os.Setenv(envEniMTU, "10000")
	_ = os.Setenv(envPodMTU, "500")
	assert.False(t, validateMTU(envEniMTU))
	assert.False(t, validateMTU(envPodMTU))

	// Integer values within IPv6 range should succeed
	_ = os.Setenv(envEnIPv6, "true")
	_ = os.Setenv(envEniMTU, "5000")
	_ = os.Setenv(envPodMTU, "3000")
	assert.True(t, validateMTU(envEniMTU))
	assert.True(t, validateMTU(envPodMTU))

	// Integer values outside IPv6 range should fail
	_ = os.Setenv(envEniMTU, "10000")
	_ = os.Setenv(envPodMTU, "1200")
	assert.False(t, validateMTU(envEniMTU))
	assert.False(t, validateMTU(envPodMTU))
}
