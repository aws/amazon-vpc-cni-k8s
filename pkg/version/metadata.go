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
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const (
	metadataSchemaVersion = 1
	metadataComponent     = "aws-vpc-cni"
)

type metadata struct {
	SchemaVersion int    `json:"schemaVersion"`
	Component     string `json:"component"`
	Version       string `json:"version"`
	GitCommit     string `json:"gitCommit"`
	GoVersion     string `json:"goVersion"`
	Platform      string `json:"platform"`
}

type metadataWriter func(string) error

// PublishMetadataAsync starts one best-effort metadata publication attempt.
// Publication never delays IPAMD startup.
func PublishMetadataAsync(path string, errorOutput io.Writer) {
	publishMetadataAsync(path, errorOutput, writeMetadata)
}

func publishMetadataAsync(path string, errorOutput io.Writer, writer metadataWriter) {
	go func() {
		if err := writer(path); err != nil {
			_, _ = fmt.Fprintf(errorOutput, "warning: failed to publish AWS VPC CNI metadata: %v\n", err)
		}
	}()
}

func writeMetadata(path string) error {
	data, err := marshalMetadata()
	if err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create metadata temporary file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}()

	bytesWritten, err := tempFile.Write(data)
	if err != nil {
		return fmt.Errorf("write metadata temporary file: %w", err)
	}
	if bytesWritten != len(data) {
		return fmt.Errorf("write metadata temporary file: wrote %d of %d bytes", bytesWritten, len(data))
	}
	if err := tempFile.Chmod(0o644); err != nil {
		return fmt.Errorf("set metadata file mode: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close metadata temporary file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace metadata file: %w", err)
	}
	return nil
}

func marshalMetadata() ([]byte, error) {
	record := metadata{
		SchemaVersion: metadataSchemaVersion,
		Component:     metadataComponent,
		Version:       valueOrUnknown(Version),
		GitCommit:     valueOrUnknown(GitCommit),
		GoVersion:     valueOrUnknown(GoVersion),
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}
	data = append(data, '\n')
	return data, nil
}

func valueOrUnknown(value string) string {
	return cmp.Or(value, "unknown")
}
