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

package awsutils

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2metadata"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2wrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/retry"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
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
	maxENIDeleteRetries  = 12
	maxENIBackoffDelay   = time.Minute
	eniDescriptionPrefix = "aws-K8S-"
	metadataOwnerID      = "/owner-id"

	// AllocENI need to choose a first free device number between 0 and maxENI
	maxENIs                 = 128
	clusterNameEnvVar       = "CLUSTER_NAME"
	eniNodeTagKey           = "node.k8s.amazonaws.com/instance_id"
	eniClusterTagKey        = "cluster.k8s.amazonaws.com/name"
	additionalEniTagsEnvVar = "ADDITIONAL_ENI_TAGS"
	reservedTagKeyPrefix    = "k8s.amazonaws.com"
	// UnknownInstanceType indicates that the instance type is not yet supported
	UnknownInstanceType = "vpc ip resource(eni ip limit): unknown instance type"

	// Stagger cleanup start time to avoid calling EC2 too much. Time in seconds.
	eniCleanupStartupDelayMax = 300
)

var (
	// ErrENINotFound is an error when ENI is not found.
	ErrENINotFound = errors.New("ENI is not found")
	// ErrNoNetworkInterfaces occurs when
	// DesribeNetworkInterfaces(eniID) returns no network interfaces
	ErrNoNetworkInterfaces = errors.New("No network interfaces found for ENI")
	// Custom user agent
	userAgent = request.WithAppendUserAgent("amazon-vpc-cni-k8s")
)

var log = logger.Get()

var (
	awsAPILatency = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "awscni_aws_api_latency_ms",
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

	// GetIPv4sFromEC2 returns the IPv4 addresses for a given ENI
	GetIPv4sFromEC2(eniID string) (addrList []*ec2.NetworkInterfacePrivateIpAddress, err error)

	// DescribeAllENIs calls EC2 and returns the ENIMetadata and a tag map for each ENI
	DescribeAllENIs() ([]ENIMetadata, map[string]TagMap, error)

	// AllocIPAddress allocates an IP address for an ENI
	AllocIPAddress(eniID string) error

	// AllocIPAddresses allocates numIPs IP addresses on a ENI
	AllocIPAddresses(eniID string, numIPs int) error

	// DeallocIPAddresses deallocates the list of IP addresses from a ENI
	DeallocIPAddresses(eniID string, ips []string) error

	// GetVPCIPv4CIDR returns VPC's 1st CIDR
	GetVPCIPv4CIDR() string

	// GetVPCIPv4CIDRs returns VPC's CIDRs
	GetVPCIPv4CIDRs() []*string

	// GetLocalIPv4 returns the primary IP address on the primary ENI interface
	GetLocalIPv4() string

	// GetPrimaryENI returns the primary ENI
	GetPrimaryENI() string

	// GetENIipLimit return IP address limit per ENI based on EC2 instance type
	GetENIipLimit() (int, error)

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

	ec2Metadata ec2metadata.EC2Metadata
	ec2SVC      ec2wrapper.EC2
}

// ENIMetadata contains information about an ENI
type ENIMetadata struct {
	// ENIID is the id of network interface
	ENIID string

	// MAC is the mac address of network interface
	MAC string

	// DeviceNumber is the  device number of network interface
	DeviceNumber int // 0 means it is primary interface

	// SubnetIPv4CIDR is the ipv4 cider of network interface
	SubnetIPv4CIDR string

	// The ip addresses allocated for the network interface
	IPv4Addresses []*ec2.NetworkInterfacePrivateIpAddress
}

func (eni ENIMetadata) PrimaryIPv4Address() string {
	for _, addr := range eni.IPv4Addresses {
		if aws.BoolValue(addr.Primary) {
			return aws.StringValue(addr.PrivateIpAddress)
		}
	}
	return ""
}

type TagMap map[string]string

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

	// Clean up leaked ENIs in the background
	go wait.Forever(cache.cleanUpLeakedENIs, time.Hour)

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
			log.Debugf("%s is the primary ENI of this instance", eni)
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
	log.Debugf("Found ENI MAC address: %s", eniMAC)

	eni, deviceNum, err := cache.getENIDeviceNumber(eniMAC)
	if err != nil {
		return ENIMetadata{}, errors.Wrapf(err, "get ENI metadata: failed to retrieve device and ENI from metadata service: %s", eniMAC)
	}
	log.Debugf("Found ENI: %s, MAC %s, device %d", eni, eniMAC, deviceNum)

	imdsIPv4s, cidr, err := cache.getIPsAndCIDR(eniMAC)
	if err != nil {
		return ENIMetadata{}, errors.Wrapf(err, "get ENI metadata: failed to retrieve IPs and CIDR for ENI: %s", eniMAC)
	}
	return ENIMetadata{
		ENIID:          eni,
		MAC:            eniMAC,
		DeviceNumber:   deviceNum,
		SubnetIPv4CIDR: cidr,
		IPv4Addresses:  imdsIPv4s,
	}, nil
}

// getIPsAndCIDR return list of IPs, CIDR, error
func (cache *EC2InstanceMetadataCache) getIPsAndCIDR(eniMAC string) ([]*ec2.NetworkInterfacePrivateIpAddress, string, error) {
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
	ipv4sAsString, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataIPv4s)
	awsAPILatency.WithLabelValues("GetMetadata", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve ENI %s local-ipv4s from instance metadata service, %v", eniMAC, err)
		return nil, "", errors.Wrapf(err, "failed to retrieve ENI %s local-ipv4s", eniMAC)
	}

	ipv4Strs := strings.Fields(ipv4sAsString)
	log.Debugf("Found IP addresses %v on ENI %s", ipv4Strs, eniMAC)
	ipv4s := make([]*ec2.NetworkInterfacePrivateIpAddress, 0, len(ipv4Strs))
	// network/interfaces/macs/mac/public-ipv4s	The public IP address or Elastic IP addresses associated with the interface.
	// There may be multiple IPv4 addresses on an instance. https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instancedata-data-categories.html
	isFirst := true
	for _, ipv4 := range ipv4Strs {
		// TODO: Verify that the first IP is always the primary
		primary := isFirst
		ip := ipv4
		ipv4s = append(ipv4s, &ec2.NetworkInterfacePrivateIpAddress{PrivateIpAddress: &ip, Primary: &primary})
		isFirst = false
	}
	return ipv4s, cidr, nil
}

// getENIDeviceNumber returns ENI ID, device number, error
func (cache *EC2InstanceMetadataCache) getENIDeviceNumber(eniMAC string) (string, int, error) {
	// get device-number
	start := time.Now()
	device, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataDeviceNum)
	awsAPILatency.WithLabelValues("GetMetadata", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve the device-number of ENI %s, %v", eniMAC, err)
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
	return eni, int(deviceNum + 1), nil
}

// awsGetFreeDeviceNumber calls EC2 API DescribeInstances to get the next free device index
func (cache *EC2InstanceMetadataCache) awsGetFreeDeviceNumber() (int, error) {
	input := &ec2.DescribeInstancesInput{
		InstanceIds: []*string{aws.String(cache.instanceID)},
	}

	start := time.Now()
	result, err := cache.ec2SVC.DescribeInstancesWithContext(context.Background(), input, userAgent)
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
			return freeDeviceIndex, nil
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

	attachmentID, err := cache.attachENI(eniID)
	if err != nil {
		derr := cache.deleteENI(eniID, maxENIBackoffDelay)
		if derr != nil {
			awsUtilsErrInc("AllocENIDeleteErr", err)
			log.Errorf("Failed to delete newly created untagged ENI! %v", err)
		}
		return "", errors.Wrap(err, "AllocENI: error attaching ENI")
	}

	// Once the ENI is attached, tag it.
	cache.tagENI(eniID, maxENIBackoffDelay)

	// Also change the ENI's attribute so that the ENI will be deleted when the instance is deleted.
	attributeInput := &ec2.ModifyNetworkInterfaceAttributeInput{
		Attachment: &ec2.NetworkInterfaceAttachmentChanges{
			AttachmentId:        aws.String(attachmentID),
			DeleteOnTermination: aws.Bool(true),
		},
		NetworkInterfaceId: aws.String(eniID),
	}

	start := time.Now()
	_, err = cache.ec2SVC.ModifyNetworkInterfaceAttributeWithContext(context.Background(), attributeInput, userAgent)
	awsAPILatency.WithLabelValues("ModifyNetworkInterfaceAttribute", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("ModifyNetworkInterfaceAttribute", err)
		err := cache.FreeENI(eniID)
		if err != nil {
			awsUtilsErrInc("ENICleanupUponModifyNetworkErr", err)
		}
		return "", errors.Wrap(err, "AllocENI: unable to change the ENI's attribute")
	}

	log.Infof("Successfully created and attached a new ENI %s to instance", eniID)
	return eniID, nil
}

//  attachENI calls EC2 API to attach the ENI and returns the attachment id
func (cache *EC2InstanceMetadataCache) attachENI(eniID string) (string, error) {
	// attach to instance
	freeDevice, err := cache.awsGetFreeDeviceNumber()
	if err != nil {
		return "", errors.Wrap(err, "attachENI: failed to get a free device number")
	}

	attachInput := &ec2.AttachNetworkInterfaceInput{
		DeviceIndex:        aws.Int64(int64(freeDevice)),
		InstanceId:         aws.String(cache.instanceID),
		NetworkInterfaceId: aws.String(eniID),
	}
	start := time.Now()
	attachOutput, err := cache.ec2SVC.AttachNetworkInterfaceWithContext(context.Background(), attachInput, userAgent)
	awsAPILatency.WithLabelValues("AttachNetworkInterface", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("AttachNetworkInterface", err)
		log.Errorf("Failed to attach ENI %s: %v", eniID, err)
		return "", errors.Wrap(err, "attachENI: failed to attach ENI")
	}
	return aws.StringValue(attachOutput.AttachmentId), err
}

// return ENI id, error
func (cache *EC2InstanceMetadataCache) createENI(useCustomCfg bool, sg []*string, subnet string) (string, error) {
	eniDescription := eniDescriptionPrefix + cache.instanceID
	input := &ec2.CreateNetworkInterfaceInput{
		Description: aws.String(eniDescription),
		Groups:      cache.securityGroups,
		SubnetId:    aws.String(cache.subnetID),
	}

	if useCustomCfg {
		log.Info("Using a custom network config for the new ENI")
		input.Groups = sg
		input.SubnetId = aws.String(subnet)
	} else {
		log.Info("Using same config as the primary interface for the new ENI")
	}
	var sgs []string
	for i := range input.Groups {
		sgs = append(sgs, *input.Groups[i])
	}
	log.Infof("Creating ENI with security groups: %v in subnet: %s", sgs, *input.SubnetId)
	start := time.Now()
	result, err := cache.ec2SVC.CreateNetworkInterfaceWithContext(context.Background(), input, userAgent)
	awsAPILatency.WithLabelValues("CreateNetworkInterface", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("CreateNetworkInterface", err)
		log.Errorf("Failed to CreateNetworkInterface %v", err)
		return "", errors.Wrap(err, "failed to create network interface")
	}
	log.Infof("Created a new ENI: %s", aws.StringValue(result.NetworkInterface.NetworkInterfaceId))
	return aws.StringValue(result.NetworkInterface.NetworkInterfaceId), nil
}

func (cache *EC2InstanceMetadataCache) tagENI(eniID string, maxBackoffDelay time.Duration) {
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

	//additionalEniTags for adding additional tags on ENI
	additionalEniTags := os.Getenv(additionalEniTagsEnvVar)
	if additionalEniTags != "" {
		tagsMap, err := parseAdditionalEniTagsMap(additionalEniTags)
		if err != nil {
			log.Warnf("Failed to add additional tags to the newly created ENI %s: %v", eniID, err)
		}
		tags = mapToTags(tagsMap, tags)
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

	_ = retry.RetryNWithBackoff(retry.NewSimpleBackoff(500*time.Millisecond, maxBackoffDelay, 0.3, 2), 5, func() error {
		start := time.Now()
		_, err := cache.ec2SVC.CreateTagsWithContext(context.Background(), input, userAgent)
		awsAPILatency.WithLabelValues("CreateTags", fmt.Sprint(err != nil)).Observe(msSince(start))
		if err != nil {
			awsAPIErrInc("CreateTags", err)
			log.Warnf("Failed to tag the newly created ENI %s:", eniID)
			return err
		}
		log.Debugf("Successfully tagged ENI: %s", eniID)
		return nil
	})
}

//parseAdditionalEniTagsMap will create a map for additional tags
func parseAdditionalEniTagsMap(additionalEniTags string) (map[string]string, error) {
	var additionalEniTagsMap map[string]string

	// If duplicate keys exist, the value of the key will be the value of latter key.
	err := json.Unmarshal([]byte(additionalEniTags), &additionalEniTagsMap)
	if err != nil {
		log.Errorf("Invalid format for ADDITIONAL_ENI_TAGS. Expected a json hash: %v", err)
	}
	return additionalEniTagsMap, err
}

// MapToTags converts a map to a slice of tags.
func mapToTags(tagsMap map[string]string, tags []*ec2.Tag) []*ec2.Tag {
	if tagsMap == nil {
		return tags
	}

	for key, value := range tagsMap {
		keyPrefix := reservedTagKeyPrefix
		if strings.Contains(key, keyPrefix) {
			log.Warnf("Additional tags has %s prefix. Ignoring %s tag as it is reserved", keyPrefix, key)
			continue
		}
		tags = append(tags, &ec2.Tag{
			Key:   aws.String(key),
			Value: aws.String(value),
		})
	}
	return tags
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
	return cache.freeENI(eniName, 2*time.Second, maxENIBackoffDelay)
}

func (cache *EC2InstanceMetadataCache) freeENI(eniName string, sleepDelayAfterDetach time.Duration, maxBackoffDelay time.Duration) error {
	log.Infof("Trying to free ENI: %s", eniName)

	// Find out attachment
	attachID, err := cache.getENIAttachmentID(eniName)
	if err != nil {
		if err == ErrENINotFound {
			log.Infof("ENI %s not found. It seems to be already freed", eniName)
			return nil
		}
		awsUtilsErrInc("getENIAttachmentIDFailed", err)
		log.Errorf("Failed to retrieve ENI %s attachment id: %v", eniName, err)
		return errors.Wrap(err, "FreeENI: failed to retrieve ENI's attachment id")
	}
	log.Debugf("Found ENI %s attachment id: %s ", eniName, aws.StringValue(attachID))

	detachInput := &ec2.DetachNetworkInterfaceInput{
		AttachmentId: attachID,
	}

	// Retry detaching the ENI from the instance
	err = retry.RetryNWithBackoff(retry.NewSimpleBackoff(time.Millisecond*200, maxBackoffDelay, 0.15, 2.0), maxENIDeleteRetries, func() error {
		start := time.Now()
		_, ec2Err := cache.ec2SVC.DetachNetworkInterfaceWithContext(context.Background(), detachInput, userAgent)
		awsAPILatency.WithLabelValues("DetachNetworkInterface", fmt.Sprint(ec2Err != nil)).Observe(msSince(start))
		if ec2Err != nil {
			awsAPIErrInc("DetachNetworkInterface", ec2Err)
			log.Errorf("Failed to detach ENI %s %v", eniName, ec2Err)
			return errors.New("unable to detach ENI from EC2 instance, giving up")
		}
		log.Infof("Successfully detached ENI: %s", eniName)
		return nil
	})

	if err != nil {
		log.Errorf("Failed to detach ENI %s %v", eniName, err)
		return err
	}

	// It does take awhile for EC2 to detach ENI from instance, so we wait 2s before trying the delete.
	time.Sleep(sleepDelayAfterDetach)
	err = cache.deleteENI(eniName, maxBackoffDelay)
	if err != nil {
		awsUtilsErrInc("FreeENIDeleteErr", err)
		return errors.Wrapf(err, "FreeENI: failed to free ENI: %s", eniName)
	}

	log.Infof("Successfully freed ENI: %s", eniName)
	return nil
}

// getENIAttachmentID calls EC2 to fetch the attachmentID of a given ENI
func (cache *EC2InstanceMetadataCache) getENIAttachmentID(eniID string) (*string, error) {
	eniIds := make([]*string, 0)
	eniIds = append(eniIds, aws.String(eniID))
	input := &ec2.DescribeNetworkInterfacesInput{NetworkInterfaceIds: eniIds}

	start := time.Now()
	result, err := cache.ec2SVC.DescribeNetworkInterfacesWithContext(context.Background(), input, userAgent)
	awsAPILatency.WithLabelValues("DescribeNetworkInterfaces", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "InvalidNetworkInterfaceID.NotFound" {
				return nil, ErrENINotFound
			}
		}
		awsAPIErrInc("DescribeNetworkInterfaces", err)
		log.Errorf("Failed to get ENI %s information from EC2 control plane %v", eniID, err)
		return nil, errors.Wrap(err, "failed to describe network interface")
	}
	// Shouldn't happen, but let's be safe
	if len(result.NetworkInterfaces) == 0 {
		return nil, ErrNoNetworkInterfaces
	}
	firstNI := result.NetworkInterfaces[0]

	// We cannot assume that the NetworkInterface.Attachment field is a non-nil
	// pointer to a NetworkInterfaceAttachment struct.
	// Ref: https://github.com/aws/amazon-vpc-cni-k8s/issues/914
	var attachID *string
	if firstNI.Attachment != nil {
		attachID = firstNI.Attachment.AttachmentId
	}
	return attachID, nil
}

func (cache *EC2InstanceMetadataCache) deleteENI(eniName string, maxBackoffDelay time.Duration) error {
	log.Debugf("Trying to delete ENI: %s", eniName)
	deleteInput := &ec2.DeleteNetworkInterfaceInput{
		NetworkInterfaceId: aws.String(eniName),
	}
	err := retry.RetryNWithBackoff(retry.NewSimpleBackoff(time.Millisecond*500, maxBackoffDelay, 0.15, 2.0), maxENIDeleteRetries, func() error {
		start := time.Now()
		_, ec2Err := cache.ec2SVC.DeleteNetworkInterfaceWithContext(context.Background(), deleteInput, userAgent)
		awsAPILatency.WithLabelValues("DeleteNetworkInterface", fmt.Sprint(ec2Err != nil)).Observe(msSince(start))
		if ec2Err != nil {
			if aerr, ok := ec2Err.(awserr.Error); ok {
				// If already deleted, we are good
				if aerr.Code() == "InvalidNetworkInterfaceID.NotFound" {
					log.Infof("ENI %s has already been deleted", eniName)
					return nil
				}
			}
			awsAPIErrInc("DeleteNetworkInterface", ec2Err)
			log.Debugf("Not able to delete ENI: %v ", ec2Err)
			return errors.Wrapf(ec2Err, "unable to delete ENI")
		}
		log.Infof("Successfully deleted ENI: %s", eniName)
		return nil
	})
	return err
}

// GetIPv4sFromEC2 calls EC2 and returns a list of all addresses on the ENI
func (cache *EC2InstanceMetadataCache) GetIPv4sFromEC2(eniID string) (addrList []*ec2.NetworkInterfacePrivateIpAddress, err error) {
	eniIds := make([]*string, 0)
	eniIds = append(eniIds, aws.String(eniID))
	input := &ec2.DescribeNetworkInterfacesInput{NetworkInterfaceIds: eniIds}

	start := time.Now()
	result, err := cache.ec2SVC.DescribeNetworkInterfacesWithContext(context.Background(), input, userAgent)
	awsAPILatency.WithLabelValues("DescribeNetworkInterfaces", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "InvalidNetworkInterfaceID.NotFound" {
				return nil, ErrENINotFound
			}
		}
		awsAPIErrInc("DescribeNetworkInterfaces", err)
		log.Errorf("Failed to get ENI %s information from EC2 control plane %v", eniID, err)
		return nil, errors.Wrap(err, "failed to describe network interface")
	}

	// Shouldn't happen, but let's be safe
	if len(result.NetworkInterfaces) == 0 {
		return nil, ErrNoNetworkInterfaces
	}
	firstNI := result.NetworkInterfaces[0]

	return firstNI.PrivateIpAddresses, nil
}

// DescribeAllENIs calls EC2 to refrech the ENIMetadata and tags for all attached ENIs
func (cache *EC2InstanceMetadataCache) DescribeAllENIs() ([]ENIMetadata, map[string]TagMap, error) {
	// Fetch all local ENI info from metadata
	allENIs, err := cache.GetAttachedENIs()
	if err != nil {
		return nil, nil, errors.Wrap(err, "DescribeAllENIs: failed to get local ENI metadata")
	}

	eniMap := make(map[string]ENIMetadata, len(allENIs))
	var eniIDs []string
	for _, eni := range allENIs {
		eniIDs = append(eniIDs, eni.ENIID)
		eniMap[eni.ENIID] = eni
	}
	input := &ec2.DescribeNetworkInterfacesInput{NetworkInterfaceIds: aws.StringSlice(eniIDs)}

	start := time.Now()
	ec2Response, err := cache.ec2SVC.DescribeNetworkInterfacesWithContext(context.Background(), input, userAgent)
	awsAPILatency.WithLabelValues("DescribeNetworkInterfaces", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "InvalidNetworkInterfaceID.NotFound" {
				return nil, nil, ErrENINotFound
			}
		}
		awsAPIErrInc("DescribeNetworkInterfaces", err)
		log.Errorf("Failed to call ec2:DescribeNetworkInterfaces for %v: %v", eniIDs, err)
		return nil, nil, errors.Wrap(err, "failed to describe network interfaces")
	}

	// Collect ENI response into ENI metadata and tags.
	tagMap := make(map[string]TagMap, len(ec2Response.NetworkInterfaces))
	for _, ec2res := range ec2Response.NetworkInterfaces {
		eniID := aws.StringValue(ec2res.NetworkInterfaceId)
		eniMetadata := eniMap[eniID]
		// Check IPv4 addresses
		logOutOfSyncState(eniID, eniMetadata.IPv4Addresses, ec2res.PrivateIpAddresses)
		tags := make(map[string]string, len(ec2res.TagSet))
		for _, tag := range ec2res.TagSet {
			if tag.Key == nil || tag.Value == nil {
				log.Errorf("nil tag on ENI: %v", eniMetadata.ENIID)
				continue
			}
			tags[*tag.Key] = *tag.Value
		}
		if len(tags) > 0 {
			tagMap[eniMetadata.ENIID] = tags
		}
	}
	return allENIs, tagMap, nil
}

// logOutOfSyncState compares the IP and metadata returned by IMDS and the EC2 API DescribeNetworkInterfaces calls
func logOutOfSyncState(eniID string, imdsIPv4s, ec2IPv4s []*ec2.NetworkInterfacePrivateIpAddress) {
	// Comparing the IMDS IPv4 addresses attached to the ENI with the DescribeNetworkInterfaces AWS API call, which
	// technically should be the source of truth and contain the freshest information. Let's just do a quick scan here
	// and output some diagnostic messages if we find stale info in the IMDS result.
	imdsIPv4Set := sets.String{}
	imdsPrimaryIP := ""
	for _, imdsIPv4 := range imdsIPv4s {
		imdsIPv4Set.Insert(aws.StringValue(imdsIPv4.PrivateIpAddress))
		if aws.BoolValue(imdsIPv4.Primary) {
			imdsPrimaryIP = aws.StringValue(imdsIPv4.PrivateIpAddress)
		}
	}
	ec2IPv4Set := sets.String{}
	ec2IPv4PrimaryIP := ""
	for _, privateIPv4 := range ec2IPv4s {
		ec2IPv4Set.Insert(aws.StringValue(privateIPv4.PrivateIpAddress))
		if aws.BoolValue(privateIPv4.Primary) {
			ec2IPv4PrimaryIP = aws.StringValue(privateIPv4.PrivateIpAddress)
		}
	}
	missingIMDS := ec2IPv4Set.Difference(imdsIPv4Set).List()
	missingDNI := imdsIPv4Set.Difference(ec2IPv4Set).List()
	if len(missingIMDS) > 0 {
		strMissing := strings.Join(missingIMDS, ",")
		log.Infof("logOutOfSyncState: DescribeNetworkInterfaces(%s) yielded private IPv4 addresses %s that were not yet found in IMDS.", eniID, strMissing)
	}
	if len(missingDNI) > 0 {
		strMissing := strings.Join(missingDNI, ",")
		log.Infof("logOutOfSyncState: IMDS query yielded stale IPv4 addresses %s that were not found in DescribeNetworkInterfaces(%s).", strMissing, eniID)
	}
	if imdsPrimaryIP != ec2IPv4PrimaryIP {
		log.Infof("logOutOfSyncState: Primary IPs do not mach for %s. IMDS: %s, EC2: %s", eniID, imdsPrimaryIP, ec2IPv4PrimaryIP)
	}
}

// AllocIPAddress allocates an IP address for an ENI
func (cache *EC2InstanceMetadataCache) AllocIPAddress(eniID string) error {
	log.Infof("Trying to allocate an IP address on ENI: %s", eniID)

	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String(eniID),
		SecondaryPrivateIpAddressCount: aws.Int64(1),
	}

	start := time.Now()
	output, err := cache.ec2SVC.AssignPrivateIpAddressesWithContext(context.Background(), input, userAgent)
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
func (cache *EC2InstanceMetadataCache) GetENIipLimit() (int, error) {
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
		// Fetch from EC2 API
		describeInstanceTypesInput := &ec2.DescribeInstanceTypesInput{InstanceTypes: []*string{aws.String(cache.instanceType)}}
		output, err := cache.ec2SVC.DescribeInstanceTypesWithContext(context.Background(), describeInstanceTypesInput, userAgent)
		if err != nil || len(output.InstanceTypes) != 1 {
			log.Errorf("", err)
			return 0, errors.New(fmt.Sprintf("Failed calling DescribeInstanceTypes for `%s`: %v", cache.instanceType, err))
		}
		info := output.InstanceTypes[0]
		// Ignore any missing values
		instanceType := aws.StringValue(info.InstanceType)
		eniLimit = int(aws.Int64Value(info.NetworkInfo.MaximumNetworkInterfaces))
		ipLimit := int(aws.Int64Value(info.NetworkInfo.Ipv4AddressesPerInterface))
		if instanceType != "" && eniLimit > 0 && ipLimit > 0 {
			InstanceENIsAvailable[instanceType] = eniLimit
			InstanceIPsAvailable[instanceType] = ipLimit
		} else {
			return 0, errors.New(fmt.Sprintf("%s: %s", UnknownInstanceType, cache.instanceType))
		}
	}
	return eniLimit, nil
}

// AllocIPAddresses allocates numIPs of IP address on an ENI
func (cache *EC2InstanceMetadataCache) AllocIPAddresses(eniID string, numIPs int) error {
	var needIPs = numIPs

	ipLimit, err := cache.GetENIipLimit()
	if err != nil {
		awsUtilsErrInc("UnknownInstanceType", err)
		return err
	}

	if ipLimit < needIPs {
		needIPs = ipLimit
	}

	// If we don't need any more IPs, exit
	if needIPs < 1 {
		return nil
	}

	log.Infof("Trying to allocate %d IP addresses on ENI %s", needIPs, eniID)
	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String(eniID),
		SecondaryPrivateIpAddressCount: aws.Int64(int64(needIPs)),
	}

	start := time.Now()
	output, err := cache.ec2SVC.AssignPrivateIpAddressesWithContext(context.Background(), input, userAgent)
	awsAPILatency.WithLabelValues("AssignPrivateIpAddresses", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		log.Errorf("Failed to allocate a private IP addresses on ENI %v: %v", eniID, err)
		awsAPIErrInc("AssignPrivateIpAddresses", err)
		if containsPrivateIPAddressLimitExceededError(err) {
			log.Debug("AssignPrivateIpAddresses returned PrivateIpAddressLimitExceeded")
			return nil
		}
		return errors.Wrap(err, "allocate IP address: failed to allocate a private IP address")
	}
	if output != nil {
		log.Infof("Allocated %d private IP addresses", len(output.AssignedPrivateIpAddresses))
	}
	return nil
}

// DeallocIPAddresses allocates numIPs of IP address on an ENI
func (cache *EC2InstanceMetadataCache) DeallocIPAddresses(eniID string, ips []string) error {
	log.Infof("Trying to unassign the following IPs %s from ENI %s", ips, eniID)
	var ipsInput []*string
	for _, ip := range ips {
		ipsInput = append(ipsInput, aws.String(ip))
	}

	input := &ec2.UnassignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String(eniID),
		PrivateIpAddresses: ipsInput,
	}

	start := time.Now()
	_, err := cache.ec2SVC.UnassignPrivateIpAddressesWithContext(context.Background(), input, userAgent)
	awsAPILatency.WithLabelValues("UnassignPrivateIpAddresses", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("UnassignPrivateIpAddresses", err)
		log.Errorf("Failed to deallocate a private IP address %v", err)
		return errors.Wrap(err, fmt.Sprintf("deallocate IP addresses: failed to deallocate private IP addresses: %s", ips))
	}
	return nil
}

func (cache *EC2InstanceMetadataCache) cleanUpLeakedENIs() {
	rand.Seed(time.Now().UnixNano())
	startupDelay := time.Duration(rand.Intn(eniCleanupStartupDelayMax)) * time.Second
	log.Infof("Will attempt to clean up AWS CNI leaked ENIs after waiting %s.", startupDelay)
	time.Sleep(startupDelay)

	log.Debug("Checking for leaked AWS CNI ENIs.")
	networkInterfaces, err := cache.getFilteredListOfNetworkInterfaces()
	if err != nil {
		log.Warnf("Unable to get leaked ENIs: %v", err)
	} else {
		// Clean up all the leaked ones we found
		for _, networkInterface := range networkInterfaces {
			eniID := aws.StringValue(networkInterface.NetworkInterfaceId)
			err = cache.deleteENI(eniID, maxENIBackoffDelay)
			if err != nil {
				awsUtilsErrInc("cleanUpLeakedENIDeleteErr", err)
				log.Warnf("Failed to clean up leaked ENI %s: %v", eniID, err)
			}
		}
	}
}

// getFilteredListOfNetworkInterfaces calls DescribeNetworkInterfaces to get all available ENIs that were allocated by
// the AWS CNI plugin, but were not deleted.
func (cache *EC2InstanceMetadataCache) getFilteredListOfNetworkInterfaces() ([]*ec2.NetworkInterface, error) {
	// The tag key has to be "node.k8s.amazonaws.com/instance_id"
	tagFilter := &ec2.Filter{
		Name: aws.String("tag-key"),
		Values: []*string{
			aws.String(eniNodeTagKey),
		},
	}
	// Only fetch "available" ENIs.
	statusFilter := &ec2.Filter{
		Name: aws.String("status"),
		Values: []*string{
			aws.String("available"),
		},
	}

	input := &ec2.DescribeNetworkInterfacesInput{
		Filters: []*ec2.Filter{tagFilter, statusFilter},
	}
	result, err := cache.ec2SVC.DescribeNetworkInterfacesWithContext(context.Background(), input, userAgent)
	if err != nil {
		return nil, errors.Wrap(err, "awsutils: unable to obtain filtered list of network interfaces")
	}

	networkInterfaces := make([]*ec2.NetworkInterface, 0)
	for _, networkInterface := range result.NetworkInterfaces {
		// Verify the description starts with "aws-K8S-"
		if strings.HasPrefix(aws.StringValue(networkInterface.Description), eniDescriptionPrefix) {
			networkInterfaces = append(networkInterfaces, networkInterface)
		}
	}

	if len(networkInterfaces) < 1 {
		log.Debug("No AWS CNI leaked ENIs found.")
		return nil, nil
	}

	log.Debugf("Found %d available instances with the AWS CNI tag.", len(networkInterfaces))
	return networkInterfaces, nil
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
