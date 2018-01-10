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

package ecsclient

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/async"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/ec2"
	"github.com/aws/amazon-ecs-agent/agent/ecs_client/model/ecs"
	"github.com/aws/amazon-ecs-agent/agent/httpclient"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/cihub/seelog"
	"github.com/docker/docker/pkg/system"
)

const (
	ecsMaxReasonLength    = 255
	pollEndpointCacheSize = 1
	pollEndpointCacheTTL  = 20 * time.Minute
	roundtripTimeout      = 5 * time.Second
)

// APIECSClient implements ECSClient
type APIECSClient struct {
	credentialProvider      *credentials.Credentials
	config                  *config.Config
	standardClient          api.ECSSDK
	submitStateChangeClient api.ECSSubmitStateSDK
	ec2metadata             ec2.EC2MetadataClient
	pollEndpoinCache        async.Cache
}

// NewECSClient creates a new ECSClient interface object
func NewECSClient(
	credentialProvider *credentials.Credentials,
	config *config.Config,
	ec2MetadataClient ec2.EC2MetadataClient) api.ECSClient {

	var ecsConfig aws.Config
	ecsConfig.Credentials = credentialProvider
	ecsConfig.Region = &config.AWSRegion
	ecsConfig.HTTPClient = httpclient.New(roundtripTimeout, config.AcceptInsecureCert)
	if config.APIEndpoint != "" {
		ecsConfig.Endpoint = &config.APIEndpoint
	}
	standardClient := ecs.New(session.New(&ecsConfig))
	submitStateChangeClient := newSubmitStateChangeClient(&ecsConfig)
	pollEndpoinCache := async.NewLRUCache(pollEndpointCacheSize, pollEndpointCacheTTL)
	return &APIECSClient{
		credentialProvider:      credentialProvider,
		config:                  config,
		standardClient:          standardClient,
		submitStateChangeClient: submitStateChangeClient,
		ec2metadata:             ec2MetadataClient,
		pollEndpoinCache:        pollEndpoinCache,
	}
}

// SetSDK overrides the SDK to the given one. This is useful for injecting a
// test implementation
func (client *APIECSClient) SetSDK(sdk api.ECSSDK) {
	client.standardClient = sdk
}

// SetSubmitStateChangeSDK overrides the SDK to the given one. This is useful
// for injecting a test implementation
func (client *APIECSClient) SetSubmitStateChangeSDK(sdk api.ECSSubmitStateSDK) {
	client.submitStateChangeClient = sdk
}

// CreateCluster creates a cluster from a given name and returns its arn
func (client *APIECSClient) CreateCluster(clusterName string) (string, error) {
	resp, err := client.standardClient.CreateCluster(&ecs.CreateClusterInput{ClusterName: &clusterName})
	if err != nil {
		seelog.Criticalf("Could not create cluster: %v", err)
		return "", err
	}
	seelog.Infof("Created a cluster named: %s", clusterName)
	return *resp.Cluster.ClusterName, nil
}

// RegisterContainerInstance calculates the appropriate resources, creates
// the default cluster if necessary, and returns the registered
// ContainerInstanceARN if successful. Supplying a non-empty container
// instance ARN allows a container instance to update its registered
// resources.
func (client *APIECSClient) RegisterContainerInstance(containerInstanceArn string, attributes []*ecs.Attribute) (string, error) {
	clusterRef := client.config.Cluster
	// If our clusterRef is empty, we should try to create the default
	if clusterRef == "" {
		clusterRef = config.DefaultClusterName
		defer func() {
			// Update the config value to reflect the cluster we end up in
			client.config.Cluster = clusterRef
		}()
		// Attempt to register without checking existence of the cluster so we don't require
		// excess permissions in the case where the cluster already exists and is active
		containerInstanceArn, err := client.registerContainerInstance(clusterRef, containerInstanceArn, attributes)
		if err == nil {
			return containerInstanceArn, nil
		}
		// If trying to register fails, try to create the cluster before calling
		// register again
		clusterRef, err = client.CreateCluster(clusterRef)
		if err != nil {
			return "", err
		}
	}
	return client.registerContainerInstance(clusterRef, containerInstanceArn, attributes)
}

func (client *APIECSClient) registerContainerInstance(clusterRef string, containerInstanceArn string, attributes []*ecs.Attribute) (string, error) {
	registerRequest := ecs.RegisterContainerInstanceInput{Cluster: &clusterRef}
	var registrationAttributes []*ecs.Attribute
	if containerInstanceArn != "" {
		// We are re-connecting a previously registered instance, restored from snapshot.
		registerRequest.ContainerInstanceArn = &containerInstanceArn
	} else {
		// This is a new instance, not previously registered.
		// Custom attribute registration only happens on initial instance registration.
		for _, attribute := range client.getCustomAttributes() {
			seelog.Debugf("Added a new custom attribute %v=%v",
				aws.StringValue(attribute.Name),
				aws.StringValue(attribute.Value),
			)
			registrationAttributes = append(registrationAttributes, attribute)
		}
	}
	// Standard attributes are included with all registrations.
	registrationAttributes = append(registrationAttributes, attributes...)

	// Add additional attributes such as the os type
	registrationAttributes = append(registrationAttributes, client.getAdditionalAttributes()...)
	registerRequest.Attributes = registrationAttributes
	iidRetrieved := true
	instanceIdentityDoc, err := client.ec2metadata.GetDynamicData(ec2.InstanceIdentityDocumentResource)
	if err != nil {
		seelog.Errorf("Unable to get instance identity document: %v", err)
		iidRetrieved = false
		instanceIdentityDoc = ""
	}
	registerRequest.InstanceIdentityDocument = &instanceIdentityDoc

	instanceIdentitySignature := ""
	if iidRetrieved {
		instanceIdentitySignature, err = client.ec2metadata.GetDynamicData(ec2.InstanceIdentityDocumentSignatureResource)
		if err != nil {
			seelog.Errorf("Unable to get instance identity signature: %v", err)
		}
	}

	registerRequest.InstanceIdentityDocumentSignature = &instanceIdentitySignature

	// Micro-optimization, the pointer to this is used multiple times below
	integerStr := "INTEGER"

	cpu, mem := getCpuAndMemory()
	remainingMem := mem - int64(client.config.ReservedMemory)
	if remainingMem < 0 {
		return "", fmt.Errorf(
			"api register-container-instance: reserved memory is higher than available memory on the host, total memory: %d, reserved: %d",
			mem, client.config.ReservedMemory)
	}

	cpuResource := ecs.Resource{
		Name:         utils.Strptr("CPU"),
		Type:         &integerStr,
		IntegerValue: &cpu,
	}
	memResource := ecs.Resource{
		Name:         utils.Strptr("MEMORY"),
		Type:         &integerStr,
		IntegerValue: &remainingMem,
	}
	portResource := ecs.Resource{
		Name:           utils.Strptr("PORTS"),
		Type:           utils.Strptr("STRINGSET"),
		StringSetValue: utils.Uint16SliceToStringSlice(client.config.ReservedPorts),
	}
	udpPortResource := ecs.Resource{
		Name:           utils.Strptr("PORTS_UDP"),
		Type:           utils.Strptr("STRINGSET"),
		StringSetValue: utils.Uint16SliceToStringSlice(client.config.ReservedPortsUDP),
	}

	resources := []*ecs.Resource{&cpuResource, &memResource, &portResource, &udpPortResource}
	registerRequest.TotalResources = resources

	resp, err := client.standardClient.RegisterContainerInstance(&registerRequest)
	if err != nil {
		seelog.Errorf("Could not register: %v", err)
		return "", err
	}
	seelog.Info("Registered container instance with cluster!")
	err = validateRegisteredAttributes(registerRequest.Attributes, resp.ContainerInstance.Attributes)
	return aws.StringValue(resp.ContainerInstance.ContainerInstanceArn), err
}

func attributesToMap(attributes []*ecs.Attribute) map[string]string {
	attributeMap := make(map[string]string)
	attribs := attributes
	for _, attribute := range attribs {
		attributeMap[aws.StringValue(attribute.Name)] = aws.StringValue(attribute.Value)
	}
	return attributeMap
}

func findMissingAttributes(expectedAttributes, actualAttributes map[string]string) ([]string, error) {
	missingAttributes := make([]string, 0)
	var err error
	for key, val := range expectedAttributes {
		if actualAttributes[key] != val {
			missingAttributes = append(missingAttributes, key)
		} else {
			seelog.Tracef("Response contained expected value for attribute %v", key)
		}
	}
	if len(missingAttributes) > 0 {
		err = utils.NewAttributeError("Attribute validation failed")
	}
	return missingAttributes, err
}

func validateRegisteredAttributes(expectedAttributes, actualAttributes []*ecs.Attribute) error {
	var err error
	expectedAttributesMap := attributesToMap(expectedAttributes)
	actualAttributesMap := attributesToMap(actualAttributes)
	missingAttributes, err := findMissingAttributes(expectedAttributesMap, actualAttributesMap)
	if err != nil {
		msg := strings.Join(missingAttributes, ",")
		seelog.Errorf("Error registering attributes: %v", msg)
	}
	return err
}

func getCpuAndMemory() (int64, int64) {
	memInfo, err := system.ReadMemInfo()
	mem := int64(0)
	if err == nil {
		mem = memInfo.MemTotal / 1024 / 1024 // MiB
	} else {
		seelog.Errorf("Unable getting memory info: %v", err)
	}

	cpu := runtime.NumCPU() * 1024

	return int64(cpu), mem
}

func (client *APIECSClient) getAdditionalAttributes() []*ecs.Attribute {
	return []*ecs.Attribute{&ecs.Attribute{
		Name:  aws.String("ecs.os-type"),
		Value: aws.String(api.OSType),
	}}
}

func (client *APIECSClient) getCustomAttributes() []*ecs.Attribute {
	var attributes []*ecs.Attribute
	for attribute, value := range client.config.InstanceAttributes {
		attributes = append(attributes, &ecs.Attribute{
			Name:  aws.String(attribute),
			Value: aws.String(value),
		})
	}
	return attributes
}

func (client *APIECSClient) SubmitTaskStateChange(change api.TaskStateChange) error {
	// Submit attachment state change
	if change.Attachment != nil {
		var attachments []*ecs.AttachmentStateChange

		eniStatus := change.Attachment.Status.String()
		attachments = []*ecs.AttachmentStateChange{
			{
				AttachmentArn: aws.String(change.Attachment.AttachmentARN),
				Status:        aws.String(eniStatus),
			},
		}

		_, err := client.submitStateChangeClient.SubmitTaskStateChange(&ecs.SubmitTaskStateChangeInput{
			Cluster:     aws.String(client.config.Cluster),
			Task:        aws.String(change.TaskARN),
			Attachments: attachments,
		})
		if err != nil {
			seelog.Warnf("Could not submit an attachment state change: %v", err)
			return err
		}

		return nil
	}

	status := change.Status.BackendStatus()

	req := ecs.SubmitTaskStateChangeInput{
		Cluster:            aws.String(client.config.Cluster),
		Task:               aws.String(change.TaskARN),
		Status:             aws.String(status),
		Reason:             aws.String(change.Reason),
		PullStartedAt:      change.PullStartedAt,
		PullStoppedAt:      change.PullStoppedAt,
		ExecutionStoppedAt: change.ExecutionStoppedAt,
	}

	containerEvents := make([]*ecs.ContainerStateChange, len(change.Containers))
	for i, containerEvent := range change.Containers {
		containerEvents[i] = client.buildContainerStateChangePayload(containerEvent)
	}

	req.Containers = containerEvents

	_, err := client.submitStateChangeClient.SubmitTaskStateChange(&req)
	if err != nil {
		seelog.Warnf("Could not submit task state change: [%s]: %v", change.String(), err)
		return err
	}

	return nil
}

func (client *APIECSClient) buildContainerStateChangePayload(change api.ContainerStateChange) *ecs.ContainerStateChange {
	statechange := &ecs.ContainerStateChange{
		ContainerName: aws.String(change.ContainerName),
	}

	if change.Reason != "" {
		if len(change.Reason) > ecsMaxReasonLength {
			trimmed := change.Reason[0:ecsMaxReasonLength]
			statechange.Reason = aws.String(trimmed)
		} else {
			statechange.Reason = aws.String(change.Reason)
		}
	}
	status := change.Status

	if status != api.ContainerStopped && status != api.ContainerRunning {
		seelog.Warnf("Not submitting unsupported upstream container state %s for container %s in task %s",
			status.String(), change.ContainerName, change.TaskArn)
		return nil
	}

	statechange.Status = aws.String(status.String())

	if change.ExitCode != nil {
		exitCode := int64(aws.IntValue(change.ExitCode))
		statechange.ExitCode = aws.Int64(exitCode)
	}
	networkBindings := make([]*ecs.NetworkBinding, len(change.PortBindings))
	for i, binding := range change.PortBindings {
		hostPort := int64(binding.HostPort)
		containerPort := int64(binding.ContainerPort)
		bindIP := binding.BindIP
		protocol := binding.Protocol.String()

		networkBindings[i] = &ecs.NetworkBinding{
			BindIP:        aws.String(bindIP),
			ContainerPort: aws.Int64(containerPort),
			HostPort:      aws.Int64(hostPort),
			Protocol:      aws.String(protocol),
		}
	}
	statechange.NetworkBindings = networkBindings

	return statechange
}

func (client *APIECSClient) SubmitContainerStateChange(change api.ContainerStateChange) error {
	req := ecs.SubmitContainerStateChangeInput{
		Cluster:       &client.config.Cluster,
		Task:          &change.TaskArn,
		ContainerName: &change.ContainerName,
	}
	if change.Reason != "" {
		if len(change.Reason) > ecsMaxReasonLength {
			trimmed := change.Reason[0:ecsMaxReasonLength]
			req.Reason = &trimmed
		} else {
			req.Reason = &change.Reason
		}
	}
	stat := change.Status.String()
	if stat == "DEAD" {
		stat = "STOPPED"
	}
	if stat != "STOPPED" && stat != "RUNNING" {
		seelog.Infof("Not submitting unsupported upstream container state: %s", stat)
		return nil
	}
	req.Status = &stat
	if change.ExitCode != nil {
		exitCode := int64(*change.ExitCode)
		req.ExitCode = &exitCode
	}
	networkBindings := make([]*ecs.NetworkBinding, len(change.PortBindings))
	for i, binding := range change.PortBindings {
		hostPort := int64(binding.HostPort)
		containerPort := int64(binding.ContainerPort)
		bindIP := binding.BindIP
		protocol := binding.Protocol.String()
		networkBindings[i] = &ecs.NetworkBinding{
			BindIP:        &bindIP,
			ContainerPort: &containerPort,
			HostPort:      &hostPort,
			Protocol:      &protocol,
		}
	}
	req.NetworkBindings = networkBindings

	_, err := client.submitStateChangeClient.SubmitContainerStateChange(&req)
	if err != nil {
		seelog.Warnf("Could not submit container state change: [%s]: %v", change.String(), err)
		return err
	}
	return nil
}

func (client *APIECSClient) DiscoverPollEndpoint(containerInstanceArn string) (string, error) {
	resp, err := client.discoverPollEndpoint(containerInstanceArn)
	if err != nil {
		return "", err
	}

	return aws.StringValue(resp.Endpoint), nil
}

func (client *APIECSClient) DiscoverTelemetryEndpoint(containerInstanceArn string) (string, error) {
	resp, err := client.discoverPollEndpoint(containerInstanceArn)
	if err != nil {
		return "", err
	}
	if resp.TelemetryEndpoint == nil {
		return "", errors.New("No telemetry endpoint returned; nil")
	}

	return aws.StringValue(resp.TelemetryEndpoint), nil
}

func (client *APIECSClient) discoverPollEndpoint(containerInstanceArn string) (*ecs.DiscoverPollEndpointOutput, error) {
	// Try getting an entry from the cache
	cachedEndpoint, found := client.pollEndpoinCache.Get(containerInstanceArn)
	if found {
		// Cache hit. Return the output.
		if output, ok := cachedEndpoint.(*ecs.DiscoverPollEndpointOutput); ok {
			return output, nil
		}
	}

	// Cache miss, invoke the ECS DiscoverPollEndpoint API.
	seelog.Debugf("Invoking DiscoverPollEndpoint for '%s'", containerInstanceArn)
	output, err := client.standardClient.DiscoverPollEndpoint(&ecs.DiscoverPollEndpointInput{
		ContainerInstance: &containerInstanceArn,
		Cluster:           &client.config.Cluster,
	})
	if err != nil {
		return nil, err
	}

	// Cache the response from ECS.
	client.pollEndpoinCache.Set(containerInstanceArn, output)
	return output, nil
}
