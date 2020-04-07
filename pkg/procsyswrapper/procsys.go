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

package procsyswrapper

import (
	"io/ioutil"
)

type ProcSys interface {
	Get(key string) (string, error)
	Set(key, value string) error
}

type procSys struct {
	prefix string
}

func NewProcSys() ProcSys {
	return &procSys{prefix: "/proc/sys/"}
}

func (p *procSys) path(key string) string {
	return p.prefix + key
}

func (p *procSys) Get(key string) (string, error) {
	data, err := ioutil.ReadFile(p.path(key))
	return string(data), err
}

func (p *procSys) Set(key, value string) error {
	return ioutil.WriteFile(p.path(key), []byte(value), 0644)
}
