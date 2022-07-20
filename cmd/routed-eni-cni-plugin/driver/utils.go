package driver

import (
	"syscall"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper"
	"github.com/vishvananda/netlink"
)

// netLinkRuleDelAll deletes all matching route rules.
func netLinkRuleDelAll(netlink netlinkwrapper.NetLink, rule *netlink.Rule) error {
	for {
		if err := netlink.RuleDel(rule); err != nil {
			if !containsNoSuchRule(err) {
				return err
			}
			break
		}
	}
	return nil
}

func containsNoSuchRule(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.ENOENT
	}
	return false
}

func isRuleExistsError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.EEXIST
	}
	return false
}
