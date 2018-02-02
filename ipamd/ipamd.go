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
	"time"

	log "github.com/cihub/seelog"
	"github.com/pkg/errors"

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
)

// IPAMContext contains node level control information
type IPAMContext struct {
	awsClient     awsutils.APIs
	dataStore     *datastore.DataStore
	k8sClient     k8sapi.K8SAPIs
	networkClient networkutils.NetworkAPIs

	currentMaxAddrsPerENI int
	maxAddrsPerENI        int
	primaryIP             map[string]string
}

// New retrieves IP address usage information from Instance MetaData service and Kubelet
// then initializes IP address pool data store
func New() (*IPAMContext, error) {
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
	c.primaryIP = make(map[string]string)

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
		// This can happens when L-IPAMD starts before kubelet.
		// TODO  need to add node health stats here
		return nil
	}

	for _, ip := range usedIPs {
		_, _, err = c.dataStore.AssignPodIPv4Address(ip)
		if err != nil {
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
		// TODO what trigger nodeIPPoolReconcile()
		c.nodeIPPoolReconcile()
	}
}

func (c *IPAMContext) updateIPPoolIfRequired() {
	if c.nodeIPPoolTooLow() {
		c.increaseIPPool()
	} else if c.nodeIPPoolTooHigh() {
		c.decreaseIPPool()
	}
}

func (c *IPAMContext) decreaseIPPool() {
	eni, err := c.dataStore.FreeENI()
	if err != nil {
		log.Errorf("Failed to decrease pool %v", err)
		return
	}
	c.awsClient.FreeENI(eni)
}

func (c *IPAMContext) increaseIPPool() {
	eni, err := c.awsClient.AllocENI()
	if err != nil {
		log.Errorf("Failed to increase pool size due to not able to allocate ENI %v", err)
		// TODO need to add health stats
		return
	}

	err = c.awsClient.AllocAllIPAddress(eni)
	if err != nil {
		log.Warnf("Failed to allocate all available ip addresses on an ENI %v", err)
		// continue to proecsses those allocated ip addresses
	}

	eniMetadata, err := c.waitENIAttached(eni)
	if err != nil {
		log.Errorf("Failed to increase pool size: not able to discover attached eni from metadata service %v", err)
		return
	}

	err = c.setupENI(eni, eniMetadata)
	if err != nil {
		log.Errorf("Failed to increase pool size: %v", err)
		return
	}
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
		}
	}

	return primaryIP
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
		retry++
		if retry > maxRetryCheckENI {
			log.Errorf("Unable to discover attached ENI from metadata service")
			// TODO need to add health stats
			return awsutils.ENIMetadata{}, errors.New("add eni: not able to retrieve eni from metata service")
		}
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

		log.Debugf("Not able to discover attached eni yet (attempt %d/%d)", retry, maxRetryCheckENI)

		time.Sleep(eniAttachTime)
	}
}

//nodeIPPoolTooLow returns true if IP pool is below low threshhold
func (c *IPAMContext) nodeIPPoolTooLow() bool {
	total, used := c.dataStore.GetStats()
	log.Debugf("IP pool stats: total=%d, used=%d, c.currentMaxAddrsPerENI =%d, c.maxAddrsPerENI = %d",
		total, used, c.currentMaxAddrsPerENI, c.maxAddrsPerENI)

	return ((total - used) <= c.currentMaxAddrsPerENI)
}

// NodeIPPoolTooHigh returns true if IP pool is above high threshhold
func (c *IPAMContext) nodeIPPoolTooHigh() bool {
	total, used := c.dataStore.GetStats()

	log.Debugf("IP pool stats: total=%d, used=%d, c.currentMaxAddrsPerENI =%d, c.maxAddrsPerENI = %d",
		total, used, c.currentMaxAddrsPerENI, c.maxAddrsPerENI)

	return (total-used > 2*c.currentMaxAddrsPerENI)

}

// nodeIPPoolReconcile reconcile ENI and IP info from metadata service and IP addresses in datastore
func (c *IPAMContext) nodeIPPoolReconcile() error {
	log.Debug("Reconciling IP pool info...")
	attachedENIs, err := c.awsClient.GetAttachedENIs()

	if err != nil {
		log.Error("ip pool reconcile: Failed to get attached eni info", err.Error())
		errors.Wrap(err, "ip pool reconcile: failed to get attached eni")
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
			//TODO should continue if having trouble with ONLY 1 eni, instead of bailout here?
			return errors.Wrapf(err, "ip pool reconcile: failed to setup eni %v", attachedENI.ENIID)
		}

	}

	// sweep phase: since the marked eni have been removed, the remaining ones needs to be sweeped
	for eni := range curENIs.ENIIPPools {
		log.Infof("Reconcile and delete detached eni %s", eni)
		c.dataStore.DeleteENI(eni)
	}

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
			return errors.Wrapf(err, "ip pool reconcile: failed to reconcile ip %s on eni %s", localIP, eni)
		}

	}

	// sweep phase, delete remaining IPs

	for existingIP := range ipPool {
		log.Debugf("Reconcile and delete ip %s on eni %s", existingIP, eni)
		err := c.dataStore.DelENIIPv4Address(eni, existingIP)
		if err != nil {
			log.Errorf("Failed to reconcile and delete IP %s on eni %s, %v", existingIP, eni, err)
			//TODO should contine instead of bailout due to one ip ?
			return errors.Wrapf(err, "ip pool reconcile: failed to reconcile and delete IP %s on eni %s", existingIP, eni)
		}
	}

	return nil

}
