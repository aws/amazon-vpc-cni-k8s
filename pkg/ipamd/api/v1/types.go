package v1

import (
	"fmt"
	"net"
	"time"
)

const (
	// minENILifeTime is the shortest time before we consider deleting a newly created ENI
	minENILifeTime = 1 * time.Minute
)

// IPAMKey is the IPAM primary key.  Quoting CNI spec:
//
//	Plugins that store state should do so using a primary key of
//	(network name, CNI_CONTAINERID, CNI_IFNAME).
type IPAMKey struct {
	NetworkName string `json:"networkName"`
	ContainerID string `json:"containerID"`
	IfName      string `json:"ifName"`
}

// IsZero returns true if object is equal to the golang zero/null value.
func (k IPAMKey) IsZero() bool {
	return k == IPAMKey{}
}

// String() implements the fmt.Stringer interface.
func (k IPAMKey) String() string {
	return fmt.Sprintf("%s/%s/%s", k.NetworkName, k.ContainerID, k.IfName)
}

// IPAMMetadata is the metadata associated with IP allocations.
type IPAMMetadata struct {
	K8SPodNamespace string `json:"k8sPodNamespace,omitempty"`
	K8SPodName      string `json:"k8sPodName,omitempty"`
}

// ENI represents a single ENI. Exported fields will be marshaled for introspection.
type ENI struct {
	// CreateTime is excluded from marshalled representation, used only by internal datastore
	CreateTime time.Time `json:"-"`

	// AWS ENI ID
	ID string
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

// IsTooYoung returns true if the ENI hasn't been around long enough to be deleted.
func (e *ENI) IsTooYoung() bool {
	return time.Since(e.CreateTime) < minENILifeTime
}

// HasIPInCooling returns true if an IP address was unassigned recently.
func (e *ENI) HasIPInCooling(ipCooldownPeriod time.Duration) bool {
	for _, assignedaddr := range e.AvailableIPv4Cidrs {
		for _, addr := range assignedaddr.IPAddresses {
			if addr.InCoolingPeriod(ipCooldownPeriod) {
				return true
			}
		}
	}
	return false
}

// HasPods returns true if the ENI has pods assigned to it.
func (e *ENI) HasPods() bool {
	return e.AssignedIPv4Addresses() != 0
}

// AddressInfo contains information about an IP, Exported fields will be marshaled for introspection.
type AddressInfo struct {
	Address string

	IPAMKey        IPAMKey
	IPAMMetadata   IPAMMetadata
	AssignedTime   time.Time
	UnassignedTime time.Time
}

// CidrInfo
type CidrInfo struct {
	// Either v4/v6 Host or LPM Prefix
	Cidr net.IPNet
	// Key is individual IP addresses from the Prefix - /32 (v4) or /128 (v6)
	IPAddresses map[string]*AddressInfo
	// true if Cidr here is an LPM prefix
	IsPrefix bool
	// IP Address Family of the Cidr
	AddressFamily string
}

func (cidr *CidrInfo) Size() int {
	ones, bits := cidr.Cidr.Mask.Size()
	return (1 << (bits - ones))
}

func (e *ENI) findAddressForSandbox(ipamKey IPAMKey) (*CidrInfo, *AddressInfo) {
	// Either v4 or v6 for now.
	// Check in V4 prefixes
	for _, availableCidr := range e.AvailableIPv4Cidrs {
		for _, addr := range availableCidr.IPAddresses {
			if addr.IPAMKey == ipamKey {
				return availableCidr, addr
			}
		}
	}

	// Check in V6 prefixes
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

// AssignedIPAddressesInCidr is the number of IP addresses already assigned in the IPv4 CIDR
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

type CidrStats struct {
	AssignedIPs int
	CooldownIPs int
}

// Gets number of assigned IPs and the IPs in cooldown from a given CIDR
func (cidr *CidrInfo) GetIPStatsFromCidr(ipCooldownPeriod time.Duration) CidrStats {
	stats := CidrStats{}
	for _, addr := range cidr.IPAddresses {
		if addr.Assigned() {
			stats.AssignedIPs++
		} else if addr.InCoolingPeriod(ipCooldownPeriod) {
			stats.CooldownIPs++
		}
	}
	return stats
}

// Assigned returns true iff the address is allocated to a pod/sandbox.
func (addr AddressInfo) Assigned() bool {
	return !addr.IPAMKey.IsZero()
}

// InCoolingPeriod checks whether an addr is in ipCooldownPeriod
func (addr AddressInfo) InCoolingPeriod(ipCooldownPeriod time.Duration) bool {
	return time.Since(addr.UnassignedTime) <= ipCooldownPeriod
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

// ENIInfos contains ENI IP information
type ENIInfos struct {
	// TotalIPs is the total number of IP addresses
	TotalIPs int
	// assigned is the number of IP addresses that has been assigned
	AssignedIPs int
	// ENIs contains ENI IP pool information
	ENIs map[string]ENI
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
	IPv4                string       `json:"ipv4,omitempty"`
	IPv6                string       `json:"ipv6,omitempty"`
	AllocationTimestamp int64        `json:"allocationTimestamp"`
	Metadata            IPAMMetadata `json:"metadata"`
}

type DataStoreStats struct {
	// Total number of addresses allocated
	TotalIPs int
	// Total number of prefixes allocated
	TotalPrefixes int

	// Number of assigned addresses
	AssignedIPs int
	// Number of addresses in cooldown
	CooldownIPs int
}

func (stats *DataStoreStats) String() string {
	return fmt.Sprintf("Total IPs/Prefixes = %d/%d, AssignedIPs/CooldownIPs: %d/%d",
		stats.TotalIPs, stats.TotalPrefixes, stats.AssignedIPs, stats.CooldownIPs)
}

func (stats *DataStoreStats) AvailableAddresses() int {
	return stats.TotalIPs - stats.AssignedIPs
}
