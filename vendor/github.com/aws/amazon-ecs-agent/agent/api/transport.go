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
	"strings"

	"github.com/cihub/seelog"
)

const (
	// TransportProtocolTCP represents TCP
	TransportProtocolTCP TransportProtocol = iota
	// TransportProtocolUDP represents UDP
	TransportProtocolUDP

	tcp = "tcp"
	udp = "udp"
)

// TransportProtocol is an enumeration of valid transport protocols
type TransportProtocol int32

// NewTransportProtocol returns a TransportProtocol from a string in the task
func NewTransportProtocol(protocol string) (TransportProtocol, error) {
	switch protocol {
	case tcp:
		return TransportProtocolTCP, nil
	case udp:
		return TransportProtocolUDP, nil
	default:
		return TransportProtocolTCP, errors.New(protocol + " is not a recognized transport protocol")
	}
}

// String converts TransportProtocol to a string
func (tp *TransportProtocol) String() string {
	if tp == nil {
		return tcp
	}
	switch *tp {
	case TransportProtocolUDP:
		return udp
	case TransportProtocolTCP:
		return tcp
	default:
		seelog.Critical("Unknown TransportProtocol type!")
		return tcp
	}
}

// UnmarshalJSON for TransportProtocol determines whether to use TCP or UDP,
// setting TCP as the zero-value but treating other unrecognized values as
// errors
func (tp *TransportProtocol) UnmarshalJSON(b []byte) error {
	if strings.ToLower(string(b)) == "null" {
		*tp = TransportProtocolTCP
		seelog.Warn("Unmarshalled nil TransportProtocol as TCP")
		return nil
	}
	switch string(b) {
	case `"tcp"`:
		*tp = TransportProtocolTCP
	case `"udp"`:
		*tp = TransportProtocolUDP
	default:
		*tp = TransportProtocolTCP
		return errors.New("TransportProtocol must be \"tcp\" or \"udp\"; Got " + string(b))
	}
	return nil
}

// MarshalJSON overrides the logic for JSON-encoding the TransportProtocol type
func (tp *TransportProtocol) MarshalJSON() ([]byte, error) {
	if tp == nil {
		return []byte("null"), nil
	}
	return []byte(`"` + tp.String() + `"`), nil
}
