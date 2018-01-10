// +build linux

// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

// Package networkutils is a collection of helpers for eni/watcher
package networkutils

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/eni/netlinkwrapper"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	"github.com/pkg/errors"

	"github.com/cihub/seelog"
)

const (
	// macAddressBackoffMin specifies the mimimum duration for the backoff
	// when looking for an ENI's mac address on the host
	macAddressBackoffMin = 2 * time.Millisecond

	// macAddressBackoffMax specifies the maximum duration for the backoff
	// when looking for an ENI's mac address on the host
	macAddressBackoffMax = 200 * time.Millisecond

	// macAddressBackoffJitter specifies the jitter multiple percentage when
	// looking for an ENI's mac address on the host
	macAddressBackoffJitter = 0.2

	// macAddressBackoffMultiple specifies the backoff duration multiplier
	// when looking for an ENI's mac address on the host
	macAddressBackoffMultiple = 1.5
)

// macAddressRetriever is used to retrieve the mac address of a device. It collects
// all the information necessary to start this operation and stores the result in
// the 'macAddress' attribute
type macAddressRetriever struct {
	dev           string
	netlinkClient netlinkwrapper.NetLink
	macAddress    string
	// timeout specifies the timeout duration before giving up when
	// looking for an ENI's mac address on the host
	timeout time.Duration
	ctx     context.Context
}

// GetMACAddress retrieves the MAC address of a device using netlink
func GetMACAddress(ctx context.Context,
	timeout time.Duration,
	dev string,
	netlinkClient netlinkwrapper.NetLink) (string, error) {
	retriever := &macAddressRetriever{
		dev:           dev,
		netlinkClient: netlinkClient,
		ctx:           ctx,
		timeout:       timeout,
	}
	return retriever.retrieve()
}

// retrieve retrives the mac address of a network device. If the retrieved mac
// address is empty, it retries the operation with a timeout specified by the
// caller
func (retriever *macAddressRetriever) retrieve() (string, error) {
	backoff := utils.NewSimpleBackoff(macAddressBackoffMin, macAddressBackoffMax,
		macAddressBackoffJitter, macAddressBackoffMultiple)
	ctx, cancel := context.WithTimeout(retriever.ctx, retriever.timeout)
	defer cancel()

	err := utils.RetryWithBackoffCtx(ctx, backoff, func() error {
		retErr := retriever.retrieveOnce()
		if retErr != nil {
			seelog.Warnf("Unable to retrieve mac address for device '%s': %v",
				retriever.dev, retErr)
			return retErr
		}

		if retriever.macAddress == "" {
			seelog.Debugf("Empty mac address for device '%s'", retriever.dev)
			// Return a retriable error when mac address is empty. If the error
			// is not wrapped with the RetriableError interface, RetryWithBackoffCtx
			// treats them as retriable by default
			return errors.Errorf("eni mac address: retrieved empty address for device %s",
				retriever.dev)
		}

		return nil
	})
	if err != nil {
		return "", err
	}
	// RetryWithBackoffCtx returns nil when the context is cancelled. Check if there was
	// a timeout here. TODO: Fix RetryWithBackoffCtx to return ctx.Err() on context Done()
	if err = ctx.Err(); err != nil {
		return "", errors.Wrapf(err, "eni mac address: timed out waiting for eni device '%s'",
			retriever.dev)
	}

	return retriever.macAddress, nil
}

// retrieveOnce retrieves the MAC address of a device using netlink.LinkByName
func (retriever *macAddressRetriever) retrieveOnce() error {
	dev := filepath.Base(retriever.dev)
	link, err := retriever.netlinkClient.LinkByName(dev)
	if err != nil {
		return utils.NewRetriableError(utils.NewRetriable(false), err)
	}
	retriever.macAddress = link.Attrs().HardwareAddr.String()
	return nil
}

// IsValidNetworkDevice is used to differentiate virtual and physical devices
// Returns true only for pci or vif interfaces
func IsValidNetworkDevice(devicePath string) bool {
	/*
	* DevicePath Samples:
	* eth1 -> /devices/pci0000:00/0000:00:05.0/net/eth1
	* eth0 -> ../../devices/pci0000:00/0000:00:03.0/net/eth0
	* lo   -> ../../devices/virtual/net/lo
	 */
	splitDevLink := strings.SplitN(devicePath, "devices/", 2)
	if len(splitDevLink) != 2 {
		seelog.Warnf("Cannot determine device validity: %s", devicePath)
		return false
	}
	/*
	* CoreOS typically employs the vif style for physical net interfaces
	* Amazon Linux, Ubuntu, RHEL, Fedora, Suse use the traditional pci convention
	 */
	if strings.HasPrefix(splitDevLink[1], pciDevicePrefix) || strings.HasPrefix(splitDevLink[1], vifDevicePrefix) {
		return true
	}
	if strings.HasPrefix(splitDevLink[1], virtualDevicePrefix) {
		return false
	}
	// NOTE: Should never reach here
	seelog.Criticalf("Failed to validate device path: %s", devicePath)
	return false
}
