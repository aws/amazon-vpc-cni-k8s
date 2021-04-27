package datastore

import (
	"net"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/pkg/errors"
)

const (
	//"/32" IPv4 prefix
	ipv4DefaultPrefixSize = 32
)

var log = logger.Get()

// PrefixIPsStore will hold the size of the prefix Eg: 16 for /28 prefix. In future if prefix sizes are customizable
// UsedIPs will be a map of <IP, cooldowntime>
type PrefixIPsStore struct {
	UsedIPs      map[string]time.Time
	IPsPerPrefix int
}

// ENIPrefix will have the prefixes allocated for an ENI and the list of allocated IPs under
// the prefix
type ENIPrefix struct {
	Prefix       net.IPNet
	AllocatedIPs PrefixIPsStore
	UsedIPs      int
	FreeIps      int
}

//New - allocate a PrefixIPsStore per prefix of size "prefixsize"
func NewPrefixStore(prefixsize int) PrefixIPsStore {
	return PrefixIPsStore{IPsPerPrefix: prefixsize, UsedIPs: make(map[string]time.Time)}
}

// Size returns the number of IPs per prefix
func (prefix PrefixIPsStore) getPrefixSize() int {
	return prefix.IPsPerPrefix
}

func isOutofCoolingPeriod(cooldown time.Time) bool {
	return (!(time.Since(cooldown) <= addressCoolingPeriod))
}

func getNextIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func (prefix PrefixIPsStore) getUnusedIP(eniPrefix string) (string, error) {
	//Check if there is any IP out of cooldown
	var cachedIP string
	for ipv4, cooldown := range prefix.UsedIPs {
		//Currently we have 2 DB's, IP allocated from the prefixDB is added to
		//IP pool so instead of checking if the IP is assigned from IPAMKey [ipAddr.Assigned()] considering an
		//ip with 0 cooldown time as assigned. Will fix this up as part of merging the 2 DBs.
		if isOutofCoolingPeriod(cooldown) && !cooldown.IsZero() {
			//if the IP is out of cooldown and not assigned then cache the first available IP
			//continue cleaning up the DB, this is to avoid stale entries and a new thread :)
			if cachedIP == "" {
				cachedIP = ipv4
			}
			delete(prefix.UsedIPs, ipv4)
		}
	}
	if cachedIP != "" {
		prefix.UsedIPs[cachedIP] = time.Time{}
		return cachedIP, nil
	}

	//If not in cooldown then generate next IP
	ip, ipnet, err := net.ParseCIDR(eniPrefix)
	if err != nil {
		return "", err
	}

	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); getNextIP(ip) {
		strPrivateIPv4 := ip.String()
		if _, ok := prefix.UsedIPs[strPrivateIPv4]; ok {
			continue
		}
		log.Debugf("Found a free IP not in DB - %s", strPrivateIPv4)
		return strPrivateIPv4, nil
	}

	return "", errors.New("No free IP in the prefix store")
}

func (prefix PrefixIPsStore) restorePrefixIP(recoveredIP string) error {
	// Already there
	_, ok := prefix.UsedIPs[recoveredIP]
	if ok {
		log.Debugf("IP already in DS")
		return errors.New(IPAlreadyInStoreError)
	}
	prefix.UsedIPs[recoveredIP] = time.Time{}
	log.Debugf("Recovered IP in prefix store %s", recoveredIP)
	return nil

}

//Given prefix, this function returns a free IP in the prefix
func getFreeIPv4AddrfromPrefix(prefix *ENIPrefix) (string, error) {
	if prefix == nil {
		log.Errorf("Prefix datastore not initialized")
		return "", errors.New("Prefix datastore not initialized")
	}
	strPrivateIPv4, err := prefix.AllocatedIPs.getUnusedIP(prefix.Prefix.String())
	if err != nil {
		log.Debugf("Get free IP from prefix failed %v", err)
		return "", err
	}
	prefix.AllocatedIPs.UsedIPs[strPrivateIPv4] = time.Time{}
	prefix.FreeIps--
	prefix.UsedIPs++
	log.Debugf("Returning Free IP %s", strPrivateIPv4)
	return strPrivateIPv4, nil
}

//This function sets the cooldown for the IP freed from the prefix
func deleteIPv4AddrfromPrefix(prefix *ENIPrefix, addr *AddressInfo) {
	prefix.AllocatedIPs.UsedIPs[addr.Address] = addr.UnassignedTime
	log.Debugf("Setting cooldown for IP address %s at time %v", addr.Address, addr.UnassignedTime)

	prefix.FreeIps++
	prefix.UsedIPs--
}

//Given a IPv4 address, this will compute the associated prefix
func getPrefixFromIPv4Addr(IPaddr string) net.IP {
	_, _, supportedPrefixLen := GetPrefixDelegationDefaults()
	ipv4Prefix := net.ParseIP(IPaddr)
	ipv4PrefixMask := net.CIDRMask(supportedPrefixLen, ipv4DefaultPrefixSize)
	ipv4Prefix = ipv4Prefix.To4()
	ipv4Prefix = ipv4Prefix.Mask(ipv4PrefixMask)
	return ipv4Prefix
}

//Function to return PD defaults supported by VPC
func GetPrefixDelegationDefaults() (int, int, int) {
	numPrefixesPerENI := 1
	numIPsPerPrefix := 16
	supportedPrefixLen := 28

	return numPrefixesPerENI, numIPsPerPrefix, supportedPrefixLen
}
