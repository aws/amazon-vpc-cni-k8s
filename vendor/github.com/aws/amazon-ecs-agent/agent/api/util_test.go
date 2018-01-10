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

package api

import (
	"reflect"
	"testing"
)

func taskN(n int) *Task {
	return &Task{
		Arn: string(n),
	}
}

func TestRemoveFromTaskArray(t *testing.T) {
	arr := []*Task{taskN(1), taskN(2), taskN(3)}

	arr2 := RemoveFromTaskArray(arr, -1)
	if !reflect.DeepEqual(arr, arr2) {
		t.Error("Index -1 out of bounds altered arr")
	}

	arr2 = RemoveFromTaskArray(arr, 3)
	if !reflect.DeepEqual(arr, arr2) {
		t.Error("Index 3 out of bounds altered arr")
	}

	arr = RemoveFromTaskArray(arr, 2)
	if !reflect.DeepEqual(arr, []*Task{taskN(1), taskN(2)}) {
		t.Error("Last element not removed")
	}
	arr = RemoveFromTaskArray(arr, 0)
	if !reflect.DeepEqual(arr, []*Task{taskN(2)}) {
		t.Error("First element not removed")
	}
	arr = RemoveFromTaskArray(arr, 0)
	if !reflect.DeepEqual(arr, []*Task{}) {
		t.Error("First element not removed")
	}
	arr = RemoveFromTaskArray(arr, 0)
	if !reflect.DeepEqual(arr, []*Task{}) {
		t.Error("Removing from empty arr changed it")
	}
}
