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
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

const (
	defaultHostCNIBinPath = "/host/opt/cni/bin"
	vpcCniInitDonePath    = "/vpc-cni-init/done"
	metadataLocalIP       = "local-ipv4"
	metadataMAC           = "mac"

	envDisableIPv4TcpEarlyDemux = "DISABLE_TCP_EARLY_DEMUX"
	envEnableIPv6               = "ENABLE_IPv6"
	envHostCniBinPath           = "HOST_CNI_BIN_PATH"
)

func getEnv(env, defaultVal string) string {
	if val, ok := os.LookupEnv(env); ok {
		return val
	}
	return defaultVal
}

func getNodePrimaryIF() (string, error) {
	var primaryIF string
	primaryMAC, err := imds.GetMetaData("mac")
	if err != nil {
		return primaryIF, errors.Wrap(err, "Failed to get primary MAC from IMDS")
	}
	log.Infof("Found primaryMAC %s", primaryMAC)

	links, err := netlink.LinkList()
	if err != nil {
		return primaryIF, errors.Wrap(err, "Failed to list links")
	}
	for _, link := range links {
		if link.Attrs().HardwareAddr.String() == primaryMAC {
			primaryIF = link.Attrs().Name
			break
		}
	}

	if primaryIF == "" {
		return primaryIF, errors.Wrap(err, "Failed to retrieve primary IF")
	}
	return primaryIF, nil
}

func configureSystemParams(sys sysctl.Interface, primaryIF string) error {
	var err error
	// Configure rp_filter in loose mode
	entry := "net/ipv4/conf/" + primaryIF + "/rp_filter"
	err = sys.SetSysctl(entry, 2)
	if err != nil {
		return errors.Wrapf(err, "Failed to set rp_filter for %s", primaryIF)
	}
	val, _ := sys.GetSysctl(entry)
	log.Infof("Updated %s to %d", entry, val)

	// Enable or disable TCP early demux based on environment variable
	// Note that older kernels may not support tcp_early_demux, so we must first check that it exists.
	entry = "net/ipv4/tcp_early_demux"
	if _, err := sys.GetSysctl(entry); err != nil {
		disableIPv4EarlyDemux := getEnv(envDisableIPv4TcpEarlyDemux, "false")
		if disableIPv4EarlyDemux == "true" {
			err = sys.SetSysctl(entry, 0)
			if err != nil {
				return errors.Wrap(err, "Failed to disable tcp_early_demux")
			}
		} else {
			err = sys.SetSysctl(entry, 1)
			if err != nil {
				return errors.Wrap(err, "Failed to enable tcp_early_demux")
			}
		}
		val, _ = sys.GetSysctl(entry)
		log.Infof("Updated %s to %d", entry, val)
	}
	return nil
}

func configureIPv6Settings(sys sysctl.Interface, primaryIF string) error {
	var err error
	// Enable IPv6 when environment variable is set
	// Note that IPv6 is not disabled when environment variable is unset. This is omitted to preserve default host semantics.
	enableIPv6 := getEnv(envEnableIPv6, "false")
	if enableIPv6 == "true" {
		entry := "net/ipv6/conf/all/disable_ipv6"
		err = sys.SetSysctl(entry, 0)
		if err != nil {
			return errors.Wrap(err, "Failed to set disable_ipv6 to 0")
		}
		val, _ := sys.GetSysctl(entry)
		log.Infof("Updated %s to %d", entry, val)

		entry = "net/ipv6/conf/all/forwarding"
		err = sys.SetSysctl(entry, 1)
		if err != nil {
			return errors.Wrap(err, "Failed to enable ipv6 forwarding")
		}
		val, _ = sys.GetSysctl(entry)
		log.Infof("Updated %s to %d", entry, val)

		entry = "net/ipv6/conf/" + primaryIF + "/accept_ra"
		err = sys.SetSysctl(entry, 2)
		if err != nil {
			return errors.Wrap(err, "Failed to enable ipv6 accept_ra")
		}
		val, _ = sys.GetSysctl(entry)
		log.Infof("Updated %s to %d", entry, val)
	}
	return nil
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
			log.WithError(err).Fatalf("Required executable: %s not found", plugin)
			return 1
		}
	}

	log.Infof("Copying CNI plugin binaries ...")
	hostCNIBinPath := getEnv(envHostCniBinPath, defaultHostCNIBinPath)
	err = cp.InstallBinaries(pluginBins, hostCNIBinPath)
	if err != nil {
		log.WithError(err).Errorf("Failed to install binaries")
		return 1
	}
	log.Infof("Copied all CNI plugin binaries to %s", hostCNIBinPath)

	var primaryIF string
	primaryIF, err = getNodePrimaryIF()
	if err != nil {
		log.WithError(err).Errorf("Failed to get primary IF")
		return 1
	}
	log.Infof("Found primaryIF %s", primaryIF)

	sys := sysctl.New()
	err = configureSystemParams(sys, primaryIF)
	if err != nil {
		log.WithError(err).Errorf("Failed to configure system parameters")
		return 1
	}

	err = configureIPv6Settings(sys, primaryIF)
	if err != nil {
		log.WithError(err).Errorf("Failed to configure IPv6 settings")
		return 1
	}

	// TODO: In order to speed up pod launch time, VPC CNI init container is not a Kubernetes init container.
	// The VPC CNI container blocks on the existence of vpcCniInitDonePath
	//err = cp.TouchFile(vpcCniInitDonePath)
	//if err != nil {
	//	log.WithError(err).Errorf("Failed to set VPC CNI init done")
	//	return 1
	//}

	log.Infof("CNI init container done")

	// TODO: Since VPC CNI init container is a real container, it never exits
	// time.Sleep(time.Duration(1<<63 - 1))
	return 0
}
