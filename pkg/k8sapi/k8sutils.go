package k8sapi

import (
	"fmt"

	eniconfigscheme "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = logger.Get()
var eventRecorder = initBroadcast()

// CreateKubeClient creates a k8s client
func CreateKubeClient() (client.Client, error) {
	restCfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	vpcCniScheme := runtime.NewScheme()
	clientgoscheme.AddToScheme(vpcCniScheme)
	eniconfigscheme.AddToScheme(vpcCniScheme)

	rawK8SClient, err := client.New(restCfg, client.Options{Scheme: vpcCniScheme})
	if err != nil {
		return nil, err
	}

	return rawK8SClient, nil
}

// CreateKubeClient creates a k8s client
func CreateCachedKubeClient(rawK8SClient client.Client) (client.Client, error) {
	restCfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	vpcCniScheme := runtime.NewScheme()
	clientgoscheme.AddToScheme(vpcCniScheme)
	eniconfigscheme.AddToScheme(vpcCniScheme)

	stopChan := ctrl.SetupSignalHandler()
	cache, err := cache.New(restCfg, cache.Options{Scheme: vpcCniScheme})
	if err != nil {
		return nil, err
	}
	go func() {
		cache.Start(stopChan)
	}()
	cache.WaitForCacheSync(stopChan)

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

func initBroadcast() record.EventRecorder {
	restCfg, err := ctrl.GetConfig()
	if err != nil {
		panic(err.Error())
	}
	clientSet, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		panic(err.Error())
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: clientSet.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(clientgoscheme.Scheme, v1.EventSource{
		Component: "aws-node",
	})
	return recorder
}

func BroadcastEvent(object runtime.Object, reason string, message string, eventType string) {
	eventRecorder.Event(object, eventType, reason, message)
}
