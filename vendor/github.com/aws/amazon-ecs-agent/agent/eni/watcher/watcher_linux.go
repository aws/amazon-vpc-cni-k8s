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

package watcher

import (
	"context"
	"fmt"
	"time"

	log "github.com/cihub/seelog"
	"github.com/deniswernert/udev"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/eni/netlinkwrapper"
	"github.com/aws/amazon-ecs-agent/agent/eni/networkutils"
	"github.com/aws/amazon-ecs-agent/agent/eni/udevwrapper"
	"github.com/aws/amazon-ecs-agent/agent/statechange"
	"github.com/aws/amazon-ecs-agent/agent/utils"
)

const (
	// linkTypeDevice defines the string that's expected to be the output of
	// netlink.Link.Type() method for netlink.Device type
	linkTypeDevice = "device"

	// encapTypeLoopback defines the string that's set for the link.Attrs.EncapType
	// field for localhost devices. The EncapType field defines the link
	// encapsulation method. For localhost, it's set to "loopback"
	encapTypeLoopback = "loopback"

	// sendENIStateChangeRetryTimeout specifies the timeout before giving up
	// when looking for ENI in agent's state. If for whatever reason, the message
	// from ACS is received after the ENI has been attached to the instance, this
	// timeout duration will be used to wait for ENI message to be sent from ACS
	sendENIStateChangeRetryTimeout = 3 * time.Second

	// sendENIStateChangeBackoffMin specifies minimum value for backoff when
	// waiting for attachment message from ACS
	sendENIStateChangeBackoffMin = 100 * time.Millisecond

	// sendENIStateChangeBackoffMax specifies maximum value for backoff when
	// waiting for attachment message from ACS
	sendENIStateChangeBackoffMax = 250 * time.Millisecond

	// sendENIStateChangeBackoffJitter specifies the jitter multiple percentage
	// when waiting for attachment message from ACS
	sendENIStateChangeBackoffJitter = 0.2

	// sendENIStateChangeBackoffMultiple specifies the backoff duration multipler
	// when waiting for the attachment message from ACS
	sendENIStateChangeBackoffMultiple = 1.5

	// macAddressRetryTimeout specifies the timeout before giving up when
	// looking for an ENI's mac address on the host. It takes a few milliseconds
	// for the host to learn about an ENIs mac address from netlink.LinkList().
	// We are capping off this duration to 1s assuming worst-case behavior
	macAddressRetryTimeout = time.Second
)

// UdevWatcher maintains the state of attached ENIs
// to the instance. It also has supporting elements to
// maintain consistency and update intervals
type UdevWatcher struct {
	ctx                  context.Context
	cancel               context.CancelFunc
	updateIntervalTicker *time.Ticker
	netlinkClient        netlinkwrapper.NetLink
	udevMonitor          udevwrapper.Udev
	events               chan *udev.UEvent
	agentState           dockerstate.TaskEngineState
	eniChangeEvent       chan<- statechange.Event
	primaryMAC           string
}

// unmanagedENIError is used to indicate that the agent found an ENI, but the agent isn't
// aware if this ENI is being managed by ECS
type unmanagedENIError struct {
	mac string
}

// Error returns the error string for the unmanagedENIError type
func (err *unmanagedENIError) Error() string {
	return fmt.Sprintf("udev watcher send ENI state change: eni not managed by ecs: %s", err.mac)
}

// New is used to return an instance of the UdevWatcher struct
func New(ctx context.Context, primaryMAC string, udevwrap udevwrapper.Udev,
	state dockerstate.TaskEngineState, stateChangeEvents chan<- statechange.Event) *UdevWatcher {
	return newWatcher(ctx, primaryMAC, netlinkwrapper.New(), udevwrap, state, stateChangeEvents)
}

// newWatcher is used to nest the return of the UdevWatcher struct
func newWatcher(ctx context.Context,
	primaryMAC string,
	nlWrap netlinkwrapper.NetLink,
	udevWrap udevwrapper.Udev,
	state dockerstate.TaskEngineState,
	stateChangeEvents chan<- statechange.Event) *UdevWatcher {

	derivedContext, cancel := context.WithCancel(ctx)
	return &UdevWatcher{
		ctx:            derivedContext,
		cancel:         cancel,
		netlinkClient:  nlWrap,
		udevMonitor:    udevWrap,
		events:         make(chan *udev.UEvent),
		agentState:     state,
		eniChangeEvent: stateChangeEvents,
		primaryMAC:     primaryMAC,
	}
}

// Init initializes a new ENI Watcher
func (udevWatcher *UdevWatcher) Init() error {
	return udevWatcher.reconcileOnce()
}

// Start periodically updates the state of ENIs connected to the system
func (udevWatcher *UdevWatcher) Start() {
	// Udev Event Handler
	go udevWatcher.eventHandler()
	udevWatcher.performPeriodicReconciliation(defaultReconciliationInterval)
}

// Stop is used to invoke the cancellation routine
func (udevWatcher *UdevWatcher) Stop() {
	udevWatcher.cancel()
}

// performPeriodicReconciliation is used to periodically invoke the
// reconciliation process based on a ticker
func (udevWatcher *UdevWatcher) performPeriodicReconciliation(updateInterval time.Duration) {
	udevWatcher.updateIntervalTicker = time.NewTicker(updateInterval)
	for {
		select {
		case <-udevWatcher.updateIntervalTicker.C:
			if err := udevWatcher.reconcileOnce(); err != nil {
				log.Warnf("Udev watcher reconciliation failed: %v", err)
			}
		case <-udevWatcher.ctx.Done():
			udevWatcher.updateIntervalTicker.Stop()
			return
		}
	}
}

// reconcileOnce is used to reconcile the state of ENIs attached to the instance
func (udevWatcher *UdevWatcher) reconcileOnce() error {
	links, err := udevWatcher.netlinkClient.LinkList()
	if err != nil {
		return errors.Wrapf(err, "udev watcher: unable to retrieve network interfaces")
	}

	// Return on empty list
	if len(links) == 0 {
		log.Info("Udev watcher reconciliation: no network interfaces discovered for reconciliation")
		return nil
	}

	currentState := udevWatcher.buildState(links)

	// NOTE: For correct semantics, this entire function needs to be locked.
	// As we postulate the netlinkClient.LinkList() call to be expensive, we allow
	// the race here. The state would be corrected during the next reconciliation loop.

	// Add new interfaces next
	for mac, _ := range currentState {
		if err := udevWatcher.sendENIStateChange(mac); err != nil {
			log.Warnf("Udev watcher reconciliation: unable to send state change: %v", err)
		}
	}
	return nil
}

// sendENIStateChange handles the eni event from udev or reconcile phase
func (udevWatcher *UdevWatcher) sendENIStateChange(mac string) error {
	if mac == "" {
		return errors.New("udev watcher send ENI state change: empty mac address")
	}
	// check if this is an eni required by a task
	eni, ok := udevWatcher.agentState.ENIByMac(mac)
	if !ok {
		return &unmanagedENIError{mac}
	}
	if eni.IsSent() {
		return errors.Errorf("udev watcher send ENI state change: eni status already sent: %s", eni.String())
	}
	if eni.HasExpired() {
		// Agent is aware of the ENI, but we decide not to ack it
		// as it's ack timeout has expired
		udevWatcher.agentState.RemoveENIAttachment(eni.MACAddress)
		return errors.Errorf(
			"udev watcher send ENI state change: eni status expired, no longer tracking it: %s",
			eni.String())
	}

	// We found an ENI, which has the expiration time set in future and
	// needs to be acknowledged as having been 'attached' to the Instance
	go func(eni *api.ENIAttachment) {
		eni.Status = api.ENIAttached
		log.Infof("Emitting ENI change event for: %s", eni.String())
		udevWatcher.eniChangeEvent <- api.TaskStateChange{
			TaskARN:    eni.TaskARN,
			Attachment: eni,
		}
	}(eni)
	return nil
}

// buildState is used to build a state of the system for reconciliation
func (udevWatcher *UdevWatcher) buildState(links []netlink.Link) map[string]string {
	state := make(map[string]string)
	for _, link := range links {
		if link.Type() != linkTypeDevice {
			// We only care about netlink.Device types. These are created
			// by udev like 'lo' and 'eth0'. Ignore other link types
			continue
		}
		if link.Attrs().EncapType == encapTypeLoopback {
			// Ignore localhost
			continue
		}
		macAddress := link.Attrs().HardwareAddr.String()
		if macAddress != "" && macAddress != udevWatcher.primaryMAC {
			state[macAddress] = link.Attrs().Name
		}
	}
	return state
}

// eventHandler is used to manage udev net subsystem events to add/remove interfaces
func (udevWatcher *UdevWatcher) eventHandler() {
	// The shutdown channel will be used to terminate the watch for udev events
	shutdown := udevWatcher.udevMonitor.Monitor(udevWatcher.events)
	for {
		select {
		case event := <-udevWatcher.events:
			subsystem, ok := event.Env[udevSubsystem]
			if !ok || subsystem != udevNetSubsystem {
				continue
			}
			if event.Env[udevEventAction] != udevAddEvent {
				continue
			}
			if !networkutils.IsValidNetworkDevice(event.Env[udevDevPath]) {
				log.Debugf("Udev watcher event handler: ignoring event for invalid network device: %s", event.String())
				continue
			}
			netInterface := event.Env[udevInterface]
			// GetMACAddres and sendENIStateChangeWithRetries can block the execution
			// of this method for a few seconds in the worst-case scenario.
			// Execute these within a go-routine
			go func(ctx context.Context, dev string, timeout time.Duration) {
				log.Debugf("Udev watcher event-handler: add interface: %s", dev)
				macAddress, err := networkutils.GetMACAddress(udevWatcher.ctx, macAddressRetryTimeout,
					dev, udevWatcher.netlinkClient)
				if err != nil {
					log.Warnf("Udev watcher event-handler: error obtaining MACAddress for interface %s", dev)
					return
				}

				if err := udevWatcher.sendENIStateChangeWithRetries(ctx, macAddress, timeout); err != nil {
					log.Warnf("Udev watcher event-handler: unable to send state change: %v", err)
				}
			}(udevWatcher.ctx, netInterface, sendENIStateChangeRetryTimeout)
		case <-udevWatcher.ctx.Done():
			log.Info("Stopping udev event handler")
			// Send the shutdown signal and close the connection
			shutdown <- true
			if err := udevWatcher.udevMonitor.Close(); err != nil {
				log.Warnf("Unable to close the udev monitoring socket: %v", err)
			}
			return
		}
	}
}

// sendENIStateChangeWithRetries invokes the sendENIStateChange method, with backoff and
// retries. Retries are only effective if sendENIStateChange returns an unmanagedENIError.
// We're effectively waiting for the ENI attachment message from ACS for a network device
// at this point of time.
func (udevWatcher *UdevWatcher) sendENIStateChangeWithRetries(parentCtx context.Context,
	macAddress string,
	timeout time.Duration) error {
	backoff := utils.NewSimpleBackoff(sendENIStateChangeBackoffMin, sendENIStateChangeBackoffMax,
		sendENIStateChangeBackoffJitter, sendENIStateChangeBackoffMultiple)
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	err := utils.RetryWithBackoffCtx(ctx, backoff, func() error {
		sendErr := udevWatcher.sendENIStateChange(macAddress)
		if sendErr != nil {
			if _, ok := sendErr.(*unmanagedENIError); ok {
				log.Debugf("Unable to send state change for unmanaged ENI: %v", sendErr)
				return sendErr
			}
			// Not unmanagedENIError. Stop retrying when this happens
			return utils.NewRetriableError(utils.NewRetriable(false), sendErr)
		}

		return nil
	})

	if err != nil {
		return err
	}
	// RetryWithBackoffCtx returns nil when the context is cancelled. Check if there was
	// a timeout here. TODO: Fix RetryWithBackoffCtx to return ctx.Err() on context Done()
	if err = ctx.Err(); err != nil {
		return errors.Wrapf(err,
			"udev watcher send ENI state change: timed out waiting for eni '%s' in state", macAddress)
	}

	return nil
}
