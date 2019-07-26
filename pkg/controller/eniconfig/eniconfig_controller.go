package eniconfig

import (
	"context"
	"sync"
	"time"

	crdv1alpha1 "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/controller/node"

	"github.com/pkg/errors"
	//corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	//metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	//"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	//"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type ENIConfig interface {
	MyENIConfig() (*crdv1alpha1.ENIConfigSpec, error)
	Getter() *ENIConfigInfo
}

// ENIConfigInfo returns locally cached ENIConfigs
type ENIConfigInfo struct {
	ENI                    map[string]crdv1alpha1.ENIConfigSpec
	MyENI                  string
	EniConfigAnnotationDef string
	EniConfigLabelDef      string
}

// ENIConfigController reconciles a ENIConfig object
type ReconcileENIConfig struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme

	nodeController         *node.ReconcileNode
	eni                    map[string]*crdv1alpha1.ENIConfigSpec
	eniLock                sync.RWMutex
}

// blank assignment to verify that ReconcileENIConfig implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileENIConfig{}

var ErrNoENIConfig = errors.New("eniconfig: eniconfig is not available")

var log = logf.Log.WithName("controller_eniconfig")

// Add creates a new ENIConfig Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, nodeController *node.ReconcileNode) (*ReconcileENIConfig, error) {
	r := newReconciler(mgr, nodeController)
	return r, add(mgr, r)
}

// newReconciler returns a new ReconcileENIConfig
func newReconciler(mgr manager.Manager, nodeController *node.ReconcileNode) *ReconcileENIConfig {
	return &ReconcileENIConfig{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
		nodeController: nodeController,
		eni: make(map[string]*crdv1alpha1.ENIConfigSpec),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("eniconfig-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource ENIConfig
	err = c.Watch(&source.Kind{Type: &crdv1alpha1.ENIConfig{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// Reconcile reads that state of the cluster for a ENIConfig object and makes changes based on the state read
// and what is in the ENIConfig.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileENIConfig) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling ENIConfig")

	reconcilePeriod := 5 * time.Second
	reconcileResult := reconcile.Result{RequeueAfter: reconcilePeriod}

	// Fetch the ENIConfig instance
	o := &crdv1alpha1.ENIConfig{}
	err := r.client.Get(context.TODO(), request.NamespacedName, o)
	eniConfigName := request.NamespacedName.Name;

	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.

			reqLogger.Info("Deleting ENIConfig")
			r.eniLock.Lock()
			defer r.eniLock.Unlock()
			delete(r.eni, eniConfigName)


			// Return and don't requeue
			return reconcileResult, nil
		}
		// Error reading the object - requeue the request.
		return reconcileResult, err
	}

	curENIConfig := o.DeepCopy()

	reqLogger.Info("ENIConfig Add/Update", "SecurityGroups", curENIConfig.Spec.SecurityGroups, "Subnet", curENIConfig.Spec.Subnet)

	r.eniLock.Lock()
	defer r.eniLock.Unlock()
	r.eni[eniConfigName] = &curENIConfig.Spec

	return reconcileResult, nil
}

func (eniCfg *ReconcileENIConfig) Getter() *ENIConfigInfo {
	output := &ENIConfigInfo{
		ENI: make(map[string]crdv1alpha1.ENIConfigSpec),
	}

	output.MyENI = eniCfg.nodeController.GetMyENI()
	output.EniConfigAnnotationDef = node.GetEniConfigAnnotationDef()
	output.EniConfigLabelDef = node.GetEniConfigLabelDef()

	eniCfg.eniLock.Lock()
	defer eniCfg.eniLock.Unlock()

	for name, val := range eniCfg.eni {
		output.ENI[name] = *val
	}

	return output
}

// MyENIConfig returns the security
func (eniCfg *ReconcileENIConfig) MyENIConfig() (*crdv1alpha1.ENIConfigSpec, error) {
	eniCfg.eniLock.Lock()
	defer eniCfg.eniLock.Unlock()

	myENIConfig, ok := eniCfg.eni[eniCfg.nodeController.GetMyENI()]

	if ok {
		return &crdv1alpha1.ENIConfigSpec{
			SecurityGroups: myENIConfig.SecurityGroups,
			Subnet:         myENIConfig.Subnet,
		}, nil
	}
	return nil, ErrNoENIConfig
}

