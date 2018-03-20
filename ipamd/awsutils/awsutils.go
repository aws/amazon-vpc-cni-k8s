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

package awsutils

import (
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	log "github.com/cihub/seelog"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2metadata"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2wrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/resourcegroupstaggingapiwrapper"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/resourcegroupstaggingapi"
)

const (
	metadataMACPath      = "network/interfaces/macs/"
	metadataAZ           = "placement/availability-zone/"
	metadataLocalIP      = "local-ipv4"
	metadataInstanceID   = "instance-id"
	metadataMAC          = "mac"
	metadataSGs          = "/security-group-ids/"
	metadataSubnetID     = "/subnet-id/"
	metadataVPCcidr      = "/vpc-ipv4-cidr-block/"
	metadataDeviceNum    = "/device-number/"
	metadataInterface    = "/interface-id/"
	metadataSubnetCIDR   = "/subnet-ipv4-cidr-block"
	metadataIPv4s        = "/local-ipv4s"
	maxENIDeleteRetries  = 5
	eniDescriptionPrefix = "aws-K8S-"
	metadataOwnerID      = "/owner-id"
	// AllocENI need to choose a first free device number between 0 and maxENI
	maxENIs   = 128
	eniTagKey = "k8s-eni-key"

	retryDeleteENIInternal = 5 * time.Second
)

// APIs defines interfaces calls for adding/getting/deleting ENIs/secondary IPs. The APIs are not thread-safe.
type APIs interface {
	// AllocENI creates an eni and attaches it to instance
	AllocENI() (eni string, err error)

	// FreeENI detaches eni interface and deletes it
	FreeENI(eniName string) error

	// GetAttachedENIs retrieves eni information from instance metadata service
	GetAttachedENIs() (eniList []ENIMetadata, err error)

	// DescribeENI returns the IPv4 addresses of eni interface and eni attachment id
	DescribeENI(eniID string) (addrList []*ec2.NetworkInterfacePrivateIpAddress, attachemdID *string, err error)

	// AllocIPAddress allocates an ip address for an eni
	AllocIPAddress(eniID string) error

	// AllocAllIPAddress allocates all ip addresses available on an eni
	AllocAllIPAddress(eniID string) error

	// GetVPCIPv4CIDR returns vpc's cidr
	GetVPCIPv4CIDR() string

	// GetLocalIPv4 returns the primary IP address on the primary eni interface
	GetLocalIPv4() string

	// GetPrimaryENI returns the primary eni
	GetPrimaryENI() string
}

// EC2InstanceMetadataCache caches instance metadata
type EC2InstanceMetadataCache struct {
	// metadata info
	securityGroups   []*string
	subnetID         string
	cidrBlock        string
	localIPv4        string
	instanceID       string
	vpcIPv4CIDR      string
	primaryENI       string
	primaryENImac    string
	availabilityZone string
	region           string
	accountID        string

	// dynamic
	currentENIs int

	ec2Metadata ec2metadata.EC2Metadata
	ec2SVC      ec2wrapper.EC2
	tagSVC      resourcegroupstaggingapiwrapper.ResourceGroupsTaggingAPI
}

// ENIMetadata contains ENI information retrieved from EC2 meta data service
type ENIMetadata struct {
	// ENIID is the id of network interface
	ENIID string

	// MAC is the mac address of network interface
	MAC string

	// DeviceNumber is the  device number of network interface
	DeviceNumber int64 // 0 means it is primary interface

	// SubnetIPv4CIDR is the ipv4 cider of network interface
	SubnetIPv4CIDR string

	// The ip addresses allocated for the network interface
	LocalIPv4s []string
}

// New creates an EC2InstanceMetadataCache
func New() (*EC2InstanceMetadataCache, error) {
	cache := &EC2InstanceMetadataCache{}
	cache.ec2Metadata = ec2metadata.New()

	region, err := cache.ec2Metadata.Region()
	if err != nil {
		log.Errorf("Failed to retrieve region data from instance metadata %v", err)
		return nil, errors.Wrap(err, "instance metadata: failed to retrieve region data")
	}
	cache.region = region
	log.Debugf("Discovered region: %s", cache.region)

	sess, err := session.NewSession(&aws.Config{Region: aws.String(cache.region)})
	if err != nil {
		log.Errorf("Failed to initialize AWS SDK session %v", err)
		return nil, errors.Wrap(err, "instance metadata: failed to initialize AWS SDK session")
	}

	ec2SVC := ec2wrapper.New(sess)
	cache.ec2SVC = ec2SVC

	tagSVC := resourcegroupstaggingapiwrapper.New(sess)
	cache.tagSVC = tagSVC

	err = cache.initWithEC2Metadata()
	if err != nil {
		return nil, err
	}

	return cache, nil
}

// InitWithEC2metadata initializes the EC2InstanceMetadataCache with the data retrieved from EC2 metadata service
func (cache *EC2InstanceMetadataCache) initWithEC2Metadata() error {
	// retrieve availability-zone
	az, err := cache.ec2Metadata.GetMetadata(metadataAZ)
	if err != nil {
		log.Errorf("Failed to retrieve availability zone data from EC2 metadata service, %v", err)
		return errors.Wrapf(err, "get instance metadata: failed to retrieve availability zone data")
	}
	cache.availabilityZone = az
	log.Debugf("Found avalability zone: %s ", cache.availabilityZone)

	// retrieve eth0 local-ipv4
	cache.localIPv4, err = cache.ec2Metadata.GetMetadata(metadataLocalIP)
	if err != nil {
		log.Errorf("Failed to retrieve instance's primary ip address data from instance metadata service %v", err)
		return errors.Wrap(err, "get instance metadata: failed to retrieve the instance primary ip address data")
	}
	log.Debugf("Discovered the instance primary ip address: %s", cache.localIPv4)

	// retrieve instance-id
	cache.instanceID, err = cache.ec2Metadata.GetMetadata(metadataInstanceID)
	if err != nil {
		log.Errorf("Failed to retrieve instance-id from instance metadata %v", err)
		return errors.Wrap(err, "get instance metadata: failed to retrieve instance-id")
	}
	log.Debugf("Found instance-id: %s ", cache.instanceID)

	// retrieve primary interface's mac
	mac, err := cache.ec2Metadata.GetMetadata(metadataMAC)
	if err != nil {
		log.Errorf("Failed to retrieve primary interface mac address from instance metadata service %v", err)
		return errors.Wrap(err, "get instance metadata: failed to retrieve primary interface mac addess")
	}
	cache.primaryENImac = mac
	log.Debugf("Found primary interface's mac address: %s", mac)

	err = cache.setPrimaryENI()
	if err != nil {
		return errors.Wrap(err, "get instance metadata: failed to find primary eni")
	}

	// retrieve security groups
	metadataSGIDs, err := cache.ec2Metadata.GetMetadata(metadataMACPath + mac + metadataSGs)
	if err != nil {
		log.Errorf("Failed to retrieve security-group-ids data from instance metadata service,  %v", err)
		return errors.Wrap(err, "get instance metadata: failed to retrieve security-group-ids")
	}
	sgIDs := strings.Fields(metadataSGIDs)

	for _, sgID := range sgIDs {
		log.Debugf("Found security-group id: %s", sgID)
		cache.securityGroups = append(cache.securityGroups, aws.String(sgID))
	}

	// retrieve sub-id
	cache.subnetID, err = cache.ec2Metadata.GetMetadata(metadataMACPath + mac + metadataSubnetID)
	if err != nil {
		log.Errorf("Failed to retrieve subnet-ids from instance metadata %v", err)
		return errors.Wrap(err, "get instance metadata: failed to retrieve subnet-ids")
	}
	log.Debugf("Found subnet-id: %s ", cache.subnetID)

	// retrieve vpc-ipv4-cidr-block
	cache.vpcIPv4CIDR, err = cache.ec2Metadata.GetMetadata(metadataMACPath + mac + metadataVPCcidr)
	if err != nil {
		log.Errorf("Failed to retrieve vpc-ipv4-cidr-block from instance metadata service")
		return errors.Wrap(err, "get instance metadata: failed to retrieve vpc-ipv4-cidr-block data")
	}
	log.Debugf("Found vpc-ipv4-cidr-block: %s ", cache.vpcIPv4CIDR)

	return nil
}

func (cache *EC2InstanceMetadataCache) setPrimaryENI() error {
	if cache.primaryENI != "" {
		return nil
	}

	// retrieve number of interfaces
	metadataENImacs, err := cache.ec2Metadata.GetMetadata(metadataMACPath)
	if err != nil {
		log.Errorf("Failed to retrieve interfaces data from instance metadata service , %v", err)
		return errors.Wrap(err, "set primary eni: failed to retrieve interfaces data")
	}
	eniMACs := strings.Fields(metadataENImacs)
	log.Debugf("Discovered %d interfaces.", len(eniMACs))
	cache.currentENIs = len(eniMACs)

	// retrieve the attached ENIs
	for _, eniMAC := range eniMACs {
		// get device-number
		device, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataDeviceNum)
		if err != nil {
			log.Errorf("Failed to retrieve the device-number data of eni %s,  %v", eniMAC, err)
			return errors.Wrapf(err, "set primary eni: failed to retrieve the device-number data of eni %s", eniMAC)
		}

		deviceNum, err := strconv.ParseInt(device, 0, 32)
		if err != nil {
			return errors.Wrapf(err, "set primary eni: invalid device %s", device)
		}
		log.Debugf("Found device-number: %d ", deviceNum)

		if cache.accountID == "" {
			ownerID, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataOwnerID)
			if err != nil {
				log.Errorf("Failed to retrieve owner ID from instance metadata %v", err)
				return errors.Wrap(err, "set primary eni: failed to retrive ownerID")
			}
			log.Debugf("Found account ID: %s", ownerID)
			cache.accountID = ownerID
		}

		eni, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataInterface)
		if err != nil {
			log.Errorf("Failed to retrieve interface-id data from instance metadata %v", err)
			return errors.Wrap(err, "set primary eni: failed to retrieve interface-id")
		}

		log.Debugf("Found eni: %s ", eni)
		result := strings.Split(eniMAC, "/")
		if cache.primaryENImac == result[0] {
			//primary interface
			cache.primaryENI = eni
			log.Debugf("Found eni %s is a primary eni", eni)
			return nil
		}

	}
	return errors.New("set primary eni: primary eni not found")
}

// GetAttachedENIs retrieves ENI information from meta data service
func (cache *EC2InstanceMetadataCache) GetAttachedENIs() (eniList []ENIMetadata, err error) {
	// retrieve number of interfaces
	macs, err := cache.ec2Metadata.GetMetadata(metadataMACPath)
	if err != nil {
		log.Errorf("Failed to retrieve interfaces data from instance metadata %v", err)
		return nil, errors.Wrap(err, "get attached enis: failed to retrieve interfaces data")
	}
	macsStrs := strings.Fields(macs)
	log.Debugf("Total number of interfaces found: %d ", len(macsStrs))
	cache.currentENIs = len(macsStrs)

	var enis []ENIMetadata
	// retrieve the attached ENIs
	for _, macStr := range macsStrs {
		eniMetadata, err := cache.getENIMetadata(macStr)
		if err != nil {
			return nil, errors.Wrapf(err, "get attached enis: failed to retrieve eni metadata for eni: %s", macStr)
		}

		enis = append(enis, eniMetadata)
	}

	return enis, nil
}

func (cache *EC2InstanceMetadataCache) getENIMetadata(macStr string) (ENIMetadata, error) {
	eniMACList := strings.Split(macStr, "/")
	eniMAC := eniMACList[0]
	log.Debugf("Found eni mac address : %s", eniMAC)

	eni, deviceNum, err := cache.getENIDeviceNumber(eniMAC)
	if err != nil {
		return ENIMetadata{}, errors.Wrapf(err, "get eni metadata: failed to retrieve device and eni from metadata service: %s", eniMAC)
	}

	log.Debugf("Found eni: %s, mac %s, device %d", eni, eniMAC, deviceNum)

	localIPv4s, cidr, err := cache.getIPsAndCIDR(eniMAC)
	if err != nil {
		return ENIMetadata{}, errors.Wrapf(err, "get eni metadata: failed to retrieves IPs and CIDR for eni: %s", eniMAC)
	}

	return ENIMetadata{ENIID: eni,
		MAC:            eniMAC,
		DeviceNumber:   deviceNum,
		SubnetIPv4CIDR: cidr,
		LocalIPv4s:     localIPv4s}, nil
}

// return list of IPs, CIDR, error
func (cache *EC2InstanceMetadataCache) getIPsAndCIDR(eniMAC string) ([]string, string, error) {
	cidr, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataSubnetCIDR)
	if err != nil {
		log.Errorf("Failed to retrieve subnet-ipv4-cidr-block data from instance metadata %v", err)
		return nil, "", errors.Wrapf(err,
			"failed to retrieve subnet-ipv4-cidr-block for eni %s", eniMAC)
	}

	log.Debugf("Found cidr %s for eni %s", cidr, eniMAC)

	ipv4s, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataIPv4s)
	if err != nil {
		log.Errorf("Failed to retrieve eni %s local-ipv4s from instance metadata service, %v", eniMAC, err)
		return nil, "", errors.Wrapf(err, "failed to retrieve eni %s local-ipv4s", eniMAC)
	}

	ipv4Strs := strings.Fields(ipv4s)
	log.Debugf("Found ip addresses %v on eni %s", ipv4Strs, eniMAC)

	return ipv4Strs, cidr, nil
}

// returns eni id, device number, error
func (cache *EC2InstanceMetadataCache) getENIDeviceNumber(eniMAC string) (string, int64, error) {
	// get device-number
	device, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataDeviceNum)
	if err != nil {
		log.Errorf("Failed to retrieve the device-number of eni %s,  %v", eniMAC, err)
		return "", 0, errors.Wrapf(err, "failed to retrieve device-number for eni %s",
			eniMAC)
	}

	deviceNum, err := strconv.ParseInt(device, 0, 32)
	if err != nil {
		return "", 0, errors.Wrapf(err, "invalid device %s for eni %s", device, eniMAC)
	}

	eni, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataInterface)
	if err != nil {
		log.Errorf("Failed to retrieve the interface-id data from instance metadata service,  %v", err)
		return "", 0, errors.Wrapf(err, "get attached enis: failed to retrieve interface-id for eni %s",
			eniMAC)
	}

	if cache.primaryENI == eni {
		log.Debugf("Using device number 0 for primary eni: %s", eni)
		return eni, 0, nil
	}

	// 0 is reserved for primary ENI, the rest of them has to +1 to avoid collision at 0
	return eni, deviceNum + 1, nil
}

func (cache *EC2InstanceMetadataCache) awsGetFreeDeviceNumber() (int64, error) {
	input := &ec2.DescribeInstancesInput{
		InstanceIds: []*string{aws.String(cache.instanceID)},
	}

	result, err := cache.ec2SVC.DescribeInstances(input)
	if err != nil {
		log.Errorf("Unable to retrieve instance data from EC2 control plane %v", err)
		return 0, errors.Wrap(err,
			"find a free device number for eni: not able to retrieve instance data from EC2 control plane")
	}

	if len(result.Reservations) != 1 {
		return 0, errors.Errorf("find a free device number: invalid instance id %s", cache.instanceID)
	}

	inst := result.Reservations[0].Instances[0]

	var device [maxENIs]bool

	for _, eni := range inst.NetworkInterfaces {
		if aws.Int64Value(eni.Attachment.DeviceIndex) > maxENIs {
			log.Warnf("The Device Index %d of the attached eni %s > instance max slot %d",
				aws.Int64Value(eni.Attachment.DeviceIndex), aws.StringValue(eni.NetworkInterfaceId),
				maxENIs)
		} else {
			log.Debugf("Discovered device number is used: %d", aws.Int64Value(eni.Attachment.DeviceIndex))
			device[aws.Int64Value(eni.Attachment.DeviceIndex)] = true
		}
	}

	for freeDeviceIndex := 0; freeDeviceIndex < maxENIs; freeDeviceIndex++ {
		if !device[freeDeviceIndex] {
			log.Debugf("Found a free device number: %d", freeDeviceIndex)
			return int64(freeDeviceIndex), nil
		}
	}

	return 0, errors.New("allocate eni: no available device number")

}

// AllocENI creates an eni and attach it to the instance
// returns: newly created eni id
func (cache *EC2InstanceMetadataCache) AllocENI() (string, error) {
	eniID, err := cache.createENI()
	if err != nil {
		return "", errors.Wrap(err, "allocate eni: failed to create eni")
	}

	cache.tagENI(eniID)

	attachmentID, err := cache.attachENI(eniID)
	if err != nil {
		cache.deleteENI(eniID)
		return "", errors.Wrap(err, "allocate eni: error attaching eni")
	}

	// also change the eni's attribute so that eni will be delete when instance is delete.
	attributeInput := &ec2.ModifyNetworkInterfaceAttributeInput{
		Attachment: &ec2.NetworkInterfaceAttachmentChanges{
			AttachmentId:        aws.String(attachmentID),
			DeleteOnTermination: aws.Bool(true),
		},
		NetworkInterfaceId: aws.String(eniID),
	}

	_, err = cache.ec2SVC.ModifyNetworkInterfaceAttribute(attributeInput)
	if err != nil {
		cache.deleteENI(eniID)
		return "", errors.Wrap(err, "allocate eni: unable changing the eni's attribute")
	}

	log.Infof("Successfully created and attached a new eni %s to instance", eniID)
	return eniID, nil
}

// return attachment id, error
func (cache *EC2InstanceMetadataCache) attachENI(eniID string) (string, error) {
	// attach to instance
	freeDevice, err := cache.awsGetFreeDeviceNumber()
	if err != nil {
		return "", errors.Wrap(err, "attach eni: failed to get a free device number")
	}

	attachInput := &ec2.AttachNetworkInterfaceInput{
		DeviceIndex:        aws.Int64(freeDevice),
		InstanceId:         aws.String(cache.instanceID),
		NetworkInterfaceId: aws.String(eniID),
	}
	attachOutput, err := cache.ec2SVC.AttachNetworkInterface(attachInput)

	if err != nil {
		if containsAttachmentLimitExceededError(err) {
			// TODO once reached limit, should stop retrying increasePool
			log.Infof("Exceeded instance eni attachment limit: %d ", cache.currentENIs)
		}
		log.Errorf("Failed to attach eni %s: %v", eniID, err)
		return "", errors.Wrap(err, "failed to attach eni")
	}

	return aws.StringValue(attachOutput.AttachmentId), err
}

// return eni id , error
func (cache *EC2InstanceMetadataCache) createENI() (string, error) {
	eniDescription := eniDescriptionPrefix + cache.instanceID

	input := &ec2.CreateNetworkInterfaceInput{
		Description: aws.String(eniDescription),
		Groups:      cache.securityGroups,
		SubnetId:    aws.String(cache.subnetID),
	}

	result, err := cache.ec2SVC.CreateNetworkInterface(input)
	if err != nil {
		log.Errorf("Failed to CreateNetworkInterface %v", err)
		return "", errors.Wrap(err, "failed to create network interface")
	}

	log.Infof("Created a new eni: %s", aws.StringValue(result.NetworkInterface.NetworkInterfaceId))
	return aws.StringValue(result.NetworkInterface.NetworkInterfaceId), nil
}

func (cache *EC2InstanceMetadataCache) tagENI(eniID string) {
	tagSvc := cache.tagSVC
	arns := make([]*string, 0)

	eniARN := &arn.ARN{
		Partition: "aws",
		Service:   "ec2",
		Region:    cache.region,
		AccountID: cache.accountID,
		Resource:  "network-interface/" + eniID}
	arnString := eniARN.String()
	arns = append(arns, aws.String(arnString))

	tags := make(map[string]*string)

	tagValue := cache.instanceID
	tags[eniTagKey] = aws.String(tagValue)
	log.Debugf("Trying to tag newly created eni: keys=%s, value=%s", eniTagKey, tagValue)

	tagInput := &resourcegroupstaggingapi.TagResourcesInput{
		ResourceARNList: arns,
		Tags:            tags,
	}

	_, err := tagSvc.TagResources(tagInput)
	if err != nil {
		log.Warnf("Fail to tag the newly created eni %s  %v", eniID, err)
	} else {
		log.Debugf("Tag the newly created eni with arn: %s", arnString)
	}
}

//containsAttachmentLimitExceededError returns whether exceeds instance's ENI limit
func containsAttachmentLimitExceededError(err error) bool {
	if aerr, ok := err.(awserr.Error); ok {
		return aerr.Code() == "AttachmentLimitExceeded"
	}
	return false
}

// containsPrivateIPAddressLimitExceededError returns whether exceeds eni's IP address limit
func containsPrivateIPAddressLimitExceededError(err error) bool {
	if aerr, ok := err.(awserr.Error); ok {
		return aerr.Code() == "PrivateIpAddressLimitExceeded"
	}
	return false
}

// FreeENI detachs ENI interface and delete ENI interface
func (cache *EC2InstanceMetadataCache) FreeENI(eniName string) error {
	log.Infof("Trying to free eni: %s", eniName)

	// find out attachment
	//TODO: use metadata
	_, attachID, err := cache.DescribeENI(eniName)
	if err != nil {
		log.Errorf("Failed to retrive eni %s attachment id %d", eniName, err)
		return errors.Wrap(err, "free eni: failed to retrieve eni's attachment id")
	}

	log.Debugf("Found eni %s attachment id: %s ", eniName, aws.StringValue(attachID))

	// detach it first
	v := true
	detachInput := &ec2.DetachNetworkInterfaceInput{
		AttachmentId: attachID,
		Force:        aws.Bool(v),
	}

	_, err = cache.ec2SVC.DetachNetworkInterface(detachInput)
	if err != nil {
		log.Errorf("Failed to detach eni %s %v", eniName, err)
		return errors.Wrap(err, "free eni: failed to detach eni from instance")
	}

	// It may take awhile for EC2-VPC to detach ENI from instance
	// retry maxENIDeleteRetries times with sleep 5 sec between to delete the interface
	// TODO check if can use inbuild waiter in the aws-sdk-go,
	// Example: https://github.com/aws/aws-sdk-go/blob/master/service/ec2/waiters.go#L874
	err = cache.deleteENI(eniName)
	if err != nil {
		return errors.Wrapf(err, "fail to free eni: %s", eniName)
	}

	log.Infof("Successfully freed eni: %s", eniName)

	return nil
}

func (cache *EC2InstanceMetadataCache) deleteENI(eniName string) error {
	log.Debugf("Trying to delete eni: %s", eniName)

	retry := 0
	var err error
	for {
		retry++
		if retry > maxENIDeleteRetries {
			return errors.New("unable to delete ENI, giving up")
		}

		deleteInput := &ec2.DeleteNetworkInterfaceInput{
			NetworkInterfaceId: aws.String(eniName),
		}

		_, err = cache.ec2SVC.DeleteNetworkInterface(deleteInput)
		if err == nil {
			log.Infof("Successfully deleted eni: %s", eniName)
			return nil
		}

		log.Debugf("Not able to delete eni yet (attempt %d/%d): %v ", retry, maxENIDeleteRetries, err)
		time.Sleep(retryDeleteENIInternal)
	}

}

// DescribeENI returns the IPv4 addresses of interface and the attachment id
// return: private IP address, attachment id, error
func (cache *EC2InstanceMetadataCache) DescribeENI(eniID string) ([]*ec2.NetworkInterfacePrivateIpAddress, *string, error) {
	eniIds := make([]*string, 0)

	eniIds = append(eniIds, aws.String(eniID))
	input := &ec2.DescribeNetworkInterfacesInput{
		NetworkInterfaceIds: eniIds}

	result, err := cache.ec2SVC.DescribeNetworkInterfaces(input)
	if err != nil {
		log.Errorf("Failed to get eni %s information from EC2 control plane %v", eniID, err)
		return nil, nil, errors.Wrap(err, "failed to describe network interface")
	}

	return result.NetworkInterfaces[0].PrivateIpAddresses, result.NetworkInterfaces[0].Attachment.AttachmentId, nil
}

// AllocIPAddress allocates an IP address for an ENI
func (cache *EC2InstanceMetadataCache) AllocIPAddress(eniID string) error {
	log.Infof("Trying to allocate an ip address on eni: %s", eniID)

	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String(eniID),
		SecondaryPrivateIpAddressCount: aws.Int64(1),
	}

	output, err := cache.ec2SVC.AssignPrivateIpAddresses(input)
	if err != nil {
		log.Errorf("Failed to allocate a private IP address  %v", err)
		return errors.Wrap(err, "failed to assign private ip addresses")
	}

	log.Infof("Successfully allocated ip addess %s on eni %s", output.String(), eniID)

	return nil
}

// AllocAllIPAddress allocates all IP addresses available on eni
func (cache *EC2InstanceMetadataCache) AllocAllIPAddress(eniID string) error {
	log.Infof("Trying to allocate all available ip addresses on eni: %s", eniID)

	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String(eniID),
		SecondaryPrivateIpAddressCount: aws.Int64(1),
	}

	for {
		// until error
		_, err := cache.ec2SVC.AssignPrivateIpAddresses(input)
		if err != nil {
			if containsPrivateIPAddressLimitExceededError(err) {
				return nil
			}
			log.Errorf("Failed to allocate a private IP address %v", err)
			return errors.Wrap(err, "allocate ip address: failed to allocate a private IP address")
		}
	}
}

// GetVPCIPv4CIDR returns VPC CIDR
func (cache *EC2InstanceMetadataCache) GetVPCIPv4CIDR() string {
	return cache.vpcIPv4CIDR
}

// GetLocalIPv4 returns the primary IP address on the primary interface
func (cache *EC2InstanceMetadataCache) GetLocalIPv4() string {
	return cache.localIPv4
}

// GetPrimaryENI returns the primary ENI
func (cache *EC2InstanceMetadataCache) GetPrimaryENI() string {
	return cache.primaryENI
}
