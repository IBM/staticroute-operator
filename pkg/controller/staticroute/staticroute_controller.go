package staticroute

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	iksv1 "github.com/IBM-Cloud/kube-samples/staticroute-operator/pkg/apis/iks/v1"
	"github.com/IBM-Cloud/kube-samples/staticroute-operator/pkg/util"
	corev1 "k8s.io/api/core/v1"
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
	Hostname string
	Zone     string
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

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner StaticRoute
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &iksv1.StaticRoute{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileStaticRoute implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileStaticRoute{}

// Reconcile reads that state of the cluster for a StaticRoute object and makes changes based on the state read
// and what is in the StaticRoute.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileStaticRoute) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling StaticRoute")

	// Fetch the StaticRoute instance
	instance := &iksv1.StaticRoute{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
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

	isDeleted := instance.GetDeletionTimestamp() != nil
	if isDeleted {
		if len(instance.Status.NodeStatus) > 0 && removeFromStatusIfExist(instance, r.options.Hostname) {
			// Here comes the DELETE logic
			err := delStaticRoute(instance.Spec.Subnet)
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
			err = r.client.Update(context.TODO(), instance)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	isNew := len(instance.Finalizers) == 0
	if isNew {
		// We are the first one
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

	if addToStatusIfNotExist(instance, r.options.Hostname) {
		// Here comes the ADD logic
		// note that if "gateway" is still empty, we'll create the route through the default private network gateway
		if gateway == "" {
			gateway, err = getFallbackGateway()
			if err != nil {
				reqLogger.Info("Unable to retrieve fallback gateway")
				return reconcile.Result{}, err
			}
		}

		err = addStaticRoute(instance.Spec.Subnet, gateway)
		if err != nil {
			return reconcile.Result{}, err
		}

		reqLogger.Info("Update the StaticRoute status", "staticroute", instance.Status)
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
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
			Hostname:   hostname,
			Generation: m.Generation,
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

func getRouteDevice(subnet string) (string, error) {
	// find the device that the subnet is being routed through
	re := regexp.MustCompile(`(?s).*dev ([^\s]*) .*`)
	out, _, err := util.ExecIpCmd(fmt.Sprintf("route get %s", subnet))
	if err != nil {
		return "", err
	}

	device := re.ReplaceAllString(out, "$1")

	return device, nil
}

func getFallbackGateway() (string, error) {
	// if no gateway is defined, just use the same gateway that 10.0.0.0/8 uses and assume
	// that the edge device will route/NAT us to the network
	re := regexp.MustCompile(`(?s).*via ([^\s]*) .*`)
	out, _, err := util.ExecIpCmd(fmt.Sprintf("route get 10.0.0.0/8"))
	if err != nil {
		return "", err
	}

	myGateway := re.ReplaceAllString(out, "$1")

	return myGateway, nil
}

func addStaticRoute(subnet string, gateway string) error {
	// check if route already exists
	out, code, err := util.ExecIpCmd(fmt.Sprintf("route show %s", subnet))
	if err != nil {
		return err
	}

	re := regexp.MustCompile(`(?s).*via ([^\s]*) .*`)

	// if route exists already
	if out != "" {
		currGateway := re.ReplaceAllString(out, "$1")

		if currGateway == gateway {
			log.Info(fmt.Sprintf("Route for %s via %s already exists", subnet, gateway))
			return nil
		}

		// delete the route if the gateway doesn't match
		out, code, err := util.ExecIpCmd(fmt.Sprintf("route del %s via %s", subnet, currGateway))
		if err != nil {
			return err
		}

		if code != 0 {
			return fmt.Errorf("Error executing \"ip route del\", output: %s", out)
		}
	}

	// add the new route
	out, code, err = util.ExecIpCmd(fmt.Sprintf("route add %s via %s", subnet, gateway))
	if err != nil {
		return err
	}

	if code != 0 {
		return fmt.Errorf("Error executing \"ip route add\", output: %s", out)
	}

	return nil
}

func delStaticRoute(subnet string) error {
	// check if route already exists
	out, code, err := util.ExecIpCmd(fmt.Sprintf("route show %s", subnet))
	if err != nil {
		return err
	}

	// if route already doesn't exists
	if out == "" {
		log.Info(fmt.Sprintf("Route for %s already doesn't exist", subnet))
		return nil
	}

	// delete the route
	out, code, err = util.ExecIpCmd(fmt.Sprintf("route del %s", strings.TrimSuffix(out, " \n")))
	if err != nil {
		return err
	}

	if code != 0 {
		return fmt.Errorf("Error executing \"ip route del\", output: %s", out)
	}

	return nil
}
