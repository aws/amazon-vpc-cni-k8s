package k8sapi

import (
	"fmt"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	eniconfigscheme "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = logger.Get()

func InitializeRestMapper() (meta.RESTMapper, error) {
	restCfg, err := ctrl.GetConfig()
	restCfg.Burst = 200
	if err != nil {
		return nil, err
	}
	mapper, err := apiutil.NewDynamicRESTMapper(restCfg)
	if err != nil {
		return nil, err
	}
	return mapper, nil
}

// CreateKubeClient creates a k8s client
func CreateKubeClient(mapper meta.RESTMapper) (client.Client, error) {
	restCfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	vpcCniScheme := runtime.NewScheme()
	clientgoscheme.AddToScheme(vpcCniScheme)
	eniconfigscheme.AddToScheme(vpcCniScheme)

	rawK8SClient, err := client.New(restCfg, client.Options{Scheme: vpcCniScheme, Mapper: mapper})

	if err != nil {
		return nil, err
	}

	return rawK8SClient, nil
}

// CreateKubeClient creates a k8s client
func CreateCachedKubeClient(rawK8SClient client.Client, mapper meta.RESTMapper) (client.Client, error) {
	restCfg, err := ctrl.GetConfig()
	restCfg.Burst = 100

	if err != nil {
		return nil, err
	}
	vpcCniScheme := runtime.NewScheme()
	clientgoscheme.AddToScheme(vpcCniScheme)
	eniconfigscheme.AddToScheme(vpcCniScheme)

	stopChan := ctrl.SetupSignalHandler()
	cache, err := cache.New(restCfg, cache.Options{Scheme: vpcCniScheme, Mapper: mapper})
	if err != nil {
		return nil, err
	}
	go func() {
		cache.Start(stopChan)
	}()
	cache.WaitForCacheSync(stopChan)

	cachedK8SClient := client.NewDelegatingClientInput{
		CacheReader: cache,
		Client:      rawK8SClient,
	}

	returnedCachedK8SClient, err := client.NewDelegatingClient(cachedK8SClient)
	if err != nil {
		return nil, err
	}
	return returnedCachedK8SClient, nil
}
func GetKubeClientSet() (kubernetes.Interface, error) {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
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
	restCfg, err := ctrl.GetConfig()
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
