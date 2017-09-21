// +build !integration,!e2e

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
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-cni-plugins/pkg/oswrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/oswrapper/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestLoadEnvConfig(t *testing.T) {
	testCases := []struct {
		name                   string
		executableVal          string
		expecedExecutable      string
		leasesFilePathVal      string
		expectedLeasesFilePath string
		pidFilePathVal         string
		expectedPIDFilePath    string
	}{
		{
			"default values are loaded",
			"", dhclientExecutableNameDefault,
			"", dhclientLeasesFilePathDefault,
			"", dhclientPIDFilePathDefault,
		},
		{
			"non-default values are loaded",
			"/sbin/dhclient", "/sbin/dhclient",
			"/leases/file/path", "/leases/file/path",
			"/pid/file/path", "/pid/file/path",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv(dhclientExecutableNameEnvConfig, tc.executableVal)
			os.Setenv(dhclientLeasesFilePathEnvConfig, tc.leasesFilePathVal)
			os.Setenv(dhclientPIDFilePathEnvConfig, tc.pidFilePathVal)

			defer func() {
				os.Unsetenv(dhclientExecutableNameEnvConfig)
				os.Unsetenv(dhclientLeasesFilePathEnvConfig)
				os.Unsetenv(dhclientPIDFilePathEnvConfig)
			}()

			dhclient := newDHClient(oswrapper.NewOS(), nil, nil,
				maxDHClientStartAttempts, durationBetweenDHClientStartRetries)
			assert.Equal(t, tc.expecedExecutable, dhclient.executable)
			assert.Equal(t, tc.expectedLeasesFilePath, dhclient.leasesFilePath)
			assert.Equal(t, tc.expectedPIDFilePath, dhclient.pidFilePath)
		})
	}
}

func TestIsDHClientInPathReturnsFalseOnLookPathError(t *testing.T) {
	ctrl, _, mockIOUtil, _, _, mockExec, mockOS := setup(t)
	defer ctrl.Finish()

	gomock.InOrder(
		mockOS.EXPECT().Getenv(dhclientExecutableNameEnvConfig).Return(""),
		mockOS.EXPECT().Getenv(dhclientLeasesFilePathEnvConfig).Return(""),
		mockOS.EXPECT().Getenv(dhclientPIDFilePathEnvConfig).Return(""),
		mockExec.EXPECT().LookPath(dhclientExecutableNameDefault).Return("", errors.New("error")),
	)
	dhclient := newDHClient(mockOS, mockExec, mockIOUtil,
		maxDHClientStartAttempts, durationBetweenDHClientStartRetries)

	ok := dhclient.IsExecutableInPath()
	assert.False(t, ok)
}

func TestIsDHClientInPath(t *testing.T) {
	ctrl, _, mockIOUtil, _, _, mockExec, mockOS := setup(t)
	defer ctrl.Finish()

	gomock.InOrder(
		mockOS.EXPECT().Getenv(dhclientExecutableNameEnvConfig).Return(""),
		mockOS.EXPECT().Getenv(dhclientLeasesFilePathEnvConfig).Return(""),
		mockOS.EXPECT().Getenv(dhclientPIDFilePathEnvConfig).Return(""),
		mockExec.EXPECT().LookPath(dhclientExecutableNameDefault).Return("dhclient", nil),
	)
	dhclient := newDHClient(mockOS, mockExec, mockIOUtil,
		maxDHClientStartAttempts, durationBetweenDHClientStartRetries)

	ok := dhclient.IsExecutableInPath()
	assert.True(t, ok)
}

func TestGetDHClientArgs(t *testing.T) {
	ctrl, _, mockIOUtil, _, _, mockExec, mockOS := setup(t)
	defer ctrl.Finish()

	gomock.InOrder(
		mockOS.EXPECT().Getenv(dhclientExecutableNameEnvConfig).Return(""),
		mockOS.EXPECT().Getenv(dhclientLeasesFilePathEnvConfig).Return(""),
		mockOS.EXPECT().Getenv(dhclientPIDFilePathEnvConfig).Return(""),
	)

	dhclient := newDHClient(mockOS, mockExec, mockIOUtil,
		maxDHClientStartAttempts, durationBetweenDHClientStartRetries)
	testCases := []struct {
		ipRev        int
		expectedArgs []string
	}{
		{
			ipRev4,
			[]string{
				"-q",
				"-lf",
				dhclientLeasesFilePathDefault + "/ns-" + deviceName + "-dhclient4.leases",
				"-pf",
				dhclientPIDFilePathDefault + "/ns-" + deviceName + "-dhclient4.pid",
				deviceName},
		},
		{
			ipRev6,
			[]string{
				"-q",
				"-6",
				"-lf",
				dhclientLeasesFilePathDefault + "/ns-" + deviceName + "-dhclient6.leases",
				"-pf",
				dhclientPIDFilePathDefault + "/ns-" + deviceName + "-dhclient6.pid",
				deviceName},
		},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("IP%d", tc.ipRev), func(t *testing.T) {
			args := dhclient.constructDHClientArgs(deviceName, tc.ipRev)
			assert.NotEmpty(t, args)
			assert.Equal(t, tc.expectedArgs, args)
		})
	}
}

func TestGetLeasesAndPIDFile(t *testing.T) {
	ctrl, _, mockIOUtil, _, _, mockExec, mockOS := setup(t)
	defer ctrl.Finish()

	gomock.InOrder(
		mockOS.EXPECT().Getenv(dhclientExecutableNameEnvConfig).Return(""),
		mockOS.EXPECT().Getenv(dhclientLeasesFilePathEnvConfig).Return(""),
		mockOS.EXPECT().Getenv(dhclientPIDFilePathEnvConfig).Return(""),
	)

	dhclient := newDHClient(mockOS, mockExec, mockIOUtil,
		maxDHClientStartAttempts, durationBetweenDHClientStartRetries)
	testCases := []struct {
		ipRev              int
		expectedLeasesFile string
		expectedPIDFile    string
	}{
		{
			ipRev4,
			dhclientLeasesFilePathDefault + "/ns-" + deviceName + "-dhclient4.leases",
			dhclientPIDFilePathDefault + "/ns-" + deviceName + "-dhclient4.pid",
		},
		{
			ipRev6,
			dhclientLeasesFilePathDefault + "/ns-" + deviceName + "-dhclient6.leases",
			dhclientPIDFilePathDefault + "/ns-" + deviceName + "-dhclient6.pid",
		},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("IP%d", tc.ipRev), func(t *testing.T) {
			leasesFile := dhclient.getLeasesFile(deviceName, tc.ipRev)
			assert.Equal(t, tc.expectedLeasesFile, leasesFile)
			pidFile := dhclient.getPIDFile(deviceName, tc.ipRev)
			assert.Equal(t, tc.expectedPIDFile, pidFile)
		})
	}
}

func TestIsProcessFinishedReturnsErrorWhenSignalProcessFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dhclient := &dhclient{}
	mockDHClientProcess := mock_oswrapper.NewMockOSProcess(ctrl)
	mockDHClientProcess.EXPECT().Signal(syscall.Signal(0)).Return(errors.New("error"))

	ok, err := dhclient.isProcessFinished(mockDHClientProcess)
	assert.Error(t, err)
	assert.False(t, ok)
}

func TestIsProcessFinishedReturnsFalseWhenSignalProcessSucceeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dhclient := &dhclient{}
	mockDHClientProcess := mock_oswrapper.NewMockOSProcess(ctrl)
	mockDHClientProcess.EXPECT().Signal(syscall.Signal(0)).Return(nil)

	ok, err := dhclient.isProcessFinished(mockDHClientProcess)
	assert.NoError(t, err)
	assert.False(t, ok)
}

func TestIsProcessFinishedReturnsTrueWhenSignalProcessFailsWithProcessFinishedError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dhclient := &dhclient{}
	mockDHClientProcess := mock_oswrapper.NewMockOSProcess(ctrl)
	mockDHClientProcess.EXPECT().Signal(syscall.Signal(0)).Return(errors.New(processFinishedErrorMessage))
	ok, err := dhclient.isProcessFinished(mockDHClientProcess)
	assert.NoError(t, err)
	assert.True(t, ok)
}

func TestWaitForStopWaitsForDuration(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dhclient := &dhclient{}
	ctx, cancel := context.WithCancel(context.TODO())
	mockDHClientProcess := mock_oswrapper.NewMockOSProcess(ctrl)

	gomock.InOrder(
		// Both invocations of isProcessFinished return no error, however the
		// second invocation causes the context being cancelled, thus
		// simulating a context timing out
		mockDHClientProcess.EXPECT().Signal(syscall.Signal(0)).Return(nil),
		mockDHClientProcess.EXPECT().Signal(syscall.Signal(0)).Do(func(_ syscall.Signal) {
			cancel()
		}).Return(nil),
	)

	err := dhclient.waitForProcesToFinish(ctx, cancel, time.Microsecond, mockDHClientProcess)
	assert.Error(t, err)
}

func TestWaitForStopReturnsNoErrorWhenProcessIsFinished(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dhclient := &dhclient{}
	ctx, cancel := context.WithCancel(context.TODO())
	mockDHClientProcess := mock_oswrapper.NewMockOSProcess(ctrl)
	mockDHClientProcess.EXPECT().Signal(syscall.Signal(0)).Return(errors.New(processFinishedErrorMessage))

	err := dhclient.waitForProcesToFinish(ctx, cancel, time.Microsecond, mockDHClientProcess)
	assert.NoError(t, err)
}

func TestWaitForStopWaitsForDurationWhenSignallingProcessFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dhclient := &dhclient{}
	ctx, cancel := context.WithCancel(context.TODO())
	mockDHClientProcess := mock_oswrapper.NewMockOSProcess(ctrl)

	gomock.InOrder(
		// Both invocations of isProcessFinished return error, however the
		// second invocation causes the context being cancelled, thus
		// simulating a context timing out
		mockDHClientProcess.EXPECT().Signal(syscall.Signal(0)).Return(errors.New("error")),
		mockDHClientProcess.EXPECT().Signal(syscall.Signal(0)).Do(func(_ syscall.Signal) {
			cancel()
		}).Return(nil),
	)
	err := dhclient.waitForProcesToFinish(ctx, cancel, time.Microsecond, mockDHClientProcess)
	assert.Error(t, err)
}

func TestStopProcessFailsWhenUnableToSendSIGTERM(t *testing.T) {
	ctrl, _, _, _, _, _, mockOS := setup(t)
	defer ctrl.Finish()

	dhclient := &dhclient{os: mockOS}
	mockDHClientProcess := mock_oswrapper.NewMockOSProcess(ctrl)

	gomock.InOrder(
		mockOS.EXPECT().FindProcess(dhclientV4PID).Return(mockDHClientProcess, nil),
		mockDHClientProcess.EXPECT().Signal(syscall.SIGTERM).Return(errors.New("error")),
	)
	err := dhclient.stopProcess(dhclientV4PID, time.Microsecond, 10*time.Minute)
	assert.Error(t, err)
}

func TestStopProcessKillsProcessWhenWaitTimesout(t *testing.T) {
	ctrl, _, _, _, _, _, mockOS := setup(t)
	defer ctrl.Finish()

	dhclient := &dhclient{os: mockOS}
	mockDHClientProcess := mock_oswrapper.NewMockOSProcess(ctrl)

	gomock.InOrder(
		mockOS.EXPECT().FindProcess(dhclientV4PID).Return(mockDHClientProcess, nil),
		mockDHClientProcess.EXPECT().Signal(syscall.SIGTERM).Return(nil),
	)

	// These are unordered because the ticker might or might not kick in
	// during test execution. We have that code path covered in previous
	// tests anyway. The important thing is that Signal(SIGKILL) is invoked
	mockOS.EXPECT().FindProcess(dhclientV4PID).Return(mockDHClientProcess, nil).AnyTimes()
	mockDHClientProcess.EXPECT().Signal(syscall.Signal(0)).Return(nil).AnyTimes()

	mockDHClientProcess.EXPECT().Signal(syscall.SIGKILL).Return(nil)
	err := dhclient.stopProcess(dhclientV4PID, time.Microsecond, 10*time.Microsecond)
	assert.NoError(t, err)
}

func TestStopProcessReturnsErrorWhenSIGKILLSignalFails(t *testing.T) {
	ctrl, _, _, _, _, _, mockOS := setup(t)
	defer ctrl.Finish()

	dhclient := &dhclient{os: mockOS}
	mockDHClientProcess := mock_oswrapper.NewMockOSProcess(ctrl)

	gomock.InOrder(
		mockOS.EXPECT().FindProcess(dhclientV4PID).Return(mockDHClientProcess, nil),
		mockDHClientProcess.EXPECT().Signal(syscall.SIGTERM).Return(nil),
	)

	// These are unordered because the ticker might or might not kick in
	// during test execution. We have that code path covered in previous
	// tests anyway. The important thing is that Signal(SIGKILL) is invoked
	mockOS.EXPECT().FindProcess(dhclientV4PID).Return(mockDHClientProcess, nil).AnyTimes()
	mockDHClientProcess.EXPECT().Signal(syscall.Signal(0)).Return(nil).AnyTimes()

	mockDHClientProcess.EXPECT().Signal(syscall.SIGKILL).Return(errors.New("error"))
	err := dhclient.stopProcess(dhclientV4PID, time.Microsecond, 10*time.Microsecond)
	assert.Error(t, err)
}

func TestStopDHClientFailsWhenReadFileReturnsError(t *testing.T) {
	ctrl, _, mockIOUtil, _, _, mockExec, mockOS := setup(t)
	defer ctrl.Finish()

	pidFilePath := dhclientPIDFilePathDefault + "/ns-" + deviceName + "-dhclient4.pid"

	gomock.InOrder(
		mockOS.EXPECT().Getenv(dhclientExecutableNameEnvConfig).Return(""),
		mockOS.EXPECT().Getenv(dhclientLeasesFilePathEnvConfig).Return(""),
		mockOS.EXPECT().Getenv(dhclientPIDFilePathEnvConfig).Return(""),
		mockIOUtil.EXPECT().ReadFile(pidFilePath).Return(nil, errors.New("error")),
	)

	dhclient := newDHClient(mockOS, mockExec, mockIOUtil,
		maxDHClientStartAttempts, durationBetweenDHClientStartRetries)

	err := dhclient.Stop(deviceName, ipRev4, checkDHClientStateInteval, maxDHClientStopWait)
	assert.Error(t, err)
}

func TestStopDHClientFailsWhenReadFileReturnsInvalidPID(t *testing.T) {
	ctrl, _, mockIOUtil, _, _, _, _ := setup(t)
	defer ctrl.Finish()

	dhclient := &dhclient{
		ioutil:      mockIOUtil,
		pidFilePath: dhclientPIDFilePathDefault,
	}
	pidFilePath := dhclientPIDFilePathDefault + "/ns-" + deviceName + "-dhclient4.pid"
	mockIOUtil.EXPECT().ReadFile(pidFilePath).Return([]byte(invalidDHClientPIDFieContents), nil)

	err := dhclient.Stop(deviceName, ipRev4, checkDHClientStateInteval, maxDHClientStopWait)
	assert.Error(t, err)
}

func TestStopDHClientFailsWhenDHClientProcessNotFound(t *testing.T) {
	ctrl, _, mockIOUtil, _, _, _, mockOS := setup(t)
	defer ctrl.Finish()

	dhclient := &dhclient{
		os:          mockOS,
		ioutil:      mockIOUtil,
		pidFilePath: dhclientPIDFilePathDefault,
	}
	pidFilePath := dhclientPIDFilePathDefault + "/ns-" + deviceName + "-dhclient4.pid"

	gomock.InOrder(
		mockIOUtil.EXPECT().ReadFile(pidFilePath).Return([]byte(dhclientV4PIDFileContents), nil),
		mockOS.EXPECT().FindProcess(dhclientV4PID).Return(nil, errors.New("error")),
	)

	err := dhclient.Stop(deviceName, ipRev4, checkDHClientStateInteval, maxDHClientStopWait)
	assert.Error(t, err)
}

func TestStopDHClientRunFailsWhenDHClientProcessCannotBeKilled(t *testing.T) {
	ctrl, _, mockIOUtil, _, _, _, mockOS := setup(t)
	defer ctrl.Finish()

	dhclient := &dhclient{
		os:          mockOS,
		ioutil:      mockIOUtil,
		pidFilePath: dhclientPIDFilePathDefault,
	}
	pidFilePath := dhclientPIDFilePathDefault + "/ns-" + deviceName + "-dhclient4.pid"
	mockDHClientProcess := mock_oswrapper.NewMockOSProcess(ctrl)

	gomock.InOrder(
		mockIOUtil.EXPECT().ReadFile(pidFilePath).Return([]byte(dhclientV4PIDFileContents), nil),
		mockOS.EXPECT().FindProcess(dhclientV4PID).Return(mockDHClientProcess, nil),
		mockDHClientProcess.EXPECT().Signal(syscall.SIGTERM).Return(nil),
	)

	// These are unordered because the ticker might or might not kick in
	// during test execution. We have that code path covered in previous
	// tests anyway. The important thing is that Signal(SIGKILL) is invoked
	mockOS.EXPECT().FindProcess(dhclientV4PID).Return(mockDHClientProcess, nil).AnyTimes()
	mockDHClientProcess.EXPECT().Signal(syscall.Signal(0)).Return(nil).AnyTimes()
	mockDHClientProcess.EXPECT().Signal(syscall.SIGKILL).Return(errors.New("error"))

	err := dhclient.Stop(deviceName, ipRev4, checkDHClientStateInteval, maxDHClientStopWait)
	assert.Error(t, err)
}
