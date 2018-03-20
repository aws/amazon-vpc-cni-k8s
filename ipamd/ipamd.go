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
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	log "github.com/cihub/seelog"
	"github.com/pkg/errors"

	"github.com/aws/aws-sdk-go/aws"

	"github.com/aws/aws-sdk-go/service/ec2"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/ipamd/eni"
	"github.com/aws/amazon-vpc-cni-k8s/ipamd/network"
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
	awsClient     eni.ENIService
	dataStore     datastore.Datastore
	networkClient network.Network
	podClient     getter

	currentMaxAddrsPerENI int
	maxAddrsPerENI        int
	// maxENI indicate the maximum number of ENIs can be attached to the instance
	// It is initialized to 0 and it is set to current number of ENIs attached
	// when ipamD receives AttachmentLimitExceeded error
	maxENI int
}

// New retrieves IP address usage information from Instance MetaData service and Kubelet
// then initializes IP address pool data store
func New() (*IPAMContext, error) {
	c := &IPAMContext{
		podClient: http.DefaultClient,
	}

	c.networkClient = network.NewLinuxNetwork()
	region, instanceID, instanceType, err := eni.GetMetadata()
	if err != nil {
		log.Errorf("Failed to initialize awsutil interface %v", err)
		return nil, errors.Wrap(err, "ipamd: can not initialize with AWS SDK interface")
	}
	client, err := eni.NewEC2Instance(region, instanceID, instanceType)
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
	cidr, localIP, _, err := c.awsClient.GetPrimaryDetails(context.Background())
	if err != nil {
		log.Error("Failed to parse GetPrimaryDetails", err.Error())
		return errors.Wrap(err, "ipamd init: failed to get VPC CIDR")
	}

	_, vpcCIDR, err := net.ParseCIDR(cidr)
	if err != nil {
		log.Error("Failed to parse VPC IPv4 CIDR", err.Error())
		return errors.Wrap(err, "ipamd init: failed to retrieve VPC CIDR")
	}

	primaryIP := net.ParseIP(localIP)

	enis, err := c.awsClient.GetENIs(context.Background())
	if err != nil {
		log.Error("Failed to retrive ENI info")
		return errors.New("ipamd init: failed to retrieve attached ENIs info")
	}

	err = c.networkClient.SetupHostNetwork(vpcCIDR, &primaryIP)
	if err != nil {
		log.Error("Failed to setup host network", err)
		return errors.Wrap(err, "ipamd init: failed to setup host network")
	}

	c.dataStore = datastore.NewDatastore()

	for _, eniID := range enis {
		log.Debugf("Discovered ENI %s", eniID)
		err = c.setupENI(eniID, eni.ENIMetadata{})
		if err != nil {
			log.Errorf("Failed to setup eni %s network: %v", eniID, err)
			return errors.Wrapf(err, "Failed to setup eni %v", eniID)
		}
	}

	usedIPs, err := k8sGetLocalPodIPs(c.podClient, localIP)
	if err != nil {
		log.Warnf("During ipamd init, failed to get Pod information from Kubelet %v", err)
		// This can happens when L-IPAMD starts before kubelet.
		// TODO  need to add node health stats here
		return nil
	}

	for _, ip := range usedIPs {
		err = c.dataStore.ReconstructPodIP(ip.Name, ip.Namespace, ip.IP)
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
	c.awsClient.FreeENI(context.Background(), eni)
}

func isAttachmentLimitExceededError(err error) bool {
	return strings.Contains(err.Error(), "AttachmentLimitExceeded")
}

func (c *IPAMContext) increaseIPPool() {

	// if (c.maxENI > 0) && (c.maxENI == c.dataStore.GetENIs()) {
	// 	log.Debugf("Skipping increase IPPOOL due to max ENI already attached to the instance : %d", c.maxENI)
	// 	return
	// }
	eni, _, err := c.awsClient.AddENI(context.Background())
	if err != nil {
		log.Errorf("Failed to increase pool size due to not able to allocate ENI %v", err)

		if isAttachmentLimitExceededError(err) {
			// c.maxENI = c.dataStore.GetENIs()
			log.Infof("Discovered the instance max ENI allowed is: %d", c.maxENI)
		}
		// TODO need to add health stats
		return
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
func (c *IPAMContext) setupENI(eni string, eniMetadata eni.ENIMetadata) error {
	_, _, primaryENI, err := c.awsClient.GetPrimaryDetails(context.Background())
	if err != nil {
		return errors.Wrapf(err, "failed to retrieve eni %s ip addresses", eni)
	}
	// Have discovered the attached ENI from metadata service
	// add eni's IP to IP pool
	err = c.dataStore.AddENI(eni, int(eniMetadata.Device), (eni == primaryENI))
	if err != nil && err != datastore.ErrDuplicateENI {
		return errors.Wrapf(err, "failed to add eni %s to data store", eni)
	}

	// ec2Addrs, eniPrimaryIP, err := c.getENIaddresses(eni)
	// if err != nil {
	// 	return errors.Wrapf(err, "failed to retrieve eni %s ip addresses", eni)
	// }

	// c.currentMaxAddrsPerENI = len(ec2Addrs)
	// if c.currentMaxAddrsPerENI > c.maxAddrsPerENI {
	// 	c.maxAddrsPerENI = c.currentMaxAddrsPerENI
	// }

	if eni != primaryENI {
		if err := c.networkClient.SetupENINetwork(
			eniMetadata.PrimaryIP,
			eniMetadata.MAC,
			int(eniMetadata.Device),
			eniMetadata.CIDR); err != nil {
			return errors.Wrapf(err, "failed to setup eni %s network", eni)
		}
	}

	for _, ec2Addr := range eniMetadata.SecondaryIPs {
		err := c.dataStore.AddIPAddr(eni, ec2Addr)
		if err != nil && err != datastore.ErrDuplicateIP {
			log.Warnf("Failed to increase ip pool, failed to add ip %s to data store", ec2Addr)
			// continue to add next address
			// TODO(aws): need to add health stats for err
		}
	}
	// c.addENIaddressesToDataStore(ec2Addrs, eni)

	return nil

}

func (c *IPAMContext) addENIaddressesToDataStore(ec2Addrs []*ec2.NetworkInterfacePrivateIpAddress, eni string) {
	for _, ec2Addr := range ec2Addrs {
		if aws.BoolValue(ec2Addr.Primary) {
			continue
		}
		err := c.dataStore.AddIPAddr(eni, aws.StringValue(ec2Addr.PrivateIpAddress))
		if err != nil && err != datastore.ErrDuplicateIP {
			log.Warnf("Failed to increase ip pool, failed to add ip %s to data store", ec2Addr.PrivateIpAddress)
			// continue to add next address
			// TODO need to add health stats for err
		}
	}
}

// returns all addresses on eni, the primary adderss on eni, error
// func (c *IPAMContext) getENIaddresses(eni string) ([]*ec2.NetworkInterfacePrivateIpAddress, string, error) {
// 	ec2Addrs, _, err := c.awsClient.DescribeENI(eni)
// 	if err != nil {
// 		return nil, "", errors.Wrapf(err, "fail to find eni addresses for eni %s", eni)
// 	}

// 	for _, ec2Addr := range ec2Addrs {
// 		if aws.BoolValue(ec2Addr.Primary) {
// 			eniPrimaryIP := aws.StringValue(ec2Addr.PrivateIpAddress)
// 			return ec2Addrs, eniPrimaryIP, nil
// 		}
// 	}

// 	return nil, "", errors.Wrapf(err, "faind to find eni's primary address for eni %s", eni)
// }

func (c *IPAMContext) waitENIAttached(eniID string) (eni.ENIMetadata, error) {
	// wait till eni is showup in the instance meta data service
	retry := 0
	for {
		retry++
		if retry > maxRetryCheckENI {
			log.Errorf("Unable to discover attached ENI from metadata service")
			// TODO need to add health stats
			return eni.ENIMetadata{}, errors.New("add eni: not able to retrieve eni from metata service")
		}
		ok, err := c.awsClient.IsENIReady(context.Background(), eniID)
		if err != nil {
			return eni.ENIMetadata{}, errors.Wrapf(err, "failed to retrieve eni %s readyness", eniID)
		}
		if !ok {
			log.Warnf("Failed to increase pool, error trying to discover attached enis: %v ", err)
			time.Sleep(eniAttachTime)
			continue
		}

		// verify eni is in the returned eni list
		// for _, returnedENI := range enis {
		// 	if eniID == returnedENI.ENIID {
		// 		return returnedENI, nil
		// 	}
		// }

		log.Debugf("Not able to discover attached eni yet (attempt %d/%d)", retry, maxRetryCheckENI)

		time.Sleep(eniAttachTime)
	}
}

//nodeIPPoolTooLow returns true if IP pool is below low threshhold
func (c *IPAMContext) nodeIPPoolTooLow() bool {
	total, used, _ := c.dataStore.GetStats()
	log.Debugf("IP pool stats: total=%d, used=%d, c.currentMaxAddrsPerENI =%d, c.maxAddrsPerENI = %d",
		total, used, c.currentMaxAddrsPerENI, c.maxAddrsPerENI)

	return ((total - used) <= c.currentMaxAddrsPerENI)
}

// NodeIPPoolTooHigh returns true if IP pool is above high threshhold
func (c *IPAMContext) nodeIPPoolTooHigh() bool {
	total, used, _ := c.dataStore.GetStats()

	log.Debugf("IP pool stats: total=%d, used=%d, c.currentMaxAddrsPerENI =%d, c.maxAddrsPerENI = %d",
		total, used, c.currentMaxAddrsPerENI, c.maxAddrsPerENI)

	return (total-used > 2*c.currentMaxAddrsPerENI)

}
