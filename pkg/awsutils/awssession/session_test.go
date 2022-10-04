package awssession

import (
	"github.com/aws/aws-sdk-go/aws/endpoints"
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

func TestAwsEc2EndpointOverride(t *testing.T) {
	customUrl := "custom-url"
	region := "us-west-2"
	os.Setenv(awsEc2EndpointOverride, customUrl)
	defer os.Unsetenv(awsEc2EndpointOverride)
	session := New()
	resolvedEndpoint, err := session.Config.EndpointResolver.EndpointFor(endpoints.Ec2ServiceID, region)
	assert.NoError(t, err)
	assert.Equal(
		t,
		endpoints.ResolvedEndpoint{
			URL:           customUrl,
			SigningRegion: region,
		},
		resolvedEndpoint,
	)
}
