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

package driver

import (
	"errors"
	"net"
	"os"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/cninswrapper/mock_ns"
	mocks_ip "github.com/aws/amazon-vpc-cni-k8s/pkg/ipwrapper/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mock_netlink"
	mock_netlinkwrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mocks"
	mock_nswrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/nswrapper/mocks"
	mock_procsyswrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/procsyswrapper/mocks"
)

const (
	testMAC          = "01:23:45:67:89:ab"
	testIP           = "10.0.10.10"
	testContVethName = "eth0"
	testHostVethName = "aws-eth0"
	testFD           = 10
	testnetnsPath    = "/proc/1234/netns"
	testTable        = 10
	mtu              = 9001
)

var logConfig = logger.Configuration{
	BinaryName:  "aws-cni",
	LogLevel:    "Debug",
	LogLocation: "stdout",
}

var log = logger.New(&logConfig)

type testMocks struct {
	ctrl    *gomock.Controller
	netlink *mock_netlinkwrapper.MockNetLink
	ip      *mocks_ip.MockIP
	ns      *mock_nswrapper.MockNS
	netns   *mock_ns.MockNetNS
	procsys *mock_procsyswrapper.MockProcSys
}

func setup(t *testing.T) *testMocks {
	ctrl := gomock.NewController(t)
	return &testMocks{
		ctrl:    ctrl,
		netlink: mock_netlinkwrapper.NewMockNetLink(ctrl),
		ip:      mocks_ip.NewMockIP(ctrl),
		ns:      mock_nswrapper.NewMockNS(ctrl),
		netns:   mock_ns.NewMockNetNS(ctrl),
		procsys: mock_procsyswrapper.NewMockProcSys(ctrl),
	}
}

func (m *testMocks) mockWithFailureAt(t *testing.T, failAt string) *createVethPairContext {
	mockContext := &createVethPairContext{
		contVethName: testContVethName,
		hostVethName: testHostVethName,
		netLink:      m.netlink,
		ip:           m.ip,
		addr: &net.IPNet{
			IP:   net.ParseIP(testIP),
			Mask: net.IPv4Mask(255, 255, 255, 255),
		},
	}

	hwAddr, err := net.ParseMAC(testMAC)
	assert.NoError(t, err)

	mockLinkAttrs := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	mockHostVeth := mock_netlink.NewMockLink(m.ctrl)
	mockContVeth := mock_netlink.NewMockLink(m.ctrl)

	var call *gomock.Call

	// veth pair
	if failAt == "link-add" {
		call = m.netlink.EXPECT().LinkAdd(gomock.Any()).Return(errors.New("error on LinkAdd"))
		return mockContext
	}
	call = m.netlink.EXPECT().LinkAdd(gomock.Any()).Return(nil)

	//hostVeth
	if failAt == "link-by-name" {
		call = m.netlink.EXPECT().LinkByName(gomock.Any()).Return(nil, errors.New("error on LinkByName host")).After(call)
		return mockContext
	}
	call = m.netlink.EXPECT().LinkByName(gomock.Any()).Return(mockHostVeth, nil).After(call)

	//host side setup
	if failAt == "link-setup" {
		call = m.netlink.EXPECT().LinkSetUp(mockHostVeth).Return(errors.New("error on LinkSetup")).After(call)
		return mockContext
	}
	call = m.netlink.EXPECT().LinkSetUp(mockHostVeth).Return(nil).After(call)

	//container side
	if failAt == "link-byname" {
		call = m.netlink.EXPECT().LinkByName(gomock.Any()).Return(mockContVeth, errors.New("error on LinkByName container")).After(call)
		return mockContext
	}
	call = m.netlink.EXPECT().LinkByName(gomock.Any()).Return(mockContVeth, nil).After(call)

	// container setup
	call = m.netlink.EXPECT().LinkSetUp(mockContVeth).Return(nil).After(call)
	// container
	call = mockContVeth.EXPECT().Attrs().Return(mockLinkAttrs).After(call)

	if failAt == "route-replace" {
		call = m.netlink.EXPECT().RouteReplace(gomock.Any()).Return(errors.New("error on RouteReplace")).After(call)
		return mockContext
	}
	call = m.netlink.EXPECT().RouteReplace(gomock.Any()).Return(nil).After(call)

	if failAt == "add-defaultroute" {
		call = m.ip.EXPECT().AddDefaultRoute(gomock.Any(), mockContVeth).Return(errors.New("error on AddDefaultRoute")).After(call)
		return mockContext
	}
	call = m.ip.EXPECT().AddDefaultRoute(gomock.Any(), mockContVeth).Return(nil).After(call)

	// container addr
	if failAt == "addr-add" {
		call = m.netlink.EXPECT().AddrAdd(mockContVeth, gomock.Any()).Return(errors.New("error on AddrAdd")).After(call)
		return mockContext
	}
	call = m.netlink.EXPECT().AddrAdd(mockContVeth, gomock.Any()).Return(nil).After(call)

	// neighbor
	call = mockContVeth.EXPECT().Attrs().Return(mockLinkAttrs).After(call)
	// hostVethMAC
	call = mockHostVeth.EXPECT().Attrs().Return(mockLinkAttrs).After(call)
	if failAt == "neigh-add" {
		call = m.netlink.EXPECT().NeighAdd(gomock.Any()).Return(errors.New("error on NeighAdd")).After(call)
		return mockContext
	}
	call = m.netlink.EXPECT().NeighAdd(gomock.Any()).Return(nil).After(call)

	call = m.netns.EXPECT().Fd().Return(uintptr(testFD)).After(call)
	// move it host namespace
	if failAt == "link-setns" {
		call = m.netlink.EXPECT().LinkSetNsFd(mockHostVeth, testFD).Return(errors.New("error on LinkSetNsFd")).After(call)
		return mockContext
	}
	call = m.netlink.EXPECT().LinkSetNsFd(mockHostVeth, testFD).Return(nil).After(call)

	return mockContext
}

func TestRun(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := m.mockWithFailureAt(t, "")

	err := mockContext.run(m.netns)
	assert.NoError(t, err)
}

func TestRunLinkAddErr(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := m.mockWithFailureAt(t, "link-add")

	err := mockContext.run(m.netns)
	assert.Error(t, err)
}

func TestRunErrLinkByNameHost(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := m.mockWithFailureAt(t, "link-by-name")

	err := mockContext.run(m.netns)
	assert.Error(t, err)
}

func TestRunErrSetup(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := m.mockWithFailureAt(t, "link-setup")

	err := mockContext.run(m.netns)
	assert.Error(t, err)
}

func TestRunErrLinkByNameCont(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := m.mockWithFailureAt(t, "link-byname")

	err := mockContext.run(m.netns)
	assert.Error(t, err)
}

func TestRunErrRouteAdd(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := m.mockWithFailureAt(t, "route-replace")

	err := mockContext.run(m.netns)
	assert.Error(t, err)
}

func TestRunErrAddDefaultRoute(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := m.mockWithFailureAt(t, "add-defaultroute")

	err := mockContext.run(m.netns)
	assert.Error(t, err)
}

func TestRunErrAddrAdd(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := m.mockWithFailureAt(t, "addr-add")

	err := mockContext.run(m.netns)
	assert.Error(t, err)
}

func TestRunErrNeighAdd(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := m.mockWithFailureAt(t, "neigh-add")

	err := mockContext.run(m.netns)
	assert.Error(t, err)
}

func TestRunErrLinkSetNsFd(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := m.mockWithFailureAt(t, "link-setns")

	err := mockContext.run(m.netns)
	assert.Error(t, err)
}

func (m *testMocks) mockSetupPodNetworkWithFailureAt(t *testing.T, failAt string) {
	mockHostVeth := mock_netlink.NewMockLink(m.ctrl)

	m.netlink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, errors.New("hostVeth already exists"))
	m.ns.EXPECT().WithNetNSPath(testnetnsPath, gomock.Any()).Return(nil)

	if failAt == "link-byname" {
		m.netlink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, errors.New("error on hostVethName"))
		return
	}
	m.netlink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, nil)

	if failAt == "procsys" {
		m.procsys.EXPECT().Set("net/ipv6/conf/"+testHostVethName+"/accept_ra", "0").Return(errors.New("Error writing to /proc/sys/..."))
		return
	}
	var procsysRet error
	if failAt == "no-ipv6" {
		// Note os.ErrNotExist return - should be ignored
		procsysRet = os.ErrNotExist
	}
	m.procsys.EXPECT().Set("net/ipv6/conf/"+testHostVethName+"/accept_ra", "0").Return(procsysRet)
	m.procsys.EXPECT().Set("net/ipv6/conf/"+testHostVethName+"/accept_redirects", "0").Return(procsysRet)

	if failAt == "link-setup" {
		m.netlink.EXPECT().LinkSetUp(mockHostVeth).Return(errors.New("error on LinkSetup"))
		return
	}
	m.netlink.EXPECT().LinkSetUp(mockHostVeth).Return(nil)

	hwAddr, err := net.ParseMAC(testMAC)
	assert.NoError(t, err)
	mockLinkAttrs := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	//log.Printf
	mockHostVeth.EXPECT().Attrs().Return(mockLinkAttrs)
	//add host route
	mockHostVeth.EXPECT().Attrs().Return(mockLinkAttrs)
	if failAt == "route-replace" {
		m.netlink.EXPECT().RouteReplace(gomock.Any()).Return(errors.New("error on RouteReplace"))
		return
	}
	m.netlink.EXPECT().RouteReplace(gomock.Any()).Return(nil)

	testRule := &netlink.Rule{
		SuppressIfgroup:   -1,
		SuppressPrefixlen: -1,
		Priority:          -1,
		Mark:              -1,
		Mask:              -1,
		Goto:              -1,
		Flow:              -1,
	}
	m.netlink.EXPECT().NewRule().Return(testRule)
	// test to-pod rule
	m.netlink.EXPECT().RuleDel(gomock.Any()).Return(nil)
	m.netlink.EXPECT().RuleAdd(gomock.Any()).Return(nil)

	// test from-pod rule
	// FIXME(gus): this is the same as to-pod rule :/
	m.netlink.EXPECT().NewRule().Return(testRule)
	m.netlink.EXPECT().RuleDel(gomock.Any()).Return(nil)
	m.netlink.EXPECT().RuleAdd(gomock.Any()).Return(nil)
}

func TestSetupPodNetwork(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	m.mockSetupPodNetworkWithFailureAt(t, "")

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	var cidrs []string
	err := setupNS(testHostVethName, testContVethName, testnetnsPath, addr, testTable, cidrs, true, m.netlink, m.ns, mtu, log, m.procsys)
	assert.NoError(t, err)
}

func TestSetupPodNetworkErrNoIPv6(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	m.mockSetupPodNetworkWithFailureAt(t, "no-ipv6")

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	var cidrs []string
	err := setupNS(testHostVethName, testContVethName, testnetnsPath, addr, testTable, cidrs, true, m.netlink, m.ns, mtu, log, m.procsys)
	assert.NoError(t, err)
}

func TestSetupPodNetworkErrLinkByName(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	m.mockSetupPodNetworkWithFailureAt(t, "link-byname")

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	var cidrs []string
	err := setupNS(testHostVethName, testContVethName, testnetnsPath, addr, testTable, cidrs, false, m.netlink, m.ns, mtu, log, m.procsys)

	assert.Error(t, err)
}

func TestSetupPodNetworkErrLinkSetup(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	m.mockSetupPodNetworkWithFailureAt(t, "link-setup")

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	var cidrs []string
	err := setupNS(testHostVethName, testContVethName, testnetnsPath, addr, testTable, cidrs, false, m.netlink, m.ns, mtu, log, m.procsys)

	assert.Error(t, err)
}

func TestSetupPodNetworkErrProcSys(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	m.mockSetupPodNetworkWithFailureAt(t, "procsys")

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	var cidrs []string
	err := setupNS(testHostVethName, testContVethName, testnetnsPath, addr, testTable, cidrs, false, m.netlink, m.ns, mtu, log, m.procsys)

	assert.Error(t, err)
}

func TestSetupPodNetworkErrRouteReplace(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	m.mockSetupPodNetworkWithFailureAt(t, "route-replace")

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	var cidrs []string
	err := setupNS(testHostVethName, testContVethName, testnetnsPath, addr, testTable, cidrs, false, m.netlink, m.ns, mtu, log, m.procsys)

	assert.Error(t, err)
}

func TestTearDownPodNetwork(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	testRule := &netlink.Rule{
		SuppressIfgroup:   -1,
		SuppressPrefixlen: -1,
		Priority:          -1,
		Mark:              -1,
		Mask:              -1,
		Goto:              -1,
		Flow:              -1,
	}
	gomock.InOrder(
		m.netlink.EXPECT().NewRule().Return(testRule),
		// test to-pod rule
		m.netlink.EXPECT().RuleDel(gomock.Any()).Return(nil),

		// test from-pod rule
		m.netlink.EXPECT().RouteDel(gomock.Any()).Return(nil),
	)

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	err := tearDownNS(addr, 0, m.netlink, log)
	assert.NoError(t, err)
}
