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
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
)

// Package ipamd is a long running daemon which manages a warn-pool of available IP addresses.
// It also monitors the size of the pool, dynamically allocate more ENIs when the pool size goes below threshold and
// free them back when the pool size goes above max threshold.

const (
	ipPoolMonitorInterval = 5 * time.Second
	maxRetryCheckENI      = 5
	eniAttachTime         = 10 * time.Second
	defaultWarmENITarget  = 1
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
	prometheusRegistered = false
)

// IPAMContext contains node level control information
type IPAMContext struct {
	awsClient     awsutils.APIs
	dataStore     *datastore.DataStore
	k8sClient     k8sapi.K8SAPIs
	networkClient networkutils.NetworkAPIs

	currentMaxAddrsPerENI int
	maxAddrsPerENI        int
	// maxENI indicate the maximum number of ENIs can be attached to the instance
	// It is initialized to 0 and it is set to current number of ENIs attached
	// when ipamD receives AttachmentLimitExceeded error
	maxENI int
}

func prometheusRegister() {
	if !prometheusRegistered {
		prometheus.MustRegister(ipamdErr)
		prometheus.MustRegister(ipamdActionsInprogress)
		prometheus.MustRegister(enisMax)
		prometheus.MustRegister(ipMax)
		prometheusRegistered = true
	}
}

// New retrieves IP address usage information from Instance MetaData service and Kubelet
// then initializes IP address pool data store
func New() (*IPAMContext, error) {
	prometheusRegister()
	c := &IPAMContext{}

	c.k8sClient = k8sapi.New()
	c.networkClient = networkutils.New()

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
	enis, err := c.awsClient.GetAttachedENIs()
	if err != nil {
		log.Error("Failed to retrive ENI info")
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

		err = c.awsClient.AllocAllIPAddress(eni.ENIID)
		if err != nil {
			ipamdErrInc("nodeInitAllocAllIPAddressFailed", err)
			//TODO need to increment ipamd err stats
			log.Warn("During ipamd init:  error encountered on trying to allocate all available IP addresses", err)
			// fall though to add those allocated OK addresses
		}

		err = c.setupENI(eni.ENIID, eni)
		if err != nil {
			log.Errorf("Failed to setup eni %s network: %v", eni.ENIID, err)
			return errors.Wrapf(err, "Failed to setup eni %v", eni.ENIID)
		}
	}

	usedIPs, err := c.k8sClient.K8SGetLocalPodIPs(c.awsClient.GetLocalIPv4())
	if err != nil {
		log.Warnf("During ipamd init, failed to get Pod information from Kubelet %v", err)
		ipamdErrInc("nodeInitK8SGetLocalPodIPsFailed", err)
		// This can happens when L-IPAMD starts before kubelet.
		// TODO  need to add node health stats here
		return nil
	}

	for _, ip := range usedIPs {
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

// StartNodeIPPoolManager monitors the IP Pool, add or del them when it is required.
func (c *IPAMContext) StartNodeIPPoolManager() {
	for {
		time.Sleep(ipPoolMonitorInterval)
		c.updateIPPoolIfRequired()
	}
}

func (c *IPAMContext) updateIPPoolIfRequired() {
	c.reconcileENIIP()
	if c.nodeIPPoolTooLow() {
		c.increaseIPPool()
	} else if c.nodeIPPoolTooHigh() {
		c.decreaseIPPool()
	}
}

func (c *IPAMContext) reconcileENIIP() {
	ipamdActionsInprogress.WithLabelValues("reconcileENIip").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("reconcileENIip").Sub(float64(1))
	maxIPLimit, err := c.awsClient.GetENIipLimit()
	if err != nil {
		log.Infof("Failed to retrieve ENI IP limit: %v", err)
		return
	}
	eni := c.dataStore.GetENINeedsIP(maxIPLimit)
	if eni != nil {
		log.Debugf("Attempt again to allocate IP address for eni :%s", eni.Id)
		err := c.awsClient.AllocAllIPAddress(eni.Id)
		if err != nil {
			ipamdErrInc("reconcileENIIPAllocAllIPAddressFailed", err)
			log.Warn("During eni repair: error encountered on allocate IP address", err)
			return
		}
		ec2Addrs, _, err := c.getENIaddresses(eni.Id)
		if err != nil {
			ipamdErrInc("reconcileENIIPgetENIaddressesFailed", err)
			log.Warn("During eni repair: failed to get ENI ip addresses", err)
			return
		}
		c.addENIaddressesToDataStore(ec2Addrs, eni.Id)
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
	total, used := c.dataStore.GetStats()
	log.Debugf("Successfully decreased IP Pool")
	logPoolStats(total, used, c.currentMaxAddrsPerENI, c.maxAddrsPerENI)
}

func isAttachmentLimitExceededError(err error) bool {
	return strings.Contains(err.Error(), "AttachmentLimitExceeded")
}

func (c *IPAMContext) increaseIPPool() {
	log.Debug("Start increasing IP Pool size")
	ipamdActionsInprogress.WithLabelValues("increaseIPPool").Add(float64(1))
	defer ipamdActionsInprogress.WithLabelValues("increaseIPPool").Sub(float64(1))
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
	eni, err := c.awsClient.AllocENI()
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

	err = c.awsClient.AllocAllIPAddress(eni)
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
	total, used := c.dataStore.GetStats()
	log.Debugf("Successfully increased IP Pool")
	logPoolStats(total, used, c.currentMaxAddrsPerENI, c.maxAddrsPerENI)
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

	c.currentMaxAddrsPerENI = len(ec2Addrs)
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

	c.addENIaddressesToDataStore(ec2Addrs, eni)

	return nil

}

func (c *IPAMContext) addENIaddressesToDataStore(ec2Addrs []*ec2.NetworkInterfacePrivateIpAddress, eni string) {
	for _, ec2Addr := range ec2Addrs {
		if aws.BoolValue(ec2Addr.Primary) {
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
}

// returns all addresses on eni, the primary adderss on eni, error
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
	inputStr, found := os.LookupEnv("WARM_ENI_TARGET")

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

func logPoolStats(total, used, currentMaxAddrsPerENI, maxAddrsPerENI int) {
	log.Debugf("IP pool stats: total = %d, used = %d, c.currentMaxAddrsPerENI = %d, c.maxAddrsPerENI = %d",
		total, used, currentMaxAddrsPerENI, maxAddrsPerENI)
}

//nodeIPPoolTooLow returns true if IP pool is below low threshhold
func (c *IPAMContext) nodeIPPoolTooLow() bool {
	warmENITarget := getWarmENITarget()
	total, used := c.dataStore.GetStats()
	logPoolStats(total, used, c.currentMaxAddrsPerENI, c.maxAddrsPerENI)

	available := total - used
	return (available <= c.currentMaxAddrsPerENI*warmENITarget)
}

// NodeIPPoolTooHigh returns true if IP pool is above high threshhold
func (c *IPAMContext) nodeIPPoolTooHigh() bool {
	warmENITarget := getWarmENITarget()
	total, used := c.dataStore.GetStats()
	logPoolStats(total, used, c.currentMaxAddrsPerENI, c.maxAddrsPerENI)

	available := total - used
	return (available > (warmENITarget+1)*c.currentMaxAddrsPerENI)

}

func ipamdErrInc(fn string, err error) {
	ipamdErr.With(prometheus.Labels{"fn": fn, "error": err.Error()}).Inc()
}

func ipamdActionsInprogressSet(fn string, curNum int) {
	ipamdActionsInprogress.WithLabelValues(fn).Set(float64(curNum))
}
