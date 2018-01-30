// +build windows

// Copyright 2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package dependencies

import (
	"io"
	"io/ioutil"
	"os"

	"golang.org/x/sys/windows/registry"
)

//go:generate go run ../../../scripts/generate/mockgen.go github.com/aws/amazon-ecs-agent/agent/statemanager/dependencies WindowsRegistry,RegistryKey,FS,File mocks/mocks_windows.go

// WindowsRegistry is an interface for the package-level methods in the golang.org/x/sys/windows/registry package
type WindowsRegistry interface {
	CreateKey(k registry.Key, path string, access uint32) (newk RegistryKey, openedExisting bool, err error)
	OpenKey(k registry.Key, path string, access uint32) (RegistryKey, error)
}

// RegistryKey is an interface for the registry.Key type
type RegistryKey interface {
	GetStringValue(name string) (val string, valtype uint32, err error)
	SetStringValue(name, value string) error
	Close() error
}

// FS is an interface for file-related operations in the os and ioutil packages
type FS interface {
	Open(name string) (File, error)
	IsNotExist(err error) bool
	ReadAll(f File) ([]byte, error)
	TempFile(dir, prefix string) (File, error)
	Remove(name string) error
}

// File is an interface for the os.File type
type File interface {
	Write(b []byte) (int, error)
	Sync() error
	Close() error
	Name() string
}

type StdRegistry struct{}

func (StdRegistry) CreateKey(k registry.Key, path string, access uint32) (RegistryKey, bool, error) {
	return registry.CreateKey(k, path, access)
}

func (StdRegistry) OpenKey(k registry.Key, path string, access uint32) (RegistryKey, error) {
	return registry.OpenKey(k, path, access)
}

type StdFS struct{}

func (StdFS) Open(name string) (File, error) {
	return os.Open(name)
}

func (StdFS) IsNotExist(err error) bool {
	return os.IsNotExist(err)
}

func (StdFS) ReadAll(f File) ([]byte, error) {
	reader := f.(io.Reader)
	return ioutil.ReadAll(reader)
}

func (StdFS) TempFile(dir, prefix string) (File, error) {
	return ioutil.TempFile(dir, prefix)
}

func (StdFS) Remove(name string) error {
	return os.Remove(name)
}
