// +build windows

// Copyright 2014-2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.
package statemanager

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows/registry"

	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/statemanager/dependencies/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func setup(t *testing.T, options ...Option) (
	*mock_dependencies.MockWindowsRegistry,
	*mock_dependencies.MockRegistryKey,
	*mock_dependencies.MockFS,
	*mock_dependencies.MockFile,
	StateManager, func()) {
	ctrl := gomock.NewController(t)
	mockRegistry := mock_dependencies.NewMockWindowsRegistry(ctrl)
	mockKey := mock_dependencies.NewMockRegistryKey(ctrl)
	mockFS := mock_dependencies.NewMockFS(ctrl)
	mockFile := mock_dependencies.NewMockFile(ctrl)

	// TODO set this to platform-specific tmpdir
	tmpDir, err := ioutil.TempDir("", "ecs_statemanager_test")
	assert.Nil(t, err)
	cleanup := func() {
		ctrl.Finish()
		os.RemoveAll(tmpDir)
	}
	cfg := &config.Config{DataDir: tmpDir}
	manager, err := NewStateManager(cfg, options...)
	assert.Nil(t, err)
	basicManager := manager.(*basicStateManager)
	basicManager.platformDependencies = windowsDependencies{
		registry: mockRegistry,
		fs:       mockFS,
	}
	return mockRegistry, mockKey, mockFS, mockFile, basicManager, cleanup
}

func TestStateManagerLoadNoRegistryKey(t *testing.T) {
	mockRegistry, _, _, _, manager, cleanup := setup(t)
	defer cleanup()

	// TODO figure out why gomock does not like registry.READ as the mode
	mockRegistry.EXPECT().OpenKey(ecsDataFileRootKey, ecsDataFileKeyPath, gomock.Any()).Return(nil, registry.ErrNotExist)

	err := manager.Load()
	assert.Nil(t, err, "Expected loading a non-existant file to not be an error")
}

func TestStateManagerLoadNoFile(t *testing.T) {
	mockRegistry, mockKey, mockFS, _, manager, cleanup := setup(t)
	defer cleanup()

	// TODO figure out why gomock does not like registry.READ as the mode
	mockRegistry.EXPECT().OpenKey(ecsDataFileRootKey, ecsDataFileKeyPath, gomock.Any()).Return(mockKey, nil)
	mockKey.EXPECT().GetStringValue(ecsDataFileValueName).Return(`C:\data.json`, uint32(0), nil)
	mockKey.EXPECT().Close()
	mockFS.EXPECT().Open(`C:\data.json`).Return(nil, errors.New("test error"))
	mockFS.EXPECT().IsNotExist(gomock.Any()).Return(true)

	err := manager.Load()
	assert.Nil(t, err, "Expected loading a non-existant file to not be an error")
}

func TestStateManagerLoadError(t *testing.T) {
	mockRegistry, mockKey, mockFS, _, manager, cleanup := setup(t)
	defer cleanup()

	// TODO figure out why gomock does not like registry.READ as the mode
	testError := errors.New("test error")
	mockRegistry.EXPECT().OpenKey(ecsDataFileRootKey, ecsDataFileKeyPath, gomock.Any()).Return(mockKey, nil)
	mockKey.EXPECT().GetStringValue(ecsDataFileValueName).Return(`C:\data.json`, uint32(0), nil)
	mockKey.EXPECT().Close()
	mockFS.EXPECT().Open(`C:\data.json`).Return(nil, testError)
	mockFS.EXPECT().IsNotExist(testError).Return(false)

	err := manager.Load()
	assert.Equal(t, testError, err, "Expected error opening file to be an error")
}

func TestStateManagerLoadState(t *testing.T) {
	containerInstanceArn := ""
	mockRegistry, mockKey, mockFS, mockFile, manager, cleanup := setup(t, AddSaveable("ContainerInstanceArn", &containerInstanceArn))
	defer cleanup()

	data := `{"Version":1,"Data":{"ContainerInstanceArn":"foo"}}`
	// TODO figure out why gomock does not like registry.READ as the mode
	mockRegistry.EXPECT().OpenKey(ecsDataFileRootKey, ecsDataFileKeyPath, gomock.Any()).Return(mockKey, nil)
	mockKey.EXPECT().GetStringValue(ecsDataFileValueName).Return(`C:\data.json`, uint32(0), nil)
	mockKey.EXPECT().Close()
	mockFS.EXPECT().Open(`C:\data.json`).Return(mockFile, nil)
	mockFS.EXPECT().ReadAll(mockFile).Return([]byte(data), nil)
	mockFile.EXPECT().Close()

	err := manager.Load()
	assert.Nil(t, err, "Expected loading correctly")
	assert.Equal(t, "foo", containerInstanceArn)
}

func TestStateManagerLoadV1Data(t *testing.T) {
	var containerInstanceArn, cluster, savedInstanceID string
	var sequenceNumber int64
	mockRegistry, mockKey, mockFS, _, manager, cleanup := setup(t,
		AddSaveable("ContainerInstanceArn", &containerInstanceArn),
		AddSaveable("Cluster", &cluster),
		AddSaveable("EC2InstanceID", &savedInstanceID),
		AddSaveable("SeqNum", &sequenceNumber))
	defer cleanup()
	dataFile, err := os.Open(filepath.Join(".", "testdata", "v1", "1", ecsDataFile))
	assert.Nil(t, err, "Error opening test data")
	defer dataFile.Close()

	// TODO figure out why gomock does not like registry.READ as the mode
	mockRegistry.EXPECT().OpenKey(ecsDataFileRootKey, ecsDataFileKeyPath, gomock.Any()).Return(mockKey, nil)
	mockKey.EXPECT().GetStringValue(ecsDataFileValueName).Return(`C:\data.json`, uint32(0), nil)
	mockKey.EXPECT().Close()
	mockFS.EXPECT().Open(`C:\data.json`).Return(dataFile, nil)
	mockFS.EXPECT().ReadAll(dataFile).Return(ioutil.ReadAll(dataFile))

	err = manager.Load()
	assert.Nil(t, err, "Error loading state")
	assert.Equal(t, "test", cluster, "Wrong cluster")
	assert.Equal(t, int64(0), sequenceNumber, "v1 should give a sequence number of 0")
	assert.Equal(t, "arn:aws:ecs:us-west-2:1234567890:container-instance/a9f8e650-e66e-466d-9b0e-3cbce3ba5245", containerInstanceArn)
	assert.Equal(t, "i-00000000", savedInstanceID)
}

func TestStateManagerSaveCreateFileError(t *testing.T) {
	mockRegistry, mockKey, mockFS, _, manager, cleanup := setup(t)
	defer cleanup()

	testError := errors.New("test error")
	basicManager := manager.(*basicStateManager)
	// TODO figure out why gomock does not like registry.READ as the mode
	gomock.InOrder(
		mockRegistry.EXPECT().OpenKey(ecsDataFileRootKey, ecsDataFileKeyPath, gomock.Any()).Return(mockKey, nil),
		mockKey.EXPECT().GetStringValue(ecsDataFileValueName).Return(`C:\data.json`, uint32(0), nil),
		mockKey.EXPECT().Close(),
		mockFS.EXPECT().TempFile(basicManager.statePath, ecsDataFile).Return(nil, testError),
	)
	err := manager.Save()
	assert.Equal(t, testError, err, "expected error creating file")
}

func TestStateManagerSaveSyncFileError(t *testing.T) {
	mockRegistry, mockKey, mockFS, mockFile, manager, cleanup := setup(t)
	defer cleanup()

	testError := errors.New("test error")
	basicManager := manager.(*basicStateManager)
	// TODO figure out why gomock does not like registry.READ as the mode
	gomock.InOrder(
		mockRegistry.EXPECT().OpenKey(ecsDataFileRootKey, ecsDataFileKeyPath, gomock.Any()).Return(mockKey, nil),
		mockKey.EXPECT().GetStringValue(ecsDataFileValueName).Return(`C:\data.json`, uint32(0), nil),
		mockKey.EXPECT().Close(),
		mockFS.EXPECT().TempFile(basicManager.statePath, ecsDataFile).Return(mockFile, nil),
		mockFile.EXPECT().Write(gomock.Any()),
		mockFile.EXPECT().Sync().Return(testError),
		mockFile.EXPECT().Name(),
		mockFile.EXPECT().Close(),
	)
	err := manager.Save()
	assert.Equal(t, testError, err, "expected error creating file")
}

func TestStateManagerSave(t *testing.T) {
	mockRegistry, mockKey, mockFS, mockFile, manager, cleanup := setup(t)
	defer cleanup()

	basicManager := manager.(*basicStateManager)
	// TODO figure out why gomock does not like registry.READ as the mode
	gomock.InOrder(
		mockRegistry.EXPECT().OpenKey(ecsDataFileRootKey, ecsDataFileKeyPath, gomock.Any()).Return(mockKey, nil),
		mockKey.EXPECT().GetStringValue(ecsDataFileValueName).Return(`C:\old.json`, uint32(0), nil),
		mockKey.EXPECT().Close(),
		mockFS.EXPECT().TempFile(basicManager.statePath, ecsDataFile).Return(mockFile, nil),
		mockFile.EXPECT().Write(gomock.Any()),
		mockFile.EXPECT().Sync(),
		mockFile.EXPECT().Close(),
		mockFile.EXPECT().Name().Return(`C:\new.json`),
		mockRegistry.EXPECT().CreateKey(ecsDataFileRootKey, ecsDataFileKeyPath, gomock.Any()).Return(mockKey, false, nil),
		mockKey.EXPECT().SetStringValue(ecsDataFileValueName, `C:\new.json`),
		mockKey.EXPECT().Close(),
		mockFS.EXPECT().Remove(`C:\old.json`),
		mockFile.EXPECT().Close(),
	)
	err := manager.Save()
	assert.Nil(t, err)
}

// TODO TestStateManagerSave + errors
