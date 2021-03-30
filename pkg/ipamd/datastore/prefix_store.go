package datastore

import (
	"fmt"
	//"math"
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
	IPsPerPrefix int64
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
func NewPrefixStore(prefixsize int64) PrefixIPsStore {
	//No of bytes needed for the bits
	len := prefixsize/8
	log.Infof("IPsperPrefix - %d and UsedIPs len %d", prefixsize, len+1)
	return PrefixIPsStore{IPsPerPrefix: prefixsize, UsedIPs: make([]byte, len), CooldownIPs: make([]time.Time, prefixsize)}
}

// Size returns the size of a bitmap. This is the number
// of bits.
func (prefix PrefixIPsStore) getPrefixSize() int64 {
	return prefix.IPsPerPrefix
}

// Validate - validates if the IP can be used
func (prefix PrefixIPsStore) Validate(pos int64) error {
	if pos > prefix.getPrefixSize() || pos < 1 {
		return fmt.Errorf("Invalid index requested")
	}
	return nil
}

func getIPlocation(pos int64) (int64, int64) {
	octet := pos >> 3
	bitpos := pos - (octet * 8)

	return octet, bitpos
}

//SetUnset - Bit will be changes to 0 <-> 1
func (prefix PrefixIPsStore) SetUnset(pos int64) {
	octet, bitpos := getIPlocation(pos)

	if bitpos == 1 {
		prefix.UsedIPs[octet] = prefix.UsedIPs[octet] ^ 1
	} else {
		prefix.UsedIPs[octet] = prefix.UsedIPs[octet] ^ (1 << int(bitpos-1))
	}
}

// isIPUsed - Checks if the IP is already used
func (prefix PrefixIPsStore) isIPUsed(pos int64) bool {
	if err := prefix.Validate(pos); err != nil {
		return false
	}

	octet, bitpos := getIPlocation(pos)
	if bitpos == 1 {
		return prefix.UsedIPs[octet] > prefix.UsedIPs[octet]^1
	} else {
		return prefix.UsedIPs[octet] > prefix.UsedIPs[octet]^(1<<uint(bitpos-1))
	}
}

func (prefix PrefixIPsStore) getPosOfRightMostUnsetBit(n byte, octetlen int) int {
	/*
	if n == 1 && octet == 0 {
		return -1
	}

	value := (float64)(^n & (n + 1))
	return math.Log(value) / math.Log(2)
	*/
	/*
	// if n = 0, return 1
    if (n == 0) {
        return 1
	}
      
    // if all bits of 'n' are set
    if ((n & (n + 1)) == 0) {  
        return -1
	}
      
    n = ^n
	value := (float64)(n & -n)
    return math.Log2(value)  
	*/ 
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
func (prefix PrefixIPsStore) getIPfromPrefix() (int64, error) {
	DBlen := (int)(prefix.IPsPerPrefix/8 + 1)
    log.Infof("In get IP from prefix - %d", DBlen)
	var octet int
	for octet = 0; octet < DBlen; octet++ {
		log.Infof("O/p of DATA %d", prefix.UsedIPs[octet])
		var index = (int)(prefix.getPosOfRightMostUnsetBit(prefix.UsedIPs[octet], binary.Size((prefix.UsedIPs[octet]))))
		log.Infof("Found Index %d", index)
		if index != -1 {
			IPindex := (int64)((octet * 8) + index)
			log.Infof("Return IPindex is %d", IPindex)
		    prefix.UsedIPs[octet] = prefix.UsedIPs[octet] ^ (1 << int(index))	
			log.Infof("DUMP - %x",prefix.UsedIPs[octet])
			return IPindex, nil
		}
	}
	return -1, errors.New("No free index")
}

func (prefix PrefixIPsStore) freeIPtoPrefix(IPindex int64) error {
	if err := prefix.Validate(IPindex); err != nil {
		return err
	}
	prefix.SetUnset(IPindex)
	return nil
}
