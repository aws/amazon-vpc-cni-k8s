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

package iptableswrapper

import (
	"fmt"
	"os/exec"
	"testing"
)

func TestGetIptablesMode(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		cmdErr      error
		wantMode    string
		wantErr     bool
	}{
		{
			name:     "iptables-nft",
			output:   "iptables v1.8.8 (nf_tables)",
			wantMode: "nf_tables",
			wantErr:  false,
		},
		{
			name:     "iptables-legacy",
			output:   "iptables v1.8.8 (legacy)",
			wantMode: "legacy",
			wantErr:  false,
		},
		{
			name:     "iptables-legacy implicitly",
			output:   "iptables v1.6.1",
			wantMode: "legacy",
			wantErr:  false,
		},
		{
			name:     "iptables-nft different version",
			output:   "iptables v1.8.4 (nf_tables)",
			wantMode: "nf_tables",
			wantErr:  false,
		},
		{
			name:    "command execution error",
			cmdErr:  fmt.Errorf("command not found"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mocked exec.Command
			oldExecCommand := iptablesModeExecCommand
			defer func() { iptablesModeExecCommand = oldExecCommand }()
			
			iptablesModeExecCommand = func(name string, args ...string) *exec.Cmd {
				return mockCommand(tt.output, tt.cmdErr)
			}

			mode, err := GetIptablesMode()

			if (err != nil) != tt.wantErr {
				t.Errorf("GetIptablesMode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && mode != tt.wantMode {
				t.Errorf("GetIptablesMode() = %v, want %v", mode, tt.wantMode)
			}
		})
	}
}

func mockCommand(output string, err error) *exec.Cmd {
	cmd := exec.Command("echo", output)
	if err != nil {
		cmd = exec.Command("false")
	}
	return cmd
}
