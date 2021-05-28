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
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/eniconfig"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
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
	//     If WARM-IP-TARGET is set to 1, and there are 9 pods running on the node, ipamd will try
	//     to make the "warm pool" have 10 IP addresses with 9 being assigned to pods and 1 free IP.
	//
	//     If "WARM-IP-TARGET is not set, it will default to 30 (which the maximum number of IPs per ENI).
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
	// when "WARM-IP-TARGET" is defined, ipamd will use behavior defined for "WARM-IP-TARGET".
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

	// eniNoManageTagKey is the tag that may be set on an ENI to indicate ipamd
	// should not manage it in any form.
	eniNoManageTagKey = "node.k8s.amazonaws.com/no_manage"

	// disableENIProvisioning is used to specify that ENI doesn't need to be synced during initializing a pod.
	envDisableENIProvisioning = "DISABLE_NETWORK_RESOURCE_PROVISIONING"

	// Specify where ipam should persist its current IP<->container allocations.
	envBackingStorePath     = "AWS_VPC_K8S_CNI_BACKING_STORE"
	defaultBackingStorePath = "/var/run/aws-node/ipam.json"

	// envEnablePodENI is used to attach a Trunk ENI to every node. Required in order to give Branch ENIs to pods.
	envEnablePodENI = "ENABLE_POD_ENI"

	// envNodeName will be used to store Node name
	envNodeName = "MY_NODE_NAME"

	// vpcENIConfigLabel is used by the VPC resource controller to pick the right ENI config.
	vpcENIConfigLabel = "vpc.amazonaws.com/eniConfig"
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
			Help: "The number of times ipamd reconciles on ENIs and IP addresses",
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
	awsClient            awsutils.APIs
	dataStore            *datastore.DataStore
	rawK8SClient         client.Client
	cachedK8SClient      client.Client
	useCustomNetworking  bool
	networkClient        networkutils.NetworkAPIs
	maxIPsPerENI         int
	maxENI               int
	unmanagedENI         int
	warmENITarget        int
	warmIPTarget         int
	minimumIPTarget      int
	primaryIP            map[string]string // primaryIP is a map from ENI ID to primary IP of that ENI
	lastNodeIPPoolAction time.Time
	lastDecreaseIPPool   time.Time
	// reconcileCooldownCache keeps timestamps of the last time an IP address was unassigned from an ENI,
	// so that we don't reconcile and add it back too quickly if IMDS lags behind reality.
	reconcileCooldownCache ReconcileCooldownCache
	terminating            int32 // Flag to warn that the pod is about to shut down.
	disableENIProvisioning bool
	enablePodENI           bool
	myNodeName             string
}

// setUnmanagedENIs will rebuild the set of ENI IDs for ENIs tagged as "no_manage"
func (c *IPAMContext) setUnmanagedENIs(tagMap map[string]awsutils.TagMap) {
	if len(tagMap) == 0 {
		return
	}
	var unmanagedENIlist []string
	for eniID, tags := range tagMap {
		if tags[eniNoManageTagKey] == "true" {
			if eniID == c.awsClient.GetPrimaryENI() {
				log.Debugf("Ignoring no_manage tag on primary ENI %s", eniID)
			} else {
				log.Debugf("Marking ENI %s tagged with %s as being unmanaged", eniID, eniNoManageTagKey)
				unmanagedENIlist = append(unmanagedENIlist, eniID)
			}
		}
	}
	c.awsClient.SetUnmanagedENIs(unmanagedENIlist)
}

// ReconcileCooldownCache keep track of recently freed IPs to avoid reading stale EC2 metadata
type ReconcileCooldownCache struct {
	sync.RWMutex
	cache map[string]time.Time
}

// Add sets a timestamp for the list of IPs added that says how long they are not to be put back in the data store.
func (r *ReconcileCooldownCache) Add(ips []string) {
	r.Lock()
	defer r.Unlock()
	expiry := time.Now().Add(ipReconcileCooldown)
	for _, ip := range ips {
		r.cache[ip] = expiry
	}
}

// Remove removes an IP from the cooldown cache.
func (r *ReconcileCooldownCache) Remove(ip string) {
	r.Lock()
	defer r.Unlock()
	log.Debugf("Removing %s from cooldown cache.", ip)
	delete(r.cache, ip)
}

// RecentlyFreed checks if this IP was recently freed.
func (r *ReconcileCooldownCache) RecentlyFreed(ip string) (found, recentlyFreed bool) {
	r.Lock()
	defer r.Unlock()
	now := time.Now()
	if expiry, ok := r.cache[ip]; ok {
		log.Debugf("Checking if IP %s has been recently freed. Cooldown expires at: %s. (Cooldown: %v)", ip, expiry, now.Sub(expiry) < 0)
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

// New retrieves IP address usage information from Instance MetaData service and Kubelet
// then initializes IP address pool data store
func New(rawK8SClient client.Client, cachedK8SClient client.Client) (*IPAMContext, error) {
	prometheusRegister()
	c := &IPAMContext{}

	c.rawK8SClient = rawK8SClient
	c.cachedK8SClient = cachedK8SClient
	c.networkClient = networkutils.New()
	c.useCustomNetworking = UseCustomNetworkCfg()

	client, err := awsutils.New(c.useCustomNetworking)
	if err != nil {
		return nil, errors.Wrap(err, "ipamd: can not initialize with AWS SDK interface")
	}
	c.awsClient = client

	c.primaryIP = make(map[string]string)
	c.reconcileCooldownCache.cache = make(map[string]time.Time)
	c.warmENITarget = getWarmENITarget()
	c.warmIPTarget = getWarmIPTarget()
	c.minimumIPTarget = getMinimumIPTarget()

	c.disableENIProvisioning = disablingENIProvisioning()
	c.enablePodENI = enablePodENI()
	c.myNodeName = os.Getenv("MY_NODE_NAME")
	checkpointer := datastore.NewJSONFile(dsBackingStorePath())
	c.dataStore = datastore.NewDataStore(log, checkpointer)

	err = c.nodeInit()
	if err != nil {
		return nil, err
	}

	mac := c.awsClient.GetPrimaryENImac()
	// retrieve security groups
	err = c.awsClient.RefreshSGIDs(mac)
	if err != nil {
		return nil, err
	}

	// Refresh security groups and VPC CIDR blocks in the background
	// Ignoring errors since we will retry in 30s
	go wait.Forever(func() { _ = c.awsClient.RefreshSGIDs(mac) }, 30*time.Second)
	return c, nil
}

func (c *IPAMContext) nodeInit() error {
	ipamdActionsInprogress.WithLabelValues("nodeInit").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("nodeInit").Sub(float64(1))
	var err error
	ctx := context.TODO()

	log.Debugf("Start node init")

	nodeMaxENI, err := c.getMaxENI()
	if err != nil {
		log.Error("Failed to get ENI limit")
		return err
	}
	c.maxENI = nodeMaxENI
	c.maxIPsPerENI, err = c.awsClient.GetENIIPv4Limit()
	if err != nil {
		return err
	}

	vpcCIDRs, err := c.awsClient.GetVPCIPv4CIDRs()
	if err != nil {
		return err
	}
	primaryIP := c.awsClient.GetLocalIPv4()
	err = c.networkClient.SetupHostNetwork(vpcCIDRs, c.awsClient.GetPrimaryENImac(), &primaryIP, c.enablePodENI)
	if err != nil {
		return errors.Wrap(err, "ipamd init: failed to set up host network")
	}

	metadataResult, err := c.awsClient.DescribeAllENIs()
	if err != nil {
		return errors.New("ipamd init: failed to retrieve attached ENIs info")
	}
	log.Debugf("DescribeAllENIs success: ENIs: %d, tagged: %d", len(metadataResult.ENIMetadata), len(metadataResult.TagMap))
	c.awsClient.SetCNIUnmanagedENIs(metadataResult.MultiCardENIIDs)
	c.setUnmanagedENIs(metadataResult.TagMap)
	enis := c.filterUnmanagedENIs(metadataResult.ENIMetadata)

	for _, eni := range enis {
		log.Debugf("Discovered ENI %s, trying to set it up", eni.ENIID)
		// Retry ENI sync
		if c.awsClient.IsCNIUnmanagedENI(eni.ENIID) {
			log.Infof("Skipping ENI %s since it is not on network card 0", eni.ENIID)
			continue
		}
		retry := 0
		for {
			retry++

			if err = c.setupENI(eni.ENIID, eni, eni.ENIID == metadataResult.TrunkENI, metadataResult.EFAENIs[eni.ENIID]); err == nil {
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

	if err := c.dataStore.ReadBackingStore(); err != nil {
		return err
	}

	if err = c.configureIPRulesForPods(); err != nil {
		return err
	}
	// Spawning updateCIDRsRulesOnChange go-routine
	go wait.Forever(func() {
		vpcCIDRs = c.updateCIDRsRulesOnChange(vpcCIDRs)
	}, 30*time.Second)

	eniConfigName, err := eniconfig.GetNodeSpecificENIConfigName(ctx, c.cachedK8SClient)
	if err == nil && c.useCustomNetworking && eniConfigName != "default" {
		// Signal to VPC Resource Controller that the node is using custom networking
		err := c.SetNodeLabel(ctx, vpcENIConfigLabel, eniConfigName)
		if err != nil {
			log.Errorf("Failed to set eniConfig node label", err)
			podENIErrInc("nodeInit")
			return err
		}
	} else {
		// Remove the custom networking label
		err := c.SetNodeLabel(ctx, vpcENIConfigLabel, "")
		if err != nil {
			log.Errorf("Failed to delete eniConfig node label", err)
			podENIErrInc("nodeInit")
			return err
		}
	}

	// If we started on a node with a trunk ENI already attached, add the node label.
	if metadataResult.TrunkENI != "" {
		// Signal to VPC Resource Controller that the node has a trunk already
		err := c.SetNodeLabel(ctx, "vpc.amazonaws.com/has-trunk-attached", "true")
		if err != nil {
			log.Errorf("Failed to set node label", err)
			podENIErrInc("nodeInit")
			// If this fails, we probably can't talk to the API server. Let the pod restart
			return err
		}
	} else {
		// Check if we want to ask for one
		c.askForTrunkENIIfNeeded(ctx)
	}

	// For a new node, attach IPs
	increasedPool, err := c.tryAssignIPs()
	if err == nil && increasedPool {
		c.updateLastNodeIPPoolAction()
	} else if err != nil {
		return err
	}

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
		err = c.networkClient.UpdateHostIptablesRules(newVPCCIDRs, c.awsClient.GetPrimaryENImac(), &primaryIP)
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
	c.askForTrunkENIIfNeeded(ctx)
	if c.nodeIPPoolTooLow() {
		c.increaseIPPool(ctx)
	} else if c.nodeIPPoolTooHigh() {
		c.decreaseIPPool(decreaseIPPoolInterval)
	}

	if c.shouldRemoveExtraENIs() {
		c.tryFreeENI()
	}
}

// decreaseIPPool runs every `interval` and attempts to return unused ENIs and IPs
func (c *IPAMContext) decreaseIPPool(interval time.Duration) {
	ipamdActionsInprogress.WithLabelValues("decreaseIPPool").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("decreaseIPPool").Sub(float64(1))

	now := time.Now()
	timeSinceLast := now.Sub(c.lastDecreaseIPPool)
	if timeSinceLast <= interval {
		log.Debugf("Skipping decrease IP pool because time since last %v <= %v", timeSinceLast, interval)
		return
	}

	log.Debugf("Starting to decrease IP pool")

	c.tryUnassignIPsFromAll()

	c.lastDecreaseIPPool = now
	c.lastNodeIPPoolAction = now
	total, used := c.dataStore.GetStats()
	log.Debugf("Successfully decreased IP pool")
	logPoolStats(total, used, c.maxIPsPerENI)
}

// tryFreeENI always tries to free one ENI
func (c *IPAMContext) tryFreeENI() {
	if c.isTerminating() {
		log.Debug("AWS CNI is terminating, not detaching any ENIs")
		return
	}

	eni := c.dataStore.RemoveUnusedENIFromStore(c.warmIPTarget, c.minimumIPTarget)
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

// tryUnassignIPsFromAll determines if there are IPs to free when we have extra IPs beyond the target and warmIPTargetDefined
// is enabled, deallocate extra IP addresses
func (c *IPAMContext) tryUnassignIPsFromAll() {
	if _, over, warmIPTargetDefined := c.ipTargetState(); warmIPTargetDefined && over > 0 {
		eniInfos := c.dataStore.GetENIInfos()
		for eniID := range eniInfos.ENIs {
			ips, err := c.findFreeableIPs(eniID)
			if err != nil {
				log.Errorf("Error finding unassigned IPs: %s", err)
				return
			}

			if len(ips) == 0 {
				continue
			}

			// Delete IPs from datastore
			var deletedIPs []string
			for _, toDelete := range ips {
				// Don't force the delete, since a freeable IP might have been assigned to a pod
				// before we get around to deleting it.
				err := c.dataStore.DelIPv4AddressFromStore(eniID, toDelete, false /* force */)
				if err != nil {
					log.Warnf("Failed to delete IP %s on ENI %s from datastore: %s", toDelete, eniID, err)
					ipamdErrInc("decreaseIPPool")
					continue
				} else {
					deletedIPs = append(deletedIPs, toDelete)
				}
			}

			// Deallocate IPs from the instance if they aren't used by pods.
			if err := c.awsClient.DeallocIPAddresses(eniID, deletedIPs); err != nil {
				log.Warnf("Failed to decrease IP pool by removing IPs %v from ENI %s: %s", deletedIPs, eniID, err)
			} else {
				log.Debugf("Successfully decreased IP pool by removing IPs %v from ENI %s", deletedIPs, eniID)
			}

			// Track the last time we unassigned IPs from an ENI. We won't reconcile any IPs in this cache
			// for at least ipReconcileCooldown
			c.reconcileCooldownCache.Add(deletedIPs)
		}
	}
}

// findFreeableIPs finds and returns IPs that are not assigned to Pods but are attached
// to ENIs on the node.
func (c *IPAMContext) findFreeableIPs(eni string) ([]string, error) {
	freeableIPs := c.dataStore.FreeableIPs(eni)

	// Free the number of IPs `over` the warm IP target, unless `over` is greater than the number of available IPs on
	// this ENI. In that case we should only free the number of available IPs.
	_, over, _ := c.ipTargetState()
	numFreeable := min(over, len(freeableIPs))
	freeableIPs = freeableIPs[:numFreeable]

	return freeableIPs, nil
}

func (c *IPAMContext) increaseIPPool(ctx context.Context) {
	log.Debug("Starting to increase IP pool size")
	ipamdActionsInprogress.WithLabelValues("increaseIPPool").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("increaseIPPool").Sub(float64(1))

	short, _, warmIPTargetDefined := c.ipTargetState()
	if warmIPTargetDefined && short == 0 {
		log.Debugf("Skipping increase IP pool, warm IP target reached")
		return
	}

	if c.isTerminating() {
		log.Debug("AWS CNI is terminating, will not try to attach any new IPs or ENIs right now")
		return
	}

	// Try to add more IPs to existing ENIs first.
	increasedPool, err := c.tryAssignIPs()
	if err != nil {
		log.Errorf(err.Error())
	}
	if increasedPool {
		c.updateLastNodeIPPoolAction()
	} else {
		// Check if we need to make room for the VPC Resource Controller to attach a trunk ENI
		reserveSlotForTrunkENI := 0
		if c.enablePodENI && c.dataStore.GetTrunkENI() == "" {
			reserveSlotForTrunkENI = 1
		}
		// If we did not add an IP, try to add an ENI instead.
		if c.dataStore.GetENIs() < (c.maxENI - c.unmanagedENI - reserveSlotForTrunkENI) {
			if err = c.tryAllocateENI(ctx); err == nil {
				c.updateLastNodeIPPoolAction()
			}
		} else {
			log.Debugf("Skipping ENI allocation as the max ENI limit of %d is already reached (accounting for %d unmanaged ENIs and %d trunk ENIs)",
				c.maxENI, c.unmanagedENI, reserveSlotForTrunkENI)
		}
	}
}

func (c *IPAMContext) updateLastNodeIPPoolAction() {
	c.lastNodeIPPoolAction = time.Now()
	total, used := c.dataStore.GetStats()
	log.Debugf("Successfully increased IP pool, total: %d, used: %d", total, used)
	logPoolStats(total, used, c.maxIPsPerENI)
}

func (c *IPAMContext) tryAllocateENI(ctx context.Context) error {
	var securityGroups []*string
	var subnet string

	if c.useCustomNetworking {
		eniCfg, err := eniconfig.MyENIConfig(ctx, c.cachedK8SClient)

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

	ipsToAllocate := c.maxIPsPerENI
	short, _, warmIPTargetDefined := c.ipTargetState()
	if warmIPTargetDefined {
		ipsToAllocate = short
	}

	err = c.awsClient.AllocIPAddresses(eni, ipsToAllocate)
	if err != nil {
		log.Warnf("Failed to allocate %d IP addresses on an ENI: %v", ipsToAllocate, err)
		// Continue to process the allocated IP addresses
		ipamdErrInc("increaseIPPoolAllocIPAddressesFailed")
	}

	eniMetadata, err := c.awsClient.WaitForENIAndIPsAttached(eni, ipsToAllocate)
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

// For an ENI, try to fill in missing IPs on an existing ENI
func (c *IPAMContext) tryAssignIPs() (increasedPool bool, err error) {
	// If WARM_IP_TARGET is set, only proceed if we are short of target
	short, _, warmIPTargetDefined := c.ipTargetState()
	if warmIPTargetDefined && short == 0 {
		return false, nil
	}

	// Find an ENI where we can add more IPs
	eni := c.dataStore.GetENINeedsIP(c.maxIPsPerENI, c.useCustomNetworking)
	if eni != nil && len(eni.IPv4Addresses) < c.maxIPsPerENI {
		currentNumberOfAllocatedIPs := len(eni.IPv4Addresses)
		// Try to allocate all available IPs for this ENI
		err = c.awsClient.AllocIPAddresses(eni.ID, c.maxIPsPerENI-currentNumberOfAllocatedIPs)
		if err != nil {
			log.Warnf("failed to allocate all available IP addresses on ENI %s, err: %v", eni.ID, err)
			// Try to just get one more IP
			err = c.awsClient.AllocIPAddresses(eni.ID, 1)
			if err != nil {
				ipamdErrInc("increaseIPPoolAllocIPAddressesFailed")
				return false, errors.Wrap(err, fmt.Sprintf("failed to allocate one IP addresses on ENI %s, err: %v", eni.ID, err))
			}
		}
		// This call to EC2 is needed to verify which IPs got attached to this ENI.
		ec2Addrs, err := c.awsClient.GetIPv4sFromEC2(eni.ID)
		if err != nil {
			ipamdErrInc("increaseIPPoolGetENIaddressesFailed")
			return true, errors.Wrap(err, "failed to get ENI IP addresses during IP allocation")
		}
		c.addENIaddressesToDataStore(ec2Addrs, eni.ID)
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

	c.addENIaddressesToDataStore(eniMetadata.IPv4Addresses, eni)
	return nil
}

// return primary ip address on the interface
func (c *IPAMContext) addENIaddressesToDataStore(ec2Addrs []*ec2.NetworkInterfacePrivateIpAddress, eni string) {
	for _, ec2Addr := range ec2Addrs {
		if aws.BoolValue(ec2Addr.Primary) {
			continue
		}
		err := c.dataStore.AddIPv4AddressToStore(eni, aws.StringValue(ec2Addr.PrivateIpAddress))
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Warnf("Failed to increase IP pool, failed to add IP %s to data store", ec2Addr.PrivateIpAddress)
			// continue to add next address
			ipamdErrInc("addENIaddressesToDataStoreAddENIIPv4AddressFailed")
		}
	}
	total, assigned := c.dataStore.GetStats()
	log.Debugf("IP Address Pool stats: total: %d, assigned: %d", total, assigned)
}

// getMaxENI returns the maximum number of ENIs to attach to this instance. This is calculated as the lesser of
// the limit for the instance type and the value configured via the MAX_ENI environment variable. If the value of
// the environment variable is 0 or less, it will be ignored and the maximum for the instance is returned.
func (c *IPAMContext) getMaxENI() (int, error) {
	instanceMaxENI, err := c.awsClient.GetENILimit()
	if err != nil {
		return 0, err
	}
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

func logPoolStats(total, used, maxAddrsPerENI int) {
	log.Debugf("IP pool stats: total = %d, used = %d, c.maxIPsPerENI = %d", total, used, maxAddrsPerENI)
}

func (c *IPAMContext) askForTrunkENIIfNeeded(ctx context.Context) {
	if c.enablePodENI && c.dataStore.GetTrunkENI() == "" {
		// Check that there is room for a trunk ENI to be attached:
		if c.dataStore.GetENIs() >= (c.maxENI - c.unmanagedENI) {
			log.Debug("No slot available for a trunk ENI to be attached. Not labeling the node")
			return
		}
		// We need to signal that VPC Resource Controller needs to attach a trunk ENI
		err := c.SetNodeLabel(ctx, "vpc.amazonaws.com/has-trunk-attached", "false")
		if err != nil {
			podENIErrInc("askForTrunkENIIfNeeded")
			log.Errorf("Failed to set node label", err)
		}
	}
}

// nodeIPPoolTooLow returns true if IP pool is below low threshold
func (c *IPAMContext) nodeIPPoolTooLow() bool {
	short, _, warmIPTargetDefined := c.ipTargetState()
	if warmIPTargetDefined {
		return short > 0
	}

	total, used := c.dataStore.GetStats()

	available := total - used
	poolTooLow := available < c.maxIPsPerENI*c.warmENITarget || (c.warmENITarget == 0 && available == 0)
	if poolTooLow {
		logPoolStats(total, used, c.maxIPsPerENI)
		log.Debugf("IP pool is too low: available (%d) < ENI target (%d) * addrsPerENI (%d)", available, c.warmENITarget, c.maxIPsPerENI)
	}
	return poolTooLow
}

// nodeIPPoolTooHigh returns true if IP pool is above high threshold
func (c *IPAMContext) nodeIPPoolTooHigh() bool {
	_, over, warmIPTargetDefined := c.ipTargetState()
	if warmIPTargetDefined {
		return over > 0
	}

	// We only ever report the pool being too high if WARM_IP_TARGET is set
	return false
}

// shouldRemoveExtraENIs returns true if we should attempt to find an ENI to free. When WARM_IP_TARGET is set, we
// always check and do verification in getDeletableENI()
func (c *IPAMContext) shouldRemoveExtraENIs() bool {
	_, _, warmIPTargetDefined := c.ipTargetState()
	if warmIPTargetDefined {
		return true
	}

	total, used := c.dataStore.GetStats()
	available := total - used
	// We need the +1 to make sure we are not going below the WARM_ENI_TARGET.
	shouldRemoveExtra := available >= (c.warmENITarget+1)*c.maxIPsPerENI
	if shouldRemoveExtra {
		logPoolStats(total, used, c.maxIPsPerENI)
		log.Debugf("It might be possible to remove extra ENIs because available (%d) >= (ENI target (%d) + 1) * addrsPerENI (%d): ", available, c.warmENITarget, c.maxIPsPerENI)
	}
	return shouldRemoveExtra
}

func ipamdErrInc(fn string) {
	ipamdErr.With(prometheus.Labels{"fn": fn}).Inc()
}

func podENIErrInc(fn string) {
	podENIErr.With(prometheus.Labels{"fn": fn}).Inc()
}

// nodeIPPoolReconcile reconcile ENI and IP info from metadata service and IP addresses in datastore
func (c *IPAMContext) nodeIPPoolReconcile(ctx context.Context, interval time.Duration) {
	curTime := time.Now()
	timeSinceLast := curTime.Sub(c.lastNodeIPPoolAction)
	if timeSinceLast <= interval {
		return
	}

	ipamdActionsInprogress.WithLabelValues("nodeIPPoolReconcile").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("nodeIPPoolReconcile").Sub(float64(1))

	log.Debugf("Reconciling ENI/IP pool info because time since last %v <= %v", timeSinceLast, interval)
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
	if needToUpdateTags {
		log.Debugf("A new ENI added but not by ipamd, updating tags by calling EC2")
		metadataResult, err := c.awsClient.DescribeAllENIs()
		if err != nil {
			log.Warnf("Failed to call EC2 to describe ENIs, aborting reconcile: %v", err)
			return
		}

		if c.enablePodENI && metadataResult.TrunkENI != "" {
			// Label the node that we have a trunk
			err = c.SetNodeLabel(ctx, "vpc.amazonaws.com/has-trunk-attached", "true")
			if err != nil {
				podENIErrInc("askForTrunkENIIfNeeded")
				log.Errorf("Failed to set node label for trunk. Aborting reconcile", err)
				return
			}
		}
		// Update trunk ENI
		trunkENI = metadataResult.TrunkENI
		// Just copy values of the EFA set
		efaENIs = metadataResult.EFAENIs
		c.setUnmanagedENIs(metadataResult.TagMap)
		c.awsClient.SetCNIUnmanagedENIs(metadataResult.MultiCardENIIDs)
		attachedENIs = c.filterUnmanagedENIs(metadataResult.ENIMetadata)
	}

	// Mark phase
	for _, attachedENI := range attachedENIs {
		eniIPPool, err := c.dataStore.GetENIIPs(attachedENI.ENIID)
		if err == nil {
			// If the attached ENI is in the data store
			log.Debugf("Reconcile existing ENI %s IP pool", attachedENI.ENIID)
			// Reconcile IP pool
			c.eniIPPoolReconcile(eniIPPool, attachedENI, attachedENI.ENIID)
			// Mark action, remove this ENI from currentENIs map
			delete(currentENIs, attachedENI.ENIID)
			continue
		}

		// Add new ENI
		log.Debugf("Reconcile and add a new ENI %s", attachedENI)
		err = c.setupENI(attachedENI.ENIID, attachedENI, attachedENI.ENIID == trunkENI, efaENIs[attachedENI.ENIID])
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
	log.Debug("Successfully Reconciled ENI/IP pool")
	total, assigned := c.dataStore.GetStats()
	log.Debugf("IP Address Pool stats: total: %d, assigned: %d", total, assigned)
	c.lastNodeIPPoolAction = curTime
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
		err := c.dataStore.DelIPv4AddressFromStore(eni, existingIP, true /* force */)
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
			log.Debugf("Reconcile and skip primary IP %s on ENI %s", strPrivateIPv4, eni)
			continue
		}

		// Check if this IP was recently freed
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

		// Try to add the IP
		err := c.dataStore.AddIPv4AddressToStore(eni, strPrivateIPv4)
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Errorf("Failed to reconcile IP %s on ENI %s", strPrivateIPv4, eni)
			ipamdErrInc("ipReconcileAdd")
			// Continue to check the other IPs instead of bailout due to one wrong IP
			continue

		}
		// Mark action
		seenIPs[strPrivateIPv4] = true
		reconcileCnt.With(prometheus.Labels{"fn": "eniIPPoolReconcileAdd"}).Inc()
	}
	return seenIPs
}

// UseCustomNetworkCfg returns whether Pods needs to use pod specific configuration or not.
func UseCustomNetworkCfg() bool {
	if strValue := os.Getenv(envCustomNetworkCfg); strValue != "" {
		parsedValue, err := strconv.ParseBool(strValue)
		if err == nil {
			return parsedValue
		}
		log.Warnf("Failed to parse %s; using default: false, err: %v", envCustomNetworkCfg, err)
	}
	return false
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

func disablingENIProvisioning() bool {
	return getEnvBoolWithDefault(envDisableENIProvisioning, false)
}

func enablePodENI() bool {
	return getEnvBoolWithDefault(envEnablePodENI, false)
}

// filterUnmanagedENIs filters out ENIs marked with the "node.k8s.amazonaws.com/no_manage" tag
func (c *IPAMContext) filterUnmanagedENIs(enis []awsutils.ENIMetadata) []awsutils.ENIMetadata {
	numFiltered := 0
	ret := make([]awsutils.ENIMetadata, 0, len(enis))
	for _, eni := range enis {
		// If we have unmanaged ENIs, filter them out
		if c.awsClient.IsUnmanagedENI(eni.ENIID) {
			log.Debugf("Skipping ENI %s: tagged with %s", eni.ENIID, eniNoManageTagKey)
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

// ipTargetState determines the number of IPs `short` or `over` our WARM_IP_TARGET,
// accounting for the MINIMUM_IP_TARGET
func (c *IPAMContext) ipTargetState() (short int, over int, enabled bool) {
	if c.warmIPTarget == noWarmIPTarget && c.minimumIPTarget == noMinimumIPTarget {
		// there is no WARM_IP_TARGET defined and no MINIMUM_IP_TARGET, fallback to use all IP addresses on ENI
		return 0, 0, false
	}

	total, assigned := c.dataStore.GetStats()
	available := total - assigned

	// short is greater than 0 when we have fewer available IPs than the warm IP target
	short = max(c.warmIPTarget-available, 0)

	// short is greater than the warm IP target alone when we have fewer total IPs than the minimum target
	short = max(short, c.minimumIPTarget-total)

	// over is the number of available IPs we have beyond the warm IP target
	over = max(available-c.warmIPTarget, 0)

	// over is less than the warm IP target alone if it would imply reducing total IPs below the minimum target
	over = max(min(over, total-c.minimumIPTarget), 0)

	log.Debugf("Current warm IP stats: target: %d, total: %d, assigned: %d, available: %d, short: %d, over %d", c.warmIPTarget, total, assigned, available, short, over)
	return short, over, true
}

// setTerminating atomically sets the terminating flag.
func (c *IPAMContext) setTerminating() {
	atomic.StoreInt32(&c.terminating, 1)
}

func (c *IPAMContext) isTerminating() bool {
	return atomic.LoadInt32(&c.terminating) > 0
}

// GetConfigForDebug returns the active values of the configuration env vars (for debugging purposes).
func GetConfigForDebug() map[string]interface{} {
	return map[string]interface{}{
		envWarmIPTarget:     getWarmIPTarget(),
		envWarmENITarget:    getWarmENITarget(),
		envCustomNetworkCfg: UseCustomNetworkCfg(),
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

// SetNodeLabel sets or deletes a node label
func (c *IPAMContext) SetNodeLabel(ctx context.Context, key, value string) error {
	var node corev1.Node
	// Find my node
	err := c.cachedK8SClient.Get(ctx, types.NamespacedName{Name: c.myNodeName}, &node)
	log.Debugf("Node found %q - labels - %q", node.Name, len(node.Labels))

	if err != nil {
		log.Errorf("Failed to get node: %v", err)
		return err
	}

	if labelValue, ok := node.Labels[key]; ok && labelValue == value {
		log.Debugf("Node label %q is already %q", key, labelValue)
		return nil
	}

	// Make deep copy for modification
	updateNode := node.DeepCopy()

	// Set node label
	if value != "" {
		updateNode.Labels[key] = value
	} else {
		// Empty value, delete the label
		log.Debugf("Deleting label %q", key)
		delete(updateNode.Labels, key)
	}

	// Update node status to advertise the resource.
	err = c.cachedK8SClient.Update(ctx, updateNode)
	if err != nil {
		log.Errorf("Failed to update node %s with label %q: %q, error: %v", c.myNodeName, key, value, err)
	}
	log.Debugf("Updated node %s with label %q: %q", c.myNodeName, key, value)

	return nil
}

// GetPod returns the pod matching the name and namespace
func (c *IPAMContext) GetPod(podName, namespace string) (*corev1.Pod, error) {
	ctx := context.TODO()
	var pod corev1.Pod

	podKey := types.NamespacedName{
		Namespace: namespace,
		Name:      podName,
	}
	err := c.rawK8SClient.Get(ctx, podKey, &pod)
	if err != nil {
		return nil, fmt.Errorf("Error while trying to retrieve Pod Info: %s", err)
	}
	return &pod, nil
}
