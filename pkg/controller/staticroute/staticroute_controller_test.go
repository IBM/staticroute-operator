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
	"errors"
	"net"
	"testing"

	iksv1 "github.com/IBM/staticroute-operator/pkg/apis/iks/v1"
	"github.com/IBM/staticroute-operator/pkg/routemanager"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcileImplCRGetFatalError(t *testing.T) {
	//err "no kind is registered for the type v1."" because fake client doesn't have CRD
	params := newReconcileImplParams(fake.NewFakeClient())

	res, err := reconcileImpl(params)

	if res != crGetError {
		t.Error("Result must be notFound")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplCRGetNotFound(t *testing.T) {
	route := &iksv1.StaticRoute{}

	params := newReconcileImplParams(newFakeClient(route))

	res, err := reconcileImpl(params)

	if res != crNotFound {
		t.Error("Result must be crNotFound")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplNotSameZone(t *testing.T) {
	route := newStaticRouteWithValues()
	route.ObjectMeta.Labels = map[string]string{ZoneLabel: "b"}

	params := newReconcileImplParams(newFakeClient(route))
	params.options.Zone = "a"

	res, err := reconcileImpl(params)

	if res != notSameZone {
		t.Error("Result must be notSameZone")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplNotDeleted(t *testing.T) {
	route := newStaticRouteWithValues()
	route.Status = iksv1.StaticRouteStatus{
		NodeStatus: []iksv1.StaticRouteNodeStatus{
			iksv1.StaticRouteNodeStatus{
				Hostname: "hostname",
			},
		},
	}
	mockClient := reconcileImplClientMock{
		client: newFakeClient(route),
		postfixGet: func(obj runtime.Object) {
			obj.(*iksv1.StaticRoute).SetDeletionTimestamp(&v1.Time{})
		},
	}
	params := newReconcileImplParams(mockClient)
	params.options.Hostname = "hostname2"
	params.options.RouteManager = routeManagerMock{}

	res, err := reconcileImpl(params)

	if res != alreadyDeleted {
		t.Error("Result must be alreadyDeleted")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplDeleted(t *testing.T) {
	route := newStaticRouteWithValues()
	route.Status = iksv1.StaticRouteStatus{
		NodeStatus: []iksv1.StaticRouteNodeStatus{
			iksv1.StaticRouteNodeStatus{
				Hostname: "hostname",
			},
		},
	}
	mockClient := reconcileImplClientMock{
		client: newFakeClient(route),
		postfixGet: func(obj runtime.Object) {
			obj.(*iksv1.StaticRoute).SetDeletionTimestamp(&v1.Time{})
		},
	}
	params := newReconcileImplParams(mockClient)
	params.options.Hostname = "hostname"
	params.options.RouteManager = routeManagerMock{}

	res, err := reconcileImpl(params)

	if res != deletionFinished {
		t.Error("Result must be deletionFinished")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplUpdated(t *testing.T) {
	route := newStaticRouteWithValues()
	route.Spec = iksv1.StaticRouteSpec{
		Subnet:  "subnet2",
		Gateway: "gateway",
	}
	route.Status = iksv1.StaticRouteStatus{
		NodeStatus: []iksv1.StaticRouteNodeStatus{
			iksv1.StaticRouteNodeStatus{
				Hostname: "hostname",
				State: iksv1.StaticRouteSpec{
					Subnet:  "subnet",
					Gateway: "gateway",
				},
			},
		},
	}
	params := newReconcileImplParams(newFakeClient(route))
	params.options.Hostname = "hostname"
	params.options.RouteManager = routeManagerMock{}

	res, err := reconcileImpl(params)

	if res != updateFinished {
		t.Error("Result must be updateFinished")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplDeletedButCantDeregister(t *testing.T) {
	route := newStaticRouteWithValues()
	route.Status = iksv1.StaticRouteStatus{
		NodeStatus: []iksv1.StaticRouteNodeStatus{
			iksv1.StaticRouteNodeStatus{
				Hostname: "hostname",
			},
		},
	}
	mockClient := reconcileImplClientMock{
		client: newFakeClient(route),
		postfixGet: func(obj runtime.Object) {
			obj.(*iksv1.StaticRoute).SetDeletionTimestamp(&v1.Time{})
		},
	}
	params := newReconcileImplParams(mockClient)
	params.options.Hostname = "hostname"
	params.options.RouteManager = routeManagerMock{
		deRegisterRouteErr: errors.New("Couldn't deregister route"),
	}

	res, err := reconcileImpl(params)

	if res != deRegisterError {
		t.Error("Result must be deRegisterError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplDeletedButCantDeleteStatus(t *testing.T) {
	route := newStaticRouteWithValues()
	route.Status = iksv1.StaticRouteStatus{
		NodeStatus: []iksv1.StaticRouteNodeStatus{
			iksv1.StaticRouteNodeStatus{
				Hostname: "hostname",
			},
		},
	}
	mockClient := reconcileImplClientMock{
		client: newFakeClient(route),
		postfixGet: func(obj runtime.Object) {
			obj.(*iksv1.StaticRoute).SetDeletionTimestamp(&v1.Time{})
		},
		statusWriteMock: statusWriterMock{
			updateErr: errors.New("Couldn't update status"),
		},
	}
	params := newReconcileImplParams(mockClient)
	params.options.Hostname = "hostname"
	params.options.RouteManager = routeManagerMock{}

	res, err := reconcileImpl(params)

	if res != delStatusUpdateError {
		t.Error("Result must be delStatusUpdateError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplDeletedButCantEmptyFinalizers(t *testing.T) {
	route := newStaticRouteWithValues()
	route.Status = iksv1.StaticRouteStatus{
		NodeStatus: []iksv1.StaticRouteNodeStatus{
			iksv1.StaticRouteNodeStatus{
				Hostname: "hostname",
			},
		},
	}
	mockClient := reconcileImplClientMock{
		client: newFakeClient(route),
		postfixGet: func(obj runtime.Object) {
			obj.(*iksv1.StaticRoute).SetDeletionTimestamp(&v1.Time{})
		},
		updateErr: errors.New("Couldn't empty finalizers"),
	}
	params := newReconcileImplParams(mockClient)
	params.options.Hostname = "hostname"
	params.options.RouteManager = routeManagerMock{}

	res, err := reconcileImpl(params)

	if res != emptyFinalizerError {
		t.Error("Result must be emptyFinalizerError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplIsNewButCantSetFinalizers(t *testing.T) {
	route := newStaticRouteWithValues()
	mockClient := reconcileImplClientMock{
		client:    newFakeClient(route),
		updateErr: errors.New("Couldn't fill finalizers"),
	}
	params := newReconcileImplParams(mockClient)

	res, err := reconcileImpl(params)

	if res != setFinalizerError {
		t.Error("Result must be setFinalizerError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplIsNotRegisteredButCantParseSubnet(t *testing.T) {
	route := newStaticRouteWithValues()
	route.Spec.Gateway = "10.0.0.1"
	route.Spec.Subnet = "invalid-subnet"
	params := newReconcileImplParams(newFakeClient(route))
	params.options.RouteManager = routeManagerMock{}

	res, err := reconcileImpl(params)

	if res != parseSubnetError {
		t.Error("Result must be parseSubnetError")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplIsNotRegisteredButCantRegister(t *testing.T) {
	route := newStaticRouteWithValues()
	route.Spec.Gateway = "10.0.0.1"
	route.Spec.Subnet = "10.0.0.1/16"
	params := newReconcileImplParams(newFakeClient(route))
	params.options.RouteManager = routeManagerMock{
		registerRouteErr: errors.New("Couldn't register route"),
	}

	res, err := reconcileImpl(params)

	if res != registerRouteError {
		t.Error("Result must be registerRouteError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplIsRegisteredButCantAddStatus(t *testing.T) {
	route := newStaticRouteWithValues()
	route.Spec.Gateway = "10.0.0.1"
	mockClient := reconcileImplClientMock{
		client: newFakeClient(route),
		statusWriteMock: statusWriterMock{
			updateErr: errors.New("Couldn't update status"),
		},
	}
	params := newReconcileImplParams(mockClient)
	params.options.Hostname = "hostname"
	params.options.RouteManager = routeManagerMock{
		isRegistered: true,
	}

	res, err := reconcileImpl(params)

	if res != addStatusUpdateError {
		t.Error("Result must be addStatusUpdateError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplCantDetermineGateway(t *testing.T) {
	route := newStaticRouteWithValues()
	params := newReconcileImplParams(newFakeClient(route))
	params.options.RouteGet = func() (net.IP, error) {
		return nil, errors.New("Can't determine gateway")
	}

	res, err := reconcileImpl(params)

	if res != routeGetError {
		t.Error("Result must be routeGetError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplDetermineGateway(t *testing.T) {
	var gatewayParam string

	route := newStaticRouteWithValues()
	route.Spec.Subnet = "10.0.0.1/16"
	params := newReconcileImplParams(newFakeClient(route))
	params.options.RouteGet = func() (net.IP, error) {
		return net.IP{10, 0, 0, 1}, nil
	}
	params.options.Hostname = "hostname"
	params.options.RouteManager = routeManagerMock{
		isRegistered: false,
		registeredCallback: func(n string, r routemanager.Route) error {
			gatewayParam = r.Gw.String()
			return nil
		},
	}

	//nolint:errcheck
	reconcileImpl(params)

	if gatewayParam != "10.0.0.1" {
		t.Errorf("Wrong gateway selected: %s", gatewayParam)
	}
}

func TestReconcileImpl(t *testing.T) {
	route := newStaticRouteWithValues()
	route.Spec.Gateway = "10.0.0.1"
	params := newReconcileImplParams(newFakeClient(route))
	params.options.Hostname = "hostname"
	params.options.RouteManager = routeManagerMock{
		isRegistered: true,
	}

	res, err := reconcileImpl(params)

	if res != finished {
		t.Error("Result must be finished")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestRouteWrapperSetFinalizer(t *testing.T) {
	route := newStaticRouteWithValues()
	rw := routeWrapper{instance: route}

	done := rw.setFinalizer()

	if !done || len(route.GetFinalizers()) == 0 {
		t.Error("Finalizer must be added")
	} else if route.GetFinalizers()[0] != "finalizer.iks.ibm.com" {
		t.Errorf("`finalizer.iks.ibm.com` not setted as finalizer: %v", route.GetFinalizers())
	}
}

func TestRouteWrapperSetFinalizerNotEmpty(t *testing.T) {
	route := newStaticRouteWithValues()
	route.SetFinalizers([]string{"finalizer"})
	rw := routeWrapper{instance: route}

	done := rw.setFinalizer()

	if done {
		t.Error("Finalizer must be not added")
	}
}

func TestRouteWrapperSameZone(t *testing.T) {
	route := newStaticRouteWithValues()
	route.Labels = map[string]string{ZoneLabel: "a"}
	rw := routeWrapper{instance: route}

	same := rw.isSameZone("a", ZoneLabel)

	if !same {
		t.Error("Must be in same zone")
	}
}

func TestRouteWrapperNotSameZone(t *testing.T) {
	route := newStaticRouteWithValues()
	route.Labels = map[string]string{ZoneLabel: "a"}
	rw := routeWrapper{instance: route}

	same := rw.isSameZone("b", ZoneLabel)

	if same {
		t.Error("Must not be in same zone")
	}
}

func TestRouteWrapperGetGatewayMissing(t *testing.T) {
	route := newStaticRouteWithValues()
	rw := routeWrapper{instance: route}

	gw := rw.getGateway()

	if gw != nil {
		t.Errorf("Gateway must be nil: %s", gw.String())
	}
}

func TestRouteWrapperGetGatewayInvalid(t *testing.T) {
	route := newStaticRouteWithValues()
	route.Spec.Gateway = "invalid-gateway"
	rw := routeWrapper{instance: route}

	gw := rw.getGateway()

	if gw != nil {
		t.Errorf("Gateway must be nil: %s", gw.String())
	}
}
