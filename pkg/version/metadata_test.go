// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You
// may not use this file except in compliance with the License. A copy of
// the License is located at
//
//       http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is
// distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF
// ANY KIND, either express or implied. See the License for the specific
// language governing permissions and limitations under the License.

package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMarshalMetadata(t *testing.T) {
	commit := strings.Repeat("a", 40)
	tests := []struct {
		name      string
		version   string
		commit    string
		goVersion string
		expected  string
	}{
		{
			name:      "populated",
			version:   "v1.2.3",
			commit:    commit,
			goVersion: "go1.26.6",
			expected: "{\n" +
				"  \"schemaVersion\": 1,\n" +
				"  \"component\": \"aws-vpc-cni\",\n" +
				"  \"version\": \"v1.2.3\",\n" +
				"  \"gitCommit\": \"" + commit + "\",\n" +
				"  \"goVersion\": \"go1.26.6\",\n" +
				"  \"platform\": \"" + runtime.GOOS + "/" + runtime.GOARCH + "\"\n" +
				"}\n",
		},
		{
			name: "unknown fallbacks",
			expected: "{\n" +
				"  \"schemaVersion\": 1,\n" +
				"  \"component\": \"aws-vpc-cni\",\n" +
				"  \"version\": \"unknown\",\n" +
				"  \"gitCommit\": \"unknown\",\n" +
				"  \"goVersion\": \"unknown\",\n" +
				"  \"platform\": \"" + runtime.GOOS + "/" + runtime.GOARCH + "\"\n" +
				"}\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setBuildMetadataForTest(t, test.version, test.commit, test.goVersion)

			data, err := marshalMetadata()
			if err != nil {
				t.Fatalf("marshalMetadata() error = %v", err)
			}
			if string(data) != test.expected {
				t.Fatalf("marshalMetadata() = %q, want %q", data, test.expected)
			}
		})
	}
}

func TestWriteMetadataCreatesAndReplacesFile(t *testing.T) {
	setBuildMetadataForTest(t, "v1.2.3", strings.Repeat("b", 40), "go1.26.6")
	path := filepath.Join(t.TempDir(), "aws-vpc-cni-metadata.json")

	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	if err := writeMetadata(path); err != nil {
		t.Fatalf("writeMetadata() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if !json.Valid(data) || data[len(data)-1] != '\n' {
		t.Fatalf("metadata file = %q, want valid JSON with trailing newline", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("metadata mode = %o, want 0644", info.Mode().Perm())
	}
}

func TestPublishMetadataAsyncDoesNotWaitForWriter(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan struct{})
	defer close(release)

	go func() {
		publishMetadataAsync("unused", os.Stderr, func(string) error {
			close(started)
			<-release
			return nil
		})
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("metadata publication did not return")
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("metadata writer did not start")
	}
}

func setBuildMetadataForTest(t *testing.T, version, commit, goVersion string) {
	t.Helper()

	originalVersion := Version
	originalCommit := GitCommit
	originalGoVersion := GoVersion
	t.Cleanup(func() {
		Version = originalVersion
		GitCommit = originalCommit
		GoVersion = originalGoVersion
	})

	Version = version
	GitCommit = commit
	GoVersion = goVersion
}
