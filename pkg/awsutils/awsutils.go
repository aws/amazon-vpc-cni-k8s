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

// Package awsutils is a utility package for calling EC2 or IMDS
package awsutils

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils/awssession"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2wrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/eventrecorder"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/retry"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/vpc"
	"github.com/aws/amazon-vpc-cni-k8s/utils/prometheusmetrics"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
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
	subnetDiscoveryTagKey   = "kubernetes.io/role/cni"
	// UnknownInstanceType indicates that the instance type is not yet supported
	UnknownInstanceType = "vpc ip resource(eni ip limit): unknown instance type"

	// Stagger cleanup start time to avoid calling EC2 too much. Time in seconds.
	eniCleanupStartupDelayMax = 300
	eniDeleteCooldownTime     = 5 * time.Minute

	// the default page size when paginating the DescribeNetworkInterfaces call
	describeENIPageSize = 1000
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
)

var log = logger.Get()

// APIs defines interfaces calls for adding/getting/deleting ENIs/secondary IPs. The APIs are not thread-safe.
type APIs interface {
	// AllocENI creates an ENI and attaches it to the instance
	AllocENI(useCustomCfg bool, sg []*string, eniCfgSubnet string, numIPs int) (eni string, err error)

	// FreeENI detaches ENI interface and deletes it
	FreeENI(eniName string) error

	// TagENI Tags ENI with current tags to contain expected tags.
	TagENI(eniID string, currentTags map[string]string) error

	// GetAttachedENIs retrieves eni information from instance metadata service
	GetAttachedENIs() (eniList []ENIMetadata, err error)

	// GetIPv4sFromEC2 returns the IPv4 addresses for a given ENI
	GetIPv4sFromEC2(eniID string) (addrList []*ec2.NetworkInterfacePrivateIpAddress, err error)

	// GetIPv4PrefixesFromEC2 returns the IPv4 prefixes for a given ENI
	GetIPv4PrefixesFromEC2(eniID string) (addrList []*ec2.Ipv4PrefixSpecification, err error)

	// GetIPv6PrefixesFromEC2 returns the IPv6 prefixes for a given ENI
	GetIPv6PrefixesFromEC2(eniID string) (addrList []*ec2.Ipv6PrefixSpecification, err error)

	// DescribeAllENIs calls EC2 and returns a fully populated DescribeAllENIsResult struct and an error
	DescribeAllENIs() (DescribeAllENIsResult, error)

	// AllocIPAddress allocates an IP address for an ENI
	AllocIPAddress(eniID string) error

	// AllocIPAddresses allocates numIPs IP addresses on a ENI
	AllocIPAddresses(eniID string, numIPs int) (*ec2.AssignPrivateIpAddressesOutput, error)

	// DeallocIPAddresses deallocates the list of IP addresses from a ENI
	DeallocIPAddresses(eniID string, ips []string) error

	// DeallocPrefixAddresses deallocates the list of IP addresses from a ENI
	DeallocPrefixAddresses(eniID string, ips []string) error

	//AllocIPv6Prefixes allocates IPv6 prefixes to the ENI passed in
	AllocIPv6Prefixes(eniID string) ([]*string, error)

	// GetVPCIPv4CIDRs returns VPC's IPv4 CIDRs from instance metadata
	GetVPCIPv4CIDRs() ([]string, error)

	// GetLocalIPv4 returns the primary IPv4 address on the primary ENI interface
	GetLocalIPv4() net.IP

	// GetVPCIPv6CIDRs returns VPC's IPv6 CIDRs from instance metadata
	GetVPCIPv6CIDRs() ([]string, error)

	// GetPrimaryENI returns the primary ENI
	GetPrimaryENI() string

	// GetENIIPv4Limit return IP address limit per ENI based on EC2 instance type
	GetENIIPv4Limit() int

	// GetENILimit returns the number of ENIs that can be attached to an instance
	GetENILimit() int

	// GetNetworkCards returns the network cards the instance has
	GetNetworkCards() []vpc.NetworkCard

	// GetPrimaryENImac returns the mac address of the primary ENI
	GetPrimaryENImac() string

	// SetUnmanagedENIs sets the list of unmanaged ENI IDs
	SetUnmanagedENIs(eniIDs []string)

	// IsUnmanagedENI checks if an ENI is unmanaged
	IsUnmanagedENI(eniID string) bool

	// WaitForENIAndIPsAttached waits until the ENI has been attached and the secondary IPs have been added
	WaitForENIAndIPsAttached(eni string, wantedSecondaryIPs int) (ENIMetadata, error)

	//SetMultiCardENIs ENI
	SetMultiCardENIs(eniID []string) error

	//IsMultiCardENI
	IsMultiCardENI(eniID string) bool

	//IsPrimaryENI
	IsPrimaryENI(eniID string) bool

	//RefreshSGIDs
	RefreshSGIDs(mac string) error

	//GetInstanceHypervisorFamily returns the hypervisor family for the instance
	GetInstanceHypervisorFamily() string

	//GetInstanceType returns the EC2 instance type
	GetInstanceType() string

	//Update cached prefix delegation flag
	InitCachedPrefixDelegation(bool)

	// GetInstanceID returns the instance ID
	GetInstanceID() string

	// FetchInstanceTypeLimits Verify if the InstanceNetworkingLimits has the ENI limits else make EC2 call to fill cache.
	FetchInstanceTypeLimits() error

	IsPrefixDelegationSupported() bool
}

// EC2InstanceMetadataCache caches instance metadata
type EC2InstanceMetadataCache struct {
	// metadata info
	securityGroups   StringSet
	subnetID         string
	localIPv4        net.IP
	v4Enabled        bool
	v6Enabled        bool
	instanceID       string
	instanceType     string
	primaryENI       string
	primaryENImac    string
	availabilityZone string
	region           string
	vpcID            string

	unmanagedENIs          StringSet
	useCustomNetworking    bool
	multiCardENIs          StringSet
	useSubnetDiscovery     bool
	enablePrefixDelegation bool

	clusterName       string
	additionalENITags map[string]string

	imds   TypedIMDS
	ec2SVC ec2wrapper.EC2
}

// ENIMetadata contains information about an ENI
type ENIMetadata struct {
	// ENIID is the id of network interface
	ENIID string

	// MAC is the mac address of network interface
	MAC string

	// DeviceNumber is the  device number of network interface
	DeviceNumber int // 0 means it is primary interface

	// SubnetIPv4CIDR is the IPv4 CIDR of network interface
	SubnetIPv4CIDR string

	// SubnetIPv6CIDR is the IPv6 CIDR of network interface
	SubnetIPv6CIDR string

	// The ip addresses allocated for the network interface
	IPv4Addresses []*ec2.NetworkInterfacePrivateIpAddress

	// IPv4 Prefixes allocated for the network interface
	IPv4Prefixes []*ec2.Ipv4PrefixSpecification

	// IPv6 addresses allocated for the network interface
	IPv6Addresses []*ec2.NetworkInterfaceIpv6Address

	// IPv6 Prefixes allocated for the network interface
	IPv6Prefixes []*ec2.Ipv6PrefixSpecification
}

// PrimaryIPv4Address returns the primary IPv4 address of this node
func (eni ENIMetadata) PrimaryIPv4Address() string {
	for _, addr := range eni.IPv4Addresses {
		if aws.BoolValue(addr.Primary) {
			return aws.StringValue(addr.PrivateIpAddress)
		}
	}
	return ""
}

// PrimaryIPv6Address returns the primary IPv6 address of this node
func (eni ENIMetadata) PrimaryIPv6Address() string {
	for _, addr := range eni.IPv6Addresses {
		if addr.Ipv6Address != nil {
			return aws.StringValue(addr.Ipv6Address)
		}
	}
	return ""
}

// TagMap keeps track of the EC2 tags on each ENI
type TagMap map[string]string

// DescribeAllENIsResult contains the fully
type DescribeAllENIsResult struct {
	ENIMetadata     []ENIMetadata
	TagMap          map[string]TagMap
	TrunkENI        string
	EFAENIs         map[string]bool
	MultiCardENIIDs []string
}

// msSince returns milliseconds since start.
func msSince(start time.Time) float64 {
	return float64(time.Since(start) / time.Millisecond)
}

// StringSet is a set of strings
type StringSet struct {
	sync.RWMutex
	data sets.String
}

// SortedList returns a sorted string slice from this set
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

type instrumentedIMDS struct {
	EC2MetadataIface
}

func awsReqStatus(err error) string {
	if err == nil {
		return "200"
	}
	var aerr awserr.RequestFailure
	if errors.As(err, &aerr) {
		return fmt.Sprint(aerr.StatusCode())
	}
	return "" // Unknown HTTP status code
}

func (i instrumentedIMDS) GetMetadataWithContext(ctx context.Context, p string) (string, error) {
	start := time.Now()
	result, err := i.EC2MetadataIface.GetMetadataWithContext(ctx, p)
	duration := msSince(start)

	prometheusmetrics.AwsAPILatency.WithLabelValues("GetMetadata", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(duration)

	if err != nil {
		return "", newIMDSRequestError(p, err)
	}
	return result, nil
}

// New creates an EC2InstanceMetadataCache
func New(useSubnetDiscovery, useCustomNetworking, disableLeakedENICleanup, v4Enabled, v6Enabled bool) (*EC2InstanceMetadataCache, error) {
	// ctx is passed to initWithEC2Metadata func to cancel spawned go-routines when tests are run
	ctx := context.Background()

	sess := awssession.New()
	ec2Metadata := ec2metadata.New(sess)
	cache := &EC2InstanceMetadataCache{}
	cache.imds = TypedIMDS{instrumentedIMDS{ec2Metadata}}
	cache.clusterName = os.Getenv(clusterNameEnvVar)
	cache.additionalENITags = loadAdditionalENITags()

	region, err := ec2Metadata.Region()
	if err != nil {
		log.Errorf("Failed to retrieve region data from instance metadata %v", err)
		return nil, errors.Wrap(err, "instance metadata: failed to retrieve region data")
	}
	cache.region = region
	log.Debugf("Discovered region: %s", cache.region)
	cache.useCustomNetworking = useCustomNetworking
	log.Infof("Custom networking enabled %v", cache.useCustomNetworking)
	cache.useSubnetDiscovery = useSubnetDiscovery
	log.Infof("Subnet discovery enabled %v", cache.useSubnetDiscovery)
	cache.v4Enabled = v4Enabled
	cache.v6Enabled = v6Enabled

	awsCfg := aws.NewConfig().WithRegion(region)
	sess = sess.Copy(awsCfg)
	ec2SVC := ec2wrapper.New(sess)
	cache.ec2SVC = ec2SVC
	err = cache.initWithEC2Metadata(ctx)
	if err != nil {
		return nil, err
	}

	// Clean up leaked ENIs in the background
	if !disableLeakedENICleanup {
		go wait.Forever(cache.cleanUpLeakedENIs, time.Hour)
	}
	return cache, nil
}

func (cache *EC2InstanceMetadataCache) InitCachedPrefixDelegation(enablePrefixDelegation bool) {
	cache.enablePrefixDelegation = enablePrefixDelegation
	log.Infof("Prefix Delegation enabled %v", cache.enablePrefixDelegation)
}

// InitWithEC2metadata initializes the EC2InstanceMetadataCache with the data retrieved from EC2 metadata service
func (cache *EC2InstanceMetadataCache) initWithEC2Metadata(ctx context.Context) error {
	var err error
	// retrieve availability-zone
	cache.availabilityZone, err = cache.imds.GetAZ(ctx)
	if err != nil {
		awsAPIErrInc("GetAZ", err)
		return err
	}
	log.Debugf("Found availability zone: %s ", cache.availabilityZone)

	// retrieve primary interface local-ipv4
	cache.localIPv4, err = cache.imds.GetLocalIPv4(ctx)
	if err != nil {
		awsAPIErrInc("GetLocalIPv4", err)
		return err
	}
	log.Debugf("Discovered the instance primary IPv4 address: %s", cache.localIPv4)

	// retrieve instance-id
	cache.instanceID, err = cache.imds.GetInstanceID(ctx)
	if err != nil {
		awsAPIErrInc("GetInstanceID", err)
		return err
	}
	log.Debugf("Found instance-id: %s ", cache.instanceID)

	// retrieve instance-type
	cache.instanceType, err = cache.imds.GetInstanceType(ctx)
	if err != nil {
		awsAPIErrInc("GetInstanceType", err)
		return err
	}
	log.Debugf("Found instance-type: %s ", cache.instanceType)

	// retrieve primary interface's mac
	mac, err := cache.imds.GetMAC(ctx)
	if err != nil {
		awsAPIErrInc("GetMAC", err)
		return err
	}
	cache.primaryENImac = mac
	log.Debugf("Found primary interface's MAC address: %s", mac)

	cache.primaryENI, err = cache.imds.GetInterfaceID(ctx, mac)
	if err != nil {
		awsAPIErrInc("GetInterfaceID", err)
		return errors.Wrap(err, "get instance metadata: failed to find primary ENI")
	}
	log.Debugf("%s is the primary ENI of this instance", cache.primaryENI)

	// retrieve subnet-id
	cache.subnetID, err = cache.imds.GetSubnetID(ctx, mac)
	if err != nil {
		awsAPIErrInc("GetSubnetID", err)
		return err
	}
	log.Debugf("Found subnet-id: %s ", cache.subnetID)

	// retrieve vpc-id
	cache.vpcID, err = cache.imds.GetVpcID(ctx, mac)
	if err != nil {
		awsAPIErrInc("GetVpcID", err)
		return err
	}
	log.Debugf("Found vpc-id: %s ", cache.vpcID)

	// We use the ctx here for testing, since we spawn go-routines above which will run forever.
	select {
	case <-ctx.Done():
		return nil
	default:
	}
	return nil
}

// RefreshSGIDs retrieves security groups
func (cache *EC2InstanceMetadataCache) RefreshSGIDs(mac string) error {
	ctx := context.TODO()

	sgIDs, err := cache.imds.GetSecurityGroupIDs(ctx, mac)
	if err != nil {
		awsAPIErrInc("GetSecurityGroupIDs", err)
		return err
	}

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

		tempfilteredENIs := newENIs.Difference(&cache.multiCardENIs)
		filteredENIs := tempfilteredENIs.Difference(&cache.unmanagedENIs)

		sgIDsPtrs := aws.StringSlice(sgIDs)
		// This will update SG for managed ENIs created by EKS.
		for _, eniID := range filteredENIs.SortedList() {
			log.Debugf("Update ENI %s", eniID)

			attributeInput := &ec2.ModifyNetworkInterfaceAttributeInput{
				Groups:             sgIDsPtrs,
				NetworkInterfaceId: aws.String(eniID),
			}
			start := time.Now()
			_, err = cache.ec2SVC.ModifyNetworkInterfaceAttributeWithContext(context.Background(), attributeInput)
			prometheusmetrics.Ec2ApiReq.WithLabelValues("ModifyNetworkInterfaceAttribute").Inc()
			prometheusmetrics.AwsAPILatency.WithLabelValues("ModifyNetworkInterfaceAttribute", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					if aerr.Code() == "InvalidNetworkInterfaceID.NotFound" {
						awsAPIErrInc("IMDSMetaDataOutOfSync", err)
					}
				}
				checkAPIErrorAndBroadcastEvent(err, "ec2:ModifyNetworkInterfaceAttribute")
				awsAPIErrInc("ModifyNetworkInterfaceAttribute", err)
				prometheusmetrics.Ec2ApiErr.WithLabelValues("ModifyNetworkInterfaceAttribute").Inc()
				//No need to return error here since retry will happen in 30seconds and also
				//If update failed due to stale ENI then returning error will prevent updating SG
				//for following ENIs since the list is sorted
				log.Debugf("refreshSGIDs: unable to update the ENI %s SG - %v", eniID, err)
			}
		}
	}
	return nil
}

// GetAttachedENIs retrieves ENI information from meta data service
func (cache *EC2InstanceMetadataCache) GetAttachedENIs() (eniList []ENIMetadata, err error) {
	ctx := context.TODO()

	// retrieve number of interfaces
	macs, err := cache.imds.GetMACs(ctx)
	if err != nil {
		awsAPIErrInc("GetMACs", err)
		return nil, err
	}
	log.Debugf("Total number of interfaces found: %d ", len(macs))

	enis := make([]ENIMetadata, len(macs))
	// retrieve the attached ENIs
	for i, mac := range macs {
		enis[i], err = cache.getENIMetadata(mac)
		if err != nil {
			return nil, errors.Wrapf(err, "get attached ENIs: failed to retrieve ENI metadata for ENI: %s", mac)
		}
	}
	return enis, nil
}

func (cache *EC2InstanceMetadataCache) getENIMetadata(eniMAC string) (ENIMetadata, error) {
	ctx := context.TODO()

	log.Debugf("Found ENI MAC address: %s", eniMAC)
	var err error
	var deviceNum int

	eniID, err := cache.imds.GetInterfaceID(ctx, eniMAC)
	if err != nil {
		awsAPIErrInc("GetInterfaceID", err)
		return ENIMetadata{}, err
	}

	deviceNum, err = cache.imds.GetDeviceNumber(ctx, eniMAC)
	if err != nil {
		awsAPIErrInc("GetDeviceNumber", err)
		return ENIMetadata{}, err
	}

	primaryMAC, err := cache.imds.GetMAC(ctx)
	if err != nil {
		awsAPIErrInc("GetMAC", err)
		return ENIMetadata{}, err
	}
	if eniMAC == primaryMAC && deviceNum != 0 {
		// Can this even happen? To be backwards compatible, we will always use 0 here and log an error.
		log.Errorf("Device number of primary ENI is %d! Forcing it to be 0 as expected", deviceNum)
		deviceNum = 0
	}

	log.Debugf("Found ENI: %s, MAC %s, device %d", eniID, eniMAC, deviceNum)

	// Get IPv4 and IPv6 addresses assigned to interface
	cidr, err := cache.imds.GetSubnetIPv4CIDRBlock(ctx, eniMAC)
	if err != nil {
		awsAPIErrInc("GetSubnetIPv4CIDRBlock", err)
		return ENIMetadata{}, err
	}

	imdsIPv4s, err := cache.imds.GetLocalIPv4s(ctx, eniMAC)
	if err != nil {
		awsAPIErrInc("GetLocalIPv4s", err)
		return ENIMetadata{}, err
	}

	ec2ip4s := make([]*ec2.NetworkInterfacePrivateIpAddress, len(imdsIPv4s))
	for i, ip4 := range imdsIPv4s {
		ec2ip4s[i] = &ec2.NetworkInterfacePrivateIpAddress{
			Primary:          aws.Bool(i == 0),
			PrivateIpAddress: aws.String(ip4.String()),
		}
	}

	var ec2ip6s []*ec2.NetworkInterfaceIpv6Address
	var subnetV6Cidr string
	if cache.v6Enabled {
		// For IPv6 ENIs, do not error on missing IPv6 information
		v6cidr, err := cache.imds.GetSubnetIPv6CIDRBlocks(ctx, eniMAC)
		if err != nil {
			awsAPIErrInc("GetSubnetIPv6CIDRBlocks", err)
		} else {
			subnetV6Cidr = v6cidr.String()
		}

		imdsIPv6s, err := cache.imds.GetIPv6s(ctx, eniMAC)
		if err != nil {
			awsAPIErrInc("GetIPv6s", err)
		} else {
			ec2ip6s = make([]*ec2.NetworkInterfaceIpv6Address, len(imdsIPv6s))
			for i, ip6 := range imdsIPv6s {
				ec2ip6s[i] = &ec2.NetworkInterfaceIpv6Address{
					Ipv6Address: aws.String(ip6.String()),
				}
			}
		}
	}

	var ec2ipv4Prefixes []*ec2.Ipv4PrefixSpecification
	var ec2ipv6Prefixes []*ec2.Ipv6PrefixSpecification

	// If IPv6 is enabled, get attached v6 prefixes.
	if cache.v6Enabled {
		imdsIPv6Prefixes, err := cache.imds.GetIPv6Prefixes(ctx, eniMAC)
		if err != nil {
			awsAPIErrInc("GetIPv6Prefixes", err)
			return ENIMetadata{}, err
		}
		for _, ipv6prefix := range imdsIPv6Prefixes {
			ec2ipv6Prefixes = append(ec2ipv6Prefixes, &ec2.Ipv6PrefixSpecification{
				Ipv6Prefix: aws.String(ipv6prefix.String()),
			})
		}
	} else if cache.v4Enabled && ((eniMAC == primaryMAC && !cache.useCustomNetworking) || (eniMAC != primaryMAC)) {
		// Get prefix on primary ENI when custom networking is enabled is not needed.
		// If primary ENI has prefixes attached and then we move to custom networking, we don't need to fetch
		// the prefix since recommendation is to terminate the nodes and that would have deleted the prefix on the
		// primary ENI.
		imdsIPv4Prefixes, err := cache.imds.GetIPv4Prefixes(ctx, eniMAC)
		if err != nil {
			awsAPIErrInc("GetIPv4Prefixes", err)
			return ENIMetadata{}, err
		}
		for _, ipv4prefix := range imdsIPv4Prefixes {
			ec2ipv4Prefixes = append(ec2ipv4Prefixes, &ec2.Ipv4PrefixSpecification{
				Ipv4Prefix: aws.String(ipv4prefix.String()),
			})
		}
	}

	return ENIMetadata{
		ENIID:          eniID,
		MAC:            eniMAC,
		DeviceNumber:   deviceNum,
		SubnetIPv4CIDR: cidr.String(),
		IPv4Addresses:  ec2ip4s,
		IPv4Prefixes:   ec2ipv4Prefixes,
		SubnetIPv6CIDR: subnetV6Cidr,
		IPv6Addresses:  ec2ip6s,
		IPv6Prefixes:   ec2ipv6Prefixes,
	}, nil
}

// awsGetFreeDeviceNumber calls EC2 API DescribeInstances to get the next free device index
func (cache *EC2InstanceMetadataCache) awsGetFreeDeviceNumber() (int, error) {
	input := &ec2.DescribeInstancesInput{
		InstanceIds: []*string{aws.String(cache.instanceID)},
	}

	start := time.Now()
	result, err := cache.ec2SVC.DescribeInstancesWithContext(context.Background(), input)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("DescribeInstances").Inc()
	prometheusmetrics.AwsAPILatency.WithLabelValues("DescribeInstances", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err != nil {
		checkAPIErrorAndBroadcastEvent(err, "ec2:DescribeInstances")
		awsAPIErrInc("DescribeInstances", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("DescribeInstances").Inc()
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
		// We don't support multi-card yet, so only account for network card zero
		if aws.Int64Value(eni.Attachment.NetworkCardIndex) == 0 {
			if aws.Int64Value(eni.Attachment.DeviceIndex) > maxENIs {
				log.Warnf("The Device Index %d of the attached ENI %s > instance max slot %d",
					aws.Int64Value(eni.Attachment.DeviceIndex), aws.StringValue(eni.NetworkInterfaceId),
					maxENIs)
			} else {
				log.Debugf("Discovered device number is used: %d", aws.Int64Value(eni.Attachment.DeviceIndex))
				device[aws.Int64Value(eni.Attachment.DeviceIndex)] = true
			}
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
func (cache *EC2InstanceMetadataCache) AllocENI(useCustomCfg bool, sg []*string, eniCfgSubnet string, numIPs int) (string, error) {
	eniID, err := cache.createENI(useCustomCfg, sg, eniCfgSubnet, numIPs)
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

	// Also change the ENI's attribute so that the ENI will be deleted when the instance is deleted.
	attributeInput := &ec2.ModifyNetworkInterfaceAttributeInput{
		Attachment: &ec2.NetworkInterfaceAttachmentChanges{
			AttachmentId:        aws.String(attachmentID),
			DeleteOnTermination: aws.Bool(true),
		},
		NetworkInterfaceId: aws.String(eniID),
	}

	start := time.Now()
	_, err = cache.ec2SVC.ModifyNetworkInterfaceAttributeWithContext(context.Background(), attributeInput)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("ModifyNetworkInterfaceAttribute").Inc()
	prometheusmetrics.AwsAPILatency.WithLabelValues("ModifyNetworkInterfaceAttribute", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err != nil {
		checkAPIErrorAndBroadcastEvent(err, "ec2:ModifyNetworkInterfaceAttribute")
		awsAPIErrInc("ModifyNetworkInterfaceAttribute", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("ModifyNetworkInterfaceAttribute").Inc()
		err := cache.FreeENI(eniID)
		if err != nil {
			awsUtilsErrInc("ENICleanupUponModifyNetworkErr", err)
		}
		return "", errors.Wrap(err, "AllocENI: unable to change the ENI's attribute")
	}

	log.Infof("Successfully created and attached a new ENI %s to instance", eniID)
	return eniID, nil
}

// attachENI calls EC2 API to attach the ENI and returns the attachment id
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
		NetworkCardIndex:   aws.Int64(0),
	}
	start := time.Now()
	attachOutput, err := cache.ec2SVC.AttachNetworkInterfaceWithContext(context.Background(), attachInput)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("AttachNetworkInterface").Inc()
	prometheusmetrics.AwsAPILatency.WithLabelValues("AttachNetworkInterface", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err != nil {
		checkAPIErrorAndBroadcastEvent(err, "ec2:AttachNetworkInterface")
		awsAPIErrInc("AttachNetworkInterface", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("AttachNetworkInterface").Inc()
		log.Errorf("Failed to attach ENI %s: %v", eniID, err)
		return "", errors.Wrap(err, "attachENI: failed to attach ENI")
	}
	return aws.StringValue(attachOutput.AttachmentId), err
}

// return ENI id, error
func (cache *EC2InstanceMetadataCache) createENI(useCustomCfg bool, sg []*string, eniCfgSubnet string, numIPs int) (string, error) {
	eniDescription := eniDescriptionPrefix + cache.instanceID
	tags := map[string]string{
		eniCreatedAtTagKey: time.Now().Format(time.RFC3339),
	}
	for key, value := range cache.buildENITags() {
		tags[key] = value
	}
	tagSpec := []*ec2.TagSpecification{
		{
			ResourceType: aws.String(ec2.ResourceTypeNetworkInterface),
			Tags:         convertTagsToSDKTags(tags),
		},
	}
	var needIPs = numIPs

	ipLimit := cache.GetENIIPv4Limit()
	if ipLimit < needIPs {
		needIPs = ipLimit
	}

	log.Infof("Trying to allocate %d IP addresses on new ENI", needIPs)
	log.Debugf("PD enabled - %t", cache.enablePrefixDelegation)

	input := &ec2.CreateNetworkInterfaceInput{}

	if cache.enablePrefixDelegation {
		input = &ec2.CreateNetworkInterfaceInput{
			Description:       aws.String(eniDescription),
			Groups:            aws.StringSlice(cache.securityGroups.SortedList()),
			SubnetId:          aws.String(cache.subnetID),
			TagSpecifications: tagSpec,
			Ipv4PrefixCount:   aws.Int64(int64(needIPs)),
		}
	} else {
		input = &ec2.CreateNetworkInterfaceInput{
			Description:                    aws.String(eniDescription),
			Groups:                         aws.StringSlice(cache.securityGroups.SortedList()),
			SubnetId:                       aws.String(cache.subnetID),
			TagSpecifications:              tagSpec,
			SecondaryPrivateIpAddressCount: aws.Int64(int64(needIPs)),
		}
	}

	var err error
	var networkInterfaceID string
	if cache.useCustomNetworking {
		input = createENIUsingCustomCfg(sg, eniCfgSubnet, input)
		log.Infof("Creating ENI with security groups: %v in subnet: %s", aws.StringValueSlice(input.Groups), aws.StringValue(input.SubnetId))

		networkInterfaceID, err = cache.tryCreateNetworkInterface(input)
		if err == nil {
			return networkInterfaceID, nil
		}
	} else {
		if cache.useSubnetDiscovery {
			subnetResult, vpcErr := cache.getVpcSubnets()
			if vpcErr != nil {
				log.Warnf("Failed to call ec2:DescribeSubnets: %v", vpcErr)
				log.Info("Defaulting to same subnet as the primary interface for the new ENI")
				networkInterfaceID, err = cache.tryCreateNetworkInterface(input)
				if err == nil {
					return networkInterfaceID, nil
				}
			} else {
				for _, subnet := range subnetResult {
					if *subnet.SubnetId != cache.subnetID {
						if !validTag(subnet) {
							continue
						}
					}
					log.Infof("Creating ENI with security groups: %v in subnet: %s", aws.StringValueSlice(input.Groups), aws.StringValue(input.SubnetId))

					input.SubnetId = subnet.SubnetId
					networkInterfaceID, err = cache.tryCreateNetworkInterface(input)
					if err == nil {
						return networkInterfaceID, nil
					}
				}
			}
		} else {
			log.Info("Using same security group config as the primary interface for the new ENI")
			networkInterfaceID, err = cache.tryCreateNetworkInterface(input)
			if err == nil {
				return networkInterfaceID, nil
			}
		}
	}
	return "", errors.Wrap(err, "failed to create network interface")
}

func (cache *EC2InstanceMetadataCache) getVpcSubnets() ([]*ec2.Subnet, error) {
	describeSubnetInput := &ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{aws.String(cache.vpcID)},
			},
			{
				Name:   aws.String("availability-zone"),
				Values: []*string{aws.String(cache.availabilityZone)},
			},
		},
	}

	start := time.Now()
	subnetResult, err := cache.ec2SVC.DescribeSubnetsWithContext(context.Background(), describeSubnetInput)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("DescribeSubnets").Inc()
	prometheusmetrics.AwsAPILatency.WithLabelValues("DescribeSubnets", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err != nil {
		checkAPIErrorAndBroadcastEvent(err, "ec2:DescribeSubnets")
		awsAPIErrInc("DescribeSubnets", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("DescribeSubnets").Inc()
		return nil, errors.Wrap(err, "AllocENI: unable to describe subnets")
	}

	// Sort the subnet by available IP address counter (desc order) before determining subnet to use
	sort.SliceStable(subnetResult.Subnets, func(i, j int) bool {
		return *subnetResult.Subnets[j].AvailableIpAddressCount < *subnetResult.Subnets[i].AvailableIpAddressCount
	})

	return subnetResult.Subnets, nil
}

func validTag(subnet *ec2.Subnet) bool {
	for _, tag := range subnet.Tags {
		if *tag.Key == subnetDiscoveryTagKey {
			return true
		}
	}
	return false
}

func createENIUsingCustomCfg(sg []*string, eniCfgSubnet string, input *ec2.CreateNetworkInterfaceInput) *ec2.CreateNetworkInterfaceInput {
	log.Info("Using a custom network config for the new ENI")

	if len(sg) != 0 {
		input.Groups = sg
	} else {
		log.Warnf("No custom networking security group found, will use the node's primary ENI's SG: %v", aws.StringValueSlice(input.Groups))
	}
	input.SubnetId = aws.String(eniCfgSubnet)

	return input
}

func (cache *EC2InstanceMetadataCache) tryCreateNetworkInterface(input *ec2.CreateNetworkInterfaceInput) (string, error) {
	start := time.Now()
	result, err := cache.ec2SVC.CreateNetworkInterfaceWithContext(context.Background(), input)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("CreateNetworkInterface").Inc()
	prometheusmetrics.AwsAPILatency.WithLabelValues("CreateNetworkInterface", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err == nil {
		log.Infof("Created a new ENI: %s", aws.StringValue(result.NetworkInterface.NetworkInterfaceId))
		return aws.StringValue(result.NetworkInterface.NetworkInterfaceId), nil
	}
	checkAPIErrorAndBroadcastEvent(err, "ec2:CreateNetworkInterface")
	awsAPIErrInc("CreateNetworkInterface", err)
	prometheusmetrics.Ec2ApiErr.WithLabelValues("CreateNetworkInterface").Inc()
	log.Errorf("Failed to CreateNetworkInterface %v for subnet %s", err, *input.SubnetId)
	return "", err
}

// buildENITags computes the desired AWS Tags for eni
func (cache *EC2InstanceMetadataCache) buildENITags() map[string]string {
	tags := map[string]string{
		eniNodeTagKey: cache.instanceID,
	}

	// If clusterName is provided,
	// tag the ENI with "cluster.k8s.amazonaws.com/name=<cluster_name>"
	if cache.clusterName != "" {
		tags[eniClusterTagKey] = cache.clusterName
	}
	for key, value := range cache.additionalENITags {
		tags[key] = value
	}
	return tags
}

func (cache *EC2InstanceMetadataCache) TagENI(eniID string, currentTags map[string]string) error {
	tagChanges := make(map[string]string)
	for tagKey, tagValue := range cache.buildENITags() {
		if currentTagValue, ok := currentTags[tagKey]; !ok || currentTagValue != tagValue {
			tagChanges[tagKey] = tagValue
		}
	}
	if len(tagChanges) == 0 {
		return nil
	}

	input := &ec2.CreateTagsInput{
		Resources: []*string{
			aws.String(eniID),
		},
		Tags: convertTagsToSDKTags(tagChanges),
	}

	log.Debugf("Tagging ENI %s with missing tags: %v", eniID, tagChanges)
	return retry.NWithBackoff(retry.NewSimpleBackoff(500*time.Millisecond, maxENIBackoffDelay, 0.3, 2), 5, func() error {
		start := time.Now()
		_, err := cache.ec2SVC.CreateTagsWithContext(context.Background(), input)
		prometheusmetrics.Ec2ApiReq.WithLabelValues("CreateTags").Inc()
		prometheusmetrics.AwsAPILatency.WithLabelValues("CreateTags", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
		if err != nil {
			checkAPIErrorAndBroadcastEvent(err, "ec2:CreateTags")
			awsAPIErrInc("CreateTags", err)
			prometheusmetrics.Ec2ApiErr.WithLabelValues("CreateTags").Inc()
			log.Warnf("Failed to tag the newly created ENI %s:", eniID)
			return err
		}
		log.Debugf("Successfully tagged ENI: %s", eniID)
		return nil
	})
}

func awsAPIErrInc(api string, err error) {
	if aerr, ok := err.(awserr.Error); ok {
		prometheusmetrics.AwsAPIErr.With(prometheus.Labels{"api": api, "error": aerr.Code()}).Inc()
	}
}

func awsUtilsErrInc(fn string, err error) {
	prometheusmetrics.AwsUtilsErr.With(prometheus.Labels{"fn": fn, "error": err.Error()}).Inc()
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
		_, ec2Err := cache.ec2SVC.DetachNetworkInterfaceWithContext(context.Background(), detachInput)
		prometheusmetrics.Ec2ApiReq.WithLabelValues("DetachNetworkInterface").Inc()
		prometheusmetrics.AwsAPILatency.WithLabelValues("DetachNetworkInterface", fmt.Sprint(ec2Err != nil), awsReqStatus(ec2Err)).Observe(msSince(start))
		if ec2Err != nil {
			checkAPIErrorAndBroadcastEvent(err, "ec2:DetachNetworkInterface")
			awsAPIErrInc("DetachNetworkInterface", ec2Err)
			prometheusmetrics.Ec2ApiErr.WithLabelValues("DetachNetworkInterface").Inc()
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
	result, err := cache.ec2SVC.DescribeNetworkInterfacesWithContext(context.Background(), input)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("DescribeNetworkInterfaces").Inc()
	prometheusmetrics.AwsAPILatency.WithLabelValues("DescribeNetworkInterfaces", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "InvalidNetworkInterfaceID.NotFound" {
				return nil, ErrENINotFound
			}
		}
		checkAPIErrorAndBroadcastEvent(err, "ec2:DescribeNetworkInterfaces")
		awsAPIErrInc("DescribeNetworkInterfaces", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("DescribeNetworkInterfaces").Inc()
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
		_, ec2Err := cache.ec2SVC.DeleteNetworkInterfaceWithContext(context.Background(), deleteInput)
		prometheusmetrics.Ec2ApiReq.WithLabelValues("DeleteNetworkInterface").Inc()
		prometheusmetrics.AwsAPILatency.WithLabelValues("DeleteNetworkInterface", fmt.Sprint(ec2Err != nil), awsReqStatus(ec2Err)).Observe(msSince(start))
		if ec2Err != nil {
			if aerr, ok := ec2Err.(awserr.Error); ok {
				// If already deleted, we are good
				if aerr.Code() == "InvalidNetworkInterfaceID.NotFound" {
					log.Infof("ENI %s has already been deleted", eniName)
					return nil
				}
			}
			checkAPIErrorAndBroadcastEvent(ec2Err, "ec2:DeleteNetworkInterface")
			awsAPIErrInc("DeleteNetworkInterface", ec2Err)
			prometheusmetrics.Ec2ApiErr.WithLabelValues("DeleteNetworkInterface").Inc()
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
	result, err := cache.ec2SVC.DescribeNetworkInterfacesWithContext(context.Background(), input)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("DescribeNetworkInterfaces").Inc()
	prometheusmetrics.AwsAPILatency.WithLabelValues("DescribeNetworkInterfaces", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "InvalidNetworkInterfaceID.NotFound" {
				return nil, ErrENINotFound
			}
		}
		checkAPIErrorAndBroadcastEvent(err, "ec2:DescribeNetworkInterfaces")
		awsAPIErrInc("DescribeNetworkInterfaces", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("DescribeNetworkInterfaces").Inc()
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

// GetIPv4PrefixesFromEC2 calls EC2 and returns a list of all addresses on the ENI
func (cache *EC2InstanceMetadataCache) GetIPv4PrefixesFromEC2(eniID string) (addrList []*ec2.Ipv4PrefixSpecification, err error) {
	eniIds := []*string{aws.String(eniID)}
	input := &ec2.DescribeNetworkInterfacesInput{NetworkInterfaceIds: eniIds}

	start := time.Now()
	result, err := cache.ec2SVC.DescribeNetworkInterfacesWithContext(context.Background(), input)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("DescribeNetworkInterfaces").Inc()
	prometheusmetrics.AwsAPILatency.WithLabelValues("DescribeNetworkInterfaces", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "InvalidNetworkInterfaceID.NotFound" {
				return nil, ErrENINotFound
			}

		}
		checkAPIErrorAndBroadcastEvent(err, "ec2:DescribeNetworkInterfaces")
		awsAPIErrInc("DescribeNetworkInterfaces", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("DescribeNetworkInterfaces").Inc()
		log.Errorf("Failed to get ENI %s information from EC2 control plane %v", eniID, err)
		return nil, errors.Wrap(err, "failed to describe network interface")
	}

	// Shouldn't happen, but let's be safe
	if len(result.NetworkInterfaces) == 0 {
		return nil, ErrNoNetworkInterfaces
	}
	returnedENI := result.NetworkInterfaces[0]

	return returnedENI.Ipv4Prefixes, nil
}

// GetIPv6PrefixesFromEC2 calls EC2 and returns a list of all addresses on the ENI
func (cache *EC2InstanceMetadataCache) GetIPv6PrefixesFromEC2(eniID string) (addrList []*ec2.Ipv6PrefixSpecification, err error) {
	eniIds := []*string{aws.String(eniID)}
	input := &ec2.DescribeNetworkInterfacesInput{NetworkInterfaceIds: eniIds}

	start := time.Now()
	result, err := cache.ec2SVC.DescribeNetworkInterfacesWithContext(context.Background(), input)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("DescribeNetworkInterfaces").Inc()
	prometheusmetrics.AwsAPILatency.WithLabelValues("DescribeNetworkInterfaces", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "InvalidNetworkInterfaceID.NotFound" {
				return nil, ErrENINotFound
			}

		}
		checkAPIErrorAndBroadcastEvent(err, "ec2:DescribeNetworkInterfaces")
		awsAPIErrInc("DescribeNetworkInterfaces", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("DescribeNetworkInterfaces").Inc()
		log.Errorf("Failed to get ENI %s information from EC2 control plane %v", eniID, err)
		return nil, errors.Wrap(err, "failed to describe network interface")
	}

	if len(result.NetworkInterfaces) == 0 {
		return nil, ErrNoNetworkInterfaces
	}
	returnedENI := result.NetworkInterfaces[0]

	return returnedENI.Ipv6Prefixes, nil
}

// DescribeAllENIs calls EC2 to refresh the ENIMetadata and tags for all attached ENIs
func (cache *EC2InstanceMetadataCache) DescribeAllENIs() (DescribeAllENIsResult, error) {
	// Fetch all local ENI info from metadata
	allENIs, err := cache.GetAttachedENIs()
	if err != nil {
		return DescribeAllENIsResult{}, errors.Wrap(err, "DescribeAllENIs: failed to get local ENI metadata")
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
		ec2Response, err = cache.ec2SVC.DescribeNetworkInterfacesWithContext(context.Background(), input)
		prometheusmetrics.Ec2ApiReq.WithLabelValues("DescribeNetworkInterfaces").Inc()
		prometheusmetrics.AwsAPILatency.WithLabelValues("DescribeNetworkInterfaces", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
		if err == nil {
			// No error, exit the loop
			break
		}
		awsAPIErrInc("DescribeNetworkInterfaces", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("DescribeNetworkInterfaces").Inc()
		checkAPIErrorAndBroadcastEvent(err, "ec2:DescribeNetworkInterfaces")
		log.Errorf("Failed to call ec2:DescribeNetworkInterfaces for %v: %v", aws.StringValueSlice(input.NetworkInterfaceIds), err)
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "InvalidNetworkInterfaceID.NotFound" {
				badENIID := badENIID(aerr.Message())
				log.Debugf("Could not find interface: %s, ID: %s", aerr.Message(), badENIID)
				awsAPIErrInc("IMDSMetaDataOutOfSync", err)
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
		return DescribeAllENIsResult{}, err
	}

	// Collect the verified ENIs
	var verifiedENIs []ENIMetadata
	for _, eniMetadata := range eniMap {
		verifiedENIs = append(verifiedENIs, eniMetadata)
	}

	// Collect ENI response into ENI metadata and tags.
	var trunkENI string
	var multiCardENIIDs []string
	efaENIs := make(map[string]bool, 0)
	tagMap := make(map[string]TagMap, len(ec2Response.NetworkInterfaces))
	for _, ec2res := range ec2Response.NetworkInterfaces {
		eniID := aws.StringValue(ec2res.NetworkInterfaceId)
		attachment := ec2res.Attachment
		// Validate that Attachment is populated by EC2 response before logging
		if attachment != nil {
			log.Infof("Got network card index %v for ENI %v", aws.Int64Value(attachment.NetworkCardIndex), eniID)
			if aws.Int64Value(attachment.DeviceIndex) == 0 && !aws.BoolValue(attachment.DeleteOnTermination) {
				log.Warn("Primary ENI will not get deleted when node terminates because 'delete_on_termination' is set to false")
			}
			if aws.Int64Value(attachment.NetworkCardIndex) > 0 {
				multiCardENIIDs = append(multiCardENIIDs, eniID)
			}
		} else {
			log.Infof("Got empty attachment for ENI %v", eniID)
		}

		eniMetadata := eniMap[eniID]
		interfaceType := aws.StringValue(ec2res.InterfaceType)
		log.Infof("%s is of type: %s", eniID, interfaceType)

		// This assumes we only have one trunk attached to the node..
		if interfaceType == "trunk" {
			trunkENI = eniID
		}
		if interfaceType == "efa" {
			efaENIs[eniID] = true
		}
		// Check IPv4 addresses
		logOutOfSyncState(eniID, eniMetadata.IPv4Addresses, ec2res.PrivateIpAddresses)
		tagMap[eniMetadata.ENIID] = convertSDKTagsToTags(ec2res.TagSet)
	}
	return DescribeAllENIsResult{
		ENIMetadata:     verifiedENIs,
		TagMap:          tagMap,
		TrunkENI:        trunkENI,
		EFAENIs:         efaENIs,
		MultiCardENIIDs: multiCardENIIDs,
	}, nil
}

// convertTagsToSDKTags converts tags in stringMap format to AWS SDK format
func convertTagsToSDKTags(tagsMap map[string]string) []*ec2.Tag {
	if len(tagsMap) == 0 {
		return nil
	}

	sdkTags := make([]*ec2.Tag, 0, len(tagsMap))
	for _, key := range sets.StringKeySet(tagsMap).List() {
		sdkTags = append(sdkTags, &ec2.Tag{
			Key:   aws.String(key),
			Value: aws.String(tagsMap[key]),
		})
	}
	return sdkTags
}

// convertSDKTagsToTags converts tags in AWS SDKs format to stringMap format
func convertSDKTagsToTags(sdkTags []*ec2.Tag) map[string]string {
	if len(sdkTags) == 0 {
		return nil
	}

	tagsMap := make(map[string]string, len(sdkTags))
	for _, sdkTag := range sdkTags {
		tagsMap[aws.StringValue(sdkTag.Key)] = aws.StringValue(sdkTag.Value)
	}
	return tagsMap
}

// loadAdditionalENITags will load the additional ENI Tags from environment variables.
func loadAdditionalENITags() map[string]string {
	additionalENITagsStr := os.Getenv(additionalEniTagsEnvVar)
	if additionalENITagsStr == "" {
		return nil
	}

	// TODO: ideally we should fail in CNI init phase if the validation fails instead of warn.
	// currently we only warn to be backwards-compatible and keep changes minimal in this version.

	var additionalENITags map[string]string
	// If duplicate keys exist, the value of the key will be the value of latter key.
	err := json.Unmarshal([]byte(additionalENITagsStr), &additionalENITags)
	if err != nil {
		log.Warnf("failed to parse additional ENI Tags from env %v due to %v", additionalEniTagsEnvVar, err)
		return nil
	}
	for key := range additionalENITags {
		if strings.Contains(key, reservedTagKeyPrefix) {
			log.Warnf("ignoring tagKey %v from additional ENI Tags as it contains reserved prefix %v", key, reservedTagKeyPrefix)
			delete(additionalENITags, key)
		}
	}
	return additionalENITags
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

// AllocIPAddress allocates an IP address for an ENI
func (cache *EC2InstanceMetadataCache) AllocIPAddress(eniID string) error {
	log.Infof("Trying to allocate an IP address on ENI: %s", eniID)

	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String(eniID),
		SecondaryPrivateIpAddressCount: aws.Int64(1),
	}

	start := time.Now()
	output, err := cache.ec2SVC.AssignPrivateIpAddressesWithContext(context.Background(), input)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("AssignPrivateIpAddresses").Inc()
	prometheusmetrics.AwsAPILatency.WithLabelValues("AssignPrivateIpAddresses", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err != nil {
		checkAPIErrorAndBroadcastEvent(err, "ec2:AssignPrivateIpAddresses")
		awsAPIErrInc("AssignPrivateIpAddresses", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("AssignPrivateIpAddresses").Inc()
		log.Errorf("Failed to allocate a private IP address  %v", err)
		return errors.Wrap(err, "failed to assign private IP addresses")
	}

	log.Infof("Successfully allocated IP address %s on ENI %s", output.String(), eniID)
	return nil
}

func (cache *EC2InstanceMetadataCache) FetchInstanceTypeLimits() error {
	_, ok := vpc.GetInstance(cache.instanceType)
	if ok {
		return nil
	}

	log.Debugf("Instance type limits are missing from vpc_ip_limits.go hence making an EC2 call to fetch the limits")
	describeInstanceTypesInput := &ec2.DescribeInstanceTypesInput{InstanceTypes: []*string{aws.String(cache.instanceType)}}
	output, err := cache.ec2SVC.DescribeInstanceTypesWithContext(context.Background(), describeInstanceTypesInput)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("DescribeInstanceTypes").Inc()
	if err != nil || len(output.InstanceTypes) != 1 {
		prometheusmetrics.Ec2ApiErr.WithLabelValues("DescribeInstanceTypes").Inc()
		checkAPIErrorAndBroadcastEvent(err, "ec2:DescribeInstanceTypes")
		return errors.New(fmt.Sprintf("Failed calling DescribeInstanceTypes for `%s`: %v", cache.instanceType, err))
	}
	info := output.InstanceTypes[0]
	// Ignore any missing values
	instanceType := aws.StringValue(info.InstanceType)
	eniLimit := int(aws.Int64Value(info.NetworkInfo.MaximumNetworkInterfaces))
	ipv4Limit := int(aws.Int64Value(info.NetworkInfo.Ipv4AddressesPerInterface))
	isBareMetalInstance := aws.BoolValue(info.BareMetal)
	hypervisorType := aws.StringValue(info.Hypervisor)
	if hypervisorType == "" {
		hypervisorType = "unknown"
	}
	networkCards := make([]vpc.NetworkCard, aws.Int64Value(info.NetworkInfo.MaximumNetworkCards))
	defaultNetworkCardIndex := int(aws.Int64Value(info.NetworkInfo.DefaultNetworkCardIndex))
	for idx := 0; idx < len(networkCards); idx += 1 {
		networkCards[idx] = vpc.NetworkCard{
			MaximumNetworkInterfaces: *info.NetworkInfo.NetworkCards[idx].MaximumNetworkInterfaces,
			NetworkCardIndex:         *info.NetworkInfo.NetworkCards[idx].NetworkCardIndex,
		}
	}
	//Not checking for empty hypervisorType since have seen certain instances not getting this filled.
	if instanceType != "" && eniLimit > 0 && ipv4Limit > 0 {
		vpc.SetInstance(instanceType, eniLimit, ipv4Limit, defaultNetworkCardIndex, networkCards, hypervisorType, isBareMetalInstance)
	} else {
		return errors.New(fmt.Sprintf("%s: %s", UnknownInstanceType, cache.instanceType))
	}
	return nil
}

// GetENIIPv4Limit return IP address limit per ENI based on EC2 instance type
func (cache *EC2InstanceMetadataCache) GetENIIPv4Limit() int {
	ipv4Limit, err := vpc.GetIPv4Limit(cache.instanceType)
	if err != nil {
		return -1
	}
	// Subtract one from the IPv4Limit since we don't use the primary IP on each ENI for pods.
	return ipv4Limit - 1
}

// GetENILimit returns the number of ENIs can be attached to an instance
func (cache *EC2InstanceMetadataCache) GetENILimit() int {
	eniLimit, err := vpc.GetENILimit(cache.instanceType)
	if err != nil {
		return -1
	}
	return eniLimit
}

// GetNetworkCards returns the network cards the instance has
func (cache *EC2InstanceMetadataCache) GetNetworkCards() []vpc.NetworkCard {
	networkCards, err := vpc.GetNetworkCards(cache.instanceType)
	if err != nil {
		return nil
	}
	return networkCards
}

// GetInstanceHypervisorFamily returns hypervisor of EC2 instance type
func (cache *EC2InstanceMetadataCache) GetInstanceHypervisorFamily() string {
	hypervisor, err := vpc.GetHypervisorType(cache.instanceType)
	if err != nil {
		return ""
	}
	log.Debugf("Instance hypervisor family %s", hypervisor)
	return hypervisor
}

// IsInstanceBareMetal derives bare metal value of the instance
func (cache *EC2InstanceMetadataCache) IsInstanceBareMetal() bool {
	isBaremetal, err := vpc.GetIsBareMetal(cache.instanceType)
	if err != nil {
		return false
	}
	log.Debugf("Bare Metal Instance %s", isBaremetal)
	return isBaremetal
}

// GetInstanceType return EC2 instance type
func (cache *EC2InstanceMetadataCache) GetInstanceType() string {
	return cache.instanceType
}

// IsPrefixDelegationSupported return true if the instance type supports Prefix Assignment/Delegation
func (cache *EC2InstanceMetadataCache) IsPrefixDelegationSupported() bool {
	log.Debugf("Check if instance supports Prefix Delegation")
	if cache.GetInstanceHypervisorFamily() == "nitro" || cache.IsInstanceBareMetal() {
		log.Debugf("Instance supports Prefix Delegation")
		return true
	}
	return false
}

// AllocIPAddresses allocates numIPs of IP address on an ENI
func (cache *EC2InstanceMetadataCache) AllocIPAddresses(eniID string, numIPs int) (*ec2.AssignPrivateIpAddressesOutput, error) {
	var needIPs = numIPs

	ipLimit := cache.GetENIIPv4Limit()

	if ipLimit < needIPs {
		needIPs = ipLimit
	}

	// If we don't need any more IPs, exit
	if needIPs < 1 {
		return nil, nil
	}

	log.Infof("Trying to allocate %d IP addresses on ENI %s", needIPs, eniID)
	log.Debugf("PD enabled - %t", cache.enablePrefixDelegation)
	input := &ec2.AssignPrivateIpAddressesInput{}

	if cache.enablePrefixDelegation {
		needPrefixes := needIPs
		input = &ec2.AssignPrivateIpAddressesInput{
			NetworkInterfaceId: aws.String(eniID),
			Ipv4PrefixCount:    aws.Int64(int64(needPrefixes)),
		}

	} else {
		input = &ec2.AssignPrivateIpAddressesInput{
			NetworkInterfaceId:             aws.String(eniID),
			SecondaryPrivateIpAddressCount: aws.Int64(int64(needIPs)),
		}
	}

	start := time.Now()
	output, err := cache.ec2SVC.AssignPrivateIpAddressesWithContext(context.Background(), input)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("AssignPrivateIpAddresses").Inc()
	prometheusmetrics.AwsAPILatency.WithLabelValues("AssignPrivateIpAddresses", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err != nil {
		checkAPIErrorAndBroadcastEvent(err, "ec2:AssignPrivateIpAddresses")
		log.Errorf("Failed to allocate a private IP/Prefix addresses on ENI %v: %v", eniID, err)
		awsAPIErrInc("AssignPrivateIpAddresses", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("AssignPrivateIpAddresses").Inc()
		return nil, err
	}
	if output != nil {
		if cache.enablePrefixDelegation {
			log.Infof("Allocated %d private IP prefixes", len(output.AssignedIpv4Prefixes))
		} else {
			log.Infof("Allocated %d private IP addresses", len(output.AssignedPrivateIpAddresses))
		}
	}
	return output, nil
}

func (cache *EC2InstanceMetadataCache) AllocIPv6Prefixes(eniID string) ([]*string, error) {
	//We only need to allocate one IPv6 prefix per ENI.
	input := &ec2.AssignIpv6AddressesInput{
		NetworkInterfaceId: aws.String(eniID),
		Ipv6PrefixCount:    aws.Int64(1),
	}
	start := time.Now()
	output, err := cache.ec2SVC.AssignIpv6AddressesWithContext(context.Background(), input)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("AssignIpv6Addresses").Inc()
	prometheusmetrics.AwsAPILatency.WithLabelValues("AssignIpv6AddressesWithContext", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err != nil {
		checkAPIErrorAndBroadcastEvent(err, "ec2:AssignPrivateIpv6Addresses")
		log.Errorf("Failed to allocate IPv6 Prefixes on ENI %v: %v", eniID, err)
		awsAPIErrInc("AssignPrivateIpv6Addresses", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("AssignIpv6Addresses").Inc()
		return nil, errors.Wrap(err, "allocate IPv6 prefix: failed to allocate an IPv6 prefix address")
	}
	if output != nil {
		log.Debugf("Allocated %d private IPv6 prefix(es)", len(output.AssignedIpv6Prefixes))
	}
	return output.AssignedIpv6Prefixes, nil
}

// WaitForENIAndIPsAttached waits until the ENI has been attached and the secondary IPs have been added
func (cache *EC2InstanceMetadataCache) WaitForENIAndIPsAttached(eni string, wantedCidrs int) (eniMetadata ENIMetadata, err error) {
	return cache.waitForENIAndIPsAttached(eni, wantedCidrs, maxENIBackoffDelay)
}

func (cache *EC2InstanceMetadataCache) waitForENIAndIPsAttached(eni string, wantedCidrs int, maxBackoffDelay time.Duration) (eniMetadata ENIMetadata, err error) {
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
				// Check how many Secondary IPs or Prefixes have been attached
				var eniIPCount int
				log.Debugf("ENI ID: %v IP Addr: %s, IPv4Prefixes:- %v, IPv6Prefixes:- %v", returnedENI.ENIID,
					returnedENI.IPv4Addresses, returnedENI.IPv4Prefixes, returnedENI.IPv6Prefixes)
				if cache.enablePrefixDelegation {
					eniIPCount = len(returnedENI.IPv4Prefixes)
					if cache.v6Enabled {
						eniIPCount = len(returnedENI.IPv6Prefixes)
					}
				} else {
					//Ignore primary IP of the ENI
					//wantedCidrs will be at most 1 less then the IP limit for the ENI because of the primary IP in secondary pod
					eniIPCount = len(returnedENI.IPv4Addresses) - 1
				}

				if eniIPCount < 1 {
					log.Debugf("No secondary IPv4 addresses/prefixes available yet on ENI %s", returnedENI.ENIID)
					return ErrNoSecondaryIPsFound
				}

				// At least some are attached
				eniMetadata = returnedENI

				if eniIPCount >= wantedCidrs {
					return nil
				}
				return ErrAllSecondaryIPsNotFound
			}
		}
		log.Debugf("Not able to find the right ENI yet (attempt %d/%d)", attempt, maxENIEC2APIRetries)
		return ErrENINotFound
	})
	prometheusmetrics.AwsAPILatency.WithLabelValues("waitForENIAndIPsAttached", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err != nil {
		// If we have at least 1 Secondary IP, by now return what we have without an error
		if err == ErrAllSecondaryIPsNotFound {
			if !cache.enablePrefixDelegation && len(eniMetadata.IPv4Addresses) > 1 {
				// We have some Secondary IPs, return the ones we have
				log.Warnf("This ENI only has %d IP addresses, we wanted %d", len(eniMetadata.IPv4Addresses), wantedCidrs)
				return eniMetadata, nil
			} else if cache.enablePrefixDelegation && len(eniMetadata.IPv4Prefixes) > 1 {
				// We have some prefixes, return the ones we have
				log.Warnf("This ENI only has %d Prefixes, we wanted %d", len(eniMetadata.IPv4Prefixes), wantedCidrs)
				return eniMetadata, nil
			}
		}
		awsAPIErrInc("waitENIAttachedFailedToAssignIPs", err)
		return ENIMetadata{}, errors.New("waitForENIAndIPsAttached: giving up trying to retrieve ENIs from metadata service")
	}
	return eniMetadata, nil
}

// DeallocIPAddresses frees IP address on an ENI
func (cache *EC2InstanceMetadataCache) DeallocIPAddresses(eniID string, ips []string) error {
	if len(ips) == 0 {
		return nil
	}
	log.Infof("Trying to unassign the following IPs %v from ENI %s", ips, eniID)
	ipsInput := aws.StringSlice(ips)

	input := &ec2.UnassignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String(eniID),
		PrivateIpAddresses: ipsInput,
	}

	start := time.Now()
	_, err := cache.ec2SVC.UnassignPrivateIpAddressesWithContext(context.Background(), input)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("UnassignPrivateIpAddresses").Inc()
	prometheusmetrics.AwsAPILatency.WithLabelValues("UnassignPrivateIpAddresses", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err != nil {
		checkAPIErrorAndBroadcastEvent(err, "ec2:UnassignPrivateIpAddresses")
		awsAPIErrInc("UnassignPrivateIpAddresses", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("UnassignPrivateIpAddresses").Inc()
		log.Errorf("Failed to deallocate a private IP address %v", err)
		return errors.Wrap(err, fmt.Sprintf("deallocate IP addresses: failed to deallocate private IP addresses: %s", ips))
	}
	log.Debugf("Successfully freed IPs %v from ENI %s", ips, eniID)
	return nil
}

// DeallocPrefixAddresses frees Prefixes on an ENI
func (cache *EC2InstanceMetadataCache) DeallocPrefixAddresses(eniID string, prefixes []string) error {
	if len(prefixes) == 0 {
		return nil
	}
	log.Infof("Trying to unassign the following Prefixes %v from ENI %s", prefixes, eniID)
	prefixesInput := aws.StringSlice(prefixes)

	input := &ec2.UnassignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String(eniID),
		Ipv4Prefixes:       prefixesInput,
	}

	start := time.Now()
	_, err := cache.ec2SVC.UnassignPrivateIpAddressesWithContext(context.Background(), input)
	prometheusmetrics.Ec2ApiReq.WithLabelValues("UnassignPrivateIpAddresses").Inc()
	prometheusmetrics.AwsAPILatency.WithLabelValues("UnassignPrivateIpAddresses", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
	if err != nil {
		checkAPIErrorAndBroadcastEvent(err, "ec2:UnassignPrivateIpAddresses")
		awsAPIErrInc("UnassignPrivateIpAddresses", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("UnassignPrivateIpAddresses").Inc()
		log.Errorf("Failed to deallocate a Prefixes address %v", err)
		return errors.Wrap(err, fmt.Sprintf("deallocate prefix: failed to deallocate Prefix addresses: %v", prefixes))
	}
	log.Debugf("Successfully freed Prefixes %v from ENI %s", prefixes, eniID)
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
	networkInterfaces, err := cache.getLeakedENIs()
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
		_, err := cache.ec2SVC.CreateTagsWithContext(context.Background(), input)
		prometheusmetrics.Ec2ApiReq.WithLabelValues("CreateTags").Inc()
		prometheusmetrics.AwsAPILatency.WithLabelValues("CreateTags", fmt.Sprint(err != nil), awsReqStatus(err)).Observe(msSince(start))
		if err != nil {
			checkAPIErrorAndBroadcastEvent(err, "ec2:CreateTags")
			awsAPIErrInc("CreateTags", err)
			prometheusmetrics.Ec2ApiErr.WithLabelValues("CreateTags").Inc()
			log.Warnf("Failed to add tag to ENI %s: %v", eniID, err)
			return err
		}
		log.Debugf("Successfully tagged ENI: %s", eniID)
		return nil
	})
}

// getLeakedENIs calls DescribeNetworkInterfaces to get all available ENIs that were allocated by
// the AWS CNI plugin, but were not deleted.
func (cache *EC2InstanceMetadataCache) getLeakedENIs() ([]*ec2.NetworkInterface, error) {
	leakedENIFilters := []*ec2.Filter{
		{
			Name: aws.String("tag-key"),
			Values: []*string{
				aws.String(eniNodeTagKey),
			},
		},
		{
			Name: aws.String("status"),
			Values: []*string{
				aws.String(ec2.NetworkInterfaceStatusAvailable),
			},
		},
		{
			Name: aws.String("vpc-id"),
			Values: []*string{
				aws.String(cache.vpcID),
			},
		},
	}
	if cache.clusterName != "" {
		leakedENIFilters = append(leakedENIFilters, &ec2.Filter{
			Name: aws.String(fmt.Sprintf("tag:%s", eniClusterTagKey)),
			Values: []*string{
				aws.String(cache.clusterName),
			},
		})
	}

	input := &ec2.DescribeNetworkInterfacesInput{
		Filters:    leakedENIFilters,
		MaxResults: aws.Int64(describeENIPageSize),
	}

	var networkInterfaces []*ec2.NetworkInterface
	filterFn := func(networkInterface *ec2.NetworkInterface) error {
		// Verify the description starts with "aws-K8S-"
		if !strings.HasPrefix(aws.StringValue(networkInterface.Description), eniDescriptionPrefix) {
			return nil
		}
		// Check that it's not a newly created ENI
		tags := convertSDKTagsToTags(networkInterface.TagSet)

		if value, ok := tags[eniCreatedAtTagKey]; ok {
			parsedTime, err := time.Parse(time.RFC3339, value)
			if err != nil {
				log.Warnf("ParsedTime format %s is wrong so retagging with current TS", parsedTime)
				cache.tagENIcreateTS(aws.StringValue(networkInterface.NetworkInterfaceId), maxENIBackoffDelay)
			}
			if time.Since(parsedTime) < eniDeleteCooldownTime {
				log.Infof("Found an ENI created less than 5 minutes ago, so not cleaning it up")
				return nil
			}
			log.Debugf("%v", value)
		} else {
			/* Set a time if we didn't find one. This is to prevent accidentally deleting ENIs that are in the
			 * process of being attached by CNI versions v1.5.x or earlier.
			 */
			cache.tagENIcreateTS(aws.StringValue(networkInterface.NetworkInterfaceId), maxENIBackoffDelay)
			return nil
		}
		networkInterfaces = append(networkInterfaces, networkInterface)
		return nil
	}

	err := cache.getENIsFromPaginatedDescribeNetworkInterfaces(input, filterFn)
	if err != nil {
		return nil, errors.Wrap(err, "awsutils: unable to obtain filtered list of network interfaces")
	}

	if len(networkInterfaces) < 1 {
		log.Debug("No AWS CNI leaked ENIs found.")
		return nil, nil
	}

	log.Debugf("Found %d leaked ENIs with the AWS CNI tag.", len(networkInterfaces))
	return networkInterfaces, nil
}

// GetVPCIPv4CIDRs returns VPC CIDRs
func (cache *EC2InstanceMetadataCache) GetVPCIPv4CIDRs() ([]string, error) {
	ctx := context.TODO()

	ipnets, err := cache.imds.GetVPCIPv4CIDRBlocks(ctx, cache.primaryENImac)
	if err != nil {
		awsAPIErrInc("GetVPCIPv4CIDRBlocks", err)
		return nil, err
	}

	// TODO: keep as net.IPNet and remove this round-trip to/from string
	asStrs := make([]string, len(ipnets))
	for i, ipnet := range ipnets {
		asStrs[i] = ipnet.String()
	}

	return asStrs, nil
}

// GetLocalIPv4 returns the primary IP address on the primary interface
func (cache *EC2InstanceMetadataCache) GetLocalIPv4() net.IP {
	return cache.localIPv4
}

// GetVPCIPv6CIDRs returns VPC CIDRs
func (cache *EC2InstanceMetadataCache) GetVPCIPv6CIDRs() ([]string, error) {
	ctx := context.TODO()

	ipnets, err := cache.imds.GetVPCIPv6CIDRBlocks(ctx, cache.primaryENImac)
	if err != nil {
		awsAPIErrInc("GetVPCIPv6CIDRBlocks", err)
		return nil, err
	}

	asStrs := make([]string, len(ipnets))
	for i, ipnet := range ipnets {
		asStrs[i] = ipnet.String()
	}

	return asStrs, nil
}

// GetPrimaryENI returns the primary ENI
func (cache *EC2InstanceMetadataCache) GetPrimaryENI() string {
	return cache.primaryENI
}

// GetPrimaryENImac returns the mac address of primary eni
func (cache *EC2InstanceMetadataCache) GetPrimaryENImac() string {
	return cache.primaryENImac
}

// SetUnmanagedENIs Set unmanaged ENI set
func (cache *EC2InstanceMetadataCache) SetUnmanagedENIs(eniIDs []string) {
	cache.unmanagedENIs.Set(eniIDs)
}

// GetInstanceID returns the instance ID
func (cache *EC2InstanceMetadataCache) GetInstanceID() string {
	return cache.instanceID
}

// IsUnmanagedENI returns if the eni is unmanaged
func (cache *EC2InstanceMetadataCache) IsUnmanagedENI(eniID string) bool {
	if len(eniID) != 0 {
		return cache.unmanagedENIs.Has(eniID)
	}
	return false
}

func (cache *EC2InstanceMetadataCache) getENIsFromPaginatedDescribeNetworkInterfaces(
	input *ec2.DescribeNetworkInterfacesInput, filterFn func(networkInterface *ec2.NetworkInterface) error) error {
	pageNum := 0
	var innerErr error
	pageFn := func(output *ec2.DescribeNetworkInterfacesOutput, lastPage bool) (nextPage bool) {
		pageNum++
		log.Debugf("EC2 DescribeNetworkInterfaces succeeded with %d results on page %d",
			len(output.NetworkInterfaces), pageNum)
		for _, eni := range output.NetworkInterfaces {
			if err := filterFn(eni); err != nil {
				innerErr = err
				return false
			}
		}
		return true
	}

	if err := cache.ec2SVC.DescribeNetworkInterfacesPagesWithContext(context.TODO(), input, pageFn); err != nil {
		checkAPIErrorAndBroadcastEvent(err, "ec2:DescribeNetworkInterfaces")
		awsAPIErrInc("DescribeNetworkInterfaces", err)
		prometheusmetrics.Ec2ApiErr.WithLabelValues("DescribeNetworkInterfaces").Inc()
		return err
	}
	prometheusmetrics.Ec2ApiReq.WithLabelValues("DescribeNetworkInterfaces").Inc()
	return innerErr
}

// SetMultiCardENIs creates a StringSet tracking ENIs not behind the default network card index
func (cache *EC2InstanceMetadataCache) SetMultiCardENIs(eniID []string) error {
	if len(eniID) != 0 {
		cache.multiCardENIs.Set(eniID)
	}
	return nil
}

// IsMultiCardENI returns if the ENI is not behind the default network card index (multi-card ENI)
func (cache *EC2InstanceMetadataCache) IsMultiCardENI(eniID string) bool {
	if len(eniID) != 0 {
		return cache.multiCardENIs.Has(eniID)
	}
	return false
}

// IsPrimaryENI returns if the eni is unmanaged
func (cache *EC2InstanceMetadataCache) IsPrimaryENI(eniID string) bool {
	if len(eniID) != 0 && eniID == cache.GetPrimaryENI() {
		return true
	}
	return false
}

func checkAPIErrorAndBroadcastEvent(err error, api string) {
	if aerr, ok := err.(awserr.Error); ok {
		if aerr.Code() == "UnauthorizedOperation" {
			if eventRecorder := eventrecorder.Get(); eventRecorder != nil {
				eventRecorder.SendPodEvent(v1.EventTypeWarning, "MissingIAMPermissions", api,
					fmt.Sprintf("Unauthorized operation: failed to call %v due to missing permissions. Please refer https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/iam-policy.md to attach relevant policy to IAM role", api))
			}
		}
	}
}
