// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package main

import (
	"flag"
	"fmt"
	"log"
	"net"
)

const ServerResponse = "message from the server!"

// Server that listens to client connection and first reads data and then writes data to the client.
func main() {
	// Supported modes are tcp, udp
	var serverMode string
	// Port on which Server will listen for incoming connections
	var serverPort string

	flag.StringVar(&serverMode, "server-mode", "tcp", "server mode, accepts tcp or udp")
	flag.StringVar(&serverPort, "server-port", "2273", "port on which you want to start the server")

	flag.Parse()

	addr := fmt.Sprintf(":%s", serverPort)

	if serverMode == "tcp" {
		StartTCPServer(addr)
	} else if serverMode == "udp" {
		StartUDPServer(addr)
	} else {
		log.Fatal("invalid server mode, can accept tcp/udp only")
	}
}

func StartTCPServer(serverAddr string) {
	log.Printf("starting TCP listener on %s", serverAddr)

	listener, err := net.Listen("tcp", serverAddr)
	if err != nil {
		log.Fatalf("failed to start server: %v", err)
	}

	defer listener.Close()

	for {
		log.Printf("waiting for incomming connection")
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("failed to accept connection %v", err)
			continue
		}

		log.Printf("succesffuly established connection with client %v", conn.RemoteAddr())

		err = readAndWriteFromConnection(conn)
		if err != nil {
			log.Printf("failed to read/write from connection: %v", err)
			continue
		}

		log.Printf("sucessfully read and wrote response to tcp client %v", conn.RemoteAddr())
	}
}

func readAndWriteFromConnection(conn net.Conn) error {
	defer conn.Close()

	buff := make([]byte, 1024)
	_, err := conn.Read(buff)
	if err != nil {
		return fmt.Errorf("failed to read from connection stream: %v", err)
	}

	log.Printf("successfully received message from client %s", string(buff))

	_, err = conn.Write([]byte(ServerResponse))
	if err != nil {
		return fmt.Errorf("failed to write back to the client: %v", err)
	}

	log.Printf("successfully wrote back to the client")

	return nil
}

func StartUDPServer(serverAddr string) {
	log.Printf("starting UDP listener on address %s", serverAddr)

	udpAddr, err := net.ResolveUDPAddr("udp", serverAddr)
	if err != nil {
		log.Fatalf("failed to resolve udp address: %v", err)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Fatalf("failed to lisent for udp packets: %v", err)
	}
	defer conn.Close()

	for {
		log.Print("waiting for incoming requests")

		buffer := make([]byte, 1024)
		_, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("failed to read udp packets: %v", err)
			continue
		}

		log.Printf("successfully recieved request from remote client %v: %s",
			clientAddr.String(), string(buffer))

		_, err = conn.WriteToUDP([]byte(ServerResponse), clientAddr)
		if err != nil {
			log.Printf("failed to write back to the client: %v", err)
			continue
		}

		log.Printf("sucessfully wrote back to remote client %v", clientAddr)
	}
}
