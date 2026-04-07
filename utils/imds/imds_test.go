package imds

import (
	"context"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
)

func TestGetMetaData_ConfigSetsHTTPClientTimeout(t *testing.T) {
	t.Setenv("AWS_REGION", "us-west-2")
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithHTTPClient(&http.Client{
			Timeout: defaultAWSSDKClientTimeout,
		}),
		config.WithRetryMaxAttempts(10),
	)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.HTTPClient == nil {
		t.Fatal("HTTPClient should not be nil")
	}
	httpClient, ok := cfg.HTTPClient.(*http.Client)
	if !ok {
		t.Fatal("HTTPClient should be *http.Client")
	}
	if httpClient.Timeout != defaultAWSSDKClientTimeout {
		t.Fatalf("expected timeout %v, got %v", defaultAWSSDKClientTimeout, httpClient.Timeout)
	}
}
