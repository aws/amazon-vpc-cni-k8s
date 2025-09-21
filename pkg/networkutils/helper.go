package networkutils

import "net"

func CalculateRouteTableId(deviceNumber int, networkCardIndex int, maxENIsPerNetworkCard int) int {
	if networkCardIndex == 0 {
		return deviceNumber + 1
	} else {
		return deviceNumber + 1 + (networkCardIndex * maxENIsPerNetworkCard)
	}
}

func CalculatePodIPv4GatewayIP(index int) net.IP {
	return net.IPv4(169, 254, 1, byte(index)+1)
}

func CalculatePodIPv6GatewayIP(index int) net.IP {
	return net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, byte(index) + 1}
}

func IsIPv4(ip net.IP) bool {
	return ip.To4() != nil
}
