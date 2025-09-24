package networkutils

import (
	"net"

	"golang.org/x/sys/unix"
)

// BaseNumber is the base offset for multi-NIC route table IDs.
// This value is chosen to match the logic in Amazon EC2 net utils:
// https://github.com/amazonlinux/amazon-ec2-net-utils/blob/v2.7.1/lib/lib.sh#L301
const BaseNumber = 10000

func CalculateOldRouteTableId(deviceNumber int, networkCardIndex int, maxENIsPerNetworkCard int) int {
	return deviceNumber + 1 + (networkCardIndex * maxENIsPerNetworkCard)
}

func CalculateRouteTableId(deviceNumber int, networkCardIndex int) int {
	if networkCardIndex == 0 && deviceNumber == 0 {
		return unix.RT_TABLE_MAIN
	} else if networkCardIndex == 0 {
		return deviceNumber + 1
	} else {
		return BaseNumber + deviceNumber + (100 * networkCardIndex)
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
