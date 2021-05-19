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

	// IPv4 /32 prefix for host routes
	ipv4DefaultPrefixSize = 32
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
	// IsPDEnabled indicates whether prefix delegation is enabled
	IsPDEnabled bool
	// DeviceNumber is the device number of ENI (0 means the primary ENI)
	DeviceNumber int
	// IPv4Addresses shows whether each address is assigned, the key is IP address, which must
	// be in dot-decimal notation with no leading zeros and no whitespace(eg: "10.1.0.253")
	//Key will be a IP/32 secondary cidr or IP/28 prefix
	AvailableIPv4Cidrs map[string]*AssignedIPv4Addresses
}

// AddressInfo contains information about an IP, Exported fields will be marshaled for introspection.
type AddressInfo struct {
	IPAMKey        IPAMKey
	Address        string
	UnassignedTime time.Time
	Prefix         net.IPNet
}

type AssignedIPv4Addresses struct {
	//Cidr info /32 or /28 prefix
	Cidr net.IPNet
	//Key is the /32 IP either secondary IP or free /32 IP allocated from /28 prefix
	IPv4Addresses map[string]*AddressInfo
	//isPrefix
	IsPrefix bool
	//Cnt of number of UsedIPs
	UsedIPs int
}

func (e *ENI) findAddressForSandbox(ipamKey IPAMKey) *AddressInfo {
	for _, availableCidr := range e.AvailableIPv4Cidrs {
		for _, addr := range availableCidr.IPv4Addresses {
			if addr.IPAMKey == ipamKey {
				return addr
			}
		}
	}
	return nil
}

// AssignedIPv4Addresses is the number of IP addresses already assigned
func (e *ENI) AssignedIPv4Addresses() int {
	count := 0
	for _, availableCidr := range e.AvailableIPv4Cidrs {
		for _, addr := range availableCidr.IPv4Addresses {
			if addr.Assigned() {
				count++
			}
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
func (p *ENIPool) FindAddressForSandbox(ipamKey IPAMKey) (*ENI, *AddressInfo) {
	for _, eni := range *p {
		if addr := eni.findAddressForSandbox(ipamKey); addr != nil {
			return eni, addr
		}
	}
	return nil, nil
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
		prometheusRegistered = true
	}
}

// NewDataStore returns DataStore structure
func NewDataStore(log logger.Logger, backingStore Checkpointer) *DataStore {
	prometheusRegister()
	return &DataStore{
		eniPool:                  make(ENIPool),
		log:                      log,
		backingStore:             backingStore,
		cri:                      cri.New(),
		CheckpointMigrationPhase: checkpointMigrationPhase,
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
	IPv4 string `json:"ipv4"`
}

// ReadBackingStore initialises the IP allocation state from the
// configured backing store.  Should be called before using data
// store.
func (ds *DataStore) ReadBackingStore() error {
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
			entries = append(entries, CheckpointEntry{
				// NB: These Backfill values are also assumed in UnassignPodIPv4Address
				IPAMKey: IPAMKey{
					NetworkName: backfillNetworkName,
					ContainerID: s.ID,
					IfName:      backfillNetworkIface,
				},
				IPv4: s.IP,
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

	eniCidrs := make(ENIPool)
	for _, eni := range ds.eniPool {
		//This will have /32 or /28 cidr to ENI mapping
		for cidr, _ := range eni.AvailableIPv4Cidrs {
			eniCidrs[cidr] = eni
		}
	}

	for _, allocation := range data.Allocations {
		ipv4Addr := net.IPNet{IP: net.ParseIP(allocation.IPv4), Mask: net.IPv4Mask(255, 255, 255, 255)}
		eni := eniCidrs[ipv4Addr.String()]
		if eni == nil {
			//Check if this IP belongs to prefix, this can happen when knob is toggled from enable to disable with pods assigned
			ds.log.Infof("Got IP to recover %s\n", allocation.IPv4)
			ipv4Prefix := getPrefixFromIPv4Addr(allocation.IPv4)
			strIpv4Prefix := ipv4Prefix.String()
			ds.log.Infof("Retrieved prefix %s", strIpv4Prefix)

			eni := eniCidrs[strIpv4Prefix]
			if eni == nil {
				ds.log.Infof("datastore: Sandbox %s uses unknown IPv4 %s - presuming stale/dead", allocation.IPAMKey, allocation.IPv4)
				continue
			}
			err := ds.AddPrefixIPv4AddressToStore(eni.ID, allocation.IPv4, ipv4Prefix)
			if err != nil {
				ds.log.Errorf("Failed to ADD PD IP %s on ENI %s", allocation.IPv4, eni.ID)
				return err
			}

			addr := eni.AvailableIPv4Cidrs[strIpv4Prefix].IPv4Addresses[allocation.IPv4]
			eni.AvailableIPv4Cidrs[strIpv4Prefix].UsedIPs++
			ds.assignPodIPv4AddressUnsafe(allocation.IPAMKey, eni, addr)

			addr.Prefix = ipv4Prefix

			ds.log.Debugf("Recovered PD prefix %s ", strIpv4Prefix)
			ds.log.Debugf("Recovered %s => %s/%s", allocation.IPAMKey, eni.ID, addr.Address)

		} else {
			//This belong to secondary IP
			addr := eni.AvailableIPv4Cidrs[ipv4Addr.String()].IPv4Addresses[allocation.IPv4]
			ds.assignPodIPv4AddressUnsafe(allocation.IPAMKey, eni, addr)
			eni.AvailableIPv4Cidrs[ipv4Addr.String()].UsedIPs++
			ds.log.Debugf("Recovered %s => %s/%s", allocation.IPAMKey, eni.ID, addr.Address)
			ds.log.Debugf("DUMP %s -> %v ", ipv4Addr.String(), eni.AvailableIPv4Cidrs[ipv4Addr.String()])
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
		for _, assignedAddr := range eni.AvailableIPv4Cidrs {
			for _, addr := range assignedAddr.IPv4Addresses {
				if addr.Assigned() {
					entry := CheckpointEntry{
						IPAMKey: addr.IPAMKey,
						IPv4:    addr.Address,
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
func (ds *DataStore) AddENI(eniID string, deviceNumber int, isPrimary, isTrunk, isEFA, isPDEnabled bool) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	ds.log.Debugf("DataStore Add an ENI %s and is PD enabled %d", eniID, isPDEnabled)

	_, ok := ds.eniPool[eniID]
	if ok {
		return errors.New(DuplicatedENIError)
	}
	ds.eniPool[eniID] = &ENI{
		createTime:         time.Now(),
		IsPrimary:          isPrimary,
		IsTrunk:            isTrunk,
		IsEFA:              isEFA,
		IsPDEnabled:        isPDEnabled,
		ID:                 eniID,
		DeviceNumber:       deviceNumber,
		AvailableIPv4Cidrs: make(map[string]*AssignedIPv4Addresses)}

	enis.Set(float64(len(ds.eniPool)))
	return nil
}

// AddIPv4AddressToStore add CIDR of an ENI to data store
func (ds *DataStore) AddIPv4CidrToStore(eniID string, ipv4Cidr net.IPNet, isPrefix bool) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	strIPv4Cidr := ipv4Cidr.String()
	ds.log.Infof("Adding %s to DS for %s", strIPv4Cidr, eniID)
	curENI, ok := ds.eniPool[eniID]
	if !ok {
		ds.log.Infof("unkown ENI")
		return errors.New("add ENI's IP to datastore: unknown ENI")
	}
	// Already there
	_, ok = curENI.AvailableIPv4Cidrs[strIPv4Cidr]
	if ok {
		ds.log.Infof("IP already in DS")
		return errors.New(IPAlreadyInStoreError)
	}

	if !isPrefix {
		ds.total++
		// Prometheus gauge
		ipv4 := ipv4Cidr.IP.String()
		totalIPs.Set(float64(ds.total))
		curENI.AvailableIPv4Cidrs[strIPv4Cidr] = &AssignedIPv4Addresses{Cidr: ipv4Cidr, IPv4Addresses: make(map[string]*AddressInfo), IsPrefix: isPrefix, UsedIPs: 0}
		curENI.AvailableIPv4Cidrs[strIPv4Cidr].IPv4Addresses[ipv4] = &AddressInfo{Address: ipv4}
		ds.log.Infof("Added ENI(%s)'s IP %s to datastore", eniID, ipv4)
	} else {
		_, numIPsPerPrefix, _ := GetPrefixDelegationDefaults()
		ds.total = ds.total + numIPsPerPrefix
		ds.allocatedPrefix++
		// Prometheus gauge
		totalIPs.Set(float64(ds.total))
		curENI.AvailableIPv4Cidrs[strIPv4Cidr] = &AssignedIPv4Addresses{Cidr: ipv4Cidr, IPv4Addresses: make(map[string]*AddressInfo), IsPrefix: isPrefix, UsedIPs: 0}
		ds.log.Infof("Added ENI(%s)'s IPv4 Prefix %s to datastore", eniID, strIPv4Cidr)
	}
	return nil
}

// AddPrefixIPv4AddressToStore add an IP of an ENI to data store
func (ds *DataStore) AddPrefixIPv4AddressToStore(eniID string, ipv4 string, cidr net.IPNet) error {

	ds.log.Infof("Adding %s to DS for %s", ipv4, eniID)
	curENI, ok := ds.eniPool[eniID]
	if !ok {
		ds.log.Infof("unkown ENI")
		return errors.New("add ENI's IP to datastore: unknown ENI")
	}
	// Already there
	_, ok = curENI.AvailableIPv4Cidrs[cidr.String()]
	if !ok {
		ds.log.Infof("CIDR %s not in DS", cidr.String())
		return errors.New(IPAlreadyInStoreError)
	}

	// Prometheus gauge
	totalIPs.Set(float64(ds.total))

	if curENI.AvailableIPv4Cidrs[cidr.String()].IPv4Addresses == nil {
		curENI.AvailableIPv4Cidrs[cidr.String()].IPv4Addresses = make(map[string]*AddressInfo)
	}
	curENI.AvailableIPv4Cidrs[cidr.String()].IPv4Addresses[ipv4] = &AddressInfo{Address: ipv4}
	ds.log.Infof("Added ENI(%s)'s IP %s to datastore", eniID, ipv4)
	return nil
}

func (ds *DataStore) DelIPv4CidrFromStore(eniID string, cidr net.IPNet, force bool) error {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	curENI, ok := ds.eniPool[eniID]
	if !ok {
		ds.log.Debugf("Unkown %s while deleting the CIDR", eniID)
		return errors.New(UnknownENIError)
	}
	strIPv4Cidr := cidr.String()
	ipv4 := cidr.IP.String()

	_, ok = curENI.AvailableIPv4Cidrs[strIPv4Cidr]
	if !ok {
		ds.log.Debugf("Unkown %s CIDR", strIPv4Cidr)
		return errors.New(UnknownIPError)
	}

	deletableCidr := curENI.AvailableIPv4Cidrs[strIPv4Cidr]
	if !deletableCidr.IsPrefix {

		ipAddr := deletableCidr.IPv4Addresses[ipv4]
		if ipAddr.Assigned() {
			if !force {
				return errors.New(IPInUseError)
			}
			ds.log.Warnf("Force deleting assigned ip %s on eni %s", ipv4, eniID)
			forceRemovedIPs.Inc()
			ds.unassignPodIPv4AddressUnsafe(ipAddr)
			if err := ds.writeBackingStoreUnsafe(); err != nil {
				ds.log.Warnf("Unable to update backing store: %v", err)
				// Continuing because 'force'
			}
		}
		ds.total--
		// Prometheus gauge
		totalIPs.Set(float64(ds.total))
		delete(deletableCidr.IPv4Addresses, ipv4)
		delete(curENI.AvailableIPv4Cidrs, strIPv4Cidr)
		ds.log.Infof("Deleted ENI(%s)'s IP %s from datastore", eniID, strIPv4Cidr)

	} else {
		if deletableCidr.UsedIPs != 0 {
			if !force {
				return errors.New(IPInUseError)
			}
			ds.log.Warnf("Force deleting assigned Prefix %s on eni %s", strIPv4Cidr, eniID)

			//Cleanup datastore /32 ips
			for _, addr := range deletableCidr.IPv4Addresses {
				forceRemovedIPs.Inc()
				ds.unassignPodIPv4AddressUnsafe(addr)
			}
			if err := ds.writeBackingStoreUnsafe(); err != nil {
				ds.log.Warnf("Unable to update backing store: %v", err)
				// Continuing because 'force'
			}
		}
		_, numIPsPerPrefix, _ := GetPrefixDelegationDefaults()
		ds.total = ds.total - numIPsPerPrefix
		ds.allocatedPrefix--
		// Prometheus gauge
		totalIPs.Set(float64(ds.total))
		delete(deletableCidr.IPv4Addresses, ipv4)
		delete(curENI.AvailableIPv4Cidrs, strIPv4Cidr)
		ds.log.Infof("Deleted ENI(%s)'s Prefix %s from datastore", eniID, strIPv4Cidr)

	}
	return nil
}

// AssignPodIPv4Address assigns an IPv4 address to pod
// It returns the assigned IPv4 address, device number, error
func (ds *DataStore) AssignPodIPv4Address(ipamKey IPAMKey) (ipv4address string, deviceNumber int, err error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	ds.log.Debugf("AssignIPv4Address: IP address pool stats: total: %d, assigned %d", ds.total, ds.assigned)

	if eni, addr := ds.eniPool.FindAddressForSandbox(ipamKey); addr != nil {
		ds.log.Infof("AssignPodIPv4Address: duplicate pod assign for sandbox %s", ipamKey)
		return addr.Address, eni.DeviceNumber, nil
	}

	for _, eni := range ds.eniPool {
		ds.log.Debugf("Checking for eni %v", eni)
		if !eni.IsPDEnabled {
			for _, assignedaddr := range eni.AvailableIPv4Cidrs {
				ds.log.Debugf("DUMP Assigned addr %v", assignedaddr)
				if !assignedaddr.IsPrefix {
					for _, addr := range assignedaddr.IPv4Addresses {
						ds.log.Debugf("DUMP %v", addr)
						if !addr.Assigned() && !addr.inCoolingPeriod() {
							ds.assignPodIPv4AddressUnsafe(ipamKey, eni, addr)
							if err := ds.writeBackingStoreUnsafe(); err != nil {
								ds.log.Warnf("Failed to update backing store: %v", err)
								// Important! Unwind assignment
								ds.unassignPodIPv4AddressUnsafe(addr)
								return "", -1, err
							}

							return addr.Address, eni.DeviceNumber, nil
						}
					}
				}
			}
		} else {
			ds.log.Debugf("PD is enabled.")
			for _, availableCidr := range eni.AvailableIPv4Cidrs {
				if availableCidr.IsPrefix {
					strPrivateIPv4, err := ds.getFreeIPv4AddrfromCidr(availableCidr)
					if err != nil {
						ds.log.Debugf("Unable to get IP address from prefix: %v", err)
						//Check in next CIDR
						continue
					}
					ds.log.Debugf("New IP - %s", strPrivateIPv4)
					err = ds.AddPrefixIPv4AddressToStore(eni.ID, strPrivateIPv4, availableCidr.Cidr)
					if err != nil {
						ds.log.Errorf("Failed to ADD PD IP %s on ENI %s", strPrivateIPv4, eni)
						return "", -1, err
					}
					addr := availableCidr.IPv4Addresses[strPrivateIPv4]
					ds.assignPodIPv4AddressUnsafe(ipamKey, eni, addr)

					addr.Prefix = availableCidr.Cidr

					if err := ds.writeBackingStoreUnsafe(); err != nil {
						ds.log.Warnf("Failed to update backing store: %v", err)
						// Important! Unwind assignment
						ds.unassignPodIPv4AddressUnsafe(addr)
						//Remove the IP from eni DB
						delete(availableCidr.IPv4Addresses, addr.Address)
						return "", -1, err
					}
					return addr.Address, eni.DeviceNumber, nil
				}
			}
		}
		ds.log.Debugf("AssignPodIPv4Address: ENI %s does not have available addresses", eni.ID)
	}
	ds.log.Errorf("DataStore has no available IP addresses")
	return "", -1, errors.New("assignPodIPv4AddressUnsafe: no available IP addresses")
}

// It returns the assigned IPv4 address, device number
func (ds *DataStore) assignPodIPv4AddressUnsafe(ipamKey IPAMKey, eni *ENI, addr *AddressInfo) (string, int) {
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

func (ds *DataStore) unassignPodIPv4AddressUnsafe(addr *AddressInfo) {
	if !addr.Assigned() {
		// Already unassigned
		return
	}
	ds.log.Infof("UnAssignPodIPv4Address: Unassign IP %v from sandbox %s",
		addr.Address, addr.IPAMKey)
	addr.IPAMKey = IPAMKey{} // unassign the addr
	ds.assigned--
	// Prometheus gauge
	assignedIPs.Set(float64(ds.assigned))
}

// GetStats returns total number of IP addresses and number of assigned IP addresses
func (ds *DataStore) GetStats() (int, int, int) {
	return ds.total, ds.assigned, ds.allocatedPrefix
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
			otherWarmIPs += len(other.AvailableIPv4Cidrs) - other.AssignedIPv4Addresses()
		}
	}
	return otherWarmIPs < warmIPTarget
}

// IsRequiredForMinimumIPTarget determines if this ENI is necessary to fulfill whatever MINIMUM_IP_TARGET is
// set to.
func (ds *DataStore) isRequiredForMinimumIPTarget(minimumIPTarget int, eni *ENI) bool {
	otherIPs := 0
	for _, other := range ds.eniPool {
		if other.ID != eni.ID {
			otherIPs += len(other.AvailableIPv4Cidrs)
		}
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
				if otherPrefixes.UsedIPs == 0 {
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

		if !eni.IsPDEnabled && warmIPTarget != 0 && ds.isRequiredForWarmIPTarget(warmIPTarget, eni) {
			ds.log.Debugf("ENI %s cannot be deleted because it is required for WARM_IP_TARGET: %d", eni.ID, warmIPTarget)
			continue
		}

		if !eni.IsPDEnabled && minimumIPTarget != 0 && ds.isRequiredForMinimumIPTarget(minimumIPTarget, eni) {
			ds.log.Debugf("ENI %s cannot be deleted because it is required for MINIMUM_IP_TARGET: %d", eni.ID, minimumIPTarget)
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

		if eni.IsPDEnabled && warmPrefixTarget != 0 && ds.isRequiredForWarmPrefixTarget(warmPrefixTarget, eni) {
			ds.log.Debugf("ENI %s cannot be deleted because it is required for WARM_PREFIX_TARGET: %d", eni.ID, warmPrefixTarget)
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
		for _, addr := range assignedaddr.IPv4Addresses {
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
		if !eni.IsPDEnabled && len(eni.AvailableIPv4Cidrs) < maxIPperENI {
			ds.log.Debugf("Found ENI %s that has less than the maximum number of IP addresses allocated: cur=%d, max=%d",
				eni.ID, len(eni.AvailableIPv4Cidrs), maxIPperENI)
			return eni
		} else if eni.IsPDEnabled && len(eni.AvailableIPv4Cidrs) < maxIPperENI {
			ds.log.Debugf("Found ENI %s that has less than the maximum number of v4 Prefixes allocated: cur=%d, max=%d",
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
	if !ds.eniPool[removableENI].IsPDEnabled {
		eniIPCount := len(ds.eniPool[removableENI].AvailableIPv4Cidrs)
		ds.total -= eniIPCount
		ds.log.Infof("RemoveUnusedENIFromStore %s: IP address pool stats: free %d addresses, total: %d, assigned: %d",
			removableENI, eniIPCount, ds.total, ds.assigned)
	} else {
		_, numIPsPerPrefix, _ := GetPrefixDelegationDefaults()
		ds.total = ds.total - (len(ds.eniPool[removableENI].AvailableIPv4Cidrs) * numIPsPerPrefix)
		ds.allocatedPrefix -= len(ds.eniPool[removableENI].AvailableIPv4Cidrs)
		ds.log.Infof("RemoveUnusedENIFromStore %s: Prefix address pool stats: free %d addresses, total: %d, assigned: %d total prefixes: %d",
			removableENI, len(ds.eniPool[removableENI].AvailableIPv4Cidrs), ds.total, ds.assigned, ds.allocatedPrefix)
	}
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
			for _, addr := range assignedaddr.IPv4Addresses {
				if addr.Assigned() {
					ds.unassignPodIPv4AddressUnsafe(addr)
				}
			}
		}
		if err := ds.writeBackingStoreUnsafe(); err != nil {
			ds.log.Warnf("Unable to update backing store: %v", err)
			// Continuing, because 'force'
		}
	}

	if !eni.IsPDEnabled {
		ds.total -= len(eni.AvailableIPv4Cidrs)
		ds.log.Infof("RemoveENIFromDataStore %s: IP address pool stats: free %d addresses, total: %d, assigned: %d",
			eniID, len(eni.AvailableIPv4Cidrs), ds.total, ds.assigned)
	} else {
		_, numIPsPerPrefix, _ := GetPrefixDelegationDefaults()
		ds.total = ds.total - (len(eni.AvailableIPv4Cidrs) * numIPsPerPrefix)
		ds.allocatedPrefix -= len(eni.AvailableIPv4Cidrs)
		ds.log.Infof("RemoveENIFromDataStore %s: Prefix address pool stats: free %d addresses, total: %d, assigned: %d total prefixes: %d",
			eniID, len(eni.AvailableIPv4Cidrs), ds.total, ds.assigned, ds.allocatedPrefix)
	}
	delete(ds.eniPool, eniID)

	// Prometheus gauge
	enis.Set(float64(len(ds.eniPool)))
	return nil
}

// UnassignPodIPv4Address a) find out the IP address based on PodName and PodNameSpace
// b)  mark IP address as unassigned c) returns IP address, ENI's device number, error
func (ds *DataStore) UnassignPodIPv4Address(ipamKey IPAMKey) (e *ENI, ip string, deviceNumber int, isSecondaryIP bool, err error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()
	ds.log.Debugf("UnassignPodIPv4Address: IP address pool stats: total:%d, assigned %d, sandbox %s",
		ds.total, ds.assigned, ipamKey)

	eni, addr := ds.eniPool.FindAddressForSandbox(ipamKey)
	isSecondaryIP = false
	if addr == nil {
		// This `if` block should be removed when the CRI
		// migration code is finally removed.  Leaving a
		// compile dependency here to make that obvious :P
		var _ = checkpointMigrationPhase

		// If the entry was discovered by querying CRI at
		// restart rather than by observing an ADD operation
		// directly, then we won't have captured the true
		// networkname/ifname.
		ds.log.Debugf("UnassignPodIPv4Address: Failed to find IPAM entry under full key, trying CRI-migrated version")
		ipamKey.NetworkName = backfillNetworkName
		ipamKey.IfName = backfillNetworkIface
		eni, addr = ds.eniPool.FindAddressForSandbox(ipamKey)
	}
	if addr == nil {
		ds.log.Warnf("UnassignPodIPv4Address: Failed to find sandbox %s",
			ipamKey)
		return nil, "", 0, false, ErrUnknownPod
	}

	ds.unassignPodIPv4AddressUnsafe(addr)
	if err := ds.writeBackingStoreUnsafe(); err != nil {
		// Unwind un-assignment
		ds.assignPodIPv4AddressUnsafe(ipamKey, eni, addr)
		return nil, "", 0, false, err
	}
	addr.UnassignedTime = time.Now()
	retrievedPrefix := addr.Prefix
	if eni.IsPDEnabled && retrievedPrefix.IP == nil {
		ds.log.Infof("Prefix delegation is enabled and the IP is from secondary pool hence no need to update prefix pool")
		ds.total--
		isSecondaryIP = true
	} else if retrievedPrefix.IP != nil {
		ds.log.Infof("Retrieved Prefix %s", retrievedPrefix.String())
		//Regular pod deletes for PD enabled and if pd is disabled but prefix is populated i.e, knob disable scenario
		//deleteIPv4AddrfromPrefix(eni.IPv4Prefixes[retrievedPrefix.String()], addr)
		//Remove the IP from eni DB

		//cidr := net.IPNet{IP: net.ParseIP(addr.Address), Mask: net.IPv4Mask(255, 255, 255, 255) }
		//delete(eni.AvailableIPv4Cidrs[cidr.String()].IPv4Addresses, addr.Address)
		if !eni.IsPDEnabled {
			ds.log.Infof("Prefix delegation is disabled but IP belongs to prefix pool")
			isSecondaryIP = true
		}
	}

	ds.log.Infof("UnassignPodIPv4Address: sandbox %s's ipAddr %s, DeviceNumber %d",
		ipamKey, addr.Address, eni.DeviceNumber)
	return eni, addr.Address, eni.DeviceNumber, isSecondaryIP, nil
}

// AllocatedIPs returns a recent snapshot of allocated sandbox<->IPs.
// Note result may already be stale by the time you look at it.
func (ds *DataStore) AllocatedIPs() []PodIPInfo {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	ret := make([]PodIPInfo, 0, ds.eniPool.AssignedIPv4Addresses())
	for _, eni := range ds.eniPool {
		for _, assignedaddr := range eni.AvailableIPv4Cidrs {
			for _, addr := range assignedaddr.IPv4Addresses {
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
func (ds *DataStore) FreeableIPs(eniID string) []string {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	eni := ds.eniPool[eniID]
	if eni == nil {
		// Can't free any IPs from an ENI we don't know about...
		return []string{}
	}

	freeable := make([]string, 0, len(eni.AvailableIPv4Cidrs))
	for _, assignedaddr := range eni.AvailableIPv4Cidrs {
		for _, addr := range assignedaddr.IPv4Addresses {
			if !addr.Assigned() && !assignedaddr.IsPrefix {
				freeable = append(freeable, addr.Address)
			}
		}
	}

	return freeable
}

// FreeablePrefixes returns a list of unused and potentially freeable IPs.
// Note result may already be stale by the time you look at it.
func (ds *DataStore) FreeablePrefixes(eniID string) []string {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	eni := ds.eniPool[eniID]
	if eni == nil {
		// Can't free any IPs from an ENI we don't know about...
		return []string{}
	}
	ds.log.Debugf("In freeable prefix for eni %s", eniID)

	freeable := make([]string, 0, len(eni.AvailableIPv4Cidrs))
	for _, assignedaddr := range eni.AvailableIPv4Cidrs {
		if assignedaddr.IsPrefix && assignedaddr.UsedIPs == 0 {
			freeable = append(freeable, assignedaddr.Cidr.String())
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
		tmpENIInfo.AvailableIPv4Cidrs = make(map[string]*AssignedIPv4Addresses, len(eniInfo.AvailableIPv4Cidrs))
		for cidr, _ := range eniInfo.AvailableIPv4Cidrs {
			tmpENIInfo.AvailableIPv4Cidrs[cidr] = &AssignedIPv4Addresses{Cidr: eniInfo.AvailableIPv4Cidrs[cidr].Cidr, IPv4Addresses: make(map[string]*AddressInfo, len(eniInfo.AvailableIPv4Cidrs[cidr].IPv4Addresses)), UsedIPs: 0}
			for ip, ipAddrInfoRef := range eniInfo.AvailableIPv4Cidrs[cidr].IPv4Addresses {
				ipAddrInfo := *ipAddrInfoRef
				tmpENIInfo.AvailableIPv4Cidrs[cidr].IPv4Addresses[ip] = &ipAddrInfo
				tmpENIInfo.AvailableIPv4Cidrs[cidr].UsedIPs = eniInfo.AvailableIPv4Cidrs[cidr].UsedIPs
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

// GetENICIDRs returns the known (allocated & unallocated) ENI IPs.
func (ds *DataStore) GetENICIDRs(eniID string) ([]string, []string, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	eni, ok := ds.eniPool[eniID]
	if !ok {
		return nil, nil, errors.New(UnknownENIError)
	}

	var ipPool = make([]string, 0)
	var prefixPool = make([]string, 0)
	for _, assignedAddr := range eni.AvailableIPv4Cidrs {
		if !assignedAddr.IsPrefix {
			ipPool = append(ipPool, assignedAddr.Cidr.IP.String())
		} else {
			prefixPool = append(ipPool, assignedAddr.Cidr.String())
		}
	}
	return ipPool, prefixPool, nil
}

// GetENIPrefixes returns the known (allocated & unallocated) ENI Prefixed.
func (ds *DataStore) GetENIPrefixes(eniID string) ([]string, error) {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	eni, ok := ds.eniPool[eniID]
	if !ok {
		return nil, errors.New(UnknownENIError)
	}

	var ipPool = make([]string, 0)
	for _, prefixData := range eni.AvailableIPv4Cidrs {
		if prefixData.IsPrefix {
			ipPool = append(ipPool, prefixData.Cidr.String())
		}
	}
	return ipPool, nil
}

func (ds *DataStore) GetFreePrefixes() int {
	ds.lock.Lock()
	defer ds.lock.Unlock()

	freePrefixes := 0
	for _, other := range ds.eniPool {
		for _, otherPrefixes := range other.AvailableIPv4Cidrs {
			if otherPrefixes.IsPrefix && otherPrefixes.UsedIPs == 0 {
				freePrefixes++
			}
		}

	}
	return freePrefixes
}

func (ds *DataStore) CleanupDataStore(eniID string, ipv4 string, force bool, enableIpv4PrefixDelegation bool) error {
	var err error
	var ipv4Cidr net.IPNet
	var ipv4CidrPtr *net.IPNet
	if !enableIpv4PrefixDelegation {
		ipv4Cidr = net.IPNet{IP: net.ParseIP(ipv4), Mask: net.IPv4Mask(255, 255, 255, 255)}
	} else {
		_, ipv4CidrPtr, err = net.ParseCIDR(ipv4)
		if err != nil {
			return err
		}
		ipv4Cidr = *ipv4CidrPtr
	}
	err = ds.DelIPv4CidrFromStore(eniID, ipv4Cidr, force)
	return err
}

func (ds *DataStore) getFreeIPv4AddrfromCidr(availableCidr *AssignedIPv4Addresses) (string, error) {
	if availableCidr == nil {
		ds.log.Errorf("Prefix datastore not initialized")
		return "", errors.New("Prefix datastore not initialized")
	}
	strPrivateIPv4, err := ds.getUnusedIP(availableCidr)
	if err != nil {
		ds.log.Debugf("Get free IP from prefix failed %v", err)
		return "", err
	}
	availableCidr.UsedIPs++
	ds.log.Debugf("Returning Free IP %s", strPrivateIPv4)
	return strPrivateIPv4, nil
}

/*
  /28 -> 10.1.1.1/32[out of cooldown], 10.1.1.2/32[out of cooldown], 10.1.1.3/32[assigned]
  cached ip = 10.1.1.1/32
  /28 ->  10.1.1.3/32[assigned]
  return 10.1.1.1/32
*/

func (ds *DataStore) getUnusedIP(availableCidr *AssignedIPv4Addresses) (string, error) {
	//Check if there is any IP out of cooldown
	var cachedIP string
	for _, addr := range availableCidr.IPv4Addresses {
		if !addr.Assigned() && !addr.inCoolingPeriod() {
			//if the IP is out of cooldown and not assigned then cache the first available IP
			//continue cleaning up the DB, this is to avoid stale entries and a new thread :)
			if cachedIP == "" {
				cachedIP = addr.Address
			}
			delete(availableCidr.IPv4Addresses, addr.Address)
		}
	}

	if cachedIP != "" {
		return cachedIP, nil
	}

	//If not in cooldown then generate next IP
	ip, ipnet, err := net.ParseCIDR(availableCidr.Cidr.String())
	if err != nil {
		return "", err
	}

	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); getNextIPv4Addr(ip) {
		strPrivateIPv4 := ip.String()
		if _, ok := availableCidr.IPv4Addresses[strPrivateIPv4]; ok {
			continue
		}
		ds.log.Debugf("Found a free IP not in DB - %s", strPrivateIPv4)
		return strPrivateIPv4, nil
	}

	return "", errors.New("No free IP in the prefix store")
}

func getNextIPv4Addr(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

//Given a IPv4 address, this will compute the associated prefix
func getPrefixFromIPv4Addr(IPaddr string) net.IPNet {
	_, _, supportedPrefixLen := GetPrefixDelegationDefaults()
	ipv4Prefix := net.ParseIP(IPaddr)
	ipv4PrefixMask := net.CIDRMask(supportedPrefixLen, ipv4DefaultPrefixSize)
	ipv4Prefix = ipv4Prefix.To4()
	ipv4Prefix = ipv4Prefix.Mask(ipv4PrefixMask)
	return net.IPNet{IP: net.ParseIP(ipv4Prefix.String()), Mask: ipv4PrefixMask}
}

//Function to return PD defaults supported by VPC
func GetPrefixDelegationDefaults() (int, int, int) {
	numPrefixesPerENI := 1
	numIPsPerPrefix := 16
	supportedPrefixLen := 28

	return numPrefixesPerENI, numIPsPerPrefix, supportedPrefixLen
}
