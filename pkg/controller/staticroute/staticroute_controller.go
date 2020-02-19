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
	"github.com/vishvananda/netlink"
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

var log = logf.Log.WithName("controller_staticroute")

// ManagerOptions contains static route management related node properties
type ManagerOptions struct {
	RouteManager routemanager.RouteManager
	Hostname     string
	Zone         string
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
	err = c.Watch(&source.Kind{Type: &iksv1.StaticRoute{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileStaticRoute implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileStaticRoute{}

// Reconcile reads that state of the cluster for a StaticRoute object and makes changes based on the state read
// and what is in the StaticRoute.Spec
func (r *ReconcileStaticRoute) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Name", request.Name)
	reqLogger.Info("Reconciling StaticRoute")

	// Fetch the StaticRoute instance
	instance := &iksv1.StaticRoute{}
	if err := r.client.Get(context.TODO(), request.NamespacedName, instance); err != nil {
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

	isNew := len(instance.Finalizers) == 0
	if isNew {
		reqLogger.Info("Adding Finalizer for the StaticRoute")
		if err := r.addFinalizer(instance); err != nil {
			reqLogger.Error(err, "Failed to update StaticRoute with finalizer")
			return reconcile.Result{}, err
		}
	}

	zoneVal := instance.GetLabels()["failure-domain.beta.kubernetes.io/zone"]
	if zoneVal != "" && zoneVal != r.options.Zone {
		// a zone is specified and the route is not for this zone, ignore
		reqLogger.Info("Ignoring, zone does not match", "NodeZone", r.options.Zone, "CRZone", zoneVal)
		return reconcile.Result{}, nil
	}

	gateway := instance.Spec.Gateway
	var gwIP net.IP
	// If "gateway" is empty, we'll create the route through the default private network gateway
	if gateway == "" {
		gwRoute, err := netlink.RouteGet(net.IP{10, 0, 0, 1})
		if err != nil {
			fmt.Println(err.Error())
		}
		fmt.Printf("* %+v", gwRoute[0].Gw)
		gwIP = gwRoute[0].Gw
		if err != nil {
			reqLogger.Info("Unable to retrieve fallback gateway")
			return reconcile.Result{}, err
		}
	} else {
		gwIP = net.ParseIP(gateway)
	}
	instance.Spec.Gateway = gwIP.String()
	if !r.options.RouteManager.IsRegistered(request.Name) {
		/*  Here comes the ADD logic
		    This also runs if the CR was asked for deletion, but the operator did not run meanwhile.
			In this case the route is still programmed to the kernel, so we register the route here
			in order to successfully deregister and remove it from the kernel below */

		_, ipnet, err := net.ParseCIDR(instance.Spec.Subnet)
		if err != nil {
			reqLogger.Error(err, "Could not convert the subnet into IP range and mask")
		}
		reqLogger.Info("Registering route")
		err = r.options.RouteManager.RegisterRoute(request.Name, routemanager.Route{Dst: *ipnet, Gw: gwIP, Table: 254})
		if err != nil {
			return reconcile.Result{}, err
		}
	} else {
		reqLogger.Info("Route already registered")
	}

	isDeleted := instance.GetDeletionTimestamp() != nil
	if isDeleted {
		if len(instance.Status.NodeStatus) > 0 && removeFromStatusIfExist(instance, r.options.Hostname) {
			// Here comes the DELETE logic
			reqLogger.Info("Deregistering route")
			err := r.options.RouteManager.DeRegisterRoute(request.Name)
			if err != nil {
				return reconcile.Result{}, err
			}

			reqLogger.Info("Deleted status for StaticRoute", "status", instance.Status)
			err = r.client.Status().Update(context.TODO(), instance)
			if err != nil {
				return reconcile.Result{}, err
			}
		}

		// We were the last one
		if len(instance.Status.NodeStatus) == 0 {
			reqLogger.Info("Removing finalizer for StaticRoute")
			instance.SetFinalizers(nil)
			if err := r.client.Update(context.TODO(), instance); err != nil {
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	// Delay status update until this point, so it will not be executed if CR delete is ongoing
	if addToStatusIfNotExist(instance, r.options.Hostname) {
		reqLogger.Info("Update the StaticRoute status", "staticroute", instance.Status)
		if err := r.client.Status().Update(context.TODO(), instance); err != nil {
			reqLogger.Error(err, "failed to update the staticroute")
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// TODO: Here comes the MODIFY logic

	reqLogger.Info("Reconciliation done, no add, no delete.")
	return reconcile.Result{}, nil
}

//addFinalizer will add this attribute to the CR
func (r *ReconcileStaticRoute) addFinalizer(m *iksv1.StaticRoute) error {
	if len(m.GetFinalizers()) < 1 && m.GetDeletionTimestamp() == nil {
		m.SetFinalizers([]string{"finalizer.iks.ibm.com"})

		// Update CR
		err := r.client.Update(context.TODO(), m)
		if err != nil {
			return err
		}
	}
	return nil
}

func addToStatusIfNotExist(m *iksv1.StaticRoute, hostname string) bool {
	// Update the status if necessary
	foundStatus := false
	for _, val := range m.Status.NodeStatus {
		if val.Hostname != hostname {
			continue
		}
		foundStatus = true
		break
	}

	if !foundStatus {
		m.Status.NodeStatus = append(m.Status.NodeStatus, iksv1.StaticRouteNodeStatus{
			Hostname: hostname,
			State:    m.Spec,
			Error:    "",
		})
		return true
	}
	return false
}

func removeFromStatusIfExist(m *iksv1.StaticRoute, hostname string) bool {
	// Update the status if necessary
	existed := false
	statusArr := []iksv1.StaticRouteNodeStatus{}
	for _, val := range m.Status.NodeStatus {
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
	m.Status = newStatus
	return existed
}
