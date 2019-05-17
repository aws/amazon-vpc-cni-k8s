package ec2metadatawrapper

import (
	mockec2metadatawrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/ec2metadatawrapper/mocks"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws/ec2metadata"

	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

const (
	// iidRegion is the instance identity document region
	iidRegion = "us-east-1"
)

var testInstanceIdentityDoc = ec2metadata.EC2InstanceIdentityDocument{
	PrivateIP:        "172.1.1.1",
	AvailabilityZone: "us-east-1a",
	Version:          "2010-08-31",
	Region:           "us-east-1",
	AccountID:        "012345678901",
	InstanceID:       "i-01234567",
	BillingProducts:  []string{"bp-01234567"},
	ImageID:          "ami-12345678",
	InstanceType:     "t2.micro",
	PendingTime:      time.Now(),
	Architecture:     "x86_64",
}

func TestGetInstanceIdentityDocHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockGetter := mockec2metadatawrapper.NewMockHttpClient(ctrl)
	testClient := New(mockGetter)

	mockGetter.EXPECT().GetInstanceIdentityDocument().Return(testInstanceIdentityDoc, nil)

	doc, err := testClient.GetInstanceIdentityDocument()
	assert.NoError(t, err)
	assert.Equal(t, iidRegion, doc.Region)
}

func TestGetInstanceIdentityDocError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockGetter := mockec2metadatawrapper.NewMockHttpClient(ctrl)
	testClient := New(mockGetter)

	mockGetter.EXPECT().GetInstanceIdentityDocument().Return(ec2metadata.EC2InstanceIdentityDocument{}, errors.New("test error"))

	doc, err := testClient.GetInstanceIdentityDocument()
	assert.Error(t, err)
	assert.Empty(t, doc.Region)
}

func TestGetRegionHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockGetter := mockec2metadatawrapper.NewMockHttpClient(ctrl)
	testClient := New(mockGetter)

	mockGetter.EXPECT().Region().Return(iidRegion, nil)

	region, err := testClient.Region()
	assert.NoError(t, err)
	assert.Equal(t, iidRegion, region)
}

func TestGetRegionErr(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockGetter := mockec2metadatawrapper.NewMockHttpClient(ctrl)
	testClient := New(mockGetter)

	mockGetter.EXPECT().Region().Return("", errors.New("test error"))

	region, err := testClient.Region()
	assert.Error(t, err)
	assert.Empty(t, region)
}
