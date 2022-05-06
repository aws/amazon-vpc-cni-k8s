package cniutils

import "github.com/containernetworking/cni/pkg/types/current"

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
