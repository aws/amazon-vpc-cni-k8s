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
	"io/ioutil"
	"strings"
)

type LicenseProvider interface {
	GetText() (string, error)
}

type licenseProvider struct{}

const (
	licenseFile = "LICENSE"
	noticeFile  = "NOTICE"
)

var readFile = ioutil.ReadFile

func NewLicenseProvider() LicenseProvider {
	return &licenseProvider{}
}

func (l *licenseProvider) GetText() (string, error) {
	licenseText, err := readFile(licenseFile)
	if err != nil {
		return "", err
	}

	noticeText, err := readFile(noticeFile)
	if err != nil {
		return "", err
	}

	separator := "\n" + strings.Repeat("=", 80) + "\n"

	return string(licenseText) + separator + string(noticeText), nil
}
