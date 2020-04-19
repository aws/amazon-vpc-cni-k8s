// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package ec2metadata

import (
	"github.com/aws/aws-sdk-go/aws"
	ec2metadatasvc "github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
)

// EC2Metadata wraps the methods from the amazon-sdk-go's ec2metadata package
type EC2Metadata interface {
	GetMetadata(path string) (string, error)
	Region() (string, error)
}

// New creates a new EC2Metadata object
func New() EC2Metadata {
	awsSession := session.Must(session.NewSession(aws.NewConfig().
		WithMaxRetries(10),
	))
	return ec2metadatasvc.New(awsSession)
}
