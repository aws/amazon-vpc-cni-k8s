// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package ipamd

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEniV1RequestHandler_ScrubsSensitiveFields(t *testing.T) {
	// Set up a datastore with a pod assigned
	ds := datastore.NewDataStore(log, datastore.NullCheckpoint{}, false, 0)
	ds.AddENI("eni-test-001", 0, true, false, false, 254, "subnet-abc")
	ipv4Addr := net.IPNet{IP: net.ParseIP("10.0.0.5"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	ds.AddIPv4CidrToStore("eni-test-001", ipv4Addr, false)

	// Assign an IP to a pod
	key := datastore.IPAMKey{
		NetworkName: "aws-cni",
		ContainerID: "abc123-secret-container-id",
		IfName:      "eth0",
	}
	metadata := datastore.IPAMMetadata{
		K8SPodNamespace: "default",
		K8SPodName:      "my-pod",
	}
	_, _, _, _, err := ds.AssignPodIPAddress(key, metadata, true, false)
	require.NoError(t, err)

	// Build IPAMContext
	dsAccess := &datastore.DataStoreAccess{DataStores: []*datastore.DataStore{ds}}
	ipamCtx := &IPAMContext{
		dataStoreAccess: dsAccess,
	}

	// Call the handler
	handler := eniV1RequestHandler(ipamCtx)
	req := httptest.NewRequest(http.MethodGet, "/v1/enis", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Parse response and verify sensitive fields are scrubbed
	body := rec.Body.String()
	assert.NotEmpty(t, body)

	// The response should NOT contain the container ID or interface name
	assert.NotContains(t, body, "abc123-secret-container-id",
		"ContainerID should be scrubbed from introspection response")
	assert.NotContains(t, body, `"ifName":"eth0"`,
		"IfName should be scrubbed from introspection response")

	// But should still contain useful non-sensitive info
	assert.Contains(t, body, "eni-test-001", "ENI ID should still be present")
	assert.Contains(t, body, "10.0.0.5", "IP address should still be present")

	// Verify the JSON structure is valid
	var result map[string]json.RawMessage
	err = json.Unmarshal(rec.Body.Bytes(), &result)
	assert.NoError(t, err, "Response should be valid JSON")
}

func TestEniV1RequestHandler_ScrubsIPv6Fields(t *testing.T) {
	// Set up a datastore with an IPv6 prefix
	ds := datastore.NewDataStore(log, datastore.NullCheckpoint{}, true, 0)
	ds.AddENI("eni-v6-001", 0, true, false, false, 254, "subnet-v6")
	_, ipv6Net, _ := net.ParseCIDR("2001:db8::/64")
	ds.AddIPv6CidrToStore("eni-v6-001", *ipv6Net, true)

	// Assign an IPv6 address to a pod
	key := datastore.IPAMKey{
		NetworkName: "aws-cni",
		ContainerID: "v6-container-secret-id",
		IfName:      "eth0",
	}
	metadata := datastore.IPAMMetadata{
		K8SPodNamespace: "default",
		K8SPodName:      "v6-pod",
	}
	_, _, _, _, err := ds.AssignPodIPAddress(key, metadata, false, true)
	require.NoError(t, err)

	dsAccess := &datastore.DataStoreAccess{DataStores: []*datastore.DataStore{ds}}
	ipamCtx := &IPAMContext{
		dataStoreAccess: dsAccess,
	}

	handler := eniV1RequestHandler(ipamCtx)
	req := httptest.NewRequest(http.MethodGet, "/v1/enis", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, "v6-container-secret-id",
		"IPv6 ContainerID should be scrubbed from introspection response")
}
