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
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	metadataSGs          = "/security-group-ids"
	metadataSubnetID     = "/subnet-id"
	metadataVPCv4cidrs   = "/vpc-ipv4-cidr-blocks"
	metadataVPCv6cidrs   = "/vpc-ipv6-cidr-blocks"
	metadataDeviceNum    = "/device-number"
	metadataInterface    = "/interface-id"
	metadataSubnetCIDR   = "/subnet-ipv4-cidr-block"
	metadataIPv4s        = "/local-ipv4s"
	metadataIPv6s        = "/ipv6s"
	maxENIEC2APIRetries  = 12
	maxENIBackoffDelay   = time.Minute
	eniDescriptionPrefix = "aws-K8S-"

	// AllocENI need to choose a first free device number between 0 and maxENI
	// 100 is a hard limit because we use vlanID + 100 for pod networking table names
	maxENIs                 = 100
	clusterNameEnvVar       = "CLUSTER_NAME"
	eniNodeTagKey           = "node.k8s.amazonaws.com/instance_id"
	eniCreatedAtTagKey      = "node.k8s.amazonaws.com/createdAt"
	eniClusterTagKey        = "cluster.k8s.amazonaws.com/name"
	additionalEniTagsEnvVar = "ADDITIONAL_ENI_TAGS"
	reservedTagKeyPrefix    = "k8s.amazonaws.com"
	// UnknownInstanceType indicates that the instance type is not yet supported
	UnknownInstanceType = "vpc ip resource(eni ip limit): unknown instance type"

	// Stagger cleanup start time to avoid calling EC2 too much. Time in seconds.
	eniCleanupStartupDelayMax = 300
	eniDeleteCooldownTime     = (5 * time.Minute)
)

var (
	// ErrENINotFound is an error when ENI is not found.
	ErrENINotFound = errors.New("ENI is not found")
	// ErrAllSecondaryIPsNotFound is returned when not all secondary IPs on an ENI have been assigned
	ErrAllSecondaryIPsNotFound = errors.New("All secondary IPs not found")
	// ErrNoSecondaryIPsFound is returned when not all secondary IPs on an ENI have been assigned
	ErrNoSecondaryIPsFound = errors.New("No secondary IPs have been assigned to this ENI")
	// ErrNoNetworkInterfaces occurs when DescribeNetworkInterfaces(eniID) returns no network interfaces
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

	// GetIPsFromEC2 returns the IPv4 and IPv6 addresses for a given ENI
	GetIPsFromEC2(eniID string) (ipv4 []*ec2.NetworkInterfacePrivateIpAddress, ipv6 []string, err error)

	// DescribeAllENIs calls EC2 and returns the ENIMetadata and a tag map for each ENI
	DescribeAllENIs() (eniMetadata []ENIMetadata, tagMap map[string]TagMap, trunkENI string, err error)

	// AllocIPv4Addresses allocates numIPs IP addresses on a ENI
	AllocIPv4Addresses(eniID string, numIPs int) ([]string, error)

	// AllocIPv6Addresses allocates numIPs IP addresses on a ENI
	AllocIPv6Addresses(eniID string, numIPs int) ([]string, error)

	// DeallocIPv4Addresses deallocates the list of IP addresses from a ENI
	DeallocIPv4Addresses(eniID string, ips []string) error

	// DeallocIPv6Addresses deallocates the list of IP addresses from a ENI
	DeallocIPv6Addresses(eniID string, ips []string) error

	// GetVPCIPv4CIDRs returns VPC's IPv4 CIDRs from instance metadata
	GetVPCIPv4CIDRs() []string

	// GetVPCIPv6CIDRs returns VPC's IPv6 CIDRs from instance metadata
	GetVPCIPv6CIDRs() []string

	// GetLocalIPv4 returns the primary IPv4 address on the primary ENI interface
	GetLocalIPv4() string

	// GetPrimaryENI returns the primary ENI
	GetPrimaryENI() string

	// GetENIIPv4Limit return IP address limit per ENI based on EC2 instance type
	GetENIIPv4Limit() (int, error)

	// GetENILimit returns the number of ENIs that can be attached to an instance
	GetENILimit() (int, error)

	// GetPrimaryENImac returns the mac address of the primary ENI
	GetPrimaryENImac() string

	// SetUnmanagedENIs sets the list of unmanaged ENI IDs
	SetUnmanagedENIs(eniIDs []string)

	// IsUnmanagedENI checks if an ENI is unmanaged
	IsUnmanagedENI(eniID string) bool

	// WaitForENIAndIPsAttached waits until the ENI has been attached and the secondary IPs have been added
	WaitForENIAndIPsAttached(eni string, wantedSecondaryIPs int) (ENIMetadata, error)
}

// EC2InstanceMetadataCache caches instance metadata
type EC2InstanceMetadataCache struct {
	// metadata info
	securityGroups      StringSet
	subnetID            string
	localIPv4           string
	instanceID          string
	instanceType        string
	vpcIPv4CIDRs        StringSet
	vpcIPv6CIDRs        StringSet
	primaryENI          string
	primaryENImac       string
	availabilityZone    string
	region              string
	unmanagedENIs       StringSet
	useCustomNetworking bool

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

	// The IPv4 addresses allocated for the network interface
	IPv4Addresses []*ec2.NetworkInterfacePrivateIpAddress

	// The IPv6 addresses allocated for the network interface
	IPv6Addresses []string
}

// InstanceTypeLimits keeps track of limits for an instance type
type InstanceTypeLimits struct {
	ENILimit  int
	IPv4Limit int
}

// PrimaryIPv4Address returns the primary IP of this node
func (eni ENIMetadata) PrimaryIPv4Address() string {
	for _, addr := range eni.IPv4Addresses {
		if aws.BoolValue(addr.Primary) {
			return aws.StringValue(addr.PrivateIpAddress)
		}
	}
	return ""
}

// TagMap keeps track of the EC2 tags on each ENI
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

//StringSet is a set of strings
type StringSet struct {
	sync.RWMutex
	data sets.String
}

// SortedList returnsa sorted string slice from this set
func (ss *StringSet) SortedList() []string {
	ss.RLock()
	defer ss.RUnlock()
	// sets.String.List() returns a sorted list
	return ss.data.List()
}

// Set sets the string set
func (ss *StringSet) Set(items []string) {
	ss.Lock()
	defer ss.Unlock()
	ss.data = sets.NewString(items...)
}

// Difference compares this StringSet with another
func (ss *StringSet) Difference(other *StringSet) *StringSet {
	ss.RLock()
	other.RLock()
	defer ss.RUnlock()
	defer other.RUnlock()
	//example: s1 = {a1, a2, a3} s2 = {a1, a2, a4, a5} s1.Difference(s2) = {a3} s2.Difference(s1) = {a4, a5}
	return &StringSet{data: ss.data.Difference(other.data)}
}

// Has returns true if the StringSet contains the string
func (ss *StringSet) Has(item string) bool {
	ss.RLock()
	defer ss.RUnlock()
	return ss.data.Has(item)
}

// New creates an EC2InstanceMetadataCache
func New(useCustomNetworking bool) (*EC2InstanceMetadataCache, error) {
	//ctx is passed to initWithEC2Metadata func to cancel spawned go-routines when tests are run
	ctx := context.Background()

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

	cache.useCustomNetworking = useCustomNetworking
	log.Infof("Custom networking %v", cache.useCustomNetworking)

	sess, err := session.NewSession(&aws.Config{Region: aws.String(cache.region), MaxRetries: aws.Int(15)})
	if err != nil {
		log.Errorf("Failed to initialize AWS SDK session %v", err)
		return nil, errors.Wrap(err, "instance metadata: failed to initialize AWS SDK session")
	}

	ec2SVC := ec2wrapper.New(sess)
	cache.ec2SVC = ec2SVC
	err = cache.initWithEC2Metadata(ctx)
	if err != nil {
		return nil, err
	}

	// Clean up leaked ENIs in the background
	go wait.Forever(cache.cleanUpLeakedENIs, time.Hour)

	return cache, nil
}

// InitWithEC2metadata initializes the EC2InstanceMetadataCache with the data retrieved from EC2 metadata service
func (cache *EC2InstanceMetadataCache) initWithEC2Metadata(ctx context.Context) error {
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

	// retrieve sub-id
	cache.subnetID, err = cache.ec2Metadata.GetMetadata(metadataMACPath + mac + metadataSubnetID)
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve subnet-ids from instance metadata %v", err)
		return errors.Wrap(err, "get instance metadata: failed to retrieve subnet-ids")
	}
	log.Debugf("Found subnet-id: %s ", cache.subnetID)

	// retrieve security groups
	err = cache.refreshSGIDs(mac)
	if err != nil {
		return err
	}

	// retrieve VPC IPv4 CIDR blocks
	err = cache.refreshVPCCIDRs(mac)
	if err != nil {
		return err
	}

	// Refresh security groups and VPC CIDR blocks in the background
	// Ignoring errors since we will retry in 30s
	go wait.Forever(func() { _ = cache.refreshSGIDs(mac) }, 30*time.Second)
	go wait.Forever(func() { _ = cache.refreshVPCCIDRs(mac) }, 30*time.Second)

	// We use the ctx here for testing, since we spawn go-routines above which will run forever.
	select {
	case <-ctx.Done():
		return nil
	default:
	}
	return nil
}

// refreshSGIDs retrieves security groups
func (cache *EC2InstanceMetadataCache) refreshSGIDs(mac string) error {
	metadataSGIDs, err := cache.ec2Metadata.GetMetadata(metadataMACPath + mac + metadataSGs)
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve security-group-ids data from instance metadata service")
		return errors.Wrap(err, "get instance metadata: failed to retrieve security-group-ids")
	}

	sgIDs := strings.Fields(metadataSGIDs)

	newSGs := StringSet{}
	newSGs.Set(sgIDs)
	addedSGs := newSGs.Difference(&cache.securityGroups)
	addedSGsCount := 0
	deletedSGs := cache.securityGroups.Difference(&newSGs)
	deletedSGsCount := 0

	for _, sg := range addedSGs.SortedList() {
		log.Infof("Found %s, added to ipamd cache", sg)
		addedSGsCount++
	}
	for _, sg := range deletedSGs.SortedList() {
		log.Infof("Removed %s from ipamd cache", sg)
		deletedSGsCount++
	}
	cache.securityGroups.Set(sgIDs)

	if !cache.useCustomNetworking && (addedSGsCount != 0 || deletedSGsCount != 0) {
		allENIs, err := cache.GetAttachedENIs()
		if err != nil {
			return errors.Wrap(err, "DescribeAllENIs: failed to get local ENI metadata")
		}

		var eniIDs []string
		for _, eni := range allENIs {
			eniIDs = append(eniIDs, eni.ENIID)
		}

		newENIs := StringSet{}
		newENIs.Set(eniIDs)

		filteredENIs := newENIs.Difference(&cache.unmanagedENIs)

		sgIDsPtrs := aws.StringSlice(sgIDs)
		// This will update SG for managed ENIs created by EKS.
		for _, eniID := range filteredENIs.SortedList() {
			log.Debugf("Update ENI %s", eniID)

			attributeInput := &ec2.ModifyNetworkInterfaceAttributeInput{
				Groups:             sgIDsPtrs,
				NetworkInterfaceId: aws.String(eniID),
			}
			start := time.Now()
			_, err = cache.ec2SVC.ModifyNetworkInterfaceAttributeWithContext(context.Background(), attributeInput, userAgent)
			awsAPILatency.WithLabelValues("ModifyNetworkInterfaceAttribute", fmt.Sprint(err != nil)).Observe(msSince(start))
			if err != nil {
				awsAPIErrInc("ModifyNetworkInterfaceAttribute", err)
				return errors.Wrap(err, "refreshSGIDs: unable to update the ENI's SG")
			}
		}
	}
	return nil
}

// refreshVPCCIDRs retrieves VPC IPv4 and IPv6 CIDR blocks and updates the
func (cache *EC2InstanceMetadataCache) refreshVPCCIDRs(mac string) error {
	// Helper struct to update both IPv4 and IPv6
	type setMetadata struct {
		set  *StringSet
		path string
	}
	// TODO: Check which mode is enabled instead of always updating both
	vpcCIDRMetadata := []setMetadata{
		{&cache.vpcIPv4CIDRs, metadataVPCv4cidrs},
		{&cache.vpcIPv6CIDRs, metadataVPCv6cidrs}}
	for _, vpcCIDRSet := range vpcCIDRMetadata {
		metadataVPCCIDRs, err := cache.ec2Metadata.GetMetadata(metadataMACPath + mac + vpcCIDRSet.path)
		if err != nil {
			awsAPIErrInc("GetMetadata", err)
			log.Errorf("Failed to retrieve %s from instance metadata service", vpcCIDRSet.path)
			return errors.Wrap(err, "get instance metadata: failed to retrieve"+vpcCIDRSet.path+"data")
		}

		vpcCIDRs := strings.Fields(metadataVPCCIDRs)

		newVPCCIDRs := StringSet{}
		newVPCCIDRs.Set(vpcCIDRs)
		addedVVPCCIDRs := *newVPCCIDRs.Difference(vpcCIDRSet.set)
		deletedVPCCIDRs := *vpcCIDRSet.set.Difference(&newVPCCIDRs)
		for _, vpcCIDR := range addedVVPCCIDRs.SortedList() {
			log.Infof("Found %s, added to ipamd cache", vpcCIDR)
		}
		for _, vpcCIDR := range deletedVPCCIDRs.SortedList() {
			log.Infof("Removed %s from ipamd cache", vpcCIDR)
		}
		vpcCIDRSet.set.Set(vpcCIDRs)
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

	imdsIPv6s, err := cache.getIPv6s(eniMAC)
	if err != nil {
		return ENIMetadata{}, errors.Wrapf(err, "get ENI metadata: failed to retrieve IPv6 addresses for ENI: %s", eniMAC)
	}

	return ENIMetadata{
		ENIID:          eni,
		MAC:            eniMAC,
		DeviceNumber:   deviceNum,
		SubnetIPv4CIDR: cidr,
		IPv4Addresses:  imdsIPv4s,
		IPv6Addresses:  imdsIPv6s,
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
	log.Debugf("Found IPv4 addresses %v on ENI %s", ipv4Strs, eniMAC)
	ipv4s := make([]*ec2.NetworkInterfacePrivateIpAddress, 0, len(ipv4Strs))
	// network/interfaces/macs/mac/public-ipv4s	The public IP address or Elastic IP addresses associated with the interface.
	// There may be multiple IPv4 addresses on an instance. https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instancedata-data-categories.html
	isFirst := true
	for _, ipv4 := range ipv4Strs {
		// The first IP in the list is always the primary IP of that ENI
		primary := isFirst
		ip := ipv4
		ipv4s = append(ipv4s, &ec2.NetworkInterfacePrivateIpAddress{PrivateIpAddress: &ip, Primary: &primary})
		isFirst = false
	}
	return ipv4s, cidr, nil
}

// getIPv6s returns list of IPv6 IPs
func (cache *EC2InstanceMetadataCache) getIPv6s(eniMAC string) ([]string, error) {
	ip6sAsString, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataIPv6s)
	if containsStatusNotFound(err) {
		// Alas, no IPv6 :(
		ip6sAsString = ""
	} else if err != nil {
		return nil, err
	}

	ip6s := strings.Fields(ip6sAsString)
	log.Debugf("Found IPv6 addresses %v on ENI %s", ip6s, eniMAC)

	return ip6s, nil
}

// getENIDeviceNumber returns ENI ID, device number, error
func (cache *EC2InstanceMetadataCache) getENIDeviceNumber(eniMAC string) (eniID string, deviceNumber int, err error) {
	// get device-number
	start := time.Now()
	device, err := cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataDeviceNum)
	awsAPILatency.WithLabelValues("GetMetadata", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve the device-number of ENI %s, %v", eniMAC, err)
		return "", -1, errors.Wrapf(err, "failed to retrieve device-number for ENI %s", eniMAC)
	}

	deviceNumber, err = strconv.Atoi(device)
	if err != nil {
		return "", -1, errors.Wrapf(err, "invalid device %s for ENI %s", device, eniMAC)
	}

	start = time.Now()
	eniID, err = cache.ec2Metadata.GetMetadata(metadataMACPath + eniMAC + metadataInterface)
	awsAPILatency.WithLabelValues("GetMetadata", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("GetMetadata", err)
		log.Errorf("Failed to retrieve the interface-id data from instance metadata service, %v", err)
		return "", -1, errors.Wrapf(err, "get attached ENIs: failed to retrieve interface-id for ENI %s", eniMAC)
	}

	if cache.primaryENI == eniID {
		log.Debugf("Using device number 0 for primary ENI: %s", eniID)
		if deviceNumber != 0 {
			// Can this even happen? To be backwards compatible, we will always return 0 here and log an error.
			log.Errorf("Device number of primary ENI is %d! It was expected to be 0", deviceNumber)
			return eniID, 0, nil
		}
		return eniID, deviceNumber, nil
	}
	// 0 is reserved for primary ENI
	return eniID, deviceNumber, nil
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
	//Tag on create with create TS
	tags := []*ec2.Tag{
		{
			Key:   aws.String(eniCreatedAtTagKey),
			Value: aws.String(time.Now().Format(time.RFC3339)),
		},
	}
	resourceType := "network-interface"
	tagspec := []*ec2.TagSpecification{
		{
			ResourceType: &resourceType,
			Tags:         tags,
		},
	}
	input := &ec2.CreateNetworkInterfaceInput{
		Description:       aws.String(eniDescription),
		Groups:            aws.StringSlice(cache.securityGroups.SortedList()),
		SubnetId:          aws.String(cache.subnetID),
		TagSpecifications: tagspec,
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

	_ = retry.NWithBackoff(retry.NewSimpleBackoff(500*time.Millisecond, maxBackoffDelay, 0.3, 2), 5, func() error {
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

func containsStatusNotFound(err error) bool {
	if err != nil {
		var aerr awserr.RequestFailure
		if errors.As(err, &aerr) {
			return aerr.StatusCode() == http.StatusNotFound
		}
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
	err = retry.NWithBackoff(retry.NewSimpleBackoff(time.Millisecond*200, maxBackoffDelay, 0.15, 2.0), maxENIEC2APIRetries, func() error {
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
	eniIds := []*string{aws.String(eniID)}
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
	err := retry.NWithBackoff(retry.NewSimpleBackoff(time.Millisecond*500, maxBackoffDelay, 0.15, 2.0), maxENIEC2APIRetries, func() error {
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

// GetIPsFromEC2 calls EC2 and returns a list of all IPv4 and IPv6 addresses on the ENI
func (cache *EC2InstanceMetadataCache) GetIPsFromEC2(eniID string) (ipv4 []*ec2.NetworkInterfacePrivateIpAddress, ipv6 []string, err error) {
	eniIds := make([]*string, 0)
	eniIds = append(eniIds, aws.String(eniID))
	input := &ec2.DescribeNetworkInterfacesInput{NetworkInterfaceIds: eniIds}

	start := time.Now()
	result, err := cache.ec2SVC.DescribeNetworkInterfacesWithContext(context.Background(), input, userAgent)
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

	// Shouldn't happen, but let's be safe
	if len(result.NetworkInterfaces) == 0 {
		return nil, nil, ErrNoNetworkInterfaces
	}
	firstNI := result.NetworkInterfaces[0]

	ipv6Addresses := []string{}
	for _, ipv6Resp := range firstNI.Ipv6Addresses {
		ipv6Addresses = append(ipv6Addresses, aws.StringValue(ipv6Resp.Ipv6Address))
	}
	return firstNI.PrivateIpAddresses, ipv6Addresses, nil
}

// DescribeAllENIs calls EC2 to refrech the ENIMetadata and tags for all attached ENIs
func (cache *EC2InstanceMetadataCache) DescribeAllENIs() (eniMetadata []ENIMetadata, tagMap map[string]TagMap, trunkENI string, err error) {
	// Fetch all local ENI info from metadata
	allENIs, err := cache.GetAttachedENIs()
	if err != nil {
		return nil, nil, "", errors.Wrap(err, "DescribeAllENIs: failed to get local ENI metadata")
	}

	eniMap := make(map[string]ENIMetadata, len(allENIs))
	var eniIDs []string
	for _, eni := range allENIs {
		eniIDs = append(eniIDs, eni.ENIID)
		eniMap[eni.ENIID] = eni
	}

	var ec2Response *ec2.DescribeNetworkInterfacesOutput
	// Try calling EC2 to describe the interfaces.
	for retryCount := 0; retryCount < maxENIEC2APIRetries && len(eniIDs) > 0; retryCount++ {
		input := &ec2.DescribeNetworkInterfacesInput{NetworkInterfaceIds: aws.StringSlice(eniIDs)}
		start := time.Now()
		ec2Response, err = cache.ec2SVC.DescribeNetworkInterfacesWithContext(context.Background(), input, userAgent)
		awsAPILatency.WithLabelValues("DescribeNetworkInterfaces", fmt.Sprint(err != nil)).Observe(msSince(start))
		if err == nil {
			// No error, exit the loop
			break
		}
		awsAPIErrInc("DescribeNetworkInterfaces", err)
		log.Errorf("Failed to call ec2:DescribeNetworkInterfaces for %v: %v", aws.StringValueSlice(input.NetworkInterfaceIds), err)
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "InvalidNetworkInterfaceID.NotFound" {
				badENIID := badENIID(aerr.Message())
				log.Debugf("Could not find interface: %s, ID: %s", aerr.Message(), badENIID)
				// Remove this ENI from the map
				delete(eniMap, badENIID)
				// Remove the failing ENI ID from the EC2 API request and try again
				var tmpENIIDs []string
				for _, eniID := range eniIDs {
					if eniID != badENIID {
						tmpENIIDs = append(tmpENIIDs, eniID)
					}
				}
				eniIDs = tmpENIIDs
				continue
			}
		}
		// For other errors sleep a short while before the next retry
		time.Sleep(time.Duration(retryCount*10) * time.Millisecond)
	}

	if err != nil {
		return nil, nil, "", err
	}

	// Collect the verified ENIs
	var verifiedENIs []ENIMetadata
	for _, eniMetadata := range eniMap {
		verifiedENIs = append(verifiedENIs, eniMetadata)
	}

	// Collect ENI response into ENI metadata and tags.
	tagMap = make(map[string]TagMap, len(ec2Response.NetworkInterfaces))
	for _, ec2res := range ec2Response.NetworkInterfaces {
		if ec2res.Attachment != nil && aws.Int64Value(ec2res.Attachment.DeviceIndex) == 0 && !aws.BoolValue(ec2res.Attachment.DeleteOnTermination) {
			log.Warn("Primary ENI will not get deleted when node terminates because 'delete_on_termination' is set to false")
		}
		eniID := aws.StringValue(ec2res.NetworkInterfaceId)
		eniMetadata := eniMap[eniID]
		interfaceType := aws.StringValue(ec2res.InterfaceType)
		// This assumes we only have one trunk attached to the node..
		if interfaceType == "trunk" {
			trunkENI = eniID
		}
		// Check IPv4 addresses
		logOutOfSyncState(eniID, eniMetadata.IPv4Addresses, ec2res.PrivateIpAddresses)
		tags := getTags(ec2res, eniMetadata.ENIID)
		if len(tags) > 0 {
			tagMap[eniMetadata.ENIID] = tags
		}
	}
	return verifiedENIs, tagMap, trunkENI, nil
}

// getTags collects tags from an EC2 DescribeNetworkInterfaces call
func getTags(ec2res *ec2.NetworkInterface, eniID string) map[string]string {
	tags := make(map[string]string, len(ec2res.TagSet))
	for _, tag := range ec2res.TagSet {
		if tag.Key == nil || tag.Value == nil {
			log.Errorf("nil tag on ENI: %v", eniID)
			continue
		}
		tags[*tag.Key] = *tag.Value
	}
	return tags
}

var eniErrorMessageRegex = regexp.MustCompile("'([a-zA-Z0-9-]+)'")

func badENIID(errMsg string) string {
	found := eniErrorMessageRegex.FindStringSubmatch(errMsg)
	if found == nil || len(found) < 2 {
		return ""
	}
	return found[1]
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

// GetENIIPv4Limit return IP address limit per ENI based on EC2 instance type
func (cache *EC2InstanceMetadataCache) GetENIIPv4Limit() (int, error) {
	eniLimits, ok := InstanceNetworkingLimits[cache.instanceType]
	if !ok {
		log.Errorf("Failed to get ENI IP limit due to unknown instance type %s", cache.instanceType)
		return 0, errors.New(UnknownInstanceType)
	}
	// Subtract one from the IPv4Limit since we don't use the primary IP on each ENI for pods.
	return eniLimits.IPv4Limit - 1, nil
}

// GetENILimit returns the number of ENIs can be attached to an instance
func (cache *EC2InstanceMetadataCache) GetENILimit() (int, error) {
	eniLimits, ok := InstanceNetworkingLimits[cache.instanceType]
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
		eniLimit := int(aws.Int64Value(info.NetworkInfo.MaximumNetworkInterfaces))
		ipv4Limit := int(aws.Int64Value(info.NetworkInfo.Ipv4AddressesPerInterface))
		if instanceType != "" && eniLimit > 0 && ipv4Limit > 0 {
			eniLimits = InstanceTypeLimits{
				ENILimit:  eniLimit,
				IPv4Limit: ipv4Limit,
			}
			InstanceNetworkingLimits[instanceType] = eniLimits
		} else {
			return 0, errors.New(fmt.Sprintf("%s: %s", UnknownInstanceType, cache.instanceType))
		}
	}
	return eniLimits.ENILimit, nil
}

// AllocIPAddresses allocates numIPs of IP address on an ENI
func (cache *EC2InstanceMetadataCache) AllocIPv4Addresses(eniID string, numIPs int) ([]string, error) {
	var needIPs = numIPs

	ipLimit, err := cache.GetENIIPv4Limit()
	if err != nil {
		awsUtilsErrInc("UnknownInstanceType", err)
		return nil, err
	}

	if ipLimit < needIPs {
		needIPs = ipLimit
	}

	// If we don't need any more IPs, exit
	if needIPs < 1 {
		return nil, nil
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
		if containsPrivateIPAddressLimitExceededError(err) {
			log.Debug("AssignPrivateIpAddresses returned PrivateIpAddressLimitExceeded. This can happen if the data store is out of sync." +
				"Returning without an error here since we will verify the actual state by calling EC2 to see what addresses have already assigned to this ENI.")
			return nil, nil
		}
		log.Errorf("Failed to allocate a private IP addresses on ENI %v: %v", eniID, err)
		awsAPIErrInc("AssignPrivateIpAddresses", err)
		return nil, errors.Wrap(err, "allocate IP address: failed to allocate a private IP address")
	}
	if output == nil {
		return nil, errors.New("no IPs returned")
	}
	log.Infof("Allocated %d private IP addresses", len(output.AssignedPrivateIpAddresses))

	ipv4Addresses := make([]string, len(output.AssignedPrivateIpAddresses))
	for i, addr := range output.AssignedPrivateIpAddresses {
		ipv4Addresses[i] = aws.StringValue(addr.PrivateIpAddress)
	}
	return ipv4Addresses, nil
}

// AllocIPv6Addresses allocates numIPs of IP address on an ENI
func (cache *EC2InstanceMetadataCache) AllocIPv6Addresses(eniID string, numIPs int) ([]string, error) {
	var needIPs = numIPs

	// TODO: Separate IPv6 limit
	ipLimit, err := cache.GetENILimit()
	if err != nil {
		awsUtilsErrInc("UnknownInstanceType", err)
		return nil, err
	}

	if ipLimit < needIPs {
		needIPs = ipLimit
	}

	// If we don't need any more IPs, exit
	if needIPs < 1 {
		return nil, nil
	}

	log.Infof("Trying to allocate %d IP addresses on ENI %s", needIPs, eniID)
	input := &ec2.AssignIpv6AddressesInput{
		NetworkInterfaceId: aws.String(eniID),
		Ipv6AddressCount:   aws.Int64(int64(needIPs)),
	}

	start := time.Now()
	output, err := cache.ec2SVC.AssignIpv6AddressesWithContext(context.Background(), input, userAgent)
	awsAPILatency.WithLabelValues("AssignIpv6Addresses", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		// TODO: Check for IPv6 limit exceeded error?
		log.Errorf("Failed to allocate a private IPv6 addresses on ENI %v: %v", eniID, err)
		awsAPIErrInc("AssignIpv6Addresses", err)
		return nil, errors.Wrap(err, "allocate IPv6 address: failed to allocate an IPv6 address")
	}
	if output != nil {
		log.Infof("Allocated %d IPv6 addresses", len(output.AssignedIpv6Addresses))
	}
	return aws.StringValueSlice(output.AssignedIpv6Addresses), nil
}

// WaitForENIAndIPsAttached waits until the ENI has been attached and the secondary IPs have been added
func (cache *EC2InstanceMetadataCache) WaitForENIAndIPsAttached(eni string, wantedSecondaryIPs int) (eniMetadata ENIMetadata, err error) {
	return cache.waitForENIAndIPsAttached(eni, wantedSecondaryIPs, maxENIBackoffDelay)
}

func (cache *EC2InstanceMetadataCache) waitForENIAndIPsAttached(eni string, wantedSecondaryIPs int, maxBackoffDelay time.Duration) (eniMetadata ENIMetadata, err error) {
	start := time.Now()
	attempt := 0
	// Wait until the ENI shows up in the instance metadata service and has at least some secondary IPs
	err = retry.NWithBackoff(retry.NewSimpleBackoff(time.Millisecond*100, maxBackoffDelay, 0.15, 2.0), maxENIEC2APIRetries, func() error {
		attempt++
		enis, err := cache.GetAttachedENIs()
		if err != nil {
			log.Warnf("Failed to increase pool, error trying to discover attached ENIs on attempt %d/%d: %v ", attempt, maxENIEC2APIRetries, err)
			return ErrNoNetworkInterfaces
		}
		// Verify that the ENI we are waiting for is in the returned list
		for _, returnedENI := range enis {
			if eni == returnedENI.ENIID {
				// Check how many Secondary IPs have been attached
				eniIPCount := len(returnedENI.IPv4Addresses)
				if eniIPCount <= 1 {
					log.Debugf("No secondary IPv4 addresses available yet on ENI %s", returnedENI.ENIID)
					return ErrNoSecondaryIPsFound
				}
				// At least some are attached
				eniMetadata = returnedENI
				// ipsToAllocate will be at most 1 less then the IP limit for the ENI because of the primary IP
				if eniIPCount > wantedSecondaryIPs {
					return nil
				}
				return ErrAllSecondaryIPsNotFound
			}
		}
		log.Debugf("Not able to find the right ENI yet (attempt %d/%d)", attempt, maxENIEC2APIRetries)
		return ErrENINotFound
	})
	awsAPILatency.WithLabelValues("waitForENIAndIPsAttached", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		// If we have at least 1 Secondary IP, by now return what we have without an error
		if err == ErrAllSecondaryIPsNotFound {
			if len(eniMetadata.IPv4Addresses) > 1 {
				// We have some Secondary IPs, return the ones we have
				log.Warnf("This ENI only has %d IP addresses, we wanted %d", len(eniMetadata.IPv4Addresses), wantedSecondaryIPs)
				return eniMetadata, nil
			}
		}
		awsAPIErrInc("waitENIAttachedFailedToAssignIPs", err)
		return ENIMetadata{}, errors.New("waitForENIAndIPsAttached: giving up trying to retrieve ENIs from metadata service")
	}
	return eniMetadata, nil
}

// DeallocIPv4Addresses deallocates IPv4 addresses on an ENI
func (cache *EC2InstanceMetadataCache) DeallocIPv4Addresses(eniID string, ips []string) error {
	log.Infof("Trying to unassign the following IPs %s from ENI %s", ips, eniID)
	var ipsInput []*string
	for _, ip := range ips {
		ipsInput = append(ipsInput, aws.String(ip))
	}

	input := &ec2.UnassignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String(eniID),
		PrivateIpAddresses: aws.StringSlice(ips),
	}

	start := time.Now()
	_, err := cache.ec2SVC.UnassignPrivateIpAddressesWithContext(context.Background(), input, userAgent)
	awsAPILatency.WithLabelValues("UnassignPrivateIpAddresses", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("UnassignPrivateIpAddresses", err)
		log.Errorf("Failed to deallocate private IP addresses %v", err)
		return errors.Wrap(err, fmt.Sprintf("deallocate IP addresses: failed to deallocate private IP addresses: %s", ips))
	}
	return nil
}

// DeallocIPv6Addresses deallocates IPv6 addresses on an ENI
func (cache *EC2InstanceMetadataCache) DeallocIPv6Addresses(eniID string, ips []string) error {
	log.Infof("Trying to unassign the following IP6s %s from ENI %s", ips, eniID)
	input := &ec2.UnassignIpv6AddressesInput{
		NetworkInterfaceId: aws.String(eniID),
		Ipv6Addresses:      aws.StringSlice(ips),
	}

	start := time.Now()
	_, err := cache.ec2SVC.UnassignIpv6AddressesWithContext(context.Background(), input, userAgent)
	awsAPILatency.WithLabelValues("UnassignPrivateIpv6Addresses", fmt.Sprint(err != nil)).Observe(msSince(start))
	if err != nil {
		awsAPIErrInc("UnassignPrivateIpv6Addresses", err)
		log.Errorf("Failed to deallocate IPv6 addresses %v", err)
		return errors.Wrap(err, fmt.Sprintf("deallocate IPv6 addresses: failed to deallocate IPv6 addresses: %s", ips))
	}
	return nil
}

func (cache *EC2InstanceMetadataCache) cleanUpLeakedENIs() {
	cache.cleanUpLeakedENIsInternal(time.Duration(rand.Intn(eniCleanupStartupDelayMax)) * time.Second)
}

func (cache *EC2InstanceMetadataCache) cleanUpLeakedENIsInternal(startupDelay time.Duration) {
	rand.Seed(time.Now().UnixNano())
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
			} else {
				log.Debugf("Cleaned up leaked CNI ENI %s", eniID)
			}
		}
	}
}

func (cache *EC2InstanceMetadataCache) tagENIcreateTS(eniID string, maxBackoffDelay time.Duration) {
	// Tag the ENI with "node.k8s.amazonaws.com/createdAt=currentTime"
	tags := []*ec2.Tag{
		{
			Key:   aws.String(eniCreatedAtTagKey),
			Value: aws.String(time.Now().Format(time.RFC3339)),
		},
	}

	log.Debugf("Tag untagged ENI %s: key=%s, value=%s", eniID, aws.StringValue(tags[0].Key), aws.StringValue(tags[0].Value))

	input := &ec2.CreateTagsInput{
		Resources: []*string{
			aws.String(eniID),
		},
		Tags: tags,
	}

	_ = retry.NWithBackoff(retry.NewSimpleBackoff(500*time.Millisecond, maxBackoffDelay, 0.3, 2), 5, func() error {
		start := time.Now()
		_, err := cache.ec2SVC.CreateTagsWithContext(context.Background(), input, userAgent)
		awsAPILatency.WithLabelValues("CreateTags", fmt.Sprint(err != nil)).Observe(msSince(start))
		if err != nil {
			awsAPIErrInc("CreateTags", err)
			log.Warnf("Failed to add tag to ENI %s: %v", eniID, err)
			return err
		}
		log.Debugf("Successfully tagged ENI: %s", eniID)
		return nil
	})
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
		if !strings.HasPrefix(aws.StringValue(networkInterface.Description), eniDescriptionPrefix) {
			continue
		}
		// Check that it's not a newly created ENI
		tags := getTags(networkInterface, aws.StringValue(networkInterface.NetworkInterfaceId))

		if value, ok := tags[eniCreatedAtTagKey]; ok {
			parsedTime, err := time.Parse(time.RFC3339, value)
			if err != nil {
				log.Warnf("ParsedTime format %s is wrong so retagging with current TS", parsedTime)
				cache.tagENIcreateTS(aws.StringValue(networkInterface.NetworkInterfaceId), maxENIBackoffDelay)
			}
			if time.Since(parsedTime) < eniDeleteCooldownTime {
				log.Infof("Found an ENI created less than 5 mins so not cleaning it up")
				continue
			}
			log.Debugf("%v", value)
		} else {
			// Set a time if we didn't find one. This is to prevent accidentally deleting ENIs that are in the
			// process of being attached by CNI versions v1.5.x or earlier.
			cache.tagENIcreateTS(aws.StringValue(networkInterface.NetworkInterfaceId), maxENIBackoffDelay)
			continue
		}
		networkInterfaces = append(networkInterfaces, networkInterface)
	}

	if len(networkInterfaces) < 1 {
		log.Debug("No AWS CNI leaked ENIs found.")
		return nil, nil
	}

	log.Debugf("Found %d leaked ENIs with the AWS CNI tag.", len(networkInterfaces))
	return networkInterfaces, nil
}

// GetVPCIPv4CIDRs returns VPC CIDRs
func (cache *EC2InstanceMetadataCache) GetVPCIPv4CIDRs() []string {
	return cache.vpcIPv4CIDRs.SortedList()
}

// GetVPCIPv6CIDRs returns VPC CIDRs
func (cache *EC2InstanceMetadataCache) GetVPCIPv6CIDRs() []string {
	return cache.vpcIPv6CIDRs.SortedList()
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

//SetUnmanagedENIs Set unmanaged ENI set
func (cache *EC2InstanceMetadataCache) SetUnmanagedENIs(eniIDs []string) {
	cache.unmanagedENIs.Set(eniIDs)
}

//IsUnmanagedENI returns if the eni is unmanaged
func (cache *EC2InstanceMetadataCache) IsUnmanagedENI(eniID string) bool {
	if len(eniID) != 0 {
		return cache.unmanagedENIs.Has(eniID)
	}
	return false
}
