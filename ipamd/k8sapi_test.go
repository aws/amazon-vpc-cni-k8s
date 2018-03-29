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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/api/core/v1"
)

const (
	pod1IP = "10.0.10.10"
	pod2IP = "10.0.20.20"
	testIP = "10.10.0.1"
)

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

type mockGetter struct {
	fn func(url string) (resp *http.Response, err error)
}

func (g mockGetter) Get(url string) (resp *http.Response, err error) {
	return g.fn(url)
}

type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

func TestK8SGetLocalPodIPs(t *testing.T) {
	pod1 := v1.Pod{Status: v1.PodStatus{PodIP: pod1IP}}
	pod2 := v1.Pod{Status: v1.PodStatus{PodIP: pod2IP}}
	testResp := &v1.PodList{Items: []v1.Pod{pod1, pod2}}
	testRespByte, _ := json.Marshal(testResp)

	podsInfo, err := k8sGetLocalPodIPs(mockGetter{func(url string) (resp *http.Response, err error) {
		if url == fmt.Sprintf(kubeletPods, "localhost") {
			return &http.Response{
				Body: nopCloser{bytes.NewBuffer(testRespByte)},
			}, nil
		}
		return nil, nil
	}}, testIP)

	// TODO (tvi): reenable
	assert.NoError(t, err)
	assert.Equal(t, len(podsInfo), 2)
}

func TestK8SGetLocalPodIPsErrGETLocal(t *testing.T) {
	_, err := k8sGetLocalPodIPs(mockGetter{func(url string) (resp *http.Response, err error) {
		if url == fmt.Sprintf(kubeletPods, "localhost") {
			return nil, errors.New("Err on HTTP.get localHost")
		} else if url == fmt.Sprintf(kubeletPods, testIP) {
			return nil, errors.New("Err on HTTP.get by IP")
		}
		return nil, nil
	}}, testIP)

	assert.Error(t, err)
}

func TestK8SGetLocalPodIPsFallback(t *testing.T) {
	pod1 := v1.Pod{Status: v1.PodStatus{PodIP: pod1IP}}
	pod2 := v1.Pod{Status: v1.PodStatus{PodIP: pod2IP}}
	testResp := &v1.PodList{Items: []v1.Pod{pod1, pod2}}
	testRespByte, _ := json.Marshal(testResp)

	podsInfo, err := k8sGetLocalPodIPs(mockGetter{func(url string) (resp *http.Response, err error) {
		if url == fmt.Sprintf(kubeletPods, "localhost") {
			return nil, errors.New("Err on HTTP.get localHost")
		} else if url == fmt.Sprintf(kubeletPods, testIP) {
			return &http.Response{
				Body: nopCloser{bytes.NewBuffer(testRespByte)},
			}, nil
		}
		return nil, nil
	}}, testIP)

	assert.NoError(t, err)
	assert.Equal(t, len(podsInfo), 2)
}

func TestK8SGetLocalPodIPsErrRead(t *testing.T) {
	_, err := k8sGetLocalPodIPs(mockGetter{func(url string) (resp *http.Response, err error) {
		if url == fmt.Sprintf(kubeletPods, "localhost") {
			return &http.Response{
				Body: nopCloser{bytes.NewBufferString("randomNoise")},
			}, nil
		}
		return nil, nil
	}}, testIP)

	assert.Error(t, err)
}
