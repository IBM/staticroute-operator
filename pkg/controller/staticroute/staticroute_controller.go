//
// Copyright 2020 IBM Corporation
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

package staticroute

import (
	"context"
	"fmt"
	"net"

	iksv1 "github.com/IBM/staticroute-operator/pkg/apis/iks/v1"
	"github.com/IBM/staticroute-operator/pkg/routemanager"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	//ZoneLabel Kubernetes node label to determine node zone
	ZoneLabel = "failure-domain.beta.kubernetes.io/zone"
	//RouteTable Route table number for static routes
	RouteTable = 254
)

var log = logf.Log.WithName("controller_staticroute")

// ManagerOptions contains static route management related node properties
type ManagerOptions struct {
	RouteManager routemanager.RouteManager
	Hostname     string
	Zone         string
	RouteGet     func() (net.IP, error)
}

// ReconcileStaticRoute reconciles a StaticRoute object
type ReconcileStaticRoute struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client  client.Client
	scheme  *runtime.Scheme
	options ManagerOptions
}

// Add creates a new StaticRoute Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, options ManagerOptions) error {
	return add(mgr, newReconciler(mgr, options))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, options ManagerOptions) reconcile.Reconciler {
	return &ReconcileStaticRoute{client: mgr.GetClient(), scheme: mgr.GetScheme(), options: options}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("staticroute-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource StaticRoute
	return c.Watch(&source.Kind{Type: &iksv1.StaticRoute{}}, &handler.EnqueueRequestForObject{})
}

// blank assignment to verify that ReconcileStaticRoute implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileStaticRoute{}

// Reconcile reads that state of the cluster for a StaticRoute object and makes changes based on the state read
// and what is in the StaticRoute.Spec
func (r *ReconcileStaticRoute) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	params := reconcileImplParams{
		request: request,
		client:  r.client,
		options: r.options,
	}
	return reconcileImpl(params)
}

type reconcileImplClient interface {
	Get(context.Context, client.ObjectKey, runtime.Object) error
	Update(context.Context, runtime.Object, ...client.UpdateOption) error
	Status() client.StatusWriter
}

type reconcileImplParams struct {
	request reconcile.Request
	client  reconcileImplClient
	options ManagerOptions
}

func reconcileImpl(params reconcileImplParams) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Name", params.request.Name)
	reqLogger.Info("Reconciling StaticRoute")

	// Fetch the StaticRoute instance
	instance := &iksv1.StaticRoute{}
	if err := params.client.Get(context.Background(), params.request.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Info("Object not found. Probably deleted meanwhile")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	rw := routeWrapper{
		instance: instance,
	}

	if !rw.isSameZone(params.options.Zone, ZoneLabel) {
		// a zone is specified and the route is not for this zone, ignore
		reqLogger.Info("Ignoring, zone does not match", "NodeZone", params.options.Zone, "CRZone", instance.GetLabels()[ZoneLabel])
		return reconcile.Result{}, nil
	}
	if instance.GetDeletionTimestamp() != nil && rw.removeFromStatus(params.options.Hostname) {
		// Here comes the DELETE logic
		reqLogger.Info("Deregistering route")
		err := params.options.RouteManager.DeRegisterRoute(params.request.Name)
		if err != nil {
			reqLogger.Error(err, "Unable to deregister route")
			return reconcile.Result{}, err
		}

		reqLogger.Info("Deleted status for StaticRoute", "status", instance.Status)
		err = params.client.Status().Update(context.Background(), instance)
		if err != nil {
			reqLogger.Error(err, "Unable to update status of CR")
			return reconcile.Result{}, err
		}

		// We were the last one
		if len(instance.Status.NodeStatus) == 0 {
			reqLogger.Info("Removing finalizer for StaticRoute")
			instance.SetFinalizers(nil)
			if err := params.client.Update(context.Background(), instance); err != nil {
				reqLogger.Error(err, "Unable to delete finalizers")
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	if instance.GetDeletionTimestamp() == nil {
		reqLogger.Info("Adding Finalizer for the StaticRoute")
		if rw.setFinalizer() {
			if err := params.client.Update(context.Background(), rw.instance); err != nil {
				reqLogger.Error(err, "Failed to update StaticRoute with finalizer")
				return reconcile.Result{}, err
			}
		}
	}

	// If "gateway" is empty, we'll create the route through the default private network gateway
	gateway := rw.getGateway()
	if gateway == nil {
		defaultGateway, err := params.options.RouteGet()
		if err != nil {
			reqLogger.Error(err, "")
			return reconcile.Result{}, err
		}
		reqLogger.Info(fmt.Sprintf("* %+v", defaultGateway))
		gateway = defaultGateway
	}

	if !params.options.RouteManager.IsRegistered(params.request.Name) {
		/*  Here comes the ADD logic
		    This also runs if the CR was asked for deletion, but the operator did not run meanwhile.
			In this case the route is still programmed to the kernel, so we register the route here
			in order to successfully deregister and remove it from the kernel below */

		_, ipnet, err := net.ParseCIDR(instance.Spec.Subnet)
		if err != nil {
			reqLogger.Error(err, "Unable to convert the subnet into IP range and mask")
			return reconcile.Result{}, nil
		}
		reqLogger.Info("Registering route")

		err = params.options.RouteManager.RegisterRoute(params.request.Name, routemanager.Route{Dst: *ipnet, Gw: gateway, Table: RouteTable})
		if err != nil {
			reqLogger.Error(err, "Unable to register route")
			return reconcile.Result{}, err
		}
	}

	// Delay status update until this point, so it will not be executed if CR delete is ongoing
	if rw.addToStatus(params.options.Hostname, gateway) {
		reqLogger.Info("Update the StaticRoute status", "staticroute", instance.Status)
		if err := params.client.Status().Update(context.Background(), instance); err != nil {
			reqLogger.Error(err, "failed to update the staticroute")
			return reconcile.Result{}, err
		}
	}

	// TODO: Here comes the MODIFY logic

	reqLogger.Info("Reconciliation done, no add, no delete.")
	return reconcile.Result{}, nil
}

type routeWrapper struct {
	instance *iksv1.StaticRoute
}

//addFinalizer will add this attribute to the CR
func (rw *routeWrapper) setFinalizer() bool {
	if len(rw.instance.GetFinalizers()) != 0 {
		return false
	}
	rw.instance.SetFinalizers([]string{"finalizer.iks.ibm.com"})
	return true
}

func (rw *routeWrapper) isSameZone(zone, label string) bool {
	instanceZone := rw.instance.GetLabels()[label]

	return instanceZone == zone
}

// Returns nil like the underlaying net.ParseIP()
func (rw *routeWrapper) getGateway() net.IP {
	gateway := rw.instance.Spec.Gateway
	if len(gateway) == 0 {
		return nil
	}
	return net.ParseIP(gateway)
}

func (rw *routeWrapper) addToStatus(hostname string, gateway net.IP) bool {
	// Update the status if necessary
	foundStatus := false
	for _, val := range rw.instance.Status.NodeStatus {
		if val.Hostname != hostname {
			continue
		}
		foundStatus = true
		break
	}

	if foundStatus {
		return false
	}

	spec := rw.instance.Spec
	spec.Gateway = gateway.String()
	rw.instance.Status.NodeStatus = append(rw.instance.Status.NodeStatus, iksv1.StaticRouteNodeStatus{
		Hostname: hostname,
		State:    spec,
		Error:    "",
	})
	return true
}

func (rw *routeWrapper) removeFromStatus(hostname string) (existed bool) {
	if len(rw.instance.Status.NodeStatus) == 0 {
		return
	}
	// Update the status if necessary
	statusArr := []iksv1.StaticRouteNodeStatus{}
	for _, val := range rw.instance.Status.NodeStatus {
		valCopy := val.DeepCopy()

		if valCopy.Hostname == hostname {
			existed = true
			continue
		}

		statusArr = append(statusArr, *valCopy)
	}

	newStatus := iksv1.StaticRouteStatus{
		NodeStatus: statusArr,
	}
	rw.instance.Status = newStatus
	return existed
}
