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

package ipamd

import (
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/rpc"
)

const (
	ipamdgRPCaddress      = "127.0.0.1:50051"
	grpcHealthServiceName = "grpc.health.v1.aws-node"
)

// server controls RPC service responses.
type server struct {
	ipamContext *IPAMContext
}

// AddNetwork processes CNI add network request and return an IP address for container
func (s *server) AddNetwork(ctx context.Context, in *rpc.AddNetworkRequest) (*rpc.AddNetworkReply, error) {
	log.Infof("Received AddNetwork for NS %s, Pod %s, NameSpace %s, Sandbox %s, ifname %s",
		in.Netns, in.K8S_POD_NAME, in.K8S_POD_NAMESPACE, in.K8S_POD_INFRA_CONTAINER_ID, in.IfName)

	addr, deviceNumber, err := s.ipamContext.dataStore.AssignPodIPv4Address(&k8sapi.K8SPodInfo{
		Name:      in.K8S_POD_NAME,
		Namespace: in.K8S_POD_NAMESPACE,
		Sandbox:   in.K8S_POD_INFRA_CONTAINER_ID})

	var pbVPCcidrs []string
	for _, cidr := range s.ipamContext.awsClient.GetVPCIPv4CIDRs() {
		log.Debugf("VPC CIDR %s", *cidr)
		pbVPCcidrs = append(pbVPCcidrs, *cidr)
	}

	useExternalSNAT := s.ipamContext.networkClient.UseExternalSNAT()
	if !useExternalSNAT {
		for _, cidr := range s.ipamContext.networkClient.GetExcludeSNATCIDRs() {
			log.Debugf("CIDR SNAT Exclusion %s", cidr)
			pbVPCcidrs = append(pbVPCcidrs, cidr)
		}
	}

	resp := rpc.AddNetworkReply{
		Success:         err == nil,
		IPv4Addr:        addr,
		DeviceNumber:    int32(deviceNumber),
		UseExternalSNAT: useExternalSNAT,
		VPCcidrs:        pbVPCcidrs,
	}

	log.Infof("Send AddNetworkReply: IPv4Addr %s, DeviceNumber: %d, err: %v", addr, deviceNumber, err)
	addIPCnt.Inc()
	return &resp, nil
}

func (s *server) DelNetwork(ctx context.Context, in *rpc.DelNetworkRequest) (*rpc.DelNetworkReply, error) {
	log.Infof("Received DelNetwork for Pod %s, Namespace %s, Sandbox %s",
		in.K8S_POD_NAME, in.K8S_POD_NAMESPACE, in.K8S_POD_INFRA_CONTAINER_ID)
	delIPCnt.With(prometheus.Labels{"reason": in.Reason}).Inc()

	ip, deviceNumber, err := s.ipamContext.dataStore.UnassignPodIPv4Address(&k8sapi.K8SPodInfo{
		Name:      in.K8S_POD_NAME,
		Namespace: in.K8S_POD_NAMESPACE,
		Sandbox:   in.K8S_POD_INFRA_CONTAINER_ID})

	if err != nil && err == datastore.ErrUnknownPod {
		// If L-IPAMD restarts, the pod's IP address are assigned by only pod's name and namespace due to kubelet's introspection.
		ip, deviceNumber, err = s.ipamContext.dataStore.UnassignPodIPv4Address(&k8sapi.K8SPodInfo{
			Name:      in.K8S_POD_NAME,
			Namespace: in.K8S_POD_NAMESPACE})
	}
	log.Infof("Send DelNetworkReply: IPv4Addr %s, DeviceNumber: %d, err: %v", ip, deviceNumber, err)

	return &rpc.DelNetworkReply{Success: err == nil, IPv4Addr: ip, DeviceNumber: int32(deviceNumber)}, err
}

// RunRPCHandler handles request from gRPC
func (c *IPAMContext) RunRPCHandler() error {
	log.Infof("Serving RPC Handler on ", ipamdgRPCaddress)
	listener, err := net.Listen("tcp", ipamdgRPCaddress)
	if err != nil {
		log.Errorf("Failed to listen gRPC port: %v", err)
		return errors.Wrap(err, "ipamd: failed to listen to gRPC port")
	}
	grpcServer := grpc.NewServer()
	rpc.RegisterCNIBackendServer(grpcServer, &server{ipamContext: c})
	healthServer := health.NewServer()
	// If ipamd can talk to the API server and to the EC2 API, the pod is healthy.
	// No need to ever change this to HealthCheckResponse_NOT_SERVING since it's a local service only
	healthServer.SetServingStatus(grpcHealthServiceName, healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(grpcServer, healthServer)

	// Register reflection service on gRPC server.
	reflection.Register(grpcServer)
	// Add shutdown hook
	go c.shutdownListener()
	if err := grpcServer.Serve(listener); err != nil {
		log.Errorf("Failed to start server on gRPC port: %v", err)
		return errors.Wrap(err, "ipamd: failed to start server on gPRC port")
	}
	return nil
}

// shutdownListener - Listen to signals and set ipamd to be in status "terminating"
func (c *IPAMContext) shutdownListener() {
	log.Info("Setting up shutdown hook.")
	sig := make(chan os.Signal, 1)

	// Interrupt signal sent from terminal
	signal.Notify(sig, syscall.SIGINT)
	// Terminate signal sent from Kubernetes
	signal.Notify(sig, syscall.SIGTERM)

	<-sig
	log.Info("Received shutdown signal, setting 'terminating' to true")
	// We received an interrupt signal, shut down.
	c.setTerminating()
}
