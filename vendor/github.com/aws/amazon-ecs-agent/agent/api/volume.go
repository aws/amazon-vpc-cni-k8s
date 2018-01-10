// Copyright 2014-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package api

// MountPoint describes the in-container location of a Volume and references
// that Volume by name.
type MountPoint struct {
	SourceVolume  string `json:"sourceVolume"`
	ContainerPath string `json:"containerPath"`
	ReadOnly      bool   `json:"readOnly"`
}

// HostVolume is an interface for something that may be used as the host half of a
// docker volume mount
type HostVolume interface {
	SourcePath() string
}

// FSHostVolume is a simple type of HostVolume which references an arbitrary
// location on the host as the Volume.
type FSHostVolume struct {
	FSSourcePath string `json:"sourcePath"`
}

// SourcePath returns the path on the host filesystem that should be mounted
func (fs *FSHostVolume) SourcePath() string {
	return fs.FSSourcePath
}

// EmptyHostVolume represents a volume without a specified host path
type EmptyHostVolume struct {
	HostPath string `json:"hostPath"`
}

// SourcePath returns the generated host path for the volume
func (e *EmptyHostVolume) SourcePath() string {
	return e.HostPath
}

// VolumeFrom is a volume which references another container as its source.
type VolumeFrom struct {
	SourceContainer string `json:"sourceContainer"`
	ReadOnly        bool   `json:"readOnly"`
}
