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
	"errors"
	"fmt"
	"net"

	iksv1 "github.com/IBM/staticroute-operator/pkg/apis/iks/v1"
	"github.com/IBM/staticroute-operator/pkg/routemanager"
	"github.com/IBM/staticroute-operator/pkg/types"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	//ZoneLabel Node hostname label to determine hostname
	HostNameLabel = "kubernetes.io/hostname"
)

var log = logf.Log.WithName("controller_staticroute")

// ManagerOptions contains static route management related node properties
type ManagerOptions struct {
	RouteManager     routemanager.RouteManager
	Hostname         string
	Table            int
	ProtectedSubnets []*net.IPNet
	RouteGet         func() (net.IP, error)
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

	// Watch if the self node labels are changed, so reconcile every route
	err = c.Watch(
		&source.Kind{Type: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: r.(*ReconcileStaticRoute).options.Hostname}}},
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(func(a handler.MapObject) []reconcile.Request {
				routes := &iksv1.StaticRouteList{}
				if err := r.(*ReconcileStaticRoute).client.List(context.Background(), routes); err != nil {
					log.Error(err, "Failed to List StaticRoute CRs")
					return nil
				}

				var result []reconcile.Request
				for _, route := range routes.Items {
					result = append(result, reconcile.Request{
						NamespacedName: k8stypes.NamespacedName{
							Name:      route.GetName(),
							Namespace: "",
						},
					})
				}
				return result
			}),
		},
		&predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return false
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				if len(e.MetaNew.GetLabels()) != len(e.MetaOld.GetLabels()) {
					log.Info("Node label amount changed. Submitting all StaticRoute CRs for reconciliation.")
					return true
				}
				for k, v := range e.MetaOld.GetLabels() {
					if e.MetaNew.GetLabels()[k] != v {
						log.Info("Node labels are changed. Submitting all StaticRoute CRs for reconciliation.")
						return true
					}
				}
				return false
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
		},
	)
	return err
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
	result, err := reconcileImpl(params)
	return *result, err
}

type reconcileImplClient interface {
	Get(context.Context, client.ObjectKey, runtime.Object) error
	Update(context.Context, runtime.Object, ...client.UpdateOption) error
	List(ctx context.Context, list runtime.Object, opts ...client.ListOption) error
	Status() client.StatusWriter
}

type reconcileImplParams struct {
	request reconcile.Request
	client  reconcileImplClient
	options ManagerOptions
}

var (
	crNotFound        = &reconcile.Result{}
	nodeNotFound      = &reconcile.Result{}
	overlapsProtected = &reconcile.Result{}
	alreadyDeleted    = &reconcile.Result{}
	deletionFinished  = &reconcile.Result{}
	updateFinished    = &reconcile.Result{Requeue: true}
	finished          = &reconcile.Result{}

	crGetError           = &reconcile.Result{}
	wrongSelectorErr     = &reconcile.Result{}
	nodeGetError         = &reconcile.Result{}
	deRegisterError      = &reconcile.Result{}
	delStatusUpdateError = &reconcile.Result{}
	emptyFinalizerError  = &reconcile.Result{}
	setFinalizerError    = &reconcile.Result{}
	invalidGatewayError  = &reconcile.Result{}
	routeGetError        = &reconcile.Result{}
	parseSubnetError     = &reconcile.Result{}
	registerRouteError   = &reconcile.Result{}
	addStatusUpdateError = &reconcile.Result{}
)

func reconcileImpl(params reconcileImplParams) (*reconcile.Result, error) {
	reqLogger := log.WithValues("Node", params.options.Hostname, "Request.Name", params.request.Name)
	reqLogger.Info("Reconciling StaticRoute")

	// Fetch the StaticRoute instance
	instance := &iksv1.StaticRoute{}
	if err := params.client.Get(context.Background(), params.request.NamespacedName, instance); err != nil {
		if kerrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Info("Object not found. Probably deleted meanwhile")
			return crNotFound, nil
		}
		// Error reading the object - requeue the request.
		return crGetError, err
	}

	rw := routeWrapper{instance: instance}

	if rw.isProtected(params.options.ProtectedSubnets) {
		// a subnet overlaps some protected, ignore
		reqLogger.Info("Ignoring, subnet overlaps some protected", "Subnet", rw.instance.Spec.Subnet)
		return overlapsProtected, nil
	}

	// If "gateway" is empty, we'll create the route through the default private network gateway
	res, gateway, err := selectGateway(params, rw, reqLogger)
	if gateway == nil {
		return res, err
	}

	selectorNoLongerMatches := false
	if len(rw.instance.Spec.Selectors) > 0 {
		reqLogger.Info("Node selector found", "Selector", rw.instance.Spec.Selectors)
		if res, err := validateNodeBySelector(params, &rw, reqLogger); res != nil {
			if res != nodeNotFound || !rw.alreadyInStatus(params.options.Hostname) {
				return res, err
			}
			reqLogger.Info("Node labels likely changed and no longer applies to this CR")
			selectorNoLongerMatches = true
		}
	}

	isChanged := rw.isChanged(params.options.Hostname, gateway.String(), rw.instance.Spec.Selectors)
	reqLogger.Info("The resource is", "changed", isChanged)
	if instance.GetDeletionTimestamp() != nil ||
		isChanged ||
		selectorNoLongerMatches {
		if !rw.removeFromStatus(params.options.Hostname) {
			return alreadyDeleted, nil
		}
		res, err := deleteOperation(params, &rw, reqLogger)

		if isChanged {
			return updateFinished, err
		}
		return res, err
	}

	return addOperation(params, &rw, gateway, params.options.Table, reqLogger)
}

func selectGateway(params reconcileImplParams, rw routeWrapper, logger types.Logger) (*reconcile.Result, net.IP, error) {
	gateway := rw.getGateway()
	if gateway == nil && len(rw.instance.Spec.Gateway) != 0 {
		logger.Error(errors.New("Invalid gateway found in Spec"), rw.instance.Spec.Gateway)
		return invalidGatewayError, nil, nil
	}
	if gateway == nil {
		defaultGateway, err := params.options.RouteGet()
		if err != nil {
			logger.Error(err, "")
			return routeGetError, nil, err
		}
		logger.Info(fmt.Sprintf("* %+v", defaultGateway))
		gateway = defaultGateway
	}
	return nil, gateway, nil
}

func validateNodeBySelector(params reconcileImplParams, rw *routeWrapper, logger types.Logger) (*reconcile.Result, error) {
	nodes := &corev1.NodeList{}
	selector := labels.NewSelector()
	allSelector := rw.instance.Spec.Selectors
	allSelector = append(allSelector, metav1.LabelSelectorRequirement{
		Key:      HostNameLabel,
		Operator: metav1.LabelSelectorOpIn,
		Values:   []string{params.options.Hostname},
	})
	for _, s := range allSelector {
		operator, err := convertToOperator(s.Operator)
		if err != nil {
			log.Info("There is something wrong with the node selector operator", "Value", s)
			return wrongSelectorErr, nil
		}
		req, err := labels.NewRequirement(s.Key, operator, s.Values)
		if err != nil {
			log.Info("There is something wrong with the node selector", "Value", s)
			return wrongSelectorErr, nil
		}
		selector = selector.Add(*req)
	}
	listOptions := &client.ListOptions{LabelSelector: selector}
	if err := params.client.List(context.Background(), nodes, listOptions); err != nil {
		log.Error(err, "Failed to fetch nodes")
		return nodeGetError, err
	} else if len(nodes.Items) == 0 {
		log.Info("Node not found with the given selectors", "Value", allSelector)
		return nodeNotFound, nil
	}
	return nil, nil
}

func deleteOperation(params reconcileImplParams, rw *routeWrapper, logger types.Logger) (*reconcile.Result, error) {
	// Here comes the DELETE logic
	logger.Info("Deregistering route")
	err := params.options.RouteManager.DeRegisterRoute(params.request.Name)
	if err != nil && err != routemanager.ErrNotFound {
		logger.Error(err, "Unable to deregister route")
		return deRegisterError, err
	}

	logger.Info("Deleted status for StaticRoute", "status", rw.instance.Status)
	err = params.client.Status().Update(context.Background(), rw.instance)
	if err != nil {
		logger.Error(err, "Unable to update status of CR")
		return delStatusUpdateError, err
	}

	// We were the last one
	if len(rw.instance.Status.NodeStatus) == 0 {
		logger.Info("Removing finalizer for StaticRoute")
		rw.instance.SetFinalizers(nil)
		if err := params.client.Update(context.Background(), rw.instance); err != nil {
			logger.Error(err, "Unable to delete finalizers")
			return emptyFinalizerError, err
		}
	}
	return deletionFinished, nil
}

func addOperation(params reconcileImplParams, rw *routeWrapper, gateway net.IP, table int, logger types.Logger) (*reconcile.Result, error) {
	if rw.setFinalizer() {
		logger.Info("Adding Finalizer for the StaticRoute")
		if err := params.client.Update(context.Background(), rw.instance); err != nil {
			logger.Error(err, "Failed to update StaticRoute with finalizer")
			return setFinalizerError, err
		}
	}

	if !params.options.RouteManager.IsRegistered(params.request.Name) {
		/*  Here comes the ADD logic
		    This also runs if the CR was asked for deletion, but the operator did not run meanwhile.
			In this case the route is still programmed to the kernel, so we register the route here
			in order to successfully deregister and remove it from the kernel below */

		_, ipnet, err := net.ParseCIDR(rw.instance.Spec.Subnet)
		if err != nil {
			logger.Error(err, "Unable to convert the subnet into IP range and mask")
			return parseSubnetError, nil
		}
		logger.Info("Registering route")

		err = params.options.RouteManager.RegisterRoute(params.request.Name, routemanager.Route{Dst: *ipnet, Gw: gateway, Table: table})
		if err != nil {
			logger.Error(err, "Unable to register route")
			return registerRouteError, err
		}
	}

	// Delay status update until this point, so it will not be executed if CR delete is ongoing
	if rw.addToStatus(params.options.Hostname, gateway) {
		logger.Info("Update the StaticRoute status", "staticroute", rw.instance.Status)
		if err := params.client.Status().Update(context.Background(), rw.instance); err != nil {
			logger.Error(err, "failed to update the staticroute")
			return addStatusUpdateError, err
		}
	}

	logger.Info("Reconciliation done, no add, no delete.")
	return finished, nil
}

func convertToOperator(operator metav1.LabelSelectorOperator) (selection.Operator, error) {
	switch operator {
	case metav1.LabelSelectorOpIn:
		return selection.In, nil
	case metav1.LabelSelectorOpNotIn:
		return selection.NotIn, nil
	case metav1.LabelSelectorOpExists:
		return selection.Exists, nil
	case metav1.LabelSelectorOpDoesNotExist:
		return selection.DoesNotExist, nil
	default:
		return selection.Equals, errors.New("Unable to convert operator")
	}
}
