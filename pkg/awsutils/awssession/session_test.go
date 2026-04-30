package awssession

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/transport/http"
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

func TestNew_SetsHTTPClientTimeout(t *testing.T) {
	t.Setenv("AWS_REGION", "us-west-2")
	t.Setenv(httpTimeoutEnv, "15")
	cfg, err := New(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, cfg.HTTPClient)
	// Verify the HTTP client is the buildable client type with timeout configured
	httpClient, ok := cfg.HTTPClient.(*http.BuildableClient)
	assert.True(t, ok, "HTTPClient should be BuildableClient")
	assert.Equal(t, 15*time.Second, httpClient.GetTimeout(), "HTTPClient timeout should match HTTP_TIMEOUT env")
}

func TestNewAWSSDKHTTPClient_SetsTimeout(t *testing.T) {
	client := NewAWSSDKHTTPClient()
	assert.NotNil(t, client)
	assert.Equal(t, DefaultAWSSDKClientTimeout, client.Timeout)
}

func TestNewAWSSDKHTTPClient_RespectsEnv(t *testing.T) {
	t.Setenv(httpTimeoutEnv, "20")
	client := NewAWSSDKHTTPClient()
	assert.Equal(t, 20*time.Second, client.Timeout)
}
