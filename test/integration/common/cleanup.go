package common

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	. "github.com/onsi/ginkgo/v2"
)

// Subnet deletes get the longest bound because they must wait out the CNI's async
// ENI drain, and a leaked subnet pins the VPC stack. A leftover security group does
// not pin the VPC (eksctl delete force-drops it), so it gets a shorter bound.
const (
	subnetPollTimeout       = 5 * time.Minute
	sgPollTimeout           = 2 * time.Minute
	cidrDisassociateTimeout = 90 * time.Second
	pollInterval            = 15 * time.Second
)

// IsNotFound reports whether err is an AWS API error with a ".NotFound" code,
// so polled deletes can treat an already-deleted resource as success.
func IsNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return strings.HasSuffix(apiErr.ErrorCode(), ".NotFound")
	}
	return false
}

// pollUntilDeleted retries deleteFn until it succeeds, the resource is already
// gone, or timeout. All errors are retried: the common blocker (DependencyViolation
// during ENI drain) is transient, and distinguishing retriable codes isn't worth the
// taxonomy risk when the bound caps the cost. Returns an error instead of asserting
// so callers can run every cleanup step and aggregate failures.
func pollUntilDeleted(timeout time.Duration, description string, deleteFn func() error) error {
	var lastErr error
	waitErr := wait.PollUntilContextTimeout(context.Background(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		if err := deleteFn(); err != nil && !IsNotFound(err) {
			lastErr = err
			return false, nil
		}
		return true, nil
	})
	if waitErr != nil {
		if lastErr != nil {
			return fmt.Errorf("%s within %s: %w", description, timeout, lastErr)
		}
		return fmt.Errorf("%s within %s: %w", description, timeout, waitErr)
	}
	return nil
}

// EnsureSecurityGroupDeleted retries DeleteSecurityGroup until it succeeds or the
// group is already gone.
func EnsureSecurityGroupDeleted(f *framework.Framework, groupID string) error {
	if groupID == "" {
		return nil
	}
	return pollUntilDeleted(sgPollTimeout,
		fmt.Sprintf("security group %s could not be deleted", groupID),
		func() error { return f.CloudServices.EC2().DeleteSecurityGroup(context.TODO(), groupID) })
}

// EnsureSubnetDeleted retries DeleteSubnet until it succeeds or the subnet is
// already gone. Only pass subnets the test created: it force-releases CNI ENIs in the subnet.
func EnsureSubnetDeleted(f *framework.Framework, subnetID string) error {
	if subnetID == "" {
		return nil
	}
	return pollUntilDeleted(subnetPollTimeout,
		fmt.Sprintf("subnet %s could not be deleted (it will leak and pin the VPC stack)", subnetID),
		func() error {
			err := f.CloudServices.EC2().DeleteSubnet(context.TODO(), subnetID)
			if err != nil && !IsNotFound(err) {
				releaseCNIENIsInSubnet(f, subnetID)
			}
			return err
		})
}

func releaseCNIENIsInSubnet(f *framework.Framework, subnetID string) {
	enis, err := f.CloudServices.EC2().DescribeCNINetworkInterfacesInSubnet(context.TODO(), subnetID)
	if err != nil {
		return
	}
	for _, eni := range enis {
		eniID := aws.ToString(eni.NetworkInterfaceId)
		if eni.Status == ec2types.NetworkInterfaceStatusAvailable {
			_, _ = fmt.Fprintf(GinkgoWriter, "deleting CNI ENI %s in subnet %s\n", eniID, subnetID)
			_ = f.CloudServices.EC2().DeleteNetworkInterface(context.TODO(), eniID)
		} else if eni.Attachment != nil && eni.Attachment.AttachmentId != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "force-detaching CNI ENI %s in subnet %s\n", eniID, subnetID)
			_ = f.CloudServices.EC2().DetachNetworkInterface(context.TODO(), *eni.Attachment.AttachmentId, true)
		}
	}
}

// EnsureVPCCIDRDisassociated retries DisAssociateVPCCIDRBlock until it succeeds or
// is already disassociated. Every subnet in the CIDR must be deleted first.
func EnsureVPCCIDRDisassociated(f *framework.Framework, associationID, cidr string) error {
	if associationID == "" {
		return nil
	}
	return pollUntilDeleted(cidrDisassociateTimeout,
		fmt.Sprintf("CIDR %s could not be disassociated from the VPC", cidr),
		func() error { return f.CloudServices.EC2().DisAssociateVPCCIDRBlock(context.TODO(), associationID) })
}

// EnsureRouteTableDisassociated disassociates a route table association, treating
// an already-gone association as success.
func EnsureRouteTableDisassociated(f *framework.Framework, associationID string) error {
	if associationID == "" {
		return nil
	}
	if err := f.CloudServices.EC2().DisassociateRouteTable(context.TODO(), associationID); err != nil && !IsNotFound(err) {
		return err
	}
	return nil
}
