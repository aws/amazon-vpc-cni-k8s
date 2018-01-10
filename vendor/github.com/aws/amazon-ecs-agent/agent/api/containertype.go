// Copyright 2014-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"errors"

	"github.com/cihub/seelog"
)

const (
	// ContainerNormal represents the container type for 'Normal' containers
	// These are the ones specified in the task definition via container
	// definitions
	ContainerNormal ContainerType = iota
	// ContainerEmptyHostVolume represents the internal container type
	// for the empty volumes container
	ContainerEmptyHostVolume
	// ContainerCNIPause represents the internal container type for the
	// pause container
	ContainerCNIPause
)

// ContainerType represents the type of the internal container created
type ContainerType int32

var stringToContainerType = map[string]ContainerType{
	"NORMAL":            ContainerNormal,
	"EMPTY_HOST_VOLUME": ContainerEmptyHostVolume,
	"CNI_PAUSE":         ContainerCNIPause,
}

// String converts the container type enum to a string
func (containerType ContainerType) String() string {
	for str, contType := range stringToContainerType {
		if contType == containerType {
			return str
		}
	}

	return "NORMAL"
}

// UnmarshalJSON decodes the container type field in the JSON encoded string
// into the ContainerType object
func (containerType *ContainerType) UnmarshalJSON(b []byte) error {
	strType := string(b)

	switch strType {
	case "null":
		*containerType = ContainerNormal
		seelog.Warn("Unmarshalled nil ContainerType as Normal")
		return nil
	// 'true' or 'false' for compatibility with state version <= 5
	case "true":
		*containerType = ContainerEmptyHostVolume
		return nil
	case "false":
		*containerType = ContainerNormal
		return nil
	}

	if len(strType) < 2 {
		*containerType = ContainerNormal
		return errors.New("invalid length set for ContainerType: " + string(b))
	}
	if b[0] != '"' || b[len(b)-1] != '"' {
		*containerType = ContainerNormal
		return errors.New("invalid value set for ContainerType, must be a string or null; got " + string(b))
	}
	strType = string(b[1 : len(b)-1])

	contType, ok := stringToContainerType[strType]
	if !ok {
		*containerType = ContainerNormal
		return errors.New("unrecognized ContainerType: " + strType)
	}
	*containerType = contType
	return nil
}

// MarshalJSON overrides the logic for JSON-encoding a ContainerType object
func (containerType *ContainerType) MarshalJSON() ([]byte, error) {
	if containerType == nil {
		return []byte("null"), nil
	}

	return []byte(`"` + containerType.String() + `"`), nil
}
