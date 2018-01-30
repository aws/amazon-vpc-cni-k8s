// This file is derived from the fsouza/go-dockerclient Project, Copyright (c) 2013-2016, go-dockerclient authors
// The original code may be found :
// https://github.com/fsouza/go-dockerclient/blob/master/misc.go
//
// Copyright 2013 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Modifications are Copyright 2016 Amazon.com, Inc. or its affiliates. Licensed under the Apache License 2.0

package engine

import "strings"

// parseRepositoryTag mimics the go-dockerclient's ParseReposirotyData. The only difference
// is that it doesn't ignore the sha when present.
func parseRepositoryTag(repoTag string) (repository string, tag string) {
	n := strings.LastIndex(repoTag, ":")
	if n < 0 {
		return repoTag, ""
	}
	if tag := repoTag[n+1:]; !strings.Contains(tag, "/") {
		return repoTag[:n], tag
	}
	return repoTag, ""
}
