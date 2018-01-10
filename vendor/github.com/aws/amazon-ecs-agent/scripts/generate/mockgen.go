// Copyright 2016-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	projectVendor         = `github.com/aws/amazon-ecs-agent/agent/vendor/`
	copyrightHeaderFormat = "// Copyright 2015-%v Amazon.com, Inc. or its affiliates. All Rights Reserved."
	licenseBlock          = `
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

  `
)

func main() {
	if len(os.Args) != 4 {
		usage()
		os.Exit(1)
	}
	packageName := os.Args[1]
	interfaces := os.Args[2]
	outputPath := os.Args[3]

	err := generateMocks(packageName, interfaces, outputPath)
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok {
			// Prevents swallowing CLI program errors that come
			// from running mockgen and goimports
			// https://golang.org/pkg/os/exec/#ExitError
			fmt.Fprintln(os.Stderr, string(exitErr.Stderr))
		}
		// Print the encapsulating golang error type
		fmt.Println(err)
		os.Exit(1)
	}

}

func generateMocks(packageName string, interfaces string, outputPath string) error {
	copyrightHeader := fmt.Sprintf(copyrightHeaderFormat, time.Now().Year())

	path, _ := filepath.Split(outputPath)
	err := os.MkdirAll(path, os.ModeDir|0755)
	if err != nil {
		return err
	}

	outputFile, err := os.Create(outputPath)
	defer outputFile.Close()
	if err != nil {
		return err
	}

	mockgen := exec.Command("mockgen", packageName, interfaces)
	mockgenOut, err := mockgen.Output()
	if err != nil {
		return err
	}

	sanitized := strings.Replace(string(mockgenOut), projectVendor, "", -1)

	withHeader := copyrightHeader + licenseBlock + sanitized

	goimports := exec.Command("goimports")
	goimports.Stdin = bytes.NewBufferString(withHeader)
	goimports.Stdout = outputFile
	return goimports.Run()
}

func usage() {
	fmt.Println(os.Args[0], " PACKAGE INTERFACE_NAMES OUTPUT_FILE")
}
