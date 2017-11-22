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

package k8sapi

import (
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"k8s.io/kubernetes/pkg/api"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/httpwrapper/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ioutilwrapper/mocks"
)

const (
	pod1IP = "10.0.10.10"
	pod2IP = "10.0.20.20"
	testIP = "10.10.0.1"
)

func setup(t *testing.T) (*gomock.Controller,
	*mock_ioutilwrapper.MockIOUtil,
	*mock_httpwrapper.MockHTTP) {
	ctrl := gomock.NewController(t)
	return ctrl,
		mock_ioutilwrapper.NewMockIOUtil(ctrl),
		mock_httpwrapper.NewMockHTTP(ctrl)
}

type mockHTTPResp struct{}

func (*mockHTTPResp) Read([]byte) (int, error) {
	return 0, nil
}

func (*mockHTTPResp) Close() error {
	return nil
}

func NewmockHTTPResp() io.ReadCloser {
	return &mockHTTPResp{}
}

func TestK8SGetLocalPodIPs(t *testing.T) {
	ctrl, mocksIOUtil, mocksHTTP := setup(t)
	defer ctrl.Finish()

	resp := NewmockHTTPResp()

	mocksHTTP.EXPECT().Get(kubeletURL).Return(resp, nil)

	pod1 := api.Pod{Status: api.PodStatus{PodIP: pod1IP}}
	pod2 := api.Pod{Status: api.PodStatus{PodIP: pod2IP}}
	testResp := &api.PodList{Items: []api.Pod{pod1, pod2}}

	testRespByte, _ := json.Marshal(testResp)
	mocksIOUtil.EXPECT().ReadAll(gomock.Any()).Return(testRespByte, nil)

	podsInfo, err := k8sGetLocalPodIPs(mocksHTTP, mocksIOUtil, testIP)

	assert.NoError(t, err)
	assert.Equal(t, len(podsInfo), 2)
}

func TestK8SGetLocalPodIPsErrGETLocal(t *testing.T) {
	ctrl, mocksIOUtil, mocksHTTP := setup(t)
	defer ctrl.Finish()

	mocksHTTP.EXPECT().Get(kubeletURL).Return(nil, errors.New("Err on HTTP.get localHost"))
	mocksHTTP.EXPECT().Get(kubeletURLPrefix+testIP+kubeletURLSurfix).Return(nil, errors.New("Err on HTTP.get by IP"))

	_, err := k8sGetLocalPodIPs(mocksHTTP, mocksIOUtil, testIP)

	assert.Error(t, err)

}

func TestK8SGetLocalPodIPsErrRead(t *testing.T) {
	ctrl, mocksIOUtil, mocksHTTP := setup(t)
	defer ctrl.Finish()

	resp := NewmockHTTPResp()

	mocksHTTP.EXPECT().Get(kubeletURL).Return(resp, nil)
	mocksIOUtil.EXPECT().ReadAll(gomock.Any()).Return(nil, errors.New("Err on Readall"))

	_, err := k8sGetLocalPodIPs(mocksHTTP, mocksIOUtil, testIP)

	assert.Error(t, err)
}
