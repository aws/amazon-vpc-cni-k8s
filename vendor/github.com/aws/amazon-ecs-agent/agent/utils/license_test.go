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

package utils

import (
	"errors"
	"strings"
	"testing"
)

func TestGetTextSuccess(t *testing.T) {
	readFile = func(name string) ([]byte, error) {
		return []byte(name), nil
	}

	provider := licenseProvider{}
	text, err := provider.GetText()

	if err != nil {
		t.Error("Did not expect error", err)
	}

	expectedText := "LICENSE\n" + strings.Repeat("=", 80) + "\nNOTICE"
	if text != expectedText {
		t.Errorf("Expected %s but was %s", expectedText, text)
	}
}

func TestGetTextErrorLicense(t *testing.T) {
	readFile = func(name string) ([]byte, error) {
		return nil, errors.New("test error")
	}

	provider := licenseProvider{}
	_, err := provider.GetText()

	if err == nil {
		t.Error("Expected error but was nil")
	}
}

func TestGetTextErrorNotice(t *testing.T) {
	readFile = func(name string) ([]byte, error) {
		if name == "LICENSE" {
			return nil, nil
		}
		return nil, errors.New("test error")
	}

	provider := licenseProvider{}
	_, err := provider.GetText()

	if err == nil {
		t.Error("Expected error but was nil")
	}
}
