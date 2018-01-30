// Copyright 2014-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

// Package handlers deals with the agent introspection api.
package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/logger"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	"github.com/aws/amazon-ecs-agent/agent/version"
)

var log = logger.ForModule("Handlers")

const (
	dockerIdQueryField = "dockerid"
	taskArnQueryField  = "taskarn"
	dockerShortIdLen   = 12
)

type rootResponse struct {
	AvailableCommands []string
}

// ValueFromRequest returns the value of a field in the http request. The boolean value is
// set to true if the field exists in the query.
func ValueFromRequest(r *http.Request, field string) (string, bool) {
	values := r.URL.Query()
	_, exists := values[field]
	return values.Get(field), exists
}

func metadataV1RequestHandlerMaker(containerInstanceArn *string, cfg *config.Config) func(http.ResponseWriter, *http.Request) {
	resp := &MetadataResponse{
		Cluster:              cfg.Cluster,
		ContainerInstanceArn: containerInstanceArn,
		Version:              version.String(),
	}
	responseJSON, _ := json.Marshal(resp)

	return func(w http.ResponseWriter, r *http.Request) {
		w.Write(responseJSON)
	}
}

func newTaskResponse(task *api.Task, containerMap map[string]*api.DockerContainer) *TaskResponse {
	containers := []ContainerResponse{}
	for containerName, container := range containerMap {
		if container.Container.IsInternal() {
			continue
		}
		containers = append(containers, ContainerResponse{container.DockerID, container.DockerName, containerName})
	}

	knownStatus := task.GetKnownStatus()
	knownBackendStatus := knownStatus.BackendStatus()
	desiredStatusInAgent := task.GetDesiredStatus()
	desiredStatus := desiredStatusInAgent.BackendStatus()

	if (knownBackendStatus == "STOPPED" && desiredStatus != "STOPPED") || (knownBackendStatus == "RUNNING" && desiredStatus == "PENDING") {
		desiredStatus = ""
	}

	return &TaskResponse{
		Arn:           task.Arn,
		DesiredStatus: desiredStatus,
		KnownStatus:   knownBackendStatus,
		Family:        task.Family,
		Version:       task.Version,
		Containers:    containers,
	}
}

func newTasksResponse(state dockerstate.TaskEngineState) *TasksResponse {
	allTasks := state.AllTasks()
	taskResponses := make([]*TaskResponse, len(allTasks))
	for ndx, task := range allTasks {
		containerMap, _ := state.ContainerMapByArn(task.Arn)
		taskResponses[ndx] = newTaskResponse(task, containerMap)
	}

	return &TasksResponse{Tasks: taskResponses}
}

// Creates JSON response and sets the http status code for the task queried.
func createTaskJSONResponse(task *api.Task, found bool, resourceId string, state dockerstate.TaskEngineState) ([]byte, int) {
	var responseJSON []byte
	status := http.StatusOK
	if found {
		containerMap, _ := state.ContainerMapByArn(task.Arn)
		responseJSON, _ = json.Marshal(newTaskResponse(task, containerMap))
	} else {
		log.Warn("Could not find requested resource: " + resourceId)
		responseJSON, _ = json.Marshal(&TaskResponse{})
		status = http.StatusNotFound
	}
	return responseJSON, status
}

// Creates response for the 'v1/tasks' API. Lists all tasks if the request
// doesn't contain any fields. Returns a Task if either of 'dockerid' or
// 'taskarn' are specified in the request.
func tasksV1RequestHandlerMaker(taskEngine DockerStateResolver) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var responseJSON []byte
		dockerTaskEngineState := taskEngine.State()
		dockerId, dockerIdExists := ValueFromRequest(r, dockerIdQueryField)
		taskArn, taskArnExists := ValueFromRequest(r, taskArnQueryField)
		var status int
		if dockerIdExists && taskArnExists {
			log.Info("Request contains both ", dockerIdQueryField, " and ", taskArnQueryField, ". Expect at most one of these.")
			w.WriteHeader(http.StatusBadRequest)
			w.Write(responseJSON)
			return
		}
		if dockerIdExists {
			// Create TaskResponse for the docker id in the query.
			var task *api.Task
			var found bool
			if len(dockerId) > dockerShortIdLen {
				task, found = dockerTaskEngineState.TaskByID(dockerId)
			} else {
				tasks, _ := dockerTaskEngineState.TaskByShortID(dockerId)
				if len(tasks) == 0 {
					task = nil
					found = false
				} else if len(tasks) == 1 {
					task = tasks[0]
					found = true
				} else {
					log.Info("Multiple tasks found for requested dockerId: " + dockerId)
					w.WriteHeader(http.StatusBadRequest)
					w.Write(responseJSON)
					return
				}
			}
			responseJSON, status = createTaskJSONResponse(task, found, dockerId, dockerTaskEngineState)
			w.WriteHeader(status)
		} else if taskArnExists {
			// Create TaskResponse for the task arn in the query.
			task, found := dockerTaskEngineState.TaskByArn(taskArn)
			responseJSON, status = createTaskJSONResponse(task, found, taskArn, dockerTaskEngineState)
			w.WriteHeader(status)
		} else {
			// List all tasks.
			responseJSON, _ = json.Marshal(newTasksResponse(dockerTaskEngineState))
		}
		w.Write(responseJSON)
	}
}

var licenseProvider = utils.NewLicenseProvider()

func licenseHandler(w http.ResponseWriter, h *http.Request) {
	text, err := licenseProvider.GetText()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.Write([]byte(text))
	}
}

func setupServer(containerInstanceArn *string, taskEngine DockerStateResolver, cfg *config.Config) *http.Server {
	serverFunctions := map[string]func(w http.ResponseWriter, r *http.Request){
		"/v1/metadata": metadataV1RequestHandlerMaker(containerInstanceArn, cfg),
		"/v1/tasks":    tasksV1RequestHandlerMaker(taskEngine),
		"/license":     licenseHandler,
	}

	paths := make([]string, 0, len(serverFunctions))
	for path := range serverFunctions {
		paths = append(paths, path)
	}
	availableCommands := &rootResponse{paths}
	// Autogenerated list of the above serverFunctions paths
	availableCommandResponse, _ := json.Marshal(&availableCommands)

	defaultHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Write(availableCommandResponse)
	}

	serverMux := http.NewServeMux()
	serverMux.HandleFunc("/", defaultHandler)
	for key, fn := range serverFunctions {
		serverMux.HandleFunc(key, fn)
	}

	// Log all requests and then pass through to serverMux
	loggingServeMux := http.NewServeMux()
	loggingServeMux.Handle("/", LoggingHandler{serverMux})

	server := &http.Server{
		Addr:         ":" + strconv.Itoa(config.AgentIntrospectionPort),
		Handler:      loggingServeMux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	return server
}

// ServeHttp serves information about this agent / containerInstance and tasks
// running on it.
func ServeHttp(containerInstanceArn *string, taskEngine engine.TaskEngine, cfg *config.Config) {
	// Is this the right level to type assert, assuming we'd abstract multiple taskengines here?
	// Revisit if we ever add another type..
	dockerTaskEngine := taskEngine.(*engine.DockerTaskEngine)

	server := setupServer(containerInstanceArn, dockerTaskEngine, cfg)
	for {
		once := sync.Once{}
		utils.RetryWithBackoff(utils.NewSimpleBackoff(time.Second, time.Minute, 0.2, 2), func() error {
			// TODO, make this cancellable and use the passed in context; for
			// now, not critical if this gets interrupted
			err := server.ListenAndServe()
			once.Do(func() {
				log.Error("Error running http api", "err", err)
			})
			return err
		})
	}
}
