// +build e2e

// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package e2eTests

import (
	"fmt"
	"io/ioutil"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/containernetworking/cni/pkg/invoke"
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
)

const (
	ifName        = "ecs-test-eth0"
	containerID   = "contain-er"
	netConfFormat = `
{
    "type":"ecs-eni",
    "cniVersion":"0.3.0",
    "eni":"%s",
    "ipv4-address":"%s",
    "mac":"%s",
    "block-instance-metadata":true
}`
	imdsEndpoint = "169.254.169.254/32"
)

func init() {
	// This is to ensure that all the namespace operations are performed for
	// a single thread
	runtime.LockOSThread()
}

type config struct {
	region         string
	subnet         string
	index          int64
	instanceID     string
	securityGroups []string
	vpc            string
}

func TestAddDel(t *testing.T) {
	// Ensure that the eni plugin exists
	eniPluginPath, err := invoke.FindInPath("ecs-eni", []string{os.Getenv("CNI_PATH")})
	require.NoError(t, err, "Unable to find eni plugin in path")

	// Ensure that we are able to build a config from instance's metadata
	cfg, err := newConfig()
	require.NoError(t, err, "Unable to get instance config")
	ec2Client := ec2.New(session.Must(session.NewSession()), &aws.Config{
		Region: aws.String(cfg.region),
	})

	// Create an ENI
	eni, err := createENI(ec2Client, cfg)
	require.NoError(t, err, "Unable to create ENI")
	defer deleteENI(ec2Client, eni)

	require.NoError(t, waitUntilNetworkInterfaceAvailable(ec2Client, eni), "ENI didn't transition into 'available'")
	// Attach the ENI to the instance
	attachment, err := attachENI(ec2Client, cfg, eni)
	require.NoError(t, err, "Unable to attach ENI")
	defer detachENI(ec2Client, attachment)

	require.NoError(t, waitUntilNetworkInterfaceAttached(eni, 5*time.Second), "ENI was not attached to the instance")

	// Create a directory for storing test logs
	testLogDir, err := ioutil.TempDir("", "ecs-eni-e2e-test-")
	require.NoError(t, err, "Unable to create directory for storing test logs")

	// Configure the env var to use the test logs directory
	os.Setenv("ECS_CNI_LOG_FILE", fmt.Sprintf("%s/eni.log", testLogDir))
	t.Logf("Using %s for test logs", testLogDir)
	defer os.Unsetenv("ECS_CNI_LOG_FILE")

	// Configure dhclient env var's for the plugin to use test logs directory
	os.Setenv("ENI_DHCLIENT_LEASES_PATH", testLogDir)
	defer os.Unsetenv("ENI_DHCLIENT_LEASES_PATH")
	os.Setenv("ENI_DHCLIENT_PID_FILE_PATH", testLogDir)
	defer os.Unsetenv("ENI_DHCLIENT_PID_FILE_PATH")

	// Handle deletion of test logs at the end of the test execution if
	// specified
	ok, err := strconv.ParseBool(getEnvOrDefault("ECS_PRESERVE_E2E_TEST_LOGS", "false"))
	assert.NoError(t, err, "Unable to parse ECS_PRESERVE_E2E_TEST_LOGS env var")
	defer func(preserve bool) {
		if !t.Failed() && !preserve {
			os.RemoveAll(testLogDir)
		}
	}(ok)

	// Use the current network namespace to execute the test in
	testNS, err := ns.GetCurrentNS()
	require.NoError(t, err, "Unable to get the network namespace to run the test in")
	defer testNS.Close()

	// Create a network namespace to mimic the container's network namespace.
	// The ENI will be moved to this namespace
	targetNS, err := ns.NewNS()
	require.NoError(t, err,
		"Unable to create the network namespace that represents the network namespace of the container")
	defer targetNS.Close()

	// Construct args to invoke the CNI plugin with
	execInvokeArgs := &invoke.Args{
		ContainerID: containerID,
		NetNS:       targetNS.Path(),
		IfName:      ifName,
		Path:        os.Getenv("CNI_PATH"),
	}
	netConf := []byte(fmt.Sprintf(netConfFormat,
		aws.StringValue(eni.NetworkInterfaceId),
		aws.StringValue(eni.PrivateIpAddress),
		aws.StringValue(eni.MacAddress)))
	t.Logf("Using config: %s", string(netConf))

	testNS.Do(func(ns.NetNS) error {
		// Execute the "ADD" command for the plugin
		execInvokeArgs.Command = "ADD"
		err := invoke.ExecPluginWithoutResult(
			eniPluginPath,
			netConf,
			execInvokeArgs)
		require.NoError(t, err, "Unable to execute ADD command for ecs-eni plugin")
		return nil
	})

	targetNS.Do(func(ns.NetNS) error {
		// Validate that only 2 devices exist in the target network
		// namespace (lo and eni)
		links, err := netlink.LinkList()
		require.NoError(t, err, "Unable to list devices in target network namespace")
		assert.Len(t, links, 2, "Incorrect number of devices discovered in taget network namespace")
		eniFound := false
		for _, link := range links {
			if link.Attrs().HardwareAddr.String() == aws.StringValue(eni.MacAddress) {
				eniFound = true
				break
			}
		}
		require.True(t, eniFound, "ENI not found in target network namespace")

		validateTargetNSRoutes(t)
		// TODO: Validate that dhclient process is running
		return nil
	})

	testNS.Do(func(ns.NetNS) error {
		// Execute the "DEL" command for the plugin
		execInvokeArgs.Command = "DEL"
		err := invoke.ExecPluginWithoutResult(
			eniPluginPath,
			netConf,
			execInvokeArgs)
		require.NoError(t, err, "Unable to execute DEL command for ecs-eni plugin")
		// TODO: Validate that the dhclient process is stopped
		return nil
	})
}

// newConfig creates a new config object
func newConfig() (*config, error) {
	ec2Metadata := ec2metadata.New(session.Must(session.NewSession()))
	region, err := ec2Metadata.Region()
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get region from ec2 metadata")
	}

	instanceID, err := ec2Metadata.GetMetadata("instance-id")
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get instance id from ec2 metadata")
	}

	mac, err := ec2Metadata.GetMetadata("mac")
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get mac from ec2 metadata")
	}

	securityGroups, err := ec2Metadata.GetMetadata("security-groups")
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get security groups from ec2 metadata")
	}

	interfaces, err := ec2Metadata.GetMetadata("network/interfaces/macs")
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get interfaces from ec2 metadata")
	}

	subnet, err := ec2Metadata.GetMetadata("network/interfaces/macs/" + mac + "/subnet-id")
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get subnet from ec2 metadata")
	}

	vpc, err := ec2Metadata.GetMetadata("network/interfaces/macs/" + mac + "/vpc-id")
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get vpc from ec2 metadata")
	}

	return &config{region: region,
		subnet:         subnet,
		index:          int64(len(strings.Split(interfaces, "\n"))),
		instanceID:     instanceID,
		securityGroups: strings.Split(securityGroups, "\n"),
		vpc:            vpc,
	}, nil
}

// createENI creates an ENI in the same subnet as the instance's primary ENI
func createENI(ec2Client *ec2.EC2, cfg *config) (*ec2.NetworkInterface, error) {
	var filterValuesGroupName []*string
	for _, sg := range cfg.securityGroups {
		filterValuesGroupName = append(filterValuesGroupName, aws.String(sg))
	}
	// Get security group id for the security group that the instance was
	// started with
	securityGroups, err := ec2Client.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("group-name"),
				Values: filterValuesGroupName,
			},
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{aws.String(cfg.vpc)},
			},
		}})
	if err != nil {
		return nil, errors.Wrapf(err, "unable to get security group ids")
	}
	var securityGroupIDs []*string
	for _, sg := range securityGroups.SecurityGroups {
		securityGroupIDs = append(securityGroupIDs, sg.GroupId)
	}

	// Create the ENI
	output, err := ec2Client.CreateNetworkInterface(&ec2.CreateNetworkInterfaceInput{
		Description: aws.String("for running end-to-end test for ECS ENI Plugin"),
		Groups:      securityGroupIDs,
		SubnetId:    aws.String(cfg.subnet),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "unable to create network interface")
	}
	return output.NetworkInterface, nil
}

// waitUntilNetworkInterfaceAvailable waits until the ENI state == "available"
func waitUntilNetworkInterfaceAvailable(ec2Client *ec2.EC2, eni *ec2.NetworkInterface) error {
	return ec2Client.WaitUntilNetworkInterfaceAvailable(&ec2.DescribeNetworkInterfacesInput{
		Filters: []*ec2.Filter{{
			Name:   aws.String("network-interface-id"),
			Values: []*string{eni.NetworkInterfaceId}},
		}})
}

// deleteENI deletes the ENI
func deleteENI(ec2Client *ec2.EC2, eni *ec2.NetworkInterface) error {
	err := waitUntilNetworkInterfaceAvailable(ec2Client, eni)
	if err != nil {
		return errors.Wrapf(err, "failed waiting for ENI to be 'available'")
	}
	_, err = ec2Client.DeleteNetworkInterface(&ec2.DeleteNetworkInterfaceInput{
		NetworkInterfaceId: eni.NetworkInterfaceId,
	})
	if err != nil {
		return errors.Wrapf(err, "unable to deleye ENI")
	}
	return nil
}

// attachENI attaches the ENI to the current EC2 instance
func attachENI(ec2Client *ec2.EC2, cfg *config, eni *ec2.NetworkInterface) (*ec2.AttachNetworkInterfaceOutput, error) {
	return ec2Client.AttachNetworkInterface(&ec2.AttachNetworkInterfaceInput{
		DeviceIndex:        aws.Int64(cfg.index),
		InstanceId:         aws.String(cfg.instanceID),
		NetworkInterfaceId: eni.NetworkInterfaceId,
	})
}

// detachENI detaches the ENI from the current EC2 instance
func detachENI(ec2Client *ec2.EC2, attachment *ec2.AttachNetworkInterfaceOutput) error {
	_, err := ec2Client.DetachNetworkInterface(&ec2.DetachNetworkInterfaceInput{
		AttachmentId: attachment.AttachmentId,
		Force:        aws.Bool(true),
	})

	if err != nil {
		errors.Wrapf(err, "unable to detach ENI")
	}
	return nil
}

// waitUntilNetworkInterfaceAttached waits until the ENI shows up on the instance
func waitUntilNetworkInterfaceAttached(eni *ec2.NetworkInterface, interval time.Duration) error {
	for {
		links, err := netlink.LinkList()
		if err != nil {
			return err
		}
		for _, link := range links {
			if link.Attrs().HardwareAddr.String() == aws.StringValue(eni.MacAddress) {
				return nil
			}
		}
		time.Sleep(interval)
	}
}

// validateTargetNSRoutes validates routes in the target network namespace
func validateTargetNSRoutes(t *testing.T) {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	require.NoError(t, err, "Unable to list routes")

	var imdsRouteFound, gatewayRouteFound bool
	for _, route := range routes {
		if route.Gw == nil && route.Dst.String() == imdsEndpoint {
			imdsRouteFound = true
		}
		if route.Gw != nil && route.Dst == nil {
			gatewayRouteFound = true
		}
	}

	require.True(t, imdsRouteFound, "Blocking route for instance metadata not found ")
	require.True(t, gatewayRouteFound, "Route to use the vpc subnet gateway not found ")
}

// getEnvOrDefault gets the value of an env var. It returns the fallback value
// if the env var is not set
func getEnvOrDefault(name string, fallback string) string {
	val := os.Getenv(name)
	if val == "" {
		return fallback
	}

	return val
}
