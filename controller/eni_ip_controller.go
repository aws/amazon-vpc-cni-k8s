// Copyright 2017-2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package main

import (
	"os"
	"strings"
	"time"

	"github.com/aws/amazon-ecs-cni-plugins/pkg/logger"

	log "github.com/cihub/seelog"
	"github.com/pkg/errors"

	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	lister_v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	kubeletapis "k8s.io/kubernetes/pkg/kubelet/apis"

	"github.com/aws/amazon-vpc-cni-k8s/controller/vpcipresource"
)

const (
	defaultLogFilePath = "/host/var/log/aws-routed-eni/controller.log"
	version            = "0.1.0"
	instanceTypeLabel  = "beta.kubernetes.io/instance-type"
)

// ENIIPController is responsible managing VPI IP address resources
type ENIIPController struct {
	client     kubernetes.Interface
	vpcIP      vpcipresource.Interface
	nodeLister lister_v1.NodeLister
	queue      workqueue.RateLimitingInterface
	informer   cache.Controller
	indexer    cache.Indexer
	nodeName   string
	// nodeStoreSynced returns true if the node store has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	nodeStoreSynced cache.InformerSynced
}

// NewENIIPController creates ENIIPController
func NewENIIPController(client kubernetes.Interface, vpcIP vpcipresource.Interface, nodeName string) *ENIIPController {

	eniIPController := &ENIIPController{
		client:   client,
		vpcIP:    vpcIP,
		queue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		nodeName: nodeName,
	}

	indexer, informer := cache.NewIndexerInformer(
		&cache.ListWatch{
			ListFunc: func(lo meta_v1.ListOptions) (runtime.Object, error) {
				// We do not add any selectors because we want to watch all nodes.
				return client.Core().Nodes().List(lo)
			},
			WatchFunc: func(lo meta_v1.ListOptions) (watch.Interface, error) {
				return client.Core().Nodes().Watch(lo)
			},
		},
		// The types of objects this informer will return
		&v1.Node{},
		// The resync period of this object. This will force a re-queue of all cached objects at this interval.
		// Every object will trigger the `Updatefunc` even if there have been no actual updates triggered.
		// In some cases you can set this to a very high interval - as you can assume you will see periodic updates in normal operation.
		// The interval is set low here for demo purposes.
		10*time.Second,
		// Callback Functions to trigger on add/update/delete
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if key, err := cache.MetaNamespaceKeyFunc(obj); err == nil {
					log.Infof("Add key %s", key)
					eniIPController.queue.Add(key)
				}
			},
			/*
				// TODO feedback from node about their eni requirement
					UpdateFunc: func(old, new interface{}) {
						if key, err := cache.MetaNamespaceKeyFunc(new); err == nil {
							log.Infof("Update key %s", key)
							//eniIPController.queue.Add(key)
						}
					},
			*/
			DeleteFunc: func(obj interface{}) {
				if key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj); err == nil {
					log.Infof("Delete key %s", key)
					eniIPController.queue.Add(key)
				}
			},
		},
		cache.Indexers{},
	)

	eniIPController.informer = informer
	eniIPController.indexer = indexer
	eniIPController.nodeLister = lister_v1.NewNodeLister(indexer)
	eniIPController.nodeStoreSynced = informer.HasSynced

	return eniIPController
}

func (c *ENIIPController) processNextItem() bool {
	// Wait until there is a new item in the working queue
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	// Tell the queue that we are done with processing this key. This unblocks the key for other workers
	// This allows safe parallel processing because two pods with the same key are never processed in
	// parallel.
	defer c.queue.Done(key)

	// Invoke the method containing the business logic
	err := c.nodeENIIPController(key.(string))
	// Handle the error if something went wrong during the execution of the business logic
	c.handleErr(err, key)
	return true
}

func (c *ENIIPController) nodeENIIPController(key string) error {
	log.Infof("Processing: %s", key)
	obj, exists, err := c.indexer.GetByKey(key)
	if err != nil {
		log.Errorf("Fetching object with key %s from store failed with %v", key, err)
		return err
	}
	if !exists {
		log.Infof("Node %s does not exist anymore", key)
		return nil
	}

	node, ok := obj.(*v1.Node)
	if !ok {
		log.Errorf("Object with key %s [%v] is not a node!", key, obj)
		//TODO err
		return nil
	}

	instanceType, ok := node.Labels[kubeletapis.LabelInstanceType]
	if !ok {
		log.Infof("Node %s has no labels %s", key, instanceTypeLabel)
		// TODO query EC2 about instance type
		return nil
	}

	ipLimit, err := vpcipresource.GetVPICIPLimit(instanceType)

	if err != nil {
		log.Errorf("Failed to get vpc ip limit %v", err)
		//TODO handle err
		return nil
	}

	if strings.Compare(key, c.nodeName) == 0 {
		// decrement by 1 since eni-ip-controller pod also takes 1 IP address
		ipLimit--
	}
	log.Infof("Updating node(%s: %s) ip resource limit to %d", key, instanceType, ipLimit)

	err = c.vpcIP.Update(c.client, node.Name, ipLimit)

	if err != nil {
		log.Errorf("Failed to update node vpc ip resource for node %s", key)
		return errors.Wrapf(err, "eni-ip-controller: can not update vpc ip resource for node %s", key)
	}

	return nil
}

// handleErr checks if an error happened and makes sure we will retry later.
func (c *ENIIPController) handleErr(err error, key interface{}) {
	if err == nil {
		// Forget about the #AddRateLimited history of the key on every successful synchronization.
		// This ensures that future processing of updates for this key is not delayed because of
		// an outdated error history.
		c.queue.Forget(key)
		return
	}

	// This controller retries 5 times if something goes wrong. After that, it stops trying.
	if c.queue.NumRequeues(key) < 5 {
		log.Infof("Error syncing node %v: %v", key, err)

		// Re-enqueue the key rate limited. Based on the rate limiter on the
		// queue and the re-enqueue history, the key will be processed later again.
		c.queue.AddRateLimited(key)
		return
	}

	c.queue.Forget(key)
	log.Infof("Dropping pod %q out of the queue: %v", key, err)
}

// Run began watching Node object
func (c *ENIIPController) Run(stopCh chan struct{}) {
	defer c.queue.ShutDown()

	go c.informer.Run(stopCh)

	log.Info("Waiting for cache sync")

	// Wait for all involved caches to be synced, before processing items from the queue is started
	if !cache.WaitForCacheSync(stopCh, c.nodeStoreSynced) {
		log.Error("Timed out waiting for caches to sync")
		return
	}
	log.Info("Informer cache has synced")

	// Launching additional goroutines would parallelize workers consuming from the queue (but we don't really need this)
	go wait.Until(c.runWorker, time.Second, stopCh)

	<-stopCh
	log.Info("Stopping eni-ip-controller")
}

func (c *ENIIPController) runWorker() {
	for c.processNextItem() {
	}
}

func main() {
	defer log.Flush()
	logger.SetupLogger(logger.GetLogFileLocation(defaultLogFilePath))

	nodeName := os.Getenv("MY_NODE_NAME")
	log.Infof("Starting eni-ip-controller on %s: %s  ...", nodeName, version)

	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Errorf("Failed to get incluster config %v", err)
		panic(err.Error())
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Errorf("Failed to create clientset %v", err)
		panic(err.Error())
	}

	// Now let's start the controller
	stop := make(chan struct{})
	defer close(stop)

	NewENIIPController(clientset, vpcipresource.NewVPCIPResource(), nodeName).Run(stop)
}
