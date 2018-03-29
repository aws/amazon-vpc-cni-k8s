// +build linux

package network

import "github.com/vishvananda/netlink"

var (
	SCOPE_LINK     = netlink.SCOPE_LINK
	SCOPE_UNIVERSE = netlink.SCOPE_UNIVERSE
)

// RouteReplace will add a route to the system.
// Equivalent to: `ip route replace $route`
func RouteReplace(route *netlink.Route) error {
	return netlink.RouteReplace(route)
}
