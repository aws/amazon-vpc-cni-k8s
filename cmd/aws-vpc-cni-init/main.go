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

	"github.com/aws/amazon-vpc-cni-k8s/pkg/procsyswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/utils/cp"
	"github.com/aws/amazon-vpc-cni-k8s/utils/imds"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

const (
	defaultHostCNIBinPath           = "/host/opt/cni/bin"
	vpcCniInitDonePath              = "/vpc-cni-init/done"
	metadataLocalIP                 = "local-ipv4"
	metadataMAC                     = "mac"
	defaultDisableIPv4TcpEarlyDemux = false
	defaultEnableIPv6               = false
	defaultEnableIPv6Egress         = false

	envDisableIPv4TcpEarlyDemux = "DISABLE_TCP_EARLY_DEMUX"
	envEnableIPv6               = "ENABLE_IPv6"
	envHostCniBinPath           = "HOST_CNI_BIN_PATH"
	envEgressV6                 = "ENABLE_V6_EGRESS"
)

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

func configureSystemParams(procSys procsyswrapper.ProcSys, primaryIF string) error {
	var err error
	// Configure rp_filter in loose mode
	entry := "net/ipv4/conf/" + primaryIF + "/rp_filter"
	err = procSys.Set(entry, "2")
	if err != nil {
		return errors.Wrapf(err, "Failed to set rp_filter for %s", primaryIF)
	}
	val, _ := procSys.Get(entry)
	log.Infof("Updated %s to %s", entry, val)

	// Enable or disable TCP early demux based on environment variable
	// Note that older kernels may not support tcp_early_demux, so we must first check that it exists.
	entry = "net/ipv4/tcp_early_demux"
	if _, err := procSys.Get(entry); err == nil {
		disableIPv4EarlyDemux := utils.GetBoolAsStringEnvVar(envDisableIPv4TcpEarlyDemux, defaultDisableIPv4TcpEarlyDemux)
		if disableIPv4EarlyDemux {
			err = procSys.Set(entry, "0")
			if err != nil {
				return errors.Wrap(err, "Failed to disable tcp_early_demux")
			}
		} else {
			err = procSys.Set(entry, "1")
			if err != nil {
				return errors.Wrap(err, "Failed to enable tcp_early_demux")
			}
		}
		val, _ = procSys.Get(entry)
		log.Infof("Updated %s to %s", entry, val)
	}
	return nil
}

func configureIPv6Settings(procSys procsyswrapper.ProcSys, primaryIF string) error {
	var err error
	// Enable IPv6 when environment variable is set
	// Note that IPv6 is not disabled when environment variable is unset. This is omitted to preserve default host semantics.
	enableIPv6 := utils.GetBoolAsStringEnvVar(envEnableIPv6, defaultEnableIPv6)
	if enableIPv6 {
		entry := "net/ipv6/conf/all/disable_ipv6"
		err = procSys.Set(entry, "0")
		if err != nil {
			return errors.Wrap(err, "Failed to set disable_ipv6 to 0")
		}
		val, _ := procSys.Get(entry)
		log.Infof("Updated %s to %s", entry, val)
	}
	// Check if IPv6 egress support is enabled in IPv4 cluster.
	ipv6EgressEnabled := utils.GetBoolAsStringEnvVar(envEgressV6, defaultEnableIPv6Egress)
	if enableIPv6 || ipv6EgressEnabled {
		// For IPv6, the following sysctls are set:
		// 1. forwarding defaults to 1
		// 2. accept_ra defaults to 2
		// 3. accept_redirects defaults to 1
		entry := "net/ipv6/conf/all/forwarding"
		err = procSys.Set(entry, "1")
		if err != nil {
			return errors.Wrap(err, "Failed to enable IPv6 forwarding")
		}
		val, _ := procSys.Get(entry)
		log.Infof("Updated %s to %s", entry, val)

		// accept_ra must be set to 2 so that RA routes are installed by the kernel on secondary ENIs
		// For IPv6, this setting must be inherited by the trunk ENI. It must be set here as IPAMD does
		// not have permission to set sysctl values.
		entry = "net/ipv6/conf/default/accept_ra"
		err = procSys.Set(entry, "2")
		if err != nil {
			return errors.Wrap(err, "Failed to set IPv6 accept Router Advertisements to 2")
		}
		val, _ = procSys.Get(entry)
		log.Infof("Updated %s to %s", entry, val)

		entry = "net/ipv6/conf/default/accept_redirects"
		err = procSys.Set(entry, "1")
		if err != nil {
			return errors.Wrap(err, "Failed to enable IPv6 accept redirects")
		}
		val, _ = procSys.Get(entry)
		log.Infof("Updated %s to %s", entry, val)

		// For the primary ENI in IPv6, sysctls are set to:
		// 1. forwarding=1
		// 2. accept_ra=2
		// 3. accept_redirects=1
		entry = "net/ipv6/conf/" + primaryIF + "/accept_ra"
		err = procSys.Set(entry, "2")
		if err != nil {
			return errors.Wrap(err, "Failed to enable IPv6 accept_ra on primary ENI")
		}
		val, _ = procSys.Get(entry)
		log.Infof("Updated %s to %s", entry, val)
	}
	return nil
}

func main() {
	os.Exit(_main())
}

func _main() int {
	log.Debug("Started Initialization")
	var err error

	log.Infof("Copying CNI plugin binaries ...")
	hostCNIBinPath := utils.GetEnv(envHostCniBinPath, defaultHostCNIBinPath)
	excludeBins := map[string]bool{"aws-vpc-cni-init": true}
	// Copy all binaries from workdir to host bin dir except container init binary
	err = cp.InstallBinariesFromDir(".", hostCNIBinPath, excludeBins)
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

	procSys := procsyswrapper.NewProcSys()
	err = configureSystemParams(procSys, primaryIF)
	if err != nil {
		log.WithError(err).Errorf("Failed to configure system parameters")
		return 1
	}

	err = configureIPv6Settings(procSys, primaryIF)
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
