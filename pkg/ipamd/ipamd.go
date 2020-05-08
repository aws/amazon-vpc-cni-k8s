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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/cri"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/eniconfig"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
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
	maxK8SRetries               = 5
	retryK8SInterval            = 3 * time.Second

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
	noDisableENIProvisioning  = false
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
	prometheusRegistered = false
)

// IPAMContext contains node level control information
type IPAMContext struct {
	awsClient            awsutils.APIs
	dataStore            *datastore.DataStore
	k8sClient            k8sapi.K8SAPIs
	useCustomNetworking  bool
	eniConfig            eniconfig.ENIConfig
	criClient            cri.APIs
	networkClient        networkutils.NetworkAPIs
	maxIPsPerENI         int
	maxENI               int
	unmanagedENIs        UnmanagedENISet // a set of ENIs tagged with "node.k8s.amazonaws.com/no_manage"
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
}

// UnmanagedENISet keeps a set of ENI IDs for ENIs tagged with "node.k8s.amazonaws.com/no_manage"
type UnmanagedENISet struct {
	sync.RWMutex
	data map[string]bool
}

func (u *UnmanagedENISet) isUnmanaged(eniID string) bool {
	val, ok := u.data[eniID]
	return ok && val == true
}

func (u *UnmanagedENISet) reset() {
	u.Lock()
	defer u.Unlock()
	u.data = make(map[string]bool)
}

func (u *UnmanagedENISet) add(eniID string) {
	u.Lock()
	defer u.Unlock()
	if len(u.data) == 0 {
		u.data = make(map[string]bool)
	}
	u.data[eniID] = true
}

// setUnmanagedENIs will rebuild the set of ENI IDs for ENIs tagged as "no_manage"
func (c *IPAMContext) setUnmanagedENIs(tagMap map[string]awsutils.TagMap) {
	c.unmanagedENIs.reset()
	if len(tagMap) == 0 {
		return
	}
	for eniID, tags := range tagMap {
		if tags[eniNoManageTagKey] == "true" {
			if eniID == c.awsClient.GetPrimaryENI() {
				log.Debugf("Ignoring no_manage tag on primary ENI %s", eniID)
			} else {
				log.Debugf("Marking ENI %s tagged with %s as being unmanaged", eniID, eniNoManageTagKey)
				c.unmanagedENIs.add(eniID)
			}
		}
	}
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
		prometheusRegistered = true
	}
}

// New retrieves IP address usage information from Instance MetaData service and Kubelet
// then initializes IP address pool data store
func New(k8sapiClient k8sapi.K8SAPIs, eniConfig *eniconfig.ENIConfigController) (*IPAMContext, error) {
	prometheusRegister()
	c := &IPAMContext{}

	c.k8sClient = k8sapiClient
	c.networkClient = networkutils.New()
	c.criClient = cri.New()
	c.eniConfig = eniConfig

	client, err := awsutils.New()
	if err != nil {
		return nil, errors.Wrap(err, "ipamd: can not initialize with AWS SDK interface")
	}
	c.awsClient = client

	c.primaryIP = make(map[string]string)
	c.reconcileCooldownCache.cache = make(map[string]time.Time)
	c.warmENITarget = getWarmENITarget()
	c.warmIPTarget = getWarmIPTarget()
	c.minimumIPTarget = getMinimumIPTarget()
	c.useCustomNetworking = UseCustomNetworkCfg()
	c.disableENIProvisioning = disablingENIProvisioning()

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

	eniMetadata, tagMap, err := c.awsClient.DescribeAllENIs()
	if err != nil {
		return errors.New("ipamd init: failed to retrieve attached ENIs info")
	} else {
		log.Debugf("DescribeAllENIs success: ENIs: %d, tagged: %d", len(eniMetadata), len(tagMap))
	}
	c.setUnmanagedENIs(tagMap)
	enis := c.filterUnmanagedENIs(eniMetadata)
	nodeMaxENI, err := c.getMaxENI()
	if err != nil {
		log.Error("Failed to get ENI limit")
		return err
	}
	c.maxENI = nodeMaxENI
	c.maxIPsPerENI, err = c.awsClient.GetENIipLimit()
	if err != nil {
		return err
	}

	var pbVPCcidrs []string
	vpcCIDRs := c.awsClient.GetVPCIPv4CIDRs()

	for _, cidr := range vpcCIDRs {
		pbVPCcidrs = append(pbVPCcidrs, *cidr)
	}

	_, vpcCIDR, err := net.ParseCIDR(c.awsClient.GetVPCIPv4CIDR())
	if err != nil {
		return errors.Wrap(err, "ipamd init: failed to retrieve VPC CIDR")
	}

	primaryIP := net.ParseIP(c.awsClient.GetLocalIPv4())
	err = c.networkClient.SetupHostNetwork(vpcCIDR, vpcCIDRs, c.awsClient.GetPrimaryENImac(), &primaryIP)
	if err != nil {
		return errors.Wrap(err, "ipamd init: failed to set up host network")
	}

	c.dataStore = datastore.NewDataStore(log)
	for _, eni := range enis {
		log.Debugf("Discovered ENI %s, trying to set it up", eni.ENIID)
		// Retry ENI sync
		retry := 0
		for {
			retry++
			if err = c.setupENI(eni.ENIID, eni); err == nil {
				log.Infof("ENI %s set up.", eni.ENIID)
				break
			}

			if retry > maxRetryCheckENI {
				log.Warn("Unable to discover attached IPs for ENI from metadata service")
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
	localPods, err := c.getLocalPodsWithRetry()
	if err != nil {
		log.Warnf("During ipamd init, failed to get Pod information from Kubernetes API Server %v", err)
		ipamdErrInc("nodeInitK8SGetLocalPodIPsFailed")
		// This can happens when L-IPAMD starts before kubelet.
		return errors.Wrap(err, "failed to get running pods!")
	}
	log.Debugf("getLocalPodsWithRetry() found %d local pods", len(localPods))

	rules, err := c.networkClient.GetRuleList()
	if err != nil {
		log.Errorf("During ipamd init: failed to retrieve IP rule list %v", err)
		return nil
	}

	for _, ip := range localPods {
		if ip.Sandbox == "" {
			log.Infof("Skipping Pod %s, Namespace %s, due to no matching sandbox", ip.Name, ip.Namespace)
			continue
		}
		if ip.IP == "" {
			log.Infof("Skipping Pod %s, Namespace %s, due to no IP", ip.Name, ip.Namespace)
			continue
		}
		log.Infof("Recovered AddNetwork for Pod %s, Namespace %s, Sandbox %s", ip.Name, ip.Namespace, ip.Sandbox)
		_, _, err = c.dataStore.AssignPodIPv4Address(ip)
		if err != nil {
			ipamdErrInc("nodeInitAssignPodIPv4AddressFailed")
			log.Warnf("During ipamd init, failed to use pod IP %s returned from Kubernetes API Server %v", ip.IP, err)
		}

		// Update ip rules in case there is a change in VPC CIDRs, AWS_VPC_K8S_CNI_EXTERNALSNAT setting
		srcIPNet := net.IPNet{IP: net.ParseIP(ip.IP), Mask: net.IPv4Mask(255, 255, 255, 255)}

		err = c.networkClient.UpdateRuleListBySrc(rules, srcIPNet, pbVPCcidrs, !c.networkClient.UseExternalSNAT())
		if err != nil {
			log.Errorf("UpdateRuleListBySrc in nodeInit() failed for IP %s: %v", ip.IP, err)
		}
	}
	// For a new node, attach IPs
	increasedPool, err := c.tryAssignIPs()
	if err == nil && increasedPool {
		c.updateLastNodeIPPoolAction()
	}
	return err
}

func (c *IPAMContext) updateIPStats(unmanaged int) {
	ipMax.Set(float64(c.maxIPsPerENI * (c.maxENI - unmanaged)))
	enisMax.Set(float64(c.maxENI - unmanaged))
}

func (c *IPAMContext) getLocalPodsWithRetry() ([]*k8sapi.K8SPodInfo, error) {
	var pods []*k8sapi.K8SPodInfo
	var err error
	for retry := 1; retry <= maxK8SRetries; retry++ {
		pods, err = c.k8sClient.K8SGetLocalPodIPs()
		if err == nil {
			// Check for pods with no IP since the API server might not have the latest state of the node.
			allPodsHaveAnIP := true
			for _, pod := range pods {
				if pod.IP == "" {
					log.Infof("Pod %s, Namespace %s, has no IP", pod.Name, pod.Namespace)
					allPodsHaveAnIP = false
				}
			}
			if allPodsHaveAnIP {
				break
			}
			log.Warnf("Not all pods have an IP, trying again in %v seconds.", retryK8SInterval.Seconds())
		}
		log.Infof("Not able to get local pods yet (attempt %d/%d): %v", retry, maxK8SRetries, err)
		time.Sleep(retryK8SInterval)
	}

	if err != nil {
		return nil, errors.Wrap(err, "no pods because apiserver not running.")
	}

	if pods == nil {
		return nil, nil
	}

	// Ask the CRI for the set of running pod sandboxes. These sandboxes are
	// what the CNI operates on, but the Kubernetes API doesn't expose any
	// information about them. If we relied only on the Kubernetes API, we
	// could leak IPs or unassign an IP from a still-running pod.
	var sandboxes map[string]*cri.SandboxInfo
	for retry := 1; retry <= maxK8SRetries; retry++ {
		sandboxes, err = c.criClient.GetRunningPodSandboxes(log)
		if err == nil {
			break
		}
		log.Infof("Not able to get local pod sandboxes yet (attempt %d/%d): %v", retry, maxK8SRetries, err)
		time.Sleep(retryK8SInterval)
	}
	if err != nil {
		return nil, errors.Wrap(err, "Unable to get local pod sandboxes")
	}

	// TODO consider using map
	for _, pod := range pods {
		// Fill in the sandbox ID by matching against the pod's UID
		for _, sandbox := range sandboxes {
			if sandbox.K8SUID == pod.UID {
				log.Debugf("Found pod(%v)'s sandbox ID: %v ", sandbox.Name, sandbox.ID)
				pod.Sandbox = sandbox.ID
				break
			}
		}
	}
	return pods, nil
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
	if _, over, warmIPTargetDefined := c.ipTargetState(); warmIPTargetDefined && over > 0 {
		eniInfos := c.dataStore.GetENIInfos()
		for eniID := range eniInfos.ENIIPPools {
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
	podIPInfos := c.dataStore.GetPodInfos()
	usedIPs := sets.String{}
	// Get IPs that are currently in use by pods
	for _, pod := range *podIPInfos {
		usedIPs.Insert(pod.IP)
	}

	// Get IPs that are currently attached to the instance
	eniInfos := c.dataStore.GetENIInfos()
	eniIPPools := eniInfos.ENIIPPools

	pool, ok := eniIPPools[eni]
	if !ok {
		return nil, fmt.Errorf("error finding available IPs: eni %s does not exist", eni)
	}

	allocatedIPs := sets.String{}
	for _, ip := range pool.IPv4Addresses {
		allocatedIPs.Insert(ip.Address)
	}

	availableIPs := allocatedIPs.Difference(usedIPs).UnsortedList()
	var freeableIPs []string

	// Free the number of IPs `over` the warm IP target, unless `over` is greater than the number of available IPs on
	// this ENI. In that case we should only free the number of available IPs.
	_, over, _ := c.ipTargetState()
	numFreeable := min(over, len(availableIPs))

	for _, ip := range availableIPs[:numFreeable] {
		freeableIPs = append(freeableIPs, ip)
	}
	return freeableIPs, nil
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
		// If we did not add an IP, try to add an ENI instead.
		if c.dataStore.GetENIs() < (c.maxENI - c.unmanagedENI) {
			if err = c.tryAllocateENI(); err == nil {
				c.updateLastNodeIPPoolAction()
			}
		} else {
			log.Debugf("Skipping ENI allocation as the instance's max ENI limit of %d is already reached (accounting for %d unmanaged ENIs)", c.maxENI, c.unmanagedENI)
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

	err = c.awsClient.AllocIPAddresses(eni, ipsToAllocate)
	if err != nil {
		log.Warnf("Failed to allocate %d IP addresses on an ENI: %v", ipsToAllocate, err)
		// Continue to process the allocated IP addresses
		ipamdErrInc("increaseIPPoolAllocIPAddressesFailed")
	}

	eniMetadata, err := c.waitENIAttached(eni)
	if err != nil {
		ipamdErrInc("increaseIPPoolwaitENIAttachedFailed")
		log.Errorf("Failed to increase pool size: Unable to discover attached ENI from metadata service %v", err)
		return err
	}

	err = c.setupENI(eni, eniMetadata)
	if err != nil {
		ipamdErrInc("increaseIPPoolsetupENIFailed")
		log.Errorf("Failed to increase pool size: %v", err)
		return err
	}
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
func (c *IPAMContext) setupENI(eni string, eniMetadata awsutils.ENIMetadata) error {
	// Add the ENI to the datastore
	err := c.dataStore.AddENI(eni, eniMetadata.DeviceNumber, eni == c.awsClient.GetPrimaryENI())
	if err != nil && err.Error() != datastore.DuplicatedENIError {
		return errors.Wrapf(err, "failed to add ENI %s to data store", eni)
	}

	// For secondary ENIs, set up the network
	if eni != c.awsClient.GetPrimaryENI() {
		eniPrimaryIP := eniMetadata.PrimaryIPv4Address()
		err = c.networkClient.SetupENINetwork(eniPrimaryIP, eniMetadata.MAC, eniMetadata.DeviceNumber, eniMetadata.SubnetIPv4CIDR)
		if err != nil {
			return errors.Wrapf(err, "failed to set up ENI %s network", eni)
		}
	}

	c.primaryIP[eni] = c.addENIaddressesToDataStore(eniMetadata.IPv4Addresses, eni)
	return nil
}

// return primary ip address on the interface
func (c *IPAMContext) addENIaddressesToDataStore(ec2Addrs []*ec2.NetworkInterfacePrivateIpAddress, eni string) string {
	var primaryIP string
	for _, ec2Addr := range ec2Addrs {
		if aws.BoolValue(ec2Addr.Primary) {
			primaryIP = aws.StringValue(ec2Addr.PrivateIpAddress)
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
	return primaryIP
}

func (c *IPAMContext) waitENIAttached(eni string) (awsutils.ENIMetadata, error) {
	// Wait until the ENI shows up in the instance metadata service
	retry := 0
	for {
		enis, err := c.awsClient.GetAttachedENIs()
		if err != nil {
			log.Warnf("Failed to increase pool, error trying to discover attached ENIs: %v ", err)
		} else {
			// Verify that the ENI we are waiting for is in the returned list
			for _, returnedENI := range enis {
				if eni == returnedENI.ENIID {
					return returnedENI, nil
				}
			}
			log.Debugf("Not able to find the right ENI yet (attempt %d/%d)", retry, maxRetryCheckENI)
		}
		retry++
		if retry > maxRetryCheckENI {
			ipamdErrInc("waitENIAttachedMaxRetryExceeded")
			return awsutils.ENIMetadata{}, errors.New("waitENIAttached: giving up trying to retrieve ENIs from metadata service")
		}
		log.Debugf("Not able to discover attached ENIs yet (attempt %d/%d)", retry, maxRetryCheckENI)
		time.Sleep(eniAttachTime)
	}
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
	attachedENIs := c.filterUnmanagedENIs(allENIs)
	currentENIIPPools := c.dataStore.GetENIInfos().ENIIPPools

	// Check if a new ENI was added, if so we need to update the tags
	needToUpdateTags := false
	for _, attachedENI := range attachedENIs {
		if _, ok := currentENIIPPools[attachedENI.ENIID]; !ok {
			needToUpdateTags = true
			break
		}
	}
	if needToUpdateTags {
		log.Debugf("A new ENI added but not by ipamd, updating tags")
		allENIs, tagMap, err := c.awsClient.DescribeAllENIs()
		if err != nil {
			log.Warnf("Failed to call EC2 to describe ENIs, aborting reconcile: %v", err)
			return
		}
		c.setUnmanagedENIs(tagMap)
		attachedENIs = c.filterUnmanagedENIs(allENIs)
	}

	// Mark phase
	for _, attachedENI := range attachedENIs {
		eniIPPool, err := c.dataStore.GetENIIPPools(attachedENI.ENIID)
		if err == nil {
			// If the attached ENI is in the data store
			log.Debugf("Reconcile existing ENI %s IP pool", attachedENI.ENIID)
			// Reconcile IP pool
			c.eniIPPoolReconcile(eniIPPool, attachedENI, attachedENI.ENIID)
			// Mark action, remove this ENI from currentENIIPPools map
			delete(currentENIIPPools, attachedENI.ENIID)
			continue
		}

		// Add new ENI
		log.Debugf("Reconcile and add a new ENI %s", attachedENI)
		err = c.setupENI(attachedENI.ENIID, attachedENI)
		if err != nil {
			log.Errorf("IP pool reconcile: Failed to set up ENI %s network: %v", attachedENI.ENIID, err)
			ipamdErrInc("eniReconcileAdd")
			// Continue if having trouble with ONLY 1 ENI, instead of bailout here?
			continue
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniReconcileAdd"}).Inc()
	}

	// Sweep phase: since the marked ENI have been removed, the remaining ones needs to be sweeped
	for eni := range currentENIIPPools {
		log.Infof("Reconcile and delete detached ENI %s", eni)
		// Force the delete, since aws local metadata has told us that this ENI is no longer
		// attached, so any IPs assigned from this ENI will no longer work.
		err = c.dataStore.RemoveENIFromDataStore(eni, true /* force */)
		if err != nil {
			log.Errorf("IP pool reconcile: Failed to delete ENI during reconcile: %v", err)
			ipamdErrInc("eniReconcileDel")
			continue
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniReconcileDel"}).Inc()
	}
	log.Debug("Successfully Reconciled ENI/IP pool")
	total, assigned := c.dataStore.GetStats()
	log.Debugf("IP Address Pool stats: total: %d, assigned: %d", total, assigned)
	c.lastNodeIPPoolAction = curTime
}

func (c *IPAMContext) eniIPPoolReconcile(ipPool map[string]*datastore.AddressInfo, attachedENI awsutils.ENIMetadata, eni string) {
	for _, privateIPv4 := range attachedENI.IPv4Addresses {
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
				log.Debugf("This IP was recently freed, but is out of cooldown. We need to verify with EC2 control plane.")
				// Call EC2 to verify IPs on this ENI
				ec2Addresses, err := c.awsClient.GetIPv4sFromEC2(eni)
				if err != nil {
					log.Error("Failed to fetch ENI IP addresses!")
					continue
				} else {
					// Verify that the IP really belongs to this ENI
					isReallyAttachedToENI := false
					for _, ec2Addr := range ec2Addresses {
						if strPrivateIPv4 == aws.StringValue(ec2Addr.PrivateIpAddress) {
							isReallyAttachedToENI = true
							log.Debugf("Verified that IP %s is attached to ENI %s", strPrivateIPv4, eni)
							break
						}
					}
					if isReallyAttachedToENI {
						c.reconcileCooldownCache.Remove(strPrivateIPv4)
					} else {
						log.Warnf("Skipping IP %s on ENI %s because it does not belong to this ENI!.", strPrivateIPv4, eni)
						continue
					}
				}
			}
		}

		err := c.dataStore.AddIPv4AddressToStore(eni, strPrivateIPv4)
		if err != nil && err.Error() == datastore.IPAlreadyInStoreError {
			// mark action = remove it from ipPool since the IP should not be deleted
			delete(ipPool, strPrivateIPv4)
			continue
		}

		if err != nil {
			log.Errorf("Failed to reconcile IP %s on ENI %s", strPrivateIPv4, eni)
			ipamdErrInc("ipReconcileAdd")
			// continue instead of bailout due to one IP
			continue
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniIPPoolReconcileAdd"}).Inc()
	}

	// Sweep phase, delete remaining IPs
	for existingIP := range ipPool {
		log.Debugf("Reconcile and delete IP %s on ENI %s", existingIP, eni)
		// Force the delete, since aws local metadata has told us that this ENI is no longer
		// attached, so any IPs assigned from this ENI will no longer work.
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
	return getEnvBoolWithDefault(envDisableENIProvisioning, noDisableENIProvisioning)
}

// filterUnmanagedENIs filters out ENIs marked with the "node.k8s.amazonaws.com/no_manage" tag
func (c *IPAMContext) filterUnmanagedENIs(enis []awsutils.ENIMetadata) []awsutils.ENIMetadata {
	numFiltered := 0
	ret := make([]awsutils.ENIMetadata, 0, len(enis))
	for _, eni := range enis {
		// If we have unmanaged ENIs, filter them out
		if c.unmanagedENIs.isUnmanaged(eni.ENIID) {
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
