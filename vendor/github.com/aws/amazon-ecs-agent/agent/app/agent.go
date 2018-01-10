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

package app

import (
	"errors"
	"fmt"

	"golang.org/x/net/context"

	acshandler "github.com/aws/amazon-ecs-agent/agent/acs/handler"
	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/api/ecsclient"
	"github.com/aws/amazon-ecs-agent/agent/app/factory"
	"github.com/aws/amazon-ecs-agent/agent/app/oswrapper"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/containermetadata"
	"github.com/aws/amazon-ecs-agent/agent/credentials"
	"github.com/aws/amazon-ecs-agent/agent/ec2"
	"github.com/aws/amazon-ecs-agent/agent/ecs_client/model/ecs"
	"github.com/aws/amazon-ecs-agent/agent/ecscni"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerclient"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/eni/pause"
	"github.com/aws/amazon-ecs-agent/agent/eventhandler"
	"github.com/aws/amazon-ecs-agent/agent/eventstream"
	"github.com/aws/amazon-ecs-agent/agent/handlers"
	credentialshandler "github.com/aws/amazon-ecs-agent/agent/handlers/credentials"
	"github.com/aws/amazon-ecs-agent/agent/resources"
	"github.com/aws/amazon-ecs-agent/agent/sighandlers"
	"github.com/aws/amazon-ecs-agent/agent/sighandlers/exitcodes"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/aws/amazon-ecs-agent/agent/tcs/handler"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	"github.com/aws/amazon-ecs-agent/agent/version"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	aws_credentials "github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/defaults"
	"github.com/cihub/seelog"
)

const (
	containerChangeEventStreamName             = "ContainerChange"
	deregisterContainerInstanceEventStreamName = "DeregisterContainerInstance"
	clusterMismatchErrorFormat                 = "Data mismatch; saved cluster '%v' does not match configured cluster '%v'. Perhaps you want to delete the configured checkpoint file?"
	instanceIDMismatchErrorFormat              = "Data mismatch; saved InstanceID '%s' does not match current InstanceID '%s'. Overwriting old datafile"
	instanceTypeMismatchErrorFormat            = "The current instance type does not match the registered instance type. Please revert the instance type change, or alternatively launch a new instance: %v"

	vpcIDAttributeName    = "ecs.vpc-id"
	subnetIDAttributeName = "ecs.subnet-id"
)

var (
	instanceNotLaunchedInVPCError = errors.New("instance not launched in VPC")
)

// agent interface is used by the app runner to interact with the ecsAgent
// object. Its purpose is to mostly demonstrate how to interact with the
// ecsAgent type.
type agent interface {
	// printVersion prints the Agent version string
	printVersion() int
	// printECSAttributes prints the Agent's capabilities based on
	// its environment
	printECSAttributes() int
	// startWindowsService starts the agent as a Windows Service
	startWindowsService() int
	// start starts the Agent execution
	start() int
	// setTerminationHandler sets the termination handler
	setTerminationHandler(sighandlers.TerminationHandler)
}

// ecsAgent wraps all the entities needed to start the ECS Agent execution.
// after creating it via
// the newAgent() method
type ecsAgent struct {
	ctx                   context.Context
	ec2MetadataClient     ec2.EC2MetadataClient
	cfg                   *config.Config
	dockerClient          engine.DockerClient
	containerInstanceARN  string
	credentialProvider    *aws_credentials.Credentials
	stateManagerFactory   factory.StateManager
	saveableOptionFactory factory.SaveableOption
	pauseLoader           pause.Loader
	cniClient             ecscni.CNIClient
	os                    oswrapper.OS
	vpc                   string
	subnet                string
	mac                   string
	metadataManager       containermetadata.Manager
	resource              resources.Resource
	terminationHandler    sighandlers.TerminationHandler
}

// newAgent returns a new ecsAgent object, but does not start anything
func newAgent(
	ctx context.Context,
	blackholeEC2Metadata bool,
	acceptInsecureCert *bool) (agent, error) {

	ec2MetadataClient := ec2.NewEC2MetadataClient(nil)
	if blackholeEC2Metadata {
		ec2MetadataClient = ec2.NewBlackholeEC2MetadataClient()
	}

	seelog.Info("Loading configuration")
	cfg, err := config.NewConfig(ec2MetadataClient)
	if err != nil {
		// All required config values can be inferred from EC2 Metadata,
		// so this error could be transient.
		seelog.Criticalf("Error loading config: %v", err)
		return nil, err
	}
	cfg.AcceptInsecureCert = aws.BoolValue(acceptInsecureCert)
	if cfg.AcceptInsecureCert {
		seelog.Warn("SSL certificate verification disabled. This is not recommended.")
	}
	seelog.Debugf("Loaded config: %s", cfg.String())

	dockerClient, err := engine.NewDockerGoClient(dockerclient.NewFactory(cfg.DockerEndpoint), cfg)
	if err != nil {
		// This is also non terminal in the current config
		seelog.Criticalf("Error creating Docker client: %v", err)
		return nil, err
	}

	var metadataManager containermetadata.Manager
	if cfg.ContainerMetadataEnabled {
		// We use the default API client for the metadata inspect call. This version has some information
		// missing which means if we need those fields later we will need to change this client to
		// the appropriate version
		metadataManager = containermetadata.NewManager(dockerClient, cfg)
	}

	return &ecsAgent{
		ctx:               ctx,
		ec2MetadataClient: ec2MetadataClient,
		cfg:               cfg,
		dockerClient:      dockerClient,
		// We instantiate our own credentialProvider for use in acs/tcs. This tries
		// to mimic roughly the way it's instantiated by the SDK for a default
		// session.
		credentialProvider:    defaults.CredChain(defaults.Config(), defaults.Handlers()),
		stateManagerFactory:   factory.NewStateManager(),
		saveableOptionFactory: factory.NewSaveableOption(),
		pauseLoader:           pause.New(),
		cniClient: ecscni.NewClient(&ecscni.Config{
			PluginsPath:            cfg.CNIPluginsPath,
			MinSupportedCNIVersion: config.DefaultMinSupportedCNIVersion,
		}),
		os:                 oswrapper.New(),
		metadataManager:    metadataManager,
		resource:           resources.New(),
		terminationHandler: sighandlers.StartDefaultTerminationHandler,
	}, nil
}

// printVersion prints the ECS Agent version string
func (agent *ecsAgent) printVersion() int {
	version.PrintVersion(agent.dockerClient)
	return exitcodes.ExitSuccess
}

// printECSAttributes prints the Agent's ECS Attributes based on its
// environment
func (agent *ecsAgent) printECSAttributes() int {
	capabilities, err := agent.capabilities()
	if err != nil {
		seelog.Warnf("Unable to obtain capabilit: %v", err)
		return exitcodes.ExitError
	}
	for _, attr := range capabilities {
		fmt.Printf("%s\t%s\n", aws.StringValue(attr.Name), aws.StringValue(attr.Value))
	}
	return exitcodes.ExitSuccess
}

func (agent *ecsAgent) setTerminationHandler(handler sighandlers.TerminationHandler) {
	agent.terminationHandler = handler
}

// start starts the ECS Agent
func (agent *ecsAgent) start() int {
	sighandlers.StartDebugHandler()

	containerChangeEventStream := eventstream.NewEventStream(containerChangeEventStreamName, agent.ctx)
	credentialsManager := credentials.NewManager()
	state := dockerstate.NewTaskEngineState()
	imageManager := engine.NewImageManager(agent.cfg, agent.dockerClient, state)
	client := ecsclient.NewECSClient(agent.credentialProvider, agent.cfg, agent.ec2MetadataClient)

	return agent.doStart(containerChangeEventStream, credentialsManager, state, imageManager, client)
}

// doStart is the worker invoked by start for starting the ECS Agent. This involves
// initializing the docker task engine, state saver, image manager, credentials
// manager, poll and telemetry sessions, api handler etc
func (agent *ecsAgent) doStart(containerChangeEventStream *eventstream.EventStream,
	credentialsManager credentials.Manager,
	state dockerstate.TaskEngineState,
	imageManager engine.ImageManager,
	client api.ECSClient) int {

	// Create the task engine
	taskEngine, currentEC2InstanceID, err := agent.newTaskEngine(containerChangeEventStream,
		credentialsManager, state, imageManager)
	if err != nil {
		return exitcodes.ExitTerminal
	}

	// Initialize the state manager
	stateManager, err := agent.newStateManager(taskEngine,
		&agent.cfg.Cluster, &agent.containerInstanceARN, &currentEC2InstanceID)
	if err != nil {
		seelog.Criticalf("Error creating state manager: %v", err)
		return exitcodes.ExitTerminal
	}

	// Conditionally create '/ecs' cgroup root
	if agent.cfg.TaskCPUMemLimit.Enabled() {
		err = agent.resource.Init()
		// When task CPU and memory limits are enabled, all tasks are placed
		// under the '/ecs' cgroup root.
		if err != nil {
			if agent.cfg.TaskCPUMemLimit == config.ExplicitlyEnabled {
				seelog.Criticalf("Unable to setup '/ecs' cgroup: %v", err)
				return exitcodes.ExitTerminal
			}
			seelog.Warnf("Disabling TaskCPUMemLimit because agent is unabled to setup '/ecs' cgroup: %v", err)
			agent.cfg.TaskCPUMemLimit = config.ExplicitlyDisabled
		}
	}

	var vpcSubnetAttributes []*ecs.Attribute
	// Check if Task ENI is enabled
	if agent.cfg.TaskENIEnabled {
		err, terminal := agent.initializeTaskENIDependencies(state, taskEngine)
		switch err {
		case nil:
			// No error, we can proceed with the rest of initialization
			// Set vpc and subnet id attributes
			vpcSubnetAttributes = agent.constructVPCSubnetAttributes()
		case instanceNotLaunchedInVPCError:
			// We have ascertained that the EC2 Instance is not running in a VPC
			// No need to stop the ECS Agent in this case; all we need to do is
			// to not update the config to disable the TaskENIEnabled flag and
			// move on
			seelog.Warnf("Unable to detect VPC ID for the Instance, disabling Task ENI capability: %v", err)
			agent.cfg.TaskENIEnabled = false
		default:
			// Encountered an error initializing dependencies for dealing with
			// ENIs for Tasks. Exit with the appropriate error code
			seelog.Criticalf("Unable to initialize Task ENI dependencies: %v", err)
			if terminal {
				return exitcodes.ExitTerminal
			}
			return exitcodes.ExitError
		}
	}

	// Register the container instance
	err = agent.registerContainerInstance(stateManager, client, vpcSubnetAttributes)
	if err != nil {
		if isTransient(err) {
			return exitcodes.ExitError
		}
		return exitcodes.ExitTerminal
	}
	// Add container instance ARN to metadata manager
	if agent.cfg.ContainerMetadataEnabled {
		agent.metadataManager.SetContainerInstanceARN(agent.containerInstanceARN)
	}

	// Begin listening to the docker daemon and saving changes
	taskEngine.SetSaver(stateManager)
	imageManager.SetSaver(stateManager)
	taskEngine.MustInit(agent.ctx)

	// Start back ground routines, including the telemetry session
	deregisterInstanceEventStream := eventstream.NewEventStream(
		deregisterContainerInstanceEventStreamName, agent.ctx)
	deregisterInstanceEventStream.StartListening()
	taskHandler := eventhandler.NewTaskHandler(agent.ctx, stateManager, state, client)
	agent.startAsyncRoutines(containerChangeEventStream, credentialsManager, imageManager,
		taskEngine, stateManager, deregisterInstanceEventStream, client, taskHandler)

	// Start the acs session, which should block doStart
	return agent.startACSSession(credentialsManager, taskEngine, stateManager,
		deregisterInstanceEventStream, client, state, taskHandler)
}

// newTaskEngine creates a new docker task engine object. It tries to load the
// local state if needed, else initializes a new one
func (agent *ecsAgent) newTaskEngine(containerChangeEventStream *eventstream.EventStream,
	credentialsManager credentials.Manager,
	state dockerstate.TaskEngineState,
	imageManager engine.ImageManager) (engine.TaskEngine, string, error) {

	containerChangeEventStream.StartListening()

	if !agent.cfg.Checkpoint {
		seelog.Info("Checkpointing not enabled; a new container instance will be created each time the agent is run")
		return engine.NewTaskEngine(agent.cfg, agent.dockerClient,
			credentialsManager, containerChangeEventStream, imageManager, state, agent.metadataManager), "", nil
	}

	// We try to set these values by loading the existing state file first
	var previousCluster, previousEC2InstanceID, previousContainerInstanceArn string
	previousTaskEngine := engine.NewTaskEngine(agent.cfg, agent.dockerClient,
		credentialsManager, containerChangeEventStream, imageManager, state, agent.metadataManager)

	// previousStateManager is used to verify that our current runtime configuration is
	// compatible with our past configuration as reflected by our state-file
	previousStateManager, err := agent.newStateManager(previousTaskEngine, &previousCluster,
		&previousContainerInstanceArn, &previousEC2InstanceID)
	if err != nil {
		seelog.Criticalf("Error creating state manager: %v", err)
		return nil, "", err
	}

	err = previousStateManager.Load()
	if err != nil {
		seelog.Criticalf("Error loading previously saved state: %v", err)
		return nil, "", err
	}

	err = agent.checkCompatibility(previousTaskEngine)
	if err != nil {
		seelog.Criticalf("Error checking compatibility with previously saved state: %v", err)
		return nil, "", err
	}

	currentEC2InstanceID := agent.getEC2InstanceID()
	if previousEC2InstanceID != "" && previousEC2InstanceID != currentEC2InstanceID {
		seelog.Warnf(instanceIDMismatchErrorFormat,
			previousEC2InstanceID, currentEC2InstanceID)

		// Reset agent state as a new container instance
		state.Reset()
		// Reset taskEngine; all the other values are still default
		return engine.NewTaskEngine(agent.cfg, agent.dockerClient, credentialsManager,
			containerChangeEventStream, imageManager, state, agent.metadataManager), currentEC2InstanceID, nil
	}

	if previousCluster != "" {
		if err := agent.setClusterInConfig(previousCluster); err != nil {
			return nil, "", err
		}
	}

	// Use the values we loaded if there's no issue
	agent.containerInstanceARN = previousContainerInstanceArn

	return previousTaskEngine, currentEC2InstanceID, nil
}

// setClusterInConfig sets the cluster name in the config object based on
// previous state. It returns an error if there's a mismatch between the
// the current cluster name with what's restored from the cluster state
func (agent *ecsAgent) setClusterInConfig(previousCluster string) error {
	// TODO Handle default cluster in a sane and unified way across the codebase
	configuredCluster := agent.cfg.Cluster
	if configuredCluster == "" {
		seelog.Debug("Setting cluster to default; none configured")
		configuredCluster = config.DefaultClusterName
	}
	if previousCluster != configuredCluster {
		err := clusterMismatchError{
			fmt.Errorf(clusterMismatchErrorFormat, previousCluster, configuredCluster),
		}
		seelog.Criticalf("%v", err)
		return err
	}
	agent.cfg.Cluster = previousCluster
	seelog.Infof("Restored cluster '%s'", agent.cfg.Cluster)

	return nil
}

// getEC2InstanceID gets the EC2 instance ID from the metadata service
func (agent *ecsAgent) getEC2InstanceID() string {
	instanceIdentityDoc, err := agent.ec2MetadataClient.InstanceIdentityDocument()
	if err != nil {
		seelog.Criticalf(
			"Unable to access EC2 Metadata service to determine EC2 ID: %v", err)
		return ""
	}
	return instanceIdentityDoc.InstanceID
}

// newStateManager creates a new state manager object for the task engine.
// Rest of the parameters are pointers and it's expected that all of these
// will be backfilled when state manager's Load() method is invoked
func (agent *ecsAgent) newStateManager(
	taskEngine engine.TaskEngine,
	cluster *string,
	containerInstanceArn *string,
	savedInstanceID *string) (statemanager.StateManager, error) {

	if !agent.cfg.Checkpoint {
		return statemanager.NewNoopStateManager(), nil
	}

	return agent.stateManagerFactory.NewStateManager(agent.cfg,
		statemanager.AddSaveable("TaskEngine", taskEngine),
		// This is for making testing easier as we can mock this
		agent.saveableOptionFactory.AddSaveable("ContainerInstanceArn",
			containerInstanceArn),
		agent.saveableOptionFactory.AddSaveable("Cluster", cluster),
		// This is for making testing easier as we can mock this
		agent.saveableOptionFactory.AddSaveable("EC2InstanceID", savedInstanceID),
	)
}

// constructVPCSubnetAttributes returns vpc and subnet IDs of the instance as
// an attribute list
func (agent *ecsAgent) constructVPCSubnetAttributes() []*ecs.Attribute {
	return []*ecs.Attribute{
		{
			Name:  aws.String(vpcIDAttributeName),
			Value: aws.String(agent.vpc),
		},
		{
			Name:  aws.String(subnetIDAttributeName),
			Value: aws.String(agent.subnet),
		},
	}
}

// registerContainerInstance registers the container instance ID for the ECS Agent
func (agent *ecsAgent) registerContainerInstance(
	stateManager statemanager.StateManager,
	client api.ECSClient,
	additionalAttributes []*ecs.Attribute) error {

	// Preflight request to make sure they're good
	if preflightCreds, err := agent.credentialProvider.Get(); err != nil || preflightCreds.AccessKeyID == "" {
		seelog.Warnf("Error getting valid credentials (AKID %s): %v", preflightCreds.AccessKeyID, err)
	}

	agentCapabilities, err := agent.capabilities()
	if err != nil {
		return err
	}
	capabilities := append(agentCapabilities, additionalAttributes...)

	if agent.containerInstanceARN != "" {
		seelog.Infof("Restored from checkpoint file. I am running as '%s' in cluster '%s'", agent.containerInstanceARN, agent.cfg.Cluster)
		return agent.reregisterContainerInstance(client, capabilities)
	}

	seelog.Info("Registering Instance with ECS")
	containerInstanceArn, err := client.RegisterContainerInstance("", capabilities)
	if err != nil {
		seelog.Errorf("Error registering: %v", err)
		if retriable, ok := err.(utils.Retriable); ok && !retriable.Retry() {
			return err
		}
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == ecs.ErrCodeInvalidParameterException {
			seelog.Critical("Instance registration attempt with an invalid parameter")
			return err
		}
		if _, ok := err.(utils.AttributeError); ok {
			seelog.Critical("Instance registration attempt with an invalid attribute")
			return err
		}
		return transientError{err}
	}
	seelog.Infof("Registration completed successfully. I am running as '%s' in cluster '%s'", containerInstanceArn, agent.cfg.Cluster)
	agent.containerInstanceARN = containerInstanceArn
	// Save our shiny new containerInstanceArn
	stateManager.Save()
	return nil
}

// reregisterContainerInstance registers a container instance that has already been
// registered with ECS. This is for cases where the ECS Agent is being restored
// from a check point.
func (agent *ecsAgent) reregisterContainerInstance(client api.ECSClient, capabilities []*ecs.Attribute) error {
	_, err := client.RegisterContainerInstance(agent.containerInstanceARN, capabilities)
	if err == nil {
		return nil
	}
	seelog.Errorf("Error re-registering: %v", err)
	if api.IsInstanceTypeChangedError(err) {
		seelog.Criticalf(instanceTypeMismatchErrorFormat, err)
		return err
	}
	if _, ok := err.(utils.AttributeError); ok {
		seelog.Critical("Instance re-registration attempt with an invalid attribute")
		return err
	}
	return transientError{err}
}

// startAsyncRoutines starts all of the background methods
func (agent *ecsAgent) startAsyncRoutines(
	containerChangeEventStream *eventstream.EventStream,
	credentialsManager credentials.Manager,
	imageManager engine.ImageManager,
	taskEngine engine.TaskEngine,
	stateManager statemanager.StateManager,
	deregisterInstanceEventStream *eventstream.EventStream,
	client api.ECSClient,
	taskHandler *eventhandler.TaskHandler) {

	// Start of the periodic image cleanup process
	if !agent.cfg.ImageCleanupDisabled {
		go imageManager.StartImageCleanupProcess(agent.ctx)
	}

	go agent.terminationHandler(stateManager, taskEngine)

	// Agent introspection api
	go handlers.ServeHttp(&agent.containerInstanceARN, taskEngine, agent.cfg)

	// Start serving the endpoint to fetch IAM Role credentials
	go credentialshandler.ServeHTTP(credentialsManager, agent.containerInstanceARN, agent.cfg)

	// Start sending events to the backend
	go eventhandler.HandleEngineEvents(taskEngine, client, taskHandler)

	telemetrySessionParams := tcshandler.TelemetrySessionParams{
		CredentialProvider:            agent.credentialProvider,
		Cfg:                           agent.cfg,
		ContainerInstanceArn:          agent.containerInstanceARN,
		DeregisterInstanceEventStream: deregisterInstanceEventStream,
		ContainerChangeEventStream:    containerChangeEventStream,
		DockerClient:                  agent.dockerClient,
		ECSClient:                     client,
		TaskEngine:                    taskEngine,
	}

	// Start metrics session in a go routine
	go tcshandler.StartMetricsSession(telemetrySessionParams)
}

// startACSSession starts a session with ECS's Agent Communication service. This
// is a blocking call and only returns when the handler returns
func (agent *ecsAgent) startACSSession(
	credentialsManager credentials.Manager,
	taskEngine engine.TaskEngine,
	stateManager statemanager.StateManager,
	deregisterInstanceEventStream *eventstream.EventStream,
	client api.ECSClient,
	state dockerstate.TaskEngineState,
	taskHandler *eventhandler.TaskHandler) int {

	acsSession := acshandler.NewSession(
		agent.ctx,
		agent.cfg,
		deregisterInstanceEventStream,
		agent.containerInstanceARN,
		agent.credentialProvider,
		client,
		state,
		stateManager,
		taskEngine,
		credentialsManager,
		taskHandler,
	)
	seelog.Info("Beginning Polling for updates")
	err := acsSession.Start()
	if err != nil {
		seelog.Criticalf("Unretriable error starting communicating with ACS: %v", err)
		return exitcodes.ExitTerminal
	}
	seelog.Critical("ACS Session handler should never exit")
	return exitcodes.ExitError
}
