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

package datastore

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd/metrics"
)

const (
	minLifeTime          = 1 * time.Minute
	addressCoolingPeriod = 1 * time.Minute
)

var (
	// ErrUnknownPod is an error when there is no pod in data store matching pod name, namespace, container id
	ErrUnknownPod = errors.New("datastore: unknown pod")

	// ErrUnknownPodIP is an error where pod's IP address is not found in data store
	ErrUnknownPodIP = errors.New("datastore: pod using unknown IP address")

	// DuplicatedENIError is an error when caller tries to add an duplicate ENI to data store
	ErrDuplicateENI = errors.New("datastore: duplicated ENI")

	// DuplicateIPError is an error when caller tries to add an duplicate IP address to data store
	ErrDuplicateIP = errors.New("datastore: duplicated IP")
)

type Datastore interface {
	AddENI(eniID string, deviceNumber int, isPrimary bool) error
	AddIPAddr(eniID string, ipv4 string) error
	FreeENI() (string, error)

	// get total ip addrs, assigned ips addrs, and number of enis
	GetStats() (int, int, int)

	AssignPodIP(ctx context.Context, name, namespace string) (*IPAddr, error)
	UnassignPodIP(ctx context.Context, name, namespace string) (*IPAddr, error)

	ReconstructPodIP(name, namespace, ipaddr string) error

	String() string
}

type ENI struct {
	ID      string
	Primary bool
	Device  int

	Addrs   []*IPAddr
	AddrMap map[string]*IPAddr

	Created time.Time
	Updated time.Time
}

func (e *ENI) AssignedIPs() int {
	ret := 0
	for _, ipaddr := range e.Addrs {
		if ipaddr.Assigned {
			ret += 1
		}
	}
	return ret
}

type IPAddr struct {
	Assigned bool
	IP       net.IP
	ENI      *ENI

	Created time.Time
	Updated time.Time
}

type Pod struct {
	Name      string
	Namespace string

	IPAddr *IPAddr
}

// DataStore contains node level ENI/IP
type memStore struct {
	total int

	enis   []*ENI
	eniMap map[string]*ENI

	pods   []*Pod
	podMap map[string]*Pod

	lock    sync.RWMutex
	metrics *metrics.Metrics
}

func (ms *memStore) String() string {
	out := ""
	for _, e := range ms.enis {
		out += fmt.Sprintln("\t-----")
		out += fmt.Sprintln("\tENI")
		out += fmt.Sprintln("\tID: ", e.ID)
		out += fmt.Sprintln("\tPrimary: ", e.Primary)
		out += fmt.Sprintln("\tDevice: ", e.Device)
		for _, a := range e.Addrs {
			out += fmt.Sprintln("\t\tIP: ", a.IP.String(), "Assigned: ", a.Assigned)
		}
	}
	return out
}

func (ms *memStore) assignedIPs() int {
	ret := 0
	for _, eni := range ms.enis {
		ret += eni.AssignedIPs()
	}
	return ret
}

// GetStats returns statistics total number of IP addresses,
// number of assigned IP addresses and number of ENIs.
func (ms *memStore) GetStats() (int, int, int) {
	ms.lock.RLock()
	defer ms.lock.RUnlock()
	return ms.total, ms.assignedIPs(), len(ms.enis)
}

// NewDatastore returns DataStore structure
func NewDatastore(metrics *metrics.Metrics) *memStore {
	return &memStore{
		enis:    make([]*ENI, 0),
		eniMap:  make(map[string]*ENI),
		pods:    make([]*Pod, 0),
		podMap:  make(map[string]*Pod),
		lock:    sync.RWMutex{},
		metrics: metrics,
	}
}

// AddENI add ENI to data store
func (ms *memStore) AddENI(eniID string, device int, primary bool) error {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	log.Debug("datastore: Add an ENI ", eniID)
	if _, ok := ms.eniMap[eniID]; ok {
		return ErrDuplicateENI
	}

	now := time.Now()
	eni := &ENI{
		ID:      eniID,
		Primary: primary,
		Device:  device,
		Addrs:   []*IPAddr{},
		AddrMap: map[string]*IPAddr{},
		Created: now,
		Updated: now,
	}
	ms.eniMap[eniID] = eni
	ms.enis = append(ms.enis, eni)
	ms.metrics.AddENI()
	return nil
}

// AddENIIPv4Address add an IP of an ENI to data store
func (ms *memStore) AddIPAddr(eniID string, ipv4 string) error {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	log.Debugf("datastore: Adding ENI(%s)'s IPv4 address %s to datastore", eniID, ipv4)
	cENI, ok := ms.eniMap[eniID]
	if !ok {
		return errors.New("add ENI's IP to datastore: unknown ENI")
	}

	if _, ok := cENI.AddrMap[ipv4]; ok {
		return ErrDuplicateIP
	}

	ms.total++
	now := time.Now()
	addr := &IPAddr{
		Assigned: false,
		IP:       net.ParseIP(ipv4), // TODO
		ENI:      cENI,
		Created:  now,
		Updated:  now,
	}
	cENI.Addrs = append(cENI.Addrs, addr)
	cENI.AddrMap[ipv4] = addr
	ms.metrics.AddIP()
	return nil
}

// ReconstructPodIP assigns ips to pod after ipamd has crashed.
func (ms *memStore) ReconstructPodIP(name, namespace, ipaddr string) error {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	ip := net.ParseIP(ipaddr)
	for _, eni := range ms.enis {
		for _, addr := range eni.Addrs {
			if addr.IP.Equal(ip) {
				log.Debugf("Reconstructed IP %v %v %v", name, namespace, ipaddr)
				pod := &Pod{
					Name:      name,
					Namespace: namespace,
					IPAddr:    addr,
				}
				ms.podMap[name+"_"+namespace] = pod
				ms.pods = append(ms.pods, pod)
				now := time.Now()
				addr.Assigned = true
				addr.Updated = now
				addr.ENI.Updated = now
				ms.metrics.AssignPodIP()
				return nil
			}
		}
	}

	log.Warnf("Datastore could not reconstruct IP address. It was not added during ENI setup.")
	return fmt.Errorf("could not reconstruct pod ip: %v, %v, %v", name, namespace, ipaddr)
}

// AssignPodIPv4Address assigns an IPv4 address to pod
// It returns the assigned IPv4 address, device number, error
func (ms *memStore) AssignPodIP(ctx context.Context, name, namespace string) (*IPAddr, error) {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	pod := name + "_" + namespace
	if ipAddr, ok := ms.podMap[pod]; ok {
		return ipAddr.IPAddr, nil
	}
	for _, eni := range ms.enis {
		for _, addr := range eni.Addrs {
			if !addr.Assigned {
				pod := &Pod{
					Name:      name,
					Namespace: namespace,
					IPAddr:    addr,
				}
				ms.podMap[name+"_"+namespace] = pod
				ms.pods = append(ms.pods, pod)
				now := time.Now()
				addr.Assigned = true
				addr.Updated = now
				addr.ENI.Updated = now
				ms.metrics.AssignPodIP()
				return addr, nil
			}
		}
	}

	log.Warnf("Datastore has no available IP addresses")
	return nil, fmt.Errorf("could not assign pod ip: %v, %v", name, namespace)
}

// UnAssignPodIPv4Address
// a) find out the IP address based on PodName and PodNameSpace
// b)  mark IP address as unassigned
// c) returns IP address, ENI's device number, error
func (ms *memStore) UnassignPodIP(ctx context.Context, name, namespace string) (*IPAddr, error) {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	pod, ok := ms.podMap[name+"_"+namespace]
	if !ok {
		return nil, ErrUnknownPod
	}

	now := time.Now()
	ipaddr := pod.IPAddr
	ipaddr.Assigned = false
	ipaddr.Updated = now
	ipaddr.ENI.Updated = now

	delete(ms.podMap, name+"_"+namespace)
	for i, p := range ms.pods {
		if p == pod {
			ms.pods = append(ms.pods[:i], ms.pods[i+1:]...)
			break
		}
	}

	ms.metrics.UnassignPodIP()
	return ipaddr, nil
}

func (ms *memStore) getDeletableENI() *ENI {
	for _, eni := range ms.enis {
		if eni.Primary {
			continue
		}

		if time.Now().Sub(eni.Created) < minLifeTime {
			continue
		}

		if time.Now().Sub(eni.Updated) < addressCoolingPeriod {
			continue
		}

		if eni.AssignedIPs() > 0 {
			continue
		}

		log.Infof("FreeENI: found a deletable ENI %s", eni.ID)
		return eni
	}
	return nil
}

// FreeENI free a deletable ENI.
// It returns the name of ENI which is deleted out data store
func (ms *memStore) FreeENI() (string, error) {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	dENI := ms.getDeletableENI()
	if dENI == nil {
		return "", errors.New("free ENI: no ENI can be deleted at this time")
	}

	ms.total -= len(ms.eniMap[dENI.ID].Addrs)
	deletedENI := dENI.ID
	delete(ms.eniMap, dENI.ID)
	for i, e := range ms.enis {
		if e == dENI {
			ms.enis = append(ms.enis[:i], ms.enis[i+1:]...)
			break
		}
	}
	ms.metrics.FreeENI(len(dENI.AddrMap))
	return deletedENI, nil
}
