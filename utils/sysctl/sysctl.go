// Ref: https://github.com/kubernetes/kubernetes/blob/cb2ea4bf7c029e595f44ee62013c982626fb5bd4/staging/src/k8s.io/component-helpers/node/utils/sysctl/sysctl.go

package sysctl

import (
	"io/ioutil"
	"path"
	"strconv"
	"strings"
)

const (
	sysctlBase = "/proc/sys"
)

// Interface is an injectable interface for running sysctl commands.
type Interface interface {
	// Get returns the value for the specified sysctl setting
	Get(sysctl string) (int, error)
	// Set modifies the specified sysctl flag to the new value
	Set(sysctl string, newVal int) error
}

// New returns a new Interface for accessing sysctl
func New() Interface {
	return &procSysctl{}
}

// procSysctl implements Interface by reading and writing files under /proc/sys
type procSysctl struct {
}

// Get returns the value for the specified sysctl setting
func (*procSysctl) Get(sysctl string) (int, error) {
	data, err := ioutil.ReadFile(path.Join(sysctlBase, sysctl))
	if err != nil {
		return -1, err
	}
	val, err := strconv.Atoi(strings.Trim(string(data), " \n"))
	if err != nil {
		return -1, err
	}
	return val, nil
}

// Set modifies the specified sysctl flag to the new value
func (*procSysctl) Set(sysctl string, newVal int) error {
	return ioutil.WriteFile(path.Join(sysctlBase, sysctl), []byte(strconv.Itoa(newVal)), 0640)
}
