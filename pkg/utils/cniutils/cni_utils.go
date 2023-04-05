package cniutils

import (
	"bytes"
	"fmt"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/utils/imds"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"net"
	"os"
	"syscall"
	"time"
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

func GetIPsByInterfaceName(netns ns.NetNS, ifName string, filter func(net.IP) bool) (containerIPv6 []net.IP, err error) {
	var worker = func(ifName string, filter func(net.IP) bool) error {
		containerIf, err := net.InterfaceByName(ifName)
		if err != nil {
			return err
		}
		addrs, err := containerIf.Addrs()
		if err != nil {
			return err
		}
		for _, addr := range addrs {
			ip :=  addr.(*net.IPNet).IP
			if filter(ip) {
				containerIPv6 = append(containerIPv6, ip)
			}
		}
		return nil
	}
	if netns != nil {
		err = netns.Do(func(hostNS ns.NetNS) error {
			return worker(ifName, filter)
		})
	} else {
		err = worker(ifName, filter)
	}
	return containerIPv6, err
}

func GetHostPrimaryInterfaceName() (string, error) {
	var hostPrimaryIfName string

	// figure out host primary interface
	primaryMAC, err := imds.GetMetaData("mac")
	if err != nil {
		return "", err
	}

	links, err := netlink.LinkList()
	if err != nil {
		return "", err
	}

	for _, link := range links {
		if link.Attrs().HardwareAddr.String() == primaryMAC {
			hostPrimaryIfName = link.Attrs().Name
			break
		}
	}
	return hostPrimaryIfName, nil
}

func EnableIPv6Accept_ra(ifName string, value string) error {
	var entry = "/proc/sys/net/ipv6/conf/" + ifName + "/accept_ra"

	if content, err := os.ReadFile(entry); err == nil {
		if bytes.Equal(bytes.TrimSpace(content), []byte(value)) {
			return nil
		}
	}
	return os.WriteFile(entry, []byte(value), 0644)
}

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

