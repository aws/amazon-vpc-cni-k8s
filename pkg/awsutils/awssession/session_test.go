package awssession

import (
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/service/ec2"
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
	region := "us-west-2"

	os.Setenv("AWS_EC2_ENDPOINT", customEndpoint)
	defer os.Unsetenv("AWS_EC2_ENDPOINT")

	sess := New()

	resolvedEndpoint, err := sess.Config.EndpointResolver.EndpointFor(ec2.EndpointsID, region)
	assert.NoError(t, err)
	assert.Equal(t, customEndpoint, resolvedEndpoint.URL)
	assert.Equal(t, region, resolvedEndpoint.SigningRegion)
}
