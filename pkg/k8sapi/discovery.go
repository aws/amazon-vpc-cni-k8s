// Package k8sapi contains logic to retrieve pods running on local node
package k8sapi

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/pkg/errors"

	log "github.com/cihub/seelog"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type controller struct {
	indexer  cache.Indexer
	queue    workqueue.RateLimitingInterface
	informer cache.Controller
}

// K8SAPIs defines interface to use kubelet introspection API
type K8SAPIs interface {
	K8SGetLocalPodIPs() ([]*K8SPodInfo, error)
}

// K8SPodInfo provides pod info
type K8SPodInfo struct {
	// Name is pod's name
	Name string
	// Namespace is pod's namespace
	Namespace string
	// Container is pod's container id
	Container string
	// IP is pod's ipv4 address
	IP  string
	UID string
}

// ErrInformerNotSynced indicates that it has not synced with API server yet
var ErrInformerNotSynced = errors.New("discovery: informer not synced")

// Controller defines global context for discovery controller
type Controller struct {
	workerPods     map[string]*K8SPodInfo
	workerPodsLock sync.RWMutex

	controller *controller
	kubeClient kubernetes.Interface
	myNodeName string
	synced     bool
}

// NewController creates a new DiscoveryController
func NewController(clientset kubernetes.Interface) *Controller {
	return &Controller{kubeClient: clientset,
		myNodeName: os.Getenv("MY_NODE_NAME"),
		workerPods: make(map[string]*K8SPodInfo)}
}

// CreateKubeClient creates a k8s client
func CreateKubeClient(apiserver string, kubeconfig string) (clientset.Interface, error) {
	config, err := clientcmd.BuildConfigFromFlags(apiserver, kubeconfig)
	if err != nil {
		return nil, err
	}

	kubeClient, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	// Informers don't seem to do a good job logging error messages when it
	// can't reach the server, making debugging hard. This makes it easier to
	// figure out if apiserver is configured incorrectly.
	log.Infof("Testing communication with server")
	v, err := kubeClient.Discovery().ServerVersion()
	if err != nil {
		errMsg := "Failed to communicate with K8S Server. Please check instance security groups or http proxy setting"
		log.Infof(errMsg)
		fmt.Printf(errMsg)
		return nil, fmt.Errorf("error communicating with apiserver: %v", err)
	}
	log.Infof("Running with Kubernetes cluster version: v%s.%s. git version: %s. git tree state: %s. commit: %s. platform: %s",
		v.Major, v.Minor, v.GitVersion, v.GitTreeState, v.GitCommit, v.Platform)
	log.Info("Communication with server successful")

	return kubeClient, nil
}

// DiscoverK8SPods discovers Pods running in the cluster
func (d *Controller) DiscoverK8SPods() {
	// create the pod watcher
	podListWatcher := cache.NewListWatchFromClient(d.kubeClient.CoreV1().RESTClient(), "pods", metav1.NamespaceAll, fields.Everything())

	// create the workqueue
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	// Bind the workqueue to a cache with the help of an informer. This way we make sure that
	// whenever the cache is updated, the pod key is added to the workqueue.
	// Note that when we finally process the item from the workqueue, we might see a newer version
	// of the Pod than the version which was responsible for triggering the update.
	indexer, informer := cache.NewIndexerInformer(podListWatcher, &v1.Pod{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			// IndexerInformer uses a delta queue, therefore for deletes we have to use this
			// key function.
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
	}, cache.Indexers{})

	d.controller = newController(queue, indexer, informer)

	// Now let's start the controller
	stop := make(chan struct{})
	defer close(stop)
	go d.run(1, stop)

	// Wait forever
	select {}
}

// K8SGetLocalPodIPs return the list of pods running on the local nodes
func (d *Controller) K8SGetLocalPodIPs() ([]*K8SPodInfo, error) {
	var localPods []*K8SPodInfo

	if !d.synced {
		log.Info("GetLocalPods: informer not synced yet")
		return nil, ErrInformerNotSynced
	}

	log.Debug("GetLocalPods start ...")
	d.workerPodsLock.Lock()
	defer d.workerPodsLock.Unlock()

	for _, pod := range d.workerPods {
		log.Infof("K8SGetLocalPodIPs discovered local Pods: %s %s %s %s",
			pod.Name, pod.Namespace, pod.IP, pod.UID)
		localPods = append(localPods, pod)
	}

	return localPods, nil
}

// The rest of logic/code are taken from kubernetes/client-go/examples/workqueue
func newController(queue workqueue.RateLimitingInterface, indexer cache.Indexer, informer cache.Controller) *controller {
	return &controller{
		informer: informer,
		indexer:  indexer,
		queue:    queue,
	}
}

func (d *Controller) processNextItem() bool {
	// Wait until there is a new item in the working queue
	key, quit := d.controller.queue.Get()
	if quit {
		return false
	}
	// Tell the queue that we are done with processing this key. This unblocks the key for other workers
	// This allows safe parallel processing because two pods with the same key are never processed in
	// parallel.
	defer d.controller.queue.Done(key)

	// Invoke the method containing the business logic
	err := d.handlePodUpdate(key.(string))
	// Handle the error if something went wrong during the execution of the business logic
	d.controller.handleErr(err, key)
	return true
}

func (d *Controller) handlePodUpdate(key string) error {
	obj, exists, err := d.controller.indexer.GetByKey(key)
	if err != nil {
		log.Errorf("fetching object with key %s from store failed with %v", key, err)
		return err
	}

	if !exists {
		log.Infof(" Pods deleted on my node: %v", key)
		d.workerPodsLock.Lock()
		defer d.workerPodsLock.Unlock()
		delete(d.workerPods, key)
		return nil
	}

	pod, ok := obj.(*v1.Pod)
	if !ok || pod == nil {
		log.Errorf("updated object received was not a pod: %+v", obj)
		return errors.New("received a non-pod object update")
	}
	// Note that you also have to check the uid if you have a local controlled resource, which
	// is dependent on the actual instance, to detect that a Pod was recreated with the same name
	podName := pod.GetName()

	// check to see if this is one of worker pod on my nodes
	if d.myNodeName == pod.Spec.NodeName && !pod.Spec.HostNetwork {
		d.workerPodsLock.Lock()
		defer d.workerPodsLock.Unlock()

		d.workerPods[key] = &K8SPodInfo{
			Name:      podName,
			Namespace: pod.GetNamespace(),
			UID:       string(pod.GetUID()),
			IP:        pod.Status.PodIP,
		}

		log.Infof(" Add/Update for Pod %s on my node, namespace = %s, IP = %s", podName, d.workerPods[key].Namespace, d.workerPods[key].IP)
	}
	return nil
}

// handleErr checks if an error happened and makes sure we will retry later.
func (c *controller) handleErr(err error, key interface{}) {
	if err == nil {
		// Forget about the #AddRateLimited history of the key on every successful synchronization.
		// This ensures that future processing of updates for this key is not delayed because of
		// an outdated error history.
		c.queue.Forget(key)
		return
	}

	// This controller retries 5 times if something goes wrong. After that, it stops trying.
	if c.queue.NumRequeues(key) < 5 {
		log.Infof("Error syncing pod %v: %v", key, err)

		// Re-enqueue the key rate limited. Based on the rate limiter on the
		// queue and the re-enqueue history, the key will be processed later again.
		c.queue.AddRateLimited(key)
		return
	}

	c.queue.Forget(key)
	// Report to an external entity that, even after several retries, we could not successfully process this key
	runtime.HandleError(err)
	log.Infof("Dropping pod %q out of the queue: %v", key, err)
}

func (d *Controller) run(threadiness int, stopCh chan struct{}) {

	// Let the workers stop when we are done
	defer d.controller.queue.ShutDown()
	log.Info("Starting Pod controller")

	go d.controller.informer.Run(stopCh)

	// Wait for all involved caches to be synced, before processing items from the queue is started
	if !cache.WaitForCacheSync(stopCh, d.controller.informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}

	log.Info("Synced successfully with APIServer")
	d.synced = true

	for i := 0; i < threadiness; i++ {
		go wait.Until(d.runWorker, time.Second, stopCh)
	}

	<-stopCh
	log.Info("Stopping Pod controller")
}

func (d *Controller) runWorker() {
	for d.processNextItem() {
	}
}
