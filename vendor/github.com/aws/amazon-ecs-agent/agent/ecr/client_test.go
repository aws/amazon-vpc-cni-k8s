// Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

// ecr_test packge to avoid test dependency cycle on ecr/mocks
package ecr_test

import (
	"errors"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/ecr"
	"github.com/aws/amazon-ecs-agent/agent/ecr/mocks"
	ecrapi "github.com/aws/amazon-ecs-agent/agent/ecr/model/ecr"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

const testRegistryId = "testRegistryId"

// test suite struct for handling mocks and test client
type GetAuthorizationTokenTestSuite struct {
	suite.Suite
	ctrl       *gomock.Controller
	mockClient *mock_ecr.MockECRSDK
	ecrClient  ecr.ECRClient
}

// test suite setup & teardown
func TestGetAuthorizationTokenSuite(t *testing.T) {
	suite.Run(t, new(GetAuthorizationTokenTestSuite))
}

func (suite *GetAuthorizationTokenTestSuite) SetupTest() {
	suite.ctrl = gomock.NewController(suite.T())
	suite.mockClient = mock_ecr.NewMockECRSDK(suite.ctrl)
	suite.ecrClient = ecr.NewECRClient(suite.mockClient)
}

func (suite *GetAuthorizationTokenTestSuite) TeardownTest() {
	suite.ctrl.Finish()
}

func (suite *GetAuthorizationTokenTestSuite) TestGetAuthorizationTokenMissingAuthData() {
	suite.mockClient.EXPECT().GetAuthorizationToken(
		&ecrapi.GetAuthorizationTokenInput{
			RegistryIds: []*string{aws.String(testRegistryId)},
		}).Return(&ecrapi.GetAuthorizationTokenOutput{
		AuthorizationData: []*ecrapi.AuthorizationData{},
	}, nil)

	authorizationData, err := suite.ecrClient.GetAuthorizationToken(testRegistryId)
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), authorizationData)
}

func (suite *GetAuthorizationTokenTestSuite) TestGetAuthorizationTokenError() {
	suite.mockClient.EXPECT().GetAuthorizationToken(
		&ecrapi.GetAuthorizationTokenInput{
			RegistryIds: []*string{aws.String(testRegistryId)},
		}).Return(nil, errors.New("Nope Nope Nope"))

	authorizationData, err := suite.ecrClient.GetAuthorizationToken(testRegistryId)
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), authorizationData)
}
