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

package logger

import (
	"testing"
)

func TestFormatMessage_NoCtx(t *testing.T) {
	shim := &Shim{}
	simpleMessage := "simple message"
	formatted := shim.formatMessage(simpleMessage)
	expected := simpleMessage
	if formatted != expected {
		t.Errorf("Formatted message %s does not match expected %s", formatted, expected)
	}
}

func TestFormatMessage_UnevenCtx(t *testing.T) {
	shim := &Shim{}
	simpleMessage := "simple message"
	formatted := shim.formatMessage(simpleMessage, simpleMessage)
	expected := simpleMessage + " [malformed ctx omitted]"
	if formatted != expected {
		t.Errorf("Formatted message %s does not match expected %s", formatted, expected)
	}
}

func TestFormatMessage_SimpleCtx(t *testing.T) {
	shim := &Shim{}
	simpleMessage := "simple message"
	formatted := shim.formatMessage(simpleMessage, simpleMessage, simpleMessage)
	expected := simpleMessage + " " + simpleMessage + "=\"" + simpleMessage + "\""
	if formatted != expected {
		t.Errorf("Formatted message %s does not match expected %s", formatted, expected)
	}
}

func TestFormatMessage_Struct(t *testing.T) {
	shim := &Shim{}
	simpleMessage := "simple message"
	formatted := shim.formatMessage(simpleMessage, simpleMessage, simpleMessage, "struct", struct{ hello string }{
		hello: "world",
	})
	expected := simpleMessage + " " + simpleMessage + "=\"" + simpleMessage + "\" " + "struct=\"{hello:world}\""
	if formatted != expected {
		t.Errorf("Formatted message %s does not match expected %s", formatted, expected)
	}
}

func TestFormatMessage_TopCtx(t *testing.T) {
	shim := &Shim{ctx: []interface{}{"test", "value"}}
	simpleMessage := "simple message"
	formatted := shim.formatMessage(simpleMessage)
	expected := simpleMessage + " test=\"value\""
	if formatted != expected {
		t.Errorf("Formatted message %s does not match expected %s", formatted, expected)
	}
}

func TestFormatMessage_NewCtx(t *testing.T) {
	shim := (&Shim{ctx: []interface{}{"test", "value"}}).New("test2", "value2").(*Shim)
	simpleMessage := "simple message"
	formatted := shim.formatMessage(simpleMessage, "test3", "value3")
	expected := simpleMessage + " test=\"value\" test2=\"value2\" test3=\"value3\""
	if formatted != expected {
		t.Errorf("Formatted message %s does not match expected %s", formatted, expected)
	}
}
