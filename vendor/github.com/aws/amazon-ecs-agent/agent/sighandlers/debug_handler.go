// +build !windows

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

package sighandlers

import (
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/cihub/seelog"
)

func StartDebugHandler() {
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGUSR1)
	go func() {
		for range signalChannel {
			// "640 k ought to be enough for anybody." - Bill Gates, 1981
			stackDump := make([]byte, 640*1024)
			n := runtime.Stack(stackDump, true)
			// Resize the buffer to the size of the actual stack
			stackDump = stackDump[:n]
			seelog.Criticalf("====== STACKTRACE ======\n%v\n%s\n====== /STACKTRACE ======", time.Now(), stackDump)
		}
	}()
}
