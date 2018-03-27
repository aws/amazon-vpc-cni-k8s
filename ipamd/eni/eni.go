// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

// Package eni abstracts away aws sdk calls for ENI management.
package eni

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/resourcegroupstaggingapi"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd/metrics"
)

type EC2 interface {
	DescribeInstancesWithContext(aws.Context, *ec2.DescribeInstancesInput, ...request.Option) (*ec2.DescribeInstancesOutput, error)
	CreateNetworkInterfaceWithContext(aws.Context, *ec2.CreateNetworkInterfaceInput, ...request.Option) (*ec2.CreateNetworkInterfaceOutput, error)
	AttachNetworkInterfaceWithContext(aws.Context, *ec2.AttachNetworkInterfaceInput, ...request.Option) (*ec2.AttachNetworkInterfaceOutput, error)
	ModifyNetworkInterfaceAttributeWithContext(aws.Context, *ec2.ModifyNetworkInterfaceAttributeInput, ...request.Option) (*ec2.ModifyNetworkInterfaceAttributeOutput, error)
	DetachNetworkInterfaceWithContext(aws.Context, *ec2.DetachNetworkInterfaceInput, ...request.Option) (*ec2.DetachNetworkInterfaceOutput, error)

	DeleteNetworkInterfaceWithContext(aws.Context, *ec2.DeleteNetworkInterfaceInput, ...request.Option) (*ec2.DeleteNetworkInterfaceOutput, error)
}

type EC2Instance struct {
	instance *ec2.Instance
	ownerId  string
	updated  time.Time
	dirty    bool

	ipsPerEni int64

	maxIPs, maxENIs    int64
	region, instanceID string
	lock               sync.RWMutex

	tag     *resourcegroupstaggingapi.ResourceGroupsTaggingAPI
	ec2     EC2
	metrics *metrics.Metrics
}

const (
	metadataInstanceID   = "instance-id"
	metadataInstanceType = "instance-type"
	cacheTTL             = 1 * time.Second
	eniTagKey            = "k8s-eni-key"
	// AllocENI need to choose a first free device number between 0 and maxENI
	maxENIs              = 128 // TODO(tvi): Use smaller constant, or dynamic check.
	maxENIDeleteRetries  = 5
	eniDescriptionPrefix = "aws-K8S-"

	retryDeleteENIInternal = 5 * time.Second

	metadataMACPath = "network/interfaces/macs/"
	metadataENIPath = "network/interfaces/macs/%v/interface-id"

	metadataMAC     = "mac"
	metadataVPCcidr = "/vpc-ipv4-cidr-block/"
	metadataLocalIP = "local-ipv4"

	waiterSleep = 3 * time.Second
	addENIRetry = 2 * time.Second
)

type ENIService interface {
	AddENI(context.Context) (string, bool, error)
	FreeENI(context.Context, string) error
	IsENIReady(context.Context, string) (bool, error)

	GetENIs(context.Context) ([]string, error)
	GetENIMetadata(context.Context, string) (ENIMetadata, error)

	// Returns CIDR, localIP, primaryENI
	GetPrimaryDetails(ctx context.Context) (string, string, string, error)

	GetMaxIPs() int64
}

type ENIMetadata struct {
	MAC          string
	PrimaryIP    string
	SecondaryIPs []string
	Device       int
	CIDR         string
}

// containsAttachmentLimitExceededError returns whether exceeds instance's ENI limit
func containsAttachmentLimitExceededError(err error) bool {
	if aerr, ok := err.(awserr.Error); ok {
		return aerr.Code() == "AttachmentLimitExceeded"
	}
	return false
}

func GetMetadata() (string, string, string, error) {
	sess := session.New()
	svc := ec2metadata.New(sess)

	region, err := svc.Region()
	if err != nil {
		return "", "", "", errors.Wrap(err, "instance metadata: failed to retrieve region data")
	}
	instanceID, err := svc.GetMetadata(metadataInstanceID)
	if err != nil {
		return "", "", "", errors.Wrap(err, "instance metadata: failed to retrieve instance-id")
	}
	instanceType, err := svc.GetMetadata(metadataInstanceType)
	if err != nil {
		return "", "", "", errors.Wrap(err, "instance metadata: failed to retrieve instance-type")
	}
	return region, instanceID, instanceType, nil
}

func NewEC2Instance(region, instanceID, instanceType string, metrics *metrics.Metrics) (*EC2Instance, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		return nil, errors.Wrap(err, "instance metadata: failed to initialize AWS SDK session")
	}
	ec := ec2.New(sess)
	tag := resourcegroupstaggingapi.New(sess)

	inst := &EC2Instance{
		instance:   nil,
		region:     region,
		instanceID: instanceID,
		dirty:      true,

		tag:     tag,
		ec2:     ec,
		lock:    sync.RWMutex{},
		metrics: metrics,
	}
	limit, ok := eniLimit[instanceType]
	if !ok {
		log.Warningf("instance type %v is not included in limits table using default", instanceType)
		limit = defaultLimit
	}
	inst.maxENIs, inst.maxIPs = limit.ENILimit, limit.IPv4Limit
	inst.maxIPs = inst.maxIPs - 1 // We are looking for secondary ips
	inst.ipsPerEni = inst.maxIPs

	metrics.SetMaxENI(inst.maxENIs)

	return inst, nil
}

func (e *EC2Instance) GetMaxIPs() int64 {
	return e.ipsPerEni
}

// update describes and updates instance metadata from AWS.
// Do not use e.lock inside of this function, because of recursive locking.
func (e *EC2Instance) update(ctx context.Context) error {
	if !e.dirty && time.Since(e.updated) < cacheTTL {
		log.Debugf("update using cache values from: %v", e.updated.String())
		return nil
	}

	start := time.Now()
	result, err := e.ec2.DescribeInstancesWithContext(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []*string{aws.String(e.instanceID)},
	})
	e.metrics.TimeAWSCall(start, "DescribeInstancesWithContext", err != nil)
	if err != nil {
		return errors.Errorf("could not find instance id '%s': %v", e.instanceID, err)
	}
	if result == nil {
		return fmt.Errorf("could not find instance id '%s'", e.instanceID)
	}
	if len(result.Reservations) != 1 {
		return errors.Errorf("could not find instance id '%s'", e.instanceID)
	}
	e.instance = result.Reservations[0].Instances[0]
	e.ownerId = aws.StringValue(result.Reservations[0].OwnerId)
	e.updated = time.Now()
	e.dirty = false
	return nil
}

func (e *EC2Instance) GetPrimaryDetails(ctx context.Context) (string, string, string, error) {
	e.lock.Lock()
	defer e.lock.Unlock() // TODO(tvi): Make it read only.
	if e.dirty {
		if err := e.update(ctx); err != nil {
			return "", "", "", errors.Wrapf(err, "failed to update GetENIs")
		}
	}

	sess := session.New()
	svc := ec2metadata.New(sess)

	mac, err := svc.GetMetadata(metadataMAC)
	if err != nil {
		return "", "", "", err
	}
	cidr, err := svc.GetMetadata(metadataMACPath + mac + metadataVPCcidr)
	if err != nil {
		return "", "", "", err
	}
	localip, err := svc.GetMetadata(metadataLocalIP)
	if err != nil {
		return "", "", "", err
	}
	eniID, err := svc.GetMetadata(fmt.Sprintf(metadataENIPath, mac))
	if err != nil {
		return "", "", "", err
	}
	return cidr, localip, eniID, nil
}

func (e *EC2Instance) findFreeDevice() int64 {
	var device [maxENIs]bool
	// TODO(tvi): Rewrite this logic.
	for _, eni := range e.instance.NetworkInterfaces {
		if aws.Int64Value(eni.Attachment.DeviceIndex) > maxENIs {
			log.Warnf("The Device Index %d of the attached eni %s > instance max slot %d",
				aws.Int64Value(eni.Attachment.DeviceIndex),
				aws.StringValue(eni.NetworkInterfaceId),
				maxENIs)
		} else {
			log.Debugf("Discovered device number is used: %d", aws.Int64Value(eni.Attachment.DeviceIndex))
			device[aws.Int64Value(eni.Attachment.DeviceIndex)] = true
		}
	}

	var freeDevice int64
	for freeDeviceIndex := 0; freeDeviceIndex < maxENIs; freeDeviceIndex++ {
		if !device[freeDeviceIndex] {
			log.Debugf("Found a free device number: %d", freeDeviceIndex)
			// freeDevice = aws.Int64(int64(freeDeviceIndex))
			freeDevice = int64(freeDeviceIndex)
			break
		}
	}
	return freeDevice
}

func (e *EC2Instance) AddENI(ctx context.Context) (string, bool, error) {
	e.lock.Lock()
	defer e.lock.Unlock()

	if e.dirty {
		if err := e.update(ctx); err != nil {
			return "", false, errors.Wrapf(err, "failed to update AddENI eni")
		}
	}

	groups := []*string{}
	for _, g := range e.instance.SecurityGroups {
		groups = append(groups, g.GroupId)
	}

	var retENI string
	for i := e.ipsPerEni; i > 1; i-- {
		if i != e.ipsPerEni {
			e.metrics.AddENIRetry()
			time.Sleep(addENIRetry)
			log.Infof("Retrying (attempt %v) to add eni", e.ipsPerEni-i)
		}

		eni, runOut, err := e.tryAddEni(ctx, i, groups, aws.Int64(e.findFreeDevice()))
		if err != nil {
			log.Warningf("Failed trying to add eni with %v IPs: %v", i, err)
			continue
		}
		if runOut {
			return "", true, nil
		}

		retENI = eni
		e.ipsPerEni = i
		log.Infof("Setting maxIPs to be: %v", i)
		break
	}
	if retENI == "" {
		log.Warningf("Gave up trying to add eni")
		return "", false, fmt.Errorf("could not allocate new eni")
	}
	e.dirty = true
	return retENI, false, nil
}

func (e *EC2Instance) tryAddEni(ctx context.Context, i int64, groups []*string, freeDevice *int64) (string, bool, error) {
	start := time.Now()
	result, err := e.ec2.CreateNetworkInterfaceWithContext(ctx, &ec2.CreateNetworkInterfaceInput{
		Description: aws.String(fmt.Sprintf("aws-K8S-%v", aws.StringValue(e.instance.InstanceId))),
		SubnetId:    e.instance.SubnetId,
		Groups:      groups,
		SecondaryPrivateIpAddressCount: aws.Int64(i),
	})
	e.metrics.TimeAWSCall(start, "CreateNetworkInterfaceWithContext", err != nil)
	if err != nil {
		return "", false, errors.Wrap(err, "failed to CreateNetworkInterface")
	}

	eniID := result.NetworkInterface.NetworkInterfaceId
	eni := aws.StringValue(eniID)
	log.Infof("Created a new empty eni: %s", eni)

	start = time.Now()
	_, err = e.tag.TagResourcesWithContext(ctx, &resourcegroupstaggingapi.TagResourcesInput{
		ResourceARNList: []*string{aws.String(arn.ARN{
			Partition: "aws", Service: "ec2",
			Resource: "network-interface/" + eni,
			Region:   e.region, AccountID: e.ownerId}.String()),
		},
		Tags: map[string]*string{
			eniTagKey: aws.String(e.instanceID),
		},
	})
	e.metrics.TimeAWSCall(start, "TagResourcesWithContext", err != nil)
	if err != nil {
		return "", false, errors.Wrap(err, "failed to TagResourcesWithContext")
	}

	start = time.Now()
	attachOutput, err := e.ec2.AttachNetworkInterfaceWithContext(ctx, &ec2.AttachNetworkInterfaceInput{
		NetworkInterfaceId: eniID,
		DeviceIndex:        freeDevice,
		InstanceId:         aws.String(e.instanceID),
	})
	e.metrics.TimeAWSCall(start, "AttachNetworkInterfaceWithContext", err != nil)
	if err != nil {
		e.deleteENI(ctx, eni)
		if containsAttachmentLimitExceededError(err) {
			// TODO once reached limit, should stop retrying increasePool
			log.Infof("Exceeded instance eni attachment limit: %d ", err.Error())
			return "", true, nil
		}
		return "", false, errors.Wrap(err, "failed to AttachNetworkInterfaceWithContext")
	}

	log.Infof("Attached and tagged new eni: %s", eni)

	start = time.Now()
	_, err = e.ec2.ModifyNetworkInterfaceAttributeWithContext(ctx, &ec2.ModifyNetworkInterfaceAttributeInput{
		Attachment: &ec2.NetworkInterfaceAttachmentChanges{
			AttachmentId:        attachOutput.AttachmentId,
			DeleteOnTermination: aws.Bool(true),
		},
		NetworkInterfaceId: eniID,
	})
	e.metrics.TimeAWSCall(start, "ModifyNetworkInterfaceAttributeWithContext", err != nil)
	if err != nil {
		e.deleteENI(ctx, eni)
		return "", false, errors.Wrap(err, "failed to ModifyNetworkInterfaceAttributeWithContext")
	}

	fullWait := time.Now()
	for {
		// TODO(tvi): Add max retries?
		start = time.Now()
		// err = e.ec2.WaitUntilNetworkInterfaceAvailableWithContext(ctx, &ec2.DescribeNetworkInterfacesInput{
		// TODO(tvi): Rework dependency injection.
		err = WaitUntilNetworkInterfaceAvailableWithContext(ctx, e.ec2.(*ec2.EC2), &ec2.DescribeNetworkInterfacesInput{
			NetworkInterfaceIds: []*string{eniID},
		})
		e.metrics.TimeAWSCall(start, "WaitUntilNetworkInterfaceAvailableWithContext", false)
		if err == nil {
			break
		}
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "ResourceNotReady" {
				log.Infof("WaitUntilNetworkInterfaceAvailableWithContext timed out")
				continue
			} else {
				log.Errorf("WaitUntilNetworkInterfaceAvailableWithContext error: %v", err)
				break
			}
		}
		time.Sleep(waiterSleep)
	}
	e.metrics.TimeAWSCall(fullWait, "FullWaitUntilNetworkInterfaceAvailable", err != nil)
	if err != nil {
		e.deleteENI(ctx, eni)
		return "", false, errors.Wrap(err, "failed to WaitUntilNetworkInterfaceAvailableWithContext")
	}

	log.Infof("Successfully created and attached a new eni %s to instance", eni)
	return eni, false, nil
}

func (e *EC2Instance) FreeENI(ctx context.Context, eniID string) error {
	e.lock.Lock()
	defer e.lock.Unlock()

	if e.dirty {
		if err := e.update(ctx); err != nil {
			return errors.Wrapf(err, "failed to update FreeENI eni %s", eniID)
		}
	}

	attachment := aws.String("")
	for _, eni := range e.instance.NetworkInterfaces {
		if eniID == *eni.NetworkInterfaceId {
			attachment = eni.Attachment.AttachmentId
		}
	}

	_, err := e.ec2.DetachNetworkInterfaceWithContext(ctx, &ec2.DetachNetworkInterfaceInput{
		AttachmentId: attachment,
		Force:        aws.Bool(true),
	})
	if err != nil {
		log.Errorf("Failed to detach eni %s %v", eniID, err)
		return errors.Wrap(err, "free eni: failed to detach eni from instance")
	}
	e.dirty = true

	// It may take awhile for EC2-VPC to detach ENI from instance
	// retry maxENIDeleteRetries times with sleep 5 sec between to delete the interface
	// TODO check if can use inbuild waiter in the aws-sdk-go,
	// Example: https://github.com/aws/aws-sdk-go/blob/master/service/ec2/waiters.go#L874
	err = e.deleteENI(ctx, eniID)
	if err != nil {
		return errors.Wrapf(err, "fail to free eni: %s", eniID)
	}

	log.Infof("Successfully freed eni: %s", eniID)
	return nil
}

func (e *EC2Instance) deleteENI(ctx context.Context, eniID string) error {
	log.Debugf("Trying to delete eni: %s", eniID)

	for retry := 0; retry < maxENIDeleteRetries; retry++ {
		start := time.Now()
		_, err := e.ec2.DeleteNetworkInterfaceWithContext(ctx, &ec2.DeleteNetworkInterfaceInput{
			NetworkInterfaceId: aws.String(eniID),
		})
		e.metrics.TimeAWSCall(start, "DeleteNetworkInterfaceWithContext", err != nil)
		if err != nil {
			log.Debugf("Not able to delete eni yet (attempt %d/%d): %v ", retry, maxENIDeleteRetries, err)
		} else {
			log.Infof("Successfully deleted eni: %s", eniID)
			return nil
		}

		time.Sleep(retryDeleteENIInternal)
	}
	return errors.New("unable to delete ENI, giving up")
}

func (e *EC2Instance) GetENIMetadata(ctx context.Context, eniID string) (meta ENIMetadata, err error) {
	e.lock.Lock()
	defer e.lock.Unlock()

	if e.dirty {
		if err := e.update(ctx); err != nil {
			return ENIMetadata{}, errors.Wrapf(err, "failed to update GetENIAddrs eni %s", eniID)
		}
	}
	// TODO(tvi): Dependency inject metadata service.
	sess := session.New()
	svc := ec2metadata.New(sess)

	for _, eni := range e.instance.NetworkInterfaces {
		if eniID == *eni.NetworkInterfaceId {
			for _, ip := range eni.PrivateIpAddresses {
				if aws.BoolValue(ip.Primary) {
					meta.PrimaryIP = aws.StringValue(ip.PrivateIpAddress)
				} else {
					meta.SecondaryIPs = append(meta.SecondaryIPs, aws.StringValue(ip.PrivateIpAddress))
				}
			}

			meta.MAC = aws.StringValue(eni.MacAddress)
			meta.Device = int(aws.Int64Value(eni.Attachment.DeviceIndex))
			cidr, err := svc.GetMetadata(metadataMACPath + meta.MAC + metadataVPCcidr)
			if err != nil {
				return ENIMetadata{}, errors.Wrap(err, "instance metadata: failed to retrieve cidr for mac")
			}
			meta.CIDR = cidr
		}
	}

	if meta.PrimaryIP == "" {
		return ENIMetadata{}, errors.New("ENI not found")
	}
	return
}

func (e *EC2Instance) GetENIs(ctx context.Context) ([]string, error) {
	e.lock.Lock()
	defer e.lock.Unlock()

	if e.dirty {
		if err := e.update(ctx); err != nil {
			return nil, errors.Wrapf(err, "failed to update GetENIs")
		}
	}

	ret := []string{}
	for _, eni := range e.instance.NetworkInterfaces {
		ret = append(ret, aws.StringValue(eni.NetworkInterfaceId))
	}
	return ret, nil
}

func (e *EC2Instance) IsENIReady(ctx context.Context, eniID string) (bool, error) {
	// TODO(tvi): Dependency inject metadata service.
	sess := session.New()
	svc := ec2metadata.New(sess)

	macs, err := svc.GetMetadata(metadataMACPath)
	if err != nil {
		return false, errors.Wrap(err, "instance metadata: failed to retrieve mac")
	}
	for _, mac := range strings.Fields(macs) {
		eni, err := svc.GetMetadata(fmt.Sprintf(metadataENIPath, mac))
		// log.Infof("test '%v' '%v' '%v' '%v'", fmt.Sprintf(metadataENIPath, mac), eni, err, mac)
		if err != nil {
			return false, errors.Wrap(err, "instance metadata: failed to retrieve eni for mac")
		}
		if eni == eniID {
			return true, nil
		}
	}

	return false, nil
}
