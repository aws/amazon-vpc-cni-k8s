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

package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRootFrom(t *testing.T) {
	tests := []struct {
		name string
		// repository root, relative to a temp dir
		repo string
	}{
		{name: "upstream checkout", repo: "src/amazon-vpc-cni-k8s"},
		{name: "prow checkout", repo: "repository/vpc-cni"},
		{name: "vpc-cni in a parent directory name", repo: "806020241566-Hydra-vpc-cni-release-al2023-ipv4-1-37-abc/repository/vpc-cni"},
		{name: "checkout with an unrelated name", repo: "build-vpc-cni/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), tt.repo)
			binDir := filepath.Join(root, "test", "integration", "metrics-helper")
			if err := os.MkdirAll(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module "+moduleRoot+"\n\ngo 1.26\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := projectRootFrom(binDir); got != root {
				t.Errorf("projectRootFrom(%q) = %q, want %q", binDir, got, root)
			}
		})
	}
}

func TestProjectRootFromFallsBackWithoutGoMod(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repository", "vpc-cni", "test", "integration", "cni")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := dir[:len(dir)-len("/test/integration/cni")]
	if got := projectRootFrom(dir); got != want {
		t.Errorf("projectRootFrom(%q) = %q, want %q", dir, got, want)
	}
}
