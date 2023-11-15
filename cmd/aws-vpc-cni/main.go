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

// NOTE(jaypipes): Normally, we would prefer *not* to have an entrypoint script
// and instead just start the agent daemon as the container's CMD. However, the
// design of CNI is such that Kubelet looks for the presence of binaries and CNI
// configuration files in specific directories, and the presence of those files
// is the trigger to Kubelet that that particular CNI plugin is "ready".
//
// In the case of the AWS VPC CNI plugin, we have two components to the plugin.
// The first component is the actual CNI binary that is execve'd from Kubelet
// when a container is started or destroyed. The second component is the
// aws-k8s-agent daemon which houses the IPAM controller.
//
// As mentioned above, Kubelet considers a CNI plugin "ready" when it sees the
// binary and configuration file for the plugin in a well-known directory. For
// the AWS VPC CNI plugin binary, we only want to copy the CNI plugin binary
// into that well-known directory AFTER we have successfully started the IPAM
// daemon and know that it can connect to Kubernetes and the local EC2 metadata
// service. This is why this entrypoint script exists; we start the IPAM daemon
// and wait until we know it is up and running successfully before copying the
// CNI plugin binary and its configuration file to the well-known directory that
// Kubelet looks in.

// AWS VPC CNI entrypoint binary
package main

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/containernetworking/cni/pkg/types"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/cniutils"
	"github.com/aws/amazon-vpc-cni-k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/utils/cp"
)

const (
	egressPluginIpamSubnetV4     = "169.254.172.0/22"
	egressPluginIpamSubnetV6     = "fd00::ac:00/118"
	egressPluginIpamDstV4        = "0.0.0.0/0"
	egressPluginIpamDstV6        = "::/0"
	egressPluginIpamDataDirV4    = "/run/cni/v6pd/egress-v4-ipam"
	egressPluginIpamDataDirV6    = "/run/cni/v4pd/egress-v6-ipam"
	defaultHostCniBinPath        = "/host/opt/cni/bin"
	defaultHostCniConfDirPath    = "/host/etc/cni/net.d"
	defaultAWSconflistFile       = "/app/10-aws.conflist"
	tmpAWSconflistFile           = "/tmp/10-aws.conflist"
	defaultVethPrefix            = "eni"
	defaultMTU                   = "9001"
	defaultEnablePodEni          = false
	defaultPodSGEnforcingMode    = "strict"
	defaultPluginLogFile         = "/var/log/aws-routed-eni/plugin.log"
	defaultEgressV4PluginLogFile = "/var/log/aws-routed-eni/egress-v4-plugin.log"
	defaultEgressV6PluginLogFile = "/var/log/aws-routed-eni/egress-v6-plugin.log"
	defaultPluginLogLevel        = "Debug"
	defaultEnableIPv6            = false
	defaultEnableIPv6Egress      = false
	defaultEnableIPv4Egress      = true
	defaultRandomizeSNAT         = "prng"
	awsConflistFile              = "/10-aws.conflist"
	vpcCniInitDonePath           = "/vpc-cni-init/done"
	defaultEnBandwidthPlugin     = false
	defaultEnPrefixDelegation    = false
	defaultIPCooldownPeriod      = 30
	defaultDisablePodV6          = false
	defaultWarmIPTarget          = 0
	defaultMinIPTarget           = 0
	defaultWarmPrefixTarget      = 0

	envHostCniBinPath        = "HOST_CNI_BIN_PATH"
	envHostCniConfDirPath    = "HOST_CNI_CONFDIR_PATH"
	envVethPrefix            = "AWS_VPC_K8S_CNI_VETHPREFIX"
	envEniMTU                = "AWS_VPC_ENI_MTU"
	envEnablePodEni          = "ENABLE_POD_ENI"
	envPodSGEnforcingMode    = "POD_SECURITY_GROUP_ENFORCING_MODE"
	envPluginLogFile         = "AWS_VPC_K8S_PLUGIN_LOG_FILE"
	envPluginLogLevel        = "AWS_VPC_K8S_PLUGIN_LOG_LEVEL"
	envEgressV4PluginLogFile = "AWS_VPC_K8S_EGRESS_V4_PLUGIN_LOG_FILE"
	envEgressV6PluginLogFile = "AWS_VPC_K8S_EGRESS_V6_PLUGIN_LOG_FILE"
	envEnPrefixDelegation    = "ENABLE_PREFIX_DELEGATION"
	envWarmIPTarget          = "WARM_IP_TARGET"
	envMinIPTarget           = "MINIMUM_IP_TARGET"
	envWarmPrefixTarget      = "WARM_PREFIX_TARGET"
	envEnBandwidthPlugin     = "ENABLE_BANDWIDTH_PLUGIN"
	envEnIPv6                = "ENABLE_IPv6"
	envEnIPv6Egress          = "ENABLE_V6_EGRESS"
	envEnIPv4Egress          = "ENABLE_V4_EGRESS"
	envRandomizeSNAT         = "AWS_VPC_K8S_CNI_RANDOMIZESNAT"
	envIPCooldownPeriod      = "IP_COOLDOWN_PERIOD"
	envDisablePodV6          = "DISABLE_POD_V6"
)

// NetConfList describes an ordered list of networks.
type NetConfList struct {
	CNIVersion string `json:"cniVersion,omitempty"`

	Name         string     `json:"name,omitempty"`
	DisableCheck bool       `json:"disableCheck,omitempty"`
	Plugins      []*NetConf `json:"plugins,omitempty"`
}

// NetConf stores the common network config for the CNI plugin
type NetConf struct {
	CNIVersion string `json:"cniVersion,omitempty"`

	Name         string            `json:"name,omitempty"`
	Type         string            `json:"type,omitempty"`
	Capabilities map[string]bool   `json:"capabilities,omitempty"`
	IPAM         *IPAMConfig       `json:"ipam,omitempty"`
	DNS          *types.DNS        `json:"dns,omitempty"`
	Sysctl       map[string]string `json:"sysctl,omitempty"`

	RawPrevResult map[string]interface{} `json:"prevResult,omitempty"`
	PrevResult    types.Result           `json:"-"`

	// Interface inside container to create
	IfName string `json:"ifName,omitempty"`

	Enabled string `json:"enabled,,omitempty"`

	// IP to use as SNAT target
	NodeIP net.IP `json:"nodeIP,omitempty"`

	VethPrefix string `json:"vethPrefix,omitempty"`

	PodSGEnforcingMode string `json:"podSGEnforcingMode,omitempty"`

	RandomizeSNAT string `json:"randomizeSNAT,omitempty"`

	// MTU for eth0
	MTU string `json:"mtu,omitempty"`

	PluginLogFile string `json:"pluginLogFile,omitempty"`

	PluginLogLevel string `json:"pluginLogLevel,omitempty"`
}

// IPAMConfig references containernetworking structure defined at https://github.com/containernetworking/plugins/blob/main/plugins/ipam/host-local/backend/allocator/config.go
type IPAMConfig struct {
	*Range
	Name       string         `json:"name,omitempty"`
	Type       string         `json:"type,omitempty"`
	Routes     []*types.Route `json:"routes,omitempty"`
	DataDir    string         `json:"dataDir,omitempty"`
	ResolvConf string         `json:"resolvConf,omitempty"`
	Ranges     []RangeSet     `json:"ranges"`
	IPArgs     []net.IP       `json:"-"` // Requested IPs from CNI_ARGS and args
}

// RangeSet references containernetworking structure
type RangeSet []Range

// Range references containernetworking structure
type Range struct {
	RangeStart net.IP      `json:"rangeStart,omitempty"` // The first ip, inclusive
	RangeEnd   net.IP      `json:"rangeEnd,omitempty"`   // The last ip, inclusive
	Subnet     types.IPNet `json:"subnet"`
	Gateway    net.IP      `json:"gateway,omitempty"`
}

// Wait for IPAMD health check to pass. Note that if IPAMD fails to start, wait happens indefinitely until liveness probe kills pod
func waitForIPAM() bool {
	for {
		cmd := exec.Command("./grpc-health-probe", "-addr", "127.0.0.1:50051", ">", "/dev/null", "2>&1")
		if err := cmd.Run(); err == nil {
			return true
		}
	}
}

// Wait for vpcCniInitDonePath to exist (maximum wait time is 60 seconds)
func waitForInit() error {
	start := time.Now()
	maxEnd := start.Add(time.Minute)
	for {
		// Check for existence of vpcCniInitDonePath
		if _, err := os.Stat(vpcCniInitDonePath); err == nil {
			// Delete the done file in case of a reboot of the node or restart of the container (force init container to run again)
			if err := os.Remove(vpcCniInitDonePath); err == nil {
				return nil
			}
			// If file deletion fails, log and allow retry
			log.Errorf("Failed to delete file: %s", vpcCniInitDonePath)
		}
		if time.Now().After(maxEnd) {
			return errors.Errorf("time exceeded")
		}
		time.Sleep(1 * time.Second)
	}
}

func getPrimaryIP(ipv4 bool) (string, error) {
	var hostIP string
	var err error
	imdsKey := "local-ipv4"
	if !ipv4 {
		imdsKey = "ipv6"
	}

	hostIP, err = cniutils.GetNodeMetadata(imdsKey)
	if err != nil {
		if ipv4 {
			log.WithError(err).Fatalf("failed to retrieve local-ipv4 address in imds metadata")
		} else {
			log.WithError(err).Debugf("failed to retrieve ipv6 address in imds metadata")
		}
		return "", err
	}
	return hostIP, nil
}

func isValidJSON(inFile string) error {
	var result map[string]interface{}
	return json.Unmarshal([]byte(inFile), &result)
}
func generateJSON(jsonFile string, outFile string, getPrimaryIP func(ipv4 bool) (string, error)) error {
	byteValue, err := os.ReadFile(jsonFile)
	if err != nil {
		return err
	}

	// enabledIPv6 is to determine if EKS cluster is IPv4 or IPv6 cluster
	// if this EKS cluster is IPv6 cluster, egress-cni-plugin will enable IPv4 egress by default
	// if this EKS cluster is IPv4 cluster, egress-cni-plugin will only enable IPv6 egress if env var "ENABLE_V6_EGRESS" is "true"
	enabledIPv6 := utils.GetBoolAsStringEnvVar(envEnIPv6, defaultEnableIPv6)
	var egressIPAMSubnet string
	var egressIPAMDst string
	var egressIPAMDataDir string
	var egressEnabled bool
	var egressPluginLogFile string
	var nodeIP = ""
	if enabledIPv6 {
		// EKS IPv6 cluster
		egressIPAMSubnet = egressPluginIpamSubnetV4
		egressIPAMDst = egressPluginIpamDstV4
		egressIPAMDataDir = egressPluginIpamDataDirV4
		// Enable IPv4 egress when "ENABLE_V4_EGRESS" is "true" (default)
		egressEnabled = utils.GetBoolAsStringEnvVar(envEnIPv4Egress, defaultEnableIPv4Egress)
		egressPluginLogFile = utils.GetEnv(envEgressV4PluginLogFile, defaultEgressV4PluginLogFile)
		nodeIP, err = getPrimaryIP(true)
		// Node should have a IPv4 address even in IPv6 cluster
		if err != nil {
			log.Errorf("Failed to get Node IP, error: %v", err)
			return err
		}
	} else {
		// EKS IPv4 cluster
		egressIPAMSubnet = egressPluginIpamSubnetV6
		egressIPAMDst = egressPluginIpamDstV6
		egressIPAMDataDir = egressPluginIpamDataDirV6
		egressPluginLogFile = utils.GetEnv(envEgressV6PluginLogFile, defaultEgressV6PluginLogFile)
		egressEnabled = utils.GetBoolAsStringEnvVar(envEnIPv6Egress, defaultEnableIPv6Egress)
		if egressEnabled {
			nodeIP, err = getPrimaryIP(false)
			if err != nil {
				log.Errorf("To support IPv6 egress, node primary ENI must have a global IPv6 address, error: %v", err)
				return err
			}
		}
	}
	vethPrefix := utils.GetEnv(envVethPrefix, defaultVethPrefix)
	mtu := utils.GetEnv(envEniMTU, defaultMTU)
	podSGEnforcingMode := utils.GetEnv(envPodSGEnforcingMode, defaultPodSGEnforcingMode)
	pluginLogFile := utils.GetEnv(envPluginLogFile, defaultPluginLogFile)
	pluginLogLevel := utils.GetEnv(envPluginLogLevel, defaultPluginLogLevel)
	randomizeSNAT := utils.GetEnv(envRandomizeSNAT, defaultRandomizeSNAT)

	netconf := string(byteValue)
	netconf = strings.Replace(netconf, "__VETHPREFIX__", vethPrefix, -1)
	netconf = strings.Replace(netconf, "__MTU__", mtu, -1)
	netconf = strings.Replace(netconf, "__PODSGENFORCINGMODE__", podSGEnforcingMode, -1)
	netconf = strings.Replace(netconf, "__PLUGINLOGFILE__", pluginLogFile, -1)
	netconf = strings.Replace(netconf, "__PLUGINLOGLEVEL__", pluginLogLevel, -1)
	netconf = strings.Replace(netconf, "__EGRESSPLUGINLOGFILE__", egressPluginLogFile, -1)
	netconf = strings.Replace(netconf, "__EGRESSPLUGINENABLED__", strconv.FormatBool(egressEnabled), -1)
	netconf = strings.Replace(netconf, "__EGRESSPLUGINIPAMSUBNET__", egressIPAMSubnet, -1)
	netconf = strings.Replace(netconf, "__EGRESSPLUGINIPAMDST__", egressIPAMDst, -1)
	netconf = strings.Replace(netconf, "__EGRESSPLUGINIPAMDATADIR__", egressIPAMDataDir, -1)
	netconf = strings.Replace(netconf, "__RANDOMIZESNAT__", randomizeSNAT, -1)
	netconf = strings.Replace(netconf, "__NODEIP__", nodeIP, -1)

	byteValue = []byte(netconf)

	// Chain any requested CNI plugins
	enBandwidthPlugin := utils.GetBoolAsStringEnvVar(envEnBandwidthPlugin, defaultEnBandwidthPlugin)
	disablePodV6 := utils.GetBoolAsStringEnvVar(envDisablePodV6, defaultDisablePodV6)
	if enBandwidthPlugin || disablePodV6 {
		// Unmarshall current conflist into data
		data := NetConfList{}
		err = json.Unmarshal(byteValue, &data)
		if err != nil {
			return err
		}

		// Chain the bandwidth plugin when enabled
		if enBandwidthPlugin {
			bwPlugin := NetConf{
				Type:         "bandwidth",
				Capabilities: map[string]bool{"bandwidth": true},
			}
			data.Plugins = append(data.Plugins, &bwPlugin)
		}

		// Chain the tuning plugin (configured to disable IPv6 in pod network namespace) when requested
		if disablePodV6 {
			tuningPlugin := NetConf{
				Type: "tuning",
				Sysctl: map[string]string{
					"net.ipv6.conf.all.disable_ipv6":     "1",
					"net.ipv6.conf.default.disable_ipv6": "1",
					"net.ipv6.conf.lo.disable_ipv6":      "1",
				},
			}
			data.Plugins = append(data.Plugins, &tuningPlugin)
		}

		// Marshall data back into byteValue
		byteValue, err = json.MarshalIndent(data, "", "  ")
		if err != nil {
			return err
		}
	}

	err = isValidJSON(string(byteValue))
	if err != nil {
		log.Fatalf("%s is not a valid json object, error: %s", netconf, err)
	}

	err = os.WriteFile(outFile, byteValue, 0644)
	return err
}

func validateEnvVars() bool {
	pluginLogFile := utils.GetEnv(envPluginLogFile, defaultPluginLogFile)
	if pluginLogFile == "stdout" {
		log.Errorf("AWS_VPC_K8S_PLUGIN_LOG_FILE cannot be set to stdout")
		return false
	}

	// Validate that veth prefix is less than or equal to four characters and not in reserved set: (eth, lo, vlan)
	vethPrefix := utils.GetEnv(envVethPrefix, defaultVethPrefix)
	if len(vethPrefix) > 4 {
		log.Errorf("AWS_VPC_K8S_CNI_VETHPREFIX cannot be longer than 4 characters")
		return false
	}

	if vethPrefix == "eth" || vethPrefix == "lo" || vethPrefix == "vlan" {
		log.Errorf("AWS_VPC_K8S_CNI_VETHPREFIX cannot be set to reserved values 'eth', 'vlan', or 'lo'")
		return false
	}

	// When ENABLE_POD_ENI is set, validate security group enforcing mode
	enablePodEni := utils.GetBoolAsStringEnvVar(envEnablePodEni, defaultEnablePodEni)
	if enablePodEni {
		podSGEnforcingMode := utils.GetEnv(envPodSGEnforcingMode, defaultPodSGEnforcingMode)
		if podSGEnforcingMode != "strict" && podSGEnforcingMode != "standard" {
			log.Errorf("%s must be set to either 'strict' or 'standard'", envPodSGEnforcingMode)
			return false
		}
	}

	// Validate that IP_COOLDOWN_PERIOD is a valid integer
	ipCooldownPeriod, err, ipCooldownPeriodStr := utils.GetEnvVar(envIPCooldownPeriod, defaultIPCooldownPeriod)
	if err != nil || ipCooldownPeriod < 0 {
		log.Errorf("IP_COOLDOWN_PERIOD MUST be a valid positive integer. %s is invalid", ipCooldownPeriodStr)
		return false
	}

	prefixDelegationEn := utils.GetBoolAsStringEnvVar(envEnPrefixDelegation, defaultEnPrefixDelegation)
	warmIPTarget, err, warmIPTargetStr := utils.GetEnvVar(envWarmIPTarget, defaultWarmIPTarget)
	if err != nil {
		log.Errorf("error when trying to get env WARM_IP_TARGET: %s; input is %v", err, warmIPTargetStr)
		return false
	}
	warmPrefixTarget, err, warmPrefixTargetStr := utils.GetEnvVar(envWarmPrefixTarget, defaultWarmPrefixTarget)
	if err != nil {
		log.Errorf("error when trying to get env WARM_PREFIX_TARGET: %s; input is %v", err, warmPrefixTargetStr)
		return false
	}
	minimumIPTarget, err, minimumIPTargetStr := utils.GetEnvVar(envMinIPTarget, defaultMinIPTarget)
	if err != nil {
		log.Errorf("error when trying to get env MINIMUM_IP_TARGET: %s; input is %v", err, minimumIPTargetStr)
		return false
	}

	if prefixDelegationEn && (warmIPTarget == 0 && warmPrefixTarget == 0 && minimumIPTarget == 0) {
		log.Errorf("Setting WARM_PREFIX_TARGET = 0 is not supported while WARM_IP_TARGET/MINIMUM_IP_TARGET is not set. Please configure either one of the WARM_{PREFIX/IP}_TARGET or MINIMUM_IP_TARGET env variables")
		return false
	} else if warmIPTargetStr == "" && minimumIPTargetStr != "" {
		log.Errorf("WARM_IP_TARGET MUST be set when MINIMUM_IP_TARGET is set. Please configure both environment variables.")
		return false
	}
	return true
}

func main() {
	os.Exit(_main())
}

func _main() int {
	log.Debug("Started aws-node container")
	if !validateEnvVars() {
		return 1
	}

	pluginBins := []string{"aws-cni", "egress-cni"}
	hostCNIBinPath := utils.GetEnv(envHostCniBinPath, defaultHostCniBinPath)
	err := cp.InstallBinaries(pluginBins, hostCNIBinPath)
	if err != nil {
		log.WithError(err).Error("Failed to install CNI binaries")
		return 1
	}

	log.Infof("Starting IPAM daemon... ")

	cmd := "./aws-k8s-agent"
	// Exec redirects stdout and stderr to /dev/null, redirecting to os.Stdout and os.Stderr is done explicitly.
	// This enables the output of the aws-k8s-agent to be displayed in the kubectl logs for the aws-node container via stdout and stderr.
	ipamdDaemon := exec.Command(cmd)
	ipamdDaemon.Stdout = os.Stdout
	ipamdDaemon.Stderr = os.Stderr

	err = ipamdDaemon.Start()
	if err != nil {
		log.WithError(err).Errorf("Failed to execute command: %s", cmd)
		return 1
	}

	log.Infof("Checking for IPAM connectivity... ")
	if !waitForIPAM() {
		log.Errorf("Timed out waiting for IPAM daemon to start")
		return 1
	}

	// Wait for init container to complete
	//if err := waitForInit(); err != nil {
	//	log.WithError(err).Errorf("Init container failed to complete")
	//	return 1
	//}

	log.Infof("Copying config file... ")
	err = generateJSON(defaultAWSconflistFile, tmpAWSconflistFile, getPrimaryIP)
	if err != nil {
		log.WithError(err).Errorf("Failed to generate 10-awsconflist")
		return 1
	}

	hostCniConfDirPath := utils.GetEnv(envHostCniConfDirPath, defaultHostCniConfDirPath)
	err = cp.CopyFile(tmpAWSconflistFile, hostCniConfDirPath+awsConflistFile)
	if err != nil {
		log.WithError(err).Errorf("Failed to copy %s", awsConflistFile)
		return 1
	}
	log.Infof("Successfully copied CNI plugin binary and config file.")

	err = ipamdDaemon.Wait()
	if err != nil {
		log.WithError(err).Errorf("Failed to wait for IPAM daemon to complete")
		return 1
	}
	log.Infof("IPAMD stopped hence exiting ...")
	return 0
}
