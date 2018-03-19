// +build linux

package driver

import netlink "github.com/vishvananda/netlink"

var (
	SCOPE_LINK    = netlink.SCOPE_LINK
	NUD_PERMANENT = netlink.NUD_PERMANENT
)
