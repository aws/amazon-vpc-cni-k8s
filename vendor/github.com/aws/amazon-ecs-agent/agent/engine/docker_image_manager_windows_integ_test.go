// +build windows,integration
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

// These constants are a hack around being unable to run a local registry on
// Windows.  We're unable to run a local registry due to the lack of loopback
// networking support in Windows Net NAT.
const (
	test1Image1Name = "amazon/image-cleanup-test-image1:make"
	test1Image2Name = "amazon/image-cleanup-test-image2:make"
	test1Image3Name = "amazon/image-cleanup-test-image3:make"

	test2Image1Name = "amazon/image-cleanup-test-image4:make"
	test2Image2Name = "amazon/image-cleanup-test-image5:make"
	test2Image3Name = "amazon/image-cleanup-test-image6:make"

	test3Image1Name = "amazon/image-cleanup-test-image7:make"
	test3Image2Name = "amazon/image-cleanup-test-image8:make"
	test3Image3Name = "amazon/image-cleanup-test-image9:make"

	test4Image1Name = "amazon/image-cleanup-test-image10:make"
)
