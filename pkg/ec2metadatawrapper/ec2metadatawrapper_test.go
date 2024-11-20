package ec2metadatawrapper

import (
	"context"
	"testing"

	mockec2metadatawrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/ec2metadatawrapper/mocks"

	ec2metadata "github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

const (
	// iidRegion is the instance identity document region
	iidRegion = "us-east-1"
)

var testInstanceIdentityDoc = ec2metadata.InstanceIdentityDocument{
	Version:    "2010-08-31",
	Region:     "us-east-1",
	InstanceID: "i-01234567",
	ImageID:    "ami-12345678",
}

func TestGetInstanceIdentityDocHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockGetter := mockec2metadatawrapper.NewMockHTTPClient(ctrl)
	testClient := NewMetadataService(mockGetter)

	mockGetter.EXPECT().GetInstanceIdentityDocument(gomock.Any(), gomock.Any()).Return(&ec2metadata.GetInstanceIdentityDocumentOutput{
		InstanceIdentityDocument: testInstanceIdentityDoc,
	}, nil)

	ctx := context.Background()

	doc, err := testClient.GetInstanceIdentityDocument(ctx, &ec2metadata.GetInstanceIdentityDocumentInput{})
	assert.NoError(t, err)
	assert.Equal(t, iidRegion, doc.Region)
}

func TestGetInstanceIdentityDocError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockGetter := mockec2metadatawrapper.NewMockHTTPClient(ctrl)
	testClient := NewMetadataService(mockGetter)

	mockGetter.EXPECT().GetInstanceIdentityDocument(gomock.Any(), gomock.Any()).Return(&ec2metadata.GetInstanceIdentityDocumentOutput{}, errors.New("test error"))
	ctx := context.Background()
	doc, err := testClient.GetInstanceIdentityDocument(ctx, &ec2metadata.GetInstanceIdentityDocumentInput{})
	assert.Error(t, err)
	assert.Empty(t, doc.Region)
}

func TestGetRegionHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockGetter := mockec2metadatawrapper.NewMockEC2MetadataClient(ctrl)
	testClient := NewMetadataService(mockGetter)

	expectedRegion := "us-west-2"
	mockGetter.EXPECT().GetRegion(gomock.Any(), gomock.Any()).Return(&ec2metadata.GetRegionOutput{Region: expectedRegion}, nil)

	region, err := testClient.GetRegion(context.Background(), &ec2metadata.GetRegionInput{})
	assert.NoError(t, err)
	assert.Equal(t, expectedRegion, region.Region)
}

func TestGetRegionErr(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockGetter := mockec2metadatawrapper.NewMockEC2MetadataClient(ctrl)
	testClient := NewMetadataService(mockGetter)

	mockGetter.EXPECT().GetRegion(gomock.Any(), gomock.Any()).Return(nil, errors.New("test error"))

	region, err := testClient.GetRegion(context.Background(), &ec2metadata.GetRegionInput{})
	assert.Error(t, err)
	assert.Empty(t, region)
}
