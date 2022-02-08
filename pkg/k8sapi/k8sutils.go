package k8sapi

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	eniconfigscheme "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"k8s.io/apimachinery/pkg/runtime"
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
	corev1.AddToScheme(vpcCniScheme)
	eniconfigscheme.AddToScheme(vpcCniScheme)

	log.Infof(fmt.Sprintf("raw client starting"))
	rawK8SClient, err := client.New(restCfg, client.Options{Scheme: vpcCniScheme, Mapper: mapper})
	log.Infof(fmt.Sprintf("raw client started"))

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
	log.Infof(fmt.Sprintf("cache client starting"))
	cache, err := cache.New(restCfg, cache.Options{Scheme: vpcCniScheme, Mapper: mapper})
	log.Infof(fmt.Sprintf("cache client started"))
	if err != nil {
		return nil, err
	}
	go func() {
		log.Infof(fmt.Sprintf("cache starting"))
		cache.Start(stopChan)
	}()
	_result := cache.WaitForCacheSync(stopChan)
	log.Infof(fmt.Sprintf("cache complete"))
	log.Infof(fmt.Sprintf("%f", _result))

	cachedK8SClient := client.DelegatingClient{
		Reader: &client.DelegatingReader{
			CacheReader:  cache,
			ClientReader: rawK8SClient,
		},
		Writer:       rawK8SClient,
		StatusClient: rawK8SClient,
	}
	return cachedK8SClient, nil
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
	clientSet, _ := kubernetes.NewForConfig(restCfg)

	log.Infof("Testing communication with server")
	version, err := clientSet.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("error communicating with apiserver: %v", err)
	}
	log.Infof("Successful communication with the Cluster! Cluster Version is: v%s.%s. git version: %s. git tree state: %s. commit: %s. platform: %s",
		version.Major, version.Minor, version.GitVersion, version.GitTreeState, version.GitCommit, version.Platform)

	return nil
}
