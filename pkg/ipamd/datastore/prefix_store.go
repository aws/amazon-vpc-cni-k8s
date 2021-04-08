package datastore

import (
	"fmt"
	"net"
	"time"
	"encoding/binary"

	"github.com/pkg/errors"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)


var log = logger.Get()
// PrefixIPsStore will hold the size of the prefix Eg: 16 for /28 prefix
// UsedIPs will be a bitmap of size - IPsPerPrefix which will keep track of IPs used
type PrefixIPsStore struct {
	UsedIPs      []byte
	CooldownIPs  []time.Time
	IPsPerPrefix int
}

// ENIPrefix will have the prefixes allocated for an ENI and the list of allocated IPs under
// the prefix
type ENIPrefix struct {
	Prefix       string
	PrefixLen    int
	AllocatedIPs PrefixIPsStore
	UsedIPs      int64
	FreeIps      int64
}

//New - allocate a PrefixIPsStore per prefix of size "prefixsize"
func NewPrefixStore(prefixsize int) PrefixIPsStore {
	//No of bytes needed for the bits
	len := prefixsize/8
	log.Infof("IPsperPrefix - %d and UsedIPs len %d", prefixsize, len+1)
	return PrefixIPsStore{IPsPerPrefix: prefixsize, UsedIPs: make([]byte, len), CooldownIPs: make([]time.Time, prefixsize)}
}

// Size returns the size of a bitmap. This is the number
// of bits.
func (prefix PrefixIPsStore) getPrefixSize() int {
	return prefix.IPsPerPrefix
}

// Validate - validates if the IP can be used
func (prefix PrefixIPsStore) Validate(pos int) error {
	if pos >= prefix.getPrefixSize() || pos < 0 {
		return fmt.Errorf("Invalid index requested")
	}
	return nil
}

//SetUnsetIPallocation - Bit will be changes to 0 <-> 1
func (prefix PrefixIPsStore) SetUnsetIPallocation(IPindex byte) error {
	if err := prefix.Validate(int(IPindex)); err != nil {
		log.Infof("Invalid IPindex - %d", IPindex)
		return err
	}
	octet := IPindex/8
	index := IPindex%8
	prefix.UsedIPs[octet] = prefix.UsedIPs[octet] ^ (1 << index) 
	return nil
}

func (prefix PrefixIPsStore) getPosOfRightMostUnsetBit(n byte, octetlen int) int {
	log.Infof("Size of byte array %d", octetlen)
	var i int
	for i = 0; i < octetlen * 8; i++ {
		log.Infof("Current time since - %v and cooldown %d", time.Since(prefix.CooldownIPs[i]), addressCoolingPeriod)
		if ((((n >> i) & 1) == 0) && !(time.Since(prefix.CooldownIPs[i]) <= addressCoolingPeriod))  {
			log.Infof("Cooldown at index %d - %d", i, prefix.CooldownIPs[i])
			return i
		}
	}
	return -1
}

// getIpfromPrefix - Returns a free IP in the prefix
func (prefix PrefixIPsStore) getIPfromPrefix() (int, error) {
	DBlen := (prefix.IPsPerPrefix/8) 
    log.Infof("In get IP from prefix - %d", DBlen)
	var octet int
	for octet = 0; octet < DBlen; octet++ {
		log.Infof("O/p of DATA %d", prefix.UsedIPs[octet])
		var index = (int)(prefix.getPosOfRightMostUnsetBit(prefix.UsedIPs[octet], binary.Size((prefix.UsedIPs[octet]))))
		log.Infof("Found Index %d", index)
		if index != -1 {
			IPindex := (int)((octet * 8) + index)
			log.Infof("Return IPindex is %d", IPindex)
		    prefix.UsedIPs[octet] = prefix.UsedIPs[octet] ^ (1 << int(index))	
			log.Infof("DUMP - %x",prefix.UsedIPs[octet])
			return IPindex, nil
		}
	}
	return -1, errors.New("No free index")
}

func getIPv4AddrfromPrefix(prefix *ENIPrefix) (string, int, error) {
	IPoffset, err := prefix.AllocatedIPs.getIPfromPrefix()
	if err != nil {
		log.Errorf("Mismtach between prefix free IPs and available IPs: %v", err)
		return "", -1, err
	}
	prefix.FreeIps--
	prefix.UsedIPs++

	log.Infof("Got ip offset - %d", IPoffset)
    strPrivateIPv4 := getIPfromPrefix(prefix, IPoffset)
	return strPrivateIPv4, IPoffset, nil
}

func getPrefixFromIPv4Addr(IPaddr string) (net.IP) {
	ipv4Prefix := net.ParseIP(IPaddr)
	ipv4PrefixMask := net.CIDRMask(28, 32)
	ipv4Prefix = ipv4Prefix.To4()
	ipv4Prefix = ipv4Prefix.Mask(ipv4PrefixMask)
	return ipv4Prefix
}

func getPrefixIndexfromIP(ipAddr string, ipv4Prefix net.IP) (byte) {
	ipv4Addr := net.ParseIP(ipAddr)
	ipv4AddrMask := net.CIDRMask(32, 32)
	ipv4Addr = ipv4Addr.To4()
	ipv4Addr = ipv4Addr.Mask(ipv4AddrMask)

	IPindex := ipv4Addr[3] - ipv4Prefix[3]
	return IPindex
}

func getIPfromPrefix(prefix *ENIPrefix, IPoffset int) string {
	ipv4Addr := net.ParseIP(prefix.Prefix)
	ipv4Mask := net.CIDRMask(prefix.PrefixLen, 32)
	ipv4Addr = ipv4Addr.To4()
	ipv4Addr = ipv4Addr.Mask(ipv4Mask)
	offset := make([]byte, 8)
					
	binary.LittleEndian.PutUint32(offset, uint32(IPoffset))
	log.Infof("BEFORE Last octet - %d", ipv4Addr[3])
	ipv4Addr[3] = ipv4Addr[3] + offset[0]
	log.Infof("AFTER Last octet - %d", ipv4Addr[3])
	return ipv4Addr.String() 
}
