// +build integration

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

package ipstore

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	subnet     = "169.254.172.0/29"
	testdb     = "/tmp/__boltdb_test"
	testBucket = "ipmanager"
)

func setup(t *testing.T) *IPManager {
	_, err := os.Stat(testdb)
	if err != nil {
		require.True(t, os.IsNotExist(err), "if it's not file not exist error, then there should be a problem: %v", err)
	} else {
		err = os.Remove(testdb)
		require.NoError(t, err, "Remove the existed db should not cause error")
	}

	_, subnet, err := net.ParseCIDR(subnet)
	require.NoError(t, err)

	ipAllocator, err := NewIPAllocator(&Config{DB: testdb, PersistConnection: true, Bucket: testBucket, ConnectionTimeout: 1 * time.Millisecond}, *subnet)
	require.NoError(t, err, "creating the IPManager failed")

	ipManager, ok := ipAllocator.(*IPManager)
	require.True(t, ok, "Create ip manager should succeed")

	return ipManager
}

func TestAssignReleaseExistGetAvailableIP(t *testing.T) {
	ipManager := setup(t)
	defer ipManager.Close()

	ip, err := ipManager.GetAvailableIP("id")
	assert.NoError(t, err)
	assert.Equal(t, ip, "169.254.172.1")

	ok, err := ipManager.Exists("169.254.172.1")
	assert.NoError(t, err)
	assert.True(t, ok)

	err = ipManager.Assign("169.254.172.1", "id2")
	assert.Error(t, err, "Assign to an used ip should casue error")

	ip, err = ipManager.GetAvailableIP("id3")
	assert.NoError(t, err)
	assert.Equal(t, "169.254.172.2", ip, "ip should be allocated serially")
	assert.True(t, ipManager.lastKnownIP.Equal(net.ParseIP("169.254.172.2")), "ipmanager should record the recently referenced ip")

	err = ipManager.Assign("169.254.172.3", "id4")
	assert.NoError(t, err)
	assert.True(t, ipManager.lastKnownIP.Equal(net.ParseIP("169.254.172.3")), "ipmanager should record the recently reference ip")

	ok, err = ipManager.Exists("169.254.172.1")
	assert.NoError(t, err)
	assert.True(t, ok, "ip has been assigned should existed in the db")

	ok, err = ipManager.Exists("169.254.172.2")
	assert.NoError(t, err)
	assert.True(t, ok, "ip has been assigned should existed in the db")

	ok, err = ipManager.Exists("169.254.172.3")
	assert.NoError(t, err)
	assert.True(t, ok, "ip has been assigned should existed in the db")

	err = ipManager.Release("169.254.172.1")
	assert.NoError(t, err)
	assert.True(t, ipManager.lastKnownIP.Equal(net.ParseIP("169.254.172.1")))

	ok, err = ipManager.Exists("169.254.172.1")
	assert.NoError(t, err)
	assert.False(t, ok, "released ip address should not existed in the db")

	err = ipManager.Assign("169.254.172.1", "id")
	assert.NoError(t, err)
	assert.True(t, ipManager.lastKnownIP.Equal(net.ParseIP("169.254.172.1")), "ipmanager should record the recently reference ip")
}

// TestGet tests the Get function
func TestGet(t *testing.T) {
	ipManager := setup(t)
	defer ipManager.Close()

	id, err := ipManager.Get("169.254.172.0")
	assert.NoError(t, err, "Get an non-existed key should not cause error")
	assert.Empty(t, id, "Get an non-existed key should return empty value")

	err = ipManager.Assign("169.254.170.0", "id1")
	assert.NoError(t, err)

	id, err = ipManager.Get("169.254.170.0")
	assert.NoError(t, err)
	assert.Equal(t, "id1", id)
}

func TestGetAvailableIPSerially(t *testing.T) {
	ipManager := setup(t)
	defer ipManager.Close()

	ip, err := ipManager.GetAvailableIP("id")
	assert.NoError(t, err)
	assert.Equal(t, "169.254.172.1", ip)

	ip, err = ipManager.GetAvailableIP("id1")
	assert.NoError(t, err)
	assert.Equal(t, "169.254.172.2", ip, "ip should be assigned serially")
}
