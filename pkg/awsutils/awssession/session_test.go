package awssession

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHttpTimeoutReturnDefault(t *testing.T) {
	os.Setenv(httpTimeoutEnv, "2")
	defer os.Unsetenv(httpTimeoutEnv)
	expectedHTTPTimeOut := time.Duration(10) * time.Second
	assert.Equal(t, expectedHTTPTimeOut, getHTTPTimeout())
}

func TestHttpTimeoutWithValueAbove10(t *testing.T) {
	os.Setenv(httpTimeoutEnv, "12")
	defer os.Unsetenv(httpTimeoutEnv)
	expectedHTTPTimeOut := time.Duration(12) * time.Second
	assert.Equal(t, expectedHTTPTimeOut, getHTTPTimeout())
}
