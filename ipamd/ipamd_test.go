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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/ipamd/eni"
	"github.com/aws/amazon-vpc-cni-k8s/ipamd/metrics"
)

type mockNetworkAPIs struct {
	hn   func(vpcCIDR *net.IPNet, primaryAddr *net.IP) error
	enin func(eniIP string, mac string, table int, subnetCIDR string) error
}

func (m *mockNetworkAPIs) SetupHostNetwork(vpcCIDR *net.IPNet, primaryAddr *net.IP) error {
	return m.hn(vpcCIDR, primaryAddr)
}

func (m *mockNetworkAPIs) SetupENINetwork(eniIP string, mac string, table int, subnetCIDR string) error {
	return m.enin(eniIP, mac, table, subnetCIDR)
}

type mockENI struct {
	add  func() (string, bool, error)
	free func(string) error
}

func (m *mockENI) AddENI(context.Context) (string, bool, error) {
	return m.add()
}
func (m *mockENI) FreeENI(_ context.Context, e string) error {
	return m.free(e)
}
func (m *mockENI) IsENIReady(context.Context, string) (bool, error) {
	return true, nil
}
func (m *mockENI) GetENIs(context.Context) ([]string, error) {
	return []string{eniID}, nil
}
func (m *mockENI) GetENIMetadata(context.Context, string) (eni.ENIMetadata, error) {
	return eni.ENIMetadata{
		MAC:          primaryMAC,
		PrimaryIP:    ipaddr01,
		SecondaryIPs: []string{},
		Device:       primaryDevice,
		CIDR:         vpcCIDR,
	}, nil
}
func (m *mockENI) GetPrimaryDetails(ctx context.Context) (string, string, string, error) {
	return vpcCIDR, ipaddr01, eniID, nil
}
func (m *mockENI) GetMaxIPs() int64 {
	return 5
}

const (
	primaryENIid     = "eni-00000000"
	secENIid         = "eni-00000001"
	testAttachmentID = "eni-00000000-attach"
	eniID            = "eni-5731da78"
	primaryMAC       = "12:ef:2a:98:e5:5a"
	secMAC           = "12:ef:2a:98:e5:5b"
	primaryDevice    = 2
	secDevice        = 0
	primarySubnet    = "10.10.10.0/24"
	secSubnet        = "10.10.20.0/24"
	ipaddr01         = "10.10.10.11"
	ipaddr02         = "10.10.10.12"
	ipaddr11         = "10.10.20.11"
	ipaddr12         = "10.10.20.12"
	vpcCIDR          = "10.10.0.0/16"
)

func TestNodeInitError(t *testing.T) {
	m, _ := metrics.New()
	store := datastore.NewDatastore(m)
	mockContext := &IPAMD{
		awsClient: &mockENI{func() (string, bool, error) {
			return "", true, nil
		}, func(string) error {
			return nil
		}},
		podClient: mockGetter{func(url string) (resp *http.Response, err error) {
			return nil, fmt.Errorf("error")
		}},
		networkClient: &mockNetworkAPIs{
			func(vpcCIDR *net.IPNet, primaryAddr *net.IP) error { return nil },
			func(eniIP string, mac string, table int, subnetCIDR string) error { return nil },
		},
		metrics:   m,
		dataStore: store,
	}

	err := mockContext.init(context.Background())
	assert.Error(t, err)
}

func TestNodeInit(t *testing.T) {
	tests := []struct {
		pods  []v1.Pod
		ipamd *IPAMD
		ret   func(*IPAMD)
	}{
		{
			pods: []v1.Pod{
				v1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "default"},
					Status:     v1.PodStatus{PodIP: pod1IP},
				},
				v1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "default"},
					Status:     v1.PodStatus{PodIP: pod2IP},
				},
			},
			ipamd: &IPAMD{
				awsClient: &mockENI{func() (string, bool, error) {
					return "eni-neweni", false, nil
				}, func(string) error {
					return nil
				}},
				networkClient: &mockNetworkAPIs{
					func(vpcCIDR *net.IPNet, primaryAddr *net.IP) error { return nil },
					func(eniIP string, mac string, table int, subnetCIDR string) error { return nil },
				},
			},
			ret: func(mockContext *IPAMD) {
				err := mockContext.init(context.Background())
				assert.NoError(t, err)
				mockContext.increaseIPPool(context.Background())
				mockContext.decreaseIPPool(context.Background())
			},
		},
		{
			pods: nil,
			ipamd: &IPAMD{
				awsClient: &mockENI{func() (string, bool, error) {
					return "eni-neweni", false, nil
				}, func(string) error {
					return nil
				}},
				networkClient: &mockNetworkAPIs{
					func(vpcCIDR *net.IPNet, primaryAddr *net.IP) error { return nil },
					func(eniIP string, mac string, table int, subnetCIDR string) error { return nil },
				},
			},
			ret: func(mockContext *IPAMD) {
				err := mockContext.init(context.Background())
				assert.NoError(t, err)
				mockContext.increaseIPPool(context.Background())
				mockContext.decreaseIPPool(context.Background())
			},
		},
		{
			pods: nil,
			ipamd: &IPAMD{
				awsClient: &mockENI{func() (string, bool, error) {
					return "", true, nil
				}, func(string) error {
					return nil
				}},
				networkClient: &mockNetworkAPIs{
					func(vpcCIDR *net.IPNet, primaryAddr *net.IP) error { return nil },
					func(eniIP string, mac string, table int, subnetCIDR string) error { return nil },
				},
			},
			ret: func(mockContext *IPAMD) {
				err := mockContext.init(context.Background())
				assert.NoError(t, err)
				mockContext.increaseIPPool(context.Background())
				mockContext.decreaseIPPool(context.Background())
			},
		},
		{
			pods: nil,
			ipamd: &IPAMD{
				awsClient: &mockENI{func() (string, bool, error) {
					return "", false, fmt.Errorf("testerror")
				}, func(string) error {
					return nil
				}},
				networkClient: &mockNetworkAPIs{
					func(vpcCIDR *net.IPNet, primaryAddr *net.IP) error { return nil },
					func(eniIP string, mac string, table int, subnetCIDR string) error { return nil },
				},
			},
			ret: func(mockContext *IPAMD) {
				err := mockContext.init(context.Background())
				assert.NoError(t, err)
				mockContext.increaseIPPool(context.Background())
				mockContext.decreaseIPPool(context.Background())
			},
		},
		{
			pods: nil,
			ipamd: &IPAMD{
				awsClient: &mockENI{func() (string, bool, error) {
					return "", true, nil
				}, func(string) error {
					return fmt.Errorf("testerror")
				}},
				networkClient: &mockNetworkAPIs{
					func(vpcCIDR *net.IPNet, primaryAddr *net.IP) error { return nil },
					func(eniIP string, mac string, table int, subnetCIDR string) error { return nil },
				},
			},
			ret: func(mockContext *IPAMD) {
				err := mockContext.init(context.Background())
				assert.NoError(t, err)
				mockContext.increaseIPPool(context.Background())
				mockContext.decreaseIPPool(context.Background())
			},
		},
	}

	for _, test := range tests {
		testRespByte, _ := json.Marshal(&v1.PodList{Items: test.pods})
		m, _ := metrics.New()
		store := datastore.NewDatastore(m)
		test.ipamd.metrics = m
		test.ipamd.dataStore = store
		test.ipamd.podClient = mockGetter{func(url string) (resp *http.Response, err error) {
			return &http.Response{
				Body: nopCloser{bytes.NewBuffer(testRespByte)},
			}, nil
		}}
		test.ret(test.ipamd)
	}
}
