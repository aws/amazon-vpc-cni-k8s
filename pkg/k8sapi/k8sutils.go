package k8sapi

import (
	"context"
	"fmt"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	eniconfigscheme "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	rcscheme "github.com/aws/amazon-vpc-resource-controller-k8s/apis/vpcresources/v1alpha1"
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
	awsNode = "aws-node"
)

var log = logger.Get()

// Get cache filters for IPAMD
func getIPAMDCacheFilters() map[client.Object]cache.ByObject {
	if nodeName := os.Getenv("MY_NODE_NAME"); nodeName != "" {
		return map[client.Object]cache.ByObject{
			&corev1.Pod{}: {
				Field: fields.Set{"spec.nodeName": nodeName}.AsSelector(),
			}}
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
	restCfg, err := getRestConfig(appName)
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
		return nil, err
	}
	// Start cache and wait for initial sync
	StartKubeClientCache(cacheReader)

	k8sClient, err := client.New(restCfg, client.Options{
		Cache: &client.CacheOptions{
			Reader: cacheReader,
		},
		Scheme: vpcCniScheme,
	})
	if err != nil {
		return nil, err
	}
	return k8sClient, nil
}

func GetKubeClientSet(appName string) (kubernetes.Interface, error) {
	// creates the in-cluster config
	config, err := getRestConfig(appName)
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

func CheckAPIServerConnectivity(appName string) error {
	restCfg, err := getRestConfig(appName)
	if err != nil {
		return err
	}
	restCfg.Timeout = 5 * time.Second
	clientSet, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("creating kube config, %w", err)
	}
	log.Infof("Testing communication with server")
	// Reconcile the API server query after waiting for a second, as the request
	// times out in one second if it fails to connect to the server
	return wait.PollImmediateInfinite(2*time.Second, func() (bool, error) {
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

func getRestConfig(appName string) (*rest.Config, error) {
	restCfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	restCfg.UserAgent = appName
	if endpoint, ok := os.LookupEnv("CLUSTER_ENDPOINT"); ok {
		restCfg.Host = endpoint
	}
	return restCfg, nil
}

func GetNode(ctx context.Context, k8sClient client.Client) (corev1.Node, error) {
	log.Infof("Get Node Info for: %s", os.Getenv("MY_NODE_NAME"))
	var node corev1.Node
	err := k8sClient.Get(ctx, types.NamespacedName{Name: os.Getenv("MY_NODE_NAME")}, &node)
	if err != nil {
		log.Errorf("error retrieving node: %s", err)
		return node, err
	}
	return node, nil
}
