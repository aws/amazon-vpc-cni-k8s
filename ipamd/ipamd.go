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
	"fmt"
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

// Package ipamd is a long running daemon which manages a warn-pool of available IP addresses.
// It also monitors the size of the pool, dynamically allocate more ENIs when the pool size goes below threshold and
// free them back when the pool size goes above max threshold.

const (
	ipPoolMonitorInterval       = 5 * time.Second
	maxRetryCheckENI            = 5
	eniAttachTime               = 10 * time.Second
	nodeIPPoolReconcileInterval = 60 * time.Second
	maxK8SRetries               = 12
	retryK8SInterval            = 5 * time.Second

	// This environment is used to specify the desired number of free IPs always available in "warm pool"
	// When it is not set, ipamD defaut to use the number IPs per ENI for that instance.
	// For example, for a m4.4xlarge node,
	//     if WARM-IP-TARGET is set to 1, and there are 9 pods running on the node, ipamD will try
	//     to make "warm pool" to have 10 IP address with 9 being assigned to Pod and 1 free IP.
	//
	//     if "WARM-IP-TARGET is not set, it will be defaulted to 30 (which the number of IPs per ENI). If there are 9 pods
	//     running on the node, ipamD will try to make "warm pool" to have 39 IPs with 9 being assigned to Pod and 30 free IPs.
	envWarmIPTarget = "WARM_IP_TARGET"
	noWarmIPTarget  = 0

	// This environment is used to specify the desired number of free ENIs along with all of its IP addresses
	// always available in "warm pool".
	// When it is not set, it is default to 1.
	//
	// when "WARM-IP-TARGET" is defined, ipamD will use behavior defined for "WARM-IP-TARGET".
	//
	// For example, for a m4.4xlarget node
	//     if WARM_ENI_TARGET is set to 2, and there are 9 pods running on the node, ipamD will try to
	//     make "warm  pool" to have 2 extra ENIs and its IP addresses,  in another word, 90 IP addresses with 9 IPs assigne to Pod
	//     and 81 free IPs.
	//
	//     if "WARM_ENI_TARGET" is not set, it is default to 1,  if there 9 pods running on the node, ipamD will try to
	//     make "warm pool" to have 1 extra ENI, in aother word 60 IPs with 9 being assigned to Pod and 51 free IPs.
	envWarmENITarget     = "WARM_ENI_TARGET"
	defaultWarmENITarget = 1

	// This environment is used to specify whether Pods need to use securitygroup and subnet defined in ENIConfig CRD
	// When it is NOT set or set to false, ipamD will use primary interface security group and subnet for Pod network.
	envCustomNetworkCfg = "AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG"
)

var (
	ipamdErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ipamd_error_count",
			Help: "The number of errors encountered in ipamd",
		},
		[]string{"fn", "error"},
	)
	ipamdActionsInprogress = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ipamd_action_inprogress",
			Help: "The number of ipamd actions inprogress",
		},
		[]string{"fn"},
	)
	enisMax = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "eni_max",
			Help: "The maximum number of ENIs that can be attached to the instance",
		},
	)
	ipMax = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ip_max",
			Help: "The maximum number of IP addresses that can be allocated to the instance",
		},
	)
	reconcileCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "reconcile_count",
			Help: "The number of times ipamD reconciles on ENIs and IP addresses",
		},
		[]string{"fn"},
	)
	addIPCnt = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "add_ip_req_count",
			Help: "The number of add IP address request",
		},
	)
	delIPCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "del_ip_req_count",
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
	// when ipamD receives AttachmentLimitExceeded error
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
		return nil, errors.Wrap(err, "ipamD: can not initialize with AWS SDK interface")
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
	maxENIs, err := c.awsClient.GetENILimit()
	if err == nil {
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

	err = c.networkClient.SetupHostNetwork(vpcCIDR, &primaryIP)
	if err != nil {
		log.Error("Failed to setup host network", err)
		return errors.Wrap(err, "ipamd init: failed to setup host network")
	}

	c.dataStore = datastore.NewDataStore()

	for _, eni := range enis {
		log.Debugf("Discovered ENI %s", eni.ENIID)

		err = c.setupENI(eni.ENIID, eni)
		if err != nil {
			log.Errorf("Failed to setup eni %s network: %v", eni.ENIID, err)
			return errors.Wrapf(err, "Failed to setup eni %v", eni.ENIID)
		}
	}

	usedIPs, err := c.getLocalPodsWithRetry()
	if err != nil {
		log.Warnf("During ipamd init, failed to get Pod information from Kubelet %v", err)
		ipamdErrInc("nodeInitK8SGetLocalPodIPsFailed", err)
		// This can happens when L-IPAMD starts before kubelet.
		// TODO  need to add node health stats here
		return nil
	}

	for _, ip := range usedIPs {
		if ip.Container == "" {
			log.Infof("Skipping Pod %s, Namespace %s due to no matching container",
				ip.Name, ip.Namespace)
			continue
		}
		if ip.IP == "" {
			log.Infof("Skipping Pod %s, Namespace %s due to no IP",
				ip.Name, ip.Namespace)
			continue
		}
		log.Infof("Recovered AddNetwork for Pod %s, Namespace %s, Container %s",
			ip.Name, ip.Namespace, ip.Container)
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

// StartNodeIPPoolManager monitors the IP Pool, add or del them when it is required.
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
	eni := c.dataStore.GetENINeedsIP(maxIPLimit, useCustomNetworkCfg())
	if eni != nil {
		log.Debugf("Attempt again to allocate IP address for eni :%s", eni.ID)
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
	eni, err := c.dataStore.FreeENI()
	if err != nil {
		ipamdErrInc("decreaseIPPoolFreeENIFailed", err)
		log.Errorf("Failed to decrease pool %v", err)
		return
	}
	log.Debugf("Start freeing eni %s", eni)
	c.awsClient.FreeENI(eni)
	c.lastNodeIPPoolAction = time.Now()
	total, used := c.dataStore.GetStats()
	log.Debugf("Successfully decreased IP Pool")
	logPoolStats(int64(total), int64(used), c.currentMaxAddrsPerENI, c.maxAddrsPerENI)
}

func isAttachmentLimitExceededError(err error) bool {
	return strings.Contains(err.Error(), "AttachmentLimitExceeded")
}

func (c *IPAMContext) increaseIPPool() {
	log.Debug("Start increasing IP Pool size")
	ipamdActionsInprogress.WithLabelValues("increaseIPPool").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("increaseIPPool").Sub(float64(1))

	curIPTarget, warmIPTargetDefined := c.getCurWarmIPTarget()
	if warmIPTargetDefined && curIPTarget <= 0 {
		log.Debugf("Skipping increase IP Pool, warm IP target reached")
		return
	}

	maxENIs, err := c.awsClient.GetENILimit()
	enisMax.Set(float64(maxENIs))

	if err == nil && maxENIs == c.dataStore.GetENIs() {
		log.Debugf("Skipping increase IPPOOL due to max ENI already attached to the instance : %d", maxENIs)
		return
	}
	if (c.maxENI > 0) && (c.maxENI == c.dataStore.GetENIs()) {
		if c.maxENI < maxENIs {
			errString := "desired: " + strconv.FormatInt(int64(maxENIs), 10) + "current: " + strconv.FormatInt(int64(c.maxENI), 10)
			ipamdErrInc("unExpectedMaxENIAttached", errors.New(errString))
		}
		log.Debugf("Skipping increase IPPOOL due to max ENI already attached to the instance : %d", c.maxENI)
		return
	}

	var securityGroups []*string
	var subnet string
	customNetworkCfg := useCustomNetworkCfg()

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
		// continue to proecsses those allocated ip addresses
		ipamdErrInc("increaseIPPoolAllocAllIPAddressFailed", err)
	}

	eniMetadata, err := c.waitENIAttached(eni)
	if err != nil {
		ipamdErrInc("increaseIPPoolwaitENIAttachedFailed", err)
		log.Errorf("Failed to increase pool size: not able to discover attached eni from metadata service %v", err)
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
	log.Debugf("Successfully increased IP Pool")
	logPoolStats(int64(total), int64(used), c.currentMaxAddrsPerENI, c.maxAddrsPerENI)
}

// setupENI does following:
// 1) add ENI to datastore
// 2) add all ENI's secondary IP addresses to datastore
// 3) setup linux eni related networking stack.
func (c *IPAMContext) setupENI(eni string, eniMetadata awsutils.ENIMetadata) error {
	// Have discovered the attached ENI from metadata service
	// add eni's IP to IP pool
	err := c.dataStore.AddENI(eni, int(eniMetadata.DeviceNumber), (eni == c.awsClient.GetPrimaryENI()))
	if err != nil && err.Error() != datastore.DuplicatedENIError {
		return errors.Wrapf(err, "failed to add eni %s to data store", eni)
	}

	ec2Addrs, eniPrimaryIP, err := c.getENIaddresses(eni)
	if err != nil {
		return errors.Wrapf(err, "failed to retrieve eni %s ip addresses", eni)
	}

	c.currentMaxAddrsPerENI, err = c.awsClient.GetENIipLimit()

	if err != nil {
		// if the instance type is not supported in ipamD and the GetENIipLimit() call returns an error
		// the code here fallbacks to use the number of IPs discovered on the ENI.
		// note: the number of IP discovered on the ENI at a time can be less than the number of supported IPs on
		// an ENI, for example:  ipamD has NOT allocated all IPs on the ENI yet.
		c.currentMaxAddrsPerENI = int64(len(ec2Addrs))
	}
	if c.currentMaxAddrsPerENI > c.maxAddrsPerENI {
		c.maxAddrsPerENI = c.currentMaxAddrsPerENI
	}

	if eni != c.awsClient.GetPrimaryENI() {
		err = c.networkClient.SetupENINetwork(eniPrimaryIP, eniMetadata.MAC,
			int(eniMetadata.DeviceNumber), eniMetadata.SubnetIPv4CIDR)
		if err != nil {
			return errors.Wrapf(err, "failed to setup eni %s network", eni)
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
			log.Warnf("Failed to increase ip pool, failed to add ip %s to data store", ec2Addr.PrivateIpAddress)
			// continue to add next address
			// TODO need to add health stats for err
			ipamdErrInc("addENIaddressesToDataStoreAddENIIPv4AddressFailed", err)
		}
	}

	return primaryIP
}

// returns all addresses on eni, the primary address on eni, error
func (c *IPAMContext) getENIaddresses(eni string) ([]*ec2.NetworkInterfacePrivateIpAddress, string, error) {
	ec2Addrs, _, err := c.awsClient.DescribeENI(eni)
	if err != nil {
		return nil, "", errors.Wrapf(err, "fail to find eni addresses for eni %s", eni)
	}

	for _, ec2Addr := range ec2Addrs {
		if aws.BoolValue(ec2Addr.Primary) {
			eniPrimaryIP := aws.StringValue(ec2Addr.PrivateIpAddress)
			return ec2Addrs, eniPrimaryIP, nil
		}
	}

	return nil, "", errors.Wrapf(err, "faind to find eni's primary address for eni %s", eni)
}

func (c *IPAMContext) waitENIAttached(eni string) (awsutils.ENIMetadata, error) {
	// wait till eni is showup in the instance meta data service
	retry := 0
	for {
		enis, err := c.awsClient.GetAttachedENIs()
		if err != nil {
			log.Warnf("Failed to increase pool, error trying to discover attached enis: %v ", err)
			time.Sleep(eniAttachTime)
			continue
		}

		// verify eni is in the returned eni list
		for _, returnedENI := range enis {
			if eni == returnedENI.ENIID {
				return returnedENI, nil
			}
		}

		retry++
		if retry > maxRetryCheckENI {
			log.Errorf("Unable to discover attached ENI from metadata service")
			// TODO need to add health stats
			ipamdErrInc("waitENIAttachedMaxRetryExceeded", err)
			return awsutils.ENIMetadata{}, errors.New("add eni: not able to retrieve eni from metata service")
		}
		log.Debugf("Not able to discover attached eni yet (attempt %d/%d)", retry, maxRetryCheckENI)

		time.Sleep(eniAttachTime)
	}
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

//nodeIPPoolTooLow returns true if IP pool is below low threshold
func (c *IPAMContext) nodeIPPoolTooLow() bool {
	curIPTarget, warmIPTargetDefined := c.getCurWarmIPTarget()
	if warmIPTargetDefined && curIPTarget <= 0 {
		return false
	}

	if warmIPTargetDefined && curIPTarget > 0 {
		return true
	}

	// if WARM-IP-TARGET not defined fallback using number of ENIs
	warmENITarget := getWarmENITarget()
	total, used := c.dataStore.GetStats()
	logPoolStats(int64(total), int64(used), c.currentMaxAddrsPerENI, c.maxAddrsPerENI)

	available := total - used
	return (int64(available) < c.maxAddrsPerENI*int64(warmENITarget))
}

// NodeIPPoolTooHigh returns true if IP pool is above high threshold
func (c *IPAMContext) nodeIPPoolTooHigh() bool {
	warmENITarget := getWarmENITarget()
	total, used := c.dataStore.GetStats()
	logPoolStats(int64(total), int64(used), c.currentMaxAddrsPerENI, c.maxAddrsPerENI)

	available := total - used

	target := getWarmIPTarget()
	if int64(target) != noWarmIPTarget && int64(target) >= int64(available) {
		return false
	}

	return (int64(available) >= (int64(warmENITarget)+1)*c.maxAddrsPerENI)
}

func ipamdErrInc(fn string, err error) {
	ipamdErr.With(prometheus.Labels{"fn": fn, "error": err.Error()}).Inc()
}

func ipamdActionsInprogressSet(fn string, curNum int) {
	ipamdActionsInprogress.WithLabelValues(fn).Set(float64(curNum))
}

// nodeIPPoolReconcile reconcile ENI and IP info from metadata service and IP addresses in datastore
func (c *IPAMContext) nodeIPPoolReconcile(interval time.Duration) error {
	ipamdActionsInprogress.WithLabelValues("nodeIPPoolReconcile").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("nodeIPPoolReconcile").Sub(float64(1))

	curTime := time.Now()
	last := c.lastNodeIPPoolAction

	if curTime.Sub(last) <= interval {
		return nil
	}

	log.Debug("Reconciling ENI/IP pool info...")
	attachedENIs, err := c.awsClient.GetAttachedENIs()

	if err != nil {
		log.Error("ip pool reconcile: Failed to get attached eni info", err.Error())
		ipamdErrInc("reconcileFailedGetENIs", err)
		return errors.Wrap(err, "ip pool reconcile: failed to get attached eni")
	}

	curENIs := c.dataStore.GetENIInfos()

	// mark phase
	for _, attachedENI := range attachedENIs {
		eniIPPool, err := c.dataStore.GetENIIPPools(attachedENI.ENIID)

		if err == nil {
			log.Debugf("Reconcile existing ENI %s IP pool", attachedENI.ENIID)
			// reconcile IP pool
			c.eniIPPoolReconcile(eniIPPool, attachedENI, attachedENI.ENIID)

			// mark action= remove this eni from curENIs list
			delete(curENIs.ENIIPPools, attachedENI.ENIID)
			continue
		}

		// add new ENI
		log.Debugf("Reconcile and add a new eni %s", attachedENI)
		err = c.setupENI(attachedENI.ENIID, attachedENI)
		if err != nil {
			log.Errorf("ip pool reconcile: Failed to setup eni %s network: %v", attachedENI.ENIID, err)
			ipamdErrInc("eniReconcileAdd", err)
			//continue if having trouble with ONLY 1 eni, instead of bailout here?
			continue
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniReconcileAdd"}).Inc()

	}

	// sweep phase: since the marked eni have been removed, the remaining ones needs to be sweeped
	for eni := range curENIs.ENIIPPools {
		log.Infof("Reconcile and delete detached eni %s", eni)
		err = c.dataStore.DeleteENI(eni)
		if err != nil {
			log.Errorf("ip pool reconcile: Failed to delete ENI during reconcile: %v", err)
			ipamdErrInc("eniReconcileDel", err)
			continue
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniReconcileDel"}).Inc()
	}
	log.Debug("Successfully Reconciled ENI/IP pool")
	c.lastNodeIPPoolAction = curTime
	return nil
}

func (c *IPAMContext) eniIPPoolReconcile(ipPool map[string]*datastore.AddressInfo, attachENI awsutils.ENIMetadata, eni string) error {

	for _, localIP := range attachENI.LocalIPv4s {
		if localIP == c.primaryIP[eni] {
			log.Debugf("Reconcile and skip primary IP %s on eni %s", localIP, eni)
			continue
		}

		err := c.dataStore.AddENIIPv4Address(eni, localIP)

		if err != nil && err.Error() == datastore.DuplicateIPError {
			log.Debugf("Reconciled IP %s on eni %s", localIP, eni)
			// mark action = remove it from eniPool
			delete(ipPool, localIP)
			continue
		}

		if err != nil {
			log.Errorf("Failed to reconcile IP %s on eni %s", localIP, eni)
			ipamdErrInc("ipReconcileAdd", err)
			// continue instead of bailout due to one ip
			continue
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniIPPoolReconcileAdd"}).Inc()

	}

	// sweep phase, delete remaining IPs

	for existingIP := range ipPool {
		log.Debugf("Reconcile and delete ip %s on eni %s", existingIP, eni)
		err := c.dataStore.DelENIIPv4Address(eni, existingIP)
		if err != nil {
			log.Errorf("Failed to reconcile and delete IP %s on eni %s, %v", existingIP, eni, err)
			ipamdErrInc("ipReconcileDel", err)
			// continue instead of bailout due to one ip
			continue
		}
		reconcileCnt.With(prometheus.Labels{"fn": "eniIPPoolReconcileDel"}).Inc()
	}

	return nil

}

func useCustomNetworkCfg() bool {
	defaultValue := false
	if strValue := os.Getenv(envCustomNetworkCfg); strValue != "" {
		parsedValue, err := strconv.ParseBool(strValue)
		if err != nil {
			log.Error("Failed to parse "+envCustomNetworkCfg+"; using default: "+fmt.Sprint(defaultValue), err.Error())
			return defaultValue
		}
		return parsedValue
	}
	return defaultValue
}

func getWarmIPTarget() int {
	inputStr, found := os.LookupEnv(envWarmIPTarget)

	if !found {
		return noWarmIPTarget
	}

	if input, err := strconv.Atoi(inputStr); err == nil {
		if input < 0 {
			return noWarmIPTarget
		}
		log.Debugf("Using WARM-IP-TARGET %v", input)
		return input
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
	log.Debugf("Current warm IP stats: target: %d, total: %d, used: %d",
		target, total, used)
	curTarget := int64(target) - int64(total-used)

	return curTarget, true
}

// GetConfigForDebug returns the active values of the configuration env vars (for debugging purposes).
func GetConfigForDebug() map[string]interface{} {
	return map[string]interface{}{
		envWarmIPTarget:     getWarmIPTarget(),
		envWarmENITarget:    getWarmENITarget(),
		envCustomNetworkCfg: useCustomNetworkCfg(),
	}
}
