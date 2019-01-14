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
	"github.com/prometheus/client_golang/prometheus"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
)

const (
	minLifeTime = 1 * time.Minute
	// addressENICoolingPeriod is used to ensure ENI will NOT get freed back to EC2 control plane if one of
	// its secondary IP addresses is used for a Pod within last addressENICoolingPeriod
	addressENICoolingPeriod = 1 * time.Minute

	// addressCoolingPeriod is used to ensure an IP not get assigned to a Pod if this IP is used by a different Pod
	// in addressCoolingPeriod
	addressCoolingPeriod = 30 * time.Second
	// DuplicatedENIError is an error when caller tries to add an duplicate ENI to data store
	DuplicatedENIError = "data store: duplicate ENI"

	// DuplicateIPError is an error when caller tries to add an duplicate IP address to data store
	DuplicateIPError = "datastore: duplicated IP"

	// UnknownIPError is an error when caller tries to delele an IP which is unknown to data store
	UnknownIPError = "datastore: unknown IP"

	// IPInUseError is an error when caller tries to delete an IP where IP is still assigned to a Pod
	IPInUseError = "datastore: IP is used and can not be deleted"

	// ENIInUseError is an error when caller tries to delete an ENI where there are IP still assigned to a pod
	ENIInUseError = "datastore: ENI is used and can not be deleted"

	// UnknownENIError is an error when caller tries to access an ENI which is unknown to datastore
	UnknownENIError = "datastore: unknown ENI"
)

// ErrUnknownPod is an error when there is no pod in data store matching pod name, namespace, container id
var ErrUnknownPod = errors.New("datastore: unknown pod")

// ErrUnknownPodIP is an error where pod's IP address is not found in data store
var ErrUnknownPodIP = errors.New("datastore: pod using unknown IP address")

var (
	enis = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "eni_allocated",
			Help: "The number of ENIs allocated",
		},
	)
	totalIPs = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "total_ip_addresses",
			Help: "The total number of IP addresses",
		},
	)
	assignedIPs = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "assigned_ip_addresses",
			Help: "The number of IP addresses assigned to pods",
		},
	)
	prometheusRegistered = false
)

// ENIIPPool contains ENI/IP Pool information. Exported fields will be Marshaled for introspection.
type ENIIPPool struct {
	createTime         time.Time
	lastUnAssignedTime time.Time
	// IsPrimary indicates whether ENI is a primary ENI
	IsPrimary bool
	ID        string
	// DeviceNumber is the device number of ENI
	DeviceNumber int
	// AssignedIPv4Addresses is the number of IP addesses already been assigned
	AssignedIPv4Addresses int
	// IPv4Addresses shows whether each address is assigned, the key is IP address, which must
	// be in dot-decimal notation with no leading zeros and no whitespace(eg: "10.1.0.253")
	IPv4Addresses map[string]*AddressInfo
}

// AddressInfo contains inforation about an IP, Exported fields will be Marshaled for introspection.
type AddressInfo struct {
	address        string
	Assigned       bool // true if it is assigned to a pod
	unAssignedTime time.Time
}

// PodKey is used to locate pod IP
type PodKey struct {
	name      string
	namespace string
	container string
}

// PodIPInfo contains pod's IP and the device number of the ENI
type PodIPInfo struct {
	// IP is the IP address of pod
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

// PodInfos contains pods IP information which uses key name_namespace_container
type PodInfos map[string]PodIPInfo

// ENIInfos contains ENI IP information
type ENIInfos struct {
	// TotalIPs is the total number of IP addresses
	TotalIPs int
	// assigned is the number of IP addresses that has been assigned
	AssignedIPs int
	// ENIIPPools contains ENI IP pool information
	ENIIPPools map[string]ENIIPPool
}

func prometheusRegister() {
	if !prometheusRegistered {
		prometheus.MustRegister(enis)
		prometheus.MustRegister(totalIPs)
		prometheus.MustRegister(assignedIPs)
		prometheusRegistered = true
	}
}

// NewDataStore returns DataStore structure
func NewDataStore() *DataStore {
	prometheusRegister()
	return &DataStore{
		eniIPPools: make(map[string]*ENIIPPool),
		podsIP:     make(map[PodKey]PodIPInfo),
	}
}

// AddENI add ENI to data store
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
		IsPrimary:     isPrimary,
		ID:            eniID,
		DeviceNumber:  deviceNumber,
		IPv4Addresses: make(map[string]*AddressInfo)}
	enis.Set(float64(len(ds.eniIPPools)))
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
		return errors.New("add ENI's IP to datastore: unknown ENI")
	}

	_, ok = curENI.IPv4Addresses[ipv4]
	if ok {
		return errors.New(DuplicateIPError)
	}

	ds.total++
	totalIPs.Set(float64(ds.total))

	curENI.IPv4Addresses[ipv4] = &AddressInfo{address: ipv4, Assigned: false}

	log.Infof("Added ENI(%s)'s IP %s to datastore", eniID, ipv4)

	return nil
}

// DelENIIPv4Address delete an IP of ENI from datastore
func (ds *DataStore) DelENIIPv4Address(eniID string, ipv4 string) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	log.Debugf("Deleting ENI(%s)'s IPv4 address %s from datastore", eniID, ipv4)
	log.Debugf("IP Address Pool stats: total: %d, assigned: %d",
		ds.total, ds.assigned)

	curENI, ok := ds.eniIPPools[eniID]
	if !ok {
		return errors.New(UnknownENIError)
	}

	ipAddr, ok := curENI.IPv4Addresses[ipv4]
	if !ok {
		return errors.New(UnknownIPError)
	}

	if ipAddr.Assigned {
		return errors.New(IPInUseError)
	}

	ds.total--
	totalIPs.Set(float64(ds.total))

	delete(curENI.IPv4Addresses, ipv4)

	log.Infof("Deleted ENI(%s)'s IP %s from datastore", eniID, ipv4)

	return nil
}

// AssignPodIPv4Address assigns an IPv4 address to pod
// It returns the assigned IPv4 address, device number, error
func (ds *DataStore) AssignPodIPv4Address(k8sPod *k8sapi.K8SPodInfo) (string, int, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	log.Debugf("AssignIPv4Address: IP address pool stats: total:%d, assigned %d",
		ds.total, ds.assigned)
	podKey := PodKey{
		name:      k8sPod.Name,
		namespace: k8sPod.Namespace,
		container: k8sPod.Container,
	}
	ipAddr, ok := ds.podsIP[podKey]
	if ok {
		if ipAddr.IP == k8sPod.IP && k8sPod.IP != "" {
			// The caller invoke multiple times to assign(PodName/NameSpace --> same IPAddress). It is not a error, but not very efficient.
			log.Infof("AssignPodIPv4Address: duplicate pod assign for IP %s, name %s, namespace %s, container %s",
				k8sPod.IP, k8sPod.Name, k8sPod.Namespace, k8sPod.Container)
			return ipAddr.IP, ipAddr.DeviceNumber, nil
		}
		//TODO handle this bug assert?, may need to add a counter here, if counter is too high, need to mark node as unhealthy...
		// this is a bug that the caller invoke multiple times to assign(PodName/NameSpace -> a different IPaddress).
		log.Errorf("AssignPodIPv4Address:  current IP %s is changed to IP %s for pod(name %s, namespace %s, container %s)",
			ipAddr, k8sPod.IP, k8sPod.Name, k8sPod.Namespace, k8sPod.Container)
		return "", 0, errors.New("datastore; invalid pod with multiple IP addresses")

	}

	return ds.assignPodIPv4AddressUnsafe(k8sPod)
}

// It returns the assigned IPv4 address, device number, error
func (ds *DataStore) assignPodIPv4AddressUnsafe(k8sPod *k8sapi.K8SPodInfo) (string, int, error) {
	podKey := PodKey{
		name:      k8sPod.Name,
		namespace: k8sPod.Namespace,
		container: k8sPod.Container,
	}
	curTime := time.Now()
	for _, eni := range ds.eniIPPools {
		if (k8sPod.IP == "") && (len(eni.IPv4Addresses) == eni.AssignedIPv4Addresses) {
			// skip this ENI, since it has no available IP address
			log.Debugf("AssignPodIPv4Address, skip ENI %s that does not have available addresses", eni.ID)
			continue
		}
		for _, addr := range eni.IPv4Addresses {
			if k8sPod.IP == addr.address {
				// After L-IPAM restart and built IP warm-pool, it needs to take the existing running pod IP out of the pool.
				if !addr.Assigned {
					ds.assigned++
					eni.AssignedIPv4Addresses++
					assignedIPs.Set(float64(ds.assigned))
					addr.Assigned = true
				}
				ds.podsIP[podKey] = PodIPInfo{IP: addr.address, DeviceNumber: eni.DeviceNumber}
				log.Infof("AssignPodIPv4Address Reassign IP %v to pod (name %s, namespace %s)",
					addr.address, k8sPod.Name, k8sPod.Namespace)
				return addr.address, eni.DeviceNumber, nil
			}
			if !addr.Assigned && k8sPod.IP == "" && curTime.Sub(addr.unAssignedTime) > addressCoolingPeriod {
				// This is triggered by a pod's Add Network command from CNI plugin
				ds.assigned++
				assignedIPs.Set(float64(ds.assigned))
				eni.AssignedIPv4Addresses++
				addr.Assigned = true
				ds.podsIP[podKey] = PodIPInfo{IP: addr.address, DeviceNumber: eni.DeviceNumber}
				log.Infof("AssignPodIPv4Address Assign IP %v to pod (name %s, namespace %s container %s)",
					addr.address, k8sPod.Name, k8sPod.Namespace, k8sPod.Container)
				return addr.address, eni.DeviceNumber, nil
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
		if eni.IsPrimary {
			continue
		}

		if time.Now().Sub(eni.createTime) < minLifeTime {
			continue
		}

		if time.Now().Sub(eni.lastUnAssignedTime) < addressENICoolingPeriod {
			continue
		}

		if eni.AssignedIPv4Addresses != 0 {
			continue
		}

		log.Debugf("FreeENI: found a deletable ENI %s", eni.ID)
		return eni
	}
	return nil
}

// GetENINeedsIP finds out the eni in datastore which failed to get secondary IP address
func (ds *DataStore) GetENINeedsIP(maxIPperENI int64, skipPrimary bool) *ENIIPPool {
	for _, eni := range ds.eniIPPools {
		if skipPrimary && eni.IsPrimary {
			log.Debugf("Skip the primary ENI for need IP check")
			continue
		}
		if int64(len(eni.IPv4Addresses)) < maxIPperENI {
			log.Debugf("Found eni %s that have less IP address allocated: cur=%d, max=%d",
				eni.ID, len(eni.IPv4Addresses), maxIPperENI)
			return eni
		}
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
		log.Debugf("No ENI can be deleted at this time")
		return "", errors.New("free ENI: no ENI can be deleted at this time")
	}

	ds.total -= len(ds.eniIPPools[deletableENI.ID].IPv4Addresses)
	ds.assigned -= deletableENI.AssignedIPv4Addresses
	log.Infof("FreeENI %s: IP address pool stats: free %d addresses, total: %d, assigned: %d",
		deletableENI.ID, len(ds.eniIPPools[deletableENI.ID].IPv4Addresses), ds.total, ds.assigned)
	deletedENI := deletableENI.ID
	delete(ds.eniIPPools, deletableENI.ID)

	enis.Set(float64(len(ds.eniIPPools)))
	assignedIPs.Set(float64(ds.assigned))
	totalIPs.Set(float64(ds.total))
	return deletedENI, nil
}

// DeleteENI free a ENI.
func (ds *DataStore) DeleteENI(eni string) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	eniIPPool, ok := ds.eniIPPools[eni]
	if !ok {
		return errors.New(UnknownENIError)
	}

	if eniIPPool.AssignedIPv4Addresses != 0 {
		return errors.New(ENIInUseError)
	}

	ds.total -= len(eniIPPool.IPv4Addresses)
	log.Infof("DeleteENI %s: IP address pool stats: free %d addresses, total: %d, assigned: %d",
		eni, len(ds.eniIPPools[eni].IPv4Addresses), ds.total, ds.assigned)
	delete(ds.eniIPPools, eni)
	enis.Set(float64(len(ds.eniIPPools)))

	return nil
}

// UnAssignPodIPv4Address a) find out the IP address based on PodName and PodNameSpace
// b)  mark IP address as unassigned c) returns IP address, ENI's device number, error
func (ds *DataStore) UnAssignPodIPv4Address(k8sPod *k8sapi.K8SPodInfo) (string, int, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	log.Debugf("UnAssignIPv4Address: IP address pool stats: total:%d, assigned %d, pod(Name: %s, Namespace: %s, Container %s)",
		ds.total, ds.assigned, k8sPod.Name, k8sPod.Namespace, k8sPod.Container)

	podKey := PodKey{
		name:      k8sPod.Name,
		namespace: k8sPod.Namespace,
		container: k8sPod.Container,
	}
	ipAddr, ok := ds.podsIP[podKey]
	if !ok {
		log.Warnf("UnassignIPv4Address: Failed to find pod %s namespace %s Container %s",
			k8sPod.Name, k8sPod.Namespace, k8sPod.Container)
		return "", 0, ErrUnknownPod
	}

	for _, eni := range ds.eniIPPools {
		ip, ok := eni.IPv4Addresses[ipAddr.IP]
		if ok && ip.Assigned {
			ip.Assigned = false
			ds.assigned--
			assignedIPs.Set(float64(ds.assigned))
			eni.AssignedIPv4Addresses--
			curTime := time.Now()
			ip.unAssignedTime = curTime
			eni.lastUnAssignedTime = curTime
			log.Infof("UnAssignIPv4Address: pod (Name: %s, NameSpace %s Container %s)'s ipAddr %s, DeviceNumber%d",
				k8sPod.Name, k8sPod.Namespace, k8sPod.Container, ip.address, eni.DeviceNumber)
			delete(ds.podsIP, podKey)
			return ip.address, eni.DeviceNumber, nil
		}
	}

	log.Warnf("UnassignIPv4Address: Failed to find pod %s namespace %s container %s using IP %s",
		k8sPod.Name, k8sPod.Namespace, k8sPod.Container, ipAddr.IP)
	return "", 0, ErrUnknownPodIP
}

// GetPodInfos provides pod IP information to introspection endpoint
func (ds *DataStore) GetPodInfos() *map[string]PodIPInfo {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	var podInfos = make(map[string]PodIPInfo, len(ds.podsIP))

	for podKey, podInfo := range ds.podsIP {
		key := podKey.name + "_" + podKey.namespace + "_" + podKey.container
		podInfos[key] = podInfo
		log.Debugf("introspect: key %s", key)
	}

	log.Debugf("introspect: len %d", len(ds.podsIP))

	return &podInfos
}

// GetENIInfos provides ENI IP information to introspection endpoint
func (ds *DataStore) GetENIInfos() *ENIInfos {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	var eniInfos = ENIInfos{
		TotalIPs:    ds.total,
		AssignedIPs: ds.assigned,
		ENIIPPools:  make(map[string]ENIIPPool, len(ds.eniIPPools)),
	}

	for eni, eniInfo := range ds.eniIPPools {
		eniInfos.ENIIPPools[eni] = *eniInfo
	}
	return &eniInfos
}

// GetENIs provides the number of ENI in the datastore
func (ds *DataStore) GetENIs() int {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	return len(ds.eniIPPools)
}

// GetENIIPPools returns eni's IP address list
func (ds *DataStore) GetENIIPPools(eni string) (map[string]*AddressInfo, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	eniIPPool, ok := ds.eniIPPools[eni]

	if !ok {
		return nil, errors.New(UnknownENIError)
	}

	var ipPool = make(map[string]*AddressInfo, len(eniIPPool.IPv4Addresses))

	for ip, ipAddr := range eniIPPool.IPv4Addresses {
		ipPool[ip] = ipAddr
	}

	return ipPool, nil
}
