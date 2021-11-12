//
// Copyright 2021 IBM Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package node

import (
	"context"

	staticroutev1 "github.com/IBM/staticroute-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_node")

// Add creates a new Node Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return (&NodeReconciler{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme()}).
		SetupWithManager(mgr)
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Watch for changes to primary resource Node
	return ctrl.NewControllerManagedBy(mgr).
		Named("node-controller").
		For(&corev1.Node{}).
		Watches(&source.Kind{Type: &corev1.Node{}}, &handler.EnqueueRequestForObject{}).
		WithEventFilter(
			&predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return false
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return false
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return true
				},
			}).
		Complete(r)
}

// blank assignment to verify that NodeReconciler implements reconcile.Reconciler
var _ reconcile.Reconciler = &NodeReconciler{}

// NodeReconciler reconciles a Node object
type NodeReconciler struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client reconcileImplClient
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Node object and makes changes based on the state read
// and what is in the Node.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *NodeReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	params := reconcileImplParams{
		request: request,
		client:  r.client,
	}
	result, err := reconcileImpl(params)
	return *result, err
}

type reconcileImplClient interface {
	Get(context.Context, client.ObjectKey, client.Object) error
	List(context.Context, client.ObjectList, ...client.ListOption) error
	Status() client.StatusWriter
}

type reconcileImplParams struct {
	request reconcile.Request
	client  reconcileImplClient
}

var (
	nodeStillExists = &reconcile.Result{}
	finished        = &reconcile.Result{}

	nodeGetError         = &reconcile.Result{}
	staticRouteListError = &reconcile.Result{}
	deleteRouteError     = &reconcile.Result{}
)

func reconcileImpl(params reconcileImplParams) (*reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", params.request.Namespace, "Request.Name", params.request.Name)

	// Fetch the Node instance
	node := &corev1.Node{}
	if err := params.client.Get(context.Background(), params.request.NamespacedName, node); err == nil {
		return nodeStillExists, nil
	} else if !errors.IsNotFound(err) {
		// Error reading the object - requeue the request.
		return nodeGetError, err
	}

	routes := &staticroutev1.StaticRouteList{}
	if err := params.client.List(context.Background(), routes); err != nil {
		reqLogger.Error(err, "Unable to fetch CRD")
		return staticRouteListError, err
	}

	nf := nodeFinder{
		nodeName: params.request.Name,
		updateCallback: func(route *staticroutev1.StaticRoute) error {
			return params.client.Status().Update(context.Background(), route)
		},
		infoLogger: reqLogger.Info,
	}
	if err := nf.delete(routes); err != nil {
		reqLogger.Error(err, "Unable to update CR")
		return deleteRouteError, err
	}

	return finished, nil
}

type nodeFinder struct {
	nodeName       string
	updateCallback func(*staticroutev1.StaticRoute) error
	infoLogger     func(string, ...interface{})
}

func (nf *nodeFinder) delete(routes *staticroutev1.StaticRouteList) error {
	for i := range routes.Items {
		statusToDelete := nf.findNode(&routes.Items[i])
		if statusToDelete == -1 {
			continue
		}
		nf.infoLogger("Found the node to delete")

		copy(routes.Items[i].Status.NodeStatus[statusToDelete:], routes.Items[i].Status.NodeStatus[statusToDelete+1:])
		routes.Items[i].Status.NodeStatus[len(routes.Items[i].Status.NodeStatus)-1] = staticroutev1.StaticRouteNodeStatus{}
		routes.Items[i].Status.NodeStatus = routes.Items[i].Status.NodeStatus[:len(routes.Items[i].Status.NodeStatus)-1]

		if err := nf.updateCallback(&routes.Items[i]); err != nil {
			return err
		}
	}

	return nil
}

func (nf *nodeFinder) findNode(route *staticroutev1.StaticRoute) int {
	for i, status := range route.Status.NodeStatus {
		if status.Hostname == nf.nodeName {
			return i
		}
	}

	return -1
}
