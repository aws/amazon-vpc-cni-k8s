// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	taskARN       = "t1"
	attachmentARN = "att1"
	mac           = "mac1"
	attachSent    = true
)

func TestMarshalUnmarshal(t *testing.T) {
	expiresAt := time.Now()
	attachment := &ENIAttachment{
		TaskARN:          taskARN,
		AttachmentARN:    attachmentARN,
		AttachStatusSent: attachSent,
		MACAddress:       mac,
		Status:           ENIAttachmentNone,
		ExpiresAt:        expiresAt,
	}
	bytes, err := json.Marshal(attachment)
	assert.NoError(t, err)
	var unmarshalledAttachment ENIAttachment
	err = json.Unmarshal(bytes, &unmarshalledAttachment)
	assert.NoError(t, err)
	assert.Equal(t, attachment.TaskARN, unmarshalledAttachment.TaskARN)
	assert.Equal(t, attachment.AttachmentARN, unmarshalledAttachment.AttachmentARN)
	assert.Equal(t, attachment.AttachStatusSent, unmarshalledAttachment.AttachStatusSent)
	assert.Equal(t, attachment.MACAddress, unmarshalledAttachment.MACAddress)
	assert.Equal(t, attachment.Status, unmarshalledAttachment.Status)

	expectedExpiresAtUTC, err := time.Parse(time.RFC3339, attachment.ExpiresAt.Format(time.RFC3339))
	assert.NoError(t, err)
	unmarshalledExpiresAtUTC, err := time.Parse(time.RFC3339, unmarshalledAttachment.ExpiresAt.Format(time.RFC3339))
	assert.NoError(t, err)
	assert.Equal(t, expectedExpiresAtUTC, unmarshalledExpiresAtUTC)
}

func TestStartTimerErrorWhenExpiresAtIsInThePast(t *testing.T) {
	expiresAt := time.Now().Unix() - 1
	attachment := &ENIAttachment{
		TaskARN:          taskARN,
		AttachmentARN:    attachmentARN,
		AttachStatusSent: attachSent,
		MACAddress:       mac,
		Status:           ENIAttachmentNone,
		ExpiresAt:        time.Unix(expiresAt, 0),
	}
	assert.Error(t, attachment.StartTimer(func() {}))
}

func TestHasExpired(t *testing.T) {
	for _, tc := range []struct {
		expiresAt int64
		expected  bool
		name      string
	}{
		{time.Now().Unix() - 1, true, "expiresAt in past returns true"},
		{time.Now().Unix() + 10, false, "expiresAt in future returns false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attachment := &ENIAttachment{
				TaskARN:          taskARN,
				AttachmentARN:    attachmentARN,
				AttachStatusSent: attachSent,
				MACAddress:       mac,
				Status:           ENIAttachmentNone,
				ExpiresAt:        time.Unix(tc.expiresAt, 0),
			}
			assert.Equal(t, tc.expected, attachment.HasExpired())
		})
	}
}
