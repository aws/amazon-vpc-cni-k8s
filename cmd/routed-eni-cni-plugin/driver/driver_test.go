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
	"net"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"

	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"
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
	testVlanName     = "vlan.eth.1"
	testFD           = 10
	testnetnsPath    = "/proc/1234/netns"
	testTable        = 10
	mtu              = 9001
)

var logConfig = logger.Configuration{
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
		m.netlink.EXPECT().LinkAdd(gomock.Any()).Return(errors.New("error on LinkAdd"))
		return mockContext
	}
	call = m.netlink.EXPECT().LinkAdd(gomock.Any()).Return(nil)

	//hostVeth
	if failAt == "link-by-name" {
		m.netlink.EXPECT().LinkByName(gomock.Any()).Return(nil, errors.New("error on LinkByName host")).After(call)
		return mockContext
	}
	call = m.netlink.EXPECT().LinkByName(gomock.Any()).Return(mockHostVeth, nil).After(call)

	//host side setup
	if failAt == "link-setup" {
		m.netlink.EXPECT().LinkSetUp(mockHostVeth).Return(errors.New("error on LinkSetup")).After(call)
		return mockContext
	}
	call = m.netlink.EXPECT().LinkSetUp(mockHostVeth).Return(nil).After(call)

	//container side
	if failAt == "link-byname" {
		m.netlink.EXPECT().LinkByName(gomock.Any()).Return(mockContVeth, errors.New("error on LinkByName container")).After(call)
		return mockContext
	}
	call = m.netlink.EXPECT().LinkByName(gomock.Any()).Return(mockContVeth, nil).After(call)

	// container setup
	call = m.netlink.EXPECT().LinkSetUp(mockContVeth).Return(nil).After(call)
	// container
	call = mockContVeth.EXPECT().Attrs().Return(mockLinkAttrs).After(call)

	if failAt == "route-replace" {
		m.netlink.EXPECT().RouteReplace(gomock.Any()).Return(errors.New("error on RouteReplace")).After(call)
		return mockContext
	}
	call = m.netlink.EXPECT().RouteReplace(gomock.Any()).Return(nil).After(call)

	if failAt == "add-defaultroute" {
		m.ip.EXPECT().AddDefaultRoute(gomock.Any(), mockContVeth).Return(errors.New("error on AddDefaultRoute")).After(call)
		return mockContext
	}
	call = m.ip.EXPECT().AddDefaultRoute(gomock.Any(), mockContVeth).Return(nil).After(call)

	// container addr
	if failAt == "addr-add" {
		m.netlink.EXPECT().AddrAdd(mockContVeth, gomock.Any()).Return(errors.New("error on AddrAdd")).After(call)
		return mockContext
	}
	call = m.netlink.EXPECT().AddrAdd(mockContVeth, gomock.Any()).Return(nil).After(call)

	// neighbor
	call = mockContVeth.EXPECT().Attrs().Return(mockLinkAttrs).After(call)
	// hostVethMAC
	call = mockHostVeth.EXPECT().Attrs().Return(mockLinkAttrs).After(call)
	if failAt == "neigh-add" {
		m.netlink.EXPECT().NeighAdd(gomock.Any()).Return(errors.New("error on NeighAdd")).After(call)
		return mockContext
	}
	call = m.netlink.EXPECT().NeighAdd(gomock.Any()).Return(nil).After(call)

	call = m.netns.EXPECT().Fd().Return(uintptr(testFD)).After(call)
	// move it host namespace
	if failAt == "link-setns" {
		m.netlink.EXPECT().LinkSetNsFd(mockHostVeth, testFD).Return(errors.New("error on LinkSetNsFd")).After(call)
		return mockContext
	}
	m.netlink.EXPECT().LinkSetNsFd(mockHostVeth, testFD).Return(nil).After(call)

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

func (m *testMocks) setupMockForVethCreation(failAt string) *mock_netlink.MockLink {
	mockHostVeth := mock_netlink.NewMockLink(m.ctrl)

	m.netlink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, errors.New("hostVeth already exists"))
	m.ns.EXPECT().WithNetNSPath(testnetnsPath, gomock.Any()).Return(nil)

	if failAt == "veth-link-byname" {
		m.netlink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, errors.New("error on hostVethName"))
		return nil
	}
	m.netlink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, nil)

	if failAt == "veth-procsys" {
		m.procsys.EXPECT().Set("net/ipv6/conf/"+testHostVethName+"/accept_ra", "0").Return(errors.New("error writing to /proc/sys/"))
		return nil
	}
	var procsysRet error
	if failAt == "no-ipv6" {
		// Note os.ErrNotExist return - should be ignored
		procsysRet = os.ErrNotExist
	}
	m.procsys.EXPECT().Set("net/ipv6/conf/"+testHostVethName+"/accept_ra", "0").Return(procsysRet)
	m.procsys.EXPECT().Set("net/ipv6/conf/"+testHostVethName+"/accept_redirects", "0").Return(procsysRet)

	if failAt == "veth-link-setup" {
		m.netlink.EXPECT().LinkSetUp(mockHostVeth).Return(errors.New("error on LinkSetup"))
		return nil
	}
	m.netlink.EXPECT().LinkSetUp(mockHostVeth).Return(nil)
	return mockHostVeth
}

func (m *testMocks) mockSetupPodNetworkWithFailureAt(t *testing.T, failAt string) {
	mockHostVeth := m.setupMockForVethCreation(failAt)

	// skip setting other mocks if test is expected to fail at veth creation.
	if strings.HasPrefix(failAt, "veth-") {
		return
	}

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

	m.mockSetupPodNetworkWithFailureAt(t, "veth-link-byname")

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

	m.mockSetupPodNetworkWithFailureAt(t, "veth-link-setup")

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

	m.mockSetupPodNetworkWithFailureAt(t, "veth-procsys")

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

func TestTeardownPodENINetworkHappyCase(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockVlan := mock_netlink.NewMockLink(m.ctrl)
	linuxNetwork := &linuxNetwork{
		netLink: m.netlink,
		ns:      m.ns,
		procSys: m.procsys,
	}

	actualRule := &netlink.Rule{}
	m.netlink.EXPECT().NewRule().Return(actualRule)

	expectedRule := &netlink.Rule{
		Priority: vlanRulePriority,
		Table:    101,
	}
	gomock.InOrder(
		m.netlink.EXPECT().LinkByName(testVlanName).Return(mockVlan, nil),
		m.netlink.EXPECT().LinkDel(mockVlan).Return(nil),
		// delete ip rules for the pod.
		m.netlink.EXPECT().RuleDel(gomock.Eq(expectedRule)).Return(nil),
		m.netlink.EXPECT().RuleDel(gomock.Eq(expectedRule)).Return(syscall.ENOENT),
	)

	err := linuxNetwork.TeardownPodENINetwork(1, log)
	assert.NoError(t, err)
}

func (m *testMocks) mockSetupPodENINetworkWithFailureAt(t *testing.T, addr *net.IPNet, failAt string) {
	mockHostVeth := m.setupMockForVethCreation(failAt)

	// skip setting pod ENI mocks if test is expected to fail at veth creation.
	if strings.HasPrefix(failAt, "veth-") {
		return
	}

	// link will not exist initially
	m.netlink.EXPECT().LinkByName(testVlanName).Return(nil,
		errors.New("link not found"))

	actualRule := &netlink.Rule{}
	m.netlink.EXPECT().NewRule().Return(actualRule)

	oldVethRule := &netlink.Rule{
		IifName:  testHostVethName,
		Priority: vlanRulePriority,
	}
	m.netlink.EXPECT().RuleDel(gomock.Eq(oldVethRule)).Return(syscall.ENOENT)

	vlanLink := buildVlanLink(1, 2, "eniMacAddress")
	// add the link
	m.netlink.EXPECT().LinkAdd(gomock.Eq(vlanLink)).Return(nil)

	// bring up the link
	m.netlink.EXPECT().LinkSetUp(gomock.Eq(vlanLink)).Return(nil)

	vlanRoutes := buildRoutesForVlan(101, 0, net.ParseIP("10.1.0.1"))

	// two routes for vlan
	m.netlink.EXPECT().RouteReplace(gomock.Eq(&vlanRoutes[0])).Return(nil)
	m.netlink.EXPECT().RouteReplace(gomock.Eq(&vlanRoutes[1])).Return(nil)

	hwAddr, _ := net.ParseMAC(testMAC)
	mockLinkAttrs := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
		Name:         testHostVethName,
		Index:        3,
	}
	mockHostVeth.EXPECT().Attrs().Return(mockLinkAttrs).Times(2)

	// add route for host veth
	route := netlink.Route{
		LinkIndex: 3,
		Scope:     netlink.SCOPE_LINK,
		Dst:       addr,
		Table:     101,
	}
	m.netlink.EXPECT().RouteReplace(gomock.Eq(&route)).Return(nil)

	m.netlink.EXPECT().NewRule().Return(actualRule)

	// add two ip rules based on iff interfaces
	expectedRule1 := &netlink.Rule{
		Priority: vlanRulePriority,
		Table:    101,
		IifName:  vlanLink.Name,
	}
	m.netlink.EXPECT().RuleAdd(gomock.Eq(expectedRule1)).Return(nil)

	expectedRule2 := &netlink.Rule{
		Priority: vlanRulePriority,
		Table:    101,
		IifName:  testHostVethName,
	}
	m.netlink.EXPECT().RuleAdd(gomock.Eq(expectedRule2)).Return(nil)
}

func TestSetupPodENINetworkHappyCase(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	t1 := &linuxNetwork{
		netLink: m.netlink,
		ns:      m.ns,
		procSys: m.procsys,
	}

	m.mockSetupPodENINetworkWithFailureAt(t, addr, "")

	err := t1.SetupPodENINetwork(testHostVethName, testContVethName, testnetnsPath, addr, 1, "eniMacAddress",
		"10.1.0.1", 2, mtu, log)

	assert.NoError(t, err)
}
