package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/operator-framework/operator-sdk/pkg/leader"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	"github.com/operator-framework/operator-sdk/pkg/log/zap"
	"github.com/operator-framework/operator-sdk/pkg/metrics"
	"github.com/operator-framework/operator-sdk/pkg/restmapper"
	sdkVersion "github.com/operator-framework/operator-sdk/version"
	"github.com/spf13/pflag"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/controller/eniconfig"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/controller/node"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/controller/pod"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)

const (
	defaultLogFilePath = "stdout"; // TODO: restore to "/host/var/log/aws-routed-eni/ipamd.log"

	// Environment variable to disable the metrics endpoint on 61678
	envDisableMetrics = "DISABLE_METRICS"
)

// Change below variables to serve metrics on different host or port.
var (
	metricsHost       = "0.0.0.0"
	metricsPort int32 = 61678

)
var log = logf.Log.WithName("cmd")

var (
	version string
)

func printVersion() {
	log.Info(fmt.Sprintf("Starting L-IPAMD %s  ...", version))
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	log.Info(fmt.Sprintf("Version of operator-sdk: %v", sdkVersion.Version))
}

func main() {
	// Add the zap logger flag set to the CLI. The flag set must
	// be added before calling pflag.Parse().
	pflag.CommandLine.AddFlagSet(zap.FlagSet())

	// Add flags registered by imported packages (e.g. glog and
	// controller-runtime)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	pflag.Parse()

	// Use a zap logr.Logger implementation. If none of the zap
	// flags are configured (or if the zap flag set is not being
	// used), this defaults to a production zap logger.
	//
	// The logger instantiated here can be changed to any logger
	// implementing the logr.Logger interface. This logger will
	// be propagated through the whole operator, generating
	// uniform and structured logs.
	logf.SetLogger(zap.Logger())

	// TODO: unify logging
	logger.SetupLogger(logger.GetLogFileLocation(defaultLogFilePath))

	printVersion()

	namespace, err := k8sutil.GetWatchNamespace()
	if err != nil {
		log.Error(err, "Failed to get watch namespace")
		os.Exit(1)
	}

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	ctx := context.TODO()

	// Become the leader before proceeding
	err = leader.Become(ctx, fmt.Sprintf("amazon-vpc-cni-k8s-lock-%s", os.Getenv("MY_NODE_NAME")))
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Allow metrics to be disabled
	disableMetrics := utils.GetEnvBoolWithDefault(envDisableMetrics, false)
	metricsBindAddress := ""
	if !disableMetrics {
		metricsBindAddress = fmt.Sprintf("%s:%d", metricsHost, metricsPort)
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, manager.Options{
		Namespace:          namespace,
		MapperProvider:     restmapper.NewDynamicRESTMapper,
		MetricsBindAddress: metricsBindAddress,
	})
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	log.Info("Registering Components.")

	// Setup Scheme for all resources
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Setup Pod Controller
	podController, err := pod.Add(mgr)
	if err != nil {
		log.Error(err, "Pod Controller initialization failure")
		os.Exit(1)
	}

	// Setup ENIConfig Controller if using Custom Network Config
	eniConfigController := &eniconfig.ReconcileENIConfig{}
	if ipamd.UseCustomNetworkCfg() {
		nodeController, err := node.Add(mgr)
		if err != nil {
			log.Error(err, "Node Controller initialization failure")
			os.Exit(1)
		}

		eniConfigController, err = eniconfig.Add(mgr, nodeController)
		if err != nil {
			log.Error(err, "ENIConfig Controller initialization failure")
			os.Exit(1)
		}
	}

	// Create Service object to expose the metrics port.
	if !disableMetrics {
		_, err = metrics.ExposeMetricsPort(ctx, metricsPort)
		if err != nil {
			log.Info(err.Error())
		}
	}

	stopCh := signals.SetupSignalHandler()

	// Start the controller-runtime manager
	go func() {
		log.Info("Starting the controller-runtime manager")
		if err := mgr.Start(stopCh); err != nil {
			log.Error(err, "Manager exited non-zero")
			os.Exit(1)
		}
	}()

	// Initialise IPAMd
	awsK8sAgent, err := ipamd.New(podController, eniConfigController)
	if err != nil {
		log.Error(err, "Initialization failure")
		os.Exit(1)
	}

	// Pool manager
	go awsK8sAgent.StartNodeIPPoolManager()

	// CNI introspection endpoints
	go awsK8sAgent.ServeIntrospection()

	// gRPC handler
	go func() {
		err = awsK8sAgent.RunRPCHandler()
		if err != nil {
			log.Error(err, "Failed to set up gRPC handler")
			os.Exit(1)
		}
	}()

	<-stopCh
}
