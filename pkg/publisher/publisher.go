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

// Package publisher is used to batch and send metric data to CloudWatch
package publisher

import (
	"context"
	"sync"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2metadatawrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2wrapper"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/cloudwatch/cloudwatchiface"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/pkg/errors"
)

const (
	// defaultInterval for monitoring the watch list
	defaultInterval = time.Second * 60

	// cloudwatchMetricNamespace for custom metrics
	cloudwatchMetricNamespace = "Kubernetes"

	// Metric dimension constants
	clusterIDDimension = "CLUSTER_ID"

	// localMetricData is the default size for the local queue(slice)
	localMetricDataSize = 100

	// cloudwatchClientMaxRetries for configuring CloudWatch client with maximum retries
	cloudwatchClientMaxRetries = 20

	// maxDataPoints is the maximum number of data points per PutMetricData API request
	maxDataPoints = 20

	// Default cluster id if unable to detect something more suitable
	defaultClusterID = "k8s-cluster"
)

var (
	// List of EC2 tags (in priority order) to use as the CLUSTER_ID metric dimension
	clusterIDTags = []string{
		"eks:cluster-name",
		"CLUSTER_ID",
		"Name",
	}
)

var log = logger.Get()

// Publisher defines the interface to publish one or more data points
type Publisher interface {
	// Publish publishes one or more metric data points
	Publish(metricDataPoints ...*cloudwatch.MetricDatum)

	// Start is to initiate the batch and publish operation
	Start()

	// Stop is to terminate the batch and publish operation
	Stop()
}

// cloudWatchPublisher implements the `Publisher` interface for batching and publishing
// metric data to the CloudWatch metrics backend
type cloudWatchPublisher struct {
	ctx                  context.Context
	cancel               context.CancelFunc
	updateIntervalTicker *time.Ticker
	clusterID            string
	cloudwatchClient     cloudwatchiface.CloudWatchAPI
	localMetricData      []*cloudwatch.MetricDatum
	lock                 sync.RWMutex
}

// New returns a new instance of `Publisher`
func New(ctx context.Context) (Publisher, error) {
	// Get AWS session
	awsSession := session.Must(session.NewSession())

	// Get cluster-ID
	ec2Client, err := ec2wrapper.NewMetricsClient()
	if err != nil {
		return nil, errors.Wrap(err, "publisher: unable to obtain EC2 service client")
	}
	clusterID := getClusterID(ec2Client)

	// Get CloudWatch client
	ec2MetadataClient := ec2metadatawrapper.New(nil)

	region, err := ec2MetadataClient.Region()
	if err != nil {
		return nil, errors.Wrap(err, "publisher: Unable to obtain region")
	}
	cloudwatchClient := cloudwatch.New(awsSession, aws.NewConfig().WithMaxRetries(cloudwatchClientMaxRetries).WithRegion(region))

	// Build derived context
	derivedContext, cancel := context.WithCancel(ctx)

	return &cloudWatchPublisher{
		ctx:              derivedContext,
		cancel:           cancel,
		cloudwatchClient: cloudwatchClient,
		clusterID:        clusterID,
		localMetricData:  make([]*cloudwatch.MetricDatum, 0, localMetricDataSize),
	}, nil
}

// Start is used to setup the monitor loop
func (p *cloudWatchPublisher) Start() {
	log.Info("Starting monitor loop for CloudWatch publisher")
	p.monitor(defaultInterval)
}

// Stop is used to cancel the monitor loop
func (p *cloudWatchPublisher) Stop() {
	log.Info("Stopping monitor loop for CloudWatch publisher")
	p.cancel()
}

// Publish is a variadic function to publish one or more metric data points
func (p *cloudWatchPublisher) Publish(metricDataPoints ...*cloudwatch.MetricDatum) {
	// Fetch dimensions for override
	log.Info("Fetching CloudWatch dimensions")
	dimensions := p.getCloudWatchMetricDatumDimensions()

	// Grab lock
	p.lock.Lock()
	defer p.lock.Unlock()

	// NOTE: Iteration is used to override the metric dimensions
	for _, metricDatum := range metricDataPoints {
		metricDatum.Dimensions = dimensions
		p.localMetricData = append(p.localMetricData, metricDatum)
	}
}

func (p *cloudWatchPublisher) pushLocal() {
	p.lock.Lock()
	data := p.localMetricData[:]
	p.localMetricData = make([]*cloudwatch.MetricDatum, 0, localMetricDataSize)
	p.lock.Unlock()
	p.push(data)
}

func (p *cloudWatchPublisher) push(metricData []*cloudwatch.MetricDatum) {
	if len(metricData) == 0 {
		log.Info("Missing data for publishing CloudWatch metrics")
		return
	}

	// Setup input
	input := cloudwatch.PutMetricDataInput{}
	input.Namespace = p.getCloudWatchMetricNamespace()

	// NOTE: Ensure cap of 40K per request and enforce rate limiting
	for len(metricData) > 0 {
		input.MetricData = metricData[:maxDataPoints]

		// Publish data
		err := p.send(input)
		if err != nil {
			log.Warnf("Unable to publish CloudWatch metrics: %v", err)
		}

		// Mutate slice
		index := min(maxDataPoints, len(metricData))
		metricData = metricData[index:]

		// Reset Input
		input = cloudwatch.PutMetricDataInput{}
		input.Namespace = p.getCloudWatchMetricNamespace()
	}
}

func (p *cloudWatchPublisher) send(input cloudwatch.PutMetricDataInput) error {
	log.Info("Sending data to CloudWatch metrics")
	_, err := p.cloudwatchClient.PutMetricData(&input)
	return err
}

func (p *cloudWatchPublisher) monitor(interval time.Duration) {
	p.updateIntervalTicker = time.NewTicker(interval)
	for {
		select {
		case <-p.updateIntervalTicker.C:
			p.pushLocal()

		case <-p.ctx.Done():
			p.Stop()
			return
		}
	}
}

func (p *cloudWatchPublisher) getCloudWatchMetricNamespace() *string {
	return aws.String(cloudwatchMetricNamespace)
}

func getClusterID(ec2Client *ec2wrapper.EC2Wrapper) string {
	var clusterID string
	var err error
	for _, tag := range clusterIDTags {
		clusterID, err = ec2Client.GetClusterTag(tag)
		if err == nil && clusterID != "" {
			break
		}
	}
	if clusterID == "" {
		clusterID = defaultClusterID
	}
	log.Infof("Using cluster ID ", clusterID)
	return clusterID
}

func (p *cloudWatchPublisher) getCloudWatchMetricDatumDimensions() []*cloudwatch.Dimension {
	return []*cloudwatch.Dimension{
		{
			Name:  aws.String(clusterIDDimension),
			Value: aws.String(p.clusterID),
		},
	}
}

// min is a helper to compute the min of two integers
func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}
