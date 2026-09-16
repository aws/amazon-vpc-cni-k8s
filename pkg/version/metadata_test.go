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
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMarshalMetadata(t *testing.T) {
	setBuildMetadataForTest(t, "v1.2.3", strings.Repeat("a", 40), "2026-09-16T05:00:00Z", "go1.26.6")
	generatedAt := time.Date(2026, time.September, 16, 6, 0, 0, 0, time.FixedZone("test", 2*60*60))

	data, err := marshalMetadata(generatedAt)
	if err != nil {
		t.Fatalf("marshalMetadata() error = %v", err)
	}

	expected := "{\n" +
		"  \"schemaVersion\": 1,\n" +
		"  \"component\": \"aws-vpc-cni\",\n" +
		"  \"version\": \"v1.2.3\",\n" +
		"  \"gitCommit\": \"" + strings.Repeat("a", 40) + "\",\n" +
		"  \"buildDate\": \"2026-09-16T05:00:00Z\",\n" +
		"  \"goVersion\": \"go1.26.6\",\n" +
		"  \"platform\": \"" + runtime.GOOS + "/" + runtime.GOARCH + "\",\n" +
		"  \"generatedAt\": \"2026-09-16T04:00:00Z\"\n" +
		"}\n"
	if string(data) != expected {
		t.Fatalf("marshalMetadata() = %q, want %q", data, expected)
	}
}

func TestMarshalMetadataUsesUnknownFallbacks(t *testing.T) {
	setBuildMetadataForTest(t, "", "", "", "")

	data, err := marshalMetadata(time.Unix(0, 0))
	if err != nil {
		t.Fatalf("marshalMetadata() error = %v", err)
	}

	var record metadata
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if record.Version != "unknown" || record.GitCommit != "unknown" || record.BuildDate != "unknown" || record.GoVersion != "unknown" {
		t.Fatalf("metadata fallbacks = %#v, want unknown build values", record)
	}
}

func TestMarshalMetadataRejectsOversizedRecord(t *testing.T) {
	setBuildMetadataForTest(t, strings.Repeat("x", maxMetadataSize), "commit", "date", "go")

	if _, err := marshalMetadata(time.Unix(0, 0)); err == nil {
		t.Fatal("marshalMetadata() error = nil, want size error")
	}
}

func TestWriteMetadataAtCreatesAndReplacesFile(t *testing.T) {
	setBuildMetadataForTest(t, "v1.2.3", strings.Repeat("b", 40), "2026-09-16T05:00:00Z", "go1.26.6")
	path := filepath.Join(t.TempDir(), "aws-vpc-cni-metadata.json")

	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	if err := writeMetadataAt(path, time.Unix(1, 0)); err != nil {
		t.Fatalf("writeMetadataAt() error = %v", err)
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

func TestWriteMetadataAtPreservesDestinationOnRenameFailure(t *testing.T) {
	setBuildMetadataForTest(t, "v1.2.3", "commit", "date", "go")
	dir := t.TempDir()
	path := filepath.Join(dir, "aws-vpc-cni-metadata.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("os.Mkdir() error = %v", err)
	}

	if err := writeMetadataAt(path, time.Unix(1, 0)); err == nil {
		t.Fatal("writeMetadataAt() error = nil, want rename error")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("destination is not preserved: mode = %v", info.Mode())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory entries = %d, want only preserved destination", len(entries))
	}
}

func TestWriteMetadataAtRequiresExistingParent(t *testing.T) {
	setBuildMetadataForTest(t, "v1.2.3", "commit", "date", "go")
	path := filepath.Join(t.TempDir(), "missing", "aws-vpc-cni-metadata.json")

	if err := writeMetadataAt(path, time.Unix(1, 0)); err == nil {
		t.Fatal("writeMetadataAt() error = nil, want missing parent error")
	}
}

func TestPublishMetadataAsyncDoesNotWaitForWriter(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	publishMetadataAsync("unused", os.Stderr, func(string) error {
		close(started)
		<-release
		return nil
	})

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("metadata writer did not start")
	}
	close(release)
}

func TestPublishMetadataAsyncWritesFailureToErrorOutput(t *testing.T) {
	output := make(chan string, 1)
	publishMetadataAsync("unused", channelWriter(output), func(string) error {
		return errors.New("read-only filesystem")
	})

	select {
	case warning := <-output:
		expected := "warning: failed to publish AWS VPC CNI metadata: read-only filesystem\n"
		if warning != expected {
			t.Fatalf("warning = %q, want %q", warning, expected)
		}
	case <-time.After(time.Second):
		t.Fatal("metadata warning was not written")
	}
}

type channelWriter chan<- string

func (writer channelWriter) Write(data []byte) (int, error) {
	writer <- string(data)
	return len(data), nil
}

func setBuildMetadataForTest(t *testing.T, version, commit, buildDate, goVersion string) {
	t.Helper()

	originalVersion := Version
	originalCommit := GitCommit
	originalBuildDate := BuildDate
	originalGoVersion := GoVersion
	t.Cleanup(func() {
		Version = originalVersion
		GitCommit = originalCommit
		BuildDate = originalBuildDate
		GoVersion = originalGoVersion
	})

	Version = version
	GitCommit = commit
	BuildDate = buildDate
	GoVersion = goVersion
}
