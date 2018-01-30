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

package mock_http

import (
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"github.com/golang/mock/gomock"
)

// Helpers for testing request equality

type httpMatcher struct {
	method string
	url    string
}

func NewHTTPSimpleMatcher(method, url string) gomock.Matcher {
	return &httpMatcher{method, url}
}

func (m *httpMatcher) Matches(x interface{}) bool {
	req, ok := x.(*http.Request)
	if !ok {
		return false
	}
	if req.Method != m.method {
		return false
	}
	u, err := url.Parse(m.url)
	if err != nil {
		return false
	}
	if req.URL.String() != u.String() {
		return false
	}
	return true
}

func SuccessResponse(body string) *http.Response {
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    200,
		Proto:         "HTTP/1.1",
		ProtoMinor:    1,
		ProtoMajor:    1,
		Body:          ioutil.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

func (m *httpMatcher) String() string {
	return "HTTP Matcher: " + m.method + " " + m.url
}

type httpOpMatcher struct {
	operation string
}

func (m *httpOpMatcher) Matches(x interface{}) bool {
	req, ok := x.(*http.Request)
	if !ok {
		return false
	}
	return req.Header.Get("x-amz-target") == m.operation
}

func (m *httpOpMatcher) String() string {
	return "HTTP Operation matcher: " + m.operation
}

func NewHTTPOperationMatcher(op string) gomock.Matcher {
	return &httpOpMatcher{
		operation: op,
	}
}
