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

package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/aws/amazon-ecs-cni-plugins/pkg/execwrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/ioutilwrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/oswrapper"
	log "github.com/cihub/seelog"
	"github.com/pkg/errors"
)

const (
	dhclientExecutableNameDefault   = "dhclient"
	dhclientExecutableNameEnvConfig = "ENI_DHCLIENT_EXECUTABLE"

	dhclientLeasesFilePathDefault   = "/var/lib/dhclient"
	dhclientLeasesFilePathEnvConfig = "ENI_DHCLIENT_LEASES_PATH"
	// dhclientLeasesFileFormat is the format string for the dhclient
	// leases file for a network interface. It's composed of:
	// i. Path prefix
	// ii. Device Name
	// iii. Internet Protocol Revision ('4' or '6')
	dhclientLeasesFileFormat = "%s/ns-%s-dhclient%d.leases"

	dhclientPIDFilePathDefault   = "/var/run"
	dhclientPIDFilePathEnvConfig = "ENI_DHCLIENT_PID_FILE_PATH"
	// dhclientPIDFileFormat is the format string for the dhclient
	// leases file for a network interface. It's composed of:
	// i. Path prefix
	// ii. Device Name
	// iii. Internet Protocol Revision ('4' or '6')
	dhclientPIDFileFormat = "%s/ns-%s-dhclient%d.pid"

	ipRev4 = 4
	ipRev6 = 6

	// processFinishedErrorMessage reflects the corresponding error message
	// emitted by the os package when it finds that a process has finished
	// its execution. Unfortunately, this is local variable in the os
	// package and is not exposed anywhere. Hence, we declare this string to
	// match the error message returned by the os package
	processFinishedErrorMessage = "os: process already finished"

	maxDHClientStartAttempts            = 10
	durationBetweenDHClientStartRetries = 2 * time.Second
)

// DHClient interface provides methods to perform various dhclient actions
type DHClient interface {
	// IsExecutableInPath returns true if the dhclient executable is found
	// in the path
	IsExecutableInPath() bool
	// Start starts the dhclient process for a given device name. The IP
	// revision can be either '4' or '6'
	Start(deviceName string, ipRev int) error
	// Stop stops the dhclient process for a given device name until max
	// stop wait duration. The IP revision can be either '4' or '6'.
	Stop(deviceName string, ipRev int, checkStateInterval time.Duration, maxWait time.Duration) error
}

type dhclient struct {
	os                                  oswrapper.OS
	exec                                execwrapper.Exec
	ioutil                              ioutilwrapper.IOUtil
	executable                          string
	leasesFilePath                      string
	pidFilePath                         string
	dhclientStartAttemptsMax            int
	dhclientStartDurationBetweenRetries time.Duration
}

// NewDHClient creates a new DHClient object
func NewDHClient() *dhclient {
	return newDHClient(
		oswrapper.NewOS(),
		execwrapper.NewExec(),
		ioutilwrapper.NewIOUtil(),
		maxDHClientStartAttempts,
		durationBetweenDHClientStartRetries)
}

func newDHClient(os oswrapper.OS,
	exec execwrapper.Exec,
	ioutil ioutilwrapper.IOUtil,
	dhclientStartAttemptsMax int,
	dhclientStartDurationBetweenRetries time.Duration) *dhclient {
	client := &dhclient{
		os:     os,
		exec:   exec,
		ioutil: ioutil,
		dhclientStartAttemptsMax:            dhclientStartAttemptsMax,
		dhclientStartDurationBetweenRetries: dhclientStartDurationBetweenRetries,
	}
	client.loadEnvConfig()
	return client
}

func (client *dhclient) loadEnvConfig() {
	client.executable = client.getEnvOrDefault(
		dhclientExecutableNameEnvConfig, dhclientExecutableNameDefault)
	client.leasesFilePath = client.getEnvOrDefault(
		dhclientLeasesFilePathEnvConfig, dhclientLeasesFilePathDefault)
	client.pidFilePath = client.getEnvOrDefault(
		dhclientPIDFilePathEnvConfig, dhclientPIDFilePathDefault)
}

func (client *dhclient) getEnvOrDefault(key string, defaultValue string) string {
	envVal := client.os.Getenv(key)
	if envVal == "" {
		return defaultValue
	}
	return envVal
}

func (client *dhclient) IsExecutableInPath() bool {
	dhclientPath, err := client.exec.LookPath(client.executable)
	if err != nil {
		log.Errorf("Looking up dhclient in PATH: %v", err)
		return false
	}

	log.Debugf("dhclient found in: %s", dhclientPath)
	return true
}

func (client *dhclient) Start(deviceName string, ipRev int) error {
	args := client.constructDHClientArgs(deviceName, ipRev)
	var out []byte
	var err error
	attempts := 1
	for {
		cmd := client.exec.Command(client.executable, args...)
		out, err = cmd.CombinedOutput()
		if err == nil {
			return nil
		}

		log.Warnf("Error executing '%s' with args '%v' (attempt %d/%d): raw output: %s",
			client.executable, args, attempts, client.dhclientStartAttemptsMax, string(out))
		// If a dhclient process was already running in the default namespace,
		// there might be a delay in its cleanup, which will result in an
		// error in trying to start dhclient process. Retry to offset that.
		if attempts >= client.dhclientStartAttemptsMax {
			return errors.Wrapf(err,
				"engine dhclient: unable to start dhclient for ipv%d address; command: %s %v; output: %s",
				ipRev, client.executable, args, string(out))

		}
		attempts++
		time.Sleep(client.dhclientStartDurationBetweenRetries)
	}

	return nil
}

// constructDHClientArgs constructs the arguments list for the dhclient command to
// renew the lease on the IPV4 address
func (client *dhclient) constructDHClientArgs(deviceName string, ipRev int) []string {
	commonArgs := []string{
		"-q", // Suppress all messages to terminal
	}
	if ipRev == ipRev6 {
		commonArgs = append(commonArgs,
			"-6", // Use DHCPv6 protocol
		)
	}
	return append(commonArgs,
		"-lf",
		client.getLeasesFile(deviceName, ipRev), // The path to the leases file
		"-pf",
		client.getPIDFile(deviceName, ipRev), // The path to the pid file
		deviceName)
}

func (client *dhclient) getPIDFile(deviceName string, ipRev int) string {
	return fmt.Sprintf(dhclientPIDFileFormat,
		client.pidFilePath, deviceName, ipRev)
}

func (client *dhclient) getLeasesFile(deviceName string, ipRev int) string {
	return fmt.Sprintf(dhclientLeasesFileFormat,
		client.leasesFilePath, deviceName, ipRev)
}

func (client *dhclient) Stop(deviceName string, ipRev int,
	checkStateInterval time.Duration, maxWaitDuration time.Duration) error {
	pid, err := client.getPID(deviceName, ipRev)
	if err != nil {
		return err
	}

	// TODO: Verify that pid is actually associated with dhclient

	// Stop the dhclient process
	return client.stopProcess(pid,
		checkStateInterval, maxWaitDuration)
}

func (client *dhclient) getPID(deviceName string, ipRev int) (int, error) {
	pidFile := client.getPIDFile(deviceName, ipRev)
	contents, err := client.ioutil.ReadFile(pidFile)
	if err != nil {
		return -1, errors.Wrapf(err,
			"engine dhclient: error reading dhclient pid from '%s'", pidFile)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	if err != nil {
		return -1, errors.Wrapf(err,
			"engine dhclient: error parsing dhclient pid from '%s'", pidFile)
	}

	return pid, nil
}

func (client *dhclient) stopProcess(pid int,
	checkStateInterval time.Duration, maxWaitDuration time.Duration) error {
	// Get the process handle from the pid
	process, err := client.os.FindProcess(pid)
	if err != nil {
		// As per https://golang.org/pkg/os/#FindProcess, on linux, this
		// is a noop and should never return an error
		return errors.Wrapf(err,
			"engine dhclient: error getting process handle for pid: '%d'", pid)
	}

	// Try stopping the process
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return errors.Wrapf(err,
			"engine dhclient: unable to send SIGTERM to the dhclient process")
	}

	// Wait for the process to finish
	ctx, cancel := context.WithTimeout(context.TODO(), maxWaitDuration)
	err = client.waitForProcesToFinish(ctx, cancel, checkStateInterval, process)
	if err != nil {
		// Kill the process as it didn't exit within the allowed
		// duration
		log.Infof(
			"Trying to kill dhclient process with pid: '%d' as we were unable to stop it, error: %v",
			pid, err)
		if err := process.Signal(syscall.SIGKILL); err != nil {
			return errors.Wrapf(err,
				"engine dhclient: unable to send SIGKILL to the dhclient process")
		}
	}

	return nil
}

func (client *dhclient) waitForProcesToFinish(
	ctx context.Context, cancel context.CancelFunc,
	checkStateInterval time.Duration, process oswrapper.OSProcess) error {
	ticker := time.NewTicker(checkStateInterval)
	for {
		select {
		case <-ticker.C:
			ok, err := client.isProcessFinished(process)
			if err != nil {
				log.Errorf("Error determining if dhclient process is finished: %v", err)
				continue
			}
			if ok {
				cancel()
				return nil
			}
		case <-ctx.Done():
			ticker.Stop()
			return errors.New("engine dhclient: timed out waiting for process to finish")
		}
	}
}

func (client *dhclient) isProcessFinished(process oswrapper.OSProcess) (bool, error) {
	// As per `man 2 kill`, if the signal is '0', no signal is sent, but the
	// error checking is still performed. This is the most reliable way that
	// I (aaithal) have found to check if a process with a given pid is
	// running via golang libraries.
	err := process.Signal(syscall.Signal(0))
	if err != nil {
		if err.Error() == processFinishedErrorMessage {
			return true, nil
		}
		return false, errors.Wrap(err,
			"engine dhclient: unable to send nop signal to the dhclient process")
	}

	return false, nil
}

func (client *dhclient) getExecutable() string {
	return client.executable
}
