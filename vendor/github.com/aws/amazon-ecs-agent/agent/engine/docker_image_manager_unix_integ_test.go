// +build !windows,integration
// Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

// The DockerTaskEngine is an abstraction over the DockerGoClient so that
// it does not have to know about tasks, only containers

package engine

const (
	test1Image1Name = "127.0.0.1:51670/amazon/image-cleanup-test-image1:latest"
	test1Image2Name = "127.0.0.1:51670/amazon/image-cleanup-test-image2:latest"
	test1Image3Name = "127.0.0.1:51670/amazon/image-cleanup-test-image3:latest"

	test2Image1Name = "127.0.0.1:51670/amazon/image-cleanup-test-image1:latest"
	test2Image2Name = "127.0.0.1:51670/amazon/image-cleanup-test-image2:latest"
	test2Image3Name = "127.0.0.1:51670/amazon/image-cleanup-test-image3:latest"

	test3Image1Name = "127.0.0.1:51670/amazon/image-cleanup-test-image1:latest"
	test3Image2Name = "127.0.0.1:51670/amazon/image-cleanup-test-image2:latest"
	test3Image3Name = "127.0.0.1:51670/amazon/image-cleanup-test-image3:latest"

	test4Image1Name = "127.0.0.1:51670/amazon/image-cleanup-test-image1:latest"
)
