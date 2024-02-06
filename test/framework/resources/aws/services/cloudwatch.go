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

package services

import (
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/cloudwatch/cloudwatchiface"
)

type CloudWatch interface {
	GetMetricStatistics(getMetricStatisticsInput *cloudwatch.GetMetricStatisticsInput) (*cloudwatch.GetMetricStatisticsOutput, error)
	PutMetricData(input *cloudwatch.PutMetricDataInput) (*cloudwatch.PutMetricDataOutput, error)
}

type defaultCloudWatch struct {
	cloudwatchiface.CloudWatchAPI
}

func NewCloudWatch(session *session.Session) CloudWatch {
	return &defaultCloudWatch{
		CloudWatchAPI: cloudwatch.New(session),
	}
}

func (d *defaultCloudWatch) GetMetricStatistics(getMetricStatisticsInput *cloudwatch.GetMetricStatisticsInput) (*cloudwatch.GetMetricStatisticsOutput, error) {
	return d.CloudWatchAPI.GetMetricStatistics(getMetricStatisticsInput)
}

func (d *defaultCloudWatch) PutMetricData(input *cloudwatch.PutMetricDataInput) (*cloudwatch.PutMetricDataOutput, error) {
	return d.CloudWatchAPI.PutMetricData(input)
}
