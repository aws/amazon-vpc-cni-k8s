package k8sapi

import (
	"context"
	"fmt"
	"os"
	"time"

	eniconfigscheme "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/amazon-vpc-cni-k8s/utils"
	rcscheme "github.com/aws/amazon-vpc-resource-controller-k8s/apis/vpcresources/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	cache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

const (
	awsNode         = "aws-node"
	envEnablePodENI = "ENABLE_POD_ENI"
	restCfgTimeout  = 5 * time.Second
)

var log = logger.Get()

// Get cache filters for IPAMD
func getIPAMDCacheFilters() map[client.Object]cache.ByObject {
	if nodeName := os.Getenv("MY_NODE_NAME"); nodeName != "" {
		filter := map[client.Object]cache.ByObject{
			&corev1.Pod{}: {
				Field: fields.Set{"spec.nodeName": nodeName}.AsSelector(),
			},
			&corev1.Node{}: {
				Field: fields.Set{"metadata.name": nodeName}.AsSelector(),
			},
			&rcscheme.CNINode{}: {
				Field: fields.Set{"metadata.name": nodeName}.AsSelector(),
			},
		}
		// only cache CNINode when SGP is in use
		enabledPodENI := utils.GetBoolAsStringEnvVar(envEnablePodENI, false)
		if enabledPodENI {
			log.Infof("SGP is in use, adding CNINode to cache.")
			filter[&rcscheme.CNINode{}] = cache.ByObject{
				Field: fields.Set{"metadata.name": nodeName}.AsSelector(),
			}
		}
		return filter
	}
	return nil
}

// Get cache filters for CNI Metrics Helper
func getMetricsHelperCacheFilters() map[client.Object]cache.ByObject {
	return map[client.Object]cache.ByObject{
		&corev1.Pod{}: {
			Label: labels.Set(map[string]string{
				"k8s-app": awsNode}).AsSelector(),
		}}
}

// Create cache reader for Kubernetes client
func CreateKubeClientCache(restCfg *rest.Config, scheme *runtime.Scheme, filterMap map[client.Object]cache.ByObject) (cache.Cache, error) {
	// Get HTTP client and REST mapper for cache
	httpClient, err := rest.HTTPClientFor(restCfg)
	if err != nil {
		return nil, err
	}
	mapper, err := apiutil.NewDynamicRESTMapper(restCfg, httpClient)
	if err != nil {
		return nil, err
	}

	// Create a cache for the client to read from in order to decrease the number of API server calls.
	cacheOptions := cache.Options{
		ByObject: filterMap,
		Mapper:   mapper,
		Scheme:   scheme,
	}

	cache, err := cache.New(restCfg, cacheOptions)
	if err != nil {
		return nil, err
	}
	return cache, nil
}

func StartKubeClientCache(cache cache.Cache) {
	stopChan := ctrl.SetupSignalHandler()
	go func() {
		cache.Start(stopChan)
	}()
	cache.WaitForCacheSync(stopChan)
}

// CreateKubeClient creates a k8s client
func CreateKubeClient(appName string) (client.Client, error) {
	restCfg, err := GetRestConfig()
	if err != nil {
		return nil, err
	}

	// The scheme should only contain GVKs that the client will access.
	vpcCniScheme := runtime.NewScheme()
	corev1.AddToScheme(vpcCniScheme)
	eniconfigscheme.AddToScheme(vpcCniScheme)
	rcscheme.AddToScheme(vpcCniScheme)

	var filterMap map[client.Object]cache.ByObject
	if appName == awsNode {
		filterMap = getIPAMDCacheFilters()
	} else {
		filterMap = getMetricsHelperCacheFilters()
	}
	cacheReader, err := CreateKubeClientCache(restCfg, vpcCniScheme, filterMap)
	if err != nil {
		log.Warnf("Skipping cache-based Kubernetes client: %s", err)
		cacheReader = nil
	}

	clientOpts := client.Options{Scheme: vpcCniScheme}
	if cacheReader != nil {
		log.Info("Cache-based Kubernetes client successfully created.")
		StartKubeClientCache(cacheReader)
		clientOpts.Cache = &client.CacheOptions{Reader: cacheReader}
	} else {
		log.Warn("Running Kubernetes client in direct mode (no cache)")
	}

	k8sClient, err := client.New(restCfg, clientOpts)
	if err != nil {
		return nil, err
	}
	log.Info("k8sClient created successfully")
	return k8sClient, nil
}

func GetKubeClientSet() (kubernetes.Interface, error) {
	// creates the in-cluster config
	config, err := GetRestConfig()
	if err != nil {
		return nil, err
	}

	// creates the clientset
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return clientSet, nil
}

func CheckAPIServerConnectivity() error {
	restCfg, err := GetRestConfig()
	if err != nil {
		return err
	}
	restCfg.Timeout = restCfgTimeout
	clientSet, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("creating kube config, %w", err)
	}
	log.Infof("Testing communication with server")
	// Reconcile the API server query after waiting for a second, as the request
	// times out in one second if it fails to connect to the server
	return wait.PollUntilContextCancel(context.Background(), 2*time.Second, true, func(ctx context.Context) (bool, error) {
		version, err := clientSet.Discovery().ServerVersion()
		if err != nil {
			// When times out return no error, so the PollInfinite will retry with the given interval
			if os.IsTimeout(err) {
				log.Errorf("Unable to reach API Server, %v", err)
				return false, nil
			}
			return false, fmt.Errorf("error communicating with apiserver: %v", err)
		}
		log.Infof("Successful communication with the Cluster! Cluster Version is: v%s.%s. git version: %s. git tree state: %s. commit: %s. platform: %s",
			version.Major, version.Minor, version.GitVersion, version.GitTreeState, version.GitCommit, version.Platform)
		return true, nil
	})
}

func CheckAPIServerConnectivityWithTimeout(pollInterval time.Duration, pollTimeout time.Duration) error {
	restCfg, err := GetRestConfig()
	if err != nil {
		return err
	}
	// timeout for each connect try
	restCfg.Timeout = restCfgTimeout
	clientSet, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("creating kube config, %w", err)
	}

	log.Info("Testing communication with server ...")

	return wait.PollImmediate(pollInterval, pollTimeout, func() (bool, error) {
		version, err := clientSet.Discovery().ServerVersion()
		if err != nil {
			log.Errorf("Unable to reach API Server: %v", err)
			return false, nil // Retry
		}

		log.Infof("Successful communication with the Cluster! Cluster Version is: %s", version.GitVersion)
		return true, nil
	})
}

// GetRestConfig returns a Kubernetes REST config for API interactions
func GetRestConfig() (*rest.Config, error) {
	restCfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	restCfg.UserAgent = os.Args[0] + "-" + utils.GetEnv("VPC_CNI_VERSION", "")
	if endpoint, ok := os.LookupEnv("CLUSTER_ENDPOINT"); ok {
		restCfg.Host = endpoint
	}
	return restCfg, nil
}

func GetNode(ctx context.Context, k8sClient client.Client) (corev1.Node, error) {
	nodeName := os.Getenv("MY_NODE_NAME")
	log.Infof("Get Node Info for: %s", nodeName)

	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
	}

	// If API server is unavailable, return immediately
	if k8sClient == nil {
		log.Warnf("Skipping GetNode() as Kubernetes API client is unavailable.")
		return node, fmt.Errorf("Kubernetes API client is not available")
	}

	// Create a context with timeout to avoid hanging indefinitely
	apiCtx, cancel := context.WithTimeout(ctx, 3*time.Second) // Set 3-second timeout
	defer cancel()

	err := k8sClient.Get(apiCtx, types.NamespacedName{Name: nodeName}, &node)
	if err != nil {
		klog.Errorf("Failed to get node %s: %v", nodeName, err)
		return node, err
	}

	return node, nil
}
