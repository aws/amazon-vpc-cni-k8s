// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//      http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package ipamd

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/eniconfig"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/retry"
)

const (
	// defaultIntrospectionAddress is listening on localhost 61679 for ipamd introspection
	defaultIntrospectionBindAddress = "127.0.0.1:61679"

	// Environment variable to define the bind address for the introspection endpoint
	introspectionBindAddress = "INTROSPECTION_BIND_ADDRESS"
)

type rootResponse struct {
	AvailableCommands []string
}

// LoggingHandler is a object for handling http request
type LoggingHandler struct {
	h http.Handler
}

func (lh LoggingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Infof("Handling http request: %s, from: %s, URI: %s", r.Method, r.RemoteAddr, r.RequestURI)
	lh.h.ServeHTTP(w, r)
}

// ServeIntrospection sets up ipamd introspection endpoints
func (c *IPAMContext) ServeIntrospection() {
	server := c.setupIntrospectionServer()
	for {
		_ = retry.WithBackoff(retry.NewSimpleBackoff(time.Second, time.Minute, 0.2, 2), func() error {
			var ln net.Listener
			var err error

			if strings.HasPrefix(server.Addr, "unix:") {
				socket := strings.TrimPrefix(server.Addr, "unix:")
				ln, err = net.Listen("unix", socket)
			} else {
				ln, err = net.Listen("tcp", server.Addr)
			}

			if err == nil {
				err = server.Serve(ln)
			}

			return err
		})
	}
}

func (c *IPAMContext) setupIntrospectionServer() *http.Server {
	serverFunctions := map[string]func(w http.ResponseWriter, r *http.Request){
		"/v1/enis":                      eniV1RequestHandler(c),
		"/v1/eni-configs":               eniConfigRequestHandler(c),
		"/v1/networkutils-env-settings": networkEnvV1RequestHandler(),
		"/v1/ipamd-env-settings":        ipamdEnvV1RequestHandler(),
	}
	paths := make([]string, 0, len(serverFunctions))
	for path := range serverFunctions {
		paths = append(paths, path)
	}
	availableCommands := &rootResponse{paths}
	// Autogenerated list of the above serverFunctions paths
	availableCommandResponse, err := json.Marshal(&availableCommands)

	if err != nil {
		log.Errorf("Failed to marshal: %v", err)
	}

	defaultHandler := func(w http.ResponseWriter, r *http.Request) {
		logErr(w.Write(availableCommandResponse))
	}
	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/", defaultHandler)
	for key, fn := range serverFunctions {
		serveMux.HandleFunc(key, fn)
	}

	// Log all requests and then pass through to serveMux
	loggingServeMux := http.NewServeMux()
	loggingServeMux.Handle("/", LoggingHandler{serveMux})

	addr, ok := os.LookupEnv(introspectionBindAddress)
	if !ok {
		addr = defaultIntrospectionBindAddress
	}

	log.Infof("Serving introspection endpoints on %s", addr)

	server := &http.Server{
		Addr:         addr,
		Handler:      loggingServeMux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	return server
}

func eniV1RequestHandler(ipam *IPAMContext) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		responseJSON, err := json.Marshal(ipam.dataStore[0].GetENIInfos())
		if err != nil {
			log.Errorf("Failed to marshal ENI data: %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		logErr(w.Write(responseJSON))
	}
}

func eniConfigRequestHandler(ipam *IPAMContext) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		node, err := k8sapi.GetNode(ctx, ipam.k8sClient)
		if err != nil {
			log.Errorf("Failed to get host node: %v", err)
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		myENIConfig, err := eniconfig.GetNodeSpecificENIConfigName(node)
		if err != nil {
			log.Errorf("Failed to get ENI config: %v", err)
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		responseJSON, err := json.Marshal(myENIConfig)
		if err != nil {
			log.Errorf("Failed to marshal ENI config: %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		logErr(w.Write(responseJSON))
	}
}

func networkEnvV1RequestHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		responseJSON, err := json.Marshal(networkutils.GetConfigForDebug())
		if err != nil {
			log.Errorf("Failed to marshal network env var data: %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		logErr(w.Write(responseJSON))
	}
}

func ipamdEnvV1RequestHandler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		responseJSON, err := json.Marshal(GetConfigForDebug())
		if err != nil {
			log.Errorf("Failed to marshal ipamd env var data: %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		logErr(w.Write(responseJSON))
	}
}

func logErr(_ int, err error) {
	if err != nil {
		log.Errorf("Write failed: %v", err)
	}
}
