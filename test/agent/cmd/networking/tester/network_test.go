package tester

import (
	"net"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/sgpp"
	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestPPSGValidationRejectsUnsupportedMode(t *testing.T) {
	invalidMode := sgpp.EnforcingMode("invalid")
	if errs := TestNetworkingSetupForPodsUsingSecurityGroup(
		input.PodNetworkingValidationInput{}, invalidMode); len(errs) != 1 {
		t.Fatalf("setup errors = %d, want 1: %+v", len(errs), errs)
	}
	if errs := TestNetworkTearedDownForPodsUsingSecurityGroup(
		input.PodNetworkingValidationInput{}, invalidMode); len(errs) != 1 {
		t.Fatalf("cleanup errors = %d, want 1: %+v", len(errs), errs)
	}
}

func TestStandardPodRouteRules(t *testing.T) {
	podIP := net.ParseIP("10.0.0.10")
	podNet := podIPNet(podIP)
	otherNet := podIPNet(net.ParseIP("10.0.0.11"))

	rules := []netlink.Rule{
		{
			Dst:      podNet,
			Table:    unix.RT_TABLE_MAIN,
			Priority: standardToContainerRulePriority,
		},
		{
			Src:      podNet,
			Table:    101,
			Priority: standardFromPodRulePriority,
		},
		{
			Dst:      otherNet,
			Table:    unix.RT_TABLE_MAIN,
			Priority: standardToContainerRulePriority,
		},
		{
			Src:      podNet,
			Table:    unix.RT_TABLE_MAIN,
			Priority: standardFromPodRulePriority,
		},
	}

	toMain, fromBranch := standardPodRouteRules(rules, podIP)
	if len(toMain) != 1 {
		t.Fatalf("to-main rules = %d, want 1: %+v", len(toMain), toMain)
	}
	if len(fromBranch) != 1 {
		t.Fatalf("from-branch rules = %d, want 1: %+v", len(fromBranch), fromBranch)
	}
	if fromBranch[0].Table != 101 {
		t.Fatalf("from-branch table = %d, want 101", fromBranch[0].Table)
	}
}

func TestPodIPNet(t *testing.T) {
	tests := []struct {
		ip       string
		wantMask int
	}{
		{ip: "10.0.0.10", wantMask: 32},
		{ip: "2001:db8::10", wantMask: 128},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ones, _ := podIPNet(net.ParseIP(tt.ip)).Mask.Size()
			if ones != tt.wantMask {
				t.Fatalf("mask size = %d, want %d", ones, tt.wantMask)
			}
		})
	}
}
