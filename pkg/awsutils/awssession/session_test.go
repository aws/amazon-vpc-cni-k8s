package awssession

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
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

func TestAwsEc2EndpointResolver(t *testing.T) {
	customEndpoint := "https://ec2.us-west-2.customaws.com"
	ctx := context.Background()

	os.Setenv("AWS_EC2_ENDPOINT", customEndpoint)
	defer os.Unsetenv("AWS_EC2_ENDPOINT")

	cfg, err := New(ctx)
	assert.NoError(t, err)

	resolvedEndpoint, err := cfg.EndpointResolver.ResolveEndpoint(ec2.ServiceID, "")
	assert.NoError(t, err)
	assert.Equal(t, customEndpoint, resolvedEndpoint.URL)
}
