// +build !linux

package network

import "github.com/vishvananda/netlink"

var (
	SCOPE_LINK     = netlink.Scope(0)
	SCOPE_UNIVERSE = netlink.Scope(0)
)

// RouteReplace will add a route to the system.
// Equivalent to: `ip route replace $route`
func RouteReplace(route *netlink.Route) error {
	return nil
}
