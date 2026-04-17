package imds

import (
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils/awssession"
)

func TestGetMetaData_ConfigSetsHTTPClientTimeout(t *testing.T) {
	client := awssession.NewAWSSDKHTTPClient()
	if client == nil {
		t.Fatal("NewAWSSDKHTTPClient should not return nil")
	}
	if client.Timeout != awssession.DefaultAWSSDKClientTimeout {
		t.Fatalf("expected timeout %v, got %v", awssession.DefaultAWSSDKClientTimeout, client.Timeout)
	}
}
