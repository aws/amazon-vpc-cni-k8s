// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//      http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	projectVendor         = `github.com/aws/amazon-vpc-cni-k8s/vendor`
	copyrightHeaderFormat = "// Copyright %v Amazon.com, Inc. or its affiliates. All Rights Reserved."
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

// TODO detect if mockgen or goimports are in the path

func main() {
	if len(os.Args) != 4 {
		usage()
		os.Exit(1)
	}
	packageName := os.Args[1]
	interfaces := os.Args[2]
	outputPath := os.Args[3]
	re := regexp.MustCompile("(?m)[\r\n]+^.*" + projectVendor + ".*$")

	copyrightHeader := fmt.Sprintf(copyrightHeaderFormat, time.Now().Year())

	path, _ := filepath.Split(outputPath)
	err := os.MkdirAll(path, os.ModeDir|0755)
	if err != nil {
		printErrorAndExitWithErrorCode(err)
	}

	outputFile, err := os.Create(outputPath)
	if err != nil {
		printErrorAndExitWithErrorCode(err)
	}
	defer outputFile.Close()

	mockgen := exec.Command("mockgen", packageName, interfaces)
	mockgenOut, err := mockgen.Output()
	if err != nil {
		printErrorAndExitWithErrorCode(
			fmt.Errorf("Error running mockgen for package '%s' and interfaces '%s': %v\n", packageName, interfaces, err))
	}

	sanitized := re.ReplaceAllString(string(mockgenOut), "")

	withHeader := copyrightHeader + licenseBlock + sanitized

	goimports := exec.Command("goimports")
	goimports.Stdin = bytes.NewBufferString(withHeader)
	goimports.Stdout = outputFile
	err = goimports.Run()
	if err != nil {
		printErrorAndExitWithErrorCode(err)
	}

	scanner := bufio.NewScanner(outputFile)
	mockFileContents := scanner.Bytes()
	sanitizedMockFileContents := strings.Replace(string(mockFileContents), projectVendor, "", 1)

	writer := bufio.NewWriter(outputFile)
	_, err = writer.WriteString(sanitizedMockFileContents)
	if err != nil {
		printErrorAndExitWithErrorCode(err)
	}

}

func usage() {
	fmt.Println(os.Args[0], " PACKAGE INTERFACE_NAMES OUTPUT_FILE")
}

func printErrorAndExitWithErrorCode(err error) {
	fmt.Println(err)
	os.Exit(1)
}
