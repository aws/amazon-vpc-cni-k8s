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

package app

// transientError represents a transient error when executing the ECS Agent
type transientError struct {
	error
}

// isTransient returns true if the error is transient
func isTransient(err error) bool {
	_, ok := err.(transientError)
	return ok
}

// clusterMismatchError represents a mismatch in cluster name between the
// state file and the config object
type clusterMismatchError struct {
	error
}

func isClusterMismatch(err error) bool {
	_, ok := err.(clusterMismatchError)
	return ok
}
