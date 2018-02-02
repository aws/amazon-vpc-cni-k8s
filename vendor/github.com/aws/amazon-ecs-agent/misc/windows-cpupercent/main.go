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

package main

import (
	"crypto/md5"
	"flag"
	"fmt"
)

func main() {
	concurrency := flag.Int("concurrency", 1, "amount of concurrency")
	flag.Parse()
	neverdie := make(chan struct{})

	fmt.Printf("Hogging CPU with concurrency %d\n", *concurrency)
	for i := 0; i < *concurrency; i++ {
		go func() {
			md5hash := md5.New()
			for {
				md5hash.Write([]byte{0})
			}
		}()
	}
	<-neverdie
}
