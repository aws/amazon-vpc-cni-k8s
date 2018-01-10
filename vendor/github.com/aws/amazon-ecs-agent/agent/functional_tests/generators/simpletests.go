// +build functional

// Copyright 2014-2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"text/template"

	"golang.org/x/tools/imports"
)

var simpleTestPattern = `
// +build functional,%s

// Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

// Package simpletest is an auto-generated set of tests defined by the json
// descriptions in testdata/simpletests.
//
// This file should not be edited; rather you should edit the generator instead
package simpletest

import (
	"testing"
	"time"
	"os"

	. "github.com/aws/amazon-ecs-agent/agent/functional_tests/util"
)

{{ range $i,$el := $ }}

// Test{{ $el.Name }} {{ $el.Description }}
func Test{{ $el.Name }}(t *testing.T) {
	{{if $el.DockerVersion}}
	// Test only available for docker version {{ $el.DockerVersion }}
	RequireDockerVersion(t, "{{ $el.DockerVersion }}") 
	{{end}}
	// Parallel is opt in because resource constraints could cause test failures
	// on smaller instances
	if os.Getenv("ECS_FUNCTIONAL_PARALLEL") != "" { t.Parallel() }
	agent := RunAgent(t, nil)
	defer agent.Cleanup()
	agent.RequireVersion("{{ $el.Version }}")

	td, err := GetTaskDefinition("{{ $el.TaskDefinition }}")
	if err != nil {
		t.Fatalf("Could not register task definition: %%v", err)
	}
	testTasks, err := agent.StartMultipleTasks(t, td, {{ $el.Count }})
	if err != nil {
		t.Fatalf("Could not start task: %%v", err)
	}
	timeout, err := time.ParseDuration("{{ $el.Timeout }}")
	if err != nil {
		t.Fatalf("Could not parse timeout: %%#v", err)
	}

	{{if $el.Daemon}}
	// Make sure the task is running
	for _, testTask := range testTasks {
		err = testTask.WaitRunning(timeout)
		if err != nil {
			t.Errorf("Timed out waiting for task to reach running. Error %%v, task %%v", err, testTask)
		}
	}

	// Cleanup, stop all the tasks and wait for the containers to be stopped
	for _, testTask := range testTasks {
		err = testTask.Stop()
		if err != nil {
			t.Errorf("Failed to stop task, Error %%v, task %%v", err, testTask)
		}
	}
	{{end}}

	for _, testTask := range testTasks {
		err = testTask.WaitStopped(timeout)
		if err != nil {
			t.Fatalf("Timed out waiting for task to reach stopped. Error %%#v, task %%#v", err, testTask)
		}

		{{ range $name, $code := $el.ExitCodes }}
		if exit, ok := testTask.ContainerExitcode("{{$name}}"); !ok || exit != {{ $code }} {
			t.Errorf("Expected {{$name}} to exit with {{$code}}; actually exited (%%v) with %%v", ok, exit)
		}
		{{ end }}
		defer agent.SweepTask(testTask)
	}

}
{{ end }}
`

func main() {
	type simpleTestMetadata struct {
		Name           string
		Description    string
		TaskDefinition string
		Timeout        string
		ExitCodes      map[string]int
		Tags           []string
		Version        string
		Count          int
		DockerVersion  string
		Daemon         bool
	}

	types := []struct {
		buildTag       string
		testDir        string
		templateName   string
		outputFileName string
	}{{
		buildTag:       "windows",
		testDir:        "simpletests_windows",
		templateName:   "simpleTestWindows",
		outputFileName: "simpletests_generated_windows_test",
	}, {
		buildTag:       "!windows",
		testDir:        "simpletests_unix",
		templateName:   "simpleTestUnix",
		outputFileName: "simpletests_generated_unix_test",
	}}

	for _, ostype := range types {
		_, filename, _, _ := runtime.Caller(0)
		metadataFiles, err := filepath.Glob(filepath.Join(path.Dir(filename), "..", "testdata", ostype.testDir, "*.json"))
		if err != nil || len(metadataFiles) == 0 {
			panic("No tests found" + err.Error())
		}

		testMetadatas := make([]simpleTestMetadata, len(metadataFiles))
		for i, f := range metadataFiles {
			data, err := ioutil.ReadFile(f)
			if err != nil {
				panic("Cannot read file " + f)
			}

			// By default the number of task to run is 1
			testMetadatas[i].Count = 1
			err = json.Unmarshal(data, &testMetadatas[i])
			if err != nil {
				panic("Cannot parse " + f + ": " + err.Error())
			}
		}

		simpleTests := template.Must(template.New(ostype.templateName).Parse(
			fmt.Sprintf(simpleTestPattern, ostype.buildTag),
		))
		output := bytes.NewBuffer([]byte{})
		err = simpleTests.Execute(output, testMetadatas)
		if err != nil {
			panic(err)
		}
		formattedOutput, err := imports.Process("", output.Bytes(), nil)
		if err != nil {
			fmt.Println(string(output.Bytes()))
			panic(err)
		}

		// Add '.go' so the arg can be used with 'go run' as well, without being interpreted as a file to run
		fmt.Println(ostype.testDir + "/" + ostype.outputFileName + ".go")
		outputFile, err := os.Create(filepath.Join(ostype.testDir, ostype.outputFileName+".go"))
		if err != nil {
			panic(err)
		}
		outputFile.Write(formattedOutput)
	}
}
