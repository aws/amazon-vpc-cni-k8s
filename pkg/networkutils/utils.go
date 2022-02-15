package networkutils

import (
	"encoding/binary"
	"fmt"
	"net"
)

// IncrementIPv4Addr returns incremented IPv4 address
func IncrementIPv4Addr(ip net.IP) (net.IP, error) {
	ip4 := ip.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("%q is not a valid IPv4 Address", ip)
	}
	intIP := binary.BigEndian.Uint32(ip4)
	if intIP == (1<<32 - 1) {
		return nil, fmt.Errorf("%q will be overflowed", ip)
	}
	intIP++
	nextIPv4 := make(net.IP, 4)
	binary.BigEndian.PutUint32(nextIPv4, intIP)
	return nextIPv4, nil
}
