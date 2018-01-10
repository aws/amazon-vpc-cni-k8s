// +build functional
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

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSemver(t *testing.T) {
	testCases := []struct {
		input  string
		output semver
	}{
		{
			input: "1.0.0",
			output: semver{
				major: 1,
				minor: 0,
				patch: 0,
			},
		},
		{
			input: "5.2.3",
			output: semver{
				major: 5,
				minor: 2,
				patch: 3,
			},
		},
		{
			input: "1.0.0-rc-3",
			output: semver{
				major:             1,
				minor:             0,
				patch:             0,
				preReleaseVersion: "rc-3",
			},
		},
		{
			input: "1.0.0+sha.xyz",
			output: semver{
				major:         1,
				minor:         0,
				patch:         0,
				buildMetadata: "sha.xyz",
			},
		},
		{
			input: "1.0.0-rc1+sha.xyz",
			output: semver{
				major:             1,
				minor:             0,
				patch:             0,
				preReleaseVersion: "rc1",
				buildMetadata:     "sha.xyz",
			},
		},
		{
			input: "1.14.0-1.windows.1",
			output: semver{
				major:             1,
				minor:             14,
				patch:             0,
				preReleaseVersion: "1.windows.1",
			},
		},
	}

	for i, testCase := range testCases {
		result, err := parseSemver(testCase.input)
		if err != nil {
			t.Errorf("#%v: Error parsing %v: %v", i, testCase.input, err)
		}
		if result != testCase.output {
			t.Errorf("#%v: Expected %v, got %v", i, testCase.output, result)
		}
	}

	invalidCases := []string{"foo", "", "bar", "1.2", "x.y.z", "1.x.y", "1.1.z"}
	for i, invalidCase := range invalidCases {
		_, err := parseSemver(invalidCase)
		if err == nil {
			t.Errorf("#%v: Expected error, didn't get one. Input: %v", i, invalidCase)
		}
	}
}

func TestVersionMatches(t *testing.T) {
	testCases := []struct {
		version        string
		selector       string
		expectedOutput bool
	}{
		{
			version:        "1.0.0",
			selector:       "1.0.0",
			expectedOutput: true,
		},
		{
			version:        "1.0.0",
			selector:       ">=1.0.0",
			expectedOutput: true,
		},
		{
			version:        "2.1.0",
			selector:       ">=1.0.0",
			expectedOutput: true,
		},
		{
			version:        "2.1.0",
			selector:       "<=1.0.0",
			expectedOutput: false,
		},
		{
			version:        "0.1.0",
			selector:       "<1.0.0",
			expectedOutput: true,
		},
		{
			version:        "0.1.0",
			selector:       "<=1.0.0",
			expectedOutput: true,
		},
		{
			version:        "1.0.0-dev",
			selector:       "<1.0.0",
			expectedOutput: true,
		},
		{
			version:        "1.0.0-alpha",
			selector:       "<1.0.0-beta",
			expectedOutput: true,
		},
		{
			version:        "1.0.0",
			selector:       "<1.1.0",
			expectedOutput: true,
		},
		{
			version:        "1.1.0",
			selector:       "<1.1.1",
			expectedOutput: true,
		},
		{
			version:        "1.1.1",
			selector:       ">1.1.0",
			expectedOutput: true,
		},
		{
			version:        "1.1.0",
			selector:       ">1.0.0",
			expectedOutput: true,
		},
		{
			version:        "1.1.0",
			selector:       "2.1.0,1.1.0",
			expectedOutput: true,
		},
		{
			version:        "2.0.0",
			selector:       "2.1.0,1.1.0",
			expectedOutput: false,
		},
		{
			version:        "1.9.1",
			selector:       ">=1.9.0,<=1.9.1",
			expectedOutput: true,
		},
		{
			version:        "1.14.0-1.windows.1",
			selector:       ">=1.5.0",
			expectedOutput: true,
		},
	}

	for i, testCase := range testCases {
		result, err := Version(testCase.version).Matches(testCase.selector)
		if err != nil {
			t.Errorf("#%v: Unexpected error %v", i, err)
		}
		if result != testCase.expectedOutput {
			t.Errorf("#%v: %v(%v) expected %v but got %v", i, testCase.version, testCase.selector, testCase.expectedOutput, result)
		}
	}
}

func TestExtractVersion(t *testing.T) {
	testCases := []struct {
		version        string
		expectedOutput string
	}{
		{
			version:        "Amazon ECS v1.0.0",
			expectedOutput: "1.0.0",
		},
		{
			version:        " v1.0.0-test",
			expectedOutput: "1.0.0-test",
		},
		{
			version:        "Amazon ECS v1.2.3-1.test.2",
			expectedOutput: "1.2.3-1.test.2",
		},
		{
			version:        " v1.2.3-1.test.2+build.123",
			expectedOutput: "1.2.3-1.test.2+build.123",
		},
		{
			version:        "",
			expectedOutput: "UNKNOWN",
		},
		{
			version:        " abcd",
			expectedOutput: "UNKNOWN",
		},
	}

	for i, testCase := range testCases {
		result := ExtractVersion(testCase.version)
		assert.Equal(t, testCase.expectedOutput, result, "Test case %d", i)
	}
}
