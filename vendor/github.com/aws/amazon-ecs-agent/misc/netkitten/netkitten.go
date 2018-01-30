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

// Netkitten is a slimmed down netcat intended to make our integ tests able to
// run containers lighter than busybox+netcat, but still be able to do suitably
// complex network testing. This program is only intended for testing. It
// implements cli functionality to read data from a given tcp address to stdout,
// serve data on
package main

import (
	"flag"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
)

func main() {
	var listen = flag.Int("l", -1, "Port to listen on and write data to")
	var data = flag.String("write", "", "Data to write to the server connected to")
	var serve = flag.String("serve", "", "Data to serve to clients when listening")
	var loop = flag.Bool("loop", false, "Loop forever for reading or writing")
	flag.Parse()
	lport := *listen

	target := flag.Arg(0)

	once := sync.Once{}
	var ln net.Listener
	var err error
	for {
		// If given a target, work with it first
		var cconn net.Conn
		defer func() {
			if cconn != nil {
				cconn.Close()
			}
		}()
		if target != "" {
			var err error
			cconn, err = net.Dial("tcp", target)
			if err != nil {
				log.Fatal("Error connecting to target: ", err)
			}
			if *data != "" {
				cconn.Write([]byte(*data))
			}
		}

		var conn net.Conn
		if lport >= 0 {
			once.Do(func() {
				ln, err = net.Listen("tcp", ":"+strconv.Itoa(lport))
			})
			if err != nil {
				log.Fatal("Error listening: ", err)
			}
			conn, err = ln.Accept()
			if err != nil {
				log.Fatal("Error accepting connection: ", err)
			}

			if *serve != "" {
				conn.Write([]byte(*serve))
			}
			if target != "" && cconn != nil {
				io.Copy(conn, cconn)
			}
			conn.Close()
		} else if cconn != nil {
			io.Copy(os.Stdout, cconn)
		}

		if !*loop {
			break
		}
	}
}
