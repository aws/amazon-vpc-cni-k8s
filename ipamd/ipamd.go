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

package ipamd

import (
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	log "github.com/cihub/seelog"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/aws/aws-sdk-go/aws"

	"github.com/aws/aws-sdk-go/service/ec2"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/docker"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/eniconfig"
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
	maxK8SRetries               = 12
	retryK8SInterval            = 5 * time.Second

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
)

var (
	ipamdErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_ipamd_error_count",
			Help: "The number of errors encountered in ipamd",
		},
		[]string{"fn", "error"},
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
			Help: "The maximum number of ENIs that can be attached to the instance",
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
			Help: "The number of add IP address request",
		},
	)
	delIPCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_del_ip_req_count",
			Help: "The number of delete IP address request",
		},
		[]string{"reason"},
	)
	prometheusRegistered = false
)

// IPAMContext contains node level control information
type IPAMContext struct {
	awsClient     awsutils.APIs
	dataStore     *datastore.DataStore
	k8sClient     k8sapi.K8SAPIs
	eniConfig     eniconfig.ENIConfig
	dockerClient  docker.APIs
	networkClient networkutils.NetworkAPIs

	currentMaxAddrsPerENI int64
	maxAddrsPerENI        int64
	// maxENI indicate the maximum number of ENIs can be attached to the instance
	// It is initialized to 0 and it is set to current number of ENIs attached
	// when ipamd receives AttachmentLimitExceeded error
	maxENI               int
	primaryIP            map[string]string
	lastNodeIPPoolAction time.Time
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
	c.dockerClient = docker.New()
	c.eniConfig = eniConfig

	client, err := awsutils.New()
	if err != nil {
		log.Errorf("Failed to initialize awsutil interface %v", err)
		return nil, errors.Wrap(err, "ipamd: can not initialize with AWS SDK interface")
	}

	c.awsClient = client

	err = c.nodeInit()
	if err != nil {
		return nil, err
	}
	return c, nil
}

//TODO need to break this function down(comments from CR)
func (c *IPAMContext) nodeInit() error {
	ipamdActionsInprogress.WithLabelValues("nodeInit").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("nodeInit").Sub(float64(1))

	instanceMaxENIs, _ := c.awsClient.GetENILimit()
	maxENIs := getMaxENI(instanceMaxENIs)
	if maxENIs >= 1 {
		enisMax.Set(float64(maxENIs))
	}

	maxIPs, err := c.awsClient.GetENIipLimit()
	if err == nil {
		ipMax.Set(float64(maxIPs * int64(maxENIs)))
	}
	c.primaryIP = make(map[string]string)

	enis, err := c.awsClient.GetAttachedENIs()
	if err != nil {
		log.Error("Failed to retrieve ENI info")
		return errors.New("ipamd init: failed to retrieve attached ENIs info")
	}

	_, vpcCIDR, err := net.ParseCIDR(c.awsClient.GetVPCIPv4CIDR())
	if err != nil {
		log.Error("Failed to parse VPC IPv4 CIDR", err.Error())
		return errors.Wrap(err, "ipamd init: failed to retrieve VPC CIDR")
	}

	primaryIP := net.ParseIP(c.awsClient.GetLocalIPv4())
	err = c.networkClient.SetupHostNetwork(vpcCIDR, c.awsClient.GetVPCIPv4CIDRs(), c.awsClient.GetPrimaryENImac(), &primaryIP)
	if err != nil {
		log.Error("Failed to setup host network", err)
		return errors.Wrap(err, "ipamd init: failed to setup host network")
	}

	c.dataStore = datastore.NewDataStore()
	for _, eni := range enis {
		log.Debugf("Discovered ENI %s", eni.ENIID)

		err = c.setupENI(eni.ENIID, eni)
		if err != nil {
			log.Errorf("Failed to setup ENI %s network: %v", eni.ENIID, err)
			return errors.Wrapf(err, "Failed to setup ENI %v", eni.ENIID)
		}
	}

	usedIPs, err := c.getLocalPodsWithRetry()
	if err != nil {
		log.Warnf("During ipamd init, failed to get Pod information from kubelet %v", err)
		ipamdErrInc("nodeInitK8SGetLocalPodIPsFailed", err)
		// This can happens when L-IPAMD starts before kubelet.
		// TODO  need to add node health stats here
		return nil
	}

	rules, err := c.networkClient.GetRuleList()
	if err != nil {
		log.Errorf("During ipamd init: failed to retrieve IP rule list %v", err)
		return nil
	}

	for _, ip := range usedIPs {
		if ip.Container == "" {
			log.Infof("Skipping Pod %s, Namespace %s, due to no matching container", ip.Name, ip.Namespace)
			continue
		}
		if ip.IP == "" {
			log.Infof("Skipping Pod %s, Namespace %s, due to no IP", ip.Name, ip.Namespace)
			continue
		}
		log.Infof("Recovered AddNetwork for Pod %s, Namespace %s, Container %s", ip.Name, ip.Namespace, ip.Container)
		_, _, err = c.dataStore.AssignPodIPv4Address(ip)
		if err != nil {
			ipamdErrInc("nodeInitAssignPodIPv4AddressFailed", err)
			log.Warnf("During ipamd init, failed to use pod ip %s returned from Kubelet %v", ip.IP, err)
			// TODO continue, but need to add node health stats here
			// TODO need to feed this to controller on the health of pod and node
			// This is a bug among kubelet/cni-plugin/l-ipamd/ec2-metadata that this particular pod is using an non existent ip address.
			// Here we choose to continue instead of returning error and EXIT out L-IPAMD(exit L-IPAMD will make whole node out)
			// The plan(TODO) is to feed this info back to controller and let controller cleanup this pod from this node.
		}

		// Update ip rules in case there is a change in VPC CIDRs, AWS_VPC_K8S_CNI_EXTERNALSNAT setting
		srcIPNet := net.IPNet{IP: net.ParseIP(ip.IP), Mask: net.IPv4Mask(255, 255, 255, 255)}
		vpcCIDRs := c.awsClient.GetVPCIPv4CIDRs()

		var pbVPCcidrs []string
		for _, cidr := range vpcCIDRs {
			pbVPCcidrs = append(pbVPCcidrs, *cidr)
		}

		err = c.networkClient.UpdateRuleListBySrc(rules, srcIPNet, pbVPCcidrs, !c.networkClient.UseExternalSNAT())
		if err != nil {
			log.Errorf("UpdateRuleListBySrc in nodeInit() failed for IP %s: %v", ip.IP, err)
		}
	}
	return nil
}

func (c *IPAMContext) getLocalPodsWithRetry() ([]*k8sapi.K8SPodInfo, error) {
	var pods []*k8sapi.K8SPodInfo
	var err error
	for retry := 1; retry <= maxK8SRetries; retry++ {
		pods, err = c.k8sClient.K8SGetLocalPodIPs()
		if err == nil {
			break
		}
		log.Infof("Not able to get local pods yet (attempt %d/%d): %v", retry, maxK8SRetries, err)
		time.Sleep(retryK8SInterval)
	}

	if pods == nil {
		return nil, errors.New("unable to get local pods, giving up")
	}

	var containers map[string]*docker.ContainerInfo

	for retry := 1; retry <= maxK8SRetries; retry++ {
		containers, err = c.dockerClient.GetRunningContainers()
		if err == nil {
			break
		}
		log.Infof("Not able to get local containers yet (attempt %d/%d): %v", retry, maxK8SRetries, err)
		time.Sleep(retryK8SInterval)
	}

	// TODO consider using map
	for _, pod := range pods {
		// needs to find the container ID
		for _, container := range containers {
			if container.K8SUID == pod.UID {
				log.Debugf("Found pod(%v)'s container ID: %v ", container.Name, container.ID)
				pod.Container = container.ID
				break
			}
		}
	}
	return pods, nil
}

// StartNodeIPPoolManager monitors the IP pool, add or del them when it is required.
func (c *IPAMContext) StartNodeIPPoolManager() {
	for {
		time.Sleep(ipPoolMonitorInterval)
		c.updateIPPoolIfRequired()
		c.nodeIPPoolReconcile(nodeIPPoolReconcileInterval)
	}
}

func (c *IPAMContext) updateIPPoolIfRequired() {
	c.retryAllocENIIP()
	if c.nodeIPPoolTooLow() {
		c.increaseIPPool()
	} else if c.nodeIPPoolTooHigh() {
		c.decreaseIPPool()
	}
}

// TODO: Does not retry!
func (c *IPAMContext) retryAllocENIIP() {
	ipamdActionsInprogress.WithLabelValues("retryAllocENIIP").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("retryAllocENIIP").Sub(float64(1))

	curIPTarget, warmIPTargetDefined := c.getCurWarmIPTarget()
	if warmIPTargetDefined && curIPTarget <= 0 {
		log.Debugf("Skipping retry allocating ENI IP, warm IP target reached")
		return
	}
	maxIPLimit, err := c.awsClient.GetENIipLimit()
	if err != nil {
		log.Infof("Failed to retrieve ENI IP limit: %v", err)
		return
	}
	eni := c.dataStore.GetENINeedsIP(maxIPLimit, UseCustomNetworkCfg())
	if eni != nil {
		log.Debugf("Attempt again to allocate IP address for ENI :%s", eni.ID)
		var err error
		if warmIPTargetDefined {
			err = c.awsClient.AllocIPAddresses(eni.ID, int64(curIPTarget))
		} else {
			err = c.awsClient.AllocIPAddresses(eni.ID, maxIPLimit)
		}
		if err != nil {
			ipamdErrInc("retryAllocENIIPAllocAllIPAddressFailed", err)
			log.Warn("During eni repair: error encountered on allocate IP address", err)
			return
		}
		ec2Addrs, _, err := c.getENIaddresses(eni.ID)
		if err != nil {
			ipamdErrInc("retryAllocENIIPgetENIaddressesFailed", err)
			log.Warn("During eni repair: failed to get ENI ip addresses", err)
			return
		}
		c.lastNodeIPPoolAction = time.Now()
		c.addENIaddressesToDataStore(ec2Addrs, eni.ID)

		curIPTarget, warmIPTargetDefined := c.getCurWarmIPTarget()
		if warmIPTargetDefined && curIPTarget <= 0 {
			log.Debugf("Finish retry allocating ENI IP, warm IP target reached")
			return
		}
	}
}

func (c *IPAMContext) decreaseIPPool() {
	ipamdActionsInprogress.WithLabelValues("decreaseIPPool").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("decreaseIPPool").Sub(float64(1))
	eni := c.dataStore.FreeENI()
	if eni == "" {
		log.Info("No ENI to remove, all ENIs have IPs in use")
		return
	}
	log.Debugf("Start freeing ENI %s", eni)
	err := c.awsClient.FreeENI(eni)
	if err != nil {
		ipamdErrInc("decreaseIPPoolFreeENIFailed", err)
		log.Errorf("Failed to free ENI %s, err: %v", eni, err)
		return
	}
	c.lastNodeIPPoolAction = time.Now()
	total, used := c.dataStore.GetStats()
	log.Debugf("Successfully decreased IP pool")
	logPoolStats(int64(total), int64(used), c.currentMaxAddrsPerENI, c.maxAddrsPerENI)
}

func isAttachmentLimitExceededError(err error) bool {
	return strings.Contains(err.Error(), "AttachmentLimitExceeded")
}

func (c *IPAMContext) increaseIPPool() {
	log.Debug("Start increasing IP pool size")
	ipamdActionsInprogress.WithLabelValues("increaseIPPool").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("increaseIPPool").Sub(float64(1))

	curIPTarget, warmIPTargetDefined := c.getCurWarmIPTarget()
	if warmIPTargetDefined && curIPTarget <= 0 {
		log.Debugf("Skipping increase IP pool, warm IP target reached")
		return
	}

	instanceMaxENIs, err := c.awsClient.GetENILimit()
	maxENIs := getMaxENI(instanceMaxENIs)
	if maxENIs >= 1 {
		enisMax.Set(float64(maxENIs))
	}

	if err == nil && maxENIs == c.dataStore.GetENIs() {
		log.Debugf("Skipping increase IP pool due to max ENI already attached to the instance : %d", maxENIs)
		return
	}
	if (c.maxENI > 0) && (c.maxENI == c.dataStore.GetENIs()) {
		if c.maxENI < maxENIs {
			errString := "desired: " + strconv.FormatInt(int64(maxENIs), 10) + "current: " + strconv.FormatInt(int64(c.maxENI), 10)
			ipamdErrInc("unExpectedMaxENIAttached", errors.New(errString))
		}
		log.Debugf("Skipping increase IP pool due to max ENI already attached to the instance : %d", c.maxENI)
		return
	}

	var securityGroups []*string
	var subnet string
	customNetworkCfg := UseCustomNetworkCfg()
	if customNetworkCfg {
		eniCfg, err := c.eniConfig.MyENIConfig()

		if err != nil {
			log.Errorf("Failed to get pod ENI config")
			return
		}

		log.Infof("ipamd: using custom network config: %v, %s", eniCfg.SecurityGroups, eniCfg.Subnet)
		for _, sgID := range eniCfg.SecurityGroups {
			log.Debugf("Found security-group id: %s", sgID)
			securityGroups = append(securityGroups, aws.String(sgID))
		}
		subnet = eniCfg.Subnet
	}

	eni, err := c.awsClient.AllocENI(customNetworkCfg, securityGroups, subnet)
	if err != nil {
		log.Errorf("Failed to increase pool size due to not able to allocate ENI %v", err)

		if isAttachmentLimitExceededError(err) {
			c.maxENI = c.dataStore.GetENIs()
			log.Infof("Discovered the instance max ENI allowed is: %d", c.maxENI)
		}
		// TODO need to add health stats
		ipamdErrInc("increaseIPPoolAllocENI", err)
		return
	}

	maxIPLimit, err := c.awsClient.GetENIipLimit()
	if err != nil {
		log.Infof("Failed to retrieve ENI IP limit: %v", err)
		return
	}

	if warmIPTargetDefined {
		err = c.awsClient.AllocIPAddresses(eni, int64(curIPTarget))
	} else {
		err = c.awsClient.AllocIPAddresses(eni, maxIPLimit)
	}
	if err != nil {
		log.Warnf("Failed to allocate all available ip addresses on an ENI %v", err)
		// Continue to process the allocated IP addresses
		ipamdErrInc("increaseIPPoolAllocAllIPAddressFailed", err)
	}

	eniMetadata, err := c.waitENIAttached(eni)
	if err != nil {
		ipamdErrInc("increaseIPPoolwaitENIAttachedFailed", err)
		log.Errorf("Failed to increase pool size: not able to discover attached ENI from metadata service %v", err)
		return
	}

	err = c.setupENI(eni, eniMetadata)
	if err != nil {
		ipamdErrInc("increaseIPPoolsetupENIFailed", err)
		log.Errorf("Failed to increase pool size: %v", err)
		return
	}
	c.lastNodeIPPoolAction = time.Now()
	total, used := c.dataStore.GetStats()
	log.Debugf("Successfully increased IP pool")
	logPoolStats(int64(total), int64(used), c.currentMaxAddrsPerENI, c.maxAddrsPerENI)
}

// setupENI does following:
// 1) add ENI to datastore
// 2) add all ENI's secondary IP addresses to datastore
// 3) setup linux ENI related networking stack.
func (c *IPAMContext) setupENI(eni string, eniMetadata awsutils.ENIMetadata) error {
	// Have discovered the attached ENI from metadata service
	// add eni's IP to IP pool
	err := c.dataStore.AddENI(eni, int(eniMetadata.DeviceNumber), eni == c.awsClient.GetPrimaryENI())
	if err != nil && err.Error() != datastore.DuplicatedENIError {
		return errors.Wrapf(err, "failed to add ENI %s to data store", eni)
	}

	ec2Addrs, eniPrimaryIP, err := c.getENIaddresses(eni)
	if err != nil {
		return errors.Wrapf(err, "failed to retrieve ENI %s IP addresses", eni)
	}

	c.currentMaxAddrsPerENI, err = c.awsClient.GetENIipLimit()

	if err != nil {
		// If the instance type is not supported in ipamd and the GetENIipLimit() call returns an error
		// the code here falls back to use the number of IPs discovered on the ENI.
		// note: the number of IP discovered on the ENI at a time can be less than the number of supported IPs on
		// an ENI, for example: ipamd has NOT allocated all IPs on the ENI yet.
		c.currentMaxAddrsPerENI = int64(len(ec2Addrs))
	}
	if c.currentMaxAddrsPerENI > c.maxAddrsPerENI {
		c.maxAddrsPerENI = c.currentMaxAddrsPerENI
	}

	if eni != c.awsClient.GetPrimaryENI() {
		err = c.networkClient.SetupENINetwork(eniPrimaryIP, eniMetadata.MAC,
			int(eniMetadata.DeviceNumber), eniMetadata.SubnetIPv4CIDR)
		if err != nil {
			return errors.Wrapf(err, "failed to setup ENI %s network", eni)
		}
	}

	c.primaryIP[eni] = c.addENIaddressesToDataStore(ec2Addrs, eni)
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
		err := c.dataStore.AddENIIPv4Address(eni, aws.StringValue(ec2Addr.PrivateIpAddress))
		if err != nil && err.Error() != datastore.DuplicateIPError {
			log.Warnf("Failed to increase IP pool, failed to add IP %s to data store", ec2Addr.PrivateIpAddress)
			// continue to add next address
			// TODO need to add health stats for err
			ipamdErrInc("addENIaddressesToDataStoreAddENIIPv4AddressFailed", err)
		}
	}

	return primaryIP
}

// returns all addresses on ENI, the primary address on ENI, error
func (c *IPAMContext) getENIaddresses(eni string) ([]*ec2.NetworkInterfacePrivateIpAddress, string, error) {
	ec2Addrs, _, err := c.awsClient.DescribeENI(eni)
	if err != nil {
		return nil, "", errors.Wrapf(err, "failed to find ENI addresses for ENI %s", eni)
	}

	for _, ec2Addr := range ec2Addrs {
		if aws.BoolValue(ec2Addr.Primary) {
			eniPrimaryIP := aws.StringValue(ec2Addr.PrivateIpAddress)
			return ec2Addrs, eniPrimaryIP, nil
		}
	}

	return nil, "", errors.Errorf("failed to find the ENI's primary address for ENI %s", eni)
}

func (c *IPAMContext) waitENIAttached(eni string) (awsutils.ENIMetadata, error) {
	// Wait until the ENI shows up in the instance metadata service
	retry := 0
	for {
		enis, err := c.awsClient.GetAttachedENIs()
		if err != nil {
			log.Warnf("Failed to increase pool, error trying to discover attached ENIs: %v ", err)
			time.Sleep(eniAttachTime)
			continue
		}

		// Verify that the ENI we are waiting for is in the returned list
		for _, returnedENI := range enis {
			if eni == returnedENI.ENIID {
				return returnedENI, nil
			}
		}

		retry++
		if retry > maxRetryCheckENI {
			log.Errorf("unable to discover attached ENI from metadata service")
			// TODO need to add health stats
			ipamdErrInc("waitENIAttachedMaxRetryExceeded", err)
			return awsutils.ENIMetadata{}, errors.New("waitENIAttached: not able to retrieve ENI from metadata service")
		}
		log.Debugf("Not able to discover attached ENI yet (attempt %d/%d)", retry, maxRetryCheckENI)

		time.Sleep(eniAttachTime)
	}
}

// getMaxENI returns the maximum number of ENIs for this instance, which is
// the lesser of the given lower bound (for example, the limit for the instance
// type) and a value configured via the MAX_ENI environment variable.
//
// If the value configured via environment variable is 0 or less, it is
// ignored, and the lowerBound is returned.
func getMaxENI(lowerBound int) int {
	inputStr, found := os.LookupEnv(envMaxENI)

	envMax := defaultMaxENI
	if found {
		if input, err := strconv.Atoi(inputStr); err == nil && input >= 1 {
			log.Debugf("Using MAX_ENI %v", input)
			envMax = input
		}
	}

	// If envMax is defined (>=1) and is less than the input lower bound, return
	// envMax.
	if envMax >= 1 && envMax < lowerBound {
		return envMax
	}

	return lowerBound
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
		log.Debugf("Using WARM-ENI-TARGET %v", input)
		return input
	}
	return defaultWarmENITarget
}

func logPoolStats(total, used, currentMaxAddrsPerENI, maxAddrsPerENI int64) {
	log.Debugf("IP pool stats: total = %d, used = %d, c.currentMaxAddrsPerENI = %d, c.maxAddrsPerENI = %d",
		total, used, currentMaxAddrsPerENI, maxAddrsPerENI)
}

// nodeIPPoolTooLow returns true if IP pool is below low threshold
func (c *IPAMContext) nodeIPPoolTooLow() bool {
	curIPTarget, warmIPTargetDefined := c.getCurWarmIPTarget()
	if warmIPTargetDefined && curIPTarget <= 0 {
		return false
	}

	if warmIPTargetDefined && curIPTarget > 0 {
		return true
	}

	// If WARM-IP-TARGET not defined fallback using number of ENIs
	warmENITarget := getWarmENITarget()
	total, used := c.dataStore.GetStats()
	logPoolStats(int64(total), int64(used), c.currentMaxAddrsPerENI, c.maxAddrsPerENI)

	available := total - used
	return int64(available) < c.maxAddrsPerENI*int64(warmENITarget)
}

// nodeIPPoolTooHigh returns true if IP pool is above high threshold
func (c *IPAMContext) nodeIPPoolTooHigh() bool {
	warmENITarget := getWarmENITarget()
	total, used := c.dataStore.GetStats()
	logPoolStats(int64(total), int64(used), c.currentMaxAddrsPerENI, c.maxAddrsPerENI)

	available := total - used

	target := getWarmIPTarget()
	if int64(target) != noWarmIPTarget && int64(target) >= int64(available) {
		return false
	}

	return int64(available) >= (int64(warmENITarget)+1)*c.maxAddrsPerENI
}

func ipamdErrInc(fn string, err error) {
	ipamdErr.With(prometheus.Labels{"fn": fn, "error": err.Error()}).Inc()
}

// nodeIPPoolReconcile reconcile ENI and IP info from metadata service and IP addresses in datastore
func (c *IPAMContext) nodeIPPoolReconcile(interval time.Duration) {
	ipamdActionsInprogress.WithLabelValues("nodeIPPoolReconcile").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("nodeIPPoolReconcile").Sub(float64(1))

	curTime := time.Now()
	last := c.lastNodeIPPoolAction

	if curTime.Sub(last) <= interval {
		return
	}

	log.Debug("Reconciling ENI/IP pool info...")
	attachedENIs, err := c.awsClient.GetAttachedENIs()

	if err != nil {
		log.Error("IP pool reconcile: Failed to get attached ENI info", err.Error())
		ipamdErrInc("reconcileFailedGetENIs", err)
		return
	}

	curENIs := c.dataStore.GetENIInfos()

	// mark phase
	for _, attachedENI := range attachedENIs {
		eniIPPool, err := c.dataStore.GetENIIPPools(attachedENI.ENIID)
		if err == nil {
			log.Debugf("Reconcile existing ENI %s IP pool", attachedENI.ENIID)
			// reconcile IP pool
			c.eniIPPoolReconcile(eniIPPool, attachedENI, attachedENI.ENIID)

			// Mark action, remove this ENI from curENIs list
			delete(curENIs.ENIIPPools, attachedENI.ENIID)
			continue
		}

		// add new ENI
		log.Debugf("Reconcile and add a new ENI %s", attachedENI)
		err = c.setupENI(attachedENI.ENIID, attachedENI)
		if err != nil {
			log.Errorf("IP pool reconcile: Failed to setup ENI %s network: %v", attachedENI.ENIID, err)
			ipamdErrInc("eniReconcileAdd", err)
			//continue if having trouble with ONLY 1 ENI, instead of bailout here?
			continue
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniReconcileAdd"}).Inc()
	}

	// Sweep phase: since the marked ENI have been removed, the remaining ones needs to be sweeped
	for eni := range curENIs.ENIIPPools {
		log.Infof("Reconcile and delete detached ENI %s", eni)
		err = c.dataStore.DeleteENI(eni)
		if err != nil {
			log.Errorf("IP pool reconcile: Failed to delete ENI during reconcile: %v", err)
			ipamdErrInc("eniReconcileDel", err)
			continue
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniReconcileDel"}).Inc()
	}
	log.Debug("Successfully Reconciled ENI/IP pool")
	c.lastNodeIPPoolAction = curTime
}

func (c *IPAMContext) eniIPPoolReconcile(ipPool map[string]*datastore.AddressInfo, attachENI awsutils.ENIMetadata, eni string) {
	for _, localIP := range attachENI.LocalIPv4s {
		if localIP == c.primaryIP[eni] {
			log.Debugf("Reconcile and skip primary IP %s on ENI %s", localIP, eni)
			continue
		}

		err := c.dataStore.AddENIIPv4Address(eni, localIP)
		if err != nil && err.Error() == datastore.DuplicateIPError {
			log.Debugf("Reconciled IP %s on ENI %s", localIP, eni)
			// mark action = remove it from eniPool
			delete(ipPool, localIP)
			continue
		}

		if err != nil {
			log.Errorf("Failed to reconcile IP %s on ENI %s", localIP, eni)
			ipamdErrInc("ipReconcileAdd", err)
			// continue instead of bailout due to one ip
			continue
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniIPPoolReconcileAdd"}).Inc()
	}

	// Sweep phase, delete remaining IPs
	for existingIP := range ipPool {
		log.Debugf("Reconcile and delete IP %s on ENI %s", existingIP, eni)
		err := c.dataStore.DelENIIPv4Address(eni, existingIP)
		if err != nil {
			log.Errorf("Failed to reconcile and delete IP %s on ENI %s, %v", existingIP, eni, err)
			ipamdErrInc("ipReconcileDel", err)
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
		log.Error("Failed to parse "+envCustomNetworkCfg+"; using default: false", err.Error())
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
			log.Debugf("Using WARM-IP-TARGET %v", input)
			return input
		}
	}
	return noWarmIPTarget
}

func (c *IPAMContext) getCurWarmIPTarget() (int64, bool) {
	target := getWarmIPTarget()
	if target == noWarmIPTarget {
		// there is no WARM_IP_TARGET defined, fallback to use all IP addresses on ENI
		return int64(target), false
	}

	total, used := c.dataStore.GetStats()
	curTarget := int64(target) - int64(total-used)
	log.Debugf("Current warm IP stats: target: %d, total: %d, used: %d, curTarget: %d", target, total, used, curTarget)
	return curTarget, true
}

// GetConfigForDebug returns the active values of the configuration env vars (for debugging purposes).
func GetConfigForDebug() map[string]interface{} {
	return map[string]interface{}{
		envWarmIPTarget:     getWarmIPTarget(),
		envWarmENITarget:    getWarmENITarget(),
		envCustomNetworkCfg: UseCustomNetworkCfg(),
	}
}
