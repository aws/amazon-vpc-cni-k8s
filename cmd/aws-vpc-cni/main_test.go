package main

import (
	"encoding/json"
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

// Validate setting environment AWS_VPC_ENI_MTU, takes effect for egress-cni plugin
func TestEgressCNIPlugin(t *testing.T) {
	_ = os.Setenv(envEnIPv4Egress, "true")
	_ = os.Setenv(envPodMTU, "5000")
	assert.True(t, validateMTU(envEniMTU))

	// Use a temporary file for the parsed output.
	tmpfile, err := os.CreateTemp("", "temp-aws-vpc-cni.conflist")
	assert.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	err = generateJSON(awsConflist, tmpfile.Name(), getPrimaryIPMock)
	assert.NoError(t, err)

	// Read the json file and verify the MTU value for the egress-cni plugin
	var jsonData map[string]interface{}
	jsonFile, err := os.ReadFile(tmpfile.Name())
	assert.NoError(t, err)

	err = json.Unmarshal(jsonFile, &jsonData)
	assert.NoError(t, err)

	plugins, _ := jsonData["plugins"].([]interface{})
	assert.Equal(t, "egress-cni", plugins[1].(map[string]interface{})["type"])
	assert.Equal(t, "5000", plugins[1].(map[string]interface{})["mtu"])
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
