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

package datastore

import (
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/cri"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// minENILifeTime is the shortest time before we consider deleting a newly created ENI
	minENILifeTime = 1 * time.Minute

	// addressCoolingPeriod is used to ensure an IP not get assigned to a Pod if this IP is used by a different Pod
	// in addressCoolingPeriod
	addressCoolingPeriod = 30 * time.Second

	// DuplicatedENIError is an error when caller tries to add an duplicate ENI to data store
	DuplicatedENIError = "data store: duplicate ENI"

	// IPAlreadyInStoreError is an error when caller tries to add an duplicate IP address to data store
	IPAlreadyInStoreError = "datastore: IP already in data store"

	// UnknownIPError is an error when caller tries to delete an IP which is unknown to data store
	UnknownIPError = "datastore: unknown IP"

	// IPInUseError is an error when caller tries to delete an IP where IP is still assigned to a Pod
	IPInUseError = "datastore: IP is used and can not be deleted"

	// ENIInUseError is an error when caller tries to delete an ENI where there are IP still assigned to a pod
	ENIInUseError = "datastore: ENI is used and can not be deleted"

	// UnknownENIError is an error when caller tries to access an ENI which is unknown to datastore
	UnknownENIError = "datastore: unknown ENI"
)

// We need to know which IPs are already allocated across
// ipamd/datastore restarts.  In vpc-cni <=1.6, we "stored" the
// allocated IPs by querying kubelet's CRI.  Since this requires scary
// access to CRI socket, and may race with CRI's internal logic, we
// are transitioning away from from this to storing allocations
// ourself in a file (similar to host-ipam CNI plugin).
//
// Because we don't want to require a node restart during CNI
// upgrade/downgrade, we need an "expand/contract" style upgrade to
// keep the two stores in sync:
//
// Migration phase0 (CNI 1.6): Read/write from CRI only.
// Migration phase1 (CNI 1.7): Read from CRI.  Write to CRI+file.
// Migration phase2 (CNI 1.8?): Read from file. Write to CRI+file.
// Migration phase3 (hypothetical): Read/write from file only.
//
// Note phase3 is not necessary since writes to CRI are implicit.
// At/after phase2, we can remove any code protected by
// checkpointMigrationPhase<2.
const checkpointMigrationPhase = 1

// Placeholders used for unknown values when reading from CRI.
const backfillNetworkName = "_migrated-from-cri"
const backfillNetworkIface = "unknown"

const defaultMaxIPv6addresses = 250

// ErrUnknownPod is an error when there is no pod in data store matching pod name, namespace, sandbox id
var ErrUnknownPod = errors.New("datastore: unknown pod")

var (
	enis = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_eni_allocated",
			Help: "The number of ENIs allocated",
		},
	)
	totalIPs = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_total_ip_addresses",
			Help: "The total number of IP addresses",
		},
	)
	assignedIPs = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_assigned_ip_addresses",
			Help: "The number of IP addresses assigned to pods",
		},
	)
	forceRemovedENIs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "awscni_force_removed_enis",
			Help: "The number of ENIs force removed while they had assigned pods",
		},
	)
	forceRemovedIPs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "awscni_force_removed_ips",
			Help: "The number of IPs force removed while they had assigned pods",
		},
	)
	totalPrefixes = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_total_ipv4_prefixes",
			Help: "The total number of IPv4 prefixes",
		},
	)
	ipsPerCidr = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "awscni_assigned_ip_per_ipv4cidr",
			Help: "The total number of IP addresses assigned per cidr",
		},
		[]string{"cidr"},
	)
	prometheusRegistered = false
)

// IPAMKey is the IPAM primary key.  Quoting CNI spec:
//   Plugins that store state should do so using a primary key of
//   (network name, CNI_CONTAINERID, CNI_IFNAME).
type IPAMKey struct {
	NetworkName string `json:"networkName"`
	ContainerID string `json:"containerID"`
	IfName      string `json:"ifName"`
}

// IsZero returns true iff object is equal to the golang zero/null value.
func (k IPAMKey) IsZero() bool {
	return k == IPAMKey{}
}

// String() implements the fmt.Stringer interface.
func (k IPAMKey) String() string {
	return fmt.Sprintf("%s/%s/%s", k.NetworkName, k.ContainerID, k.IfName)
}

// ENI represents a single ENI. Exported fields will be marshaled for introspection.
type ENI struct {
	// AWS ENI ID
	ID         string
	createTime time.Time
	// IsPrimary indicates whether ENI is a primary ENI
	IsPrimary bool
	// IsTrunk indicates whether this ENI is used to provide pods with dedicated ENIs
	IsTrunk bool
	// IsEFA indicates whether this ENI is tagged as an EFA
	IsEFA bool
	// DeviceNumber is the device number of ENI (0 means the primary ENI)
	DeviceNumber int
	// IPv4Addresses shows whether each address is assigned, the key is IP address, which must
	// be in dot-decimal notation with no leading zeros and no whitespace(eg: "10.1.0.253")
	// Key is the IP address - PD: "IP/28" and SIP: "IP/32"
	AvailableIPv4Cidrs map[string]*CidrInfo
	//IPv6CIDRs contains information tied to IPv6 Prefixes attached to the ENI
	IPv6Cidrs map[string]*CidrInfo
}

// AddressInfo contains information about an IP, Exported fields will be marshaled for introspection.
type AddressInfo struct {
	IPAMKey        IPAMKey
	Address        string
	UnassignedTime time.Time
}

// CidrInfo
type CidrInfo struct {
	//Either v4/v6 Host or LPM Prefix
	Cidr net.IPNet
	//Key is individual IP addresses from the Prefix - /32 (v4) or /128 (v6)
	IPAddresses map[string]*AddressInfo
	//true if Cidr here is an LPM prefix
	IsPrefix bool
	//IP Address Family of the Cidr
	AddressFamily string
}

func (cidr *CidrInfo) Size() int {
	ones, bits := cidr.Cidr.Mask.Size()
	return (1 << (bits - ones))
}

func (e *ENI) findAddressForSandbox(ipamKey IPAMKey) (*CidrInfo, *AddressInfo) {
	//Either v4 or v6 for now.
	//Check in V4 prefixes
	for _, availableCidr := range e.AvailableIPv4Cidrs {
		for _, addr := range availableCidr.IPAddresses {
			if addr.IPAMKey == ipamKey {
				return availableCidr, addr
			}
		}
	}

	//Check in V6 prefixes
	for _, availableCidr := range e.IPv6Cidrs {
		for _, addr := range availableCidr.IPAddresses {
			if addr.IPAMKey == ipamKey {
				return availableCidr, addr
			}
		}
	}
	return nil, nil
}

// AssignedIPv4Addresses is the number of IP addresses already assigned
func (e *ENI) AssignedIPv4Addresses() int {
	count := 0
	for _, availableCidr := range e.AvailableIPv4Cidrs {
		count += availableCidr.AssignedIPAddressesInCidr()
	}
	return count
}

//AssignedIPAddressesInCidr is the number of IP addresses already assigned in the IPv4 CIDR
func (cidr *CidrInfo) AssignedIPAddressesInCidr() int {
	count := 0
	//SIP : This will run just once and count will be 0 if addr is not assigned or addr is not allocated yet(unused IP)
	//PD : This will return count of number /32 assigned in /28 CIDR.
	for _, addr := range cidr.IPAddresses {
		if addr.Assigned() {
			count++
		}
	}
	return count
}

// Assigned returns true iff the address is allocated to a pod/sandbox.
func (addr AddressInfo) Assigned() bool {
	return !addr.IPAMKey.IsZero()
}

// InCoolingPeriod checks whether an addr is in addressCoolingPeriod
func (addr AddressInfo) inCoolingPeriod() bool {
	return time.Since(addr.UnassignedTime) <= addressCoolingPeriod
}

// ENIPool is a collection of ENI, keyed by ENI ID
type ENIPool map[string]*ENI

// AssignedIPv4Addresses is the number of IP addresses already assigned
func (p *ENIPool) AssignedIPv4Addresses() int {
	count := 0
	for _, eni := range *p {
		count += eni.AssignedIPv4Addresses()
	}
	return count
}

// FindAddressForSandbox returns ENI and AddressInfo or (nil, nil) if not found
func (p *ENIPool) FindAddressForSandbox(ipamKey IPAMKey) (*ENI, *CidrInfo, *AddressInfo) {
	for _, eni := range *p {
		if availableCidr, addr := eni.findAddressForSandbox(ipamKey); addr != nil && availableCidr != nil {
			return eni, availableCidr, addr
		}
	}
	return nil, nil, nil
}

// PodIPInfo contains pod's IP and the device number of the ENI
type PodIPInfo struct {
	IPAMKey IPAMKey
	// IP is the IPv4 address of pod
	IP string
	// DeviceNumber is the device number of the ENI
	DeviceNumber int
}

// DataStore contains node level ENI/IP
type DataStore struct {
	total                    int
	assigned                 int
	allocatedPrefix          int
	eniPool                  ENIPool
	lock                     sync.Mutex
	log                      logger.Logger
	CheckpointMigrationPhase int
	backingStore             Checkpointer
	cri                      cri.APIs
	isPDEnabled              bool
}

// ENIInfos contains ENI IP information
type ENIInfos struct {
	// TotalIPs is the total number of IP addresses
	TotalIPs int
	// assigned is the number of IP addresses that has been assigned
	AssignedIPs int
	// ENIs contains ENI IP pool information
	ENIs map[string]ENI
}

func prometheusRegister() {
	if !prometheusRegistered {
		prometheus.MustRegister(enis)
		prometheus.MustRegister(totalIPs)
		prometheus.MustRegister(assignedIPs)
		prometheus.MustRegister(forceRemovedENIs)
		prometheus.MustRegister(forceRemovedIPs)
		prometheus.MustRegister(totalPrefixes)
		prometheus.MustRegister(ipsPerCidr)
		prometheusRegistered = true
	}
}

// NewDataStore returns DataStore structure
func NewDataStore(log logger.Logger, backingStore Checkpointer, isPDEnabled bool) *DataStore {
	prometheusRegister()
	return &DataStore{
		eniPool:                  make(ENIPool),
		log:                      log,
		backingStore:             backingStore,
		cri:                      cri.New(),
		CheckpointMigrationPhase: checkpointMigrationPhase,
		isPDEnabled:              isPDEnabled,
	}
}

// CheckpointFormatVersion is the version stamp used on stored checkpoints.
const CheckpointFormatVersion = "vpc-cni-ipam/1"

// CheckpointData is the format of stored checkpoints. Note this is
// deliberately a "dumb" format since efficiency is less important
// than version stability here.
type CheckpointData struct {
	Version     string            `json:"version"`
	Allocations []CheckpointEntry `json:"allocations"`
}

// CheckpointEntry is a "row" in the conceptual IPAM datastore, as stored
// in checkpoints.
type CheckpointEntry struct {
	IPAMKey
	IP string `json:"ip"`
}

// ReadBackingStore initialises the IP allocation state from the
// configured backing store.  Should be called before using data
// store.
func (ds *DataStore) ReadBackingStore(isv6Enabled bool) error {
	var data CheckpointData

	switch ds.CheckpointMigrationPhase {
	case 1:
		// Phase1: Read from CRI
		ds.log.Infof("Reading ipam state from CRI")

		sandboxes, err := ds.cri.GetRunningPodSandboxes(ds.log)
		if err != nil {
			return err
		}

		entries := make([]CheckpointEntry, 0, len(sandboxes))
		for _, s := range sandboxes {
			ds.log.Debugf("Adding container ID: %v", s.ID)
			entries = append(entries, CheckpointEntry{
				// NB: These Backfill values are also assumed in UnassignPodIPAddress
				IPAMKey: IPAMKey{
					NetworkName: backfillNetworkName,
					ContainerID: s.ID,
					IfName:      backfillNetworkIface,
				},
				IP: s.IP,
			})
		}
		data = CheckpointData{
			Version:     CheckpointFormatVersion,
			Allocations: entries,
		}

	case 2:
		// Phase2: Read from checkpoint file
		ds.log.Infof("Reading ipam state from backing store")

		err := ds.backingStore.Restore(&data)
		ds.log.Debugf("backing store restore returned err %v", err)
		if os.IsNotExist(err) {
			// Assume that no file == no containers are
			// currently in use, eg a fresh reboot just
			// cleared everything out.  This is ok, and a
			// no-op.
			return nil
		} else if err != nil {
			return fmt.Errorf("datastore: error reading backing store: %v", err)
		}

		if data.Version != CheckpointFormatVersion {
			return fmt.Errorf("datastore: unknown backing store format (%s != %s) - wrong CNI/ipamd version? (Rebooting this node will restart local pods and probably help)", data.Version, CheckpointFormatVersion)
		}

	default:
		panic(fmt.Sprintf("Unexpected value of checkpointMigrationPhase: %v", ds.CheckpointMigrationPhase))
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	for _, allocation := range data.Allocations {
		ipAddr := net.ParseIP(allocation.IP)
		found := false
	eniloop:
		for _, eni := range ds.eniPool {
			eniCidrs := eni.AvailableIPv4Cidrs
			if isv6Enabled {
				ds.log.Debugf("v6 is enabled")
				eniCidrs = eni.IPv6Cidrs
			}
			for _, cidr := range eniCidrs {
				ds.log.Debugf("Checking if IP: %v belongs to CIDR: %v", ipAddr, cidr.Cidr)
				if cidr.Cidr.Contains(ipAddr) {
					// Found!
					found = true
					if _, ok := cidr.IPAddresses[allocation.IP]; ok {
						return errors.New(IPAlreadyInStoreError)
					}
					addr := &AddressInfo{Address: ipAddr.String()}
					cidr.IPAddresses[allocation.IP] = addr
					ds.assignPodIPAddressUnsafe(allocation.IPAMKey, eni, addr)
					ds.log.Debugf("Recovered %s => %s/%s", allocation.IPAMKey, eni.ID, addr.Address)
					//Update prometheus for ips per cidr
					//Secondary IP mode will have /32:1 and Prefix mode will have /28:<number of /32s>
					ipsPerCidr.With(prometheus.Labels{"cidr": cidr.Cidr.String()}).Inc()
					break eniloop
				}
			}
		}
		if !found {
			ds.log.Infof("datastore: Sandbox %s uses unknown IP Address %s - presuming stale/dead", allocation.IPAMKey, allocation.IP)
		}
	}

	if ds.CheckpointMigrationPhase == 1 {
		// For phase1: write whatever we just read above from
		// CRI to backingstore immediately - just in case we
		// _never_ see an add/del request before we upgrade to
		// phase2.
		if err := ds.writeBackingStoreUnsafe(); err != nil {
			return err
		}
	}

	ds.log.Debugf("Completed ipam state recovery")
	return nil
}

func (ds *DataStore) writeBackingStoreUnsafe() error {
	allocations := make([]CheckpointEntry, 0, ds.assigned)

	for _, eni := range ds.eniPool {
		//Loop through ENI's v4 prefixes
		for _, assignedAddr := range eni.AvailableIPv4Cidrs {
			for _, addr := range assignedAddr.IPAddresses {
				if addr.Assigned() {
					entry := CheckpointEntry{
						IPAMKey: addr.IPAMKey,
						IP:    addr.Address,
					}
					allocations = append(allocations, entry)
				}
			}
		}
		//Loop through ENI's v6 prefixes
		for _, assignedAddr := range eni.IPv6Cidrs {
			for _, addr := range assignedAddr.IPAddresses {
				if addr.Assigned() {
					entry := CheckpointEntry{
						IPAMKey: addr.IPAMKey,
						IP:    addr.Address,
					}
					allocations = append(allocations, entry)
				}
			}
		}
	}

	data := CheckpointData{
		Version:     CheckpointFormatVersion,
		Allocations: allocations,
	}

	return ds.backingStore.Checkpoint(&data)
}

// AddENI add ENI to data store
func (ds *DataStore) AddENI(eniID string, deviceNumber int, isPrimary, isTrunk, isEFA bool) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	ds.log.Debugf("DataStore Add an ENI %s", eniID)

	_, ok := ds.eniPool[eniID]
	if ok {
		return errors.New(DuplicatedENIError)
	}
	ds.eniPool[eniID] = &ENI{
		createTime:         time.Now(),
		IsPrimary:          isPrimary,
		IsTrunk:            isTrunk,
		IsEFA:              isEFA,
		ID:                 eniID,
		DeviceNumber:       deviceNumber,
		AvailableIPv4Cidrs: make(map[string]*CidrInfo)}

	enis.Set(float64(len(ds.eniPool)))
	return nil
}

// AddIPv4AddressToStore adds IPv4 CIDR of an ENI to data store
func (ds *DataStore) AddIPv4CidrToStore(eniID string, ipv4Cidr net.IPNet, isPrefix bool) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	strIPv4Cidr := ipv4Cidr.String()
	ds.log.Infof("Adding %s to DS for %s", strIPv4Cidr, eniID)
	curENI, ok := ds.eniPool[eniID]
	if !ok {
		ds.log.Infof("unknown ENI")
		return errors.New("add ENI's IP to datastore: unknown ENI")
	}
	// Already there
	_, ok = curENI.AvailableIPv4Cidrs[strIPv4Cidr]
	if ok {
		ds.log.Infof("IP already in DS")
		return errors.New(IPAlreadyInStoreError)
	}

	newCidrInfo := &CidrInfo{
		Cidr:          ipv4Cidr,
		IPAddresses: make(map[string]*AddressInfo),
		IsPrefix:      isPrefix,
		AddressFamily: "4",
	}

	curENI.AvailableIPv4Cidrs[strIPv4Cidr] = newCidrInfo

	ds.total += newCidrInfo.Size()
	if isPrefix {
		ds.allocatedPrefix++
		totalPrefixes.Set(float64(ds.allocatedPrefix))
	}
	totalIPs.Set(float64(ds.total))

	ds.log.Infof("Added ENI(%s)'s IP/Prefix %s to datastore", eniID, strIPv4Cidr)
	return nil
}

func (ds *DataStore) DelIPv4CidrFromStore(eniID string, cidr net.IPNet, force bool) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	curENI, ok := ds.eniPool[eniID]
	if !ok {
		ds.log.Debugf("Unknown ENI %s while deleting the CIDR", eniID)
		return errors.New(UnknownENIError)
	}
	strIPv4Cidr := cidr.String()

	var deletableCidr *CidrInfo
	deletableCidr, ok = curENI.AvailableIPv4Cidrs[strIPv4Cidr]
	if !ok {
		ds.log.Debugf("Unknown %s CIDR", strIPv4Cidr)
		return errors.New(UnknownIPError)
	}

	// SIP case : This runs just once
	// PD case : if (force is false) then if there are any unassigned IPs, those will get freed but the first assigned IP will
	//  break the loop, should be fine since freed IPs will be reused for new pods.
	updateBackingStore := false
	for _, addr := range deletableCidr.IPAddresses {
		if addr.Assigned() {
			if !force {
				return errors.New(IPInUseError)
			}
			forceRemovedIPs.Inc()
			ds.unassignPodIPAddressUnsafe(addr)
			updateBackingStore = true
		}
	}
	if updateBackingStore {
		if err := ds.writeBackingStoreUnsafe(); err != nil {
			ds.log.Warnf("Unable to update backing store: %v", err)
			// Continuing because 'force'
		}
	}
	ds.total -= deletableCidr.Size()
	if deletableCidr.IsPrefix {
		ds.allocatedPrefix--
		totalPrefixes.Set(float64(ds.allocatedPrefix))
	}
	totalIPs.Set(float64(ds.total))
	delete(curENI.AvailableIPv4Cidrs, strIPv4Cidr)
	ds.log.Infof("Deleted ENI(%s)'s IP/Prefix %s from datastore", eniID, strIPv4Cidr)

	return nil
}

// AddIPv6AddressToStore adds IPv6 CIDR of an ENI to data store
func (ds *DataStore) AddIPv6CidrToStore(eniID string, ipv6Cidr net.IPNet, isPrefix bool) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	strIPv6Cidr := ipv6Cidr.String()
	ds.log.Debugf("Adding %s to DS for %s", strIPv6Cidr, eniID)
	curENI, ok := ds.eniPool[eniID]
	ds.log.Debugf("ENI in pool %s", ok)

	if !ok {
		ds.log.Debugf("unkown ENI")
		return errors.New("add ENI's IP to datastore: unknown ENI")
	}
	// Check if already present in datastore.
	_, ok = curENI.IPv6Cidrs[strIPv6Cidr]
	ds.log.Debugf("IP not in  DS")
	if ok {
		ds.log.Debugf("IPv6 prefix %s already in DS", strIPv6Cidr)
		return errors.New(IPAlreadyInStoreError)
	}

	ds.log.Debugf("Assigning IPv6CIDRs")
	if curENI.IPv6Cidrs == nil { curENI.IPv6Cidrs = make(map[string]*CidrInfo) }
	curENI.IPv6Cidrs[strIPv6Cidr] = &CidrInfo{
		Cidr:          ipv6Cidr,
		IPAddresses:   make(map[string]*AddressInfo),
		IsPrefix:      isPrefix,
		AddressFamily: "6",
	}

	//curENI.IPv6Cidrs[strIPv6Cidr].Size() will end up with a huge number. So, instead capping it to Max Pods ceiling on
	//an EKS Node. TODO - Should we increase the default value further as VPC CNI can also be used in a Self Managed K8S
	//cluster with no such upper bound?
	ds.total += defaultMaxIPv6addresses
	if isPrefix {
		ds.allocatedPrefix++
	}
	totalIPs.Set(float64(ds.total))

	ds.log.Debugf("Added ENI(%s)'s IP/Prefix %s to datastore", eniID, strIPv6Cidr)
	return nil
}

func (ds *DataStore) AssignPodIPAddress(ipamKey IPAMKey, isIPv4Enabled bool, isIPv6Enabled bool) (ipv4Address string,
	ipv6Address string, deviceNumber int, err error) {
	//Currently it's either v4 or v6. Dual Stack mode isn't supported.
	if isIPv4Enabled {
		ipv4Address, deviceNumber, err = ds.AssignPodIPv4Address(ipamKey)
	} else if isIPv6Enabled {
		ipv6Address, deviceNumber, err = ds.AssignPodIPv6Address(ipamKey)
	}
	return ipv4Address, ipv6Address, deviceNumber, err
}

// AssignPodIPv6Address assigns an IPv6 address to pod. Returns the assigned IPv6 address along with device number
func (ds *DataStore) AssignPodIPv6Address(ipamKey IPAMKey) (ipv6Address string, deviceNumber int, err error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	if !ds.isPDEnabled {
		return "", -1, fmt.Errorf("PD is not enabled. V6 is only supported in PD mode")
	}
	ds.log.Debugf("AssignIPv6Address: IPv6 address pool stats: assigned %d", ds.assigned)

	if eni, _, addr := ds.eniPool.FindAddressForSandbox(ipamKey); addr != nil {
		ds.log.Infof("AssignPodIPv6Address: duplicate pod assign for sandbox %s", ipamKey)
		return addr.Address, eni.DeviceNumber, nil
	}

	//In IPv6 Prefix Delegation mode, eniPool will only have Primary ENI.
	for _, eni := range ds.eniPool {
		if len(eni.IPv6Cidrs) == 0 {
			continue
		}
		for _, V6Cidr := range eni.IPv6Cidrs {
			if !V6Cidr.IsPrefix {
				continue
			}
			ipv6Address, err = ds.getFreeIPv6AddrFromCidr(V6Cidr)
			if err != nil {
				ds.log.Debugf("Unable to get IP address from prefix: %v", err)
				//In v6 mode, we (should) only have one CIDR/Prefix. So, we can bail out but we will let the loop
				//exit instead.
				continue
			}
			ds.log.Debugf("New v6 IP from PD pool- %s", ipv6Address)
			addr := &AddressInfo{Address: ipv6Address}
			V6Cidr.IPAddresses[ipv6Address] = addr

			ds.assignPodIPAddressUnsafe(ipamKey, eni, addr)
			if err := ds.writeBackingStoreUnsafe(); err != nil {
				ds.log.Warnf("Failed to update backing store: %v", err)
				// Important! Unwind assignment
				ds.unassignPodIPAddressUnsafe(addr)
				//Remove the IP from eni DB
				delete(V6Cidr.IPAddresses, addr.Address)
				return "", -1, err
			}
			return addr.Address, eni.DeviceNumber, nil
		}
	}
	return "", -1, errors.New("assignPodIPv6AddressUnsafe: no available IP addresses")
}

// AssignPodIPv4Address assigns an IPv4 address to pod
// It returns the assigned IPv4 address, device number, error
func (ds *DataStore) AssignPodIPv4Address(ipamKey IPAMKey) (ipv4address string, deviceNumber int, err error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	ds.log.Debugf("AssignIPv4Address: IP address pool stats: total: %d, assigned %d", ds.total, ds.assigned)

	if eni, _, addr := ds.eniPool.FindAddressForSandbox(ipamKey); addr != nil {
		ds.log.Infof("AssignPodIPv4Address: duplicate pod assign for sandbox %s", ipamKey)
		return addr.Address, eni.DeviceNumber, nil
	}

	for _, eni := range ds.eniPool {
		for _, availableCidr := range eni.AvailableIPv4Cidrs {
			var addr *AddressInfo
			var strPrivateIPv4 string
			var err error

			if (ds.isPDEnabled && availableCidr.IsPrefix) || (!ds.isPDEnabled && !availableCidr.IsPrefix) {
				strPrivateIPv4, err = ds.getFreeIPv4AddrfromCidr(availableCidr)
				if err != nil {
					ds.log.Debugf("Unable to get IP address from CIDR: %v", err)
					//Check in next CIDR
					continue
				}
				ds.log.Debugf("New IP from CIDR pool- %s", strPrivateIPv4)
				if availableCidr.IPAddresses == nil {
					availableCidr.IPAddresses = make(map[string]*AddressInfo)
				}
				//Update prometheus for ips per cidr
				//Secondary IP mode will have /32:1 and Prefix mode will have /28:<number of /32s>
				ipsPerCidr.With(prometheus.Labels{"cidr": availableCidr.Cidr.String()}).Inc()
			} else {
				//This can happen during upgrade or PD enable/disable knob toggle
				//ENI can have prefixes attached and no space for SIPs or vice versa
				continue
			}

			addr = availableCidr.IPAddresses[strPrivateIPv4]
			if addr == nil {
				// addr is nil when we are using a new IP from prefix or SIP pool
				// if addr is out of cooldown or not assigned, we can reuse addr
				addr = &AddressInfo{Address: strPrivateIPv4}
			}

			availableCidr.IPAddresses[strPrivateIPv4] = addr
			ds.assignPodIPAddressUnsafe(ipamKey, eni, addr)

			if err := ds.writeBackingStoreUnsafe(); err != nil {
				ds.log.Warnf("Failed to update backing store: %v", err)
				// Important! Unwind assignment
				ds.unassignPodIPAddressUnsafe(addr)
				//Remove the IP from eni DB
				delete(availableCidr.IPAddresses, addr.Address)
				//Update prometheus for ips per cidr
				ipsPerCidr.With(prometheus.Labels{"cidr": availableCidr.Cidr.String()}).Dec()
				return "", -1, err
			}
			return addr.Address, eni.DeviceNumber, nil
		}
		ds.log.Debugf("AssignPodIPv4Address: ENI %s does not have available addresses", eni.ID)
	}

	ds.log.Errorf("DataStore has no available IP/Prefix addresses")
	return "", -1, errors.New("assignPodIPv4AddressUnsafe: no available IP/Prefix addresses")
}

// It returns the assigned IPv4 address, device number
func (ds *DataStore) assignPodIPAddressUnsafe(ipamKey IPAMKey, eni *ENI, addr *AddressInfo) (string, int) {
	ds.log.Infof("AssignPodIPv4Address: Assign IP %v to sandbox %s",
		addr.Address, ipamKey)

	if addr.Assigned() {
		panic("addr already assigned")
	}
	addr.IPAMKey = ipamKey // This marks the addr as assigned

	ds.assigned++
	// Prometheus gauge
	assignedIPs.Set(float64(ds.assigned))

	return addr.Address, eni.DeviceNumber
}

func (ds *DataStore) unassignPodIPAddressUnsafe(addr *AddressInfo) {
	if !addr.Assigned() {
		// Already unassigned
		return
	}
	ds.log.Infof("UnAssignPodIPAddress: Unassign IP %v from sandbox %s",
		addr.Address, addr.IPAMKey)
	addr.IPAMKey = IPAMKey{} // unassign the addr
	ds.assigned--
	// Prometheus gauge
	assignedIPs.Set(float64(ds.assigned))
}

// GetStats returns total number of IP addresses, number of assigned IP addresses and total prefixes
func (ds *DataStore) GetStats(addressFamily string) (int, int, int) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	totalIPs := 0
	assignedIPs := 0
	for _, eni := range ds.eniPool {
		AssignedCIDRs := eni.AvailableIPv4Cidrs
		if addressFamily == "6" {
			AssignedCIDRs = eni.IPv6Cidrs
		}
		for _, cidr := range AssignedCIDRs {
			if addressFamily == "4" && ((ds.isPDEnabled && cidr.IsPrefix) || (!ds.isPDEnabled && !cidr.IsPrefix)) {
				assignedIPs += cidr.AssignedIPAddressesInCidr()
				totalIPs += cidr.Size()
			} else if addressFamily == "6" {
				assignedIPs += cidr.AssignedIPAddressesInCidr()
				//Set to default Max pods we support on an instance, otherwise we will end up displaying
				//a huge number.
				totalIPs += defaultMaxIPv6addresses
			}
		}
	}
	return totalIPs, assignedIPs, ds.allocatedPrefix
}

// GetTrunkENI returns the trunk ENI ID or an empty string
func (ds *DataStore) GetTrunkENI() string {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	for _, eni := range ds.eniPool {
		if eni.IsTrunk {
			return eni.ID
		}
	}
	return ""
}

// GetEFAENIs returns the a map containing all attached EFA ENIs
func (ds *DataStore) GetEFAENIs() map[string]bool {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	ret := make(map[string]bool)
	for _, eni := range ds.eniPool {
		if eni.IsEFA {
			ret[eni.ID] = true
		}
	}
	return ret
}

// IsRequiredForWarmIPTarget determines if this ENI has warm IPs that are required to fulfill whatever WARM_IP_TARGET is
// set to.
func (ds *DataStore) isRequiredForWarmIPTarget(warmIPTarget int, eni *ENI) bool {
	otherWarmIPs := 0
	for _, other := range ds.eniPool {
		if other.ID != eni.ID {
			for _, otherPrefixes := range other.AvailableIPv4Cidrs {
				if (ds.isPDEnabled && otherPrefixes.IsPrefix == true) || (!ds.isPDEnabled && otherPrefixes.IsPrefix == false) {
					otherWarmIPs += otherPrefixes.Size() - otherPrefixes.AssignedIPAddressesInCidr()
				}
			}
		}
	}

	if ds.isPDEnabled {
		_, numIPsPerPrefix, _ := GetPrefixDelegationDefaults()
		numPrefixNeeded := DivCeil(warmIPTarget, numIPsPerPrefix)
		warmIPTarget = numPrefixNeeded * numIPsPerPrefix
	}
	return otherWarmIPs < warmIPTarget
}

// IsRequiredForMinimumIPTarget determines if this ENI is necessary to fulfill whatever MINIMUM_IP_TARGET is
// set to.
func (ds *DataStore) isRequiredForMinimumIPTarget(minimumIPTarget int, eni *ENI) bool {
	otherIPs := 0
	for _, other := range ds.eniPool {
		if other.ID != eni.ID {
			for _, otherPrefixes := range other.AvailableIPv4Cidrs {
				if (ds.isPDEnabled && otherPrefixes.IsPrefix == true) || (!ds.isPDEnabled && otherPrefixes.IsPrefix == false) {
					otherIPs += otherPrefixes.Size()
				}
			}
		}
	}

	if ds.isPDEnabled {
		_, numIPsPerPrefix, _ := GetPrefixDelegationDefaults()
		numPrefixNeeded := DivCeil(minimumIPTarget, numIPsPerPrefix)
		minimumIPTarget = numPrefixNeeded * numIPsPerPrefix
	}
	return otherIPs < minimumIPTarget
}

// IsRequiredForWarmPrefixTarget determines if this ENI is necessary to fulfill whatever WARM_PREFIX_TARGET is
// set to.
func (ds *DataStore) isRequiredForWarmPrefixTarget(warmPrefixTarget int, eni *ENI) bool {
	freePrefixes := 0
	for _, other := range ds.eniPool {
		if other.ID != eni.ID {
			for _, otherPrefixes := range other.AvailableIPv4Cidrs {
				if otherPrefixes.AssignedIPAddressesInCidr() == 0 {
					freePrefixes++
				}
			}
		}
	}
	return freePrefixes < warmPrefixTarget
}

func (ds *DataStore) getDeletableENI(warmIPTarget, minimumIPTarget, warmPrefixTarget int) *ENI {
	for _, eni := range ds.eniPool {
		if eni.IsPrimary {
			ds.log.Debugf("ENI %s cannot be deleted because it is primary", eni.ID)
			continue
		}

		if eni.isTooYoung() {
			ds.log.Debugf("ENI %s cannot be deleted because it is too young", eni.ID)
			continue
		}

		if eni.hasIPInCooling() {
			ds.log.Debugf("ENI %s cannot be deleted because has IPs in cooling", eni.ID)
			continue
		}

		if eni.hasPods() {
			ds.log.Debugf("ENI %s cannot be deleted because it has pods assigned", eni.ID)
			continue
		}

		if warmIPTarget != 0 && ds.isRequiredForWarmIPTarget(warmIPTarget, eni) {
			ds.log.Debugf("ENI %s cannot be deleted because it is required for WARM_IP_TARGET: %d", eni.ID, warmIPTarget)
			continue
		}

		if minimumIPTarget != 0 && ds.isRequiredForMinimumIPTarget(minimumIPTarget, eni) {
			ds.log.Debugf("ENI %s cannot be deleted because it is required for MINIMUM_IP_TARGET: %d", eni.ID, minimumIPTarget)
			continue
		}

		if ds.isPDEnabled && warmPrefixTarget != 0 && ds.isRequiredForWarmPrefixTarget(warmPrefixTarget, eni) {
			ds.log.Debugf("ENI %s cannot be deleted because it is required for WARM_PREFIX_TARGET: %d", eni.ID, warmPrefixTarget)
			continue
		}

		if eni.IsTrunk {
			ds.log.Debugf("ENI %s cannot be deleted because it is a trunk ENI", eni.ID)
			continue
		}

		if eni.IsEFA {
			ds.log.Debugf("ENI %s cannot be deleted because it is an EFA ENI", eni.ID)
			continue
		}

		ds.log.Debugf("getDeletableENI: found a deletable ENI %s", eni.ID)
		return eni
	}
	return nil
}

// IsTooYoung returns true if the ENI hasn't been around long enough to be deleted.
func (e *ENI) isTooYoung() bool {
	return time.Since(e.createTime) < minENILifeTime
}

// HasIPInCooling returns true if an IP address was unassigned recently.
func (e *ENI) hasIPInCooling() bool {
	for _, assignedaddr := range e.AvailableIPv4Cidrs {
		for _, addr := range assignedaddr.IPAddresses {
			if addr.inCoolingPeriod() {
				return true
			}
		}
	}
	return false
}

// HasPods returns true if the ENI has pods assigned to it.
func (e *ENI) hasPods() bool {
	return e.AssignedIPv4Addresses() != 0
}

// GetENINeedsIP finds an ENI in the datastore that needs more IP addresses allocated
func (ds *DataStore) GetENINeedsIP(maxIPperENI int, skipPrimary bool) *ENI {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	for _, eni := range ds.eniPool {
		if skipPrimary && eni.IsPrimary {
			ds.log.Debugf("Skip the primary ENI for need IP check")
			continue
		}
		if len(eni.AvailableIPv4Cidrs) < maxIPperENI {
			ds.log.Debugf("Found ENI %s that has less than the maximum number of IP/Prefixes addresses allocated: cur=%d, max=%d",
				eni.ID, len(eni.AvailableIPv4Cidrs), maxIPperENI)
			return eni
		}
	}
	return nil
}

// RemoveUnusedENIFromStore removes a deletable ENI from the data store.
// It returns the name of the ENI which has been removed from the data store and needs to be deleted,
// or empty string if no ENI could be removed.
func (ds *DataStore) RemoveUnusedENIFromStore(warmIPTarget, minimumIPTarget, warmPrefixTarget int) string {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	deletableENI := ds.getDeletableENI(warmIPTarget, minimumIPTarget, warmPrefixTarget)
	if deletableENI == nil {
		return ""
	}

	removableENI := deletableENI.ID

	for _, availableCidr := range ds.eniPool[removableENI].AvailableIPv4Cidrs {
		ds.total -= availableCidr.Size()
		if availableCidr.IsPrefix {
			ds.allocatedPrefix--
			totalPrefixes.Set(float64(ds.allocatedPrefix))
		}
	}
	ds.log.Infof("RemoveUnusedENIFromStore %s: IP/Prefix address pool stats: free %d addresses, total: %d, assigned: %d, total prefixes: %d",
		removableENI, len(ds.eniPool[removableENI].AvailableIPv4Cidrs), ds.total, ds.assigned, ds.allocatedPrefix)

	delete(ds.eniPool, removableENI)

	// Prometheus update
	enis.Set(float64(len(ds.eniPool)))
	totalIPs.Set(float64(ds.total))
	return removableENI
}

// RemoveENIFromDataStore removes an ENI from the datastore. It returns nil on success, or an error.
func (ds *DataStore) RemoveENIFromDataStore(eniID string, force bool) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	eni, ok := ds.eniPool[eniID]
	if !ok {
		return errors.New(UnknownENIError)
	}

	if eni.hasPods() {
		if !force {
			return errors.New(ENIInUseError)
		}
		// This scenario can occur if the reconciliation process discovered this ENI was detached
		// from the EC2 instance outside of the control of ipamd. If this happens, there's nothing
		// we can do other than force all pods to be unassigned from the IPs on this ENI.
		ds.log.Warnf("Force removing eni %s with %d assigned pods", eniID, eni.AssignedIPv4Addresses())
		forceRemovedENIs.Inc()
		forceRemovedIPs.Add(float64(eni.AssignedIPv4Addresses()))
		for _, assignedaddr := range eni.AvailableIPv4Cidrs {
			for _, addr := range assignedaddr.IPAddresses {
				if addr.Assigned() {
					ds.unassignPodIPAddressUnsafe(addr)
				}
			}
			ds.total -= assignedaddr.Size()
			if assignedaddr.IsPrefix {
				ds.allocatedPrefix--
			}
		}
		if err := ds.writeBackingStoreUnsafe(); err != nil {
			ds.log.Warnf("Unable to update backing store: %v", err)
			// Continuing, because 'force'
		}
	}

	for _, assignedaddr := range eni.AvailableIPv4Cidrs {
		ds.total -= assignedaddr.Size()
		if assignedaddr.IsPrefix {
			ds.allocatedPrefix--
		}
	}

	ds.log.Infof("RemoveENIFromDataStore %s: IP/Prefix address pool stats: free %d addresses, total: %d, assigned: %d, total prefixes: %d",
		eniID, len(eni.AvailableIPv4Cidrs), ds.total, ds.assigned, ds.allocatedPrefix)
	delete(ds.eniPool, eniID)

	// Prometheus gauge
	enis.Set(float64(len(ds.eniPool)))
	return nil
}

// UnassignPodIPAddress a) find out the IP address based on PodName and PodNameSpace
// b)  mark IP address as unassigned c) returns IP address, ENI's device number, error
func (ds *DataStore) UnassignPodIPAddress(ipamKey IPAMKey) (e *ENI, ip string, deviceNumber int, err error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	ds.log.Debugf("UnassignPodIPAddress: IP address pool stats: total:%d, assigned %d, sandbox %s",
		ds.total, ds.assigned, ipamKey)

	eni, availableCidr, addr := ds.eniPool.FindAddressForSandbox(ipamKey)
	if addr == nil {
		// This `if` block should be removed when the CRI
		// migration code is finally removed.  Leaving a
		// compile dependency here to make that obvious :P
		var _ = checkpointMigrationPhase

		// If the entry was discovered by querying CRI at
		// restart rather than by observing an ADD operation
		// directly, then we won't have captured the true
		// networkname/ifname.
		ds.log.Debugf("UnassignPodIPAddress: Failed to find IPAM entry under full key, trying CRI-migrated version")
		ipamKey.NetworkName = backfillNetworkName
		ipamKey.IfName = backfillNetworkIface
		eni, availableCidr, addr = ds.eniPool.FindAddressForSandbox(ipamKey)
	}
	if addr == nil {
		ds.log.Warnf("UnassignPodIPAddress: Failed to find sandbox %s",
			ipamKey)
		//Pod Not found. Nothing to do from IPAMD perspective.
		return nil, "", 0, ErrUnknownPod
	}

	ds.unassignPodIPAddressUnsafe(addr)
	if err := ds.writeBackingStoreUnsafe(); err != nil {
		// Unwind un-assignment
		ds.assignPodIPAddressUnsafe(ipamKey, eni, addr)
		return nil, "", 0, err
	}
	addr.UnassignedTime = time.Now()
	if ds.isPDEnabled && !availableCidr.IsPrefix {
		ds.log.Infof("Prefix delegation is enabled and the IP is from secondary pool hence no need to update prefix pool")
		ds.total--
	}
	//Update prometheus for ips per cidr
	ipsPerCidr.With(prometheus.Labels{"cidr": availableCidr.Cidr.String()}).Dec()
	ds.log.Infof("UnassignPodIPAddress: sandbox %s's ipAddr %s, DeviceNumber %d",
		ipamKey, addr.Address, eni.DeviceNumber)
	return eni, addr.Address, eni.DeviceNumber, nil
}

// AllocatedIPs returns a recent snapshot of allocated sandbox<->IPs.
// Note result may already be stale by the time you look at it.
func (ds *DataStore) AllocatedIPs() []PodIPInfo {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	ret := make([]PodIPInfo, 0, ds.eniPool.AssignedIPv4Addresses())
	for _, eni := range ds.eniPool {
		for _, assignedaddr := range eni.AvailableIPv4Cidrs {
			for _, addr := range assignedaddr.IPAddresses {
				if addr.Assigned() {
					info := PodIPInfo{
						IPAMKey:      addr.IPAMKey,
						IP:           addr.Address,
						DeviceNumber: eni.DeviceNumber,
					}
					ret = append(ret, info)
				}
			}
		}
	}
	return ret
}

// FreeableIPs returns a list of unused and potentially freeable IPs.
// Note result may already be stale by the time you look at it.
func (ds *DataStore) FreeableIPs(eniID string) []net.IPNet {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	eni := ds.eniPool[eniID]
	if eni == nil {
		// Can't free any IPs from an ENI we don't know about...
		return nil
	}

	freeable := make([]net.IPNet, 0, len(eni.AvailableIPv4Cidrs))
	for _, assignedaddr := range eni.AvailableIPv4Cidrs {
		if !assignedaddr.IsPrefix && assignedaddr.AssignedIPAddressesInCidr() == 0 {
			freeable = append(freeable, assignedaddr.Cidr)
		}
	}

	return freeable
}

// FreeablePrefixes returns a list of unused and potentially freeable IPs.
// Note result may already be stale by the time you look at it.
func (ds *DataStore) FreeablePrefixes(eniID string) []net.IPNet {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	eni := ds.eniPool[eniID]
	if eni == nil {
		// Can't free any Prefixes from an ENI we don't know about...
		return nil
	}

	freeable := make([]net.IPNet, 0, len(eni.AvailableIPv4Cidrs))
	for _, assignedaddr := range eni.AvailableIPv4Cidrs {
		if assignedaddr.IsPrefix && assignedaddr.AssignedIPAddressesInCidr() == 0 {
			freeable = append(freeable, assignedaddr.Cidr)
		}
	}
	return freeable
}

// GetENIInfos provides ENI and IP information about the datastore
func (ds *DataStore) GetENIInfos() *ENIInfos {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	var eniInfos = ENIInfos{
		TotalIPs:    ds.total,
		AssignedIPs: ds.assigned,
		ENIs:        make(map[string]ENI, len(ds.eniPool)),
	}

	for eni, eniInfo := range ds.eniPool {
		tmpENIInfo := *eniInfo
		tmpENIInfo.AvailableIPv4Cidrs = make(map[string]*CidrInfo, len(eniInfo.AvailableIPv4Cidrs))
		tmpENIInfo.IPv6Cidrs = make(map[string]*CidrInfo, len(eniInfo.IPv6Cidrs))
		for cidr, _ := range eniInfo.AvailableIPv4Cidrs {
			tmpENIInfo.AvailableIPv4Cidrs[cidr] = &CidrInfo{
				Cidr:          eniInfo.AvailableIPv4Cidrs[cidr].Cidr,
				IPAddresses: make(map[string]*AddressInfo, len(eniInfo.AvailableIPv4Cidrs[cidr].IPAddresses)),
				IsPrefix:      eniInfo.AvailableIPv4Cidrs[cidr].IsPrefix,
			}
			// Since IP Addresses might get removed, we need to make a deep copy here.
			for ip, ipAddrInfoRef := range eniInfo.AvailableIPv4Cidrs[cidr].IPAddresses {
				ipAddrInfo := *ipAddrInfoRef
				tmpENIInfo.AvailableIPv4Cidrs[cidr].IPAddresses[ip] = &ipAddrInfo
			}
		}
		for cidr, _ := range eniInfo.IPv6Cidrs {
			tmpENIInfo.IPv6Cidrs[cidr] = &CidrInfo{
				Cidr:          eniInfo.IPv6Cidrs[cidr].Cidr,
				IPAddresses: make(map[string]*AddressInfo, len(eniInfo.IPv6Cidrs[cidr].IPAddresses)),
				IsPrefix:      eniInfo.IPv6Cidrs[cidr].IsPrefix,
			}
			// Since IP Addresses might get removed, we need to make a deep copy here.
			for ip, ipAddrInfoRef := range eniInfo.IPv6Cidrs[cidr].IPAddresses {
				ipAddrInfo := *ipAddrInfoRef
				tmpENIInfo.IPv6Cidrs[cidr].IPAddresses[ip] = &ipAddrInfo
			}
		}
		eniInfos.ENIs[eni] = tmpENIInfo
	}
	return &eniInfos
}

// GetENIs provides the number of ENI in the datastore
func (ds *DataStore) GetENIs() int {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	return len(ds.eniPool)
}

// GetENICIDRs returns the known (allocated & unallocated) ENI secondary IPs and Prefixes
func (ds *DataStore) GetENICIDRs(eniID string) ([]string, []string, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	eni, ok := ds.eniPool[eniID]
	if !ok {
		return nil, nil, errors.New(UnknownENIError)
	}

	var ipPool []string
	var prefixPool []string
	for _, assignedAddr := range eni.AvailableIPv4Cidrs {
		if !assignedAddr.IsPrefix {
			ipPool = append(ipPool, assignedAddr.Cidr.IP.String())
		} else {
			prefixPool = append(prefixPool, assignedAddr.Cidr.String())
		}
	}
	return ipPool, prefixPool, nil
}

// GetFreePrefixes return free prefixes
func (ds *DataStore) GetFreePrefixes() int {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	freePrefixes := 0
	for _, other := range ds.eniPool {
		for _, otherPrefixes := range other.AvailableIPv4Cidrs {
			if otherPrefixes.IsPrefix && otherPrefixes.AssignedIPAddressesInCidr() == 0 {
				freePrefixes++
			}
		}

	}
	return freePrefixes
}

// getFreeIPv4AddrfromCidr returs a free IP/32 address from CIDR
func (ds *DataStore) getFreeIPv4AddrfromCidr(availableCidr *CidrInfo) (string, error) {
	if availableCidr == nil {
		ds.log.Errorf("Prefix datastore not initialized")
		return "", errors.New("Prefix datastore not initialized")
	}
	strPrivateIPv4, err := ds.getUnusedIP(availableCidr)
	if err != nil {
		ds.log.Debugf("Get free IP from prefix failed %v", err)
		return "", err
	}
	ds.log.Debugf("Returning Free IP %s", strPrivateIPv4)
	return strPrivateIPv4, nil
}

func (ds *DataStore) getFreeIPv6AddrFromCidr(IPv6Cidr *CidrInfo) (string, error) {
	if IPv6Cidr == nil {
		ds.log.Errorf("Prefix datastore not initialized")
		return "", errors.New("Prefix datastore not initialized")
	}
	ipv6Address, err := ds.getUnusedIP(IPv6Cidr)
	if err != nil {
		ds.log.Debugf("Get free IP from prefix failed %v", err)
		return "", err
	}
	ds.log.Debugf("Returning Free IP %s", ipv6Address)
	return ipv6Address, nil
}

/*
  /28 -> 10.1.1.1/32[out of cooldown], 10.1.1.2/32[out of cooldown], 10.1.1.3/32[assigned]
  cached ip = 10.1.1.1/32
  /28 ->  10.1.1.3/32[assigned]
  return 10.1.1.1/32
*/

func (ds *DataStore) getUnusedIP(availableCidr *CidrInfo) (string, error) {
	//Check if there is any IP out of cooldown
	var cachedIP string
	for _, addr := range availableCidr.IPAddresses {
		if !addr.Assigned() && !addr.inCoolingPeriod() {
			//if the IP is out of cooldown and not assigned then cache the first available IP
			//continue cleaning up the DB, this is to avoid stale entries and a new thread :)
			if cachedIP == "" {
				cachedIP = addr.Address
			}
			//availableCidr.IPAddresses[addr.Address] = nil //Avoid mem leak - TODO
			delete(availableCidr.IPAddresses, addr.Address)
		}
	}

	if cachedIP != "" {
		return cachedIP, nil
	}

	//If not in cooldown then generate next IP
	ipnet := availableCidr.Cidr
	ip := availableCidr.Cidr.IP

	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); getNextIPAddr(ip) {
		strPrivateIPv4 := ip.String()
		if _, ok := availableCidr.IPAddresses[strPrivateIPv4]; ok {
			continue
		}
		ds.log.Debugf("Found a free IP not in DB - %s", strPrivateIPv4)
		return strPrivateIPv4, nil
	}

	return "", fmt.Errorf("no free IP available in the prefix - %s/%s", availableCidr.Cidr.IP, availableCidr.Cidr.Mask)
}

func getNextIPAddr(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

//Function to return PD defaults supported by VPC
func GetPrefixDelegationDefaults() (int, int, int) {
	numPrefixesPerENI := 1
	numIPsPerPrefix := 16
	supportedPrefixLen := 28

	return numPrefixesPerENI, numIPsPerPrefix, supportedPrefixLen
}

// FindFreeableCidrs finds and returns Cidrs that are not assigned to Pods but are attached
// to ENIs on the node.
func (ds *DataStore) FindFreeableCidrs(eniID string) []CidrInfo {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	eni := ds.eniPool[eniID]
	if eni == nil {
		// Can't free any Cidrs from an ENI we don't know about...
		return nil
	}

	var freeable []CidrInfo
	for _, assignedaddr := range eni.AvailableIPv4Cidrs {
		if assignedaddr.AssignedIPAddressesInCidr() == 0 {
			tempFreeable := CidrInfo{
				Cidr:          assignedaddr.Cidr,
				IPAddresses: nil,
				IsPrefix:      assignedaddr.IsPrefix,
				AddressFamily: assignedaddr.AddressFamily,
			}
			freeable = append(freeable, tempFreeable)
		}
	}
	return freeable

}

func DivCeil(x, y int) int {
	return (x + y - 1) / y
}

// CheckFreeableENIexists will return true if there is an ENI which is unused.
// Could have just called getDeletaleENI, this is just to optimize a bit.
func (ds *DataStore) CheckFreeableENIexists() bool {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	for _, eni := range ds.eniPool {
		if eni.IsPrimary {
			ds.log.Debugf("ENI %s cannot be deleted because it is primary", eni.ID)
			continue
		}

		if eni.hasPods() {
			ds.log.Debugf("ENI %s cannot be deleted because it has pods assigned", eni.ID)
			continue
		}

		if eni.IsTrunk {
			ds.log.Debugf("ENI %s cannot be deleted because it is a trunk ENI", eni.ID)
			continue
		}

		if eni.IsEFA {
			ds.log.Debugf("ENI %s cannot be deleted because it is an EFA ENI", eni.ID)
			continue
		}

		ds.log.Debugf("Found a deletable ENI %s and we might be able to free", eni.ID)
		return true
	}
	return false
}
