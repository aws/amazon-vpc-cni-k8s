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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/pkg/errors"
	"k8s.io/api/core/v1"
)

const (
	// The kubelet has an internal HTTP server. It serves a read-only view at port 10255.
	// It can return a list of running pods at /pods
	kubeletPods = "http://%v:10255/pods"
)

// k8sPodInfo provides pod info
type k8sPodInfo struct {
	// Name is pod's name
	Name string
	// Namespace is pod's namespace
	Namespace string
	// IP is pod's ipv4 address
	IP string
}

type getter interface {
	Get(url string) (resp *http.Response, err error)
}

func k8sGetLocalPodIPs(http getter, localIP string) ([]*k8sPodInfo, error) {
	resp, err := http.Get(fmt.Sprintf(kubeletPods, "localhost"))
	if err != nil {
		resp, err = http.Get(fmt.Sprintf(kubeletPods, localIP))
		if err != nil {
			return nil, errors.Wrap(err, "kubelet: failed to query kubelet")
		}
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "kubelet: failed to read response body")
	}

	podlist := v1.PodList{}
	if err = json.Unmarshal(body, &podlist); err != nil {
		return nil, errors.Wrap(err, "kubelet: failed to unmarshal pod list")
	}

	podsInfo := []*k8sPodInfo{}
	for _, pod := range podlist.Items {
		if pod.Status.PodIP != "" {
			podsInfo = append(podsInfo, &k8sPodInfo{
				Name:      pod.Name,
				Namespace: pod.Namespace,
				IP:        pod.Status.PodIP,
			})
		}
	}

	return podsInfo, nil
}
