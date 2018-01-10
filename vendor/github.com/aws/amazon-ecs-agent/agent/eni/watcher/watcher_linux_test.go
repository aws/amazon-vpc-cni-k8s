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
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/deniswernert/udev"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/statechange"

	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate/mocks"
	"github.com/aws/amazon-ecs-agent/agent/eni/netlinkwrapper/mocks"
	"github.com/aws/amazon-ecs-agent/agent/eni/udevwrapper/mocks"
)

const (
	randomDevice     = "eth1"
	primaryMAC       = "00:0a:95:9d:68:61"
	randomMAC        = "00:0a:95:9d:68:16"
	randomDevPath    = " ../../devices/pci0000:00/0000:00:03.0/net/eth1"
	incorrectDevPath = "../../devices/totally/wrong/net/path"
)

// TestWatcherInit checks the sanity of watcher initialization
func TestWatcherInit(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.Background()
	mockNetlink := mock_netlinkwrapper.NewMockNetLink(mockCtrl)
	parsedMAC, err := net.ParseMAC(randomMAC)
	assert.NoError(t, err)

	primaryMACAddr, err := net.ParseMAC("00:0a:95:9d:68:61")
	assert.NoError(t, err)

	taskEngineState := dockerstate.NewTaskEngineState()
	taskEngineState.AddENIAttachment(&api.ENIAttachment{
		MACAddress:       randomMAC,
		AttachStatusSent: false,
		ExpiresAt:        time.Unix(time.Now().Unix()+10, 0),
	})
	eventChannel := make(chan statechange.Event)

	// Create Watcher
	watcher := newWatcher(ctx, primaryMAC, mockNetlink, nil, taskEngineState, eventChannel)

	// Init() uses netlink.LinkList() to build initial state
	mockNetlink.EXPECT().LinkList().Return([]netlink.Link{
		&netlink.Device{
			LinkAttrs: netlink.LinkAttrs{
				HardwareAddr: parsedMAC,
				Name:         randomDevice,
			},
		},
		&netlink.Device{
			LinkAttrs: netlink.LinkAttrs{
				HardwareAddr: primaryMACAddr,
				Name:         "lo",
				EncapType:    "loopback",
			},
		},
	}, nil)

	waitForEvents := sync.WaitGroup{}
	waitForEvents.Add(1)
	var event statechange.Event
	go func() {
		event = <-eventChannel
		assert.NotNil(t, event.(api.TaskStateChange).Attachment)
		assert.Equal(t, randomMAC, event.(api.TaskStateChange).Attachment.MACAddress)
		waitForEvents.Done()
	}()
	watcher.Init()

	waitForEvents.Wait()

	select {
	case <-eventChannel:
		t.Errorf("Expect no more state change event")
	default:
	}
}

// TestInitWithNetlinkError checks the netlink linklist error path
func TestInitWithNetlinkError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.Background()
	mockNetlink := mock_netlinkwrapper.NewMockNetLink(mockCtrl)
	mockNetlink.EXPECT().LinkList().Return([]netlink.Link{},
		errors.New("Dummy Netlink LinkList error"))

	taskEngineState := dockerstate.NewTaskEngineState()
	eventChannel := make(chan statechange.Event)

	// Create Watcher
	watcher := newWatcher(ctx, primaryMAC, mockNetlink, nil, taskEngineState, eventChannel)
	err := watcher.Init()
	assert.Error(t, err)
}

// TestWatcherInitWithEmptyList checks sanity of watcher upon empty list
func TestWatcherInitWithEmptyList(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.Background()
	mockNetlink := mock_netlinkwrapper.NewMockNetLink(mockCtrl)
	taskEngineState := dockerstate.NewTaskEngineState()
	eventChannel := make(chan statechange.Event)

	// Create Watcher
	watcher := newWatcher(ctx, primaryMAC, mockNetlink, nil, taskEngineState, eventChannel)

	// Init() uses netlink.LinkList() to build initial state
	mockNetlink.EXPECT().LinkList().Return([]netlink.Link{}, nil)

	err := watcher.Init()
	assert.NoError(t, err)
}

// TestReconcileENIs tests the reconciliation code path
func TestReconcileENIs(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.Background()
	parsedMAC, err := net.ParseMAC(randomMAC)
	assert.NoError(t, err)

	primaryMACAddr, err := net.ParseMAC("00:0a:95:9d:68:61")
	assert.NoError(t, err)

	mockNetlink := mock_netlinkwrapper.NewMockNetLink(mockCtrl)
	taskEngineState := dockerstate.NewTaskEngineState()
	eventChannel := make(chan statechange.Event)

	taskEngineState.AddENIAttachment(&api.ENIAttachment{
		MACAddress:       randomMAC,
		AttachStatusSent: false,
		ExpiresAt:        time.Unix(time.Now().Unix()+10, 0),
	})

	mockNetlink.EXPECT().LinkList().Return([]netlink.Link{
		&netlink.Device{
			LinkAttrs: netlink.LinkAttrs{
				HardwareAddr: parsedMAC,
				Name:         randomDevice,
			},
		},
		&netlink.Device{
			LinkAttrs: netlink.LinkAttrs{
				HardwareAddr: primaryMACAddr,
				Name:         "lo",
				EncapType:    "loopback",
			},
		},
	}, nil)

	var event statechange.Event
	done := make(chan struct{})
	go func() {
		event = <-eventChannel
		done <- struct{}{}
	}()

	// Create Watcher
	watcher := newWatcher(ctx, primaryMAC, mockNetlink, nil, taskEngineState, eventChannel)
	watcher.reconcileOnce()

	<-done
	assert.NotNil(t, event.(api.TaskStateChange).Attachment)
	assert.Equal(t, randomMAC, event.(api.TaskStateChange).Attachment.MACAddress)

	select {
	case <-eventChannel:
		t.Errorf("Expect no more state change event")
	default:
	}
}

// TestReconcileENIsWithNetlinkErr tests reconciliation with netlink error
func TestReconcileENIsWithNetlinkErr(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.Background()
	mockNetlink := mock_netlinkwrapper.NewMockNetLink(mockCtrl)
	mockNetlink.EXPECT().LinkList().Return([]netlink.Link{},
		errors.New("Dummy Netlink LinkList error"))

	taskEngineState := dockerstate.NewTaskEngineState()
	eventChannel := make(chan statechange.Event)

	// Create Watcher
	watcher := newWatcher(ctx, primaryMAC, mockNetlink, nil, taskEngineState, eventChannel)
	watcher.reconcileOnce()

	select {
	case <-eventChannel:
		t.Errorf("Expect no more state change event")
	default:
	}
}

// TestReconcileENIsWithEmptyList checks sanity on empty list from Netlink
func TestReconcileENIsWithEmptyList(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.Background()
	mockNetlink := mock_netlinkwrapper.NewMockNetLink(mockCtrl)

	taskEngineState := dockerstate.NewTaskEngineState()
	eventChannel := make(chan statechange.Event)

	mockNetlink.EXPECT().LinkList().Return([]netlink.Link{}, nil)

	// Create Watcher
	watcher := newWatcher(ctx, primaryMAC, mockNetlink, nil, taskEngineState, eventChannel)
	watcher.reconcileOnce()
	watcher.Stop()

	select {
	case <-eventChannel:
		t.Errorf("Expect no more state change event")
	default:
	}
}

// getUdevEventDummy builds a dummy udev.UEvent object
func getUdevEventDummy(action, subsystem, devpath string) udev.UEvent {
	m := make(map[string]string, 5)
	m["INTERFACE"] = "eth1"
	m["IFINDEX"] = "1"
	m["ACTION"] = action
	m["SUBSYSTEM"] = subsystem
	m["DEVPATH"] = devpath
	event := udev.UEvent{
		Env: m,
	}
	return event
}

// TestUdevAddEvent tests adding a device from an udev event
func TestUdevAddEvent(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.TODO()
	mockNetlink := mock_netlinkwrapper.NewMockNetLink(mockCtrl)
	mockUdev := mock_udevwrapper.NewMockUdev(mockCtrl)
	parsedMAC, _ := net.ParseMAC(randomMAC)
	mockStateManager := mock_dockerstate.NewMockTaskEngineState(mockCtrl)
	eventChannel := make(chan statechange.Event)

	// Create Watcher
	watcher := newWatcher(ctx, primaryMAC, mockNetlink, mockUdev, mockStateManager, eventChannel)

	shutdown := make(chan bool)
	gomock.InOrder(
		mockUdev.EXPECT().Monitor(watcher.events).Return(shutdown),
		mockNetlink.EXPECT().LinkByName(randomDevice).Return(
			&netlink.Device{
				LinkAttrs: netlink.LinkAttrs{
					HardwareAddr: parsedMAC,
					Name:         randomDevice,
				},
			}, nil),
		mockStateManager.EXPECT().ENIByMac(randomMAC).Return(
			&api.ENIAttachment{ExpiresAt: time.Unix(time.Now().Unix()+10, 0)}, true),
	)

	// Spin off event handler
	go watcher.eventHandler()
	// Send event to channel
	event := getUdevEventDummy(udevAddEvent, udevNetSubsystem, randomDevPath)
	watcher.events <- &event

	eniChangeEvent := <-eventChannel
	taskStateChange, ok := eniChangeEvent.(api.TaskStateChange)
	require.True(t, ok)
	assert.Equal(t, api.ENIAttached, taskStateChange.Attachment.Status)

	var waitForClose sync.WaitGroup
	waitForClose.Add(2)
	mockUdev.EXPECT().Close().Do(func() {
		waitForClose.Done()
	}).Return(nil)
	go func() {
		<-shutdown
		waitForClose.Done()
	}()

	go watcher.Stop()
	waitForClose.Wait()
}

// TestUdevSubsystemFilter checks the subsystem filter in the event handler
func TestUdevSubsystemFilter(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.TODO()
	// Setup Mock Udev
	mockUdev := mock_udevwrapper.NewMockUdev(mockCtrl)

	// Create Watcher
	watcher := newWatcher(ctx, primaryMAC, nil, mockUdev, nil, nil)

	shutdown := make(chan bool)
	mockUdev.EXPECT().Monitor(watcher.events).Return(shutdown)

	// Spin off event handler
	go watcher.eventHandler()
	// Send event to channel
	// This event shouldn't trigger the statemanager to handle HandleENIEvent
	event := getUdevEventDummy(udevAddEvent, udevPCISubsystem, randomDevPath)
	watcher.events <- &event

	var waitForClose sync.WaitGroup
	waitForClose.Add(2)
	mockUdev.EXPECT().Close().Do(func() {
		waitForClose.Done()
	}).Return(nil)
	go func() {
		<-shutdown
		waitForClose.Done()
	}()

	go watcher.Stop()
	waitForClose.Wait()
}

// TestUdevAddEventWithInvalidInterface attempts to add a device without
// a well defined interface
func TestUdevAddEventWithInvalidInterface(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.TODO()

	// Setup Mock Udev
	mockUdev := mock_udevwrapper.NewMockUdev(mockCtrl)
	// Create Watcher
	watcher := newWatcher(ctx, primaryMAC, nil, mockUdev, nil, nil)

	shutdown := make(chan bool)
	mockUdev.EXPECT().Monitor(watcher.events).Return(shutdown)

	// Spin off event handler
	go watcher.eventHandler()

	// Send event to channel
	event := getUdevEventDummy(udevAddEvent, udevNetSubsystem, incorrectDevPath)
	watcher.events <- &event

	var waitForClose sync.WaitGroup
	waitForClose.Add(2)
	mockUdev.EXPECT().Close().Do(func() {
		waitForClose.Done()
	}).Return(nil)
	go func() {
		<-shutdown
		waitForClose.Done()
	}()

	go watcher.Stop()
	waitForClose.Wait()
}

// TestUdevAddEventWithoutMACAdress attempts to add a device without
// a MACAddress based on an udev event
func TestUdevAddEventWithoutMACAdress(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	ctx := context.TODO()
	// Setup Mock Netlink
	mockNetlink := mock_netlinkwrapper.NewMockNetLink(mockCtrl)
	// Setup Mock Udev
	mockUdev := mock_udevwrapper.NewMockUdev(mockCtrl)

	watcher := newWatcher(ctx, primaryMAC, mockNetlink, mockUdev, nil, nil)

	var invoked sync.WaitGroup
	invoked.Add(1)

	shutdown := make(chan bool)
	gomock.InOrder(
		mockUdev.EXPECT().Monitor(watcher.events).Return(shutdown),
		mockNetlink.EXPECT().LinkByName(randomDevice).Do(func(device string) {
			invoked.Done()
		}).Return(
			&netlink.Device{},
			errors.New("Dummy Netlink LinkByName error")),
	)

	// Spin off event handler
	go watcher.eventHandler()

	// Send event to channel
	event := getUdevEventDummy(udevAddEvent, udevNetSubsystem, randomDevPath)
	watcher.events <- &event
	invoked.Wait()

	var waitForClose sync.WaitGroup
	waitForClose.Add(2)
	mockUdev.EXPECT().Close().Do(func() {
		waitForClose.Done()
	}).Return(nil)
	go func() {
		<-shutdown
		waitForClose.Done()
	}()

	go watcher.Stop()
	waitForClose.Wait()
}

func TestSendENIStateChange(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockStateManager := mock_dockerstate.NewMockTaskEngineState(mockCtrl)
	eventChannel := make(chan statechange.Event)

	watcher := newWatcher(context.TODO(), primaryMAC, nil, nil, mockStateManager, eventChannel)

	mockStateManager.EXPECT().ENIByMac(randomMAC).Return(&api.ENIAttachment{
		ExpiresAt: time.Unix(time.Now().Unix()+10, 0),
	}, true)

	go watcher.sendENIStateChange(randomMAC)

	eniChangeEvent := <-eventChannel
	taskStateChange, ok := eniChangeEvent.(api.TaskStateChange)
	require.True(t, ok)
	assert.Equal(t, api.ENIAttached, taskStateChange.Attachment.Status)
}

func TestSendENIStateChangeUnmanaged(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockStateManager := mock_dockerstate.NewMockTaskEngineState(mockCtrl)
	watcher := newWatcher(context.TODO(), primaryMAC, nil, nil, mockStateManager, nil)

	mockStateManager.EXPECT().ENIByMac(randomMAC).Return(nil, false)
	assert.Error(t, watcher.sendENIStateChange(randomMAC))
}

func TestSendENIStateChangeAlreadySent(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockStateManager := mock_dockerstate.NewMockTaskEngineState(mockCtrl)
	watcher := newWatcher(context.TODO(), primaryMAC, nil, nil, mockStateManager, nil)

	mockStateManager.EXPECT().ENIByMac(randomMAC).Return(&api.ENIAttachment{
		AttachStatusSent: true,
		ExpiresAt:        time.Unix(time.Now().Unix()+10, 0),
		MACAddress:       randomMAC,
	}, true)

	assert.Error(t, watcher.sendENIStateChange(randomMAC))
}

func TestSendENIStateChangeExpired(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockStateManager := mock_dockerstate.NewMockTaskEngineState(mockCtrl)
	watcher := newWatcher(context.TODO(), primaryMAC, nil, nil, mockStateManager, nil)

	gomock.InOrder(
		mockStateManager.EXPECT().ENIByMac(randomMAC).Return(
			&api.ENIAttachment{
				AttachStatusSent: false,
				ExpiresAt:        time.Unix(time.Now().Unix()-10, 0),
				MACAddress:       randomMAC,
			}, true),
		mockStateManager.EXPECT().RemoveENIAttachment(randomMAC),
	)

	assert.Error(t, watcher.sendENIStateChange(randomMAC))
}

func TestSendENIStateChangeWithRetries(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockStateManager := mock_dockerstate.NewMockTaskEngineState(mockCtrl)
	eventChannel := make(chan statechange.Event)

	watcher := newWatcher(context.TODO(), primaryMAC, nil, nil, mockStateManager, eventChannel)

	gomock.InOrder(
		mockStateManager.EXPECT().ENIByMac(randomMAC).Return(nil, false),
		mockStateManager.EXPECT().ENIByMac(randomMAC).Return(&api.ENIAttachment{
			ExpiresAt: time.Unix(time.Now().Unix()+10, 0),
		}, true),
	)

	ctx := context.TODO()
	go watcher.sendENIStateChangeWithRetries(ctx, randomMAC, sendENIStateChangeRetryTimeout)

	eniChangeEvent := <-eventChannel
	taskStateChange, ok := eniChangeEvent.(api.TaskStateChange)
	require.True(t, ok)
	assert.Equal(t, api.ENIAttached, taskStateChange.Attachment.Status)
}

func TestSendENIStateChangeWithRetriesDoesNotRetryExpiredENI(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockStateManager := mock_dockerstate.NewMockTaskEngineState(mockCtrl)

	watcher := newWatcher(context.TODO(), primaryMAC, nil, nil, mockStateManager, nil)

	gomock.InOrder(
		// ENIByMAC returns an error for exipred ENI attachment, which should
		// mean that it doesn't get retried.
		mockStateManager.EXPECT().ENIByMac(randomMAC).Return(
			&api.ENIAttachment{
				AttachStatusSent: false,
				ExpiresAt:        time.Unix(time.Now().Unix()-10, 0),
				MACAddress:       randomMAC,
			}, true),
		mockStateManager.EXPECT().RemoveENIAttachment(randomMAC),
	)

	ctx := context.TODO()
	assert.Error(t, watcher.sendENIStateChangeWithRetries(
		ctx, randomMAC, sendENIStateChangeRetryTimeout))
}
