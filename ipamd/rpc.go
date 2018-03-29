// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package ipamd

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "github.com/aws/amazon-vpc-cni-k8s/rpc"
)

const (
	grpcPort    = "127.0.0.1:50051"
	initTimeout = 1 * time.Second
)

// AddNetwork processes CNI add network request and return an IP address for container.
func (i *IPAMD) AddNetwork(ctx context.Context, in *pb.AddNetworkRequest) (*pb.AddNetworkReply, error) {
	log.Debugf("AddNetworkRequest %+#v\n", in)
	ip, err := i.dataStore.AssignPodIP(ctx, in.K8S_POD_NAME, in.K8S_POD_NAMESPACE)
	reply := &pb.AddNetworkReply{
		Success:    err == nil,
		IPv4Subnet: "",
	}
	if err == nil {
		reply.IPv4Addr = ip.IP.String()
		reply.DeviceNumber = int32(ip.ENI.Device)
	} else {
		log.Warningf("AddNetwork error: %v", err)
	}
	log.Debugf("AddNetworkReply: %+#v", reply)
	return reply, nil
}

// DelNetwork processes CNI delete network request.
func (i *IPAMD) DelNetwork(ctx context.Context, in *pb.DelNetworkRequest) (*pb.DelNetworkReply, error) {
	log.Debugf("DelNetworkRequest %+#v\n", in)
	ip, err := i.dataStore.UnassignPodIP(ctx, in.K8S_POD_NAME, in.K8S_POD_NAMESPACE)
	reply := &pb.DelNetworkReply{
		Success: err == nil,
	}
	if err == nil {
		reply.IPv4Addr = ip.IP.String()
		reply.DeviceNumber = int32(ip.ENI.Device)
	} else {
		log.Warningf("DelNetwork error: %v", err)
	}
	log.Debugf("DelNetworkReply: %+#v", reply)
	return reply, nil
}

// RunRPCHandler handles request from gRPC
func (i *IPAMD) RunRPCHandler() error {
	lis, err := net.Listen("tcp", grpcPort)
	if err != nil {
		log.Errorf("Failed to listen gRPC port %v: %v", grpcPort, err)
		return errors.Wrap(err, "ipamd: failed to listen to gRPC port")
	}

	select {
	case <-i.inited:
		break
	case <-time.After(initTimeout):
		log.Errorf("Timeout on init L-IPAMD")
		return fmt.Errorf("Timeout on init L-IPAMD")
	}

	s := grpc.NewServer()
	pb.RegisterCNIBackendServer(s, i)
	reflection.Register(s) // Register reflection service on gRPC server.
	if err := s.Serve(lis); err != nil {
		log.Errorf("Failed to start server on gRPC port: %v", err)
		return errors.Wrap(err, "ipamd: failed to start server on gPRC port")
	}

	return nil
}
