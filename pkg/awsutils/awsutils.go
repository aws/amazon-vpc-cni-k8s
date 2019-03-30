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
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	log "github.com/cihub/seelog"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2metadata"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2wrapper"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

const (
	metadataMACPath      = "network/interfaces/macs/"
	metadataAZ           = "placement/availability-zone/"
	metadataLocalIP      = "local-ipv4"
	metadataInstanceID   = "instance-id"
	metadataInstanceType = "instance-type"
	metadataMAC          = "mac"
	metadataSGs          = "/security-group-ids/"
	metadataSubnetID     = "/subnet-id/"
	metadataVPCcidrs     = "/vpc-ipv4-cidr-blocks/"
	metadataVPCcidr      = "/vpc-ipv4-cidr-block/"
	metadataDeviceNum    = "/device-number/"
	metadataInterface    = "/interface-id/"
	metadataSubnetCIDR   = "/subnet-ipv4-cidr-block"
	metadataIPv4s        = "/local-ipv4s"
	maxENIDeleteRetries  = 20
	eniDescriptionPrefix = "aws-K8S-"
	metadataOwnerID      = "/owner-id"
	// AllocENI need to choose a first free device number between 0 and maxENI
	maxENIs           = 128
	clusterNameEnvVar = "CLUSTER_NAME"
	eniNodeTagKey     = "node.k8s.amazonaws.com/instance_id"
	eniClusterTagKey  = "cluster.k8s.amazonaws.com/name"

	retryDeleteENIInternal = 5 * time.Second

	// UnknownInstanceType indicates that the instance type is not yet supported
	UnknownInstanceType = "vpc ip resource(eni ip limit): unknown instance type"
)

// ErrENINotFound is an error when ENI is not found.
var ErrENINotFound = errors.New("ENI is not found")

var (
	awsAPILatency = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "awscni_aws_api_lantency_ms",
			Help: "AWS API call latency in ms",
		},
		[]string{"api", "error"},
	)
	awsAPIErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_aws_api_error_count",
			Help: "The number of times AWS API returns an error",
		},
		[]string{"api", "error"},
	)
	awsUtilsErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_aws_utils_error_count",
			Help: "The number of errors not handled in awsutils library",
		},
		[]string{"fn", "error"},
	)
	prometheusRegistered = false
)

// APIs defines interfaces calls for adding/getting/deleting ENIs/secondary IPs. The APIs are not thread-safe.
type APIs interface {
	// AllocENI creates an ENI and attaches it to the instance
	AllocENI(useCustomCfg bool, sg []*string, subnet string) (eni string, err error)

	// FreeENI detaches ENI interface and deletes it
	FreeENI(eniName string) error

	// GetAttachedENIs retrieves eni information from instance metadata service
	GetAttachedENIs() (eniList []ENIMetadata, err error)

	// DescribeENI returns the IPv4 addresses of ENI interface and ENI attachment ID
	DescribeENI(eniID string) (addrList []*ec2.NetworkInterfacePrivateIpAddress, attachemdID *string, err error)

	// AllocIPAddress allocates an IP address for an ENI
	AllocIPAddress(eniID string) error

	// AllocAllIPAddress allocates all IP addresses available on an ENI
	AllocAllIPAddress(eniID string) error

	// AllocIPAddresses allocates numIPs IP addresses on a ENI
	AllocIPAddresses(eniID string, numIPs int64) error

	// GetVPCIPv4CIDR returns VPC's 1st CIDR
	GetVPCIPv4CIDR() string

	// GetVPCIPv4CIDRs returns VPC's CIDRs
	GetVPCIPv4CIDRs() []*string

	// GetLocalIPv4 returns the primary IP address on the primary ENI interface
	GetLocalIPv4() string

	// GetPrimaryENI returns the primary ENI
	GetPrimaryENI() string

	// GetENIipLimit return IP address limit per ENI based on EC2 instance type
	GetENIipLimit() (int64, error)

	// GetENILimit returns the number of ENIs that can be attached to an instance
	GetENILimit() (int, error)
	
	// GetPrimaryENImac returns the mac address of the primary ENI
	GetPrimaryENImac() string
}

// EC2InstanceMetadataCache caches instance metadata
type EC2InstanceMetadataCache struct {
	// metadata info
	securityGroups   []*string
	subnetID         string
	cidrBlock        string
	localIPv4        string
	instanceID       string
	instanceType     string
	vpcIPv4CIDR      string
	vpcIPv4CIDRs     []*string
	primaryENI       string
	primaryENImac    string
	availabilityZone string
	region           string
	accountID        string

	// dynamic
	currentENIs int

	ec2Metadata ec2metadata.EC2Metadata
	ec2SVC      ec2wrapper.EC2
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

// msSince returns milliseconds since start.
func msSince(start time.Time) float64 {
	return float64(time.Since(start) / time.Millisecond)
}

func prometheusRegister() {
	if !prometheusRegistered {
		prometheus.MustRegister(awsAPILatency)
		prometheus.MustRegister(awsAPIErr)
		prometheus.MustRegister(awsUtilsErr)
		prometheusRegistered = true
	}
}

// New creates an EC2InstanceMetadataCache
func New() (*EC2InstanceMetadataCache, error) {
	// Initializes prometheus metrics
	prometheusRegister()

	cache := &EC2InstanceMetadataCache{}
	cache.ec2Metadata = ec2metadata.New()

	region, err := cache.ec2Metadata.Region()
	if err != nil {
		log.Errorf("Failed to retrieve region data from instance metadata %v", err)
		return nil, errors.Wrap(err, "instance metadata: failed to retrieve region data")
	}
	cache.region = region
	log.Debugf("Discovered region: %s", cache.region)

	sess, err := session.NewSession(
		&aws.Config{Region: aws.String(cache.region),
			MaxRetries: aws.Int(15)})
	if err != nil {
		log.Errorf("Failed to initialize AWS SDK session %v", err)
		return nil, errors.Wrap(err, "instance metadata: failed to initialize AWS SDK session")
	}

	ec2SVC := ec2wrapper.New(sess)
	cache.ec2SVC = ec2SVC
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
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve availability zone data from EC2 metadata service, %v", err)
		return errors.Wrapf(err, "get instance metadata: failed to retrieve availability zone data")
	}
	cache.availabilityZone = az
	log.Debugf("Found availability zone: %s ", cache.availabilityZone)

	// retrieve eth0 local-ipv4
	cache.localIPv4, err = cache.ec2Metadata.GetMetadata(metadataLocalIP)
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve instance's primary ip address data from instance metadata service %v", err)
		return errors.Wrap(err, "get instance metadata: failed to retrieve the instance primary ip address data")
	}
	log.Debugf("Discovered the instance primary ip address: %s", cache.localIPv4)

	// retrieve instance-id
	cache.instanceID, err = cache.ec2Metadata.GetMetadata(metadataInstanceID)
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve instance-id from instance metadata %v", err)
		return errors.Wrap(err, "get instance metadata: failed to retrieve instance-id")
	}
	log.Debugf("Found instance-id: %s ", cache.instanceID)

	// retrieve instance-type
	cache.instanceType, err = cache.ec2Metadata.GetMetadata(metadataInstanceType)
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve instance-type from instance metadata %v", err)
		return errors.Wrap(err, "get instance metadata: failed to retrieve instance-type")
	}
	log.Debugf("Found instance-type: %s ", cache.instanceType)

	// retrieve primary interface's mac
	mac, err := cache.ec2Metadata.GetMetadata(metadataMAC)
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve primary interface MAC address from instance metadata service %v", err)
		return errors.Wrap(err, "get instance metadata: failed to retrieve primary interface MAC address")
	}
	cache.primaryENImac = mac
	log.Debugf("Found primary interface's MAC address: %s", mac)

	err = cache.setPrimaryENI()
	if err != nil {
		return errors.Wrap(err, "get instance metadata: failed to find primary ENI")
	}

	// retrieve security groups
	metadataSGIDs, err := cache.ec2Metadata.GetMetadata(metadataMACPath + mac + metadataSGs)
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve security-group-ids data from instance metadata service, %v", err)
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
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve subnet-ids from instance metadata %v", err)
		return errors.Wrap(err, "get instance metadata: failed to retrieve subnet-ids")
	}
	log.Debugf("Found subnet-id: %s ", cache.subnetID)

	// retrieve vpc-ipv4-cidr-block
	cache.vpcIPv4CIDR, err = cache.ec2Metadata.GetMetadata(metadataMACPath + mac + metadataVPCcidr)
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve vpc-ipv4-cidr-block from instance metadata service")
		return errors.Wrap(err, "get instance metadata: failed to retrieve vpc-ipv4-cidr-block data")
	}
	log.Debugf("Found vpc-ipv4-cidr-block: %s ", cache.vpcIPv4CIDR)

	// retrieve vpc-ipv4-cidr-blocks
	metadataVPCIPv4CIDRs, err := cache.ec2Metadata.GetMetadata(metadataMACPath + mac + metadataVPCcidrs)
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve vpc-ipv4-cidr-blocks from instance metadata service")
		return errors.Wrap(err, "get instance metadata: failed to retrieve vpc-ipv4-cidr-block data")
	}

	vpcIPv4CIDRs := strings.Fields(metadataVPCIPv4CIDRs)

	for _, vpcCIDR := range vpcIPv4CIDRs {
		log.Debugf("Found VPC CIDR: %s", vpcCIDR)
		cache.vpcIPv4CIDRs = append(cache.vpcIPv4CIDRs, aws.String(vpcCIDR))
	}
	return nil
}

func (cache *EC2InstanceMetadataCache) setPrimaryENI() error {
	if cache.primaryENI != "" {
		return nil
	}

	// retrieve number of interfaces
	metadataENImacs, err := cache.ec2Metadata.GetMetadata(metadataMACPath)
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve interfaces data from instance metadata service, %v", err)
		return errors.Wrap(err, "set primary ENI: failed to retrieve interfaces data")
	}
	eniMACs := strings.Fields(metadataENImacs)
	log.Debugf("Discovered %d interfaces.", len(eniMACs))
	cache.currentENIs = len(eniMACs)

	// retrieve the attached ENIs
	for _, eniMAC := range eniMACs {
		// get device-number
		device, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataDeviceNum)
		if err != nil {
			awsAPIErrInc("GetMetadata", err)
			log.Errorf("Failed to retrieve the device-number data of ENI %s,  %v", eniMAC, err)
			return errors.Wrapf(err, "set primary ENI: failed to retrieve the device-number data of ENI %s", eniMAC)
		}

		deviceNum, err := strconv.ParseInt(device, 0, 32)
		if err != nil {
			return errors.Wrapf(err, "set primary ENI: invalid device %s", device)
		}
		log.Debugf("Found device-number: %d ", deviceNum)

		if cache.accountID == "" {
			ownerID, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataOwnerID)
			if err != nil {
				awsAPIErrInc("GetMetadata", err)
				log.Errorf("Failed to retrieve owner ID from instance metadata %v", err)
				return errors.Wrap(err, "set primary ENI: failed to retrieve ownerID")
			}
			log.Debugf("Found account ID: %s", ownerID)
			cache.accountID = ownerID
		}

		eni, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataInterface)
		if err != nil {
			awsAPIErrInc("GetMetadata", err)
			log.Errorf("Failed to retrieve interface-id data from instance metadata %v", err)
			return errors.Wrap(err, "set primary ENI: failed to retrieve interface-id")
		}

		log.Debugf("Found eni: %s ", eni)
		result := strings.Split(eniMAC, "/")
		if cache.primaryENImac == result[0] {
			//primary interface
			cache.primaryENI = eni
			log.Debugf("Found ENI %s is a primary ENI", eni)
			return nil
		}
	}
	return errors.New("set primary ENI: primary ENI not found")
}

// GetAttachedENIs retrieves ENI information from meta data service
func (cache *EC2InstanceMetadataCache) GetAttachedENIs() (eniList []ENIMetadata, err error) {
	// retrieve number of interfaces
	macs, err := cache.ec2Metadata.GetMetadata(metadataMACPath)
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve interfaces data from instance metadata %v", err)
		return nil, errors.Wrap(err, "get attached ENIs: failed to retrieve interfaces data")
	}
	macsStrs := strings.Fields(macs)
	log.Debugf("Total number of interfaces found: %d ", len(macsStrs))
	cache.currentENIs = len(macsStrs)

	var enis []ENIMetadata
	// retrieve the attached ENIs
	for _, macStr := range macsStrs {
		eniMetadata, err := cache.getENIMetadata(macStr)
		if err != nil {
			return nil, errors.Wrapf(err, "get attached ENIs: failed to retrieve ENI metadata for ENI: %s", macStr)
		}
		enis = append(enis, eniMetadata)
	}
	return enis, nil
}

func (cache *EC2InstanceMetadataCache) getENIMetadata(macStr string) (ENIMetadata, error) {
	eniMACList := strings.Split(macStr, "/")
	eniMAC := eniMACList[0]
	log.Debugf("Found ENI mac address : %s", eniMAC)

	eni, deviceNum, err := cache.getENIDeviceNumber(eniMAC)
	if err != nil {
		return ENIMetadata{}, errors.Wrapf(err, "get ENI metadata: failed to retrieve device and ENI from metadata service: %s", eniMAC)
	}
	log.Debugf("Found ENI: %s, MAC %s, device %d", eni, eniMAC, deviceNum)

	localIPv4s, cidr, err := cache.getIPsAndCIDR(eniMAC)
	if err != nil {
		return ENIMetadata{}, errors.Wrapf(err, "get ENI metadata: failed to retrieve IPs and CIDR for ENI: %s", eniMAC)
	}
	return ENIMetadata{
		ENIID:          eni,
		MAC:            eniMAC,
		DeviceNumber:   deviceNum,
		SubnetIPv4CIDR: cidr,
		LocalIPv4s:     localIPv4s}, nil
}

// getIPsAndCIDR return list of IPs, CIDR, error
func (cache *EC2InstanceMetadataCache) getIPsAndCIDR(eniMAC string) ([]string, string, error) {
	start := time.Now()
	cidr, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataSubnetCIDR)
	awsAPILatency.WithLabelValues("GetMetadata", fmt.Sprint(err != nil)).Observe(msSince(start))

	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve subnet-ipv4-cidr-block data from instance metadata %v", err)
		return nil, "", errors.Wrapf(err, "failed to retrieve subnet-ipv4-cidr-block for ENI %s", eniMAC)
	}
	log.Debugf("Found CIDR %s for ENI %s", cidr, eniMAC)

	start = time.Now()
	ipv4s, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataIPv4s)
	awsAPILatency.WithLabelValues("GetMetadata", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve ENI %s local-ipv4s from instance metadata service, %v", eniMAC, err)
		return nil, "", errors.Wrapf(err, "failed to retrieve ENI %s local-ipv4s", eniMAC)
	}

	ipv4Strs := strings.Fields(ipv4s)
	log.Debugf("Found IP addresses %v on ENI %s", ipv4Strs, eniMAC)
	return ipv4Strs, cidr, nil
}

// getENIDeviceNumber returns ENI ID, device number, error
func (cache *EC2InstanceMetadataCache) getENIDeviceNumber(eniMAC string) (string, int64, error) {
	// get device-number
	start := time.Now()
	device, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataDeviceNum)
	awsAPILatency.WithLabelValues("GetMetadata", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve the device-number of ENI %s,  %v", eniMAC, err)
		return "", 0, errors.Wrapf(err, "failed to retrieve device-number for ENI %s",
			eniMAC)
	}

	deviceNum, err := strconv.ParseInt(device, 0, 32)
	if err != nil {
		return "", 0, errors.Wrapf(err, "invalid device %s for ENI %s", device, eniMAC)
	}

	start = time.Now()
	eni, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataInterface)
	awsAPILatency.WithLabelValues("GetMetadata", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve the interface-id data from instance metadata service, %v", err)
		return "", 0, errors.Wrapf(err, "get attached ENIs: failed to retrieve interface-id for ENI %s", eniMAC)
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

	start := time.Now()
	result, err := cache.ec2SVC.DescribeInstances(input)
	awsAPILatency.WithLabelValues("DescribeInstances", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("DescribeInstances", err)
		log.Errorf("awsGetFreeDeviceNumber: Unable to retrieve instance data from EC2 control plane %v", err)
		return 0, errors.Wrap(err,
			"find a free device number for ENI: not able to retrieve instance data from EC2 control plane")
	}

	if len(result.Reservations) != 1 {
		return 0, errors.Errorf("awsGetFreeDeviceNumber: invalid instance id %s", cache.instanceID)
	}

	inst := result.Reservations[0].Instances[0]
	var device [maxENIs]bool
	for _, eni := range inst.NetworkInterfaces {
		if aws.Int64Value(eni.Attachment.DeviceIndex) > maxENIs {
			log.Warnf("The Device Index %d of the attached ENI %s > instance max slot %d",
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
	return 0, errors.New("awsGetFreeDeviceNumber: no available device number")
}

// AllocENI creates an ENI and attaches it to the instance
// returns: newly created ENI ID
func (cache *EC2InstanceMetadataCache) AllocENI(useCustomCfg bool, sg []*string, subnet string) (string, error) {
	eniID, err := cache.createENI(useCustomCfg, sg, subnet)
	if err != nil {
		return "", errors.Wrap(err, "AllocENI: failed to create ENI")
	}

	cache.tagENI(eniID)

	attachmentID, err := cache.attachENI(eniID)
	if err != nil {
		_ = cache.deleteENI(eniID)
		return "", errors.Wrap(err, "AllocENI: error attaching ENI")
	}

	// also change the ENI's attribute so that the ENI will be deleted when the instance is deleted.
	attributeInput := &ec2.ModifyNetworkInterfaceAttributeInput{
		Attachment: &ec2.NetworkInterfaceAttachmentChanges{
			AttachmentId:        aws.String(attachmentID),
			DeleteOnTermination: aws.Bool(true),
		},
		NetworkInterfaceId: aws.String(eniID),
	}

	start := time.Now()
	_, err = cache.ec2SVC.ModifyNetworkInterfaceAttribute(attributeInput)
	awsAPILatency.WithLabelValues("ModifyNetworkInterfaceAttribute", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("ModifyNetworkInterfaceAttribute", err)
		err := cache.deleteENI(eniID)
		if err != nil {
			awsUtilsErrInc("ENICleanupUponModifyNetworkErr", err)
		}
		return "", errors.Wrap(err, "AllocENI: unable to change the ENI's attribute")
	}

	log.Infof("Successfully created and attached a new ENI %s to instance", eniID)
	return eniID, nil
}

// return attachment id, error
func (cache *EC2InstanceMetadataCache) attachENI(eniID string) (string, error) {
	// attach to instance
	freeDevice, err := cache.awsGetFreeDeviceNumber()
	if err != nil {
		return "", errors.Wrap(err, "attachENI: failed to get a free device number")
	}

	attachInput := &ec2.AttachNetworkInterfaceInput{
		DeviceIndex:        aws.Int64(freeDevice),
		InstanceId:         aws.String(cache.instanceID),
		NetworkInterfaceId: aws.String(eniID),
	}
	start := time.Now()
	attachOutput, err := cache.ec2SVC.AttachNetworkInterface(attachInput)
	awsAPILatency.WithLabelValues("AttachNetworkInterface", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("AttachNetworkInterface", err)
		if containsAttachmentLimitExceededError(err) {
			// TODO once reached limit, should stop retrying increasePool
			log.Infof("Exceeded instance ENI attachment limit: %d ", cache.currentENIs)
		}
		log.Errorf("Failed to attach ENI %s: %v", eniID, err)
		return "", errors.Wrap(err, "attachENI: failed to attach ENI")
	}
	return aws.StringValue(attachOutput.AttachmentId), err
}

// return ENI id, error
func (cache *EC2InstanceMetadataCache) createENI(useCustomCfg bool, sg []*string, subnet string) (string, error) {
	eniDescription := eniDescriptionPrefix + cache.instanceID
	var input *ec2.CreateNetworkInterfaceInput

	if useCustomCfg {
		log.Infof("createENI: use customer network config, %v, %s", sg, subnet)
		input = &ec2.CreateNetworkInterfaceInput{
			Description: aws.String(eniDescription),
			Groups:      sg,
			SubnetId:    aws.String(subnet),
		}
	} else {
		log.Infof("createENI: use primary interface's config, %v, %s", cache.securityGroups, cache.subnetID)
		input = &ec2.CreateNetworkInterfaceInput{
			Description: aws.String(eniDescription),
			Groups:      cache.securityGroups,
			SubnetId:    aws.String(cache.subnetID),
		}
	}

	start := time.Now()
	result, err := cache.ec2SVC.CreateNetworkInterface(input)
	awsAPILatency.WithLabelValues("CreateNetworkInterface", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("CreateNetworkInterface", err)
		log.Errorf("Failed to CreateNetworkInterface %v", err)
		return "", errors.Wrap(err, "failed to create network interface")
	}
	log.Infof("Created a new ENI: %s", aws.StringValue(result.NetworkInterface.NetworkInterfaceId))
	return aws.StringValue(result.NetworkInterface.NetworkInterfaceId), nil
}

func (cache *EC2InstanceMetadataCache) tagENI(eniID string) {
	// Tag the ENI with "node.k8s.amazonaws.com/instance_id=<instance_id>"
	tags := []*ec2.Tag{
		{
			Key:   aws.String(eniNodeTagKey),
			Value: aws.String(cache.instanceID),
		},
	}

	// If the CLUSTER_NAME env var is present,
	// tag the ENI with "cluster.k8s.amazonaws.com/name=<cluster_name>"
	clusterName := os.Getenv(clusterNameEnvVar)
	if clusterName != "" {
		tags = append(tags, &ec2.Tag{
			Key:   aws.String(eniClusterTagKey),
			Value: aws.String(clusterName),
		})
	}

	for _, tag := range tags {
		log.Debugf("Trying to tag newly created ENI: key=%s, value=%s", aws.StringValue(tag.Key), aws.StringValue(tag.Value))
	}

	input := &ec2.CreateTagsInput{
		Resources: []*string{
			aws.String(eniID),
		},
		Tags: tags,
	}

	start := time.Now()
	_, err := cache.ec2SVC.CreateTags(input)
	awsAPILatency.WithLabelValues("CreateTags", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("CreateTags", err)
		log.Warnf("Failed to tag the newly created ENI %s: %v", eniID, err)
	} else {
		log.Debugf("Successfully tagged ENI: %s", eniID)
	}
}

//containsAttachmentLimitExceededError returns whether exceeds instance's ENI limit
func containsAttachmentLimitExceededError(err error) bool {
	if aerr, ok := err.(awserr.Error); ok {
		return aerr.Code() == "AttachmentLimitExceeded"
	}
	return false
}

// containsPrivateIPAddressLimitExceededError returns whether exceeds ENI's IP address limit
func containsPrivateIPAddressLimitExceededError(err error) bool {
	if aerr, ok := err.(awserr.Error); ok {
		return aerr.Code() == "PrivateIpAddressLimitExceeded"
	}
	return false
}

func awsAPIErrInc(api string, err error) {
	if aerr, ok := err.(awserr.Error); ok {
		awsAPIErr.With(prometheus.Labels{"api": api, "error": aerr.Code()}).Inc()
	}
}

func awsUtilsErrInc(fn string, err error) {
	awsUtilsErr.With(prometheus.Labels{"fn": fn, "error": err.Error()}).Inc()
}

// FreeENI detaches and deletes the ENI interface
func (cache *EC2InstanceMetadataCache) FreeENI(eniName string) error {
	log.Infof("Trying to free ENI: %s", eniName)

	// Find out attachment
	// TODO: use metadata
	_, attachID, err := cache.DescribeENI(eniName)
	if err != nil {
		if err == ErrENINotFound {
			log.Infof("ENI %s not found. It seems to be already freed", eniName)
			return nil
		}

		awsUtilsErrInc("FreeENIDescribeENIFailed", err)
		log.Errorf("Failed to retrieve ENI %s attachment id: %v", eniName, err)
		return errors.Wrap(err, "FreeENI: failed to retrieve ENI's attachment id")
	}

	log.Debugf("Found ENI %s attachment id: %s ", eniName, aws.StringValue(attachID))

	// Detach it first
	detachInput := &ec2.DetachNetworkInterfaceInput{
		AttachmentId: attachID,
		Force:        aws.Bool(true),
	}

	start := time.Now()
	_, err = cache.ec2SVC.DetachNetworkInterface(detachInput)
	awsAPILatency.WithLabelValues("DetachNetworkInterface", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("DetachNetworkInterface", err)
		log.Errorf("Failed to detach ENI %s %v", eniName, err)
		return errors.Wrap(err, "FreeENI: failed to detach ENI from instance")
	}

	// It may take awhile for EC2-VPC to detach ENI from instance
	// retry maxENIDeleteRetries times with sleep 5 sec between to delete the interface
	// TODO check if can use built-in waiter in the aws-sdk-go,
	// Example: https://github.com/aws/aws-sdk-go/blob/master/service/ec2/waiters.go#L874
	err = cache.deleteENI(eniName)
	if err != nil {
		awsUtilsErrInc("FreeENIDeleteErr", err)
		return errors.Wrapf(err, "FreeENI: failed to free ENI: %s", eniName)
	}

	log.Infof("Successfully freed ENI: %s", eniName)
	return nil
}

func (cache *EC2InstanceMetadataCache) deleteENI(eniName string) error {
	log.Debugf("Trying to delete ENI: %s", eniName)

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
		start := time.Now()
		_, err = cache.ec2SVC.DeleteNetworkInterface(deleteInput)
		awsAPILatency.WithLabelValues("DeleteNetworkInterface", fmt.Sprint(err != nil)).Observe(msSince(start))
		if err == nil {
			log.Infof("Successfully deleted ENI: %s", eniName)
			return nil
		}
		awsAPIErrInc("DeleteNetworkInterface", err)

		log.Debugf("Not able to delete ENI yet (attempt %d/%d): %v ", retry, maxENIDeleteRetries, err)
		time.Sleep(retryDeleteENIInternal)
	}
}

// DescribeENI returns the IPv4 addresses of interface and the attachment id
// return: private IP address, attachment id, error
func (cache *EC2InstanceMetadataCache) DescribeENI(eniID string) ([]*ec2.NetworkInterfacePrivateIpAddress, *string, error) {
	eniIds := make([]*string, 0)
	eniIds = append(eniIds, aws.String(eniID))
	input := &ec2.DescribeNetworkInterfacesInput{ NetworkInterfaceIds: eniIds}

	start := time.Now()
	result, err := cache.ec2SVC.DescribeNetworkInterfaces(input)
	awsAPILatency.WithLabelValues("DescribeNetworkInterfaces", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "InvalidNetworkInterfaceID.NotFound" {
				return nil, nil, ErrENINotFound
			}
		}
		awsAPIErrInc("DescribeNetworkInterfaces", err)
		log.Errorf("Failed to get ENI %s information from EC2 control plane %v", eniID, err)
		return nil, nil, errors.Wrap(err, "failed to describe network interface")
	}
	return result.NetworkInterfaces[0].PrivateIpAddresses, result.NetworkInterfaces[0].Attachment.AttachmentId, nil
}

// AllocIPAddress allocates an IP address for an ENI
func (cache *EC2InstanceMetadataCache) AllocIPAddress(eniID string) error {
	log.Infof("Trying to allocate an IP address on ENI: %s", eniID)

	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String(eniID),
		SecondaryPrivateIpAddressCount: aws.Int64(1),
	}

	start := time.Now()
	output, err := cache.ec2SVC.AssignPrivateIpAddresses(input)
	awsAPILatency.WithLabelValues("AssignPrivateIpAddresses", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("AssignPrivateIpAddresses", err)
		log.Errorf("Failed to allocate a private IP address  %v", err)
		return errors.Wrap(err, "failed to assign private IP addresses")
	}

	log.Infof("Successfully allocated IP address %s on ENI %s", output.String(), eniID)
	return nil
}

// GetENIipLimit return IP address limit per ENI based on EC2 instance type
func (cache *EC2InstanceMetadataCache) GetENIipLimit() (int64, error) {
	ipLimit, ok := InstanceIPsAvailable[cache.instanceType]
	if !ok {
		log.Errorf("Failed to get ENI IP limit due to unknown instance type %s", cache.instanceType)
		return 0, errors.New(UnknownInstanceType)
	}
	return ipLimit - 1, nil
}

// GetENILimit returns the number of ENIs can be attached to an instance
func (cache *EC2InstanceMetadataCache) GetENILimit() (int, error) {
	eniLimit, ok := InstanceENIsAvailable[cache.instanceType]

	if !ok {
		log.Errorf("Failed to get ENI limit due to unknown instance type %s", cache.instanceType)
		return 0, errors.New(UnknownInstanceType)
	}
	return eniLimit, nil
}

// AllocIPAddresses allocates numIPs of IP address on an ENI
func (cache *EC2InstanceMetadataCache) AllocIPAddresses(eniID string, numIPs int64) error {
	var needIPs = int64(numIPs)

	ipLimit, err := cache.GetENIipLimit()
	if err == nil && ipLimit < int64(needIPs) {
		needIPs = ipLimit
	}

	log.Infof("Trying to allocate %d IP address on ENI %s", needIPs, eniID)

	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String(eniID),
		SecondaryPrivateIpAddressCount: aws.Int64(int64(needIPs)),
	}

	start := time.Now()
	_, err = cache.ec2SVC.AssignPrivateIpAddresses(input)
	awsAPILatency.WithLabelValues("AssignPrivateIpAddresses", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("AssignPrivateIpAddresses", err)
		if containsPrivateIPAddressLimitExceededError(err) {
			return nil
		}
		log.Errorf("Failed to allocate a private IP address %v", err)
		return errors.Wrap(err, "allocate IP address: failed to allocate a private IP address")
	}
	return nil
}

// AllocAllIPAddress allocates all IP addresses available on ENI (TODO: Cleanup)
func (cache *EC2InstanceMetadataCache) AllocAllIPAddress(eniID string) error {
	log.Infof("Trying to allocate all available IP addresses on ENI: %s", eniID)

	ipLimit, err := cache.GetENIipLimit()
	if err != nil {
		awsUtilsErrInc("UnknownInstanceType", err)
		// for unknown instance type, will allocate one ip address at a time
		ipLimit = 1

		input := &ec2.AssignPrivateIpAddressesInput{
			NetworkInterfaceId:             aws.String(eniID),
			SecondaryPrivateIpAddressCount: aws.Int64(ipLimit),
		}

		for {
			// until error
			start := time.Now()
			_, err := cache.ec2SVC.AssignPrivateIpAddresses(input)
			awsAPILatency.WithLabelValues("AssignPrivateIpAddresses", fmt.Sprint(err != nil)).Observe(msSince(start))
			if err != nil {
				awsAPIErrInc("AssignPrivateIpAddresses", err)
				if containsPrivateIPAddressLimitExceededError(err) {
					return nil
				}
				log.Errorf("Failed to allocate a private IP address %v", err)
				return errors.Wrap(err, "AllocAllIPAddress: failed to allocate a private IP address")
			}
		}
	} else {
		// for known instance type, will allocate max number ip address for that interface
		input := &ec2.AssignPrivateIpAddressesInput{
			NetworkInterfaceId:             aws.String(eniID),
			SecondaryPrivateIpAddressCount: aws.Int64(ipLimit),
		}

		start := time.Now()
		_, err := cache.ec2SVC.AssignPrivateIpAddresses(input)
		awsAPILatency.WithLabelValues("AssignPrivateIpAddresses", fmt.Sprint(err != nil)).Observe(msSince(start))
		if err != nil {
			awsAPIErrInc("AssignPrivateIpAddresses", err)
			if containsPrivateIPAddressLimitExceededError(err) {
				return nil
			}
			log.Errorf("Failed to allocate a private IP address %v", err)
			return errors.Wrap(err, "AllocAllIPAddress: failed to allocate a private IP address")
		}
	}
	return nil
}

// GetVPCIPv4CIDR returns VPC CIDR
func (cache *EC2InstanceMetadataCache) GetVPCIPv4CIDR() string {
	return cache.vpcIPv4CIDR
}

// GetVPCIPv4CIDRs returns VPC CIDRs
func (cache *EC2InstanceMetadataCache) GetVPCIPv4CIDRs() []*string {
	return cache.vpcIPv4CIDRs
}

// GetLocalIPv4 returns the primary IP address on the primary interface
func (cache *EC2InstanceMetadataCache) GetLocalIPv4() string {
	return cache.localIPv4
}

// GetPrimaryENI returns the primary ENI
func (cache *EC2InstanceMetadataCache) GetPrimaryENI() string {
	return cache.primaryENI
}

// GetPrimaryENImac returns the mac address of primary eni
func (cache *EC2InstanceMetadataCache) GetPrimaryENImac() string {
	return cache.primaryENImac
}
