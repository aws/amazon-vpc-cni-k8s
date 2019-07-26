package node

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	defaultEniConfigAnnotationDef = "k8s.amazonaws.com/eniConfig"
	defaultEniConfigLabelDef      = "k8s.amazonaws.com/eniConfig"
	eniConfigDefault              = "default"

	// when "ENI_CONFIG_LABEL_DEF is defined, ENIConfigController will use that label key to
	// search if is setting value for eniConfigLabelDef
	// Example:
	//   Node has set label k8s.amazonaws.com/eniConfigCustom=customConfig
	//   We can get that value in controller by setting environmental variable ENI_CONFIG_LABEL_DEF
	//   ENI_CONFIG_LABEL_DEF=k8s.amazonaws.com/eniConfigOverride
	//   This will set eniConfigLabelDef to eniConfigOverride
	envEniConfigAnnotationDef = "ENI_CONFIG_ANNOTATION_DEF"
	envEniConfigLabelDef      = "ENI_CONFIG_LABEL_DEF"
)

var log = logf.Log.WithName("controller_node")

// Add creates a new Node Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) (*ReconcileNode, error) {
	r := newReconciler(mgr)
	return r, add(mgr, r)
}

// newReconciler returns a new ReconcileNode
func newReconciler(mgr manager.Manager) *ReconcileNode {
	return &ReconcileNode{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),

		myNodeName: os.Getenv("MY_NODE_NAME"),
		myENI:      eniConfigDefault,
		eniConfigAnnotationDef: GetEniConfigAnnotationDef(),
		eniConfigLabelDef:      GetEniConfigLabelDef(),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("node-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Node
	err = c.Watch(&source.Kind{Type: &corev1.Node{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileNode implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileNode{}

// ReconcileNode reconciles a Node object
type ReconcileNode struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client                 client.Client
	scheme                 *runtime.Scheme

	myNodeName             string
	myENI                  string
	eniConfigAnnotationDef string
	eniConfigLabelDef      string
	eniLock                sync.RWMutex
}

// Reconcile reads that state of the cluster for a Node object and makes changes based on the state read
// and what is in the Node.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileNode) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reconcilePeriod := 5 * time.Second
	reconcileResult := reconcile.Result{RequeueAfter: reconcilePeriod}

	// Skip other node updates
	if request.Name != r.myNodeName {
		reqLogger.V(2).Info("Ignoring other node")
		return reconcile.Result{}, nil
	}

	reqLogger.Info("Reconciling Node")

	// Fetch the Node instance
	node := &corev1.Node{}
	err := r.client.Get(context.TODO(), request.NamespacedName, node)
	if err != nil {
		// Error reading the object - requeue the request.
		return reconcileResult, err
	}

	// Get annotations if not found get labels if not found fallback use default
	val, ok := node.GetAnnotations()[r.eniConfigAnnotationDef]
	if !ok {
		val, ok = node.GetLabels()[r.eniConfigLabelDef]
		if !ok {
			val = eniConfigDefault
		}
	}

	r.eniLock.Lock()
	defer r.eniLock.Unlock()
	r.myENI = val
	log.Info(fmt.Sprintf("Setting myENI to: %s", val))

	return reconcileResult, nil
}

func (r *ReconcileNode) GetMyENI() string {
	r.eniLock.Lock()
	defer r.eniLock.Unlock()
	return r.myENI
}

// getEniConfigAnnotationDef returns eniConfigAnnotation
func GetEniConfigAnnotationDef() string {
	inputStr, found := os.LookupEnv(envEniConfigAnnotationDef)

	if !found {
		return defaultEniConfigAnnotationDef
	}
	if len(inputStr) > 0 {
		log.V(2).Info(fmt.Sprintf("Using ENI_CONFIG_ANNOTATION_DEF %v", inputStr))
		return inputStr
	}
	return defaultEniConfigAnnotationDef
}

// getEniConfigLabelDef returns eniConfigLabel name
func GetEniConfigLabelDef() string {
	inputStr, found := os.LookupEnv(envEniConfigLabelDef)

	if !found {
		return defaultEniConfigLabelDef
	}
	if len(inputStr) > 0 {
		log.V(2).Info(fmt.Sprintf("Using ENI_CONFIG_LABEL_DEF %v", inputStr))
		return inputStr
	}
	return defaultEniConfigLabelDef
}
