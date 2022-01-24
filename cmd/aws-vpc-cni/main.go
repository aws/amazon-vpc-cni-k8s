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

package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/aws/amazon-vpc-cni-k8s/utils/cp"
	"github.com/aws/amazon-vpc-cni-k8s/utils/imds"
	"github.com/containernetworking/cni/pkg/types"
)

const (
	defaultHostCNIBinPath        = "/host/opt/cni/bin"
	defaultHostCNIConfDirPath    = "/host/etc/cni/net.d"
	defaultAWSconflistFile       = "/app/10-aws.conflist"
	tmpAWSconflistFile           = "/tmp/10-aws.conflist"
	defaultAgentLogPath          = "aws-k8s-agent.log"
	defaultVethPrefix            = "eni"
	defaultMTU                   = "9001"
	defaultPluginLogFile         = "/var/log/aws-routed-eni/plugin.log"
	defaultEgressV4PluginLogFile = "/var/log/aws-routed-eni/egress-v4-plugin.log"
	defaultPluginLogLevel        = "Debug"
	defaultEnableIPv6            = "false"
	awsConflistFile              = "/10-aws.conflist"

	envAgentLogPath          = "AGENT_LOG_PATH"
	envHostCniBinPath        = "HOST_CNI_BIN_PATH"
	envHostCniConfDirPath    = "HOST_CNI_CONFDIR_PATH"
	envVethPrefix            = "AWS_VPC_K8S_CNI_VETHPREFIX"
	envEniMTU                = "AWS_VPC_ENI_MTU"
	envPluginLogFile         = "AWS_VPC_K8S_PLUGIN_LOG_FILE"
	envPluginLogLevel        = "AWS_VPC_K8S_PLUGIN_LOG_LEVEL"
	envEgressV4PluginLogFile = "AWS_VPC_K8S_EGRESS_V4_PLUGIN_LOG_FILE"
	envConfRPFfilter         = "AWS_VPC_K8S_CNI_CONFIGURE_RPFILTER"
	envEnPrefixDelegation    = "ENABLE_PREFIX_DELEGATION"
	envWarmIPTarget          = "WARM_IP_TARGET"
	envMinIPTarget           = "MINIMUM_IP_TARGET"
	envWarmPrefixTarget      = "WARM_PREFIX_TARGET"
	envEnBandwidthPlugin     = "ENABLE_BANDWIDTH_PLUGIN"
	envEnIPv6                = "ENABLE_IPv6"
)

func getEnv(env, def string) string {
	if val, ok := os.LookupEnv(env); ok {
		return val
	}
	return def
}

// zero values of structs with omitempty is not supported, Golang issue for - https://github.com/golang/go/issues/11939
// cni/types DNS struct is missing omitstring
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

	Name         string          `json:"name,omitempty"`
	Type         string          `json:"type,omitempty"`
	Capabilities map[string]bool `json:"capabilities,omitempty"`
	IPAM         *IPAMConfig     `json:"ipam,omitempty"`
	DNS          *types.DNS      `json:"dns,omitempty"`

	RawPrevResult map[string]interface{} `json:"prevResult,omitempty"`
	PrevResult    types.Result           `json:"-"`

	// Interface inside container to create
	IfName string `json:"ifName,omitempty"`

	Enabled string `json:"enabled,,omitempty"`

	// IP to use as SNAT target
	NodeIP net.IP `json:"nodeIP,omitempty"`

	VethPrefix string `json:"vethPrefix,omitempty"`

	// MTU for eth0
	MTU string `json:"mtu,omitempty"`

	PluginLogFile string `json:"pluginLogFile,omitempty"`

	PluginLogLevel string `json:"pluginLogLevel,omitempty"`
}

//Ref : https://github.com/containernetworking/plugins/plugins/ipam/host-local/backend/allocator/config.go
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

type RangeSet []Range

type Range struct {
	RangeStart net.IP      `json:"rangeStart,omitempty"` // The first ip, inclusive
	RangeEnd   net.IP      `json:"rangeEnd,omitempty"`   // The last ip, inclusive
	Subnet     types.IPNet `json:"subnet"`
	Gateway    net.IP      `json:"gateway,omitempty"`
}

func waitForIPAM() bool {
	for {
		cmd := exec.Command("./grpc-health-probe", "-addr", "127.0.0.1:50051", ">", "/dev/null", "2>&1")
		var outb bytes.Buffer
		cmd.Stdout = &outb
		cmd.Run()
		if outb.String() == "" {
			return true
		}
		time.Sleep(1 * time.Second)
	}
}

func getNodePrimaryV4Address() (string, error) {
	var hostIP string
	var err error
	for {
		hostIP, err = imds.GetMetaData("local-ipv4")
		if err != nil {
			log.WithError(err).Fatalf("aws-vpc-cni init failed\n")
			return "", err
		}
		if hostIP != "" {
			return hostIP, nil
		}

		time.Sleep(1 * time.Second)
	}
}

func isValidJSON(inFile string) error {
	var result map[string]interface{}
	return json.Unmarshal([]byte(inFile), &result)
}

func generateJson(jsonFile string, outFile string) error {

	byteValue, err := ioutil.ReadFile(jsonFile)
	if err != nil {
		return err
	}

	var nodeIP string
	nodeIP, err = getNodePrimaryV4Address()
	if err != nil {
		log.Errorf("Failed to get Node IP")
		return err
	}

	vethPrefix := getEnv(envVethPrefix, defaultVethPrefix)
	mtu := getEnv(envEniMTU, defaultMTU)
	pluginLogFile := getEnv(envPluginLogFile, defaultPluginLogFile)
	pluginLogLevel := getEnv(envPluginLogLevel, defaultPluginLogLevel)
	egressV4pluginLogFile := getEnv(envEgressV4PluginLogFile, defaultEgressV4PluginLogFile)
	enabledIPv6 := getEnv(envEnIPv6, defaultEnableIPv6)

	netconf := string(byteValue)
	netconf = strings.Replace(netconf, "__VETHPREFIX__", vethPrefix, -1)
	netconf = strings.Replace(netconf, "__PLUGINLOGFILE__", pluginLogFile, -1)
	netconf = strings.Replace(netconf, "__PLUGINLOGLEVEL__", pluginLogLevel, -1)
	netconf = strings.Replace(netconf, "__MTU__", mtu, -1)
	netconf = strings.Replace(netconf, "__EGRESSV4PLUGINLOGFILE__", egressV4pluginLogFile, -1)
	netconf = strings.Replace(netconf, "__EGRESSV4PLUGINENABLED__", enabledIPv6, -1)
	netconf = strings.Replace(netconf, "__NODEIP__", nodeIP, -1)

	byteValue = []byte(netconf)

	enBandwidthPlugin := getEnv(envEnBandwidthPlugin, "false")
	if enBandwidthPlugin == "true" {
		data := NetConfList{}
		err = json.Unmarshal(byteValue, &data)
		if err != nil {
			return err
		}

		bwPlugin := NetConf{
			Type:         "bandwidth",
			Capabilities: map[string]bool{"bandwidth": true},
		}
		data.Plugins = append(data.Plugins, &bwPlugin)
		byteValue, err = json.MarshalIndent(data, "", "  ")
		if err != nil {
			return err
		}
	}

	err = isValidJSON(string(byteValue))
	if err != nil {
		log.Fatalf("%s is not a valid json object\nerror: %s", netconf, err)
	}

	err = ioutil.WriteFile(outFile, byteValue, 0644)
	return err
}

func validate_env_variable() bool {
	pluginLogFile := getEnv(envPluginLogFile, defaultPluginLogFile)
	if pluginLogFile == "stdout" {
		log.Errorf("AWS_VPC_K8S_PLUGIN_LOG_FILE cannot be set to stdout")
		return false
	}

	vethPrefix := getEnv(envVethPrefix, defaultVethPrefix)
	if len(vethPrefix) > 4 {
		log.Errorf("AWS_VPC_K8S_CNI_VETHPREFIX cannot be longer than 4 characters")
		return false
	}

	if vethPrefix == "eth" || vethPrefix == "lo" || vethPrefix == "vlan" {
		log.Errorf("AWS_VPC_K8S_CNI_VETHPREFIX cannot be set to reserved values eth or vlan or lo")
		return false
	}

	prefixDelegationEn := getEnv(envEnPrefixDelegation, "false")
	warmIPtarget := getEnv(envWarmIPTarget, "0")
	warmPrefixtarget := getEnv(envWarmPrefixTarget, "0")
	minimumIPtarget := getEnv(envMinIPTarget, "0")

	if (prefixDelegationEn == "true") && (warmIPtarget <= "0" || warmPrefixtarget <= "0" || minimumIPtarget <= "0") {
		log.Errorf("Setting WARM_PREFIX_TARGET = 0 is not supported while WARM_IP_TARGET/MINIMUM_IP_TARGET is not set. Please configure either one of the WARM_{PREFIX/IP}_TARGET or MINIMUM_IP_TARGET env variables")
		return false
	}
	return true
}

func main() {
	os.Exit(_main())
}

func _main() int {
	log.Debug("Started aws-node container")
	if !validate_env_variable() {
		return 1
	}

	hostCNIBinPath := getEnv(envHostCniBinPath, defaultHostCNIBinPath)
	configureRPFfilter := getEnv(envConfRPFfilter, "true")
	if configureRPFfilter != "false" {
		log.Infof("Copying CNI plugin binaries ... ")
		pluginBins := []string{"loopback", "portmap", "bandwidth", "aws-cni-support.sh"}
		err := cp.InstallBinaries(pluginBins, hostCNIBinPath)
		if err != nil {
			log.WithError(err).Errorf("Failed to install binaries")
			return 1
		}
	}

	log.Infof("Install CNI binaries..")
	pluginBins := []string{"aws-cni", "egress-v4-cni"}
	err := cp.InstallBinaries(pluginBins, hostCNIBinPath)
	if err != nil {
		log.WithError(err).Errorf("Failed to install binaries")
		return 1
	}

	log.Infof("Starting IPAM daemon ... ")

	agentLogPath := getEnv(envAgentLogPath, defaultAgentLogPath)

	cmd := "./aws-k8s-agent"
	ipamdDaemon := exec.Command(cmd, "|", "tee", "-i", agentLogPath, "2>&1")
	err = ipamdDaemon.Start()
	if err != nil {
		log.WithError(err).Errorf("Failed to execute command: %s", cmd)
		return 1
	}

	log.Infof("Checking for IPAM connectivity ... ")
	if !waitForIPAM() {
		log.Errorf("Timed out waiting for IPAM daemon to start:")

		byteValue, err := ioutil.ReadFile(agentLogPath)
		if err != nil {
			log.WithError(err).Errorf("Failed to read %s", agentLogPath)
		}
		log.Infof("%s", string(byteValue))
		return 1
	}

	log.Infof("Copying config file ... ")
	err = generateJson(defaultAWSconflistFile, tmpAWSconflistFile)
	if err != nil {
		log.WithError(err).Errorf("Failed to update 10-awsconflist")
		return 1
	}

	err = cp.CopyFile(tmpAWSconflistFile, defaultHostCNIConfDirPath+awsConflistFile)
	if err != nil {
		log.WithError(err).Errorf("Failed to update 10-awsconflist")
		return 1
	}

	log.Infof("Successfully copied CNI plugin binary and config file.")

	awsConfFile := defaultHostCNIConfDirPath + "/aws.conf"
	if _, err := os.Stat(awsConfFile); err == nil {
		err = os.Remove(awsConfFile)
		if err != nil {
			log.WithError(err).Errorf("Failed to delete file %s", awsConfFile)
			return 1
		}
	}

	err = ipamdDaemon.Wait()
	if err != nil {
		log.WithError(err).Errorf("Failed to wait for IPAM daemon to complete")
		return 1
	}
	log.Infof("IPAMD stopped hence exiting ...")
	return 0
}
