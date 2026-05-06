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

	ec2metadata "github.com/aws/aws-sdk-go-v2/feature/ec2/imds"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	types "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/pkg/errors"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils/awssession"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2metadatawrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2wrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)

const (
	// cloudwatchMetricNamespace for custom metrics
	cloudwatchMetricNamespace = "Kubernetes"

	// Metric dimension constants
	clusterIDDimension = "CLUSTER_ID"

	// localMetricData is the default size for the local queue(slice)
	localMetricDataSize = 100

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

// cloudWatchAPI defines the interface with methods required from CloudWatch Service
type cloudWatchAPI interface {
	PutMetricData(ctx context.Context, params *cloudwatch.PutMetricDataInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.PutMetricDataOutput, error)
}

// Publisher defines the interface to publish one or more data points
type Publisher interface {
	// Publish publishes one or more metric data points
	Publish(metricsDataPoints ...types.MetricDatum)

	// Start is to initiate the batch and publish operation
	Start(publishInterval int)

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
	cloudwatchClient     cloudWatchAPI
	localMetricData      []types.MetricDatum
	lock                 sync.RWMutex
	log                  logger.Logger
}

// Logic to fetch Region and CLUSTER_ID
// Case 1: Cx not using IRSA, we need to get region and clusterID using IMDS
// Case 2: Cx using IRSA but not specified clusterID, we can still get this info if IMDS is not blocked
// Case 3: Cx blocked IMDS access and not using IRSA (which means region == "") AND
// not specified clusterID then its a Cx error
// New returns a new instance of `Publisher`
func New(ctx context.Context, region string, clusterID string, log logger.Logger) (Publisher, error) {
	cfg, err := awssession.New(ctx)
	if err != nil {
		return nil, err
	}

	// If Customers have explicitly specified clusterID then skip generating it
	if clusterID == "" {
		ec2client, err := ec2wrapper.NewMetricsClient()
		if err != nil {
			return nil, errors.Wrap(err, "publisher: unable to obtain EC2 service client")
		}
		clusterID = getClusterID(ec2client, log)
	}

	// Try to fetch region if not available
	if region == "" {
		// Get ec2metadata client
		ec2Metadataclient, err := ec2metadatawrapper.New(ctx)
		if err != nil {
			return nil, err
		}
		output, err := ec2Metadataclient.GetRegion(ctx, &ec2metadata.GetRegionInput{})
		region = output.Region
		if err != nil {
			return nil, err
		}
	}

	log.Infof("Using REGION=%s and CLUSTER_ID=%s", region, clusterID)

	cfg.Region = region
	cloudwatchClient := cloudwatch.NewFromConfig(cfg)

	// Build derived context
	derivedContext, cancel := context.WithCancel(ctx)

	return &cloudWatchPublisher{
		ctx:              derivedContext,
		cancel:           cancel,
		cloudwatchClient: cloudwatchClient,
		clusterID:        clusterID,
		localMetricData:  make([]types.MetricDatum, 0, localMetricDataSize),
		log:              log,
	}, nil
}

// Start is used to set up the monitor loop
func (p *cloudWatchPublisher) Start(publishInterval int) {
	p.log.Infof("Starting monitor loop for CloudWatch publisher with push interval of %d seconds", publishInterval)
	publishIntervalDuration := time.Second * time.Duration(publishInterval)
	p.monitor(publishIntervalDuration)
}

// Stop is used to cancel the monitor loop
func (p *cloudWatchPublisher) Stop() {
	p.log.Info("Stopping monitor loop for CloudWatch publisher")
	p.cancel()
}

// Publish is a variadic function to publish one or more metric data points
func (p *cloudWatchPublisher) Publish(metricDataPoints ...types.MetricDatum) {
	// Fetch dimensions for override
	p.log.Info("Fetching CloudWatch dimensions")
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
	p.localMetricData = make([]types.MetricDatum, 0, localMetricDataSize)
	p.lock.Unlock()
	p.push(data)
}

func (p *cloudWatchPublisher) push(metricData []types.MetricDatum) {
	if len(metricData) == 0 {
		p.log.Info("Missing data for publishing CloudWatch metrics")
		return
	}

	// Setup input
	input := &cloudwatch.PutMetricDataInput{
		Namespace: aws.String(cloudwatchMetricNamespace),
	}

	for len(metricData) > 0 {
		input.MetricData = metricData[:min(maxDataPoints, len(metricData))]

		// Publish data
		err := p.send(input)
		if err != nil {
			p.log.Warnf("Unable to publish CloudWatch metrics: %v", err)
		}

		// Mutate slice

		metricData = metricData[min(maxDataPoints, len(metricData)):]

		// Reset Input
		input.MetricData = nil
	}
}

// Why is there a *cloudwatch.PutMetricDataInput and cloudwatch.PutMetricDataInput?
func (p *cloudWatchPublisher) send(input *cloudwatch.PutMetricDataInput) error {
	p.log.Info("Sending data to CloudWatch metrics")
	_, err := p.cloudwatchClient.PutMetricData(p.ctx, input)
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

func getClusterID(ec2Client *ec2wrapper.EC2Wrapper, log logger.Logger) string {
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

func (p *cloudWatchPublisher) getCloudWatchMetricDatumDimensions() []types.Dimension {
	return []types.Dimension{
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
