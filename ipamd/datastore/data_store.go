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
	"sync"
	"time"

	log "github.com/cihub/seelog"
	"github.com/pkg/errors"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
)

const (
	minLifeTime          = 1 * time.Minute
	addressCoolingPeriod = 1 * time.Minute
	// DuplicatedENIError is an error when caller tries to add an duplicate eni to data store
	DuplicatedENIError = "data store: duplicate eni"

	// DuplicateIPError is an error when caller tries to add an duplicate ip address to data store
	DuplicateIPError = "datastore: duplicated IP"
)

// ENIIPPool contains ENI/IP Pool information
type ENIIPPool struct {
	createTime            time.Time
	lastUnassignedTime    time.Time
	isPrimary             bool
	id                    string
	deviceNumber          int
	assignedIPv4Addresses int
	ipv4Addresses         map[string]*AddressInfo
}

// AddressInfo contains inforation about an IP
type AddressInfo struct {
	address        string
	assigned       bool // true if it is assigned to a Pod
	unAssignedTime time.Time
}

// PodKey is used to locate Pod IP
type PodKey struct {
	name      string
	namespace string
}

// PodIPInfo contains Pod's IP and the device number of the ENI
type PodIPInfo struct {
	// IP is the ip address of pod
	IP string
	// DeviceNumber is the device number of  pod
	DeviceNumber int
}

// DataStore contains node level ENI/IP
type DataStore struct {
	total      int
	assigned   int
	eniIPPools map[string]*ENIIPPool
	podsIP     map[PodKey]PodIPInfo
	lock       sync.RWMutex
}

// NewDataStore returns DataStore structure
func NewDataStore() *DataStore {
	return &DataStore{
		eniIPPools: make(map[string]*ENIIPPool),
		podsIP:     make(map[PodKey]PodIPInfo),
	}
}

// AddENI add eni to data store
func (ds *DataStore) AddENI(eniID string, deviceNumber int, isPrimary bool) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	log.Debug("DataStore Add an ENI ", eniID)

	_, ok := ds.eniIPPools[eniID]
	if ok {
		return errors.New(DuplicatedENIError)
	}
	ds.eniIPPools[eniID] = &ENIIPPool{
		createTime:    time.Now(),
		isPrimary:     isPrimary,
		id:            eniID,
		deviceNumber:  deviceNumber,
		ipv4Addresses: make(map[string]*AddressInfo)}
	return nil
}

// AddENIIPv4Address add an IP of an ENI to data store
func (ds *DataStore) AddENIIPv4Address(eniID string, ipv4 string) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	log.Debugf("Adding ENI(%s)'s IPv4 address %s to datastore", eniID, ipv4)
	log.Debugf("IP Address Pool stats: total: %d, assigned: %d",
		ds.total, ds.assigned)

	curENI, ok := ds.eniIPPools[eniID]
	if !ok {
		return errors.New("add eni's ip to datastore: unknown eni")
	}

	_, ok = curENI.ipv4Addresses[ipv4]
	if ok {
		return errors.New(DuplicateIPError)
	}

	ds.total++

	curENI.ipv4Addresses[ipv4] = &AddressInfo{address: ipv4, assigned: false}

	log.Infof("Added eni(%s)'s ip %s to datastore", eniID, ipv4)

	return nil
}

// AssignPodIPv4Address assigns an IPv4 address to Pod
// It returns the assigned IPv4 address, device number, error
func (ds *DataStore) AssignPodIPv4Address(k8sPod *k8sapi.K8SPodInfo) (string, int, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	log.Debugf("AssignIPv4Address: IP address pool stats: total:%d, assigned %d",
		ds.total, ds.assigned)
	podKey := PodKey{
		name:      k8sPod.Name,
		namespace: k8sPod.Namespace,
	}
	ipAddr, ok := ds.podsIP[podKey]
	if ok {
		if ipAddr.IP == k8sPod.IP && k8sPod.IP != "" {
			// The caller invoke multiple times to assign(PodName/NameSpace --> same IPAddress). It is not a error, but not very efficient.
			log.Infof("AssignPodIPv4Address: duplicate pod assign for ip %s, name %s, namespace %s",
				k8sPod.IP, k8sPod.Name, k8sPod.Namespace)
			return ipAddr.IP, ipAddr.DeviceNumber, nil
		}
		//TODO handle this bug assert?, may need to add a counter here, if counter is too high, need to mark node as unhealthy...
		// this is a bug that the caller invoke multiple times to assign(PodName/NameSpace -> a different IPaddress).
		log.Errorf("AssignPodIPv4Address:  current ip %s is changed to ip %s for POD(name %s, namespace %s)",
			ipAddr, k8sPod.IP, k8sPod.Name, k8sPod.Namespace)
		return "", 0, errors.New("datastore; invalid pod with multiple IP addresses")

	}

	return ds.assignPodIPv4AddressUnsafe(k8sPod)
}

// It returns the assigned IPv4 address, device number, error
func (ds *DataStore) assignPodIPv4AddressUnsafe(k8sPod *k8sapi.K8SPodInfo) (string, int, error) {
	podKey := PodKey{
		name:      k8sPod.Name,
		namespace: k8sPod.Namespace,
	}
	for _, eni := range ds.eniIPPools {
		if (k8sPod.IP == "") && (len(eni.ipv4Addresses) == eni.assignedIPv4Addresses) {
			// skip this eni, since it has no available ip address
			log.Debugf("AssignPodIPv4Address, skip ENI %s that do not have available addresses", eni.id)
			continue
		}
		for _, addr := range eni.ipv4Addresses {
			if k8sPod.IP == addr.address {
				// After L-IPAM restart and built IP warm-pool, it needs to take the existing running POD IP out of the pool.
				if !addr.assigned {
					ds.assigned++
					eni.assignedIPv4Addresses++
					addr.assigned = true
				}
				ds.podsIP[podKey] = PodIPInfo{IP: addr.address, DeviceNumber: eni.deviceNumber}
				log.Infof("AssignPodIPv4Address Reassign IP %v to Pod (name %s, namespace %s)",
					addr.address, k8sPod.Name, k8sPod.Namespace)
				return addr.address, eni.deviceNumber, nil
			}
			if !addr.assigned && k8sPod.IP == "" {
				// This is triggered by a POD's Add Network command from CNI plugin
				ds.assigned++
				eni.assignedIPv4Addresses++
				addr.assigned = true
				ds.podsIP[podKey] = PodIPInfo{IP: addr.address, DeviceNumber: eni.deviceNumber}
				log.Infof("AssignPodIPv4Address Assign IP %v to Pod (name %s, namespace %s)",
					addr.address, k8sPod.Name, k8sPod.Namespace)
				return addr.address, eni.deviceNumber, nil
			}
		}

	}

	log.Infof("DataStore has no available IP addresses")

	return "", 0, errors.New("datastore: no available IP addresses")
}

// GetStats returns statistics
// it returns total number of IP addresses, number of assigned IP addresses
func (ds *DataStore) GetStats() (int, int) {
	return ds.total, ds.assigned
}

func (ds *DataStore) getDeletableENI() *ENIIPPool {
	for _, eni := range ds.eniIPPools {
		if eni.isPrimary {
			continue
		}

		if time.Now().Sub(eni.createTime) < minLifeTime {
			continue
		}

		if time.Now().Sub(eni.lastUnassignedTime) < addressCoolingPeriod {
			continue
		}

		if eni.assignedIPv4Addresses != 0 {
			continue
		}

		log.Debugf("FreeENI: found a deletable ENI %s", eni.id)
		return eni
	}
	return nil
}

// FreeENI free a deletable ENI.
// It returns the name of ENI which is deleted out data store
func (ds *DataStore) FreeENI() (string, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	deletableENI := ds.getDeletableENI()
	if deletableENI == nil {
		log.Debugf("FreeENI: no deletable ENI")
		return "", errors.New("free eni: none of enis can be deleted at this time")
	}

	ds.total -= len(ds.eniIPPools[deletableENI.id].ipv4Addresses)
	ds.assigned -= deletableENI.assignedIPv4Addresses
	log.Debugf("FreeENI: IP address pool stats: free %d addresses, total: %d, assigned: %d",
		len(ds.eniIPPools[deletableENI.id].ipv4Addresses), ds.total, ds.assigned)
	deletedENI := deletableENI.id
	delete(ds.eniIPPools, deletableENI.id)

	return deletedENI, nil
}

// UnAssignPodIPv4Address a) find out the IP address based on PodName and PodNameSpace
// b)  mark IP address as unassigned c) returns IP address, eni's device number, error
func (ds *DataStore) UnAssignPodIPv4Address(k8sPod *k8sapi.K8SPodInfo) (string, int, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	log.Debugf("UnAssignIPv4Address: IP address pool stats: total:%d, assigned %d, Pod(Name: %s, Namespace: %s)",
		ds.total, ds.assigned, k8sPod.Name, k8sPod.Namespace)

	podKey := PodKey{
		name:      k8sPod.Name,
		namespace: k8sPod.Namespace,
	}
	ipAddr, ok := ds.podsIP[podKey]
	if !ok {
		return "", 0, errors.Errorf("datastore: unknown pod name %s namespace %s", podKey.name, podKey.namespace)
	}

	for _, eni := range ds.eniIPPools {
		ip, ok := eni.ipv4Addresses[ipAddr.IP]
		if ok && ip.assigned {
			ip.assigned = false
			ds.assigned--
			eni.assignedIPv4Addresses--
			curTime := time.Now()
			ip.unAssignedTime = curTime
			eni.lastUnassignedTime = curTime
			log.Infof("UnAssignIPv4Address: Pod (Name: %s, NameSpace %s)'s ipAddr %s, DeviceNumber%d",
				k8sPod.Name, k8sPod.Namespace, ip.address, eni.deviceNumber)
			delete(ds.podsIP, podKey)
			return ip.address, eni.deviceNumber, nil
		}
	}

	return "", 0, errors.Errorf("datastore: unknown pod name %s namespace %s", podKey.name, podKey.namespace)
}
