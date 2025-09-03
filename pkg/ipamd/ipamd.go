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

package ipamd

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"

	"github.com/aws/smithy-go"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/eniconfig"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/cniutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/amazon-vpc-cni-k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/utils/prometheusmetrics"
	rcv1alpha1 "github.com/aws/amazon-vpc-resource-controller-k8s/apis/vpcresources/v1alpha1"
)

// The package ipamd is a long running daemon which manages a warm pool of available IP addresses.
// It also monitors the size of the pool, dynamically allocates more ENIs when the pool size goes below
// the minimum threshold and frees them back when the pool size goes above max threshold.

const (
	ipPoolMonitorInterval       = 5 * time.Second
	maxRetryCheckENI            = 5
	eniAttachTime               = 10 * time.Second
	nodeIPPoolReconcileInterval = 60 * time.Second
	decreaseIPPoolInterval      = 30 * time.Second

	// ipReconcileCooldown is the amount of time that an IP address must wait until it can be added to the data store
	// during reconciliation after being discovered on the EC2 instance metadata.
	ipReconcileCooldown = 60 * time.Second

	// This environment variable is used to specify the desired number of free IPs always available in the "warm pool".
	// When it is not set, ipamd defaults to use all available IPs per ENI for that instance type.
	// For example, for a m4.4xlarge node,
	//     If WARM_IP_TARGET is set to 1, and there are 9 pods running on the node, ipamd will try
	//     to make the "warm pool" have 10 IP addresses with 9 being assigned to pods and 1 free IP.
	//
	//     If "WARM_IP_TARGET is not set, it will default to 30 (which the maximum number of IPs per ENI).
	//     If there are 9 pods running on the node, ipamd will try to make the "warm pool" have 39 IPs with 9 being
	//     assigned to pods and 30 free IPs.
	envWarmIPTarget = "WARM_IP_TARGET"
	noWarmIPTarget  = 0

	// This environment variable is used to specify the desired minimum number of total IPs.
	// When it is not set, ipamd defaults to 0.
	// For example, for a m4.4xlarge node,
	//     If WARM_IP_TARGET is set to 1 and MINIMUM_IP_TARGET is set to 12, and there are 9 pods running on the node,
	//     ipamd will make the "warm pool" have 12 IP addresses with 9 being assigned to pods and 3 free IPs.
	//
	//     If "MINIMUM_IP_TARGET is not set, it will default to 0, which causes WARM_IP_TARGET settings to be the
	//	   only settings considered.
	envMinimumIPTarget = "MINIMUM_IP_TARGET"
	noMinimumIPTarget  = 0

	// This environment is used to specify the desired number of free ENIs along with all of its IP addresses
	// always available in "warm pool".
	// When it is not set, it is default to 1.
	//
	// when "WARM_IP_TARGET" is defined, ipamd will use behavior defined for "WARM_IP_TARGET".
	//
	// For example, for a m4.4xlarge node
	//     If WARM_ENI_TARGET is set to 2, and there are 9 pods running on the node, ipamd will try to
	//     make the "warm pool" to have 2 extra ENIs and its IP addresses, in other words, 90 IP addresses
	//     with 9 IPs assigned to pods and 81 free IPs.
	//
	//     If "WARM_ENI_TARGET" is not set, it defaults to 1, so if there are 9 pods running on the node,
	//     ipamd will try to make the "warm pool" have 1 extra ENI, in other words, 60 IPs with 9 already
	//     being assigned to pods and 51 free IPs.
	envWarmENITarget     = "WARM_ENI_TARGET"
	defaultWarmENITarget = 1

	// This environment variable is used to specify the maximum number of ENIs that will be allocated.
	// When it is not set or less than 1, the default is to use the maximum available for the instance type.
	//
	// The maximum number of ENIs is in any case limited to the amount allowed for the instance type.
	envMaxENI     = "MAX_ENI"
	defaultMaxENI = -1

	// This environment is used to specify whether Pods need to use a security group and subnet defined in an ENIConfig CRD.
	// When it is NOT set or set to false, ipamd will use primary interface security group and subnet for Pod network.
	envCustomNetworkCfg = "AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG"

	// This environment variable specifies whether IPAMD should allocate or deallocate ENIs on a non-schedulable node (default false).
	envManageENIsNonSchedulable = "AWS_MANAGE_ENIS_NON_SCHEDULABLE"

	// This environment is used to specify whether we should use enhanced subnet selection or not when creating ENIs (default true).
	envSubnetDiscovery = "ENABLE_SUBNET_DISCOVERY"

	// eniNoManageTagKey is the tag that may be set on an ENI to indicate ipamd
	// should not manage it in any form.
	eniNoManageTagKey = "node.k8s.amazonaws.com/no_manage"

	// disableENIProvisioning is used to specify that ENIs do not need to be synced during initializing a pod.
	envDisableENIProvisioning = "DISABLE_NETWORK_RESOURCE_PROVISIONING"

	// disableLeakedENICleanup is used to specify that the task checking and cleaning up leaked ENIs should not be run.
	envDisableLeakedENICleanup = "DISABLE_LEAKED_ENI_CLEANUP"

	// Specify where ipam should persist its current IP<->container allocations.
	envBackingStorePath     = "AWS_VPC_K8S_CNI_BACKING_STORE"
	defaultBackingStorePath = "/var/run/aws-node/ipam.json"

	// envEnablePodENI is used to attach a Trunk ENI to every node. Required in order to give Branch ENIs to pods.
	envEnablePodENI = "ENABLE_POD_ENI"

	// envNodeName will be used to store Node name
	envNodeName = "MY_NODE_NAME"

	//envEnableIpv4PrefixDelegation is used to allocate /28 prefix instead of secondary IP for an ENI.
	envEnableIpv4PrefixDelegation = "ENABLE_PREFIX_DELEGATION"

	//envWarmPrefixTarget is used to keep a /28 prefix in warm pool.
	envWarmPrefixTarget     = "WARM_PREFIX_TARGET"
	defaultWarmPrefixTarget = 0

	//envEnableIPv4 - Env variable to enable/disable IPv4 mode
	envEnableIPv4 = "ENABLE_IPv4"

	//envEnableIPv6 - Env variable to enable/disable IPv6 mode
	envEnableIPv6 = "ENABLE_IPv6"

	ipV4AddrFamily = "4"
	ipV6AddrFamily = "6"

	// insufficientCidrErrorCooldown is the amount of time reconciler will wait before trying to fetch
	// more IPs/prefixes for an ENI. With InsufficientCidr we know the subnet doesn't have enough IPs so
	// instead of retrying every 5s which would lead to increase in EC2 AllocIPAddress calls, we wait for
	// 120 seconds for a retry.
	insufficientCidrErrorCooldown = 120 * time.Second

	// envManageUntaggedENI is used to determine if untagged ENIs should be managed or unmanaged
	envManageUntaggedENI = "MANAGE_UNTAGGED_ENI"

	eniNodeTagKey = "node.k8s.amazonaws.com/instance_id"

	// envAnnotatePodIP is used to annotate[vpc.amazonaws.com/pod-ips] pod's with IPs
	// Ref : https://github.com/projectcalico/calico/issues/3530
	// not present; in which case we fall back to the k8s podIP
	// Present and set to an IP; in which case we use it
	// Present and set to the empty string, which we use to mean "CNI DEL had occurred; networking has been removed from this pod"
	// The empty string one helps close a trace at pod shutdown where it looks like the pod still has its IP when the IP has been released
	envAnnotatePodIP = "ANNOTATE_POD_IP"

	// aws error codes for insufficient IP address scenario
	INSUFFICIENT_CIDR_BLOCKS    = "InsufficientCidrBlocks"
	INSUFFICIENT_FREE_IP_SUBNET = "InsufficientFreeAddressesInSubnet"

	// envEnableNetworkPolicy is used to enable IPAMD/CNI to send pod create events to network policy agent.
	envNetworkPolicyMode = "NETWORK_POLICY_ENFORCING_MODE"

	defaultMaxPodsFromKubelet = 110
	kubeletConfigPath         = "/host/etc/kubernetes/kubelet/kubelet-config.json"
	eniMaxPodsFilePath        = "/app/eni-max-pods.txt"

	// Application name for k8s client
	appName = "aws-node"

	defaultNetworkPolicyMode = "standard"

	// Network Card index of primary ENI
	DefaultNetworkCardIndex = 0

	// Enable Multi NIC support in CNI
	// This configures the ENIs on Network Card > 0 which is be used by pods that require multi-nic attachments
	envEnableMultiNICSupport = "ENABLE_MULTI_NIC"

	// Scale config for network cards > 0
	DefaultWarmIPTarget    = 1
	DefaultMinimumIPTarget = 1
)

var log = logger.Get()

var (
	prometheusRegistered = false
)

// IPAMContext contains node level control information
type IPAMContext struct {
	awsClient                 awsutils.APIs
	dataStoreAccess           *datastore.DataStoreAccess
	k8sClient                 client.Client
	enableIPv4                bool
	enableIPv6                bool
	useCustomNetworking       bool
	manageENIsNonScheduleable bool
	useSubnetDiscovery        bool
	networkClient             networkutils.NetworkAPIs
	maxIPsPerENI              int
	maxENI                    int
	maxPrefixesPerENI         int
	unmanagedENI              []int
	numNetworkCards           int

	warmENITarget        int
	warmIPTarget         int
	minimumIPTarget      int
	warmPrefixTarget     int
	primaryIP            map[string]string // primaryIP is a map from ENI ID to primary IP of that ENI
	lastNodeIPPoolAction time.Time
	lastDecreaseIPPool   time.Time
	// reconcileCooldownCache keeps timestamps of the last time an IP address was unassigned from an ENI,
	// so that we don't reconcile and add it back too quickly if IMDS lags behind reality.
	reconcileCooldownCache    ReconcileCooldownCache
	terminating               int32 // Flag to warn that the pod is about to shut down.
	disableENIProvisioning    bool
	enablePodENI              bool
	myNodeName                string
	enablePrefixDelegation    bool
	lastInsufficientCidrError time.Time
	enableManageUntaggedMode  bool
	enablePodIPAnnotation     bool
	maxPods                   int // maximum number of pods that can be scheduled on the node
	networkPolicyMode         string
	enableMultiNICSupport     bool
	withApiServer             bool
}

type kubeletConfig struct {
	MaxPods *int64 `json:"maxPods"`
}

// setUnmanagedENIs will rebuild the set of ENI IDs for ENIs tagged as "no_manage"
func (c *IPAMContext) setUnmanagedENIs(tagMap map[string]awsutils.TagMap) {
	if len(tagMap) == 0 {
		return
	}
	var unmanagedENIlist []string
	// if "no_manage" tag is present and is true - ENI is unmanaged
	// if "no_manage" tag is present and is "not true" - ENI is managed
	// if "instance_id" tag is present and is set to instanceID - ENI is managed since this was created by IPAMD
	// if "no_manage" tag is not present or not IPAMD created ENI, check if we are in Manage Untagged Mode, default is true.
	// if enableManageUntaggedMode is false (defaults to true), then consider all untagged ENIs as unmanaged.
	for eniID, tags := range tagMap {
		if _, found := tags[eniNoManageTagKey]; found {
			if tags[eniNoManageTagKey] != "true" {
				continue
			}
		} else if _, found := tags[eniNodeTagKey]; found && tags[eniNodeTagKey] == c.awsClient.GetInstanceID() {
			continue
		} else if c.enableManageUntaggedMode {
			continue
		}

		if eniID == c.awsClient.GetPrimaryENI() {
			log.Debugf("Ignoring primary ENI %s since it is always managed", eniID)
		} else {
			log.Debugf("Marking ENI %s as being unmanaged", eniID)
			unmanagedENIlist = append(unmanagedENIlist, eniID)
		}
	}

	c.awsClient.SetUnmanagedENIs(unmanagedENIlist)
}

// setUnmanagedNetworkCards will mark the Network Cards which are not managed by CNI
// When ENABLE_MULTI_NIC is false, all Network Cards > 0 are marked as unmanaged
// When ENABLE_MULTI_NIC is true, Network Cards which ONLY includes an EFA-only device are marked as unmanaged.
// If there is a ENA device on a Network Card along with EFA-only device, CNI manages the Network Card but excludes the EFA-only device
func (c *IPAMContext) markUnmanagedNetworkCards(efaOnlyENINetworkCards []string, enisByNetworkCard [][]string) []bool {

	skipNetworkCards := make([]bool, c.numNetworkCards)
	if !c.enableMultiNICSupport {
		skipNetworkCards = lo.Times(c.numNetworkCards, func(i int) bool {
			return true
		})
		skipNetworkCards[DefaultNetworkCardIndex] = false
	} else {
		for networkCard, efaOnlyENI := range efaOnlyENINetworkCards {

			if efaOnlyENI == "" {
				// Network card does not have an EFA-only ENI, so can be managed
				continue
			}

			// Skip the network card by default, unless we find a ENA device on this network card which is managed
			skipNetworkCards[networkCard] = true
			for _, eni := range enisByNetworkCard[networkCard] {
				if !c.awsClient.IsUnmanagedENI(eni) && efaOnlyENI != eni {
					skipNetworkCards[networkCard] = false
					break
				}
			}
		}
	}

	c.awsClient.SetUnmanagedNetworkCards(skipNetworkCards)
	return skipNetworkCards
}

// ReconcileCooldownCache keep track of recently freed CIDRs to avoid reading stale EC2 metadata
type ReconcileCooldownCache struct {
	sync.RWMutex
	cache map[string]time.Time
}

// Add sets a timestamp for the CIDR added that says how long they are not to be put back in the data store.
func (r *ReconcileCooldownCache) Add(cidr string) {
	r.Lock()
	defer r.Unlock()
	expiry := time.Now().Add(ipReconcileCooldown)
	r.cache[cidr] = expiry
}

// Remove removes a CIDR from the cooldown cache.
func (r *ReconcileCooldownCache) Remove(cidr string) {
	r.Lock()
	defer r.Unlock()
	log.Debugf("Removing %s from cooldown cache.", cidr)
	delete(r.cache, cidr)
}

// RecentlyFreed checks if this CIDR was recently freed.
func (r *ReconcileCooldownCache) RecentlyFreed(cidr string) (found, recentlyFreed bool) {
	r.Lock()
	defer r.Unlock()
	now := time.Now()
	if expiry, ok := r.cache[cidr]; ok {
		log.Debugf("Checking if CIDR %s has been recently freed. Cooldown expires at: %s. (Cooldown: %v)", cidr, expiry, now.Sub(expiry) < 0)
		return true, now.Sub(expiry) < 0
	}
	return false, false
}

func prometheusRegister() {
	if !prometheusRegistered {
		prometheusmetrics.PrometheusRegister()
		prometheusRegistered = true
	}
}

// containsInsufficientCIDRsOrSubnetIPs returns whether a CIDR cannot be carved in the subnet or subnet is running out of IP addresses
func containsInsufficientCIDRsOrSubnetIPs(err error) bool {
	log.Debugf("containsInsufficientCIDRsOrSubnetIPs encountered %v", err)
	var apiErr smithy.APIError
	// IP exhaustion can be due to Insufficient Cidr blocks or Insufficient Free Address in a Subnet
	// In these 2 cases we will back off for 2 minutes before retrying
	if errors.As(err, &apiErr) {
		log.Debugf("Insufficient IP Addresses due to: %v\n", apiErr.ErrorCode())
		return apiErr.ErrorCode() == INSUFFICIENT_CIDR_BLOCKS || apiErr.ErrorCode() == INSUFFICIENT_FREE_IP_SUBNET
	}
	return false
}

// containsPrivateIPAddressLimitExceededError returns whether exceeds ENI's IP address limit
func containsPrivateIPAddressLimitExceededError(err error) bool {
	log.Debugf("containsPrivateIPAddressLimitExceededError encountered %v", err)
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == "PrivateIpAddressLimitExceeded"
	}
	return false
}

// inInsufficientCidrCoolingPeriod checks whether IPAMD is in insufficientCidrErrorCooldown
func (c *IPAMContext) inInsufficientCidrCoolingPeriod() bool {
	return time.Since(c.lastInsufficientCidrError) <= insufficientCidrErrorCooldown
}

// New retrieves IP address usage information from Instance MetaData service and Kubelet
// then initializes IP address pool data store
func New(k8sClient client.Client, withApiServer bool) (*IPAMContext, error) {
	prometheusRegister()
	c := &IPAMContext{}
	c.k8sClient = k8sClient
	c.networkClient = networkutils.New()
	c.useCustomNetworking = UseCustomNetworkCfg()
	c.manageENIsNonScheduleable = ManageENIsOnNonSchedulableNode()
	c.useSubnetDiscovery = UseSubnetDiscovery()
	c.enablePrefixDelegation = usePrefixDelegation()
	c.enableIPv4 = isIPv4Enabled()
	c.enableIPv6 = isIPv6Enabled()
	c.disableENIProvisioning = disableENIProvisioning()
	client, err := awsutils.New(c.useSubnetDiscovery, c.useCustomNetworking, disableLeakedENICleanup(), c.enableIPv4, c.enableIPv6)
	if err != nil {
		return nil, errors.Wrap(err, "ipamd: can not initialize with AWS SDK interface")
	}
	c.awsClient = client

	c.primaryIP = make(map[string]string)
	c.reconcileCooldownCache.cache = make(map[string]time.Time)
	// WARM and Min IP/Prefix targets are ignored in IPv6 mode
	c.warmENITarget = getWarmENITarget()
	c.warmIPTarget = getWarmIPTarget()
	c.minimumIPTarget = getMinimumIPTarget()
	c.warmPrefixTarget = getWarmPrefixTarget()
	c.enablePodENI = EnablePodENI()
	c.enableManageUntaggedMode = enableManageUntaggedMode()
	c.enablePodIPAnnotation = EnablePodIPAnnotation()
	c.enableMultiNICSupport = enableMultiNICSupport()
	c.networkPolicyMode, err = getNetworkPolicyMode()
	if err != nil {
		return nil, err
	}

	err = c.awsClient.FetchInstanceTypeLimits()
	if err != nil {
		log.Errorf("Failed to get ENI limits from file:vpc_ip_limits or EC2 for %s", c.awsClient.GetInstanceType())
		return nil, err
	}

	// Validate if the configured combination of env variables is supported before proceeding further
	if !c.isConfigValid() {
		return nil, fmt.Errorf("ipamd: failed to validate configuration")
	}

	c.numNetworkCards = len(c.awsClient.GetNetworkCards())
	c.unmanagedENI = make([]int, c.numNetworkCards)
	c.awsClient.InitCachedPrefixDelegation(c.enablePrefixDelegation)
	c.myNodeName = os.Getenv(envNodeName)
	c.withApiServer = withApiServer

	if err := c.nodeInit(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *IPAMContext) nodeInit() error {
	prometheusmetrics.IpamdActionsInprogress.WithLabelValues("nodeInit").Add(float64(1))
	defer prometheusmetrics.IpamdActionsInprogress.WithLabelValues("nodeInit").Sub(float64(1))
	ctx := context.TODO()
	log.Debugf("Start node init")

	if err := c.initENIAndIPLimits(); err != nil {
		return err
	}

	vpcCIDRs, primaryIP, err := c.initNetworkConfig()
	if err != nil {
		return err
	}

	primaryENIMac := c.awsClient.GetPrimaryENImac()

	// For multi-card IPv4, non-VPC traffic is still routed out of Primary ENI
	err = c.networkClient.SetupHostNetwork(vpcCIDRs, primaryENIMac, &primaryIP, c.enablePodENI, c.enableIPv6)
	if err != nil {
		return errors.Wrap(err, "ipamd init: failed to set up host network")
	}

	err = c.networkClient.CleanUpStaleAWSChains(c.enableIPv4, c.enableIPv6)
	if err != nil {
		// We should not error if clean up fails since these chains don't affect the rules
		log.Debugf("Failed to clean up stale AWS chains: %v", err)
	}

	// Queries IMDS for all attached ENIs and then compares it against EC2.
	// Groups the ENIs into different types
	metadataResult, err := c.awsClient.DescribeAllENIs()
	if err != nil {
		return errors.Wrap(err, "ipamd init: failed to retrieve attached ENIs info")
	}
	log.Debugf("DescribeAllENIs success: ENIs: %d, tagged: %d", len(metadataResult.ENIMetadata), len(metadataResult.TagMap))

	c.setUnmanagedENIs(metadataResult.TagMap)
	c.awsClient.SetEFAOnlyENIs(metadataResult.EFAOnlyENIByNetworkCard)
	skipNetworkCards := c.markUnmanagedNetworkCards(metadataResult.EFAOnlyENIByNetworkCard, metadataResult.ENIsByNetworkCard)
	enis := c.filterUnmanagedENIs(metadataResult.ENIMetadata)

	if c.dataStoreAccess == nil {
		c.dataStoreAccess = datastore.InitializeDataStores(skipNetworkCards, dsBackingStorePath(), c.enablePrefixDelegation, log)
	}

	for _, eni := range enis {
		log.Debugf("Discovered ENI %s, trying to set it up", eni.ENIID)
		isTrunkENI := eni.ENIID == metadataResult.TrunkENI
		isEFAENI := metadataResult.EFAENIs[eni.ENIID]
		if !isTrunkENI && !c.disableENIProvisioning {
			if err := c.awsClient.TagENI(eni.ENIID, metadataResult.TagMap[eni.ENIID]); err != nil {
				return errors.Wrapf(err, "ipamd init: failed to tag managed ENI %v", eni.ENIID)
			}
		}

		// Retry ENI sync
		retry := 0
		for {
			retry++
			if err = c.setupENI(eni.ENIID, eni, isTrunkENI, isEFAENI); err == nil {
				log.Infof("ENI %s set up.", eni.ENIID)
				break
			}

			if retry > maxRetryCheckENI {
				log.Warnf("Reached max retry: Unable to discover attached IPs for ENI from metadata service (attempted %d/%d): %v", retry, maxRetryCheckENI, err)
				ipamdErrInc("waitENIAttachedMaxRetryExceeded")
				break
			}

			log.Warnf("Error trying to set up ENI %s: %v", eni.ENIID, err)
			if strings.Contains(err.Error(), "setupENINetwork: failed to find the link which uses MAC address") {
				// If we can't find the matching link for this MAC address, there is no point in retrying for this ENI.
				log.Debug("Unable to match link for this ENI, going to the next one.")
				break
			}
			log.Debugf("Unable to discover IPs for this ENI yet (attempt %d/%d)", retry, maxRetryCheckENI)
			time.Sleep(eniAttachTime)
		}
	}

	// Read from all backing datastores corresponding to the NICs
	if err := c.dataStoreAccess.ReadAllDataStores(c.enableIPv6); err != nil {
		return err
	}

	if c.enableIPv4 {
		if c.enablePrefixDelegation {
			// During upgrade or if prefix delgation knob is disabled to enabled then we
			// might have secondary IPs attached to ENIs so doing a cleanup if not used before moving on
			c.tryUnassignIPsFromENIs()
		} else {
			// When prefix delegation knob is enabled to disabled then we might
			// have unused prefixes attached to the ENIs so need to cleanup
			c.tryUnassignPrefixesFromENIs()
		}
	}

	if err = c.configureIPRulesForPods(); err != nil {
		return err
	}

	// Spawning updateCIDRsRulesOnChange go-routine
	go wait.Forever(func() {
		vpcCIDRs = c.updateCIDRsRulesOnChange(vpcCIDRs)
	}, 30*time.Second)

	// RefreshSGIDs populates the ENI cache with ENI -> security group ID mappings, and so it must be called:
	// 1. after managed/unmanaged ENIs have been determined
	// 2. before any new ENIs are attached
	if !c.disableENIProvisioning {
		if err := c.awsClient.RefreshSGIDs(primaryENIMac, c.dataStoreAccess); err != nil {
			return err
		}

		// Refresh security groups and VPC CIDR blocks in the background
		// Ignoring errors since we will retry in 30s
		go wait.Forever(func() {
			c.awsClient.RefreshSGIDs(primaryENIMac, c.dataStoreAccess)
		}, 30*time.Second)
	}

	// if apiserver is connected, get the maxPods from node
	var node corev1.Node
	if c.withApiServer {
		node, err = k8sapi.GetNode(ctx, c.k8sClient)
		if err != nil {
			log.Errorf("Failed to get node, %s", err)
			podENIErrInc("nodeInit")
			return err
		} else {
			maxPods, isInt64 := node.Status.Capacity.Pods().AsInt64()
			if !isInt64 {
				log.Errorf("Failed to parse max pods: %s", node.Status.Capacity.Pods().String)
				podENIErrInc("nodeInit")
				return errors.New("error while trying to determine max pods")
			}
			c.maxPods = int(maxPods)
		}
	} else {
		maxPods, err := c.getMaxPodsFromFile()
		if err != nil {
			log.Warnf("Using default maxPods as %d because reading from file failed: %v", defaultMaxPodsFromKubelet, err)
			c.maxPods = defaultMaxPodsFromKubelet
		} else {
			c.maxPods = int(maxPods)
		}
	}

	if c.useCustomNetworking {
		// When custom networking is enabled and a valid ENIConfig is found, IPAMD patches the CNINode
		// resource for this instance. The operation is safe as enabling/disabling custom networking
		// requires terminating the previous instance.
		eniConfigName, err := eniconfig.GetNodeSpecificENIConfigName(node)
		if err == nil && eniConfigName != "default" {
			// If Security Groups for Pods is enabled, the VPC Resource Controller must also know that Custom Networking is enabled
			if c.enablePodENI {
				err := c.AddFeatureToCNINode(ctx, rcv1alpha1.CustomNetworking, eniConfigName)
				if err != nil {
					log.Errorf("Failed to add feature custom networking into CNINode", err)
					podENIErrInc("nodeInit")
					return err
				}
				log.Infof("Enabled feature %s in CNINode for node %s if not existing", rcv1alpha1.CustomNetworking, c.myNodeName)
			}
		} else {
			log.Errorf("No ENIConfig could be found for this node", err)
		}
	}

	// Now that Custom Networking is (potentially) enabled, Security Groups for Pods can be enabled for IPv4 nodes.
	if c.enablePodENI {
		c.tryEnableSecurityGroupsForPods(ctx)
	}

	if !c.disableENIProvisioning {
		if c.enableIPv6 {
			// We do not return an error here as create ENI failure shouldn not cause the node to go in not ready state.
			// However that does restrict multi-nic pods to get failed
			c.createSecondaryIPv6ENIs(ctx)
		} else {
			if err = c.handlePreScaling(ctx); err != nil {
				return err
			}
		}
	}

	log.Debug("node init completed successfully")
	return nil
}

func (c *IPAMContext) handlePreScaling(ctx context.Context) error {

	// On node init, check if datastore pool needs to be increased. If so, attach CIDRs from existing ENIs and attach new ENIs.
	for networkCard, decisions := range c.isDatastorePoolTooLow() {
		log.Debugf("is datastore pool low for networkCard %d, decision %t", networkCard, decisions.IsLow)
		if decisions.IsLow {
			if err := c.increaseDatastorePool(ctx, networkCard); err != nil {
				// Note that the only error currently returned by increaseDatastorePool is an error attaching CIDRs (other than insufficient IPs)
				podENIErrInc("nodeInit")
				return errors.New("error while trying to increase datastore pool")
			}
			// If custom networking is enabled and the pool is empty, return an error, as there is a misconfiguration and
			// the node should not become ready.
			if c.useCustomNetworking && c.isDatastorePoolEmpty(networkCard) {
				podENIErrInc("nodeInit")
				return errors.New("Failed to attach any ENIs for custom networking")
			}
		}
	}
	return nil
}

func (c *IPAMContext) configureIPRulesForPods() error {
	rules, err := c.networkClient.GetRuleList(c.enableIPv6)
	if err != nil {
		log.Errorf("During ipamd init: failed to retrieve IP rule list %v", err)
		return nil
	}

	for _, ds := range c.dataStoreAccess.DataStores {
		for _, info := range ds.AllocatedIPs() {
			// TODO(gus): This should really be done via CNI CHECK calls, rather than in ipam (requires upstream k8s changes).
			var mask int
			// Update ip rules in case there is a change in VPC CIDRs, AWS_VPC_K8S_CNI_EXTERNALSNAT setting
			if c.enableIPv6 {
				mask = 128
			} else {
				mask = 32
			}
			srcIPNet := net.IPNet{IP: net.ParseIP(info.IP), Mask: net.CIDRMask(mask, mask)}

			err = c.networkClient.UpdateRuleListBySrc(rules, srcIPNet)
			if err != nil {
				log.Warnf("UpdateRuleListBySrc in nodeInit() failed for IP %s: %v", info.IP, err)
			}
		}
	}

	// Program IP rules for external service CIDRs and cleanup stale rules.
	// Note that we can reuse rule list despite it being modified by UpdateRuleListBySrc, as the
	// modifications touched rules that this function ignores.
	extServiceCIDRs := c.networkClient.GetExternalServiceCIDRs()
	err = c.networkClient.UpdateExternalServiceIpRules(rules, extServiceCIDRs)
	if err != nil {
		log.Warnf("UpdateExternalServiceIpRules in nodeInit() failed")
	}

	return nil
}

func (c *IPAMContext) updateCIDRsRulesOnChange(oldVPCCIDRs []string) []string {
	var newVPCCIDRs []string
	var err error
	var primaryIP net.IP

	if c.enableIPv6 {
		newVPCCIDRs, err = c.awsClient.GetVPCIPv6CIDRs()
		primaryIP = c.awsClient.GetLocalIPv6()
	} else {
		newVPCCIDRs, err = c.awsClient.GetVPCIPv4CIDRs()
		primaryIP = c.awsClient.GetLocalIPv4()
	}

	if err != nil {
		log.Warnf("skipping periodic update to VPC CIDRs due to error: %v", err)
		return oldVPCCIDRs
	}

	old := sets.NewString(oldVPCCIDRs...)
	new := sets.NewString(newVPCCIDRs...)
	if !old.Equal(new) {
		err = c.networkClient.UpdateHostIptablesRules(newVPCCIDRs, c.awsClient.GetPrimaryENImac(), &primaryIP,
			c.enableIPv6)
		if err != nil {
			log.Warnf("unable to update host iptables rules for VPC CIDRs due to error: %v", err)
		}
	}
	return newVPCCIDRs
}

func (c *IPAMContext) updateIPStats(unmanaged int) {
	prometheusmetrics.IpMax.Set(float64(c.maxIPsPerENI * (c.maxENI - unmanaged)))
	prometheusmetrics.EnisMax.Set(float64(c.maxENI - unmanaged))
}

// StartNodeIPPoolManager monitors the IP pool, add or del them when it is required.
func (c *IPAMContext) StartNodeIPPoolManager() {
	// For IPv6, if Security Groups for Pods is enabled, wait until trunk ENI is attached and add it to the datastore.
	if c.enableIPv6 {
		if c.enablePodENI && c.dataStoreAccess.GetDataStore(DefaultNetworkCardIndex).GetTrunkENI() == "" {
			for !c.checkForTrunkENI() {
				time.Sleep(ipPoolMonitorInterval)
			}
		}
		// Outside of Security Groups for Pods, no additional ENIs are attached in IPv6 mode.
		// The prefix used for the primary ENI is more than enough for all pods.
		return
	}

	log.Infof("IP pool manager - max pods: %d, warm IP target: %d, warm prefix target: %d, warm ENI target: %d, minimum IP target: %d",
		c.maxPods, c.warmIPTarget, c.warmPrefixTarget, c.warmENITarget, c.minimumIPTarget)
	sleepDuration := ipPoolMonitorInterval / 2
	ctx := context.Background()
	for {
		if !c.disableENIProvisioning {
			time.Sleep(sleepDuration)
			c.updateIPPoolIfRequired(ctx)
		}
		time.Sleep(sleepDuration)
		c.nodeIPPoolReconcile(ctx, nodeIPPoolReconcileInterval)
	}
}

func (c *IPAMContext) updateIPPoolIfRequired(ctx context.Context) {
	// When IPv4 Security Groups for Pods is configured, do not write to CNINode until there is room for a trunk ENI
	if c.enablePodENI && c.enableIPv4 && c.dataStoreAccess.GetDataStore(DefaultNetworkCardIndex).GetTrunkENI() == "" {
		c.tryEnableSecurityGroupsForPods(ctx)
	}

	isScaleDownExecuted := false
	for networkCard, decisions := range c.isDatastorePoolTooLow() {
		// Each iteration, log the current datastore IP stats
		log.Debugf("IP stats for Network Card %d - total IPs: %d, assigned IPs: %d, cooldown IPs: %d", networkCard, decisions.Stats.TotalIPs, decisions.Stats.AssignedIPs, decisions.Stats.CooldownIPs)
		if decisions.IsLow {
			c.increaseDatastorePool(ctx, networkCard)
		} else if c.isDatastorePoolTooHigh(decisions.Stats, networkCard) && !c.shouldSkipDataStorePoolDecrease() {
			isScaleDownExecuted = true
			c.decreaseDatastorePool(networkCard)
		}
		if c.shouldRemoveExtraENIs(decisions.Stats, networkCard) {
			c.tryFreeENI(networkCard)
		}
	}

	// Store the last update time so that we only scale down every
	if isScaleDownExecuted {
		now := time.Now()
		c.lastDecreaseIPPool = now
		c.lastNodeIPPoolAction = now
	}
}

func (c *IPAMContext) shouldSkipDataStorePoolDecrease() bool {

	timeSinceLastPoolDecrease := time.Since(c.lastDecreaseIPPool)

	if timeSinceLastPoolDecrease <= decreaseIPPoolInterval {
		log.Debugf("Skipping decrease Datastore pool because time since last %v <= %v", timeSinceLastPoolDecrease, decreaseIPPoolInterval)
		return true
	}

	return false
}

// decreaseDatastorePool runs every `interval` and attempts to return unused ENIs and IPs
func (c *IPAMContext) decreaseDatastorePool(networkCard int) {
	prometheusmetrics.IpamdActionsInprogress.WithLabelValues("decreaseDatastorePool").Add(float64(1))
	defer prometheusmetrics.IpamdActionsInprogress.WithLabelValues("decreaseDatastorePool").Sub(float64(1))

	log.Debugf("Starting to decrease pool size for network card %d", networkCard)
	c.tryUnassignCidrsFromAll(networkCard)

	log.Debugf("Successfully decreased IP pool")
	c.logPoolStats(c.dataStoreAccess.GetDataStore(networkCard).GetIPStats(ipV4AddrFamily), networkCard)
}

// tryFreeENI always tries to free one ENI
func (c *IPAMContext) tryFreeENI(networkCard int) {
	if c.isTerminating() {
		log.Debug("AWS CNI is terminating, not detaching any ENIs")
		return
	}

	if !c.manageENIsNonScheduleable && c.isNodeNonSchedulable() {
		log.Debug("AWS CNI is on a non schedulable node, not detaching any ENIs")
		return
	}
	warmIPTarget := c.warmIPTarget
	minimumIPTarget := c.minimumIPTarget

	if networkCard > DefaultNetworkCardIndex {
		warmIPTarget = DefaultWarmIPTarget
		minimumIPTarget = DefaultMinimumIPTarget
	}

	eni := c.dataStoreAccess.GetDataStore(networkCard).RemoveUnusedENIFromStore(warmIPTarget, minimumIPTarget, c.warmPrefixTarget)
	if eni == "" {
		return
	}

	log.Debugf("Start freeing ENI %s from network card %d", eni, networkCard)
	err := c.awsClient.FreeENI(eni)
	if err != nil {
		ipamdErrInc("decreaseIPPoolFreeENIFailed")
		log.Errorf("Failed to free ENI %s, err: %v", eni, err)
		// If the ENI attachment not found or cannot be detached we return without deleting the primary IP rules
		if errors.Is(err, awsutils.ErrENIAttachmentIdNotFound) || errors.Is(err, awsutils.ErrUnableToDetachENI) {
			return
		}
	}

	err = c.networkClient.DeleteRulesBySrc(c.primaryIP[eni], c.enableIPv6)
	if err != nil {
		log.Errorf("Failed to delete rules for Primary IP of ENI err: %v", err)
		return
	}
}

// When warm IP/prefix targets are defined, free extra IPs
func (c *IPAMContext) tryUnassignCidrsFromAll(networkCard int) {
	_, over, warmIPTargetsDefined := c.datastoreTargetState(nil, networkCard)
	// If WARM IP targets are not defined, check if WARM_PREFIX_TARGET is defined.
	if !warmIPTargetsDefined {
		over = c.computeExtraPrefixesOverWarmTarget(networkCard)
	}

	if over > 0 {
		eniInfos := c.dataStoreAccess.GetDataStore(networkCard).GetENIInfos()
		for eniID := range eniInfos.ENIs {
			// Either returns prefixes or IPs [Cidrs]
			cidrs := c.dataStoreAccess.GetDataStore(networkCard).FindFreeableCidrs(eniID)
			if cidrs == nil {
				log.Errorf("Error finding unassigned IPs for ENI %s", eniID)
				continue
			}

			// Free the number of Cidrs `over` the warm IP target, unless `over` is greater than the number of available Cidrs on
			// this ENI. In that case we should only free the number of available Cidrs.
			numFreeable := min(over, len(cidrs))
			cidrs = cidrs[:numFreeable]
			if len(cidrs) == 0 {
				continue
			}

			// Delete IPs from datastore
			var deletedCidrs []datastore.CidrInfo
			for _, toDelete := range cidrs {
				// Do not force the delete, since a freeable Cidr might have been assigned to a pod
				// before we get around to deleting it.
				err := c.dataStoreAccess.GetDataStore(networkCard).DelIPv4CidrFromStore(eniID, toDelete.Cidr, false /* force */)
				if err != nil {
					log.Warnf("Failed to delete Cidr %s on ENI %s from datastore for network card %d: %s", toDelete, eniID, networkCard, err)
					ipamdErrInc("decreaseIPPool")
					continue
				} else {
					deletedCidrs = append(deletedCidrs, toDelete)
				}
			}

			// Deallocate Cidrs from the instance if they are not used by pods.
			c.DeallocCidrs(eniID, deletedCidrs)

			// reduce the deallocation target, if the deallocation target is achieved, we can exit
			if over = over - len(deletedCidrs); over <= 0 {
				break
			}
		}
	}
}

func (c *IPAMContext) initNetworkConfig() ([]string, net.IP, error) {
	if c.enableIPv4 {
		primaryIP := c.awsClient.GetLocalIPv4()
		vpcCIDRs, err := c.awsClient.GetVPCIPv4CIDRs()
		return vpcCIDRs, primaryIP, err
	}

	primaryIP := c.awsClient.GetLocalIPv6()
	vpcCIDRs, err := c.awsClient.GetVPCIPv6CIDRs()
	return vpcCIDRs, primaryIP, err
}

func (c *IPAMContext) isENIAttachmentAllowed() bool {
	if c.isTerminating() {
		log.Debug("AWS CNI is terminating, will not try to attach any new IPs or ENIs right now")
		return false
	}
	if !c.manageENIsNonScheduleable && c.isNodeNonSchedulable() {
		log.Debug("AWS CNI is on a non schedulable node, will not try to attach any new IPs or ENIs right now")
		return false
	}
	return true
}

// PRECONDITION: isDatastorePoolTooLow returned true
func (c *IPAMContext) increaseDatastorePool(ctx context.Context, networkCard int) error {
	log.Debugf("Starting to increase pool size for network card %d", networkCard)
	prometheusmetrics.IpamdActionsInprogress.WithLabelValues("increaseDatastorePool").Add(float64(1))
	defer prometheusmetrics.IpamdActionsInprogress.WithLabelValues("increaseDatastorePool").Sub(float64(1))

	if !c.isENIAttachmentAllowed() {
		return nil
	}

	// Try to add more Cidrs to existing ENIs first.
	if c.inInsufficientCidrCoolingPeriod() {
		log.Debugf("Recently we had InsufficientCidr error hence will wait for %v before retrying", insufficientCidrErrorCooldown)
		return nil
	}

	increasedPool, err := c.tryAssignCidrs(networkCard)
	if err != nil {
		if containsInsufficientCIDRsOrSubnetIPs(err) {
			log.Errorf("Unable to attach IPs/Prefixes for the ENI, subnet doesn't seem to have enough IPs/Prefixes. Consider using new subnet or carve a reserved range using create-subnet-cidr-reservation")
			c.lastInsufficientCidrError = time.Now()
			return nil
		}
		log.Errorf(err.Error())
		return err
	}
	if increasedPool {
		c.updateLastNodeIPPoolAction(networkCard)
	} else {
		// If we did not add any IPs, try to allocate an ENI.
		if c.hasRoomForEni(networkCard) {
			if err = c.tryAllocateENI(ctx, networkCard); err == nil {
				c.updateLastNodeIPPoolAction(networkCard)
			} else {
				// Note that no error is returned if ENI allocation fails. This is because ENI allocation failure should not cause node to be "NotReady".
				log.Debugf("Error trying to allocate ENI: %v", err)
			}
		} else {
			log.Debugf("Skipping ENI allocation as the max ENI limit is already reached")
		}
	}
	return nil
}

func (c *IPAMContext) createSecondaryIPv6ENIs(ctx context.Context) error {

	log.Info("Trying to see if new ENIs need to be created")
	if !c.isENIAttachmentAllowed() {
		return nil
	}

	log.Info("Attaching ENIs allowed")

	for _, ds := range c.dataStoreAccess.DataStores {
		networkCard := ds.GetNetworkCard()
		if networkCard == DefaultNetworkCardIndex {
			continue
		}

		log.Infof("Checking for DS %d", networkCard)
		if ds.GetENIs() == 0 {
			// This means we have not added an ENI before as it would have been detected ENI detection
			log.Infof("Attaching ENI to Network Card %d, MaxENI :%d", networkCard, c.maxENI)
			if c.hasRoomForEni(networkCard) {
				if err := c.tryAllocateENI(ctx, networkCard); err == nil {
					log.Infof("Allocated ENI successfully to Network Card %d", networkCard)
				} else {
					// Note that no error is returned if ENI allocation fails. This is because ENI allocation failure should not cause node to be "NotReady".
					log.Debugf("Error trying to allocate ENI: %v", err)
				}
			} else {
				log.Debugf("Skipping ENI allocation as the max ENI limit is already reached for the instance")
			}
		}
	}
	return nil
}

func (c *IPAMContext) updateLastNodeIPPoolAction(networkCard int) {
	c.lastNodeIPPoolAction = time.Now()
	stats := c.dataStoreAccess.GetDataStore(networkCard).GetIPStats(ipV4AddrFamily)
	c.logPoolStats(stats, networkCard)
}

func (c *IPAMContext) tryAllocateENI(ctx context.Context, networkCard int) error {
	var securityGroups []*string
	var eniCfgSubnet string

	if c.useCustomNetworking {
		eniCfg, err := eniconfig.MyENIConfig(ctx, c.k8sClient)
		if err != nil {
			log.Errorf("Failed to get pod ENI config")
			return err
		}

		log.Infof("ipamd: using custom network config: %v, %s", eniCfg.SecurityGroups, eniCfg.Subnet)
		for _, sgID := range eniCfg.SecurityGroups {
			log.Debugf("Found security-group id: %s", sgID)
			securityGroups = append(securityGroups, aws.String(sgID))
		}
		eniCfgSubnet = eniCfg.Subnet
	}

	resourcesToAllocate := c.GetENIResourcesToAllocate(networkCard)
	if resourcesToAllocate > 0 {
		eni, err := c.awsClient.AllocENI(securityGroups, eniCfgSubnet, resourcesToAllocate, networkCard)
		if err != nil {
			log.Errorf("Failed to increase pool size due to not able to allocate ENI %v", err)
			ipamdErrInc("increaseIPPoolAllocENI")
			// Get current ENI diagnostics
      eniCount := len(c.dataStoreAccess.GetDataStore(networkCard).GetAllocatableENIs(c.maxIPsPerENI, c.useCustomNetworking))
    log.Warnf("Failed to allocate %d IP addresses on an ENI: %v. ENI count: %d/%d, Check ENI limits and subnet capacity", 
       resourcesToAllocate, err, eniCount, c.maxENI)
	  		if containsInsufficientCIDRsOrSubnetIPs(err) {
				ipamdErrInc("increaseIPPoolAllocIPAddressesFailed")
				log.Errorf("Unable to attach IPs/Prefixes for the ENI, subnet doesn't seem to have enough IPs/Prefixes. Consider using new subnet or carve a reserved range using create-subnet-cidr-reservation")
				c.lastInsufficientCidrError = time.Now()
			}
			return err
		}

		eniMetadata, err := c.awsClient.WaitForENIAndIPsAttached(eni, resourcesToAllocate)
		if err != nil {
			ipamdErrInc("increaseIPPoolwaitENIAttachedFailed")
			log.Errorf("Failed to increase pool size for Network card %d: Unable to discover attached ENI from metadata service %v", networkCard, err)
			return err
		}
		// The CNI does not create trunk or EFA ENIs, so they will always be false here
		err = c.setupENI(eni, eniMetadata, false, false)
		if err != nil {
			ipamdErrInc("increaseIPPoolsetupENIFailed")
			log.Errorf("Failed to increase pool size for network card %d: %v", networkCard, err)
			return err
		}
	} else {
		log.Debugf("Did not allocate ENI since IPs/Prefixes needed were not greater than 0. IPs/Prefixes needed: %d", resourcesToAllocate)
	}
	return nil
}

// For an ENI, fill in missing IPs or prefixes.
// PRECONDITION: isDatastorePoolTooLow returned true
func (c *IPAMContext) tryAssignCidrs(networkCard int) (increasedPool bool, err error) {
	if c.enablePrefixDelegation {
		return c.tryAssignPrefixes(networkCard)
	} else {
		return c.tryAssignIPs(networkCard)
	}
}

// For an ENI, try to fill in missing IPs on an existing ENI.
// PRECONDITION: isDatastorePoolTooLow returned true
func (c *IPAMContext) tryAssignIPs(networkCard int) (increasedPool bool, err error) {

	// If WARM_IP_TARGET is set, only proceed if we are short of target
	short, _, warmIPTargetsDefined := c.datastoreTargetState(nil, networkCard)
	if warmIPTargetsDefined && short == 0 {
		return false, nil
	}

	// If WARM_IP_TARGET is set we only want to allocate up to that target to avoid overallocating and releasing
	toAllocate := c.maxIPsPerENI
	if warmIPTargetsDefined {
		toAllocate = short
	}

	// Find an ENI where we can add more IPs
	enis := c.dataStoreAccess.GetDataStore(networkCard).GetAllocatableENIs(c.maxIPsPerENI, c.useCustomNetworking)
	for _, eni := range enis {
		if len(eni.AvailableIPv4Cidrs) < c.maxIPsPerENI {
			currentNumberOfAllocatedIPs := len(eni.AvailableIPv4Cidrs)
			// Try to allocate all available IPs for this ENI
			resourcesToAllocate := min((c.maxIPsPerENI - currentNumberOfAllocatedIPs), toAllocate)
			output, err := c.awsClient.AllocIPAddresses(eni.ID, resourcesToAllocate)
			if err != nil && !containsPrivateIPAddressLimitExceededError(err) {
     log.Warnf("failed to allocate IP address on ENI %s: %v. Current usage: %d/%d IPs allocated in subnet. ENI limit: %d, Available subnet IPs: %d. Fragmentation check: %d available IPs across ENIs", 
        eni.ID, err, currentIPCount, totalSubnetIPs, eniLimit, availableSubnetIPs, len(eni.AvailableIPv4Cidrs))              eni.ID, err, currentIPCount, totalSubnetIPs, eniLimit, availableSubnetIPs)
				// Try to just get one more IP
				output, err = c.awsClient.AllocIPAddresses(eni.ID, 1)
				if err != nil && !containsPrivateIPAddressLimitExceededError(err) {
					ipamdErrInc("increaseIPPoolAllocIPAddressesFailed")
					if c.useSubnetDiscovery && containsInsufficientCIDRsOrSubnetIPs(err) {
						continue
					}
					 // Add prefix delegation diagnostics
                    availablePrefixes := 0
                    if eniMetadata != nil {
                    availablePrefixes = len(eniMetadata.IPv6Prefixes)
                }
                 return errors.Wrapf(err, "Failed to allocate IPv6 Prefixes to ENI. Available prefixes: %d, Trunk ENI mode: %v", 
              availablePrefixes, !isTrunkENI)
				}
			}

			var ec2ip4s []ec2types.NetworkInterfacePrivateIpAddress
			if containsPrivateIPAddressLimitExceededError(err) {
				log.Debug("AssignPrivateIpAddresses returned PrivateIpAddressLimitExceeded. This can happen if the data store is out of sync." +
					"Returning without an error here since we will verify the actual state by calling EC2 to see what addresses have already assigned to this ENI.")
				// This call to EC2 is needed to verify which IPs got attached to this ENI.
				ec2ip4s, err = c.awsClient.GetIPv4sFromEC2(eni.ID)
				if err != nil {
					ipamdErrInc("increaseIPPoolGetENIaddressesFailed")
					return true, errors.Wrap(err, "failed to get ENI IP addresses during IP allocation")
				}
			} else {
				if output == nil {
					ipamdErrInc("increaseIPPoolGetENIaddressesFailed")
					return true, errors.Wrap(err, "failed to get ENI IP addresses during IP allocation")
				}

				ec2Addrs := output.AssignedPrivateIpAddresses
				for _, ec2Addr := range ec2Addrs {
					ec2ip4s = append(ec2ip4s, ec2types.NetworkInterfacePrivateIpAddress{PrivateIpAddress: ec2Addr.PrivateIpAddress})
				}
			}
			c.addENIsecondaryIPsToDataStore(ec2ip4s, eni.ID, networkCard)
			return true, nil
		}
	}
	return false, nil
}

func (c *IPAMContext) assignIPv6Prefix(eniID string, networkCard int) (err error) {
	log.Debugf("Assigning an IPv6Prefix for ENI: %s", eniID)
	//Let's make an EC2 API call to get a list of IPv6 prefixes (if any) that are already attached to the
	//current ENI. We will make this call only once during boot up/init and doing so will shield us from any
	//IMDS out of sync issues. We only need one v6 prefix per ENI/Node.
	ec2v6Prefixes, err := c.awsClient.GetIPv6PrefixesFromEC2(eniID)
	if err != nil {
		log.Errorf("assignIPv6Prefix; err: %s", err)
		return err
	}
	log.Debugf("ENI %s has %v prefixe(s) attached", eniID, len(ec2v6Prefixes))

	//Note: If we find more than one v6 prefix attached to the ENI, VPC CNI will not attempt to free it. VPC CNI
	//will only attach a single v6 prefix and it will not attempt to free the additional Prefixes.
	//We will add all the prefixes to our datastore. TODO - Should we instead pick one of them. If we do, how to track
	//that across restarts?

	//Check if we already have v6 Prefix(es) attached
	if len(ec2v6Prefixes) == 0 {
		//Allocate and attach a v6 Prefix to Primary ENI
		log.Debugf("No IPv6 Prefix(es) found for ENI: %s", eniID)
		strPrefixes, err := c.awsClient.AllocIPv6Prefixes(eniID)
		if err != nil {
			return err
		}
		for _, v6Prefix := range strPrefixes {
			ec2v6Prefixes = append(ec2v6Prefixes, ec2types.Ipv6PrefixSpecification{Ipv6Prefix: v6Prefix})
		}
		log.Debugf("Successfully allocated an IPv6Prefix for ENI: %s", eniID)
	} else if len(ec2v6Prefixes) > 1 {
		//Found more than one v6 prefix attached to the ENI. VPC CNI will only attach a single v6 prefix
		//and it will not attempt to free any additional Prefixes that are already attached.
		//Will use the first IPv6 Prefix attached for IP address allocation.
		ec2v6Prefixes = []ec2types.Ipv6PrefixSpecification{ec2v6Prefixes[0]}
	}
	c.addENIv6prefixesToDataStore(ec2v6Prefixes, eniID, networkCard)
	return nil
}

// PRECONDITION: isDatastorePoolTooLow returned true
func (c *IPAMContext) tryAssignPrefixes(networkCard int) (increasedPool bool, err error) {
	toAllocate := c.getPrefixesNeeded(networkCard)
	// Returns an ENI which has space for more prefixes to be attached, but this
	// ENI might not suffice the WARM_IP_TARGET/WARM_PREFIX_TARGET
	enis := c.dataStoreAccess.GetDataStore(networkCard).GetAllocatableENIs(c.maxPrefixesPerENI, c.useCustomNetworking)
	for _, eni := range enis {
		currentNumberOfAllocatedPrefixes := len(eni.AvailableIPv4Cidrs)
		resourcesToAllocate := min((c.maxPrefixesPerENI - currentNumberOfAllocatedPrefixes), toAllocate)
		output, err := c.awsClient.AllocIPAddresses(eni.ID, resourcesToAllocate)
		if err != nil && !containsPrivateIPAddressLimitExceededError(err) {
			log.Warnf("failed to allocate all available IPv4 Prefixes on ENI %s, err: %v", eni.ID, err)
			// Try to just get one more prefix
			output, err = c.awsClient.AllocIPAddresses(eni.ID, 1)
			if err != nil && !containsPrivateIPAddressLimitExceededError(err) {
				ipamdErrInc("increaseIPPoolAllocIPAddressesFailed")
				if c.useSubnetDiscovery && containsInsufficientCIDRsOrSubnetIPs(err) {
					continue
				}
     // Add IPv4 prefix diagnostics
       currentPrefixes := len(eni.IPv4Prefixes)
    return false, errors.Wrapf(err, fmt.Sprintf("failed to allocate one IPv4 prefix on ENI %s, err: %v. Current prefixes: %d, Max prefixes per ENI: %d", 
    eni.ID, err, currentPrefixes, c.maxPrefixesPerENI))			}
		}

		var ec2Prefixes []ec2types.Ipv4PrefixSpecification
		if containsPrivateIPAddressLimitExceededError(err) {
			log.Debug("AssignPrivateIpAddresses returned PrivateIpAddressLimitExceeded. This can happen if the data store is out of sync." +
				"Returning without an error here since we will verify the actual state by calling EC2 to see what addresses have already assigned to this ENI.")
			// This call to EC2 is needed to verify which IPs got attached to this ENI.
			ec2Prefixes, err = c.awsClient.GetIPv4PrefixesFromEC2(eni.ID)
			if err != nil {
				ipamdErrInc("increaseIPPoolGetENIaddressesFailed")
				return true, errors.Wrap(err, "failed to get ENI IP addresses during IP allocation")
			}
		} else {
			if output == nil {
				ipamdErrInc("increaseIPPoolGetENIprefixedFailed")
				return true, errors.Wrap(err, "failed to get ENI Prefix addresses during IPv4 Prefix allocation")
			}
			ec2Prefixes = output.AssignedIpv4Prefixes
		}
		c.addENIv4prefixesToDataStore(ec2Prefixes, eni.ID, networkCard)
		return true, nil
	}
	return false, nil
}

// setupENI does following:
// 1) add ENI to datastore
// 2) set up linux ENI related networking stack.
// 3) add all ENI's secondary IP addresses to datastore

// TODO: Will primary ENI so that we can skip Network Setup ?
func (c *IPAMContext) setupENI(eni string, eniMetadata awsutils.ENIMetadata, isTrunkENI, isEFAENI bool) error {
	primaryENI := c.awsClient.GetPrimaryENI()
	// Add the ENI to the datastore
	err := c.dataStoreAccess.GetDataStore(eniMetadata.NetworkCard).AddENI(eni, eniMetadata.DeviceNumber, eni == primaryENI, isTrunkENI, isEFAENI)
	if err != nil && err.Error() != datastore.DuplicatedENIError {
		return errors.Wrapf(err, "failed to add ENI %s to data store", eni)
	}

	// Store the addressable IP for the ENI.
	if c.enableIPv6 {
		c.primaryIP[eni] = eniMetadata.PrimaryIPv6Address()
	} else {
		c.primaryIP[eni] = eniMetadata.PrimaryIPv4Address()
	}

	if c.enableIPv6 {
		if !isTrunkENI {
			// In v6 PD mode, VPC CNI will manage the primary ENI, ENIs on NC > 0 and trunk ENI. Once we start supporting secondary
			// IP and custom networking modes for IPv6, this restriction can be relaxed.
			err := c.assignIPv6Prefix(eni, eniMetadata.NetworkCard)
			if err != nil {
				// Add prefix delegation diagnostics
        availablePrefixes := 0
     if eniMetadata != nil && eniMetadata.IPv6Prefixes != nil {
    availablePrefixes = len(eniMetadata.IPv6Prefixes)
     } 
return errors.Wrapf(err, "Failed to allocate IPv6 Prefixes to ENI. Available prefixes: %d, Trunk ENI mode: %v, PD mode active", 
    availablePrefixes, !isTrunkENI)
			}
		}
	}

	// For other ENIs, set up the network
	if eni != primaryENI {
		subnetCidr := eniMetadata.SubnetIPv4CIDR
		if c.enableIPv6 {
			subnetCidr = eniMetadata.SubnetIPv6CIDR
		}
		err = c.networkClient.SetupENINetwork(c.primaryIP[eni], eniMetadata.MAC, eniMetadata.DeviceNumber, eniMetadata.NetworkCard, subnetCidr, c.maxENI, isTrunkENI)
		if err != nil {
			// Failed to set up the ENI
			errRemove := c.dataStoreAccess.GetDataStore(eniMetadata.NetworkCard).RemoveENIFromDataStore(eni, true)
			if errRemove != nil {
				log.Warnf("failed to remove ENI %s: %v", eni, errRemove)
			}
			delete(c.primaryIP, eni)
			return errors.Wrapf(err, "failed to set up ENI %s network", eni)
		}
	}

	if !c.enableIPv6 {
		log.Infof("Found ENIs having %d secondary IPs and %d Prefixes", len(eniMetadata.IPv4Addresses), len(eniMetadata.IPv4Prefixes))
		// Either case add the IPs and prefixes to datastore.
		c.addENIsecondaryIPsToDataStore(eniMetadata.IPv4Addresses, eni, eniMetadata.NetworkCard)
		c.addENIv4prefixesToDataStore(eniMetadata.IPv4Prefixes, eni, eniMetadata.NetworkCard)
	} else {
		log.Infof("Found IPv6 ENI %s having %d secondary IPs and %d Prefixes and isTrunk: %t", eni, len(eniMetadata.IPv6Addresses), len(eniMetadata.IPv6Prefixes), isTrunkENI)
	}

	return nil
}

func (c *IPAMContext) addENIsecondaryIPsToDataStore(ec2PrivateIpAddrs []ec2types.NetworkInterfacePrivateIpAddress, eni string, networkCard int) {
	// Add all the secondary IPs
	for _, ec2PrivateIpAddr := range ec2PrivateIpAddrs {
		if aws.ToBool(ec2PrivateIpAddr.Primary) {
			continue
		}
		cidr := net.IPNet{IP: net.ParseIP(aws.ToString(ec2PrivateIpAddr.PrivateIpAddress)), Mask: net.IPv4Mask(255, 255, 255, 255)}
		err := c.dataStoreAccess.GetDataStore(networkCard).AddIPv4CidrToStore(eni, cidr, false)
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Warnf("Failed to increase IP pool, failed to add IP %s to data store for network card %d", ec2PrivateIpAddr.PrivateIpAddress, networkCard)
			// continue to add next address
			ipamdErrInc("addENIsecondaryIPsToDataStoreFailed")
		}
	}
	c.logPoolStats(c.dataStoreAccess.GetDataStore(networkCard).GetIPStats(ipV4AddrFamily), networkCard)
}

func (c *IPAMContext) addENIv4prefixesToDataStore(ec2PrefixAddrs []ec2types.Ipv4PrefixSpecification, eni string, networkCard int) {
	// Walk thru all prefixes
	for _, ec2PrefixAddr := range ec2PrefixAddrs {
		strIpv4Prefix := aws.ToString(ec2PrefixAddr.Ipv4Prefix)
		_, ipnet, err := net.ParseCIDR(strIpv4Prefix)
		if err != nil {
			//Parsing failed, get next prefix
			log.Debugf("Parsing failed, moving on to next prefix")
			continue
		}
		cidr := *ipnet
		err = c.dataStoreAccess.GetDataStore(networkCard).AddIPv4CidrToStore(eni, cidr, true)
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Warnf("Failed to increase Prefix pool, failed to add Prefix %s to data store for network card %d", ec2PrefixAddr.Ipv4Prefix, networkCard)
			// continue to add next address
			ipamdErrInc("addENIv4prefixesToDataStoreFailed")
		}
	}
	c.logPoolStats(c.dataStoreAccess.GetDataStore(networkCard).GetIPStats(ipV4AddrFamily), networkCard)
}

func (c *IPAMContext) addENIv6prefixesToDataStore(ec2PrefixAddrs []ec2types.Ipv6PrefixSpecification, eni string, networkCard int) {
	log.Debugf("Updating datastore with IPv6Prefix(es) for ENI: %v, count: %v", eni, len(ec2PrefixAddrs))
	// Walk through all prefixes
	for _, ec2PrefixAddr := range ec2PrefixAddrs {
		strIpv6Prefix := aws.ToString(ec2PrefixAddr.Ipv6Prefix)
		_, ipnet, err := net.ParseCIDR(strIpv6Prefix)
		if err != nil {
			// Parsing failed, get next prefix
			log.Debugf("Parsing failed, moving on to next prefix")
			continue
		}
		cidr := *ipnet
		err = c.dataStoreAccess.GetDataStore(networkCard).AddIPv6CidrToStore(eni, cidr, true)
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Warnf("Failed to increase Prefix pool, failed to add Prefix %s to data store for network card %d", ec2PrefixAddr.Ipv6Prefix, networkCard)
			// continue to add next address
			ipamdErrInc("addENIv6prefixesToDataStoreFailed")
		}
		c.logPoolStats(c.dataStoreAccess.GetDataStore(networkCard).GetIPStats(ipV6AddrFamily), networkCard)
	}

}

// getMaxENI returns the maximum number of ENIs to attach to this instance. This is calculated as the lesser of
// the limit for the instance type and the value configured via the MAX_ENI environment variable. If the value of
// the environment variable is 0 or less, it will be ignored and the maximum for the instance is returned.
func (c *IPAMContext) getMaxENI() (int, error) {
	instanceMaxENI := c.awsClient.GetENILimit()

	inputStr, found := os.LookupEnv(envMaxENI)
	envMax := defaultMaxENI
	if found {
		if input, err := strconv.Atoi(inputStr); err == nil && input >= 1 {
			log.Debugf("Using MAX_ENI %v", input)
			envMax = input
		}
	}

	if envMax >= 1 && envMax < instanceMaxENI {
		return envMax, nil
	}
	return instanceMaxENI, nil
}

func getWarmENITarget() int {
	inputStr, found := os.LookupEnv(envWarmENITarget)

	if !found {
		return defaultWarmENITarget
	}

	if input, err := strconv.Atoi(inputStr); err == nil {
		if input < 0 {
			return defaultWarmENITarget
		}
		log.Debugf("Using WARM_ENI_TARGET %v", input)
		return input
	}
	return defaultWarmENITarget
}

func getWarmPrefixTarget() int {
	inputStr, found := os.LookupEnv(envWarmPrefixTarget)

	if !found {
		return defaultWarmPrefixTarget
	}

	if input, err := strconv.Atoi(inputStr); err == nil {
		if input < 0 {
			return defaultWarmPrefixTarget
		}
		log.Debugf("Using WARM_PREFIX_TARGET %v", input)
		return input
	}
	return defaultWarmPrefixTarget
}

// logPoolStats logs usage information for allocated addresses/prefixes.
func (c *IPAMContext) logPoolStats(dataStoreStats *datastore.DataStoreStats, networkCard int) {
	prefix := "IP pool stats for network card"
	if c.enablePrefixDelegation {
		prefix = "Prefix pool stats for network card"
	}
	log.Debugf("%s %d: %s, c.maxIPsPerENI = %d", prefix, networkCard, dataStoreStats, c.maxIPsPerENI)
}

func (c *IPAMContext) tryEnableSecurityGroupsForPods(ctx context.Context) {

	// For IPv4, check that there is room for a trunk ENI before patching CNINode CRD. We only check on the Default Network Card
	if c.enableIPv4 && (c.dataStoreAccess.GetDataStore(DefaultNetworkCardIndex).GetENIs() >= (c.maxENI - c.unmanagedENI[DefaultNetworkCardIndex])) {
		log.Error("No slot available for a trunk ENI to be attached.")
		return
	}

	// Signal to the VPC Resource Controller that Security Groups for Pods is enabled
	err := c.AddFeatureToCNINode(ctx, rcv1alpha1.SecurityGroupsForPods, "")
	if err != nil {
		podENIErrInc("tryEnableSecurityGroupsForPods")
		log.Errorf("Failed to add SGP feature to CNINode resource", err)
	} else {
		log.Infof("Successfully added feature %s to CNINode if not existing", rcv1alpha1.SecurityGroupsForPods)
	}
}

// shouldRemoveExtraENIs returns true if we should attempt to find an ENI to free
// PD enabled: If the WARM_PREFIX_TARGET is spread across ENIs and we have more than needed, this function will return true.
// If the number of prefixes are on just one ENI, and there are more than available, it returns true so getDeletableENI will
// recheck if we need the ENI for prefix target.
func (c *IPAMContext) shouldRemoveExtraENIs(stats *datastore.DataStoreStats, networkCard int) bool {
	// When WARM_IP_TARGET is set, return true as verification is always done in getDeletableENI()
	if c.warmIPTargetsDefined() || networkCard > DefaultNetworkCardIndex {
		return true
	}

	available := stats.AvailableAddresses()
	var shouldRemoveExtra bool

	// We need the +1 to make sure we are not going below the WARM_ENI_TARGET/WARM_PREFIX_TARGET
	warmTarget := (c.warmENITarget + 1)

	if c.enablePrefixDelegation {
		warmTarget = (c.warmPrefixTarget + 1)
	}

	shouldRemoveExtra = available >= (warmTarget)*c.maxIPsPerENI
	if shouldRemoveExtra {
		c.logPoolStats(stats, networkCard)
		log.Debugf("It might be possible to remove extra ENIs because available (%d) >= (ENI/Prefix target + 1 (%d) + 1) * addrsPerENI (%d)", available, warmTarget, c.maxIPsPerENI)
	} else if c.enablePrefixDelegation {
		// When prefix target count is reduced, datastore would have deleted extra prefixes over the warm prefix target.
		// Hence available will be less than (warmTarget)*c.maxIPsPerENI, but there can be some extra ENIs which are not used hence see if we can clean it up.
		shouldRemoveExtra = c.dataStoreAccess.GetDataStore(networkCard).CheckFreeableENIexists()
	}
	return shouldRemoveExtra
}

func (c *IPAMContext) computeExtraPrefixesOverWarmTarget(networkCard int) int {
	if !c.warmPrefixTargetDefined() {
		return 0
	}

	freePrefixes := c.dataStoreAccess.GetDataStore(networkCard).GetFreePrefixes()
	over := max(freePrefixes-c.warmPrefixTarget, 0)

	stats := c.dataStoreAccess.GetDataStore(networkCard).GetIPStats(ipV4AddrFamily)
	log.Debugf("computeExtraPrefixesOverWarmTarget - available: %d, over: %d, warm_prefix_target: %d", stats.AvailableAddresses(), over, c.warmPrefixTarget)
	c.logPoolStats(stats, networkCard)
	return over
}

func ipamdErrInc(fn string) {
	prometheusmetrics.IpamdErr.With(prometheus.Labels{"fn": fn}).Inc()
}

func podENIErrInc(fn string) {
	prometheusmetrics.PodENIErr.With(prometheus.Labels{"fn": fn}).Inc()
}

// Used in IPv6 mode to check if trunk ENI has been successfully attached
func (c *IPAMContext) checkForTrunkENI() bool {
	metadataResult, err := c.awsClient.DescribeAllENIs()
	if err != nil {
		log.Debug("failed to describe attached ENIs")
		return false
	}
	if metadataResult.TrunkENI != "" {
		for _, eni := range metadataResult.ENIMetadata {
			if eni.ENIID == metadataResult.TrunkENI {
				if err := c.setupENI(eni.ENIID, eni, true, false); err == nil {
					log.Infof("ENI %s set up", eni.ENIID)
					return true
				} else {
					log.Debugf("failed to setup ENI %s: %v", eni.ENIID, err)
					return false
				}
			}
		}
	}
	return false
}

func (c *IPAMContext) getENIsByNetworkCard(allENIs []awsutils.ENIMetadata) map[int][]awsutils.ENIMetadata {
	var eniNetworkCardMap = make(map[int][]awsutils.ENIMetadata, c.numNetworkCards)
	for _, eni := range allENIs {
		eniNetworkCardMap[eni.NetworkCard] = append(eniNetworkCardMap[eni.NetworkCard], eni)
	}
	return eniNetworkCardMap
}

// nodeIPPoolReconcile reconcile ENI and IP info from metadata service and IP addresses in datastore
func (c *IPAMContext) nodeIPPoolReconcile(ctx context.Context, interval time.Duration) {
	// To reduce the number of EC2 API calls, skip reconciliation if IPs were recently added to the datastore.
	timeSinceLast := time.Since(c.lastNodeIPPoolAction)
	// Make an exception if node needs a trunk ENI and one is not currently attached.
	needsTrunkEni := c.enablePodENI && c.dataStoreAccess.GetDataStore(DefaultNetworkCardIndex).GetTrunkENI() == ""
	if timeSinceLast <= interval && !needsTrunkEni {
		return
	}
	var metadataResult awsutils.DescribeAllENIsResult

	prometheusmetrics.IpamdActionsInprogress.WithLabelValues("nodeIPPoolReconcile").Add(float64(1))
	defer prometheusmetrics.IpamdActionsInprogress.WithLabelValues("nodeIPPoolReconcile").Sub(float64(1))

	log.Debugf("Reconciling ENI/IP pool info because time since last %v > %v", timeSinceLast, interval)
	allENIs, err := c.awsClient.GetAttachedENIs()
	if err != nil {
		log.Errorf("IP pool reconcile: Failed to get attached ENI info: %v", err.Error())
		ipamdErrInc("reconcileFailedGetENIs")
		return
	}
	// We must always have at least the primary ENI of the instance
	if allENIs == nil {
		log.Error("IP pool reconcile: No ENI found at all in metadata, unable to reconcile")
		ipamdErrInc("reconcileFailedGetENIs")
		return
	}

	allManagedAndAttachedENIs := c.filterUnmanagedENIs(allENIs)
	attachedENIsByNetworkCard := c.getENIsByNetworkCard(allManagedAndAttachedENIs)

	for _, ds := range c.dataStoreAccess.DataStores {
		networkCard := ds.GetNetworkCard()
		currentENIs := ds.GetENIInfos().ENIs
		attachedENIs := attachedENIsByNetworkCard[networkCard]
		trunkENI := ds.GetTrunkENI()
		// Initialize the set with the known EFA interfaces
		efaENIs := ds.GetEFAENIs()

		// Check if a new ENI was added, if so we need to update the tags.
		needToUpdateTags := false
		for _, attachedENI := range attachedENIs {
			if _, ok := currentENIs[attachedENI.ENIID]; !ok {
				needToUpdateTags = true
				break
			}
		}

		var eniTagMap map[string]awsutils.TagMap
		if needToUpdateTags {
			log.Debugf("A new ENI added but not by ipamd, updating tags by calling EC2")
			// Call describeENIs only once per nodeIPPoolReconcile by checking if metadataResult.ENIMetadata is nil
			if len(metadataResult.ENIMetadata) == 0 {
				metadataResult, err = c.awsClient.DescribeAllENIs()
				if err != nil {
					log.Warnf("Failed to call EC2 to describe ENIs, aborting reconcile: %v", err)
					return
				}
			}

			if c.enablePodENI && metadataResult.TrunkENI != "" {
				log.Debugf("Trunk interface (%s) has been added to the node already.", metadataResult.TrunkENI)
			}
			// Update trunk ENI
			trunkENI = metadataResult.TrunkENI
			// Just copy values of the EFA set
			efaENIs = metadataResult.EFAENIs
			eniTagMap = metadataResult.TagMap

			c.setUnmanagedENIs(metadataResult.TagMap)
			c.markUnmanagedNetworkCards(metadataResult.EFAOnlyENIByNetworkCard, metadataResult.ENIsByNetworkCard)

			allManagedENIs := c.filterUnmanagedENIs(metadataResult.ENIMetadata)
			attachedENIs = c.getENIsByNetworkCard(allManagedENIs)[networkCard]
		}

		// Mark phase
		for _, attachedENI := range attachedENIs {
			eniIPPool, eniPrefixPool, err := ds.GetENICIDRs(attachedENI.ENIID)
			if err == nil {
				// If the attached ENI is in the data store
				log.Debugf("Reconcile existing ENI %s IP pool", attachedENI.ENIID)
				// Reconcile IP pool
				c.eniIPPoolReconcile(eniIPPool, attachedENI, attachedENI.ENIID, networkCard)
				// If the attached ENI is in the data store
				log.Debugf("Reconcile existing ENI %s IP prefixes", attachedENI.ENIID)
				// Reconcile IP pool
				c.eniPrefixPoolReconcile(eniPrefixPool, attachedENI, attachedENI.ENIID, networkCard)
				// Mark action, remove this ENI from currentENIs map
				delete(currentENIs, attachedENI.ENIID)
				continue
			}

			isTrunkENI := attachedENI.ENIID == trunkENI
			isEFAENI := efaENIs[attachedENI.ENIID]
			if !isTrunkENI && !c.disableENIProvisioning {
				if err := c.awsClient.TagENI(attachedENI.ENIID, eniTagMap[attachedENI.ENIID]); err != nil {
					log.Errorf("IP pool reconcile: failed to tag managed ENI %v: %v", attachedENI.ENIID, err)
					ipamdErrInc("eniReconcileAdd")
					continue
				}
			}

			// Add new ENI
			log.Debugf("Reconcile and add a new ENI %s", attachedENI.ENIID)
			err = c.setupENI(attachedENI.ENIID, attachedENI, isTrunkENI, isEFAENI)
			if err != nil {
				log.Errorf("IP pool reconcile: Failed to set up ENI %s network: %v", attachedENI.ENIID, err)
				ipamdErrInc("eniReconcileAdd")
				// Continue if having trouble with ONLY 1 ENI, instead of bailout here?
				continue
			}
			prometheusmetrics.ReconcileCnt.With(prometheus.Labels{"fn": "eniReconcileAdd"}).Inc()
		}

		// Sweep phase: since the marked ENI have been removed, the remaining ones needs to be sweeped
		for eni := range currentENIs {
			log.Infof("Reconcile and delete detached ENI %s", eni)
			// Force the delete, since aws local metadata has told us that this ENI is no longer
			// attached, so any IPs assigned from this ENI will no longer work.
			err = ds.RemoveENIFromDataStore(eni, true /* force */)
			if err != nil {
				log.Errorf("IP pool reconcile: Failed to delete ENI during reconcile: %v", err)
				ipamdErrInc("eniReconcileDel")
				continue
			}

			delete(c.primaryIP, eni)
			prometheusmetrics.ReconcileCnt.With(prometheus.Labels{"fn": "eniReconcileDel"}).Inc()
		}

		log.Debug("Successfully Reconciled ENI/IP pool")
		c.logPoolStats(ds.GetIPStats(ipV4AddrFamily), networkCard)
	}
	c.lastNodeIPPoolAction = time.Now()

	for eni, primaryIP := range c.primaryIP {
		log.Debugf("Primary IP for ENI %s is %s", eni, primaryIP)
	}

}

func (c *IPAMContext) eniIPPoolReconcile(ipPool []string, attachedENI awsutils.ENIMetadata, eni string, networkCard int) {
	attachedENIIPs := attachedENI.IPv4Addresses
	needEC2Reconcile := true
	// Here we can't trust attachedENI since the IMDS metadata can be stale. We need to check with EC2 API.
	// IPsSimilar will exclude primary IP of the ENI that is not added to the ipPool and not available for pods to use.
	if !cniutils.IPsSimilar(ipPool, attachedENIIPs) {
		log.Warnf("Instance metadata does not match data store! ipPool: %v, metadata: %v", ipPool, attachedENIIPs)
		log.Debugf("We need to check the ENI status by calling the EC2 control plane.")
		// Call EC2 to verify IPs on this ENI
		ec2Addresses, err := c.awsClient.GetIPv4sFromEC2(eni)
		if err != nil {
			log.Errorf("Failed to fetch ENI IP addresses! Aborting reconcile of ENI %s", eni)
			return
		}
		attachedENIIPs = ec2Addresses
		needEC2Reconcile = false
	}

	// Add all known attached IPs to the datastore
	seenIPs := c.verifyAndAddIPsToDatastore(eni, attachedENIIPs, needEC2Reconcile, networkCard)

	// Sweep phase, delete remaining IPs since they should not remain in the datastore
	for _, existingIP := range ipPool {
		if seenIPs[existingIP] {
			continue
		}

		log.Debugf("Reconcile and delete IP %s on ENI %s", existingIP, eni)
		// Force the delete, since we have verified with EC2 that these secondary IPs are no longer assigned to this ENI
		ipv4Addr := net.IPNet{IP: net.ParseIP(existingIP), Mask: net.IPv4Mask(255, 255, 255, 255)}
		err := c.dataStoreAccess.GetDataStore(networkCard).DelIPv4CidrFromStore(eni, ipv4Addr, true /* force */)
		if err != nil {
			log.Errorf("Failed to reconcile and delete IP %s on ENI %s, %v", existingIP, eni, err)
			ipamdErrInc("ipReconcileDel")
			// continue instead of bailout due to one ip
			continue
		}
		prometheusmetrics.ReconcileCnt.With(prometheus.Labels{"fn": "eniIPPoolReconcileDel"}).Inc()
	}
}

func (c *IPAMContext) eniPrefixPoolReconcile(prefixPool []string, attachedENI awsutils.ENIMetadata, eni string, networkCard int) {
	attachedENIIPs := attachedENI.IPv4Prefixes
	needEC2Reconcile := true
	// Here we can't trust attachedENI since the IMDS metadata can be stale. We need to check with EC2 API.
	log.Debugf("Found prefix pool count %d for eni %s\n", len(prefixPool), eni)

	if !cniutils.PrefixSimilar(prefixPool, attachedENIIPs) {
		log.Warnf("Instance metadata does not match data store! ipPool: %v, metadata: %v", prefixPool, attachedENIIPs)
		log.Debugf("We need to check the ENI status by calling the EC2 control plane.")
		// Call EC2 to verify IPs on this ENI
		ec2Addresses, err := c.awsClient.GetIPv4PrefixesFromEC2(eni)
		if err != nil {
			log.Errorf("Failed to fetch ENI IP addresses! Aborting reconcile of ENI %s", eni)
			return
		}
		attachedENIIPs = ec2Addresses
		needEC2Reconcile = false
	}

	// Add all known attached IPs to the datastore
	seenIPs := c.verifyAndAddPrefixesToDatastore(eni, attachedENIIPs, needEC2Reconcile, networkCard)

	// Sweep phase, delete remaining Prefixes since they should not remain in the datastore
	for _, existingIP := range prefixPool {
		if seenIPs[existingIP] {
			continue
		}

		log.Debugf("Reconcile and delete Prefix %s on ENI %s", existingIP, eni)
		// Force the delete, since we have verified with EC2 that these secondary IPs are no longer assigned to this ENI
		_, ipv4Cidr, err := net.ParseCIDR(existingIP)
		if err != nil {
			log.Debugf("Failed to parse so continuing with next prefix")
			continue
		}
		err = c.dataStoreAccess.GetDataStore(networkCard).DelIPv4CidrFromStore(eni, *ipv4Cidr, true /* force */)
		if err != nil {
			log.Errorf("Failed to reconcile and delete IP %s on ENI %s, %v", existingIP, eni, err)
			ipamdErrInc("ipReconcileDel")
			// continue instead of bailout due to one ip
			continue
		}
		prometheusmetrics.ReconcileCnt.With(prometheus.Labels{"fn": "eniIPPoolReconcileDel"}).Inc()
	}
}

// verifyAndAddIPsToDatastore updates the datastore with the known secondary IPs. IPs who are out of cooldown gets added
// back to the datastore after being verified against EC2.
func (c *IPAMContext) verifyAndAddIPsToDatastore(eni string, attachedENIIPs []ec2types.NetworkInterfacePrivateIpAddress, needEC2Reconcile bool, networkCard int) map[string]bool {
	var ec2VerifiedAddresses []ec2types.NetworkInterfacePrivateIpAddress
	seenIPs := make(map[string]bool)
	for _, privateIPv4 := range attachedENIIPs {
		strPrivateIPv4 := aws.ToString(privateIPv4.PrivateIpAddress)
		if strPrivateIPv4 == c.primaryIP[eni] {
			log.Infof("Reconcile and skip primary IP %s on ENI %s", strPrivateIPv4, eni)
			continue
		}

		// Check if this IP was recently freed
		ipv4Addr := net.IPNet{IP: net.ParseIP(strPrivateIPv4), Mask: net.IPv4Mask(255, 255, 255, 255)}
		found, recentlyFreed := c.reconcileCooldownCache.RecentlyFreed(strPrivateIPv4)
		if found {
			if recentlyFreed {
				log.Debugf("Reconcile skipping IP %s on ENI %s because it was recently unassigned from the ENI.", strPrivateIPv4, eni)
				continue
			} else {
				if needEC2Reconcile {
					// IMDS data might be stale
					log.Debugf("This IP was recently freed, but is now out of cooldown. We need to verify with EC2 control plane.")
					// Only call EC2 once for this ENI
					if ec2VerifiedAddresses == nil {
						var err error
						// Call EC2 to verify IPs on this ENI
						ec2VerifiedAddresses, err = c.awsClient.GetIPv4sFromEC2(eni)
						if err != nil {
							log.Errorf("Failed to fetch ENI IP addresses from EC2! %v", err)
							// Do not delete this IP from the datastore or cooldown until we have confirmed with EC2
							seenIPs[strPrivateIPv4] = true
							continue
						}
					}
					// Verify that the IP really belongs to this ENI
					isReallyAttachedToENI := false
					for _, ec2Addr := range ec2VerifiedAddresses {
						if strPrivateIPv4 == aws.ToString(ec2Addr.PrivateIpAddress) {
							isReallyAttachedToENI = true
							log.Debugf("Verified that IP %s is attached to ENI %s", strPrivateIPv4, eni)
							break
						}
					}
					if !isReallyAttachedToENI {
						log.Warnf("Skipping IP %s on ENI %s because it does not belong to this ENI!", strPrivateIPv4, eni)
						continue
					}
				}
				// The IP can be removed from the cooldown cache
				// TODO: Here we could check if the IP is still used by a pod stuck in Terminating state. (Issue #1091)
				c.reconcileCooldownCache.Remove(strPrivateIPv4)
			}
		}
		log.Infof("Trying to add %s", strPrivateIPv4)
		// Try to add the IP
		err := c.dataStoreAccess.GetDataStore(networkCard).AddIPv4CidrToStore(eni, ipv4Addr, false)
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Errorf("Failed to reconcile IP %s on ENI %s", strPrivateIPv4, eni)
			ipamdErrInc("ipReconcileAdd")
			// Continue to check the other IPs instead of bailout due to one wrong IP
			continue

		}
		// Mark action
		seenIPs[strPrivateIPv4] = true
		prometheusmetrics.ReconcileCnt.With(prometheus.Labels{"fn": "eniDataStorePoolReconcileAdd"}).Inc()
	}
	return seenIPs
}

// verifyAndAddPrefixesToDatastore updates the datastore with the known Prefixes. Prefixes who are out of cooldown gets added
// back to the datastore after being verified against EC2.
func (c *IPAMContext) verifyAndAddPrefixesToDatastore(eni string, attachedENIPrefixes []ec2types.Ipv4PrefixSpecification, needEC2Reconcile bool, networkCard int) map[string]bool {
	var ec2VerifiedAddresses []ec2types.Ipv4PrefixSpecification
	seenIPs := make(map[string]bool)
	for _, privateIPv4Cidr := range attachedENIPrefixes {
		strPrivateIPv4Cidr := aws.ToString(privateIPv4Cidr.Ipv4Prefix)
		log.Debugf("Check in coolddown Found prefix %s", strPrivateIPv4Cidr)

		// Check if this Prefix was recently freed
		_, ipv4CidrPtr, err := net.ParseCIDR(strPrivateIPv4Cidr)
		if err != nil {
			log.Debugf("Failed to parse so continuing with next prefix")
			continue
		}
		found, recentlyFreed := c.reconcileCooldownCache.RecentlyFreed(strPrivateIPv4Cidr)
		if found {
			if recentlyFreed {
				log.Debugf("Reconcile skipping IP %s on ENI %s because it was recently unassigned from the ENI.", strPrivateIPv4Cidr, eni)
				continue
			} else {
				if needEC2Reconcile {
					// IMDS data might be stale
					log.Debugf("This IP was recently freed, but is now out of cooldown. We need to verify with EC2 control plane.")
					// Only call EC2 once for this ENI and post GA fix this logic for both prefixes
					// and secondary IPs as per "split the loop" comment
					if ec2VerifiedAddresses == nil {
						var err error
						// Call EC2 to verify Prefixes on this ENI
						ec2VerifiedAddresses, err = c.awsClient.GetIPv4PrefixesFromEC2(eni)
						if err != nil {
							log.Errorf("Failed to fetch ENI IP addresses from EC2! %v", err)
							// Do not delete this Prefix from the datastore or cooldown until we have confirmed with EC2
							seenIPs[strPrivateIPv4Cidr] = true
							continue
						}
					}
					// Verify that the Prefix really belongs to this ENI
					isReallyAttachedToENI := false
					for _, ec2Addr := range ec2VerifiedAddresses {
						if strPrivateIPv4Cidr == aws.ToString(ec2Addr.Ipv4Prefix) {
							isReallyAttachedToENI = true
							log.Debugf("Verified that IP %s is attached to ENI %s", strPrivateIPv4Cidr, eni)
							break
						}
					}
					if !isReallyAttachedToENI {
						log.Warnf("Skipping IP %s on ENI %s because it does not belong to this ENI!", strPrivateIPv4Cidr, eni)
						continue
					}
				}
				// The IP can be removed from the cooldown cache
				// TODO: Here we could check if the Prefix is still used by a pod stuck in Terminating state. (Issue #1091)
				c.reconcileCooldownCache.Remove(strPrivateIPv4Cidr)
			}
		}

		err = c.dataStoreAccess.GetDataStore(networkCard).AddIPv4CidrToStore(eni, *ipv4CidrPtr, true)
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Errorf("Failed to reconcile Prefix %s on ENI %s", strPrivateIPv4Cidr, eni)
			ipamdErrInc("prefixReconcileAdd")
			// Continue to check the other Prefixs instead of bailout due to one wrong IP
			continue

		}
		// Mark action
		seenIPs[strPrivateIPv4Cidr] = true
		prometheusmetrics.ReconcileCnt.With(prometheus.Labels{"fn": "eniDataStorePoolReconcileAdd"}).Inc()
	}
	return seenIPs
}

// return true when WARM_IP_TARGET or MINIMUM_IP_TARGET is defined
func (c *IPAMContext) warmIPTargetsDefined() bool {
	return c.warmIPTarget != noWarmIPTarget || c.minimumIPTarget != noMinimumIPTarget
}

// max pods from instance type mapping file
type instanceTypeMaxPodsMapping map[string]int64

// getMaxPodsFromFile reads the max pods value from the eni-max-pods.txt file
// based on the instance type
func (c *IPAMContext) getMaxPodsFromFile() (int64, error) {
	instanceType := c.awsClient.GetInstanceType()
	if instanceType == "" {
		return 0, fmt.Errorf("failed to get instance type")
	}

	data, err := os.ReadFile(eniMaxPodsFilePath)
	if err != nil {
		return 0, fmt.Errorf("failed to read ENI max pods file: %w", err)
	}

	maxPods, err := parseMaxPodsForInstanceFromFile(string(data), instanceType)
	return maxPods, err
}

// parseMaxPodsFile parses the eni-max-pods.txt file content and returns a mapping
// of instance type to max pods
func parseMaxPodsForInstanceFromFile(content string, instanceType string) (int64, error) {
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		// Skip comments and empty lines
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split the line into instance type and max pods
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}

		currInstanceType := parts[0]
		if currInstanceType == instanceType {
			maxPods, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("failed to parse maxPods for instance type %q: %v", instanceType, err)
			}
			return maxPods, nil
		}
	}

	return 0, fmt.Errorf("instance type %q not found in ENI max pods file", instanceType)
}

// UseCustomNetworkCfg returns whether Pods needs to use pod specific configuration or not.
func UseCustomNetworkCfg() bool {
	return parseBoolEnvVar(envCustomNetworkCfg, false)
}

// ManageENIsOnNonSchedulableNode returns whether IPAMd should manage ENIs on the node or not.
func ManageENIsOnNonSchedulableNode() bool {
	return parseBoolEnvVar(envManageENIsNonSchedulable, false)
}

// UseSubnetDiscovery returns whether we should use enhanced subnet selection or not when creating ENIs.
func UseSubnetDiscovery() bool {
	return parseBoolEnvVar(envSubnetDiscovery, true)
}

func parseBoolEnvVar(envVariableName string, defaultVal bool) bool {
	if strValue := os.Getenv(envVariableName); strValue != "" {
		parsedValue, err := strconv.ParseBool(strValue)
		if err == nil {
			return parsedValue
		}
		log.Warnf("Failed to parse %s; using default: %v, err: %v", envVariableName, defaultVal, err)
	}
	return defaultVal
}

func dsBackingStorePath() string {
	if value := os.Getenv(envBackingStorePath); value != "" {
		return value
	}
	return defaultBackingStorePath
}

func getWarmIPTarget() int {
	inputStr, found := os.LookupEnv(envWarmIPTarget)
	if !found {
		return noWarmIPTarget
	}

	if input, err := strconv.Atoi(inputStr); err == nil {
		if input >= 0 {
			log.Debugf("Using WARM_IP_TARGET %v", input)
			return input
		}
	}
	return noWarmIPTarget
}

func getMinimumIPTarget() int {
	inputStr, found := os.LookupEnv(envMinimumIPTarget)
	if !found {
		return noMinimumIPTarget
	}

	if input, err := strconv.Atoi(inputStr); err == nil {
		if input >= 0 {
			log.Debugf("Using MINIMUM_IP_TARGET %v", input)
			return input
		}
	}
	return noMinimumIPTarget
}

func disableENIProvisioning() bool {
	return enableImdsOnlyMode() || utils.GetBoolAsStringEnvVar(envDisableENIProvisioning, false)
}

func disableLeakedENICleanup() bool {
	// Cases where leaked ENI cleanup is disabled:
	// 1. IPv6 is enabled, so no ENIs are attached
	// 2. ENI provisioning is disabled, so ENIs are not managed by IPAMD
	// 3. CNI is operating in IMDS only mode
	// 4. Environment var explicitly disabling task is set
	return isIPv6Enabled() || disableENIProvisioning() || enableImdsOnlyMode() || utils.GetBoolAsStringEnvVar(envDisableLeakedENICleanup, false)
}

func enableImdsOnlyMode() bool {
	return utils.GetBoolAsStringEnvVar(utils.EnvEnableImdsOnlyMode, false)
}

func EnablePodENI() bool {
	return utils.GetBoolAsStringEnvVar(envEnablePodENI, false)
}

func enableMultiNICSupport() bool {
	return utils.GetBoolAsStringEnvVar(envEnableMultiNICSupport, false)
}

func getNetworkPolicyMode() (string, error) {
	value, exists := os.LookupEnv(envNetworkPolicyMode)
	if !exists {
		return "", nil
	}
	if utils.IsValidNetworkPolicyEnforcingMode(value) {
		return value, nil
	}
	return "", errors.New("invalid Network policy mode, supported modes: none, strict, standard")
}

func usePrefixDelegation() bool {
	return utils.GetBoolAsStringEnvVar(envEnableIpv4PrefixDelegation, false)
}

func isIPv4Enabled() bool {
	return utils.GetBoolAsStringEnvVar(envEnableIPv4, false)
}

func isIPv6Enabled() bool {
	return utils.GetBoolAsStringEnvVar(envEnableIPv6, false)
}

func enableManageUntaggedMode() bool {
	return utils.GetBoolAsStringEnvVar(envManageUntaggedENI, true)
}

func EnablePodIPAnnotation() bool {
	return utils.GetBoolAsStringEnvVar(envAnnotatePodIP, false)
}

// filterUnmanagedENIs filters out ENIs marked with the "node.k8s.amazonaws.com/no_manage" tag
func (c *IPAMContext) filterUnmanagedENIs(enis []awsutils.ENIMetadata) []awsutils.ENIMetadata {
	numFiltered := 0
	ret := make([]awsutils.ENIMetadata, 0, len(enis))

	for _, eni := range enis {
		//Filter out any Unmanaged ENIs. VPC CNI will only work with Primary ENI in IPv6 Prefix Delegation mode until
		//we open up IPv6 support in Secondary IP and Custom networking modes. Filtering out the ENIs here will
		//help us avoid myriad of if/else loops elsewhere in the code.
		//We shouldn't need the IsPrimaryENI check as ENIs not created by vpc-cni will be marked unmanaged (including trunk ENI)
		isUnmanagedENI := c.awsClient.IsUnmanagedENI(eni.ENIID)
		isUnmanagedNIC := c.awsClient.IsUnmanagedNIC(eni.NetworkCard)
		isEfaOnlyENI := c.awsClient.IsEfaOnlyENI(eni.NetworkCard, eni.ENIID)

		if c.enableIPv6 {
			if !c.awsClient.IsPrimaryENI(eni.ENIID) {
				if isUnmanagedENI {
					log.Debugf("Skipping ENI %s: IPv6 Mode is enabled and VPC CNI will only ENIs created by it in v6 PD mode",
						eni.ENIID)
					numFiltered++
					c.unmanagedENI[eni.NetworkCard] += 1
					continue
				} else if isUnmanagedNIC {
					log.Debugf("Skipping ENI %s: since it is on unmanaged network card index %d", eni.ENIID, eni.NetworkCard)
					continue
				} else if isEfaOnlyENI {
					log.Debugf("Skipping ENI %s: since it is EFA only ENI on network card index %d", eni.ENIID, eni.NetworkCard)
					numFiltered++
					c.unmanagedENI[eni.NetworkCard] += 1
					continue
				}
			}
		} else if isUnmanagedENI {
			log.Debugf("Skipping ENI %s: since it is unmanaged", eni.ENIID)
			numFiltered++
			c.unmanagedENI[eni.NetworkCard] += 1
			continue
		} else if isUnmanagedNIC {
			log.Debugf("Skipping ENI %s: since it is on unmanaged network card index %d", eni.ENIID, eni.NetworkCard)
			continue
		} else if isEfaOnlyENI {
			log.Debugf("Skipping ENI %s: since it is EFA only ENI on network card index %d", eni.ENIID, eni.NetworkCard)
			numFiltered++
			c.unmanagedENI[eni.NetworkCard] += 1
			continue
		}

		ret = append(ret, eni)
	}
	c.updateIPStats(numFiltered)
	return ret
}

// datastoreTargetState determines the number of IPs `short` or `over` our WARM_IP_TARGET, accounting for the MINIMUM_IP_TARGET.
// With prefix delegation, this function determines the number of Prefixes `short` or `over`
func (c *IPAMContext) datastoreTargetState(stats *datastore.DataStoreStats, networkCard int) (short int, over int, enabled bool) {

	warmIPTarget := c.warmIPTarget
	minimumIPTarget := c.minimumIPTarget

	if networkCard > DefaultNetworkCardIndex {
		// multi card ENIs will use WARM_IP_TARGET=1 and MINIMUM_IP_TARGET=1 by default
		warmIPTarget = DefaultWarmIPTarget
		minimumIPTarget = DefaultMinimumIPTarget
	} else if !c.warmIPTargetsDefined() {
		// there is no WARM_IP_TARGET defined and no MINIMUM_IP_TARGET, fallback to use all IP addresses on ENI
		return 0, 0, false
	}

	// Calculating DataStore stats can be expensive, so allow the caller to optionally pass stats it already calculated
	if stats == nil {
		stats = c.dataStoreAccess.GetDataStore(networkCard).GetIPStats(ipV4AddrFamily)
	}

	available := stats.AvailableAddresses()

	// short is greater than 0 when we have fewer available IPs than the warm IP target
	short = max(warmIPTarget-available, 0)

	// short is greater than the warm IP target alone when we have fewer total IPs than the minimum target
	short = max(short, minimumIPTarget-stats.TotalIPs)

	// over is the number of available IPs we have beyond the warm IP target
	over = max(available-warmIPTarget, 0)

	// over is less than the warm IP target alone if it would imply reducing total IPs below the minimum target
	over = max(min(over, stats.TotalIPs-minimumIPTarget), 0)

	if networkCard == DefaultNetworkCardIndex && c.enablePrefixDelegation {
		// short : number of IPs short to reach warm targets
		// over : number of IPs over the warm targets
		_, numIPsPerPrefix, _ := datastore.GetPrefixDelegationDefaults()
		// Number of prefixes IPAMD is short of to achieve warm targets
		shortPrefix := datastore.DivCeil(short, numIPsPerPrefix)

		// Over will have number of IPs more than needed but with PD we would have allocated in chunks of /28
		// Say assigned = 1, warm ip target = 16, this will need 2 prefixes. But over will return 15.
		// Hence we need to check if 'over' number of IPs are needed to maintain the warm targets
		prefixNeededForWarmIP := datastore.DivCeil(stats.AssignedIPs+c.warmIPTarget, numIPsPerPrefix)
		prefixNeededForMinIP := datastore.DivCeil(c.minimumIPTarget, numIPsPerPrefix)

		// over will be number of prefixes over than needed but could be spread across used prefixes,
		// say, after couple of pod churns, 3 prefixes are allocated with 1 IP each assigned and warm ip target is 15
		// (J : is this needed? since we have to walk thru the loop of prefixes)
		freePrefixes := c.dataStoreAccess.GetDataStore(networkCard).GetFreePrefixes()
		overPrefix := max(min(freePrefixes, stats.TotalPrefixes-prefixNeededForWarmIP), 0)
		overPrefix = max(min(overPrefix, stats.TotalPrefixes-prefixNeededForMinIP), 0)
		return shortPrefix, overPrefix, true
	}
	return short, over, true
}

// datastorePrefixTargetState determines the number of prefixes short to reach WARM_PREFIX_TARGET
func (c *IPAMContext) datastorePrefixTargetState(networkCard int) (short int, enabled bool) {
	if !c.warmPrefixTargetDefined() {
		return 0, false
	}
	// /28 will consume 16 IPs so let's not allocate if not needed.
	freePrefixesInStore := c.dataStoreAccess.GetDataStore(networkCard).GetFreePrefixes()
	toAllocate := max(c.warmPrefixTarget-freePrefixesInStore, 0)
	log.Debugf("Prefix target is %d, short of %d prefixes, free %d prefixes", c.warmPrefixTarget, toAllocate, freePrefixesInStore)

	return toAllocate, true
}

// setTerminating atomically sets the terminating flag.
func (c *IPAMContext) setTerminating() {
	atomic.StoreInt32(&c.terminating, 1)
}

func (c *IPAMContext) isTerminating() bool {
	return atomic.LoadInt32(&c.terminating) > 0
}

func (c *IPAMContext) isNodeNonSchedulable() bool {
	ctx := context.TODO()

	request := types.NamespacedName{
		Name: c.myNodeName,
	}

	node := &corev1.Node{}
	// Find my node
	err := c.k8sClient.Get(ctx, request, node)
	if err != nil {
		log.Errorf("Failed to get node while determining schedulability: %v", err)
		return false
	}
	log.Debugf("Node found %q - no of taints - %d", node.Name, len(node.Spec.Taints))
	taintToMatch := &corev1.Taint{
		Key:    "node.kubernetes.io/unschedulable",
		Effect: corev1.TaintEffectNoSchedule,
	}
	for _, taint := range node.Spec.Taints {
		if taint.MatchTaint(taintToMatch) {
			return true
		}
	}

	return false
}

// GetConfigForDebug returns the active values of the configuration env vars (for debugging purposes).
func GetConfigForDebug() map[string]interface{} {
	return map[string]interface{}{
		envWarmIPTarget:             getWarmIPTarget(),
		envWarmENITarget:            getWarmENITarget(),
		envCustomNetworkCfg:         UseCustomNetworkCfg(),
		envManageENIsNonSchedulable: ManageENIsOnNonSchedulableNode(),
		envSubnetDiscovery:          UseSubnetDiscovery(),
	}
}

func max(x, y int) int {
	if x < y {
		return y
	}
	return x
}

func min(x, y int) int {
	if y < x {
		return y
	}
	return x
}

func (c *IPAMContext) getTrunkLinkIndex() (int, error) {
	trunkENI := c.dataStoreAccess.GetDataStore(DefaultNetworkCardIndex).GetTrunkENI()
	attachedENIs, err := c.awsClient.GetAttachedENIs()
	if err != nil {
		return -1, err
	}
	for _, eni := range attachedENIs {
		if eni.ENIID == trunkENI {
			retryLinkByMacInterval := 100 * time.Millisecond
			link, err := c.networkClient.GetLinkByMac(eni.MAC, retryLinkByMacInterval)
			if err != nil {
				return -1, err
			}
			return link.Attrs().Index, nil

		}
	}
	return -1, errors.New("no trunk found")
}

// GetPod returns the pod matching the name and namespace
func (c *IPAMContext) GetPod(podName, namespace string) (*corev1.Pod, error) {
	ctx := context.TODO()
	var pod corev1.Pod

	podKey := types.NamespacedName{
		Namespace: namespace,
		Name:      podName,
	}
	err := c.k8sClient.Get(ctx, podKey, &pod)
	if err != nil {
		return nil, fmt.Errorf("error while trying to retrieve pod info: %s", err.Error())
	}
	return &pod, nil
}

// AnnotatePod annotates the pod with the provided key and value
func (c *IPAMContext) AnnotatePod(podName string, podNamespace string, key string, newVal string, releasedIP string) error {
	ctx := context.TODO()
	var err error

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var pod *corev1.Pod
		if pod, err = c.GetPod(podName, podNamespace); err != nil || pod == nil {
			// if pod is nil and err is nil for any reason, this is not retriable case, returning a nil error to not-retry
			if err == nil && pod == nil {
				log.Warnf("get a nil pod for pod name %s and namespace %s", podName, podNamespace)
			}
			// since the GetPod() error has been decorated, we have to check key words
			// releasedIP is not empty meaning del path
			if releasedIP != "" && err != nil && strings.Contains(err.Error(), "not found") {
				return nil
			}
			return err
		}

		newPod := pod.DeepCopy()
		if newPod.Annotations == nil {
			newPod.Annotations = make(map[string]string)
		}

		oldVal, ok := newPod.Annotations[key]

		// On CNI ADD, always set new annotation
		if newVal != "" {
			// Skip patch operation if new value is the same as existing value
			if ok && oldVal == newVal {
				log.Infof("Patch updating not needed")
				return nil
			}
			newPod.Annotations[key] = newVal
		} else {
			// On CNI DEL, set annotation to empty string if IP is the one we are releasing
			if ok {
				log.Debugf("Existing annotation value: %s", oldVal)
				if oldVal != releasedIP {
					return fmt.Errorf("released IP %s does not match existing annotation. Not patching pod", releasedIP)
				}
				newPod.Annotations[key] = ""
			}
		}

		if err = c.k8sClient.Patch(ctx, newPod, client.MergeFrom(pod)); err != nil {
			log.Errorf("Failed to annotate %s the pod with %s, error %v", key, newVal, err)
			return err
		}
		log.Debugf("Annotates pod %s with %s: %s", podName, key, newVal)
		return nil
	})

	return err
}

func (c *IPAMContext) tryUnassignIPsFromENIs() {
	log.Debugf("tryUnassignIPsFromENIs")
	// From all datastores, get ENIInfos and unassign IPs
	for _, ds := range c.dataStoreAccess.DataStores {
		networkCard := ds.GetNetworkCard()
		eniInfos := ds.GetENIInfos()
		for eniID := range eniInfos.ENIs {
			c.tryUnassignIPFromENI(eniID, networkCard)
		}
	}

}

func (c *IPAMContext) tryUnassignIPFromENI(eniID string, networkCard int) {

	ds := c.dataStoreAccess.GetDataStore(networkCard)
	freeableIPs := ds.FreeableIPs(eniID)
	if len(freeableIPs) == 0 {
		log.Debugf("No freeable IPs")
		return
	}

	// Delete IPs from datastore
	var deletedIPs []string
	for _, toDelete := range freeableIPs {
		// Don't force the delete, since a freeable IP might have been assigned to a pod
		// before we get around to deleting it.
		err := ds.DelIPv4CidrFromStore(eniID, toDelete, false /* force */)
		if err != nil {
			log.Warnf("Failed to delete IP %s on ENI %s from datastore: %s", toDelete, eniID, err)
			ipamdErrInc("decreaseIPPool")
			continue
		} else {
			deletedIPs = append(deletedIPs, toDelete.IP.String())
		}
	}

	// Deallocate IPs from the instance if they aren't used by pods.
	if err := c.awsClient.DeallocIPAddresses(eniID, deletedIPs); err != nil {
		log.Warnf("Failed to decrease IP pool by removing IPs %v from ENI %s: %s", deletedIPs, eniID, err)
	} else {
		log.Debugf("Successfully decreased IP pool by removing IPs %v from ENI %s", deletedIPs, eniID)
	}
}

func (c *IPAMContext) tryUnassignPrefixesFromENIs() {
	log.Debugf("tryUnassignPrefixesFromENIs")

	// From all datastores get ENIs and remove prefixes
	for _, ds := range c.dataStoreAccess.DataStores {
		networkCard := ds.GetNetworkCard()
		eniInfos := ds.GetENIInfos()
		for eniID := range eniInfos.ENIs {
			c.tryUnassignPrefixFromENI(eniID, networkCard)
		}
	}
}

func (c *IPAMContext) tryUnassignPrefixFromENI(eniID string, networkCard int) {
	ds := c.dataStoreAccess.GetDataStore(networkCard)
	freeablePrefixes := ds.FreeablePrefixes(eniID)
	if len(freeablePrefixes) == 0 {
		return
	}
	// Delete Prefixes from datastore
	var deletedPrefixes []string
	for _, toDelete := range freeablePrefixes {
		// Don't force the delete, since a freeable Prefix might have been assigned to a pod
		// before we get around to deleting it.
		err := ds.DelIPv4CidrFromStore(eniID, toDelete, false /* force */)
		if err != nil {
			log.Warnf("Failed to delete Prefix %s on ENI %s from datastore: %s", toDelete, eniID, err)
			ipamdErrInc("decreaseIPPool")
			return
		} else {
			deletedPrefixes = append(deletedPrefixes, toDelete.String())
		}
	}

	// Deallocate IPs from the instance if they aren't used by pods.
	if err := c.awsClient.DeallocPrefixAddresses(eniID, deletedPrefixes); err != nil {
		log.Warnf("Failed to delete prefix %v from ENI %s: %s", deletedPrefixes, eniID, err)
	} else {
		log.Debugf("Successfully prefix removing IPs %v from ENI %s", deletedPrefixes, eniID)
	}
}

func (c *IPAMContext) GetENIResourcesToAllocate(networkCard int) int {
	var resourcesToAllocate int
	if c.enablePrefixDelegation {
		if c.enableIPv6 {
			// IPv6 always uses PD, so it should always be 1
			resourcesToAllocate = 1
		} else {
			resourcesToAllocate = min(c.getPrefixesNeeded(networkCard), c.maxPrefixesPerENI)
		}
	} else {
		resourcesToAllocate = c.maxIPsPerENI
		short, _, warmTargetDefined := c.datastoreTargetState(nil, networkCard)
		if warmTargetDefined {
			resourcesToAllocate = min(short, c.maxIPsPerENI)
		}
	}
	return resourcesToAllocate
}

func (c *IPAMContext) GetIPv4Limit() (int, int, error) {
	var maxIPsPerENI, maxPrefixesPerENI, maxIpsPerPrefix int
	if !c.enablePrefixDelegation {
		maxIPsPerENI = c.awsClient.GetENIIPv4Limit()
		maxPrefixesPerENI = 0
	} else if c.enablePrefixDelegation {
		//Single PD - allocate one prefix per ENI and new add will be new ENI + prefix
		//Multi - allocate one prefix per ENI and new add will be new prefix or new ENI + prefix
		_, maxIpsPerPrefix, _ = datastore.GetPrefixDelegationDefaults()
		maxPrefixesPerENI = c.awsClient.GetENIIPv4Limit()
		maxIPsPerENI = maxPrefixesPerENI * maxIpsPerPrefix
		log.Debugf("max prefix %d max ips %d", maxPrefixesPerENI, maxIPsPerENI)
	}
	return maxIPsPerENI, maxPrefixesPerENI, nil
}

func (c *IPAMContext) isDatastorePoolEmpty(networkCard int) bool {
	stats := c.dataStoreAccess.GetDataStore(networkCard).GetIPStats(ipV4AddrFamily)
	return stats.TotalIPs == 0
}

// Return whether the maximum number of ENIs that can be attached to the node has already been reached
func (c *IPAMContext) hasRoomForEni(networkCard int) bool {
	trunkEni := 0
	if c.enablePodENI && networkCard == DefaultNetworkCardIndex && c.dataStoreAccess.GetDataStore(DefaultNetworkCardIndex).GetTrunkENI() == "" {
		trunkEni = 1
	}
	return c.dataStoreAccess.GetDataStore(networkCard).GetENIs() < (c.maxENI - c.unmanagedENI[networkCard] - trunkEni)
}

func (c *IPAMContext) isDatastorePoolTooLow() map[int]Decisions {

	decisions := make(map[int]Decisions, len(c.dataStoreAccess.DataStores))
	warmTarget := c.warmENITarget
	totalIPs := c.maxIPsPerENI
	if c.enablePrefixDelegation {
		warmTarget = c.warmPrefixTarget
		_, maxIpsPerPrefix, _ := datastore.GetPrefixDelegationDefaults()
		totalIPs = maxIpsPerPrefix
	}

	for _, ds := range c.dataStoreAccess.DataStores {
		networkCard := ds.GetNetworkCard()
		stats := ds.GetIPStats(ipV4AddrFamily)
		// If max pods has been reached, pool is not too low
		if stats.TotalIPs >= c.maxPods {
			decisions[networkCard] = Decisions{Stats: stats, IsLow: false}
			continue
		}

		short, _, warmTargetDefined := c.datastoreTargetState(stats, networkCard)
		if warmTargetDefined {
			decisions[networkCard] = Decisions{Stats: stats, IsLow: short > 0}
			continue
		}

		available := stats.AvailableAddresses()
		poolTooLow := available < totalIPs*warmTarget || (warmTarget == 0 && available == 0)
		if poolTooLow {
			log.Debugf("IP pool is too low for Network Card %d: available (%d) < ENI target (%d) * addrsPerENI (%d)", networkCard, available, warmTarget, totalIPs)
			c.logPoolStats(stats, networkCard)
		}
		decisions[networkCard] = Decisions{Stats: stats, IsLow: poolTooLow}
	}
	return decisions
}

func (c *IPAMContext) isDatastorePoolTooHigh(stats *datastore.DataStoreStats, networkCard int) bool {
	// NOTE: IPs may be allocated in chunks (full ENIs of prefixes), so the "too-high" condition does not check max pods. The limit is enforced on the allocation side.
	_, over, warmTargetDefined := c.datastoreTargetState(stats, networkCard)
	if warmTargetDefined {
		return over > 0
	}

	// For the existing ENIs check if we can cleanup prefixes
	if c.warmPrefixTargetDefined() {
		freePrefixes := c.dataStoreAccess.GetDataStore(networkCard).GetFreePrefixes()
		poolTooHigh := freePrefixes > c.warmPrefixTarget
		if poolTooHigh {
			log.Debugf("Prefix pool is high so might be able to deallocate - free prefixes: %d, warm prefix target: %d from NetworkCard %d", freePrefixes, c.warmPrefixTarget, networkCard)
		}
		return poolTooHigh
	}
	// We only ever report the pool being too high if WARM_IP_TARGET or WARM_PREFIX_TARGET is set
	return false
}

func (c *IPAMContext) warmPrefixTargetDefined() bool {
	return c.warmPrefixTarget >= defaultWarmPrefixTarget && c.enablePrefixDelegation
}

// DeallocCidrs frees IPs and Prefixes from EC2
func (c *IPAMContext) DeallocCidrs(eniID string, deletableCidrs []datastore.CidrInfo) {
	var deletableIPs []string
	var deletablePrefixes []string

	for _, toDeleteCidr := range deletableCidrs {
		if toDeleteCidr.IsPrefix {
			strDeletablePrefix := toDeleteCidr.Cidr.String()
			deletablePrefixes = append(deletablePrefixes, strDeletablePrefix)
			// Track the last time we unassigned Cidrs from an ENI. We won't reconcile any Cidrs in this cache
			// for at least ipReconcileCooldown
			c.reconcileCooldownCache.Add(strDeletablePrefix)
		} else {
			strDeletableIP := toDeleteCidr.Cidr.IP.String()
			deletableIPs = append(deletableIPs, strDeletableIP)
			// Track the last time we unassigned IPs from an ENI. We won't reconcile any IPs in this cache
			// for at least ipReconcileCooldown
			c.reconcileCooldownCache.Add(strDeletableIP)
		}
	}

	if err := c.awsClient.DeallocPrefixAddresses(eniID, deletablePrefixes); err != nil {
		log.Warnf("Failed to free Prefixes %v from ENI %s: %s", deletablePrefixes, eniID, err)
	}

	if err := c.awsClient.DeallocIPAddresses(eniID, deletableIPs); err != nil {
		log.Warnf("Failed to free IPs %v from ENI %s: %s", deletableIPs, eniID, err)
	}
}

// getPrefixesNeeded returns the number of prefixes need to be allocated to the ENI
func (c *IPAMContext) getPrefixesNeeded(networkCard int) int {
	// By default allocate 1 prefix at a time
	toAllocate := 1

	// TODO - post GA we can evaluate to see if these two calls can be merged.
	// datastoreTargetState already has complex math so adding Prefix target will make it even more complex.
	short, _, warmIPTargetsDefined := c.datastoreTargetState(nil, networkCard)
	shortPrefixes, warmPrefixTargetDefined := c.datastorePrefixTargetState(networkCard)

	// WARM_IP_TARGET takes precendence over WARM_PREFIX_TARGET
	if warmIPTargetsDefined {
		toAllocate = max(toAllocate, short)
	} else if warmPrefixTargetDefined {
		toAllocate = max(toAllocate, shortPrefixes)
	}
	log.Debugf("ToAllocate: %d, Network Card %d", toAllocate, networkCard)
	return toAllocate
}

func (c *IPAMContext) initENIAndIPLimits() (err error) {

	nodeMaxENI, err := c.getMaxENI()
	if err != nil {
		log.Error("Failed to get ENI limit")
		return err
	}
	c.maxENI = nodeMaxENI

	if c.enableIPv4 {
		c.maxIPsPerENI, c.maxPrefixesPerENI, err = c.GetIPv4Limit()
		if err != nil {
			return err
		}
		log.Debugf("Max ip per ENI %d and max prefixes per ENI %d", c.maxIPsPerENI, c.maxPrefixesPerENI)
	}

	// WARM and MAX ENI & IP/Prefix counts are no-op in IPv6 Prefix delegation mode. Once we start supporting IPv6 in
	// Secondary IP mode, these variables will play the same role as they currently do in IPv4 mode. So, for now we
	// leave them at their default values.
	return nil
}

func (c *IPAMContext) isConfigValid() bool {
	// Validate that only one among v4 and v6 is enabled.
	if c.enableIPv4 && c.enableIPv6 {
		log.Errorf("IPv4 and IPv6 are both enabled. VPC CNI currently does not support dual stack mode")
		return false
	} else if !c.enableIPv4 && !c.enableIPv6 {
		log.Errorf("IPv4 and IPv6 are both disabled. One of them have to be enabled")
		return false
	}

	// Validate PD mode is enabled if VPC CNI is operating in IPv6 mode. Custom networking is not supported in IPv6 mode.
	if c.enableIPv6 && (c.useCustomNetworking || !c.enablePrefixDelegation) {
		log.Errorf("IPv6 is supported only in Prefix Delegation mode. Custom Networking is not supported in IPv6 mode. Please set the env variables accordingly.")
		return false
	}

	// Validate Prefix Delegation against v4 and v6 modes.
	if c.enablePrefixDelegation && !c.awsClient.IsPrefixDelegationSupported() {
		if c.enableIPv6 {
			log.Errorf("Prefix Delegation is not supported on non-nitro instance %s. IPv6 is only supported in Prefix delegation Mode. ", c.awsClient.GetInstanceType())
			return false
		}
		log.Warnf("Prefix delegation is not supported on non-nitro instance %s hence falling back to default (secondary IP) mode", c.awsClient.GetInstanceType())
		c.enablePrefixDelegation = false
	}

	return true
}

func (c *IPAMContext) AddFeatureToCNINode(ctx context.Context, featureName rcv1alpha1.FeatureName, featureValue string) error {
	cniNode := &rcv1alpha1.CNINode{}
	if err := c.k8sClient.Get(ctx, types.NamespacedName{Name: c.myNodeName}, cniNode); err != nil {
		return err
	}

	if lo.ContainsBy(cniNode.Spec.Features, func(addedFeature rcv1alpha1.Feature) bool {
		return featureName == addedFeature.Name && featureValue == addedFeature.Value
	}) {
		return nil
	}

	newCNINode := cniNode.DeepCopy()
	newFeature := rcv1alpha1.Feature{
		Name:  featureName,
		Value: featureValue,
	}

	if lo.ContainsBy(cniNode.Spec.Features, func(addedFeature rcv1alpha1.Feature) bool {
		return featureName == addedFeature.Name
	}) {
		// this should happen when user updated eniConfig name
		// aws-node restarted and CNINode need to be updated if node wasn't terminated
		// we need groom old features here
		newCNINode.Spec.Features = []rcv1alpha1.Feature{}
		for _, oldFeature := range cniNode.Spec.Features {
			if oldFeature.Name == featureName {
				continue
			} else {
				newCNINode.Spec.Features = append(newCNINode.Spec.Features, oldFeature)
			}
		}
	}

	newCNINode.Spec.Features = append(newCNINode.Spec.Features, newFeature)
	return c.k8sClient.Patch(ctx, newCNINode, client.MergeFromWithOptions(cniNode, client.MergeFromWithOptimisticLock{}))
}

// SetAPIServerConnectivity updates the API server connectivity status and reconfigures
// components that depend on API server access
func (c *IPAMContext) SetAPIServerConnectivity(connected bool) {
	if c.withApiServer == connected {
		// Status didn't change
		return
	}

	log.Infof("Updating API server connectivity status from %v to %v", c.withApiServer, connected)
	c.withApiServer = connected

	if connected {
		// API server is now available - update maxPods from node object
		// First, try to recreate the client with caching enabled
		newClient, err := k8sapi.CreateKubeClient(appName)
		if err != nil {
			log.Errorf("Failed to recreate k8s client with cache when API server became available: %v", err)
		} else {
			log.Info("Successfully recreated k8s client with cache after API server became available")
			c.k8sClient = newClient
		}

		// Now get the node to update maxPods
		ctx := context.TODO()
		node, err := k8sapi.GetNode(ctx, c.k8sClient)
		if err != nil {
			log.Errorf("Failed to get node after API server connection established: %s", err)
		} else {
			maxPods, isInt64 := node.Status.Capacity.Pods().AsInt64()
			if !isInt64 {
				log.Errorf("Failed to parse max pods from node: %s", node.Status.Capacity.Pods().String())
			} else {
				// Update maxPods with the value from the node
				oldMaxPods := c.maxPods
				c.maxPods = int(maxPods)
				log.Infof("Updated maxPods from %d to %d based on node capacity", oldMaxPods, c.maxPods)
			}
		}
	} else {
		// API server connection was lost
		// No action needed here as we already have maxPods from file or kubelet
		// and we want to keep working with that value
		log.Info("API server connection lost, continuing with current maxPods value")
	}
}

type Decisions struct {
	Stats *datastore.DataStoreStats
	IsLow bool
}
