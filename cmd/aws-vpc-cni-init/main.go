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

// The aws-node initialization
package main

import (
	"os"

	"github.com/aws/amazon-vpc-cni-k8s/utils/cp"
	"github.com/aws/amazon-vpc-cni-k8s/utils/imds"
	"github.com/aws/amazon-vpc-cni-k8s/utils/sysctl"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

const (
	defaultHostCNIBinPath = "/host/opt/cni/bin"
	metadataLocalIP       = "local-ipv4"
	metadataMAC           = "mac"

	envDisableIPv4TcpEarlyDemux = "DISABLE_TCP_EARLY_DEMUX"
	envEnableIPv6               = "ENABLE_IPv6"
	envHostCniBinPath           = "HOST_CNI_BIN_PATH"
)

func getEnv(env, def string) string {
	if val, ok := os.LookupEnv(env); ok {
		return val
	}
	return def
}

func main() {
	os.Exit(_main())
}

func _main() int {
	log.Debug("Started Initialization")
	pluginBins := []string{"loopback", "portmap", "bandwidth", "aws-cni-support.sh"}
	var err error
	for _, plugin := range pluginBins {
		if _, err = os.Stat(plugin); err != nil {
			log.WithError(err).Fatalf("Required executable : %s not found\n", plugin)
			return 1
		}
	}

	hostCNIBinPath := getEnv(envHostCniBinPath, defaultHostCNIBinPath)

	log.Infof("Copying CNI plugin binaries ...")
	err = cp.InstallBinaries(pluginBins, hostCNIBinPath)
	if err != nil {
		log.WithError(err).Errorf("Failed to install binaries")
		return 1
	}

	log.Infof("Copied all CNI plugin binaries to %s\n", hostCNIBinPath)

	var hostIP string
	hostIP, err = imds.GetMetaData("local-ipv4")
	if err != nil {
		log.WithError(err).Fatalf("aws-vpc-cni init failed\n")
		return 1
	}

	var primaryMAC string
	primaryMAC, err = imds.GetMetaData("mac")
	if err != nil {
		log.WithError(err).Fatalf("aws-vpc-cni init failed\n")
		return 1
	}

	log.Infof("Found hostIP %s and primaryMAC %s", hostIP, primaryMAC)

	links, err := netlink.LinkList()
	if err != nil {
		log.WithError(err).Fatalf("Failed to list links\n")
		return 1
	}

	var primaryIF string
	for _, link := range links {
		if link.Attrs().HardwareAddr.String() == primaryMAC {
			primaryIF = link.Attrs().Name
			break
		}
	}

	if primaryIF == "" {
		log.Errorf("Failed to retrieve primary IF")
		return 1
	}

	log.Infof("Found primaryIF %s", primaryIF)
	sys := sysctl.New()
	entry := "net/ipv4/conf/" + primaryIF + "/rp_filter"
	err = sys.SetSysctl(entry, 2)
	if err != nil {
		log.WithError(err).Fatalf("Failed to set rp_filter for %s\n", primaryIF)
		return 1
	}

	val, _ := sys.GetSysctl(entry)
	log.Infof("Updated entry for %d", val)

	disableIPv4EarlyDemux := getEnv(envDisableIPv4TcpEarlyDemux, "false")
	entry = "net/ipv4/tcp_early_demux"
	if disableIPv4EarlyDemux == "true" {
		err = sys.SetSysctl(entry, 0)
		if err != nil {
			log.WithError(err).Fatalf("Failed disable tcp_early_demux\n")
			return 1
		}
	} else {
		err = sys.SetSysctl(entry, 1)
		if err != nil {
			log.WithError(err).Fatalf("Failed enable tcp_early_demux\n")
			return 1
		}
	}

	val, _ = sys.GetSysctl(entry)
	log.Infof("Updated entry for %d", val)

	enableIPv6 := getEnv(envEnableIPv6, "false")
	if enableIPv6 == "true" {
		entry = "net/ipv6/conf/all/disable_ipv6"
		err = sys.SetSysctl(entry, 0)
		if err != nil {
			log.WithError(err).Fatalf("Failed enable tcp_early_demux\n")
			return 1
		}
		val, _ = sys.GetSysctl(entry)
		log.Infof("Updated entry for %d", val)

		entry = "net/ipv6/conf/all/forwarding"
		err = sys.SetSysctl(entry, 1)
		if err != nil {
			log.WithError(err).Fatalf("Failed enable tcp_early_demux\n")
			return 1
		}
		val, _ = sys.GetSysctl(entry)
		log.Infof("Updated entry for %d", val)

		entry = "net/ipv6/conf/" + primaryIF + "/accept_ra"
		err = sys.SetSysctl(entry, 2)
		if err != nil {
			log.WithError(err).Fatalf("Failed enable tcp_early_demux\n")
			return 1
		}
		val, _ = sys.GetSysctl(entry)
		log.Infof("Updated entry for %d", val)
	}

	log.Infof("CNI init container done")

	return 0
}
