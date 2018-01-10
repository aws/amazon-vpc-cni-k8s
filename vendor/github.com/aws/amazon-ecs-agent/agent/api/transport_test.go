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
	"encoding/json"
	"testing"
)

func TestUnmarshalTransportProtocol_Null(t *testing.T) {
	tp := TransportProtocolTCP

	err := json.Unmarshal([]byte("null"), &tp)
	if err != nil {
		t.Error(err)
	}
	if tp != TransportProtocolTCP {
		t.Error("null TransportProtocol should be TransportProtocolTCP")
	}
}

func TestUnmarshalTransportProtocol_TCP(t *testing.T) {
	tp := TransportProtocolTCP

	err := json.Unmarshal([]byte(`"tcp"`), &tp)
	if err != nil {
		t.Error(err)
	}
	if tp != TransportProtocolTCP {
		t.Error("tcp TransportProtocol should be TransportProtocolTCP")
	}
}

func TestUnmarshalTransportProtocol_UDP(t *testing.T) {
	tp := TransportProtocolTCP

	err := json.Unmarshal([]byte(`"udp"`), &tp)
	if err != nil {
		t.Error(err)
	}
	if tp != TransportProtocolUDP {
		t.Error("udp TransportProtocol should be TransportProtocolUDP")
	}
}

func TestUnmarshalTransportProtocol_NullStruct(t *testing.T) {
	tp := struct {
		Field1 TransportProtocol
	}{}

	err := json.Unmarshal([]byte(`{"Field1":null}`), &tp)
	if err != nil {
		t.Error(err)
	}
	if tp.Field1 != TransportProtocolTCP {
		t.Error("null TransportProtocol should be TransportProtocolTCP")
	}
}

func TestMarshalTransportProtocol_Unset(t *testing.T) {
	data := struct {
		Field TransportProtocol
	}{}

	json, err := json.Marshal(&data)
	if err != nil {
		t.Error(err)
	}
	if string(json) != `{"Field":"tcp"}` {
		t.Error(string(json))
	}
}

func TestMarshalTransportProtocol_TCP(t *testing.T) {
	data := struct {
		Field TransportProtocol
	}{TransportProtocolTCP}

	json, err := json.Marshal(&data)
	if err != nil {
		t.Error(err)
	}
	if string(json) != `{"Field":"tcp"}` {
		t.Error(string(json))
	}
}

func TestMarshalTransportProtocol_UDP(t *testing.T) {
	data := struct {
		Field TransportProtocol
	}{TransportProtocolUDP}

	json, err := json.Marshal(&data)
	if err != nil {
		t.Error(err)
	}
	if string(json) != `{"Field":"udp"}` {
		t.Error(string(json))
	}
}

func TestMarshalUnmarshalTransportProtocol(t *testing.T) {
	data := struct {
		Field1 TransportProtocol
		Field2 TransportProtocol
	}{TransportProtocolTCP, TransportProtocolUDP}

	jsonBytes, err := json.Marshal(&data)
	if err != nil {
		t.Error(err)
	}
	if string(jsonBytes) != `{"Field1":"tcp","Field2":"udp"}` {
		t.Error(string(jsonBytes))
	}

	unmarshalTo := struct {
		Field1 TransportProtocol
		Field2 TransportProtocol
	}{}

	err = json.Unmarshal(jsonBytes, &unmarshalTo)
	if err != nil {
		t.Error(err)
	}
	if unmarshalTo.Field1 != TransportProtocolTCP || unmarshalTo.Field2 != TransportProtocolUDP {
		t.Errorf("Expected tcp for Field1 but was %x, expected udp for Field2 but was %x", unmarshalTo.Field1, unmarshalTo.Field2)
	}
}
