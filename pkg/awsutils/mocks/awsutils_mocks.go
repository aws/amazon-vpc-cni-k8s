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
//

// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils (interfaces: APIs)

// Package mock_awsutils is a generated GoMock package.
package mock_awsutils

import (
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	net "net"
	reflect "reflect"

	awsutils "github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	vpc "github.com/aws/amazon-vpc-cni-k8s/pkg/vpc"
	ec2 "github.com/aws/aws-sdk-go/service/ec2"
	gomock "github.com/golang/mock/gomock"
)

// MockAPIs is a mock of APIs interface.
type MockAPIs struct {
	ctrl     *gomock.Controller
	recorder *MockAPIsMockRecorder
}

// MockAPIsMockRecorder is the mock recorder for MockAPIs.
type MockAPIsMockRecorder struct {
	mock *MockAPIs
}

// NewMockAPIs creates a new mock instance.
func NewMockAPIs(ctrl *gomock.Controller) *MockAPIs {
	mock := &MockAPIs{ctrl: ctrl}
	mock.recorder = &MockAPIsMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockAPIs) EXPECT() *MockAPIsMockRecorder {
	return m.recorder
}

// AllocENI mocks base method.
func (m *MockAPIs) AllocENI(arg0 bool, arg1 []*string, arg2 string, arg3 int) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AllocENI", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AllocENI indicates an expected call of AllocENI.
func (mr *MockAPIsMockRecorder) AllocENI(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AllocENI", reflect.TypeOf((*MockAPIs)(nil).AllocENI), arg0, arg1, arg2, arg3)
}

// AllocIPAddress mocks base method.
func (m *MockAPIs) AllocIPAddress(arg0 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AllocIPAddress", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// AllocIPAddress indicates an expected call of AllocIPAddress.
func (mr *MockAPIsMockRecorder) AllocIPAddress(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AllocIPAddress", reflect.TypeOf((*MockAPIs)(nil).AllocIPAddress), arg0)
}

// AllocIPAddresses mocks base method.
func (m *MockAPIs) AllocIPAddresses(arg0 string, arg1 int) (*ec2.AssignPrivateIpAddressesOutput, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AllocIPAddresses", arg0, arg1)
	ret0, _ := ret[0].(*ec2.AssignPrivateIpAddressesOutput)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AllocIPAddresses indicates an expected call of AllocIPAddresses.
func (mr *MockAPIsMockRecorder) AllocIPAddresses(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AllocIPAddresses", reflect.TypeOf((*MockAPIs)(nil).AllocIPAddresses), arg0, arg1)
}

// AllocIPv6Prefixes mocks base method.
func (m *MockAPIs) AllocIPv6Prefixes(arg0 string) ([]*string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AllocIPv6Prefixes", arg0)
	ret0, _ := ret[0].([]*string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AllocIPv6Prefixes indicates an expected call of AllocIPv6Prefixes.
func (mr *MockAPIsMockRecorder) AllocIPv6Prefixes(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AllocIPv6Prefixes", reflect.TypeOf((*MockAPIs)(nil).AllocIPv6Prefixes), arg0)
}

// DeallocIPAddresses mocks base method.
func (m *MockAPIs) DeallocIPAddresses(arg0 string, arg1 []string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeallocIPAddresses", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeallocIPAddresses indicates an expected call of DeallocIPAddresses.
func (mr *MockAPIsMockRecorder) DeallocIPAddresses(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeallocIPAddresses", reflect.TypeOf((*MockAPIs)(nil).DeallocIPAddresses), arg0, arg1)
}

// DeallocPrefixAddresses mocks base method.
func (m *MockAPIs) DeallocPrefixAddresses(arg0 string, arg1 []string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeallocPrefixAddresses", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeallocPrefixAddresses indicates an expected call of DeallocPrefixAddresses.
func (mr *MockAPIsMockRecorder) DeallocPrefixAddresses(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeallocPrefixAddresses", reflect.TypeOf((*MockAPIs)(nil).DeallocPrefixAddresses), arg0, arg1)
}

// DescribeAllENIs mocks base method.
func (m *MockAPIs) DescribeAllENIs() (awsutils.DescribeAllENIsResult, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DescribeAllENIs")
	ret0, _ := ret[0].(awsutils.DescribeAllENIsResult)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DescribeAllENIs indicates an expected call of DescribeAllENIs.
func (mr *MockAPIsMockRecorder) DescribeAllENIs() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DescribeAllENIs", reflect.TypeOf((*MockAPIs)(nil).DescribeAllENIs))
}

// FetchInstanceTypeLimits mocks base method.
func (m *MockAPIs) FetchInstanceTypeLimits() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FetchInstanceTypeLimits")
	ret0, _ := ret[0].(error)
	return ret0
}

// FetchInstanceTypeLimits indicates an expected call of FetchInstanceTypeLimits.
func (mr *MockAPIsMockRecorder) FetchInstanceTypeLimits() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FetchInstanceTypeLimits", reflect.TypeOf((*MockAPIs)(nil).FetchInstanceTypeLimits))
}

// FreeENI mocks base method.
func (m *MockAPIs) FreeENI(arg0 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FreeENI", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// FreeENI indicates an expected call of FreeENI.
func (mr *MockAPIsMockRecorder) FreeENI(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FreeENI", reflect.TypeOf((*MockAPIs)(nil).FreeENI), arg0)
}

// GetAttachedENIs mocks base method.
func (m *MockAPIs) GetAttachedENIs() ([]awsutils.ENIMetadata, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAttachedENIs")
	ret0, _ := ret[0].([]awsutils.ENIMetadata)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAttachedENIs indicates an expected call of GetAttachedENIs.
func (mr *MockAPIsMockRecorder) GetAttachedENIs() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAttachedENIs", reflect.TypeOf((*MockAPIs)(nil).GetAttachedENIs))
}

// GetENIIPv4Limit mocks base method.
func (m *MockAPIs) GetENIIPv4Limit() int {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetENIIPv4Limit")
	ret0, _ := ret[0].(int)
	return ret0
}

// GetENIIPv4Limit indicates an expected call of GetENIIPv4Limit.
func (mr *MockAPIsMockRecorder) GetENIIPv4Limit() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetENIIPv4Limit", reflect.TypeOf((*MockAPIs)(nil).GetENIIPv4Limit))
}

// GetENILimit mocks base method.
func (m *MockAPIs) GetENILimit() int {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetENILimit")
	ret0, _ := ret[0].(int)
	return ret0
}

// GetENILimit indicates an expected call of GetENILimit.
func (mr *MockAPIsMockRecorder) GetENILimit() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetENILimit", reflect.TypeOf((*MockAPIs)(nil).GetENILimit))
}

// GetIPv4PrefixesFromEC2 mocks base method.
func (m *MockAPIs) GetIPv4PrefixesFromEC2(arg0 string) ([]*ec2.Ipv4PrefixSpecification, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetIPv4PrefixesFromEC2", arg0)
	ret0, _ := ret[0].([]*ec2.Ipv4PrefixSpecification)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetIPv4PrefixesFromEC2 indicates an expected call of GetIPv4PrefixesFromEC2.
func (mr *MockAPIsMockRecorder) GetIPv4PrefixesFromEC2(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetIPv4PrefixesFromEC2", reflect.TypeOf((*MockAPIs)(nil).GetIPv4PrefixesFromEC2), arg0)
}

// GetIPv4sFromEC2 mocks base method.
func (m *MockAPIs) GetIPv4sFromEC2(arg0 string) ([]*ec2.NetworkInterfacePrivateIpAddress, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetIPv4sFromEC2", arg0)
	ret0, _ := ret[0].([]*ec2.NetworkInterfacePrivateIpAddress)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetIPv4sFromEC2 indicates an expected call of GetIPv4sFromEC2.
func (mr *MockAPIsMockRecorder) GetIPv4sFromEC2(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetIPv4sFromEC2", reflect.TypeOf((*MockAPIs)(nil).GetIPv4sFromEC2), arg0)
}

// GetIPv6PrefixesFromEC2 mocks base method.
func (m *MockAPIs) GetIPv6PrefixesFromEC2(arg0 string) ([]*ec2.Ipv6PrefixSpecification, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetIPv6PrefixesFromEC2", arg0)
	ret0, _ := ret[0].([]*ec2.Ipv6PrefixSpecification)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetIPv6PrefixesFromEC2 indicates an expected call of GetIPv6PrefixesFromEC2.
func (mr *MockAPIsMockRecorder) GetIPv6PrefixesFromEC2(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetIPv6PrefixesFromEC2", reflect.TypeOf((*MockAPIs)(nil).GetIPv6PrefixesFromEC2), arg0)
}

// GetInstanceHypervisorFamily mocks base method.
func (m *MockAPIs) GetInstanceHypervisorFamily() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetInstanceHypervisorFamily")
	ret0, _ := ret[0].(string)
	return ret0
}

// GetInstanceHypervisorFamily indicates an expected call of GetInstanceHypervisorFamily.
func (mr *MockAPIsMockRecorder) GetInstanceHypervisorFamily() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetInstanceHypervisorFamily", reflect.TypeOf((*MockAPIs)(nil).GetInstanceHypervisorFamily))
}

// GetInstanceID mocks base method.
func (m *MockAPIs) GetInstanceID() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetInstanceID")
	ret0, _ := ret[0].(string)
	return ret0
}

// GetInstanceID indicates an expected call of GetInstanceID.
func (mr *MockAPIsMockRecorder) GetInstanceID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetInstanceID", reflect.TypeOf((*MockAPIs)(nil).GetInstanceID))
}

// GetInstanceType mocks base method.
func (m *MockAPIs) GetInstanceType() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetInstanceType")
	ret0, _ := ret[0].(string)
	return ret0
}

// GetInstanceType indicates an expected call of GetInstanceType.
func (mr *MockAPIsMockRecorder) GetInstanceType() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetInstanceType", reflect.TypeOf((*MockAPIs)(nil).GetInstanceType))
}

// GetLocalIPv4 mocks base method.
func (m *MockAPIs) GetLocalIPv4() net.IP {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetLocalIPv4")
	ret0, _ := ret[0].(net.IP)
	return ret0
}

// GetLocalIPv4 indicates an expected call of GetLocalIPv4.
func (mr *MockAPIsMockRecorder) GetLocalIPv4() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetLocalIPv4", reflect.TypeOf((*MockAPIs)(nil).GetLocalIPv4))
}

// GetNetworkCards mocks base method.
func (m *MockAPIs) GetNetworkCards() []vpc.NetworkCard {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetNetworkCards")
	ret0, _ := ret[0].([]vpc.NetworkCard)
	return ret0
}

// GetNetworkCards indicates an expected call of GetNetworkCards.
func (mr *MockAPIsMockRecorder) GetNetworkCards() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetNetworkCards", reflect.TypeOf((*MockAPIs)(nil).GetNetworkCards))
}

// GetPrimaryENI mocks base method.
func (m *MockAPIs) GetPrimaryENI() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPrimaryENI")
	ret0, _ := ret[0].(string)
	return ret0
}

// GetPrimaryENI indicates an expected call of GetPrimaryENI.
func (mr *MockAPIsMockRecorder) GetPrimaryENI() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPrimaryENI", reflect.TypeOf((*MockAPIs)(nil).GetPrimaryENI))
}

// GetPrimaryENImac mocks base method.
func (m *MockAPIs) GetPrimaryENImac() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPrimaryENImac")
	ret0, _ := ret[0].(string)
	return ret0
}

// GetPrimaryENImac indicates an expected call of GetPrimaryENImac.
func (mr *MockAPIsMockRecorder) GetPrimaryENImac() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPrimaryENImac", reflect.TypeOf((*MockAPIs)(nil).GetPrimaryENImac))
}

// GetVPCIPv4CIDRs mocks base method.
func (m *MockAPIs) GetVPCIPv4CIDRs() ([]string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetVPCIPv4CIDRs")
	ret0, _ := ret[0].([]string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetVPCIPv4CIDRs indicates an expected call of GetVPCIPv4CIDRs.
func (mr *MockAPIsMockRecorder) GetVPCIPv4CIDRs() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetVPCIPv4CIDRs", reflect.TypeOf((*MockAPIs)(nil).GetVPCIPv4CIDRs))
}

// GetVPCIPv6CIDRs mocks base method.
func (m *MockAPIs) GetVPCIPv6CIDRs() ([]string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetVPCIPv6CIDRs")
	ret0, _ := ret[0].([]string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetVPCIPv6CIDRs indicates an expected call of GetVPCIPv6CIDRs.
func (mr *MockAPIsMockRecorder) GetVPCIPv6CIDRs() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetVPCIPv6CIDRs", reflect.TypeOf((*MockAPIs)(nil).GetVPCIPv6CIDRs))
}

// InitCachedPrefixDelegation mocks base method.
func (m *MockAPIs) InitCachedPrefixDelegation(arg0 bool) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "InitCachedPrefixDelegation", arg0)
}

// InitCachedPrefixDelegation indicates an expected call of InitCachedPrefixDelegation.
func (mr *MockAPIsMockRecorder) InitCachedPrefixDelegation(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "InitCachedPrefixDelegation", reflect.TypeOf((*MockAPIs)(nil).InitCachedPrefixDelegation), arg0)
}

// IsMultiCardENI mocks base method.
func (m *MockAPIs) IsMultiCardENI(arg0 string) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsMultiCardENI", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// IsMultiCardENI indicates an expected call of IsMultiCardENI.
func (mr *MockAPIsMockRecorder) IsMultiCardENI(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsMultiCardENI", reflect.TypeOf((*MockAPIs)(nil).IsMultiCardENI), arg0)
}

// IsPrefixDelegationSupported mocks base method.
func (m *MockAPIs) IsPrefixDelegationSupported() bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsPrefixDelegationSupported")
	ret0, _ := ret[0].(bool)
	return ret0
}

// IsPrefixDelegationSupported indicates an expected call of IsPrefixDelegationSupported.
func (mr *MockAPIsMockRecorder) IsPrefixDelegationSupported() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsPrefixDelegationSupported", reflect.TypeOf((*MockAPIs)(nil).IsPrefixDelegationSupported))
}

// IsPrimaryENI mocks base method.
func (m *MockAPIs) IsPrimaryENI(arg0 string) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsPrimaryENI", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// IsPrimaryENI indicates an expected call of IsPrimaryENI.
func (mr *MockAPIsMockRecorder) IsPrimaryENI(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsPrimaryENI", reflect.TypeOf((*MockAPIs)(nil).IsPrimaryENI), arg0)
}

// IsUnmanagedENI mocks base method.
func (m *MockAPIs) IsUnmanagedENI(arg0 string) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsUnmanagedENI", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// IsUnmanagedENI indicates an expected call of IsUnmanagedENI.
func (mr *MockAPIsMockRecorder) IsUnmanagedENI(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsUnmanagedENI", reflect.TypeOf((*MockAPIs)(nil).IsUnmanagedENI), arg0)
}

// RefreshSGIDs mocks base method.
func (m *MockAPIs) RefreshSGIDs(mac string, store *datastore.DataStore) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RefreshSGIDs", mac, store)
	ret0, _ := ret[0].(error)
	return ret0
}

// RefreshSGIDs indicates an expected call of RefreshSGIDs.
func (mr *MockAPIsMockRecorder) RefreshSGIDs(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RefreshSGIDs", reflect.TypeOf((*MockAPIs)(nil).RefreshSGIDs), arg0)
}

// SetMultiCardENIs mocks base method.
func (m *MockAPIs) SetMultiCardENIs(arg0 []string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetMultiCardENIs", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// SetMultiCardENIs indicates an expected call of SetMultiCardENIs.
func (mr *MockAPIsMockRecorder) SetMultiCardENIs(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetMultiCardENIs", reflect.TypeOf((*MockAPIs)(nil).SetMultiCardENIs), arg0)
}

// SetUnmanagedENIs mocks base method.
func (m *MockAPIs) SetUnmanagedENIs(arg0 []string) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetUnmanagedENIs", arg0)
}

// SetUnmanagedENIs indicates an expected call of SetUnmanagedENIs.
func (mr *MockAPIsMockRecorder) SetUnmanagedENIs(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetUnmanagedENIs", reflect.TypeOf((*MockAPIs)(nil).SetUnmanagedENIs), arg0)
}

// TagENI mocks base method.
func (m *MockAPIs) TagENI(arg0 string, arg1 map[string]string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "TagENI", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// TagENI indicates an expected call of TagENI.
func (mr *MockAPIsMockRecorder) TagENI(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "TagENI", reflect.TypeOf((*MockAPIs)(nil).TagENI), arg0, arg1)
}

// WaitForENIAndIPsAttached mocks base method.
func (m *MockAPIs) WaitForENIAndIPsAttached(arg0 string, arg1 int) (awsutils.ENIMetadata, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WaitForENIAndIPsAttached", arg0, arg1)
	ret0, _ := ret[0].(awsutils.ENIMetadata)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// WaitForENIAndIPsAttached indicates an expected call of WaitForENIAndIPsAttached.
func (mr *MockAPIsMockRecorder) WaitForENIAndIPsAttached(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WaitForENIAndIPsAttached", reflect.TypeOf((*MockAPIs)(nil).WaitForENIAndIPsAttached), arg0, arg1)
}
