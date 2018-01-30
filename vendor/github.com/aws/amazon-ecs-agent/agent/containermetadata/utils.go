// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package containermetadata

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	metadataJoinSuffix = "metadata"
)

// getTaskIDfromARN parses a task ARN and produces the task ID
// A task ARN has format arn:aws:ecs:[region]:[account-id]:task/[task-id]
// For a correctly formatted ARN we split it over ":" into 6 parts, the last part
// containing the task-id which we extract by splitting it by "/".
// This function should be eventually be replaced by a standardized ARN parsing
// module
func getTaskIDfromARN(taskARN string) (string, error) {
	colonSplitARN := strings.SplitN(taskARN, ":", 6)
	// Incorrectly formatted ARN (Should not happen)
	if len(colonSplitARN) < 6 {
		return "", fmt.Errorf("get task ARN: invalid TaskARN %s", taskARN)
	}
	arnTaskPartSplit := strings.SplitN(colonSplitARN[5], "/", 2)
	// Incorrectly formatted ARN (Should not happen)
	if len(arnTaskPartSplit) < 2 {
		return "", fmt.Errorf("get task ARN: cannot find TaskID for TaskARN %s", taskARN)
	}
	return arnTaskPartSplit[1], nil
}

// getMetadataFilePath gives the metadata file path for any agent-managed container
func getMetadataFilePath(taskARN string, containerName string, dataDir string) (string, error) {
	taskID, err := getTaskIDfromARN(taskARN)
	// Empty task ID indicates malformed ARN (Should not happen)
	if err != nil {
		return "", fmt.Errorf("get metdata file path of task %s container %s: %v", taskARN, containerName, err)
	}
	return filepath.Join(dataDir, metadataJoinSuffix, taskID, containerName), nil
}

// getTaskMetadataDir acquires the directory with all of the metadata
// files of a given task
func getTaskMetadataDir(taskARN string, dataDir string) (string, error) {
	taskID, err := getTaskIDfromARN(taskARN)
	if err != nil {
		fmt.Errorf("get task metadata directory: %v", err)
	}
	return filepath.Join(dataDir, metadataJoinSuffix, taskID), err
}
