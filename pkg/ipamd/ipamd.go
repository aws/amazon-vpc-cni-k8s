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
	"fmt"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go/service/ec2"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/eniconfig"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
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

	// vpcENIConfigLabel is used by the VPC resource controller to pick the right ENI config.
	vpcENIConfigLabel = "vpc.amazonaws.com/eniConfig"

	// These environment variables are used to control whether pods are assigned an IPv4, IPv6, or both (dual-stack) addresses.
	// It is an error to have ASSIGN_IPV4 and ASSIGN_IPV6 both set to false.
	// ASSIGN_IPV6=true requires the VPC subnet to have an IPv6 CIDR assigned.
	envAssignIPv4     = "ASSIGN_IPV4"
	defaultAssignIPv4 = true
	envAssignIPv6     = "ASSIGN_IPV6"
	defaultAssignIPv6 = false
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

// IPAMContext ipv6Contains node level control information
type IPAMContext struct {
	awsClient            awsutils.APIs
	dataStore            *datastore.DataStore
	k8sClient            kubernetes.Interface
	useCustomNetworking  bool
	eniConfig            eniconfig.ENIConfig
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
	assignIPv4             bool
	assignIPv6             bool
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
func New(k8sapiClient kubernetes.Interface, eniConfig *eniconfig.ENIConfigController) (*IPAMContext, error) {
	prometheusRegister()
	c := &IPAMContext{}

	c.k8sClient = k8sapiClient
	c.networkClient = networkutils.New()
	c.eniConfig = eniConfig
	c.useCustomNetworking = UseCustomNetworkCfg()

	client, err := awsutils.New(c.useCustomNetworking)
	if err != nil {
		return nil, errors.Wrap(err, "ipamd: can not initialize with AWS SDK interface")
	}
	c.awsClient = client

	c.assignIPv4 = getAssignIPv4()
	c.assignIPv6 = getAssignIPv6()
	if !c.assignIPv4 && !c.assignIPv6 {
		return nil, errors.New("ASSIGN_IPV4 and ASSIGN_IPV6 cannot both be false")
	}

	c.primaryIP = make(map[string]string)
	c.reconcileCooldownCache.cache = make(map[string]time.Time)
	c.warmENITarget = getWarmENITarget()
	c.warmIPTarget = getWarmIPTarget()
	c.minimumIPTarget = getMinimumIPTarget()

	c.disableENIProvisioning = disablingENIProvisioning()
	c.enablePodENI = enablePodENI()
	c.myNodeName = os.Getenv("MY_NODE_NAME")
	checkpointer := datastore.NewJSONFile(dsBackingStorePath())
	c.dataStore = datastore.NewDataStore(log, checkpointer, c.assignIPv4, c.assignIPv6)

	err = c.nodeInit()
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (c *IPAMContext) nodeInit() error {
	ipamdActionsInprogress.WithLabelValues("nodeInit").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("nodeInit").Sub(float64(1))
	var err error

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

	vpcIPv4CIDRs := c.awsClient.GetVPCIPv4CIDRs()
	vpcIPv6CIDRs := c.awsClient.GetVPCIPv6CIDRs()
	primaryIP := net.ParseIP(c.awsClient.GetLocalIPv4())
	err = c.networkClient.SetupHostNetwork(vpcIPv4CIDRs, vpcIPv6CIDRs, c.awsClient.GetPrimaryENImac(), &primaryIP, c.enablePodENI)
	if err != nil {
		return errors.Wrap(err, "ipamd init: failed to set up host network")
	}

	eniMetadata, tagMap, trunkENI, err := c.awsClient.DescribeAllENIs()
	if err != nil {
		return errors.New("ipamd init: failed to retrieve attached ENIs info")
	}
	log.Debugf("DescribeAllENIs success: ENIs: %d, tagged: %d", len(eniMetadata), len(tagMap))
	c.setUnmanagedENIs(tagMap)
	enis := c.filterUnmanagedENIs(eniMetadata)

	for _, eni := range enis {
		log.Debugf("Discovered ENI %s, trying to set it up", eni.ENIID)
		// Retry ENI sync
		retry := 0
		for {
			retry++
			if err = c.setupENI(eni.ENIID, eni, trunkENI); err == nil {
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

	if err = c.configureIPRulesForPods(vpcIPv4CIDRs, vpcIPv6CIDRs); err != nil {
		return err
	}

	if c.useCustomNetworking && c.eniConfig.Getter().MyENI != "default" {
		// Signal to VPC Resource Controller that the node is using custom networking
		err := c.SetNodeLabel(vpcENIConfigLabel, c.eniConfig.Getter().MyENI)
		if err != nil {
			log.Errorf("Failed to set eniConfig node label", err)
			podENIErrInc("nodeInit")
			return err
		}
	} else {
		// Remove the custom networking label
		err := c.SetNodeLabel(vpcENIConfigLabel, "")
		if err != nil {
			log.Errorf("Failed to delete eniConfig node label", err)
			podENIErrInc("nodeInit")
			return err
		}
	}

	// If we started on a node with a trunk ENI already attached, add the node label.
	if trunkENI != "" {
		// Signal to VPC Resource Controller that the node has a trunk already
		err := c.SetNodeLabel("vpc.amazonaws.com/has-trunk-attached", "true")
		if err != nil {
			log.Errorf("Failed to set node label", err)
			podENIErrInc("nodeInit")
			// If this fails, we probably can't talk to the API server. Let the pod restart
			return err
		}
	} else {
		// Check if we want to ask for one
		c.askForTrunkENIIfNeeded()
	}

	// For a new node, attach IPs
	increasedPool, err := c.tryAssignIPs()
	if err == nil && increasedPool {
		c.updateLastNodeIPPoolAction()
	} else if err != nil {
		return err
	}

	// Spawning updateCIDRsRulesOnChange go-routine
	go wait.Forever(func() { vpcIPv4CIDRs = c.updateCIDRsRulesOnChange(vpcIPv4CIDRs) }, 30*time.Second)
	return nil
}

func (c *IPAMContext) configureIPRulesForPods(vpcIPv4CIDRs []string, vpcIPv6CIDRs []string) error {
	rules, err := c.networkClient.GetRuleList()
	if err != nil {
		log.Errorf("During ipamd init: failed to retrieve IP rule list %v", err)
		return nil
	}

	for _, eniIps := range c.dataStore.GetENIIPs() {
		for _, info := range eniIps {
			if !info.IsAssignedToPod() {
				continue
			}
			// TODO(gus): This should really be done via CNI CHECK calls, rather than in ipam (requires upstream k8s changes).
			// Update ip rules in case there is a change in VPC CIDRs, AWS_VPC_K8S_CNI_EXTERNALSNAT setting
			if info.IPv4 != "" {
				srcIPNet := net.IPNet{IP: net.ParseIP(info.IPv4), Mask: net.IPv4Mask(255, 255, 255, 255)}
				err = c.networkClient.UpdateRuleListBySrc(rules, srcIPNet, vpcIPv4CIDRs, !c.networkClient.UseExternalSNAT())
				if err != nil {
					log.Errorf("UpdateRuleListBySrc in nodeInit() failed for IP %s: %v", info.IPv4, err)
				}
			}
			// IPv6?
			if info.IPv6 != "" {
				srcIPNet := net.IPNet{IP: net.ParseIP(info.IPv6), Mask: net.CIDRMask(128, 128)}
				err = c.networkClient.UpdateRuleListBySrc(rules, srcIPNet, vpcIPv6CIDRs, !c.networkClient.UseExternalSNAT())
				if err != nil {
					log.Errorf("UpdateRuleListBySrc in nodeInit() failed for IP %s: %v", info.IPv6, err)
				}
			}
		}
	}
	return nil
}

func (c *IPAMContext) updateCIDRsRulesOnChange(oldVPCCidrs []string) []string {
	newVPCCIDRs := c.awsClient.GetVPCIPv4CIDRs()

	if len(oldVPCCidrs) != len(newVPCCIDRs) || !reflect.DeepEqual(oldVPCCidrs, newVPCCIDRs) {
		_ = c.configureIPRulesForPods(newVPCCIDRs, nil)
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
	for {
		if !c.disableENIProvisioning {
			time.Sleep(sleepDuration)
			c.updateIPPoolIfRequired()
		}
		time.Sleep(sleepDuration)
		c.nodeIPPoolReconcile(nodeIPPoolReconcileInterval)
	}
}

func (c *IPAMContext) updateIPPoolIfRequired() {
	c.askForTrunkENIIfNeeded()
	if c.nodeIPPoolTooLow() {
		c.increaseIPPool()
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
	if _, over, warmIPTargetDefined := c.ipTargetState(); !warmIPTargetDefined || over <= 0 {
		return
	}
	eniInfos := c.dataStore.GetENIIPs()
	for eniID, eniAddrs := range eniInfos {
		ips := c.findFreeableSubset(eniAddrs)

		if len(ips) == 0 {
			continue
		}

		// Delete IPs from datastore
		var deletedIPv4s []string
		var deletedIPv6s []string
		for _, ip := range ips {
			// Don't force the delete, since a freeable IP might have been assigned to a pod
			// before we get around to deleting it.
			err := c.dataStore.DelAddressFromStore(eniID, ip.IPv4, ip.IPv6, false /* force */)
			if err != nil {
				log.Warnf("Failed to delete IP %s, %s on ENI %s from datastore: %s", ip.IPv4, ip.IPv6, eniID, err)
				ipamdErrInc("decreaseIPPool")
				continue
			}
			if ip.IPv4 != "" {
				deletedIPv4s = append(deletedIPv4s, ip.IPv4)
			}
			if ip.IPv6 != "" {
				deletedIPv6s = append(deletedIPv6s, ip.IPv6)
			}
		}

		// Deallocate IPs from the instance if they aren't used by pods.
		if len(deletedIPv4s) > 0 {
			if err := c.awsClient.DeallocIPv4Addresses(eniID, deletedIPv4s); err != nil {
				log.Warnf("Failed to decrease IP pool by removing IPs %v from ENI %s: %s", deletedIPv4s, eniID, err)
			} else {
				log.Debugf("Successfully decreased IP pool by removing IPs %v from ENI %s", deletedIPv4s, eniID)
			}

			// Track the last time we unassigned IPs from an ENI. We won't reconcile any IPs in this cache
			// for at least ipReconcileCooldown
			c.reconcileCooldownCache.Add(deletedIPv4s)
		}
		if len(deletedIPv6s) > 0 {
			if err := c.awsClient.DeallocIPv6Addresses(eniID, deletedIPv6s); err != nil {
				log.Warnf("Failed to decrease IP pool by removing IPs %v from ENI %s: %s", deletedIPv6s, eniID, err)
			} else {
				log.Debugf("Successfully decreased IP pool by removing IPs %v from ENI %s", deletedIPv6s, eniID)
			}

			// Track the last time we unassigned IPs from an ENI. We won't reconcile any IPs in this cache
			// for at least ipReconcileCooldown
			c.reconcileCooldownCache.Add(deletedIPv6s)
		}
	}
}

// findFreeableSubset finds and returns IPs int the datastore that are not assigned to Pods but are attached to ENIs on the node.
func (c *IPAMContext) findFreeableSubset(allIPs []datastore.PodIPInfo) []datastore.PodIPInfo {
	var freeableIPs []datastore.PodIPInfo
	for _, ip := range allIPs {
		if !ip.IsAssignedToPod() {
			freeableIPs = append(freeableIPs, ip)
		}
	}

	// Free the number of IPs `over` the warm IP target, unless `over` is greater than the number of available IPs on
	// this ENI. In that case we should only free the number of available IPs.
	_, over, warmIPenabled := c.ipTargetState()
	if warmIPenabled {
		numFreeable := min(over, len(freeableIPs))
		freeableIPs = freeableIPs[:numFreeable]
	}
	return freeableIPs
}

func (c *IPAMContext) increaseIPPool() {
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
			if err = c.tryAllocateENI(); err == nil {
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

func (c *IPAMContext) tryAllocateENI() error {
	var securityGroups []*string
	var subnet string

	if c.useCustomNetworking {
		eniCfg, err := c.eniConfig.MyENIConfig()

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

	var ip4s, ip6s []string

	if c.assignIPv4 {
		ip4s, err = c.awsClient.AllocIPv4Addresses(eni, ipsToAllocate)
		if err != nil {
			log.Warnf("Failed to allocate %d IPv4 addresses on an ENI: %v", ipsToAllocate, err)
			// Continue to process the allocated IP addresses
			ipamdErrInc("increaseIPPoolAllocIPAddressesFailed")
		}
		ipsToAllocate = len(ip4s)
	}

	if c.assignIPv6 {
		ip6s, err = c.awsClient.AllocIPv6Addresses(eni, ipsToAllocate)
		if err != nil {
			log.Warnf("Failed to allocate %d IPv6 addresses on an ENI: %v", ipsToAllocate, err)
			// Continue to process the allocated IP addresses
			ipamdErrInc("increaseIPPoolAllocIPAddressesFailed")
		}
	}

	eniMetadata, err := c.awsClient.WaitForENIAndIPsAttached(eni, ipsToAllocate)
	if err != nil {
		ipamdErrInc("increaseIPPoolwaitENIAttachedFailed")
		log.Errorf("Failed to increase pool size: Unable to discover attached ENI from metadata service %v", err)
		return err
	}

	err = c.setupENI(eni, eniMetadata, c.dataStore.GetTrunkENI())
	if err != nil {
		ipamdErrInc("increaseIPPoolsetupENIFailed")
		log.Errorf("Failed to increase pool size: %v", err)
		return err
	}

	// TODO: Move to setupENI?
	for _, ipPair := range stringPairs(c.assignIPv4, ip4s, c.assignIPv6, ip6s) {
		if err := c.dataStore.AddAddressToStore(eni, ipPair.a, ipPair.b); err != nil {
			log.Warnf("Failed to increase IP pool, failed to add IP %s, %s to data store", ipPair.a, ipPair.b)
			return err
		}
	}

	total, assigned := c.dataStore.GetStats()
	log.Debugf("IP Address Pool stats: total: %d, assigned: %d", total, assigned)

	return nil
}

// For an ENI, try to fill in missing IPs on an existing ENI
func (c *IPAMContext) tryAssignIPs() (increasedPool bool, err error) {
	// If WARM_IP_TARGET is set, only proceed if we are short of target
	short, _, warmIPTargetDefined := c.ipTargetState()
	if warmIPTargetDefined && short == 0 {
		return false, nil
	}

	// Find an ENI where we can add more IPs
	eniID, currentNumberOfAllocatedIPs := c.dataStore.GetENINeedsIP(c.maxIPsPerENI, c.useCustomNetworking)
	if eniID == "" {
		return false, nil
	}

	var ec2Addrs4, ec2Addrs6 []string

	// Try to allocate all available IPs for this ENI
	numAllocate := c.maxIPsPerENI - currentNumberOfAllocatedIPs
	if numAllocate <= 0 {
		return false, nil
	}

	if c.assignIPv4 {
		ec2Addrs4, err = c.awsClient.AllocIPv4Addresses(eniID, numAllocate)
		if err != nil {
			log.Warnf("failed to allocate all available IPv4 addresses on ENI %s, err: %v", eniID, err)
			// Try to just get one more IP
			numAllocate = 1
			ec2Addrs4, err = c.awsClient.AllocIPv4Addresses(eniID, numAllocate)
			if err != nil {
				ipamdErrInc("increaseIPPoolAllocIPAddressesFailed")
				return false, errors.Wrap(err, fmt.Sprintf("failed to allocate one IPv4 addresses on ENI %s, err: %v", eniID, err))
			}
		}
		numAllocate = len(ec2Addrs4)
	}

	if c.assignIPv6 {
		ec2Addrs6, err = c.awsClient.AllocIPv6Addresses(eniID, numAllocate)
		if err != nil {
			ipamdErrInc("increaseIPPoolAllocIPAddressesFailed")
			if len(ec2Addrs4) > 0 {
				if err2 := c.awsClient.DeallocIPv4Addresses(eniID, ec2Addrs4); err2 != nil {
					// Urgh. Leak the just-allocated IPv4 addresses. Reconciliation might clean them up eventually, depending on the cause of the dealloc failure.
					log.Warnf("Failed to free new IPv4 addresses during IPv6 error unwind: %v", err2)
				}
			}
			return false, errors.Wrap(err, fmt.Sprintf("failed to allocate %d IPv6 addresses on ENI %s, err: %v", numAllocate, eniID, err))
		}
	}

	for _, ipPair := range stringPairs(c.assignIPv4, ec2Addrs4, c.assignIPv6, ec2Addrs6) {
		err = c.dataStore.AddAddressToStore(eniID, ipPair.a, ipPair.b)
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Warnf("Failed to increase IP pool, failed to add  %s, %s to data store", ipPair.a, ipPair.b)
			// continue to add next address
			ipamdErrInc("addAddressToStore")
		}
	}

	return true, nil
}

// setupENI does following:
// 1) add ENI to datastore
// 2) set up linux ENI related networking stack.
// 3) add all ENI's secondary IP addresses to datastore
func (c *IPAMContext) setupENI(eni string, eniMetadata awsutils.ENIMetadata, trunkENI string) error {
	primaryENI := c.awsClient.GetPrimaryENI()
	// Add the ENI to the datastore
	err := c.dataStore.AddENI(eni, eniMetadata.DeviceNumber, eni == primaryENI, eni == trunkENI)
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

	c.addENIaddressesToDataStore(eniMetadata)
	return nil
}

// Add all IPv4 and IPv6 addresses to the datastore
func (c *IPAMContext) addENIaddressesToDataStore(eni awsutils.ENIMetadata) {
	for _, ipv4 := range eni.IPv4Addresses {
		if aws.BoolValue(ipv4.Primary) {
			log.Debug("TODO: Primary!")
			continue
		}
		err := c.dataStore.AddAddressToStore(eni.ENIID, aws.StringValue(ipv4.PrivateIpAddress), "")
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Warnf("Failed to increase IP pool, failed to add IPv4 %s to data store", ipv4.PrivateIpAddress)
			// continue to add next address
			ipamdErrInc("addENIaddressesToDataStoreAddENIIPv4AddressFailed")
		}
	}
	for _, ipv6 := range eni.IPv6Addresses {
		err := c.dataStore.AddAddressToStore(eni.ENIID, "", ipv6)
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Warnf("Failed to increase IP pool, failed to add IPv6 %s to data store", ipv6)
			// continue to add next address
			ipamdErrInc("addENIaddressesToDataStoreAddENIIPv6AddressFailed")
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

func (c *IPAMContext) askForTrunkENIIfNeeded() {
	if c.enablePodENI && c.dataStore.GetTrunkENI() == "" {
		// Check that there is room for a trunk ENI to be attached:
		if c.dataStore.GetENIs() >= (c.maxENI - c.unmanagedENI) {
			log.Debug("No slot available for a trunk ENI to be attached. Not labeling the node")
			return
		}
		// We need to signal that VPC Resource Controller needs to attach a trunk ENI
		err := c.SetNodeLabel("vpc.amazonaws.com/has-trunk-attached", "false")
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
func (c *IPAMContext) nodeIPPoolReconcile(interval time.Duration) {
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

	// Check if a new ENI was added, if so we need to update the tags.
	needToUpdateTags := false
	for _, attachedENI := range attachedENIs {
		if _, ok := currentENIs[attachedENI.ENIID]; !ok {
			needToUpdateTags = true
			break
		}
	}
	if needToUpdateTags {
		log.Debugf("A new ENI added but not by ipamd, updating tags")
		allENIs, tagMap, trunk, err := c.awsClient.DescribeAllENIs()
		if err != nil {
			log.Warnf("Failed to call EC2 to describe ENIs, aborting reconcile: %v", err)
			return
		}

		if c.enablePodENI && trunk != "" {
			// Label the node that we have a trunk
			err = c.SetNodeLabel("vpc.amazonaws.com/has-trunk-attached", "true")
			if err != nil {
				podENIErrInc("askForTrunkENIIfNeeded")
				log.Errorf("Failed to set node label for trunk. Aborting reconcile", err)
				return
			}
		}
		// Update trunk ENI
		trunkENI = trunk
		c.setUnmanagedENIs(tagMap)
		attachedENIs = c.filterUnmanagedENIs(allENIs)
	}

	// Mark phase
	dsENIIPs := c.dataStore.GetENIIPs()
	for _, attachedENI := range attachedENIs {
		if eniIPs, ok := dsENIIPs[attachedENI.ENIID]; ok {
			// If the attached ENI is in the data store
			log.Debugf("Reconcile existing ENI %s IP pool", attachedENI.ENIID)
			// Reconcile IP pool
			c.eniIPPoolReconcile(eniIPs, attachedENI)
			// Mark action, remove this ENI from currentENIs map
			delete(currentENIs, attachedENI.ENIID)
			continue
		}

		// Add new ENI
		log.Debugf("Reconcile and add a new ENI %s", attachedENI)
		err = c.setupENI(attachedENI.ENIID, attachedENI, trunkENI)
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

func (c *IPAMContext) eniIPPoolReconcile(ipPool []datastore.PodIPInfo, attachedENI awsutils.ENIMetadata) {
	eni := attachedENI
	needEC2Reconcile := true
	// Here we can't trust attachedENI since the IMDS metadata can be stale. We need to check with EC2 API if the
	// data store doesn't match the metadata.
	// +1 is for the primary IP of the ENI that is not added to the ipPool and not available for pods to use.
	if 1+len(ipPool) != len(eni.IPv4Addresses)+len(eni.IPv6Addresses) {
		log.Warnf("Instance metadata does not match data store! ipPool: %v, IPv4: %v, IPv6: %v", ipPool, eni.IPv4Addresses, eni.IPv6Addresses)
		log.Debugf("We need to check the ENI status by calling the EC2 control plane.")
		// Call EC2 to verify IPs on this ENI
		ipv4Addresses, ipv6Addresses, err := c.awsClient.GetIPsFromEC2(eni.ENIID)
		if err != nil {
			log.Errorf("Failed to fetch ENI IP addresses! Aborting reconcile of ENI %s", eni.ENIID)
			return
		}
		eni.IPv4Addresses = ipv4Addresses
		eni.IPv6Addresses = ipv6Addresses
		needEC2Reconcile = false
	}

	// Add all known attached IPs to the datastore
	seenIPs := c.verifyAndAddIPsToDatastore(ipPool, eni, needEC2Reconcile)

	// Sweep phase, delete remaining IPs since they should not remain in the datastore
	for _, ipInfo := range ipPool {
		ipv4 := ipInfo.IPv4
		ipv6 := ipInfo.IPv6
		// Skip the ones we have seen
		if (ipv4 != "" && seenIPs[ipv4]) || ipv6 != "" && seenIPs[ipv6] {
			continue
		}

		log.Debugf("Reconcile and delete IP %s, %s on ENI %s", ipv4, ipv6, eni.ENIID)
		// Force the delete, since we have verified with EC2 that these secondary IPs are no longer assigned to this ENI
		err := c.dataStore.DelAddressFromStore(eni.ENIID, ipv4, ipv6, true /* force */)
		if err != nil {
			log.Errorf("Failed to reconcile and delete IP %s, %s on ENI %s", ipv4, ipv6, eni.ENIID, err)
			ipamdErrInc("ipReconcileDel")
			// continue instead of bailout due to one ip
			continue
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniIPPoolReconcileDel"}).Inc()
	}
}

// verifyAndAddIPsToDatastore updates the datastore with the known secondary IPs. IPs who are out of cooldown gets added
// back to the datastore after being verified against EC2.
func (c *IPAMContext) verifyAndAddIPsToDatastore(ipPool []datastore.PodIPInfo, eni awsutils.ENIMetadata, needEC2Reconcile bool) map[string]bool {
	ec2VerifiedMetadata := awsutils.ENIMetadata{}
	if !needEC2Reconcile {
		ec2VerifiedMetadata = eni
	}
	seenIPs := make(map[string]bool)
	// Go through the datastore
	for _, ipInfo := range ipPool {
		ipv4 := ipInfo.IPv4
		ipv6 := ipInfo.IPv6
		// TODO: Just checking IPv4
		if ipv4 == c.primaryIP[eni.ENIID] {
			log.Debugf("Reconcile and skip primary IP %s on ENI %s", ipv4, eni.ENIID)
			continue
		}

		// Check if this IP was recently freed
		found4, recentlyFreedIPv4 := c.reconcileCooldownCache.RecentlyFreed(ipv4)
		found6, recentlyFreedIPv6 := c.reconcileCooldownCache.RecentlyFreed(ipv6)
		if found4 || found6 {
			// In case one of the IPs are in the cooldown cache
			if recentlyFreedIPv4 || recentlyFreedIPv6 {
				log.Debugf("Reconcile skipping IPs %s, %s on ENI %s because it was recently unassigned from the ENI.", ipv4, ipv6, eni.ENIID)
				continue
			}

			log.Debugf("This IP was recently freed, but is now out of cooldown. We need to verify with EC2 control plane.")
			// Only call EC2 once for this ENI
			if ec2VerifiedMetadata.ENIID == "" {
				var err error
				// Call EC2 to verify IPs on this ENI
				ipv4Addresses, ipv6Addresses, err := c.awsClient.GetIPsFromEC2(eni.ENIID)
				if err != nil {
					log.Errorf("Failed to fetch ENI IP addresses from EC2! %v", err)
					// Do not delete this IP from the datastore or cooldown until we have confirmed with EC2
					seenIPs[ipv4] = true
					seenIPs[ipv6] = true
					continue
				}
				ec2VerifiedMetadata = eni
				// Set the updated IP addresses we just fetched
				ec2VerifiedMetadata.IPv4Addresses = ipv4Addresses
				ec2VerifiedMetadata.IPv6Addresses = ipv6Addresses
			}
			// Verify the IPv4 really belongs to this ENI
			isIPv4ReallyAttachedToENI := false
			for _, ec2Addr := range ec2VerifiedMetadata.IPv4Addresses {
				if ipv4 == aws.StringValue(ec2Addr.PrivateIpAddress) {
					isIPv4ReallyAttachedToENI = true
					log.Debugf("Verified that IP %s is attached to ENI %s", ipv4, eni.ENIID)
					break
				}
			}
			if !isIPv4ReallyAttachedToENI {
				log.Warnf("Not putting IP %s on ENI %s back in the datastore because it no longer belongs to this ENI", ipv4, eni.ENIID)
				continue
			}
			// Verify the IPv6 really belongs to this ENI
			isIPv6ReallyAttachedToENI := false
			for _, ec2IPv6Addr := range ec2VerifiedMetadata.IPv6Addresses {
				if ipv6 == ec2IPv6Addr {
					isIPv6ReallyAttachedToENI = true
					log.Debugf("Verified that IP %s is attached to ENI %s", ipv6, eni.ENIID)
					break
				}
			}
			if !isIPv6ReallyAttachedToENI {
				log.Warnf("Not putting IP %s on ENI %s back in the datastore because it no longer belongs to this ENI", ipv6, eni.ENIID)
				continue
			}

			// The IP can be removed from the cooldown cache
			// TODO: Here we could check if the IP is still used by a pod stuck in Terminating state. (Issue #1091)
			c.reconcileCooldownCache.Remove(ipv4)
			c.reconcileCooldownCache.Remove(ipv6)
		}

		// Try to add the IP
		err := c.dataStore.AddAddressToStore(eni.ENIID, ipv4, ipv6)
		if err != nil && err.Error() != datastore.IPAlreadyInStoreError {
			log.Errorf("Failed to reconcile IP %s, %s on ENI %s", ipv4, ipv6, eni.ENIID)
			ipamdErrInc("ipReconcileAdd")
			// Continue to check the other IPs instead of bailout due to one wrong IP
			continue

		}
		// Mark action
		if needEC2Reconcile || ipv4Contains(ec2VerifiedMetadata.IPv4Addresses, ipv4) {
			seenIPs[ipv4] = true
		}
		if needEC2Reconcile || ipv6Contains(ec2VerifiedMetadata.IPv6Addresses, ipv6) {
			seenIPs[ipv6] = true
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniIPPoolReconcileAdd"}).Inc()
	}
	return seenIPs
}

func ipv4Contains(ipv4s []*ec2.NetworkInterfacePrivateIpAddress, ipv4 string) bool {
	for _, ip := range ipv4s {
		if aws.StringValue(ip.PrivateIpAddress) == ipv4 {
			return true
		}
	}
	return false
}

func ipv6Contains(ipv6s []string, ipv6 string) bool {
	for _, ip := range ipv6s {
		if ip == ipv6 {
			return true
		}
	}
	return false
}

// UseCustomNetworkCfg returns whether Pods needs to use pod specific configuration or not.
func UseCustomNetworkCfg() bool {
	return utils.GetBoolEnvVar(log, envCustomNetworkCfg, false)
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
	return utils.GetBoolEnvVar(log, envDisableENIProvisioning, false)
}

func enablePodENI() bool {
	return utils.GetBoolEnvVar(log, envEnablePodENI, false)
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
		}
		ret = append(ret, eni)
	}
	c.unmanagedENI = numFiltered
	c.updateIPStats(numFiltered)
	return ret
}

// ipTargetState determines the number of IPs `short` or `over` our WARM_IP_TARGET, accounting for the MINIMUM_IP_TARGET
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
	return short, over, c.warmIPTarget != noWarmIPTarget
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
func (c *IPAMContext) SetNodeLabel(key, value string) error {
	// Find my node
	node, err := c.k8sClient.CoreV1().Nodes().Get(c.myNodeName, metav1.GetOptions{})
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
	_, err = c.k8sClient.CoreV1().Nodes().Update(updateNode)
	if err != nil {
		log.Errorf("Failed to update node %s with label %q: %q, error: %v", c.myNodeName, key, value, err)
	}
	log.Infof("Updated node %s with label %q: %q", c.myNodeName, key, value)
	return nil
}

// GetPod returns the pod matching the name and namespace
func (c *IPAMContext) GetPod(podName, namespace string) (*v1.Pod, error) {
	return c.k8sClient.CoreV1().Pods(namespace).Get(podName, metav1.GetOptions{})
}

func getAssignIPv4() bool {
	return utils.GetBoolEnvVar(log, envAssignIPv4, defaultAssignIPv4)
}

func getAssignIPv6() bool {
	return utils.GetBoolEnvVar(log, envAssignIPv6, defaultAssignIPv6)
}

type stringPair struct{ a, b string }

func zip(a, b []string) []stringPair {
	minLen := min(len(a), len(b))
	ret := make([]stringPair, minLen)
	for i := 0; i < minLen; i++ {
		ret[i] = stringPair{a: a[i], b: b[i]}
	}
	return ret
}

func stringPairs(useA bool, a []string, useB bool, b []string) []stringPair {
	switch {
	case !useA && !useB:
		return nil
	case useA && !useB:
		return zip(a, make([]string, len(a)))
	case !useA && useB:
		return zip(make([]string, len(b)), b)
	default: /* useA && useB */
		return zip(a, b)
	}
}
