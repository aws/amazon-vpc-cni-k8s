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
	"encoding/json"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	"github.com/aws/amazon-vpc-cni-k8s/rpc"
	k8serror "k8s.io/apimachinery/pkg/api/errors"
)

const (
	ipamdgRPCaddress      = "127.0.0.1:50051"
	grpcHealthServiceName = "grpc.health.v1.aws-node"
)

// server controls RPC service responses.
type server struct {
	ipamContext *IPAMContext
}

// PodENIData is used to parse the list of ENIs in the branch ENI pod annotation
type PodENIData struct {
	ENIID      string `json:"eniId"`
	IfAddress  string `json:"ifAddress"`
	PrivateIP  string `json:"privateIp"`
	VlanID     int    `json:"vlanId"`
	SubnetCIDR string `json:"subnetCidr"`
}

// AddNetwork processes CNI add network request and return an IP address for container
func (s *server) AddNetwork(ctx context.Context, in *rpc.AddNetworkRequest) (*rpc.AddNetworkReply, error) {
	log.Infof("Received AddNetwork for NS %s, Sandbox %s, ifname %s",
		in.Netns, in.ContainerID, in.IfName)
	addIPCnt.Inc()

	failureResponse := rpc.AddNetworkReply{Success: false}
	var deviceNumber, vlanId, trunkENILinkIndex int
	var addr, branchENIMAC, podENISubnetGW string
	var err error
	if s.ipamContext.enablePodENI {
		// Check pod spec for Branch ENI
		pod, err := s.ipamContext.GetPod(in.K8S_POD_NAME, in.K8S_POD_NAMESPACE)
		if err != nil {
			log.Warnf("Send AddNetworkReply: Failed to get pod: %v", err)
			return &failureResponse, nil
		}
		limits := pod.Spec.Containers[0].Resources.Limits
		for resName := range limits {
			if strings.HasPrefix(string(resName), "vpc.amazonaws.com/pod-eni") {
				// Check that we have a trunk
				trunkENI := s.ipamContext.dataStore.GetTrunkENI()
				if trunkENI == "" {
					log.Warn("Send AddNetworkReply: No trunk ENI found, cannot add a pod ENI")
					return &failureResponse, nil
				} else {
					trunkENILinkIndex, err = s.ipamContext.getTrunkLinkIndex()
					if err != nil {
						log.Warn("Send AddNetworkReply: No trunk ENI Link Index found, cannot add a pod ENI")
						return &failureResponse, nil
					}
				}
				val, branch := pod.Annotations["vpc.amazonaws.com/pod-eni"]
				if branch {
					// Parse JSON data
					var podENIData []PodENIData
					err := json.Unmarshal([]byte(val), &podENIData)
					if err != nil || len(podENIData) < 1 {
						log.Errorf("Failed to unmarshal PodENIData JSON: %v", err)
						return &failureResponse, nil
					}
					firstENI := podENIData[0]
					addr = firstENI.PrivateIP
					branchENIMAC = firstENI.IfAddress
					vlanId = firstENI.VlanID
					if addr == "" || branchENIMAC == "" || vlanId == 0 {
						log.Errorf("Failed to parse pod-ENI annotation: %s", val)
						return &failureResponse, nil
					}
					currentGW := strings.Split(firstENI.SubnetCIDR, "/")[0]
					// Increment value CIDR value
					nextGWIP, err := networkutils.IncrementIPv4Addr(net.ParseIP(currentGW))
					if err != nil {
						log.Errorf("Unable to get next Gateway IP for branch ENI from %s: %v", currentGW, err)
						return &failureResponse, nil
					}
					podENISubnetGW = nextGWIP.String()
					deviceNumber = -1 // Not needed for branch ENI, they depend on trunkENIDeviceIndex
				} else {
					log.Infof("Send AddNetworkReply: failed to get Branch ENI resource")
					return &failureResponse, nil
				}
			}
		}
	}
	if addr == "" {
		ipamKey := datastore.IPAMKey{
			ContainerID: in.ContainerID,
			IfName:      in.IfName,
			NetworkName: in.NetworkName,
		}
		addr, deviceNumber, err = s.ipamContext.dataStore.AssignPodIPv4Address(ipamKey)
	}
	pbVPCcidrs := s.ipamContext.awsClient.GetVPCIPv4CIDRs()
	for _, cidr := range pbVPCcidrs {
		log.Debugf("VPC CIDR %s", cidr)
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
		PodVlanId:       int32(vlanId),
		PodENIMAC:       branchENIMAC,
		PodENISubnetGW:  podENISubnetGW,
		ParentIfIndex:   int32(trunkENILinkIndex),
	}

	log.Infof("Send AddNetworkReply: IPv4Addr %s, DeviceNumber: %d, err: %v", addr, deviceNumber, err)
	return &resp, nil
}

func (s *server) DelNetwork(ctx context.Context, in *rpc.DelNetworkRequest) (*rpc.DelNetworkReply, error) {
	log.Infof("Received DelNetwork for Sandbox %s", in.ContainerID)
	delIPCnt.With(prometheus.Labels{"reason": in.Reason}).Inc()
	if s.ipamContext.enablePodENI {
		pod, err := s.ipamContext.GetPod(in.K8S_POD_NAME, in.K8S_POD_NAMESPACE)
		if err != nil {
			if k8serror.IsNotFound(err) {
				log.Warn("Send AddNetworkReply: pod not found")
				return &rpc.DelNetworkReply{Success: true}, nil
			}
			log.Warnf("Send DelNetworkReply: Failed to get pod spec: %v", err)
			return &rpc.DelNetworkReply{Success: false}, err
		} else {
			val, branch := pod.Annotations["vpc.amazonaws.com/pod-eni"]
			if branch {
				// Parse JSON data
				var podENIData []PodENIData
				err := json.Unmarshal([]byte(val), &podENIData)
				if err != nil || len(podENIData) < 1 {
					log.Errorf("Failed to unmarshal PodENIData JSON: %v", err)
				}
				return &rpc.DelNetworkReply{
					Success:   true,
					PodVlanId: int32(podENIData[0].VlanID),
					IPv4Addr:  podENIData[0].PrivateIP}, err
			}
		}
	}
	ipamKey := datastore.IPAMKey{
		ContainerID: in.ContainerID,
		IfName:      in.IfName,
		NetworkName: in.NetworkName,
	}
	ip, deviceNumber, err := s.ipamContext.dataStore.UnassignPodIPv4Address(ipamKey)

	log.Infof("Send DelNetworkReply: IPv4Addr %s, DeviceNumber: %d, err: %v", ip, deviceNumber, err)

	return &rpc.DelNetworkReply{Success: err == nil, IPv4Addr: ip, DeviceNumber: int32(deviceNumber)}, err
}

// RunRPCHandler handles request from gRPC
func (c *IPAMContext) RunRPCHandler() error {
	log.Infof("Serving RPC Handler on %s", ipamdgRPCaddress)
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
