package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/vishvananda/netlink"
)

const (
	shortDescription = "packet-verifier"
	longDescription  = "Packet verifier fetches corresponding interfaces and validates the packets."
)

var (
	// version string populated during build.
	version = "unknown"

	// ip to monitor on the interfaces
	ipToMonitor string
	receiverIP  string
	device      string

	// vlan ID to monitor on the interfaces
	vlanIDToMonitor int

	// pcap parameters (requires libpcap-devel to be installed on the host)
	snapshot_len int32 = 1024
	promiscuous        = false
	timeout            = 30 * time.Second
	handle       *pcap.Handle

	// list of enis to monitor
	enis []eniConfig
)

// eniConfig details regarding ENIs
type eniConfig struct {
	name                string
	shouldCheckSrc      bool
	shouldVerifyVlanTag bool
}

func main() {
	fmt.Print("Verifying packet flow...\n")

	helpFlag := flag.Bool("help", false, "displays usage information")
	versionFlag := flag.Bool("version", false, "displays version information")
	flag.StringVar(&ipToMonitor, "ip-to-monitor", "", "pod ip to monitor.")
	flag.StringVar(&receiverIP, "receiver-ip", "", "other IP that interacts with the pod.")
	flag.IntVar(&vlanIDToMonitor, "vlanid-to-monitor", 0, "pod vlan id to monitor.")
	flag.StringVar(&device, "host-device", "eth0", "host device of the node.")

	flag.Usage = printUsage

	// Parse command line flags.
	flag.Parse()

	if *helpFlag {
		printUsage()
		os.Exit(0)
	}

	if *versionFlag {
		printVersion()
		os.Exit(0)
	}

	if ipToMonitor == "" {
		fmt.Println("tracking-ip can't be empty")
		os.Exit(1)
	}

	hostName, err := os.Hostname()
	if err != nil {
		fmt.Printf("unable to retrieve the host name due to %+v", err)
		os.Exit(1)
	}

	// if its host ip then just use eth0 and skip rest of the operation
	if hostName == ipToMonitor {
		hostENI := eniConfig{name: device}
		enis = append(enis, hostENI)
	} else {
		// read route tables to find the hostveth
		routeFilter := &netlink.Route{
			Table: vlanIDToMonitor + 100,
		}
		routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, routeFilter, netlink.RT_FILTER_TABLE)
		if err != nil {
			fmt.Printf("unable to get routes for table using vlanID %d. Error: %+v", vlanIDToMonitor, err)
			os.Exit(1)
		}
		for _, route := range routes {
			if route.Dst != nil && route.Dst.IP.Equal(net.ParseIP(ipToMonitor)) {
				linkIndex := route.LinkIndex
				link, err := netlink.LinkByIndex(linkIndex)
				if err != nil {
					fmt.Printf("unable to find index %d error %+v", linkIndex, err)
					os.Exit(1)
				}
				hostVethToMonitor := eniConfig{name: link.Attrs().Name, shouldCheckSrc: true}
				enis = append(enis, hostVethToMonitor)
				break
			}
		}

		// get vlan devices
		if vlanIDToMonitor != 0 {
			link, err := netlink.LinkByName(fmt.Sprintf("vlan.eth.%d", vlanIDToMonitor))
			if err != nil {
				fmt.Printf("unable to get vlan device, error: %+v", err)
				os.Exit(1)
			}
			vlanDevToMonitor := eniConfig{name: link.Attrs().Name, shouldCheckSrc: true}
			enis = append(enis, vlanDevToMonitor)

			// find the trunk dev
			parentLink, err := netlink.LinkByIndex(link.Attrs().ParentIndex)
			if err != nil {
				fmt.Printf("unable to get parent link %d. Error %+v", link.Attrs().ParentIndex, err)
				os.Exit(1)
			}
			trunkDevToMonitor := eniConfig{name: parentLink.Attrs().Name, shouldVerifyVlanTag: true}
			enis = append(enis, trunkDevToMonitor)
		} else {
			// find the eni to monitor associated with the pod
			rules, err := netlink.RuleList(netlink.FAMILY_V4)
			if err != nil {
				fmt.Printf("unable to get ip rules due to %+v", err)
				os.Exit(1)
			}
			for _, rule := range rules {
				// Find the ENI in the route table associated with the pod
				if rule.Src != nil && net.ParseIP(ipToMonitor).Equal(rule.Src.IP) {
					routeFilter := &netlink.Route{
						Table: rule.Table,
					}
					routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, routeFilter, netlink.RT_FILTER_TABLE)
					if err != nil {
						fmt.Printf("unable to get routes based on iprules %+v", err)
						os.Exit(1)
					}

					for _, route := range routes {
						parentLink, err := netlink.LinkByIndex(route.LinkIndex)
						if err != nil {
							fmt.Printf("unable to get parent ENI for the link %+v", err)
							os.Exit(1)
						}
						eniToMonitor := eniConfig{name: parentLink.Attrs().Name}
						enis = append(enis, eniToMonitor)
						break
					}
				}
			}
			// if explicit route is not round then pod has to be associated with $device
			if len(enis) == 0 {
				eniToMonitor := eniConfig{name: device}
				enis = append(enis, eniToMonitor)
			}
		}
	}

	fmt.Printf("ENIs to monitor: %+v\n", enis)

	err = monitorPacketOnInterfaces(ipToMonitor, vlanIDToMonitor, enis)
	if err != nil {
		fmt.Printf("unable to verify packets on the interface %+v ", err)
		os.Exit(1)
	}

	fmt.Println("Successfully verified all the interfaces.")
}

// monitorPacketOnInterfaces invokes monitorPackets for each interface
func monitorPacketOnInterfaces(ipAddress string, vlanIDToMonitor int, enis []eniConfig) error {

	for _, iface := range enis {
		fmt.Printf("Verifying interface: %+v\n", iface)
		err := monitorPackets(ipAddress, vlanIDToMonitor, iface)
		if err != nil {
			return err
		}
	}

	return nil
}

// monitorPackets monitors the packets on the interfaces
func monitorPackets(ipAddress string, vlanIDToMonitor int, iface eniConfig) error {
	handle, err := pcap.OpenLive(iface.name, snapshot_len, promiscuous, timeout)
	if err != nil {
		return err
	}
	defer handle.Close()
	var srcPacketsProcessed int
	var dstPacketsProcessed int

	// Use the handle as a packet source to process all packets
	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	for packet := range packetSource.Packets() {
		fmt.Printf("packet: %v\n", packet)

		network := packet.Layer(layers.LayerTypeIPv4)
		if network != nil {
			if iface.shouldCheckSrc && packet.NetworkLayer().NetworkFlow().Src().String() != ipAddress &&
				packet.NetworkLayer().NetworkFlow().Dst().String() != ipAddress {
				fmt.Printf("Src/Dst is different. Src %s Dst %s\n", packet.NetworkLayer().NetworkFlow().Src(),
					packet.NetworkLayer().NetworkFlow().Dst())
				return errors.New("SRC/Dst is different")
			}

			// Verify vlan tag (on ENIs we could see other IP pkts as well)
			if packet.NetworkLayer().NetworkFlow().Src().String() == ipAddress ||
				packet.NetworkLayer().NetworkFlow().Dst().String() == ipAddress {
				if iface.shouldVerifyVlanTag {

					dot1QPkt := packet.Layer(layers.LayerTypeDot1Q)
					if dot1QPkt == nil {
						fmt.Printf("vlan packet not found when expected.\n")
						return errors.New("vlan packet not found when expected")
					}

					dot1Q, _ := dot1QPkt.(*layers.Dot1Q)
					if dot1Q.VLANIdentifier != uint16(vlanIDToMonitor) {
						fmt.Printf("VlanIDs are different. expected %d but found %d\n", vlanIDToMonitor, dot1Q.VLANIdentifier)
						return errors.New("vlanIDs are different")
					}
				}

				/*icmpPkt := packet.Layer(layers.LayerTypeICMPv4)
				if icmpPkt != nil {
					icmpData, _ := icmpPkt.(*layers.ICMPv4)
					log.Infof("Icmp packet sequence: %d", icmpData.Seq)
				}*/

				if packet.NetworkLayer().NetworkFlow().Src().String() == ipAddress {
					fmt.Printf("Source pkt is verified on the iface %s\n", iface.name)
					srcPacketsProcessed++
				} else {
					fmt.Printf("Dst pkt is verified on the iface %s\n", iface.name)
					dstPacketsProcessed++
				}

				if srcPacketsProcessed >= 5 && dstPacketsProcessed >= 5 {
					break
				}
			}
		}
	}
	return nil
}

// printVersion prints the binary version to stderr.
func printVersion() {
	fmt.Fprintf(os.Stderr, "%s v%s\n", shortDescription, version)
}

// printUsage prints usage information to stderr.
func printUsage() {
	printVersion()
	fmt.Fprintln(os.Stderr, longDescription+"\n")
	flag.PrintDefaults()
}
