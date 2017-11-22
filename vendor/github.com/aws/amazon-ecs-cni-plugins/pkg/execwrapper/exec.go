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

package execwrapper

import (
	"io"
	"os/exec"
)

// Cmd wraps methods provided by the exec.Cmd struct
type Cmd interface {
	CombinedOutput() ([]byte, error)
	Output() ([]byte, error)
	Run() error
	Start() error
	StderrPipe() (io.ReadCloser, error)
	StdinPipe() (io.WriteCloser, error)
	StdoutPipe() (io.ReadCloser, error)
	Wait() error
}

// Exec wraps methods provided by the os/exec package
type Exec interface {
	LookPath(file string) (string, error)
	Command(name string, arg ...string) Cmd
}

// NewExec creates a new Exec object
func NewExec() Exec {
	return &_exec{}
}

type _exec struct {
}

func (*_exec) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (*_exec) Command(name string, arg ...string) Cmd {
	return exec.Command(name, arg...)
}
