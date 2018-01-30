// Copyright 2014-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package ec2

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
)

const (
	SecurityCrednetialsResource               = "iam/security-credentials/"
	InstanceIdentityDocumentResource          = "instance-identity/document"
	InstanceIdentityDocumentSignatureResource = "instance-identity/signature"
	MacResource                               = "mac"
	VPCIDResourceFormat                       = "network/interfaces/macs/%s/vpc-id"
	SubnetIDResourceFormat                    = "network/interfaces/macs/%s/subnet-id"
)

const (
	metadataRetries = 5
)

// RoleCredentials contains the information associated with an IAM role
type RoleCredentials struct {
	Code            string    `json:"Code"`
	LastUpdated     time.Time `json:"LastUpdated"`
	Type            string    `json:"Type"`
	AccessKeyId     string    `json:"AccessKeyId"`
	SecretAccessKey string    `json:"SecretAccessKey"`
	Token           string    `json:"Token"`
	Expiration      time.Time `json:"Expiration"`
}

type HttpClient interface {
	GetMetadata(string) (string, error)
	GetDynamicData(string) (string, error)
	GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error)
}

// EC2MetadataClient is the client used to get metadata from instance metadata service
type EC2MetadataClient interface {
	DefaultCredentials() (*RoleCredentials, error)
	GetMetadata(string) (string, error)
	GetDynamicData(string) (string, error)
	InstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error)
	VPCID(mac string) (string, error)
	SubnetID(mac string) (string, error)
	PrimaryENIMAC() (string, error)
}

type ec2MetadataClientImpl struct {
	client HttpClient
}

// NewEC2MetadataClient creates an ec2metadata client to retrieve metadata
func NewEC2MetadataClient(client HttpClient) EC2MetadataClient {
	if client == nil {
		return &ec2MetadataClientImpl{client: ec2metadata.New(session.New(), aws.NewConfig().WithMaxRetries(metadataRetries))}
	} else {
		return &ec2MetadataClientImpl{client: client}
	}
}

// DefaultCredentials returns the credentials associated with the instance iam role
func (c *ec2MetadataClientImpl) DefaultCredentials() (*RoleCredentials, error) {
	securityCredential, err := c.client.GetMetadata(SecurityCrednetialsResource)
	if err != nil {
		return nil, err
	}

	securityCredentialList := strings.Split(strings.TrimSpace(securityCredential), "\n")
	if len(securityCredentialList) == 0 {
		return nil, errors.New("No security credentials in response")
	}

	defaultCredentialName := securityCredentialList[0]

	defaultCredentialStr, err := c.client.GetMetadata(SecurityCrednetialsResource + defaultCredentialName)
	if err != nil {
		return nil, err
	}
	var credential RoleCredentials
	err = json.Unmarshal([]byte(defaultCredentialStr), &credential)
	if err != nil {
		return nil, err
	}
	return &credential, nil
}

// GetDynamicData returns the dynamic data with provided path from instance metadata
func (c *ec2MetadataClientImpl) GetDynamicData(path string) (string, error) {
	return c.client.GetDynamicData(path)
}

// InstanceIdentityDocument returns instance identity documents
// http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instance-identity-documents.html
func (c *ec2MetadataClientImpl) InstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error) {
	return c.client.GetInstanceIdentityDocument()
}

// GetMetadata returns the metadata from instance metadata service specified by the path
func (c *ec2MetadataClientImpl) GetMetadata(path string) (string, error) {
	return c.client.GetMetadata(path)
}

// PrimaryENIMAC returns the MAC address for the primary
// network interface of the instance
func (c *ec2MetadataClientImpl) PrimaryENIMAC() (string, error) {
	return c.client.GetMetadata(MacResource)
}

// VPCID returns the VPC id for the network interface, given
// its mac address
func (c *ec2MetadataClientImpl) VPCID(mac string) (string, error) {
	return c.client.GetMetadata(fmt.Sprintf(VPCIDResourceFormat, mac))
}

// SubnetID returns the subnet id for the network interface,
// given its mac address
func (c *ec2MetadataClientImpl) SubnetID(mac string) (string, error) {
	return c.client.GetMetadata(fmt.Sprintf(SubnetIDResourceFormat, mac))
}
