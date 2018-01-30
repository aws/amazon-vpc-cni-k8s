// Copyright 2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
// http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

// Package os provides interfaces around the 'os', 'io', and 'ioutil' functions
// so that may be mocked out appropriately
package os

//go:generate go run ../../../../scripts/generate/mockgen.go github.com/aws/amazon-ecs-agent/agent/acs/update_handler/os FileSystem mock/$GOFILE

import (
	"io"
	"io/ioutil"
	"os"
)

// FileSystem captures related functions from os, io, and io/ioutil packages
type FileSystem interface {
	MkdirAll(path string, perm os.FileMode) error
	TempFile(dir, prefix string) (f *os.File, err error)
	Remove(path string)
	TeeReader(r io.Reader, w io.Writer) io.Reader
	Copy(dst io.Writer, src io.Reader) (written int64, err error)
	Rename(oldpath, newpath string) error
	ReadAll(r io.Reader) ([]byte, error)
	WriteFile(filename string, data []byte, perm os.FileMode) error
	Open(name string) (f io.ReadWriteCloser, err error)
	Create(name string) (f io.ReadWriteCloser, err error)
	Exit(code int)
}

// Std delegates to the go standard library functions
type std struct{}

var Default FileSystem = &std{}

func (s *std) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (s *std) TempFile(dir, prefix string) (*os.File, error) {
	return ioutil.TempFile(dir, prefix)
}

func (s *std) Remove(path string) {
	os.Remove(path)
}

func (s *std) TeeReader(r io.Reader, w io.Writer) io.Reader {
	return io.TeeReader(r, w)
}

func (s *std) Copy(dst io.Writer, src io.Reader) (int64, error) {
	return io.Copy(dst, src)
}

func (s *std) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

func (s *std) ReadAll(r io.Reader) ([]byte, error) {
	return ioutil.ReadAll(r)
}
func (s *std) WriteFile(filename string, data []byte, perm os.FileMode) error {
	return ioutil.WriteFile(filename, data, perm)
}
func (s *std) Open(name string) (io.ReadWriteCloser, error) {
	return os.Open(name)
}

func (s *std) Create(name string) (io.ReadWriteCloser, error) {
	return os.Create(name)
}

func (s *std) Exit(code int) {
	os.Exit(code)
}
