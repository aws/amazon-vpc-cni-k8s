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
	"net"

	"github.com/pkg/errors"

	pb "github.com/aws/amazon-vpc-cni-k8s/rpc"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	log "github.com/cihub/seelog"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
)

const (
	port = "127.0.0.1:50051"
)

type server struct {
	ipamContext *IPAMContext
}

// AddNetwork processes CNI add network request and return an IP address for container
func (s *server) AddNetwork(ctx context.Context, in *pb.AddNetworkRequest) (*pb.AddNetworkReply, error) {
	log.Infof("Received AddNetwork for NS %s, Pod %s, NameSpace %s, Container %s, ifname %s",
		in.Netns, in.K8S_POD_NAME, in.K8S_POD_NAMESPACE, in.K8S_POD_INFRA_CONTAINER_ID, in.IfName)

	addr, deviceNumber, err := s.ipamContext.dataStore.AssignPodIPv4Address(&k8sapi.K8SPodInfo{
		Name:      in.K8S_POD_NAME,
		Namespace: in.K8S_POD_NAMESPACE,
		Container: in.K8S_POD_INFRA_CONTAINER_ID})
	log.Infof("Send AddNetworkReply: IPv4Addr %s, DeviceNumber: %d, err: %v", addr, deviceNumber, err)
	addIPCnt.Inc()
	return &pb.AddNetworkReply{Success: err == nil, IPv4Addr: addr, IPv4Subnet: "", DeviceNumber: int32(deviceNumber)}, nil
}

func (s *server) DelNetwork(ctx context.Context, in *pb.DelNetworkRequest) (*pb.DelNetworkReply, error) {
	log.Infof("Received DelNetwork for IP %s, Pod %s, Namespace %s, Container %s",
		in.IPv4Addr, in.K8S_POD_NAME, in.K8S_POD_NAMESPACE, in.K8S_POD_INFRA_CONTAINER_ID)
	delIPCnt.With(prometheus.Labels{"reason": in.Reason}).Inc()

	var err error

	ip, deviceNumber, err := s.ipamContext.dataStore.UnAssignPodIPv4Address(&k8sapi.K8SPodInfo{
		Name:      in.K8S_POD_NAME,
		Namespace: in.K8S_POD_NAMESPACE,
		Container: in.K8S_POD_INFRA_CONTAINER_ID})

	if err != nil && err == datastore.ErrUnknownPod {
		// If L-IPAMD restarts, the pod's IP address are assigned by only pod's name and namespace due to kubelet's introspection.
		ip, deviceNumber, err = s.ipamContext.dataStore.UnAssignPodIPv4Address(&k8sapi.K8SPodInfo{
			Name:      in.K8S_POD_NAME,
			Namespace: in.K8S_POD_NAMESPACE})
	}
	log.Infof("Send DelNetworkReply: IPv4Addr %s, DeviceNumber: %d, err: %v", ip, deviceNumber, err)

	return &pb.DelNetworkReply{Success: err == nil, IPv4Addr: ip, DeviceNumber: int32(deviceNumber)}, nil
}

// RunRPCHandler handles request from gRPC
func (c *IPAMContext) RunRPCHandler() error {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Errorf("Failed to listen gRPC port: %v", err)
		return errors.Wrap(err, "ipamd: failed to listen to gRPC port")
	}
	s := grpc.NewServer()
	pb.RegisterCNIBackendServer(s, &server{ipamContext: c})
	// Register reflection service on gRPC server.
	reflection.Register(s)
	if err := s.Serve(lis); err != nil {
		log.Errorf("Failed to start server on gRPC port: %v", err)
		return errors.Wrap(err, "ipamd: failed to start server on gPRC port")
	}

	return nil
}
