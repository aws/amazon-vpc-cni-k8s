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

const (
	// ENIAttachmentNone is zero state of a task when received attachemessage from acs
	ENIAttachmentNone ENIAttachmentStatus = iota
	// ENIAttached represents that a eni has shown on the host
	ENIAttached
	// ENIDetached represents that a eni has been actually detached from the host
	ENIDetached
)

// ENIAttachmentStatus is an enumeration type for eni attachment state
type ENIAttachmentStatus int32

var eniAttachmentStatusMap = map[string]ENIAttachmentStatus{
	"NONE":     ENIAttachmentNone,
	"ATTACHED": ENIAttached,
	"DETACHED": ENIDetached,
}

// String return the string value of the eniattachment status
func (eni *ENIAttachmentStatus) String() string {
	for k, v := range eniAttachmentStatusMap {
		if v == *eni {
			return k
		}
	}
	return "NONE"
}

// ShouldSend returns whether the status should be sent to backend
func (eni *ENIAttachmentStatus) ShouldSend() bool {
	return *eni == ENIAttached
}
