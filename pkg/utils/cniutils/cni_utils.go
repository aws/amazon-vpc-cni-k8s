package cniutils

import (
	"fmt"
	"strings"
	"syscall"
	"time"

	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/coreos/go-iptables/iptables"
	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/procsyswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/utils/imds"
	"github.com/aws/aws-sdk-go/service/ec2"
)

const (
	ipv4ForwardKey = "net/ipv4/ip_forward"
	ipv6ForwardKey = "net/ipv6/conf/all/forwarding"
)

func FindInterfaceByName(ifaceList []*current.Interface, ifaceName string) (ifaceIndex int, iface *current.Interface, found bool) {
	for ifaceIndex, iface := range ifaceList {
		if iface.Name == ifaceName {
			return ifaceIndex, iface, true
		}
	}
	return 0, nil, false
}

func FindIPConfigsByIfaceIndex(ipConfigs []*current.IPConfig, ifaceIndex int) []*current.IPConfig {
	var matchedIPConfigs []*current.IPConfig
	for _, ipConfig := range ipConfigs {
		if ipConfig.Interface != nil && *ipConfig.Interface == ifaceIndex {
			matchedIPConfigs = append(matchedIPConfigs, ipConfig)
		}
	}
	return matchedIPConfigs
}

// WaitForAddressesToBeStable Implements `SettleAddresses` functionality of the `ip` package.
// waitForAddressesToBeStable waits for all addresses on a link to leave tentative state.
// Will be particularly useful for ipv6, where all addresses need to do DAD.
// If any addresses are still tentative after timeout seconds, then error.
func WaitForAddressesToBeStable(netLink netlinkwrapper.NetLink, ifName string, timeout, waitInterval time.Duration) error {
	link, err := netLink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to retrieve link: %v", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		addrs, err := netLink.AddrList(link, netlink.FAMILY_V6)
		if err != nil {
			return fmt.Errorf("could not list addresses: %v", err)
		}

		ok := true
		for _, addr := range addrs {
			if addr.Flags&(syscall.IFA_F_TENTATIVE|syscall.IFA_F_DADFAILED) > 0 {
				ok = false
				break
			}
		}

		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("link %s still has tentative addresses after %d seconds",
				ifName,
				timeout)
		}

		time.Sleep(waitInterval)
	}
}

// GetNodeMetadata calling node local imds metadata service using provided key
// return either a non-empty value or an error
func GetNodeMetadata(key string) (string, error) {
	var value string
	var err error
	for {
		value, err = imds.GetMetaData(key)
		if err != nil {
			return "", err
		}
		if value != "" {
			return value, nil
		}
	}
}

// EnableIpForwarding sets forwarding to 1 for both IPv4 and IPv6 if applicable.
// This func is to have a unit testable version of ip.EnableForward in ipforward_linux.go file
// link: https://github.com/containernetworking/plugins/blob/main/pkg/ip/ipforward_linux.go#L34
func EnableIpForwarding(procSys procsyswrapper.ProcSys, ips []*current.IPConfig) error {
	v4 := false
	v6 := false

	for _, ip := range ips {
		isV4 := ip.Address.IP.To4() != nil
		if isV4 && !v4 {
			valueV4, err := procSys.Get(ipv4ForwardKey)
			if err != nil {
				return err
			}
			if valueV4 != "1" {
				err = procSys.Set(ipv4ForwardKey, "1")
				if err != nil {
					return err
				}
			}
			v4 = true
		} else if !isV4 && !v6 {
			valueV6, err := procSys.Get(ipv6ForwardKey)
			if err != nil {
				return err
			}
			if valueV6 != "1" {
				err = procSys.Set(ipv6ForwardKey, "1")
				if err != nil {
					return err
				}
			}
			v6 = true
		}
	}
	return nil
}

// IsLinkNotFoundError return true if err contains "Link not found"
func IsLinkNotFoundError(err error) bool {
	return strings.Contains(err.Error(), "Link not found")
}

// IsIptableTargetNotExist returns true if the error is from iptables indicating
// that the target does not exist.
func IsIptableTargetNotExist(err error) bool {
	e, ok := err.(*iptables.Error)
	if !ok {
		return false
	}
	return e.IsNotExist()
}

// PrefixSimilar checks if prefix pool and eni prefix are equivalent.
func PrefixSimilar(prefixPool []string, eniPrefixes []*ec2.Ipv4PrefixSpecification) bool {
	if len(prefixPool) != len(eniPrefixes) {
		return false
	}

	prefixPoolSet := make(map[string]struct{}, len(prefixPool))
	for _, ip := range prefixPool {
		prefixPoolSet[ip] = struct{}{}
	}

	for _, prefix := range eniPrefixes {
		if prefix == nil || prefix.Ipv4Prefix == nil {
			return false
		}
		if _, exists := prefixPoolSet[*prefix.Ipv4Prefix]; !exists {
			return false
		}
	}
	return true
}

// IPsSimilar checks if ipPool and eniIPs are equivalent.
func IPsSimilar(ipPool []string, eniIPs []*ec2.NetworkInterfacePrivateIpAddress) bool {
	// Here we do +1 in ipPool because eniIPs will also have primary IP which is not used by pods.
	if len(ipPool) +1 != len(eniIPs) {
		return false
	}

	ipPoolSet := make(map[string]struct{}, len(ipPool))
	for _, ip := range ipPool {
		ipPoolSet[ip] = struct{}{}
	}

	for _, ip := range eniIPs {
		if ip == nil || ip.PrivateIpAddress == nil || ip.Primary == nil {
			return false
		}
		if *ip.Primary {
			continue
		}
		if _, exists := ipPoolSet[*ip.PrivateIpAddress]; !exists {
			return false
		}
	}
	return true
}
