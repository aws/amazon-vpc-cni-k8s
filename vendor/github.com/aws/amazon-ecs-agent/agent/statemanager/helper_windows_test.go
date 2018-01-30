// +build windows

// Copyright 2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package statemanager_test

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows/registry"
)

func setupWindowsTest(path string) (func(), error) {
	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, `SOFTWARE\Amazon\ECS Agent\State File`, registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if err != nil {
		return nil, err
	}
	defer key.Close()
	valueName := fmt.Sprintf("test_path_%d", time.Now().UnixNano())
	os.Setenv("ZZZ_I_KNOW_SETTING_TEST_VALUES_IN_PRODUCTION_IS_NOT_SUPPORTED", valueName)
	err = key.SetStringValue(valueName, path)
	if err != nil {
		return nil, err
	}
	cleanup := func() {
		os.Unsetenv("ZZZ_I_KNOW_SETTING_TEST_VALUES_IN_PRODUCTION_IS_NOT_SUPPORTED")
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Amazon\ECS Agent\State File`, registry.ALL_ACCESS)
		if err != nil {
			return
		}
		key.DeleteValue(valueName)
	}
	return cleanup, nil
}
