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

// Package ipamd is a long running daemon which manages a warn-pool of available IP addresses.
// It also monitors the size of the pool, dynamically allocate more ENIs when the pool size goes below threshold and
// free them back when the pool size goes above max threshold.
package ipamd

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/ipamd/eni"
	"github.com/aws/amazon-vpc-cni-k8s/ipamd/metrics"
	"github.com/aws/amazon-vpc-cni-k8s/ipamd/network"
)

const (
	ipPoolMonitorInterval = 5 * time.Second
	maxRetryCheckENI      = 5
	eniAttachTime         = 10 * time.Second
)

// IPAMD contains node level control information
type IPAMD struct {
	awsClient     eni.ENIService
	dataStore     datastore.Datastore
	networkClient network.Network
	podClient     getter

	// maxENI indicate the maximum number of ENIs can be attached to the instance
	// It is initialized to 0 and it is set to current number of ENIs attached
	// when ipamD receives AttachmentLimitExceeded error
	maxENI int

	metrics *metrics.Metrics
	started bool
	inited  chan bool
}

// New retrieves IP address usage information from Instance MetaData service and Kubelet
// then initializes IP address pool data store
func New() (*IPAMD, error) {
	i := &IPAMD{
		networkClient: network.NewLinuxNetwork(),
		podClient:     http.DefaultClient,
		inited:        make(chan bool),
	}
	m, err := metrics.New()
	if err != nil {
		return nil, err
	}
	i.metrics = m

	region, instanceID, instanceType, err := eni.GetMetadata()
	if err != nil {
		log.Errorf("Failed to initialize awsutil interface %v", err)
		return nil, errors.Wrap(err, "ipamd: can not initialize with AWS SDK interface")
	}
	client, err := eni.NewEC2Instance(region, instanceID, instanceType, m)
	if err != nil {
		log.Errorf("Failed to initialize awsutil interface %v", err)
		return nil, errors.Wrap(err, "ipamd: can not initialize with AWS SDK interface")
	}

	i.awsClient = client
	i.dataStore = datastore.NewDatastore(m)
	err = i.init(context.Background())
	if err != nil {
		return nil, err
	}
	i.started = true
	return i, nil
}

//TODO(aws): need to break this function down(comments from CR)
func (i *IPAMD) init(ctx context.Context) error {
	i.metrics.Reset()
	cidr, localIP, _, err := i.awsClient.GetPrimaryDetails(ctx)
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
	if err = i.networkClient.SetupHostNetwork(vpcCIDR, &primaryIP); err != nil {
		log.Error("Failed to setup host network", err)
		return errors.Wrap(err, "ipamd init: failed to setup host network")
	}

	enis, err := i.awsClient.GetENIs(ctx)
	if err != nil {
		log.Error("Failed to retrive ENI info")
		return errors.New("ipamd init: failed to retrieve attached ENIs info")
	}

	for _, eni := range enis {
		log.Debugf("Discovered ENI %s", eni)
		if err := i.setupENI(ctx, eni); err != nil {
			log.Errorf("Failed to setup eni %s network: %v", eni, err)
			return errors.Wrapf(err, "Failed to setup eni %v", eni)
		}
	}

	usedIPs, err := k8sGetLocalPodIPs(i.podClient, localIP)
	if err != nil {
		log.Warnf("ipamd init: failed to get Pod information from Kubelet %v", err)
		// This can happens when L-IPAMD starts before kubelet.
		// We are going to error out and let liveness probe restart the daemon.
		return errors.Wrapf(err, "failed to get Pod information from Kubelet %v", err)
	}

	for _, ip := range usedIPs {
		if err := i.dataStore.ReconstructPodIP(ip.Name, ip.Namespace, ip.IP); err != nil {
			// TODO
			log.Warnf("ipamd init: failed to reconstruct pod %v %v %v : %v", ip.Name, ip.Namespace, ip.IP, err)
		}
	}

	// TODO(aws): continue, but need to add node health stats here
	// TODO(aws): need to feed this to controller on the health of pod and node
	// This is a bug among kubelet/cni-plugin/l-ipamd/ec2-metadata that this particular pod is using an non existent ip address.
	// Here we choose to continue instead of returning error and EXIT out L-IPAMD(exit L-IPAMD will make whole node out)
	// The plan(TODO) is to feed this info back to controller and let controller cleanup this pod from this node.

	return nil
}

// StartNodeIPPoolManager monitors the IP Pool, add or del them when it is required.
func (i *IPAMD) StartNodeIPPoolManager() {
	i.inited <- true
	for {
		time.Sleep(ipPoolMonitorInterval)
		i.updateIPPoolIfRequired(context.Background())
	}
}

func (i *IPAMD) updateIPPoolIfRequired(ctx context.Context) {
	total, used, _ := i.dataStore.GetStats()
	currentMaxAddrsPerENI := int(i.awsClient.GetMaxIPs())
	if (total - used) <= currentMaxAddrsPerENI {
		log.Infof("Increasing IPPool: total %v used %v currentMaxAddrsPerENI %v", total, used, currentMaxAddrsPerENI)
		i.increaseIPPool(ctx)
	} else if total-used > 2*currentMaxAddrsPerENI {
		log.Infof("Decreasing IPPool: total %v used %v currentMaxAddrsPerENI %v", total, used, currentMaxAddrsPerENI)
		i.decreaseIPPool(ctx)
	}
}

func isAttachmentLimitExceededError(err error) bool {
	return strings.Contains(err.Error(), "AttachmentLimitExceeded")
}

func (i *IPAMD) increaseIPPool(ctx context.Context) {
	eni, runOut, err := i.awsClient.AddENI(ctx)
	if err != nil {
		log.Errorf("Failed to increase pool size due to not able to allocate ENI %v", err)
		// TODO(aws): need to add health stats
		// TODO(tvi): Fix isAttachmentLimitExceededError?
		return
	}
	if runOut {
		log.Info("Run out of enis to allocate")
		return
	}

	if err := i.setupENI(ctx, eni); err != nil {
		log.Errorf("Failed to increase pool size: %v", err)
		return
	}
}

func (i *IPAMD) decreaseIPPool(ctx context.Context) {
	eni, err := i.dataStore.FreeENI()
	if err != nil {
		log.Debugf("Failed to decrease pool %v", err)
		return
	}
	if err = i.awsClient.FreeENI(ctx, eni); err != nil {
		log.Errorf("Failed to free eni: %v", err)
		// TODO(tvi): This should not happen. Maybe panic and get restarted?
		log.Warningf("Leaking eni")
	}
}

// setupENI does following:
// 1) add ENI to datastore
// 2) add all ENI's secondary IP addresses to datastore
// 3) setup linux eni related networking stack.
func (i *IPAMD) setupENI(ctx context.Context, eni string) error {
	// wait till eni is showup in the instance meta data service
	for retry := 0; retry < maxRetryCheckENI; retry++ {
		ok, err := i.awsClient.IsENIReady(ctx, eni)
		if err != nil {
			return errors.Wrapf(err, "failed to retrieve eni %s readyness", eni)
		}
		if !ok {
			time.Sleep(eniAttachTime)
			continue
		}

		eniMetadata, err := i.awsClient.GetENIMetadata(ctx, eni)
		if err != nil {
			return errors.Wrapf(err, "failed to retrieve eni %s ip addresses", eni)
		}

		_, _, primaryENI, err := i.awsClient.GetPrimaryDetails(ctx)
		if err != nil {
			return errors.Wrapf(err, "failed to retrieve eni %s ip addresses", eni)
		}
		// Have discovered the attached ENI from metadata service add eni's IP to IP pool.
		err = i.dataStore.AddENI(eni, int(eniMetadata.Device), (eni == primaryENI))
		if err != nil && err != datastore.ErrDuplicateENI {
			return errors.Wrapf(err, "failed to add eni %s to data store", eni)
		}

		if eni != primaryENI {
			if err := i.networkClient.SetupENINetwork(
				eniMetadata.PrimaryIP,
				eniMetadata.MAC,
				int(eniMetadata.Device),
				eniMetadata.CIDR); err != nil {
				return errors.Wrapf(err, "failed to setup eni %s network", eni)
			}
		}

		for _, ec2Addr := range eniMetadata.SecondaryIPs {
			err := i.dataStore.AddIPAddr(eni, ec2Addr)
			if err != nil && err != datastore.ErrDuplicateIP {
				log.Warnf("Failed to increase ip pool, failed to add ip %s to data store", ec2Addr)
				// continue to add next address
				// TODO(aws): need to add health stats for err
			}
		}
		break
	}
	return nil
}
