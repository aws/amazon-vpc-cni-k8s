// +build !linux

package driver

import netlink "github.com/vishvananda/netlink"

var (
	SCOPE_LINK    = netlink.Scope(0)
	NUD_PERMANENT = 0
)
