// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package tester

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// TestNetworkingSetupForRegularPod tests networking set by the CNI Plugin for a list of Pod is as
// expected
func TestNetworkingSetupForRegularPod(podNetworkingValidationInput input.PodNetworkingValidationInput) []error {
	ipFamily := netlink.FAMILY_V4
	if podNetworkingValidationInput.IPFamily == "IPv6" {
		ipFamily = netlink.FAMILY_V6
	}
	// Get the list of IP rules
	nl := netlinkwrapper.NewNetLink()
	ruleList, err := nl.RuleList(ipFamily)
	if err != nil {
		log.Fatalf("failed to list ip rules %v", err)
	}

	// Do validation for each Pod and if validation fails instead of failing
	// entire test add errors to a list for all the failing Pods
	var validationErrors []error
	var podIP net.IP

	secondaryRouteTableIndex := make(map[int]bool)

	// For each Pod validate the Pod networking
	for _, pod := range podNetworkingValidationInput.PodList {
		// For each pod categorize the rules into rules for the main route table
		// and for non main route table
		var mainTableRules []netlink.Rule
		var nonMainTableRules []netlink.Rule

		podIP = net.ParseIP(pod.PodIPv4Address)
		if podNetworkingValidationInput.IPFamily == "IPv6" {
			podIP = net.ParseIP(pod.PodIPv6Address)
		}

		log.Printf("testing for Pod name: %s Namespace: %s, IP: %s, IP on secondary ENI: %t",
			pod.PodName, pod.PodNamespace, podIP, pod.IsIPFromSecondaryENI)

		// Get the veth pair for pod in host network namespace
		hostVethName := getHostVethPairName(pod, podNetworkingValidationInput.VethPrefix)
		link, err := netlink.LinkByName(hostVethName)
		if err != nil {
			validationErrors = append(validationErrors,
				fmt.Errorf("failed to find netlink %s: %v", hostVethName, err))
			continue
		}

		// Validate MTU value if it is set to true
		if podNetworkingValidationInput.ValidateMTU {
			if link.Attrs().MTU != podNetworkingValidationInput.MTU {
				validationErrors = append(validationErrors,
					fmt.Errorf("MTU value %v for pod: %s on veth pair: %s failed to match the expected value: %v", link.Attrs().MTU, pod.PodName, hostVethName, podNetworkingValidationInput.MTU))
			} else {
				log.Printf("Found Valid MTU value:%d for pod: %s on veth Pair: %s\n", link.Attrs().MTU, pod.PodName, hostVethName)
			}
		}

		// Verify IP Link for the Pod is UP
		isLinkUp := strings.Contains(link.Attrs().Flags.String(), "up")
		if !isLinkUp {
			validationErrors = append(validationErrors,
				fmt.Errorf("veth pair on host side is not up %s", link.Attrs().Flags.String()))
			continue
		}

		log.Printf("found veth pair %s in host network namespace in up state with index %d",
			hostVethName, link.Attrs().Index)

		// Get the IP Rules to/from the Pod IP and categorize them into main and non-main table rules
		for _, rule := range ruleList {
			isRuleToOrFromPod := isRuleToOrFromIP(rule, podIP)
			if rule.Table == unix.RT_TABLE_MAIN && isRuleToOrFromPod {
				mainTableRules = append(mainTableRules, rule)
			} else if isRuleToOrFromPod {
				nonMainTableRules = append(nonMainTableRules, rule)
			}
		}
		log.Printf("mainTableRules %v, nonMainTableRules %v", mainTableRules, nonMainTableRules)

		// Both Pod with IP from Primary and Secondary ENI will have 1 rule for main route table
		if len(mainTableRules) != 1 {
			validationErrors = append(validationErrors,
				fmt.Errorf("found 0 or more than 1 rule for main route table: %+v",
					mainTableRules))
			continue
		}

		log.Printf("found rule for main route table to %v with priority %s",
			mainTableRules[0].Dst, mainTableRules[0].IifName)

		// Verify main table route for pod IP go through the veth pair when destination is Pod IP
		toContainerRoutes, err := netlink.RouteListFiltered(ipFamily,
			&netlink.Route{
				Dst: mainTableRules[0].Dst,
			}, netlink.RT_FILTER_DST)
		if err != nil {
			fmt.Errorf("failed to find ip rule with destination %s: %v", podIP.String(), err)
		}

		if len(toContainerRoutes) != 1 {
			validationErrors = append(validationErrors,
				fmt.Errorf("found 0 or more than 1 route to the container %s: %v",
					podIP.String(), toContainerRoutes))
			continue
		}

		// Verify that the link index for the route is the same as the veth pair index
		if toContainerRoutes[0].LinkIndex != link.Attrs().Index {
			validationErrors = append(validationErrors,
				fmt.Errorf("the link index for to contianer route %d is different from"+
					" veth index %d", toContainerRoutes[0].LinkIndex, link.Attrs().Index))
			continue
		}

		log.Printf("found route to %v with link index %d",
			toContainerRoutes[0].Dst, toContainerRoutes[0].LinkIndex)

		// Pod with IP from Secondary ENI will have additional rule for destination to each
		// VPC Cidr block
		if pod.IsIPFromSecondaryENI {
			if len(nonMainTableRules) != 1 {
				validationErrors = append(validationErrors,
					fmt.Errorf("incorrect number of ip rules to the secondary route tables: %+v",
						nonMainTableRules))
			} else {
				secondaryRouteTableIndex[nonMainTableRules[0].Table] = true
			}
		}
		log.Printf("validation for pod %s/%s succeeded", pod.PodNamespace, pod.PodName)
	}

	// Finally validate that the route table for secondary ENI has the right routes
	for index := range secondaryRouteTableIndex {
		routes, err := netlink.RouteListFiltered(ipFamily,
			&netlink.Route{
				Table: index,
			}, netlink.RT_FILTER_TABLE)
		if err != nil {
			validationErrors = append(validationErrors,
				fmt.Errorf("failed to find route for table with index %d:%v", index, err))
			continue
		}

		// Route 1 should route all traffic through Gateway via Secondary ENI
		gateway := routes[0].Gw
		secondaryENIIndex := routes[0].LinkIndex

		// Route 2 should route all traffic intended for Gateway IP through Secondary ENI
		if !routes[1].Dst.IP.Equal(gateway) ||
			routes[1].LinkIndex != secondaryENIIndex {
			validationErrors = append(validationErrors,
				fmt.Errorf("found invalid route for secondary ENI %v", err))
			continue
		}
		log.Printf("validated route table for secondary ENI %d has right routes", index)
	}
	// TODO: validate iptables rules get setup correctly

	return validationErrors
}

// TestNetworkingSetupForPods using security groups
func TestNetworkingSetupForPodsUsingSecurityGroup(podNetworkingValidationInput input.PodNetworkingValidationInput) []error {
	var validationErrors []error
	var podIP net.IP
	interfaceToVlanTableMap := make(map[string]int)
	vlanTableToBranchENIMap := make(map[int]string)

	ipFamily := netlink.FAMILY_V4
	if podNetworkingValidationInput.IPFamily == "IPv6" {
		ipFamily = netlink.FAMILY_V6
	}

	// Get the list of IP rules
	nl := netlinkwrapper.NewNetLink()
	ruleList, err := nl.RuleList(ipFamily)
	if err != nil {
		log.Fatalf("failed to list ip rules %v", err)
	}

	for _, rule := range ruleList {
		if strings.HasPrefix(rule.IifName, "vlan.eth") && rule.Table != 0 {
			vlanTableToBranchENIMap[rule.Table] = rule.IifName
		} else if rule.IifName != "" && rule.Table != 0 {
			interfaceToVlanTableMap[rule.IifName] = rule.Table
		}
	}

	for _, pod := range podNetworkingValidationInput.PodList {
		podIP = net.ParseIP(pod.PodIPv4Address)
		if podNetworkingValidationInput.IPFamily == "IPv6" {
			podIP = net.ParseIP(pod.PodIPv6Address)
		}

		log.Printf("testing for Pod name: %s Namespace: %s, IP: %s",
			pod.PodName, pod.PodNamespace, podIP)

		// Get the veth pair for pod in host network namespace
		hostVethName := getHostVethPairName(pod, podNetworkingValidationInput.VethPrefix)
		link, err := netlink.LinkByName(hostVethName)
		if err != nil {
			validationErrors = append(validationErrors,
				fmt.Errorf("failed to find netlink %s: %v", hostVethName, err))
			continue
		}

		vlanTable := 0
		if table, ok := interfaceToVlanTableMap[hostVethName]; !ok {
			validationErrors = append(validationErrors,
				fmt.Errorf("Missing Rule for pod: %s, podNamespace: %s, veth: %s", pod.PodName, pod.PodNamespace, hostVethName))
		} else {
			vlanTable = table
		}

		// Check if branch ENI exists for given pod
		if branchENI, ok := vlanTableToBranchENIMap[vlanTable]; !ok {
			validationErrors = append(validationErrors,
				fmt.Errorf("Missing Branch ENI for pod: %s, podNamespace: %s", pod.PodName, pod.PodNamespace))
		} else {
			// Check if branchENI is in UP state
			eniLink, err := netlink.LinkByName(branchENI)
			if err != nil {
				validationErrors = append(validationErrors,
					fmt.Errorf("failed to find netlink %s: %v", branchENI, err))
			} else {
				isENILinkUp := strings.Contains(eniLink.Attrs().Flags.String(), "up")
				if !isENILinkUp {
					validationErrors = append(validationErrors,
						fmt.Errorf("branch eni %s is not up %s", branchENI, link.Attrs().Flags.String()))
				} else {
					log.Printf("Found Branch ENI: %s for Pod: %s Namespace: %s, IP: %s in UP state", branchENI,
						pod.PodName, pod.PodNamespace, podIP)
				}
			}
		}

		// Validate MTU value if it is set to true
		if podNetworkingValidationInput.ValidateMTU {
			if link.Attrs().MTU != podNetworkingValidationInput.MTU {
				validationErrors = append(validationErrors,
					fmt.Errorf("MTU value %v for pod: %s on veth pair: %s failed to match the expected value: %v", link.Attrs().MTU, pod.PodName, hostVethName, podNetworkingValidationInput.MTU))
			} else {
				log.Printf("Found Valid MTU value:%d for pod: %s on veth Pair: %s\n", link.Attrs().MTU, pod.PodName, hostVethName)
			}
		}

		// Verify IP Link for the Pod is UP
		isLinkUp := strings.Contains(link.Attrs().Flags.String(), "up")
		if !isLinkUp {
			validationErrors = append(validationErrors,
				fmt.Errorf("veth pair on host side is not up %s", link.Attrs().Flags.String()))
			continue
		}

		log.Printf("found veth pair %s in host network namespace in up state with index %d",
			hostVethName, link.Attrs().Index)

		routes, err := netlink.RouteListFiltered(ipFamily, &netlink.Route{
			Table: vlanTable,
		}, netlink.RT_FILTER_TABLE)

		if err != nil {
			validationErrors = append(validationErrors,
				fmt.Errorf("Failed to list routes for table %d", vlanTable))
		}

		if len(routes) != 3 {
			validationErrors = append(validationErrors,
				fmt.Errorf("Some Routes missing for vlanTable: %d", vlanTable))
		} else {
			log.Printf("Found correct number(3) of routes in vlanTable: %d for podIP: %s", vlanTable, podIP)
		}

		routeExistsForPodIP := false
		for _, route := range routes {
			if route.Dst != nil && route.Dst.IP.String() == podIP.String() {
				routeExistsForPodIP = true
				break
			}
		}

		if !routeExistsForPodIP {
			validationErrors = append(validationErrors,
				fmt.Errorf("Missing Route for PodIP: %s in Table: %d", podIP, vlanTable))
		}
	}
	return validationErrors
}

// TestNetworkTearedDownForRegularPods test pod networking is correctly teared down by the CNI Plugin
// The test assumes that the IP assigned to the older Pod is not assigned to a new Pod while this test
// is being executed
func TestNetworkTearedDownForRegularPods(podNetworkingValidationInput input.PodNetworkingValidationInput) []error {
	ipFamily := netlink.FAMILY_V4
	maskLen := "32"
	if podNetworkingValidationInput.IPFamily == "IPv6" {
		ipFamily = netlink.FAMILY_V6
		maskLen = "128"
	}
	// Get the list of IP rules
	nl := netlinkwrapper.NewNetLink()
	ruleList, err := nl.RuleList(ipFamily)
	if err != nil {
		log.Fatalf("failed to list ip rules %v", err)
	}

	var validationError []error
	var podIP string

	for _, pod := range podNetworkingValidationInput.PodList {
		podIP = pod.PodIPv4Address
		if podNetworkingValidationInput.IPFamily == "IPv6" {
			podIP = pod.PodIPv6Address
		}
		podIP, podIPNet, err := net.ParseCIDR(podIP + "/" + maskLen)
		if err != nil {
			validationError = append(validationError,
				fmt.Errorf("failed to parse pod IP %s", pod.PodIPv4Address))
			continue
		}

		log.Printf("testing for Pod name: %s Namespace: %s, IP: %s, IP on secondary ENI: %t",
			pod.PodName, pod.PodNamespace, pod.PodIPv4Address, pod.IsIPFromSecondaryENI)

		// Make sure the veth pair doesn't exist anymore
		hostVethName := getHostVethPairName(pod, podNetworkingValidationInput.VethPrefix)
		link, err := netlink.LinkByName(hostVethName)
		if err == nil {
			validationError = append(validationError,
				fmt.Errorf("found an existing veth pair for the pod %s: %v", pod.PodName, link))
			continue
		}
		log.Printf("veth pair %s not found for the pod: %v", hostVethName, err)

		// Make sure there's no more rules either to or from the Pod's IPv4 Address
		var ruleFound bool
		for _, rule := range ruleList {
			if isRuleToOrFromIP(rule, podIP) {
				validationError = append(validationError,
					fmt.Errorf("found one ip rule to/from the pod IP %s: %v", pod.PodIPv4Address, rule))
				ruleFound = true
				break
			}
		}

		// Test the next pod if even a single leaked rule if found
		if ruleFound {
			continue
		}

		log.Printf("found no rules for the pod's IP %s", pod.PodIPv4Address)

		// Make sure there's no route to Pod IP Address
		toContainerRoutes, err := netlink.RouteListFiltered(ipFamily,
			&netlink.Route{
				Dst: podIPNet,
			}, netlink.RT_FILTER_DST)
		if err != nil {
			validationError = append(validationError,
				fmt.Errorf("failed to find routes to pod %s: %v", pod.PodName, err))
			continue
		}

		if len(toContainerRoutes) != 0 {
			validationError = append(validationError,
				fmt.Errorf("found one or more ip route for pod %s: %v", pod.PodName, toContainerRoutes))
			continue
		}

		log.Printf("no leaked resource found for the pod %s/%s", pod.PodNamespace, pod.PodName)
	}

	return validationError
}

// TestNetworkingForPods using security groups is teared down correctly
func TestNetworkTearedDownForPodsUsingSecurityGroup(podNetworkingValidationInput input.PodNetworkingValidationInput) []error {
	var validationErrors []error
	var podIP net.IP
	interfaceToVlanTableMap := make(map[string]int)
	vlanTableToBranchENIMap := make(map[int]string)

	ipFamily := netlink.FAMILY_V4
	if podNetworkingValidationInput.IPFamily == "IPv6" {
		ipFamily = netlink.FAMILY_V6
	}
	// Get the list of IP rules
	nl := netlinkwrapper.NewNetLink()
	ruleList, err := nl.RuleList(ipFamily)
	if err != nil {
		log.Fatalf("failed to list ip rules %v", err)
	}

	for _, rule := range ruleList {
		if strings.HasPrefix(rule.IifName, "vlan.eth") && rule.Table != 0 {
			vlanTableToBranchENIMap[rule.Table] = rule.IifName
		} else if rule.IifName != "" && rule.Table != 0 {
			interfaceToVlanTableMap[rule.IifName] = rule.Table
		}
	}

	// Check if branchENI's are cleanup
	if len(vlanTableToBranchENIMap) != 0 {
		validationErrors = append(validationErrors,
			fmt.Errorf("found leaked branch ENI"))
		for _, eni := range vlanTableToBranchENIMap {
			log.Printf("Leaked branch ENI: %s", eni)
		}
	}

	for _, pod := range podNetworkingValidationInput.PodList {
		podIP = net.ParseIP(pod.PodIPv4Address)
		if podNetworkingValidationInput.IPFamily == "IPv6" {
			podIP = net.ParseIP(pod.PodIPv6Address)
		}

		log.Printf("testing for Pod name: %s Namespace: %s, IP: %s",
			pod.PodName, pod.PodNamespace, pod.PodIPv4Address)

		// Make sure the veth pair doesn't exist anymore
		hostVethName := getHostVethPairName(pod, podNetworkingValidationInput.VethPrefix)
		link, err := netlink.LinkByName(hostVethName)
		if err == nil {
			// Found leaked veth pair
			validationErrors = append(validationErrors,
				fmt.Errorf("found an existing veth pair for the pod %s: %v", pod.PodName, link))

			// check if vlanTable rule exists for leaked veth pair
			if table, ok := interfaceToVlanTableMap[hostVethName]; ok {
				validationErrors = append(validationErrors,
					fmt.Errorf("Rule exists for pod: %s, podNamespace: %s, veth: %s with target vlanTable:%d", pod.PodName, pod.PodNamespace, hostVethName, table))

				routes, err := netlink.RouteListFiltered(ipFamily, &netlink.Route{
					Table: table,
				}, netlink.RT_FILTER_TABLE)

				if err != nil {
					validationErrors = append(validationErrors,
						fmt.Errorf("Failed to list routes for table %d", table))
				} else {
					if len(routes) != 0 {
						validationErrors = append(validationErrors,
							fmt.Errorf("Some Routes exist in vlanTable: %d for podIP: %s", table, podIP))
					} else {
						log.Printf("No Routes routes found in vlanTable: %d for podIP: %s", table, podIP)
					}
				}
			}
			continue
		}
		log.Printf("veth pair %s not found for the pod: %v", hostVethName, err)

		log.Printf("no leaked resource found for the pod %s/%s", pod.PodNamespace, pod.PodName)
	}

	return validationErrors
}

func isRuleToOrFromIP(rule netlink.Rule, ip net.IP) bool {
	if (rule.Src != nil && rule.Src.IP.Equal(ip)) ||
		(rule.Dst != nil && rule.Dst.IP.Equal(ip)) {
		return true
	}
	return false
}

func getHostVethPairName(input input.Pod, vethPrefix string) string {
	h := sha1.New()
	h.Write([]byte(fmt.Sprintf("%s.%s", input.PodNamespace, input.PodName)))
	return fmt.Sprintf("%s%s", vethPrefix, hex.EncodeToString(h.Sum(nil))[:11])
}
