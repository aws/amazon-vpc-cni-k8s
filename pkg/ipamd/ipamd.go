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

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
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
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
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

	//insufficientCidrErrorCooldown is the amount of time reconciler will wait before trying to fetch
	//more IPs/prefixes for an ENI. With InsufficientCidr we know the subnet doesn't have enough IPs so
	//instead of retrying every 5s which would lead to increase in EC2 AllocIPAddress calls, we wait for
	//120 seconds for a retry.
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
)

var log = logger.Get()

var (
	ipamdErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_ipamd_error_count",
			Help: "The number of errors encountered in ipamd",
		},
		[]string{"fn"},
	)
	ipamdActionsInprogress = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "awscni_ipamd_action_inprogress",
			Help: "The number of ipamd actions in progress",
		},
		[]string{"fn"},
	)
	enisMax = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_eni_max",
			Help: "The maximum number of ENIs that can be attached to the instance, accounting for unmanaged ENIs",
		},
	)
	ipMax = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_ip_max",
			Help: "The maximum number of IP addresses that can be allocated to the instance",
		},
	)
	reconcileCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_reconcile_count",
			Help: "The number of times ipamd reconciles on ENIs and IP/Prefix addresses",
		},
		[]string{"fn"},
	)
	addIPCnt = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "awscni_add_ip_req_count",
			Help: "The number of add IP address requests",
		},
	)
	delIPCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_del_ip_req_count",
			Help: "The number of delete IP address requests",
		},
		[]string{"reason"},
	)
	podENIErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_pod_eni_error_count",
			Help: "The number of errors encountered for pod ENIs",
		},
		[]string{"fn"},
	)
	prometheusRegistered = false
)

// IPAMContext contains node level control information
type IPAMContext struct {
	awsClient                 awsutils.APIs
	dataStore                 *datastore.DataStore
	k8sClient                 client.Client
	enableIPv4                bool
	enableIPv6                bool
	useCustomNetworking       bool
	manageENIsNonScheduleable bool
	networkClient             networkutils.NetworkAPIs
	maxIPsPerENI              int
	maxENI                    int
	maxPrefixesPerENI         int
	unmanagedENI              int
	warmENITarget             int
	warmIPTarget              int
	minimumIPTarget           int
	warmPrefixTarget          int
	primaryIP                 map[string]string // primaryIP is a map from ENI ID to primary IP of that ENI
	lastNodeIPPoolAction      time.Time
	lastDecreaseIPPool        time.Time
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
	// if enableManageUntaggedMode is false, then consider all untagged ENIs as unmanaged.
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
		prometheus.MustRegister(ipamdErr)
		prometheus.MustRegister(ipamdActionsInprogress)
		prometheus.MustRegister(enisMax)
		prometheus.MustRegister(ipMax)
		prometheus.MustRegister(reconcileCnt)
		prometheus.MustRegister(addIPCnt)
		prometheus.MustRegister(delIPCnt)
		prometheus.MustRegister(podENIErr)
		prometheusRegistered = true
	}
}

// containsInsufficientCIDRsOrSubnetIPs returns whether a CIDR cannot be carved in the subnet or subnet is running out of IP addresses
func containsInsufficientCIDRsOrSubnetIPs(err error) bool {
	var awsErr awserr.Error
	// IP exhaustion can be due to Insufficient Cidr blocks or Insufficient Free Address in a Subnet
	// In these 2 cases we will back off for 2 minutes before retrying
	if errors.As(err, &awsErr) {
		log.Debugf("Insufficient IP Addresses due to: %v\n", awsErr.Code())
		return awsErr.Code() == INSUFFICIENT_CIDR_BLOCKS || awsErr.Code() == INSUFFICIENT_FREE_IP_SUBNET
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

// inInsufficientCidrCoolingPeriod checks whether IPAMD is in insufficientCidrErrorCooldown
func (c *IPAMContext) inInsufficientCidrCoolingPeriod() bool {
	return time.Since(c.lastInsufficientCidrError) <= insufficientCidrErrorCooldown
}

// New retrieves IP address usage information from Instance MetaData service and Kubelet
// then initializes IP address pool data store
func New(k8sClient client.Client) (*IPAMContext, error) {
	prometheusRegister()
	c := &IPAMContext{}
	c.k8sClient = k8sClient
	c.networkClient = networkutils.New()
	c.useCustomNetworking = UseCustomNetworkCfg()
	c.manageENIsNonScheduleable = ManageENIsOnNonSchedulableNode()
	c.enablePrefixDelegation = usePrefixDelegation()
	c.enableIPv4 = isIPv4Enabled()
	c.enableIPv6 = isIPv6Enabled()
	c.disableENIProvisioning = disableENIProvisioning()
	client, err := awsutils.New(c.useCustomNetworking, disableLeakedENICleanup(), c.enableIPv4, c.enableIPv6)
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
	c.enablePodENI = enablePodENI()
	c.enableManageUntaggedMode = enableManageUntaggedMode()
	c.enablePodIPAnnotation = enablePodIPAnnotation()

	err = c.awsClient.FetchInstanceTypeLimits()
	if err != nil {
		log.Errorf("Failed to get ENI limits from file:vpc_ip_limits or EC2 for %s", c.awsClient.GetInstanceType())
		return nil, err
	}

	// Validate if the configured combination of env variables is supported before proceeding further
	if !c.isConfigValid() {
		return nil, fmt.Errorf("ipamd: failed to validate configuration")
	}

	c.awsClient.InitCachedPrefixDelegation(c.enablePrefixDelegation)
	c.myNodeName = os.Getenv(envNodeName)
	checkpointer := datastore.NewJSONFile(dsBackingStorePath())
	c.dataStore = datastore.NewDataStore(log, checkpointer, c.enablePrefixDelegation)

	if err := c.nodeInit(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *IPAMContext) nodeInit() error {
	ipamdActionsInprogress.WithLabelValues("nodeInit").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("nodeInit").Sub(float64(1))
	var err error
	var vpcV4CIDRs []string
	ctx := context.TODO()

	log.Debugf("Start node init")
	primaryV4IP := c.awsClient.GetLocalIPv4()
	if err = c.initENIAndIPLimits(); err != nil {
		return err
	}

	if c.enableIPv4 {
		// Subnets currently will have both v4 and v6 CIDRs. Once EC2 launches v6 only Subnets, that will no longer
		// be true and so it is safe (and only required) to get the v4 CIDR info only when IPv4 mode is enabled.
		vpcV4CIDRs, err = c.awsClient.GetVPCIPv4CIDRs()
		if err != nil {
			return err
		}
	}

	primaryENIMac := c.awsClient.GetPrimaryENImac()
	err = c.networkClient.SetupHostNetwork(vpcV4CIDRs, primaryENIMac, &primaryV4IP, c.enablePodENI, c.enableIPv4, c.enableIPv6)
	if err != nil {
		return errors.Wrap(err, "ipamd init: failed to set up host network")
	}

	metadataResult, err := c.awsClient.DescribeAllENIs()
	if err != nil {
		return errors.Wrap(err, "ipamd init: failed to retrieve attached ENIs info")
	}

	log.Debugf("DescribeAllENIs success: ENIs: %d, tagged: %d", len(metadataResult.ENIMetadata), len(metadataResult.TagMap))
	c.awsClient.SetCNIUnmanagedENIs(metadataResult.MultiCardENIIDs)
	c.setUnmanagedENIs(metadataResult.TagMap)
	enis := c.filterUnmanagedENIs(metadataResult.ENIMetadata)

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

	if err := c.dataStore.ReadBackingStore(c.enableIPv6); err != nil {
		return err
	}

	if c.enableIPv6 {
		// We will not support upgrading/converting an existing IPv4 cluster to operate in IPv6 mode. So, we will always
		// start with a clean slate in IPv6 mode. We also don't have to deal with dynamic update of Prefix Delegation
		// feature in IPv6 mode as we don't support (yet) a non-PD v6 option. In addition, we don't support custom
		// networking & SGPP in IPv6 mode yet. So, we will skip the corresponding setup. Will save us from checking
		// if IPv6 is enabled at multiple places. Once we start supporting these features in IPv6 mode, we can do away
		// with this check and not change anything else in the below setup.
		return nil
	}

	if c.enablePrefixDelegation {
		// During upgrade or if prefix delgation knob is disabled to enabled then we
		// might have secondary IPs attached to ENIs so doing a cleanup if not used before moving on
		c.tryUnassignIPsFromENIs()
	} else {
		// When prefix delegation knob is enabled to disabled then we might
		// have unused prefixes attached to the ENIs so need to cleanup
		c.tryUnassignPrefixesFromENIs()
	}

	if err = c.configureIPRulesForPods(); err != nil {
		return err
	}
	// Spawning updateCIDRsRulesOnChange go-routine
	go wait.Forever(func() {
		vpcV4CIDRs = c.updateCIDRsRulesOnChange(vpcV4CIDRs)
	}, 30*time.Second)

	// RefreshSGIDs populates the ENI cache with ENI -> security group ID mappings, and so it must be called:
	// 1. after managed/unmanaged ENIs have been determined
	// 2. before any new ENIs are attached
	if c.enableIPv4 && !c.disableENIProvisioning {
		if err := c.awsClient.RefreshSGIDs(primaryENIMac); err != nil {
			return err
		}

		// Refresh security groups and VPC CIDR blocks in the background
		// Ignoring errors since we will retry in 30s
		go wait.Forever(func() {
			c.awsClient.RefreshSGIDs(primaryENIMac)
		}, 30*time.Second)
	}

	node, err := k8sapi.GetNode(ctx, c.k8sClient)
	if err != nil {
		log.Errorf("Failed to get node", err)
		podENIErrInc("nodeInit")
		return err
	}

	eniConfigName, err := eniconfig.GetNodeSpecificENIConfigName(node)
	if err == nil && c.useCustomNetworking && eniConfigName != "default" {
		// Add the feature name to CNINode of this node
		err := c.AddFeatureToCNINode(ctx, rcv1alpha1.CustomNetworking, eniConfigName)
		if err != nil {
			log.Errorf("Failed to add feature custom networking into CNINode", err)
			podENIErrInc("nodeInit")
			return err
		}
		log.Infof("Enabled feature %s in CNINode for node %s if not existing", rcv1alpha1.CustomNetworking, c.myNodeName)
	}

	if c.enablePodENI {
		// Check if we want to add feature into CNINode for trunk interface during node init
		// we don't check if the node already has trunk added during initialization
		c.askForTrunkENIIfNeeded(ctx)
	}

	// On node init, check if datastore pool needs to be increased. If so, attach CIDRs from existing ENIs and attach new ENIs.
	if !c.disableENIProvisioning && c.isDatastorePoolTooLow() {
		if err := c.increaseDatastorePool(ctx); err != nil {
			// Note that the only error currently returned by increaseDatastorePool is an error attaching CIDRs (other than insufficient IPs)
			podENIErrInc("nodeInit")
			return errors.New("error while trying to increase datastore pool")
		}
		// If custom networking is enabled and the pool is empty, return an error, as there is a misconfiguration and
		// the node should not become ready.
		if c.useCustomNetworking && c.isDatastorePoolEmpty() {
			podENIErrInc("nodeInit")
			return errors.New("Failed to attach any ENIs for custom networking")
		}
	}

	log.Debug("node init completed successfully")
	return nil
}

func (c *IPAMContext) configureIPRulesForPods() error {
	rules, err := c.networkClient.GetRuleList()
	if err != nil {
		log.Errorf("During ipamd init: failed to retrieve IP rule list %v", err)
		return nil
	}

	for _, info := range c.dataStore.AllocatedIPs() {
		// TODO(gus): This should really be done via CNI CHECK calls, rather than in ipam (requires upstream k8s changes).

		// Update ip rules in case there is a change in VPC CIDRs, AWS_VPC_K8S_CNI_EXTERNALSNAT setting
		srcIPNet := net.IPNet{IP: net.ParseIP(info.IP), Mask: net.IPv4Mask(255, 255, 255, 255)}

		err = c.networkClient.UpdateRuleListBySrc(rules, srcIPNet)
		if err != nil {
			log.Warnf("UpdateRuleListBySrc in nodeInit() failed for IP %s: %v", info.IP, err)
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
	newVPCCIDRs, err := c.awsClient.GetVPCIPv4CIDRs()
	if err != nil {
		log.Warnf("skipping periodic update to VPC CIDRs due to error: %v", err)
		return oldVPCCIDRs
	}

	old := sets.NewString(oldVPCCIDRs...)
	new := sets.NewString(newVPCCIDRs...)
	if !old.Equal(new) {
		primaryIP := c.awsClient.GetLocalIPv4()
		err = c.networkClient.UpdateHostIptablesRules(newVPCCIDRs, c.awsClient.GetPrimaryENImac(), &primaryIP, c.enableIPv4,
			c.enableIPv6)
		if err != nil {
			log.Warnf("unable to update host iptables rules for VPC CIDRs due to error: %v", err)
		}
	}
	return newVPCCIDRs
}

func (c *IPAMContext) updateIPStats(unmanaged int) {
	ipMax.Set(float64(c.maxIPsPerENI * (c.maxENI - unmanaged)))
	enisMax.Set(float64(c.maxENI - unmanaged))
}

// StartNodeIPPoolManager monitors the IP pool, add or del them when it is required.
func (c *IPAMContext) StartNodeIPPoolManager() {
	if c.enableIPv6 {
		//Nothing to do in IPv6 Mode. IPv6 is only supported in Prefix delegation mode
		//and VPC CNI will only attach one V6 Prefix.
		return
	}
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
	if c.enablePodENI && c.dataStore.GetTrunkENI() == "" {
		c.askForTrunkENIIfNeeded(ctx)
	}

	if c.isDatastorePoolTooLow() {
		c.increaseDatastorePool(ctx)
	} else if c.isDatastorePoolTooHigh() {
		c.decreaseDatastorePool(decreaseIPPoolInterval)
	}
	if c.shouldRemoveExtraENIs() {
		c.tryFreeENI()
	}
	// Prometheus Metric
	c.dataStore.GetENIUtilization()
}

// decreaseDatastorePool runs every `interval` and attempts to return unused ENIs and IPs
func (c *IPAMContext) decreaseDatastorePool(interval time.Duration) {
	ipamdActionsInprogress.WithLabelValues("decreaseDatastorePool").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("decreaseDatastorePool").Sub(float64(1))

	now := time.Now()
	timeSinceLast := now.Sub(c.lastDecreaseIPPool)
	if timeSinceLast <= interval {
		log.Debugf("Skipping decrease Datastore pool because time since last %v <= %v", timeSinceLast, interval)
		return
	}

	log.Debugf("Starting to decrease Datastore pool")
	c.tryUnassignCidrsFromAll()

	c.lastDecreaseIPPool = now
	c.lastNodeIPPoolAction = now

	log.Debugf("Successfully decreased IP pool")
	c.logPoolStats(c.dataStore.GetIPStats(ipV4AddrFamily))
}

// tryFreeENI always tries to free one ENI
func (c *IPAMContext) tryFreeENI() {
	if c.isTerminating() {
		log.Debug("AWS CNI is terminating, not detaching any ENIs")
		return
	}

	if !c.manageENIsNonScheduleable && c.isNodeNonSchedulable() {
		log.Debug("AWS CNI is on a non schedulable node, not detaching any ENIs")
		return
	}

	eni := c.dataStore.RemoveUnusedENIFromStore(c.warmIPTarget, c.minimumIPTarget, c.warmPrefixTarget)
	if eni == "" {
		return
	}

	log.Debugf("Start freeing ENI %s", eni)
	err := c.awsClient.FreeENI(eni)
	if err != nil {
		ipamdErrInc("decreaseIPPoolFreeENIFailed")
		log.Errorf("Failed to free ENI %s, err: %v", eni, err)
		return
	}

}

// tryUnassignIPsorPrefixesFromAll determines if there are IPs to free when we have extra IPs beyond the target and warmIPTargetDefined
// is enabled, deallocate extra IP addresses
func (c *IPAMContext) tryUnassignCidrsFromAll() {
	_, over, warmTargetDefined := c.datastoreTargetState()
	// If WARM IP targets are not defined, check if WARM_PREFIX_TARGET is defined.
	if !warmTargetDefined {
		over = c.computeExtraPrefixesOverWarmTarget()
	}

	if over > 0 {
		eniInfos := c.dataStore.GetENIInfos()
		for eniID := range eniInfos.ENIs {
			// Either returns prefixes or IPs [Cidrs]
			cidrs := c.dataStore.FindFreeableCidrs(eniID)
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
				err := c.dataStore.DelIPv4CidrFromStore(eniID, toDelete.Cidr, false /* force */)
				if err != nil {
					log.Warnf("Failed to delete Cidr %s on ENI %s from datastore: %s", toDelete, eniID, err)
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

func (c *IPAMContext) increaseDatastorePool(ctx context.Context) error {
	log.Debug("Starting to increase pool size")
	ipamdActionsInprogress.WithLabelValues("increaseDatastorePool").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("increaseDatastorePool").Sub(float64(1))

	short, _, warmIPTargetDefined := c.datastoreTargetState()
	if warmIPTargetDefined && short == 0 {
		log.Debugf("Skipping increase Datastore pool, warm target reached")
		return nil
	}

	if !warmIPTargetDefined {
		shortPrefix, warmTargetDefined := c.datastorePrefixTargetState()
		if warmTargetDefined && shortPrefix == 0 {
			log.Debugf("Skipping increase Datastore pool, warm prefix target reached")
			return nil
		}
	}

	if c.isTerminating() {
		log.Debug("AWS CNI is terminating, will not try to attach any new IPs or ENIs right now")
		return nil
	}
	if !c.manageENIsNonScheduleable && c.isNodeNonSchedulable() {
		log.Debug("AWS CNI is on a non schedulable node, will not try to attach any new IPs or ENIs right now")
		return nil
	}

	// Try to add more Cidrs to existing ENIs first.
	if c.inInsufficientCidrCoolingPeriod() {
		log.Debugf("Recently we had InsufficientCidr error hence will wait for %v before retrying", insufficientCidrErrorCooldown)
		return nil
	}

	increasedPool, err := c.tryAssignCidrs()
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
		c.updateLastNodeIPPoolAction()
	} else {
		// If we did not add any IPs, try to allocate an ENI.
		if c.hasRoomForEni() {
			if err = c.tryAllocateENI(ctx); err == nil {
				c.updateLastNodeIPPoolAction()
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

func (c *IPAMContext) updateLastNodeIPPoolAction() {
	c.lastNodeIPPoolAction = time.Now()
	stats := c.dataStore.GetIPStats(ipV4AddrFamily)
	if !c.enablePrefixDelegation {
		log.Debugf("Successfully increased IP pool: %s", stats)
	} else {
		log.Debugf("Successfully increased Prefix pool: %s", stats)
	}
	c.logPoolStats(stats)
}

func (c *IPAMContext) tryAllocateENI(ctx context.Context) error {
	var securityGroups []*string
	var subnet string

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
		subnet = eniCfg.Subnet
	}

	eni, err := c.awsClient.AllocENI(c.useCustomNetworking, securityGroups, subnet)
	if err != nil {
		log.Errorf("Failed to increase pool size due to not able to allocate ENI %v", err)
		ipamdErrInc("increaseIPPoolAllocENI")
		return err
	}

	resourcesToAllocate := c.GetENIResourcesToAllocate()
	_, err = c.awsClient.AllocIPAddresses(eni, resourcesToAllocate)
	if err != nil {
		log.Warnf("Failed to allocate %d IP addresses on an ENI: %v", resourcesToAllocate, err)
		// Continue to process the allocated IP addresses
		ipamdErrInc("increaseIPPoolAllocIPAddressesFailed")
		if containsInsufficientCIDRsOrSubnetIPs(err) {
			log.Errorf("Unable to attach IPs/Prefixes for the ENI, subnet doesn't seem to have enough IPs/Prefixes. Consider using new subnet or carve a reserved range using create-subnet-cidr-reservation")
			c.lastInsufficientCidrError = time.Now()
			return err
		}
	}

	eniMetadata, err := c.awsClient.WaitForENIAndIPsAttached(eni, resourcesToAllocate)
	if err != nil {
		ipamdErrInc("increaseIPPoolwaitENIAttachedFailed")
		log.Errorf("Failed to increase pool size: Unable to discover attached ENI from metadata service %v", err)
		return err
	}

	// The CNI does not create trunk or EFA ENIs, so they will always be false here
	err = c.setupENI(eni, eniMetadata, false, false)
	if err != nil {
		ipamdErrInc("increaseIPPoolsetupENIFailed")
		log.Errorf("Failed to increase pool size: %v", err)
		return err
	}
	return err
}

// For an ENI, try to fill in missing IPs on an existing ENI with PD disabled
// try to fill in missing Prefixes on an existing ENI with PD enabled
func (c *IPAMContext) tryAssignCidrs() (increasedPool bool, err error) {
	short, _, warmIPTargetDefined := c.datastoreTargetState()
	if warmIPTargetDefined && short == 0 {
		log.Infof("Warm IP target set and short is 0 so not assigning Cidrs (IPs or Prefixes)")
		return false, nil
	}

	if !warmIPTargetDefined {
		shortPrefix, warmTargetDefined := c.datastorePrefixTargetState()
		if warmTargetDefined && shortPrefix == 0 {
			log.Infof("Warm prefix target set and short is 0 so not assigning Cidrs (Prefixes)")
			return false, nil
		}
	}

	if !c.enablePrefixDelegation {
		return c.tryAssignIPs()
	} else {
		return c.tryAssignPrefixes()
	}
}

// For an ENI, try to fill in missing IPs on an existing ENI
func (c *IPAMContext) tryAssignIPs() (increasedPool bool, err error) {
	// If WARM_IP_TARGET is set, only proceed if we are short of target
	short, _, warmIPTargetDefined := c.datastoreTargetState()
	if warmIPTargetDefined && short == 0 {
		return false, nil
	}

	// If WARM_IP_TARGET is set we only want to allocate up to that target
	// to avoid overallocating and releasing
	toAllocate := c.maxIPsPerENI
	if warmIPTargetDefined {
		toAllocate = short
	}

	// Find an ENI where we can add more IPs
	eni := c.dataStore.GetENINeedsIP(c.maxIPsPerENI, c.useCustomNetworking)
	if eni != nil && len(eni.AvailableIPv4Cidrs) < c.maxIPsPerENI {
		currentNumberOfAllocatedIPs := len(eni.AvailableIPv4Cidrs)
		// Try to allocate all available IPs for this ENI
		resourcesToAllocate := min((c.maxIPsPerENI - currentNumberOfAllocatedIPs), toAllocate)
		output, err := c.awsClient.AllocIPAddresses(eni.ID, resourcesToAllocate)
		if err != nil && !containsPrivateIPAddressLimitExceededError(err) {
			log.Warnf("failed to allocate all available IP addresses on ENI %s, err: %v", eni.ID, err)
			// Try to just get one more IP
			output, err = c.awsClient.AllocIPAddresses(eni.ID, 1)
			if err != nil && !containsPrivateIPAddressLimitExceededError(err) {
				ipamdErrInc("increaseIPPoolAllocIPAddressesFailed")
				return false, errors.Wrap(err, fmt.Sprintf("failed to allocate one IP addresses on ENI %s, err ", eni.ID))
			}
		}

		var ec2ip4s []*ec2.NetworkInterfacePrivateIpAddress
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
				ec2ip4s = append(ec2ip4s, &ec2.NetworkInterfacePrivateIpAddress{PrivateIpAddress: aws.String(aws.StringValue(ec2Addr.PrivateIpAddress))})
			}
		}
		c.addENIsecondaryIPsToDataStore(ec2ip4s, eni.ID)
		return true, nil
	}
	return false, nil
}

func (c *IPAMContext) assignIPv6Prefix(eniID string) (err error) {
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
			ec2v6Prefixes = append(ec2v6Prefixes, &ec2.Ipv6PrefixSpecification{Ipv6Prefix: v6Prefix})
		}
		log.Debugf("Successfully allocated an IPv6Prefix for ENI: %s", eniID)
	} else if len(ec2v6Prefixes) > 1 {
		//Found more than one v6 prefix attached to the ENI. VPC CNI will only attach a single v6 prefix
		//and it will not attempt to free any additional Prefixes that are already attached.
		//Will use the first IPv6 Prefix attached for IP address allocation.
		ec2v6Prefixes = []*ec2.Ipv6PrefixSpecification{ec2v6Prefixes[0]}
	}
	c.addENIv6prefixesToDataStore(ec2v6Prefixes, eniID)
	return nil
}

func (c *IPAMContext) tryAssignPrefixes() (increasedPool bool, err error) {
	toAllocate := c.getPrefixesNeeded()
	// Returns an ENI which has space for more prefixes to be attached, but this
	// ENI might not suffice the WARM_IP_TARGET/WARM_PREFIX_TARGET
	eni := c.dataStore.GetENINeedsIP(c.maxPrefixesPerENI, c.useCustomNetworking)
	if eni != nil {
		currentNumberOfAllocatedPrefixes := len(eni.AvailableIPv4Cidrs)
		resourcesToAllocate := min((c.maxPrefixesPerENI - currentNumberOfAllocatedPrefixes), toAllocate)
		output, err := c.awsClient.AllocIPAddresses(eni.ID, resourcesToAllocate)
		if err != nil && !containsPrivateIPAddressLimitExceededError(err) {
			log.Warnf("failed to allocate all available IPv4 Prefixes on ENI %s, err: %v", eni.ID, err)
			// Try to just get one more prefix
			output, err = c.awsClient.AllocIPAddresses(eni.ID, 1)
			if err != nil && !containsPrivateIPAddressLimitExceededError(err) {
				ipamdErrInc("increaseIPPoolAllocIPAddressesFailed")
				return false, errors.Wrap(err, fmt.Sprintf("failed to allocate one IPv4 prefix on ENI %s, err: %v", eni.ID, err))
			}
		}
		var ec2Prefixes []*ec2.Ipv4PrefixSpecification
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
		c.addENIv4prefixesToDataStore(ec2Prefixes, eni.ID)
		return true, nil
	}
	return false, nil
}

// setupENI does following:
// 1) add ENI to datastore
// 2) set up linux ENI related networking stack.
// 3) add all ENI's secondary IP addresses to datastore
func (c *IPAMContext) setupENI(eni string, eniMetadata awsutils.ENIMetadata, isTrunkENI, isEFAENI bool) error {
	primaryENI := c.awsClient.GetPrimaryENI()
	// Add the ENI to the datastore
	err := c.dataStore.AddENI(eni, eniMetadata.DeviceNumber, eni == primaryENI, isTrunkENI, isEFAENI)
	if err != nil && err.Error() != datastore.DuplicatedENIError {
		return errors.Wrapf(err, "failed to add ENI %s to data store", eni)
	}
	// Store the primary IP of the ENI
	c.primaryIP[eni] = eniMetadata.PrimaryIPv4Address()

	if c.enableIPv6 && eni == primaryENI {
		// In v6 PD Mode, VPC CNI will only manage primary ENI. Once we start supporting secondary IP and custom
		// networking modes for v6, we will relax this restriction. We filter out all the ENIs except Primary ENI
		// in v6 mode (prior to landing here), but included the primary ENI check as a safety net.
		err := c.assignIPv6Prefix(eni)
		if err != nil {
			return errors.Wrapf(err, "Failed to allocate IPv6 Prefixes to Primary ENI")
		}
	} else {
		// For secondary ENIs, set up the network
		if eni != primaryENI {
			err = c.networkClient.SetupENINetwork(c.primaryIP[eni], eniMetadata.MAC, eniMetadata.DeviceNumber, eniMetadata.SubnetIPv4CIDR)
			if err != nil {
				// Failed to set up the ENI
				errRemove := c.dataStore.RemoveENIFromDataStore(eni, true)
				if errRemove != nil {
					log.Warnf("failed to remove ENI %s: %v", eni, errRemove)
				}
				delete(c.primaryIP, eni)
				return errors.Wrapf(err, "failed to set up ENI %s network", eni)
			}
		}
		log.Infof("Found ENIs having %d secondary IPs and %d Prefixes", len(eniMetadata.IPv4Addresses), len(eniMetadata.IPv4Prefixes))
		// Either case add the IPs and prefixes to datastore.
		c.addENIsecondaryIPsToDataStore(eniMetadata.IPv4Addresses, eni)
		c.addENIv4prefixesToDataStore(eniMetadata.IPv4Prefixes, eni)
	}

	return nil
}

func (c *IPAMContext) addENIsecondaryIPsToDataStore(ec2PrivateIpAddrs []*ec2.NetworkInterfacePrivateIpAddress, eni string) {
	// Add all the secondary IPs
	for _, ec2PrivateIpAddr := range ec2PrivateIpAddrs {
		if aws.BoolValue(ec2PrivateIpAddr.Primary) {
			continue
		}
		cidr := net.IPNet{IP: net.ParseIP(aws.StringValue(ec2PrivateIpAddr.PrivateIpAddress)), Mask: net.IPv4Mask(255, 255, 255, 255)}
		err := c.dataStore.AddIPv4CidrToStore(eni, cidr, false)
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Warnf("Failed to increase IP pool, failed to add IP %s to data store", ec2PrivateIpAddr.PrivateIpAddress)
			// continue to add next address
			ipamdErrInc("addENIsecondaryIPsToDataStoreFailed")
		}
	}
	c.logPoolStats(c.dataStore.GetIPStats(ipV4AddrFamily))
}

func (c *IPAMContext) addENIv4prefixesToDataStore(ec2PrefixAddrs []*ec2.Ipv4PrefixSpecification, eni string) {
	// Walk thru all prefixes
	for _, ec2PrefixAddr := range ec2PrefixAddrs {
		strIpv4Prefix := aws.StringValue(ec2PrefixAddr.Ipv4Prefix)
		_, ipnet, err := net.ParseCIDR(strIpv4Prefix)
		if err != nil {
			//Parsing failed, get next prefix
			log.Debugf("Parsing failed, moving on to next prefix")
			continue
		}
		cidr := *ipnet
		err = c.dataStore.AddIPv4CidrToStore(eni, cidr, true)
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Warnf("Failed to increase Prefix pool, failed to add Prefix %s to data store", ec2PrefixAddr.Ipv4Prefix)
			// continue to add next address
			ipamdErrInc("addENIv4prefixesToDataStoreFailed")
		}
	}
	c.logPoolStats(c.dataStore.GetIPStats(ipV4AddrFamily))
}

func (c *IPAMContext) addENIv6prefixesToDataStore(ec2PrefixAddrs []*ec2.Ipv6PrefixSpecification, eni string) {
	log.Debugf("Updating datastore with IPv6Prefix(es) for ENI: %v, count: %v", eni, len(ec2PrefixAddrs))
	// Walk through all prefixes
	for _, ec2PrefixAddr := range ec2PrefixAddrs {
		strIpv6Prefix := aws.StringValue(ec2PrefixAddr.Ipv6Prefix)
		_, ipnet, err := net.ParseCIDR(strIpv6Prefix)
		if err != nil {
			// Parsing failed, get next prefix
			log.Debugf("Parsing failed, moving on to next prefix")
			continue
		}
		cidr := *ipnet
		err = c.dataStore.AddIPv6CidrToStore(eni, cidr, true)
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Warnf("Failed to increase Prefix pool, failed to add Prefix %s to data store", ec2PrefixAddr.Ipv6Prefix)
			// continue to add next address
			ipamdErrInc("addENIv6prefixesToDataStoreFailed")
		}
	}
	c.logPoolStats(c.dataStore.GetIPStats(ipV6AddrFamily))
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
func (c *IPAMContext) logPoolStats(dataStoreStats *datastore.DataStoreStats) {
	prefix := "IP pool stats"
	if c.enablePrefixDelegation {
		prefix = "Prefix pool stats"
	}
	log.Debugf("%s: %s, c.maxIPsPerENI = %d", prefix, dataStoreStats, c.maxIPsPerENI)
}

func (c *IPAMContext) askForTrunkENIIfNeeded(ctx context.Context) {
	// Check that there is room for a trunk ENI to be attached.
	if c.dataStore.GetENIs() >= (c.maxENI - c.unmanagedENI) {
		log.Error("No slot available for a trunk ENI to be attached.")
		return
	}

	// We need to signal that VPC Resource Controller needs to attach a trunk ENI.
	err := c.AddFeatureToCNINode(ctx, rcv1alpha1.SecurityGroupsForPods, "")
	if err != nil {
		podENIErrInc("askForTrunkENIIfNeeded")
		log.Errorf("Failed to add SGP feature to CNINode resource", err)
	} else {
		log.Infof("Successfully added feature %s to CNINode if not existing", rcv1alpha1.SecurityGroupsForPods)
	}
}

// shouldRemoveExtraENIs returns true if we should attempt to find an ENI to free. When WARM_IP_TARGET is set, we
// always check and do verification in getDeletableENI()
// PD enabled : If the WARM_PREFIX_TARGET is spread across ENIs and we have more than needed then this function will return true.
// but if the number of prefixes are on just one ENI and is more than available even then it returns true so getDeletableENI will
// recheck if we need the ENI for prefix target.
func (c *IPAMContext) shouldRemoveExtraENIs() bool {
	_, _, warmTargetDefined := c.datastoreTargetState()
	if warmTargetDefined {
		return true
	}

	stats := c.dataStore.GetIPStats(ipV4AddrFamily)
	available := stats.AvailableAddresses()
	var shouldRemoveExtra bool

	// We need the +1 to make sure we are not going below the WARM_ENI_TARGET/WARM_PREFIX_TARGET
	warmTarget := (c.warmENITarget + 1)

	if c.enablePrefixDelegation {
		warmTarget = (c.warmPrefixTarget + 1)
	}

	shouldRemoveExtra = available >= (warmTarget)*c.maxIPsPerENI

	if shouldRemoveExtra {
		c.logPoolStats(stats)
		log.Debugf("It might be possible to remove extra ENIs because available (%d) >= (ENI/Prefix target + 1 (%d) + 1) * addrsPerENI (%d)", available, warmTarget, c.maxIPsPerENI)
	} else if c.enablePrefixDelegation {
		// When prefix target count is reduced, datastorehigh would have deleted extra prefixes over the warm prefix target.
		// Hence available will be less than (warmTarget)*c.maxIPsPerENI but there can be some extra ENIs which are not used hence see if we can clean it up.
		shouldRemoveExtra = c.dataStore.CheckFreeableENIexists()
	}
	return shouldRemoveExtra
}

func (c *IPAMContext) computeExtraPrefixesOverWarmTarget() int {
	over := 0
	if !c.warmPrefixTargetDefined() {
		return over
	}

	freePrefixes := c.dataStore.GetFreePrefixes()
	over = max(freePrefixes-c.warmPrefixTarget, 0)

	stats := c.dataStore.GetIPStats(ipV4AddrFamily)
	log.Debugf("computeExtraPrefixesOverWarmTarget available %d over %d warm_prefix_target %d", stats.AvailableAddresses(), over, c.warmPrefixTarget)
	c.logPoolStats(stats)
	return over
}

func ipamdErrInc(fn string) {
	ipamdErr.With(prometheus.Labels{"fn": fn}).Inc()
}

func podENIErrInc(fn string) {
	podENIErr.With(prometheus.Labels{"fn": fn}).Inc()
}

// nodeIPPoolReconcile reconcile ENI and IP info from metadata service and IP addresses in datastore
func (c *IPAMContext) nodeIPPoolReconcile(ctx context.Context, interval time.Duration) {
	// To reduce the number of EC2 API calls, skip reconciliation if IPs were recently added to the datastore.
	timeSinceLast := time.Since(c.lastNodeIPPoolAction)
	// Make an exception if node needs a trunk ENI and one is not currently attached.
	needsTrunkEni := c.enablePodENI && c.dataStore.GetTrunkENI() == ""
	if timeSinceLast <= interval && !needsTrunkEni {
		return
	}

	ipamdActionsInprogress.WithLabelValues("nodeIPPoolReconcile").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("nodeIPPoolReconcile").Sub(float64(1))

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
	attachedENIs := c.filterUnmanagedENIs(allENIs)
	currentENIs := c.dataStore.GetENIInfos().ENIs
	trunkENI := c.dataStore.GetTrunkENI()
	// Initialize the set with the known EFA interfaces
	efaENIs := c.dataStore.GetEFAENIs()

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
		metadataResult, err := c.awsClient.DescribeAllENIs()
		if err != nil {
			log.Warnf("Failed to call EC2 to describe ENIs, aborting reconcile: %v", err)
			return
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
		c.awsClient.SetCNIUnmanagedENIs(metadataResult.MultiCardENIIDs)
		attachedENIs = c.filterUnmanagedENIs(metadataResult.ENIMetadata)
	}

	// Mark phase
	for _, attachedENI := range attachedENIs {
		eniIPPool, eniPrefixPool, err := c.dataStore.GetENICIDRs(attachedENI.ENIID)
		if err == nil {
			// If the attached ENI is in the data store
			log.Debugf("Reconcile existing ENI %s IP pool", attachedENI.ENIID)
			// Reconcile IP pool
			c.eniIPPoolReconcile(eniIPPool, attachedENI, attachedENI.ENIID)
			// If the attached ENI is in the data store
			log.Debugf("Reconcile existing ENI %s IP prefixes", attachedENI.ENIID)
			// Reconcile IP pool
			c.eniPrefixPoolReconcile(eniPrefixPool, attachedENI, attachedENI.ENIID)
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
		log.Debugf("Reconcile and add a new ENI %s", attachedENI)
		err = c.setupENI(attachedENI.ENIID, attachedENI, isTrunkENI, isEFAENI)
		if err != nil {
			log.Errorf("IP pool reconcile: Failed to set up ENI %s network: %v", attachedENI.ENIID, err)
			ipamdErrInc("eniReconcileAdd")
			// Continue if having trouble with ONLY 1 ENI, instead of bailout here?
			continue
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniReconcileAdd"}).Inc()
	}

	// Sweep phase: since the marked ENI have been removed, the remaining ones needs to be sweeped
	for eni := range currentENIs {
		log.Infof("Reconcile and delete detached ENI %s", eni)
		// Force the delete, since aws local metadata has told us that this ENI is no longer
		// attached, so any IPs assigned from this ENI will no longer work.
		err = c.dataStore.RemoveENIFromDataStore(eni, true /* force */)
		if err != nil {
			log.Errorf("IP pool reconcile: Failed to delete ENI during reconcile: %v", err)
			ipamdErrInc("eniReconcileDel")
			continue
		}
		delete(c.primaryIP, eni)
		reconcileCnt.With(prometheus.Labels{"fn": "eniReconcileDel"}).Inc()
	}
	c.lastNodeIPPoolAction = time.Now()

	log.Debug("Successfully Reconciled ENI/IP pool")
	c.logPoolStats(c.dataStore.GetIPStats(ipV4AddrFamily))
}

func (c *IPAMContext) eniIPPoolReconcile(ipPool []string, attachedENI awsutils.ENIMetadata, eni string) {
	attachedENIIPs := attachedENI.IPv4Addresses
	needEC2Reconcile := true
	// Here we can't trust attachedENI since the IMDS metadata can be stale. We need to check with EC2 API.
	// +1 is for the primary IP of the ENI that is not added to the ipPool and not available for pods to use.
	if 1+len(ipPool) != len(attachedENIIPs) {
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
	seenIPs := c.verifyAndAddIPsToDatastore(eni, attachedENIIPs, needEC2Reconcile)

	// Sweep phase, delete remaining IPs since they should not remain in the datastore
	for _, existingIP := range ipPool {
		if seenIPs[existingIP] {
			continue
		}

		log.Debugf("Reconcile and delete IP %s on ENI %s", existingIP, eni)
		// Force the delete, since we have verified with EC2 that these secondary IPs are no longer assigned to this ENI
		ipv4Addr := net.IPNet{IP: net.ParseIP(existingIP), Mask: net.IPv4Mask(255, 255, 255, 255)}
		err := c.dataStore.DelIPv4CidrFromStore(eni, ipv4Addr, true /* force */)
		if err != nil {
			log.Errorf("Failed to reconcile and delete IP %s on ENI %s, %v", existingIP, eni, err)
			ipamdErrInc("ipReconcileDel")
			// continue instead of bailout due to one ip
			continue
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniIPPoolReconcileDel"}).Inc()
	}
}

func (c *IPAMContext) eniPrefixPoolReconcile(ipPool []string, attachedENI awsutils.ENIMetadata, eni string) {
	attachedENIIPs := attachedENI.IPv4Prefixes
	needEC2Reconcile := true
	// Here we can't trust attachedENI since the IMDS metadata can be stale. We need to check with EC2 API.
	log.Debugf("Found prefix pool count %d for eni %s\n", len(ipPool), eni)

	if len(ipPool) != len(attachedENIIPs) {
		log.Warnf("Instance metadata does not match data store! ipPool: %v, metadata: %v", ipPool, attachedENIIPs)
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
	seenIPs := c.verifyAndAddPrefixesToDatastore(eni, attachedENIIPs, needEC2Reconcile)

	// Sweep phase, delete remaining Prefixes since they should not remain in the datastore
	for _, existingIP := range ipPool {
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
		err = c.dataStore.DelIPv4CidrFromStore(eni, *ipv4Cidr, true /* force */)
		if err != nil {
			log.Errorf("Failed to reconcile and delete IP %s on ENI %s, %v", existingIP, eni, err)
			ipamdErrInc("ipReconcileDel")
			// continue instead of bailout due to one ip
			continue
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniIPPoolReconcileDel"}).Inc()
	}
}

// verifyAndAddIPsToDatastore updates the datastore with the known secondary IPs. IPs who are out of cooldown gets added
// back to the datastore after being verified against EC2.
func (c *IPAMContext) verifyAndAddIPsToDatastore(eni string, attachedENIIPs []*ec2.NetworkInterfacePrivateIpAddress, needEC2Reconcile bool) map[string]bool {
	var ec2VerifiedAddresses []*ec2.NetworkInterfacePrivateIpAddress
	seenIPs := make(map[string]bool)
	for _, privateIPv4 := range attachedENIIPs {
		strPrivateIPv4 := aws.StringValue(privateIPv4.PrivateIpAddress)
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
						if strPrivateIPv4 == aws.StringValue(ec2Addr.PrivateIpAddress) {
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
		err := c.dataStore.AddIPv4CidrToStore(eni, ipv4Addr, false)
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Errorf("Failed to reconcile IP %s on ENI %s", strPrivateIPv4, eni)
			ipamdErrInc("ipReconcileAdd")
			// Continue to check the other IPs instead of bailout due to one wrong IP
			continue

		}
		// Mark action
		seenIPs[strPrivateIPv4] = true
		reconcileCnt.With(prometheus.Labels{"fn": "eniDataStorePoolReconcileAdd"}).Inc()
	}
	return seenIPs
}

// verifyAndAddPrefixesToDatastore updates the datastore with the known Prefixes. Prefixes who are out of cooldown gets added
// back to the datastore after being verified against EC2.
func (c *IPAMContext) verifyAndAddPrefixesToDatastore(eni string, attachedENIPrefixes []*ec2.Ipv4PrefixSpecification, needEC2Reconcile bool) map[string]bool {
	var ec2VerifiedAddresses []*ec2.Ipv4PrefixSpecification
	seenIPs := make(map[string]bool)
	for _, privateIPv4Cidr := range attachedENIPrefixes {
		strPrivateIPv4Cidr := aws.StringValue(privateIPv4Cidr.Ipv4Prefix)
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
						if strPrivateIPv4Cidr == aws.StringValue(ec2Addr.Ipv4Prefix) {
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

		err = c.dataStore.AddIPv4CidrToStore(eni, *ipv4CidrPtr, true)
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Errorf("Failed to reconcile Prefix %s on ENI %s", strPrivateIPv4Cidr, eni)
			ipamdErrInc("prefixReconcileAdd")
			// Continue to check the other Prefixs instead of bailout due to one wrong IP
			continue

		}
		// Mark action
		seenIPs[strPrivateIPv4Cidr] = true
		reconcileCnt.With(prometheus.Labels{"fn": "eniDataStorePoolReconcileAdd"}).Inc()
	}
	return seenIPs
}

// UseCustomNetworkCfg returns whether Pods needs to use pod specific configuration or not.
func UseCustomNetworkCfg() bool {
	return parseBoolEnvVar(envCustomNetworkCfg, false)
}

// ManageENIsOnNonSchedulableNode returns whether IPAMd should manage ENIs on the node or not.
func ManageENIsOnNonSchedulableNode() bool {
	return parseBoolEnvVar(envManageENIsNonSchedulable, false)
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
	return getEnvBoolWithDefault(envDisableENIProvisioning, false)
}

func disableLeakedENICleanup() bool {
	// Cases where leaked ENI cleanup is disabled:
	// 1. IPv6 is enabled, so no ENIs are attached
	// 2. ENI provisioning is disabled, so ENIs are not managed by IPAMD
	// 3. Environment var explicitly disabling task is set
	return isIPv6Enabled() || disableENIProvisioning() || getEnvBoolWithDefault(envDisableLeakedENICleanup, false)
}

func enablePodENI() bool {
	return getEnvBoolWithDefault(envEnablePodENI, false)
}

func usePrefixDelegation() bool {
	return getEnvBoolWithDefault(envEnableIpv4PrefixDelegation, false)
}

func isIPv4Enabled() bool {
	return getEnvBoolWithDefault(envEnableIPv4, false)
}

func isIPv6Enabled() bool {
	return getEnvBoolWithDefault(envEnableIPv6, false)
}

func enableManageUntaggedMode() bool {
	return getEnvBoolWithDefault(envManageUntaggedENI, true)
}

func enablePodIPAnnotation() bool {
	return getEnvBoolWithDefault(envAnnotatePodIP, false)
}

// filterUnmanagedENIs filters out ENIs marked with the "node.k8s.amazonaws.com/no_manage" tag
func (c *IPAMContext) filterUnmanagedENIs(enis []awsutils.ENIMetadata) []awsutils.ENIMetadata {
	numFiltered := 0
	ret := make([]awsutils.ENIMetadata, 0, len(enis))
	for _, eni := range enis {
		//Filter out any Unmanaged ENIs. VPC CNI will only work with Primary ENI in IPv6 Prefix Delegation mode until
		//we open up IPv6 support in Secondary IP and Custom networking modes. Filtering out the ENIs here will
		//help us avoid myriad of if/else loops elsewhere in the code.
		if c.enableIPv6 && !c.awsClient.IsPrimaryENI(eni.ENIID) {
			log.Debugf("Skipping ENI %s: IPv6 Mode is enabled and VPC CNI will only manage Primary ENI in v6 PD mode",
				eni.ENIID)
			numFiltered++
			continue
		} else if c.awsClient.IsUnmanagedENI(eni.ENIID) {
			log.Debugf("Skipping ENI %s: since it is unmanaged", eni.ENIID)
			numFiltered++
			continue
		} else if c.awsClient.IsCNIUnmanagedENI(eni.ENIID) {
			log.Debugf("Skipping ENI %s: since on non-zero network card", eni.ENIID)
			numFiltered++
			continue
		}
		ret = append(ret, eni)
	}
	c.unmanagedENI = numFiltered
	c.updateIPStats(numFiltered)
	return ret
}

// datastoreTargetState determines the number of IPs `short` or `over` our WARM_IP_TARGET,
// accounting for the MINIMUM_IP_TARGET
// With prefix delegation this function determines the number of Prefixes `short` or `over`
func (c *IPAMContext) datastoreTargetState() (short int, over int, enabled bool) {
	if c.warmIPTarget == noWarmIPTarget && c.minimumIPTarget == noMinimumIPTarget {
		// there is no WARM_IP_TARGET defined and no MINIMUM_IP_TARGET, fallback to use all IP addresses on ENI
		return 0, 0, false
	}

	stats := c.dataStore.GetIPStats(ipV4AddrFamily)
	available := stats.AvailableAddresses()

	// short is greater than 0 when we have fewer available IPs than the warm IP target
	short = max(c.warmIPTarget-available, 0)

	// short is greater than the warm IP target alone when we have fewer total IPs than the minimum target
	short = max(short, c.minimumIPTarget-stats.TotalIPs)

	// over is the number of available IPs we have beyond the warm IP target
	over = max(available-c.warmIPTarget, 0)

	// over is less than the warm IP target alone if it would imply reducing total IPs below the minimum target
	over = max(min(over, stats.TotalIPs-c.minimumIPTarget), 0)

	if c.enablePrefixDelegation {
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
		freePrefixes := c.dataStore.GetFreePrefixes()
		overPrefix := max(min(freePrefixes, stats.TotalPrefixes-prefixNeededForWarmIP), 0)
		overPrefix = max(min(overPrefix, stats.TotalPrefixes-prefixNeededForMinIP), 0)
		log.Debugf("Current warm IP stats : target: %d, short(prefixes): %d, over(prefixes): %d, stats: %s", c.warmIPTarget, shortPrefix, overPrefix, stats)
		return shortPrefix, overPrefix, true
	}
	log.Debugf("Current warm IP stats : target: %d, short: %d, over: %d, stats: %s", c.warmIPTarget, short, over, stats)

	return short, over, true
}

// datastorePrefixTargetState determines the number of prefixes short to reach WARM_PREFIX_TARGET
func (c *IPAMContext) datastorePrefixTargetState() (short int, enabled bool) {
	if !c.warmPrefixTargetDefined() {
		return 0, false
	}
	// /28 will consume 16 IPs so let's not allocate if not needed.
	freePrefixesInStore := c.dataStore.GetFreePrefixes()
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
	trunkENI := c.dataStore.GetTrunkENI()
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
					return fmt.Errorf("Released IP %s does not match existing annotation. Not patching pod.", releasedIP)
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
	log.Debugf("In tryUnassignIPsFromENIs")
	eniInfos := c.dataStore.GetENIInfos()
	for eniID := range eniInfos.ENIs {
		c.tryUnassignIPFromENI(eniID)
	}
}

func (c *IPAMContext) tryUnassignIPFromENI(eniID string) {
	freeableIPs := c.dataStore.FreeableIPs(eniID)

	if len(freeableIPs) == 0 {
		log.Debugf("No freeable IPs")
		return
	}

	// Delete IPs from datastore
	var deletedIPs []string
	for _, toDelete := range freeableIPs {
		// Don't force the delete, since a freeable IP might have been assigned to a pod
		// before we get around to deleting it.
		err := c.dataStore.DelIPv4CidrFromStore(eniID, toDelete, false /* force */)
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
	eniInfos := c.dataStore.GetENIInfos()
	for eniID := range eniInfos.ENIs {
		c.tryUnassignPrefixFromENI(eniID)
	}
}

func (c *IPAMContext) tryUnassignPrefixFromENI(eniID string) {
	freeablePrefixes := c.dataStore.FreeablePrefixes(eniID)
	if len(freeablePrefixes) == 0 {
		return
	}
	// Delete Prefixes from datastore
	var deletedPrefixes []string
	for _, toDelete := range freeablePrefixes {
		// Don't force the delete, since a freeable Prefix might have been assigned to a pod
		// before we get around to deleting it.
		err := c.dataStore.DelIPv4CidrFromStore(eniID, toDelete, false /* force */)
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

func (c *IPAMContext) GetENIResourcesToAllocate() int {
	var resourcesToAllocate int
	if !c.enablePrefixDelegation {
		resourcesToAllocate = c.maxIPsPerENI
		short, _, warmTargetDefined := c.datastoreTargetState()
		if warmTargetDefined {
			resourcesToAllocate = min(short, c.maxIPsPerENI)
		}
	} else {
		resourcesToAllocate = min(c.getPrefixesNeeded(), c.maxPrefixesPerENI)
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

func (c *IPAMContext) isDatastorePoolEmpty() bool {
	stats := c.dataStore.GetIPStats(ipV4AddrFamily)
	return stats.TotalIPs == 0
}

// Return whether the maximum number of ENIs that can be attached to the node has already been reached
func (c *IPAMContext) hasRoomForEni() bool {
	trunkEni := 0
	if c.enablePodENI && c.dataStore.GetTrunkENI() == "" {
		trunkEni = 1
	}
	return c.dataStore.GetENIs() < (c.maxENI - c.unmanagedENI - trunkEni)
}

func (c *IPAMContext) isDatastorePoolTooLow() bool {
	short, _, warmTargetDefined := c.datastoreTargetState()
	if warmTargetDefined {
		return short > 0
	}

	stats := c.dataStore.GetIPStats(ipV4AddrFamily)
	available := stats.AvailableAddresses()

	warmTarget := c.warmENITarget
	totalIPs := c.maxIPsPerENI

	if c.enablePrefixDelegation {
		warmTarget = c.warmPrefixTarget
		_, maxIpsPerPrefix, _ := datastore.GetPrefixDelegationDefaults()
		totalIPs = maxIpsPerPrefix
	}

	poolTooLow := available < totalIPs*warmTarget || (warmTarget == 0 && available == 0)
	if poolTooLow {
		log.Debugf("IP pool is too low: available (%d) < ENI target (%d) * addrsPerENI (%d)", available, warmTarget, totalIPs)
		c.logPoolStats(stats)
	}
	return poolTooLow
}

func (c *IPAMContext) isDatastorePoolTooHigh() bool {
	_, over, warmTargetDefined := c.datastoreTargetState()
	if warmTargetDefined {
		return over > 0
	}

	//For the existing ENIs check if we can cleanup prefixes
	if c.warmPrefixTargetDefined() {
		freePrefixes := c.dataStore.GetFreePrefixes()
		poolTooHigh := freePrefixes > c.warmPrefixTarget
		if poolTooHigh {
			log.Debugf("Prefix pool is high so might be able to deallocate : free prefixes %d and warm prefix target %d", freePrefixes, c.warmPrefixTarget)
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
func (c *IPAMContext) getPrefixesNeeded() int {

	//By default allocate 1 prefix at a time
	toAllocate := 1

	//TODO - post GA we can evaluate to see if these two calls can be merged.
	//datastoreTargetState already has complex math so adding Prefix target will make it
	//even more complex.
	short, _, warmIPTargetDefined := c.datastoreTargetState()
	shortPrefixes, warmPrefixTargetDefined := c.datastorePrefixTargetState()

	//WARM_IP_TARGET takes precendence over WARM_PREFIX_TARGET
	if warmIPTargetDefined {
		toAllocate = max(toAllocate, short)
	} else if warmPrefixTargetDefined {
		toAllocate = max(toAllocate, shortPrefixes)
	}
	log.Debugf("ToAllocate: %d", toAllocate)
	return toAllocate
}

func (c *IPAMContext) initENIAndIPLimits() (err error) {
	if c.enableIPv4 {
		nodeMaxENI, err := c.getMaxENI()
		if err != nil {
			log.Error("Failed to get ENI limit")
			return err
		}
		c.maxENI = nodeMaxENI

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
	//Validate that only one among v4 and v6 is enabled.
	if c.enableIPv4 && c.enableIPv6 {
		log.Errorf("IPv4 and IPv6 are both enabled. VPC CNI currently doesn't support dual stack mode")
		return false
	} else if !c.enableIPv4 && !c.enableIPv6 {
		log.Errorf("IPv4 and IPv6 are both disabled. One of them have to be enabled")
		return false
	}

	//Validate PD mode is enabled if VPC CNI is operating in IPv6 mode. SGPP and Custom networking are not supported in IPv6 mode.
	if c.enableIPv6 && (c.enablePodENI || c.useCustomNetworking || !c.enablePrefixDelegation) {
		log.Errorf("IPv6 is supported only in Prefix Delegation mode. Security Group Per Pod and " +
			"Custom Networking are not supported in IPv6 mode. Please set the env variables accordingly.")
		return false
	}

	//Validate Prefix Delegation against v4 and v6 modes.
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
