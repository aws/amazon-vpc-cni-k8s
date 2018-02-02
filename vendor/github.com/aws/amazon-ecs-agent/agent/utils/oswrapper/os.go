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

package oswrapper

import "os"

// OS wraps methods from the 'os' package for testing
type OS interface {
	Create(string) (File, error)
	OpenFile(string, int, os.FileMode) (File, error)
	Rename(string, string) error
	MkdirAll(string, os.FileMode) error
	RemoveAll(string) error
	IsNotExist(error) bool
}

// File wraps methods for os.File type
type File interface {
	Name() string
	Close() error
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Sync() error
}

type _os struct {
}

func NewOS() OS {
	return &_os{}
}

func (*_os) Create(name string) (File, error) {
	return os.Create(name)
}

func (*_os) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	return os.OpenFile(name, flag, perm)
}

func (*_os) Rename(name1 string, name2 string) error {
	return os.Rename(name1, name2)
}

func (*_os) MkdirAll(name string, perm os.FileMode) error {
	return os.MkdirAll(name, perm)
}

func (*_os) RemoveAll(name string) error {
	return os.RemoveAll(name)
}

func (*_os) IsNotExist(err error) bool {
	return os.IsNotExist(err)
}
