package pod

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

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

const (
	envCustomCniPodName = "AWS_CNI_POD_NAME"
)

var cniPodName = getCniPodName()

var log = logf.Log.WithName("controller_pod")

// ErrInformerNotSynced indicates that it has not synced with API server yet
var ErrInformerNotSynced = errors.New("discovery: informer not synced")

// Allow CNI Pod Prefix to be customised with env var
func getCniPodName() string {
	if value, ok := os.LookupEnv(envCustomCniPodName); ok {
		return value
	}
	return "aws-node"
}

// Add creates a new Pod Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) (*ReconcilePod, error) {
	r := NewReconciler(mgr)
	return r, add(mgr, r)
}

// newReconciler returns a new ReconcilePod
func NewReconciler(mgr manager.Manager) *ReconcilePod {
	return &ReconcilePod{
		client:     mgr.GetClient(),
		scheme:     mgr.GetScheme(),
		myNodeName: os.Getenv("MY_NODE_NAME"),
		workerPods: make(map[types.NamespacedName]*K8SPodInfo),
		cniPods:    make(map[types.NamespacedName]string),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("pod-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Pod
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Tell cache to index nodeName field on Pod resource
	err = mgr.GetFieldIndexer().IndexField(&corev1.Pod{}, "spec.nodeName", func(o runtime.Object) []string {
		var pod *corev1.Pod
		pod, ok := o.(*corev1.Pod)
		if ok {
			return []string{pod.Spec.NodeName}
		}
		return []string{}
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcilePod implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcilePod{}

// ReconcilePod reconciles a Pod object
type ReconcilePod struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme

	workerPods     map[types.NamespacedName]*K8SPodInfo
	workerPodsLock sync.RWMutex

	cniPods     map[types.NamespacedName]string
	cniPodsLock sync.RWMutex

	myNodeName string
	synced     bool
}

// Reconcile reads that state of the cluster for a Pod object and makes changes based on the state read
// and what is in the Pod.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (d *ReconcilePod) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Pod")

	// Reconcile is guaranteed to run after cache is synced.
	// On first run, query API server for all pods to populate our map
	if !d.synced {
		return reconcile.Result{}, d.InitPodList()
	}

	// Fetch the Pod instance
	key := request.NamespacedName
	pod := &corev1.Pod{}
	err := d.client.Get(context.TODO(), key, pod)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.

			log.Info("Pod deleted on my node")
			d.workerPodsLock.Lock()
			defer d.workerPodsLock.Unlock()
			delete(d.workerPods, key)
			if request.Namespace == metav1.NamespaceSystem && strings.HasPrefix(request.Name, cniPodName) {
				d.cniPodsLock.Lock()
				defer d.cniPodsLock.Unlock()
				delete(d.cniPods, key)
			}

			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	err = d.HandlePodUpdate(pod)
	return reconcile.Result{}, err
}

func (d *ReconcilePod) HandlePodUpdate(pod *corev1.Pod) error {
	// Note that you also have to check the uid if you have a local controlled resource, which
	// is dependent on the actual instance, to detect that a Pod was recreated with the same name
	podName := pod.GetName()
	podNamespace := pod.GetNamespace()
	key := types.NamespacedName{Name: podName, Namespace: podNamespace}

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

		log.Info("Add/Update for Pod on my node", "podName", podName, "namespace", d.workerPods[key].Namespace, "ip", d.workerPods[key].IP)
	} else if podNamespace == metav1.NamespaceSystem && strings.HasPrefix(podName, cniPodName) {
		d.cniPodsLock.Lock()
		defer d.cniPodsLock.Unlock()

		log.Info("Add/Update for CNI pod %s", "podName", podName)
		d.cniPods[key] = podName
	}
	return nil
}

func (d *ReconcilePod) InitPodList() error {
	log.Info("Initialising Pod list...")

	fieldSelector := fields.OneTermEqualSelector("spec.nodeName", d.myNodeName)
	continueToken := ""

	for {
		podList := corev1.PodList{}
		listOptions := &client.ListOptions{FieldSelector: fieldSelector, Raw: &metav1.ListOptions{Continue: continueToken}}
		err := d.client.List(context.TODO(), listOptions, &podList)
		if err != nil {
			return err
		}

		for _, pod := range podList.Items {
			err = d.HandlePodUpdate(&pod)

			if err != nil {
				return err
			}
		}

		if podList.Continue == "" {
			break
		}
		continueToken = podList.Continue
	}

	log.Info("Initialisation of Pod list complete")
	d.synced = true
	return nil
}

// GetCNIPods return the list of CNI pod names
func (d *ReconcilePod) GetCNIPods() []string {
	var cniPods []string

	log.Info("GetCNIPods start...")

	d.cniPodsLock.Lock()
	defer d.cniPodsLock.Unlock()

	for k := range d.cniPods {
		cniPods = append(cniPods, k.Name)
	}

	log.Info("GetCNIPods discovered", "cniPods", cniPods)
	return cniPods
}

// K8SGetLocalPodIPs return the list of pods running on the local nodes
func (d *ReconcilePod) K8SGetLocalPodIPs() ([]*K8SPodInfo, error) {
	var localPods []*K8SPodInfo

	if !d.synced {
		log.Info("GetLocalPods: informer not synced yet")
		return nil, ErrInformerNotSynced
	}

	log.V(2).Info("GetLocalPods start ...")
	d.workerPodsLock.Lock()
	defer d.workerPodsLock.Unlock()

	for _, pod := range d.workerPods {
		log.Info(fmt.Sprintf("K8SGetLocalPodIPs discovered local Pods: %s %s %s %s",
			pod.Name, pod.Namespace, pod.IP, pod.UID))
		localPods = append(localPods, pod)
	}

	return localPods, nil
}
