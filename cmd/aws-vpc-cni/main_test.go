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

// Validate that generateJSON runs against default conflist without error
func TestGenerateJSON(t *testing.T) {
	err := generateJSON(awsConflist, devNull, nodeIP)
	assert.NoError(t, err)
}

// Validate that generateJSON runs without error when bandwidth plugin is added to default conflist
func TestGenerateJSONPlusBandwidth(t *testing.T) {
	_ = os.Setenv(envEnBandwidthPlugin, "true")
	err := generateJSON(awsConflist, devNull, nodeIP)
	assert.NoError(t, err)
}
