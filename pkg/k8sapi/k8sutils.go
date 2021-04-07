package k8sapi

import (
	"fmt"
	eniconfigscheme "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = logger.Get()

// CreateKubeClient creates a k8s client
func CreateKubeClients() (client.Client, client.Client, error) {
	restCfg := ctrl.GetConfigOrDie()
	vpcCniScheme := runtime.NewScheme()
	clientgoscheme.AddToScheme(vpcCniScheme)
	eniconfigscheme.AddToScheme(vpcCniScheme)

	stopChan := ctrl.SetupSignalHandler()
	cache, err := cache.New(restCfg, cache.Options{Scheme: vpcCniScheme})
	if err != nil {
		return nil, nil, err
	}
	go func() {
		cache.Start(stopChan)
	}()
	cache.WaitForCacheSync(stopChan)

	standaloneK8SClient, err := client.New(restCfg, client.Options{Scheme: vpcCniScheme})
	k8sClient := client.DelegatingClient{
		Reader: &client.DelegatingReader{
			CacheReader:  cache,
			ClientReader: standaloneK8SClient,
		},
		Writer: standaloneK8SClient,
		StatusClient: standaloneK8SClient,
	}
	return standaloneK8SClient, k8sClient, nil
}

func GetKubeClientSet() (kubernetes.Interface, error) {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	// creates the clientset
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return clientSet, nil
}

func CheckAPIServerConnectivity() error {
	restCfg := ctrl.GetConfigOrDie()
    clientSet,_ := kubernetes.NewForConfig(restCfg)

	log.Infof("Testing communication with server")
	version, err := clientSet.Discovery().ServerVersion()
    if err !=nil {
		return fmt.Errorf("error communicating with apiserver: %v", err)
	}
	log.Infof("Successful communication with the Cluster! Cluster Version is: v%s.%s. git version: %s. git tree state: %s. commit: %s. platform: %s",
		version.Major, version.Minor, version.GitVersion, version.GitTreeState, version.GitCommit, version.Platform)

    return nil
}


