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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestConvertTooperator(t *testing.T) {
	var testData = []struct {
		in  metav1.LabelSelectorOperator
		out selection.Operator
	}{
		{
			in:  metav1.LabelSelectorOpIn,
			out: selection.In,
		},
		{
			in:  metav1.LabelSelectorOpNotIn,
			out: selection.NotIn,
		},
		{
			in:  metav1.LabelSelectorOpExists,
			out: selection.Exists,
		},
		{
			in:  metav1.LabelSelectorOpDoesNotExist,
			out: selection.DoesNotExist,
		},
	}

	for i, td := range testData {
		out, _ := convertToOperator(td.in)

		if out != td.out {
			t.Errorf("Result must be %v, it is %v at %d", td.out, out, i)
		}
	}
}

func TestReconcileImpl(t *testing.T) {
	params, _ := getReconcileContextForAddFlow(nil, true)

	res, err := reconcileImpl(*params)

	if res != finished {
		t.Error("Result must be finished")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplCRGetFatalError(t *testing.T) {
	//err "no kind is registered for the type v1."" because fake client doesn't have CRD
	params, _ := getReconcileContextForAddFlow(nil, true)
	params.client = fake.NewFakeClient()

	res, err := reconcileImpl(*params)

	if res != crGetError {
		t.Error("Result must be notFound")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplCRGetNotFound(t *testing.T) {
	route := &iksv1.StaticRoute{}
	params, _ := getReconcileContextForAddFlow(route, true)

	res, err := reconcileImpl(*params)

	if res != crNotFound {
		t.Error("Result must be crNotFound")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplNotSameZone(t *testing.T) {
	params, _ := getReconcileContextForAddFlow(nil, true)
	params.options.Zone = "a"

	res, err := reconcileImpl(*params)

	if res != notSameZone {
		t.Error("Result must be notSameZone")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplProtected(t *testing.T) {
	params, _ := getReconcileContextForAddFlow(nil, true)
	params.options.ProtectedSubnets = []*net.IPNet{&net.IPNet{IP: net.IP{10, 0, 0, 0}, Mask: net.IPv4Mask(0xff, 0, 0, 0)}}

	res, err := reconcileImpl(*params)

	if res != overlapsProtected {
		t.Error("Result must be overlapsProtected")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplNotDeleted(t *testing.T) {
	route := newStaticRouteWithValues(true, false)
	params, mockClient := getReconcileContextForAddFlow(route, true)
	mockClient.postfixGet = func(obj runtime.Object) {
		obj.(*iksv1.StaticRoute).SetDeletionTimestamp(&v1.Time{})
	}

	res, err := reconcileImpl(*params)

	if res != alreadyDeleted {
		t.Error("Result must be alreadyDeleted")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplUpdated(t *testing.T) {
	route := newStaticRouteWithValues(true, false)
	route.Status = iksv1.StaticRouteStatus{
		NodeStatus: []iksv1.StaticRouteNodeStatus{
			iksv1.StaticRouteNodeStatus{
				Hostname: "hostname",
				State: iksv1.StaticRouteSpec{
					Subnet:  "11.1.1.1/16",
					Gateway: "10.0.0.1",
				},
			},
		},
	}
	params, _ := getReconcileContextForAddFlow(route, true)

	res, err := reconcileImpl(*params)

	if res != updateFinished {
		t.Error("Result must be updateFinished")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplNodeSelectorMissingOp(t *testing.T) {
	route := newStaticRouteWithValues(true, false)
	route.Spec.Selectors = []metav1.LabelSelectorRequirement{metav1.LabelSelectorRequirement{
		Key:    "key",
		Values: []string{"value"},
	}}
	params, mockClient := getReconcileContextForAddFlow(route, true)
	mockClient.listErr = errors.New("Couldn't fetch nodes")

	res, err := reconcileImpl(*params)

	if res != wrongSelectorErr {
		t.Error("Result must be wrongSelectorErr")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplNodeSelectorInvalid(t *testing.T) {
	route := newStaticRouteWithValues(true, false)
	route.Spec.Selectors = []metav1.LabelSelectorRequirement{metav1.LabelSelectorRequirement{
		Key:      "",
		Operator: metav1.LabelSelectorOpIn,
		Values:   []string{"value"},
	}}
	params, mockClient := getReconcileContextForAddFlow(route, true)
	mockClient.listErr = errors.New("Couldn't fetch nodes")

	res, err := reconcileImpl(*params)

	if res != wrongSelectorErr {
		t.Error("Result must be wrongSelectorErr")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplNodeSelectorFatalError(t *testing.T) {
	route := newStaticRouteWithValues(true, false)
	route.Spec.Selectors = []metav1.LabelSelectorRequirement{metav1.LabelSelectorRequirement{
		Key:      "key",
		Operator: metav1.LabelSelectorOpIn,
		Values:   []string{"value"},
	}}
	params, mockClient := getReconcileContextForAddFlow(route, true)
	mockClient.listErr = errors.New("Couldn't fetch nodes")

	res, err := reconcileImpl(*params)

	if res != nodeGetError {
		t.Error("Result must be nodeGetError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplNodeSelectorNotFound(t *testing.T) {
	route := newStaticRouteWithValues(true, false)
	route.Spec.Selectors = []metav1.LabelSelectorRequirement{metav1.LabelSelectorRequirement{
		Key:      "key",
		Operator: metav1.LabelSelectorOpIn,
		Values:   []string{"value"},
	}}
	params, _ := getReconcileContextForAddFlow(route, true)

	res, err := reconcileImpl(*params)

	if res != nodeNotFound {
		t.Error("Result must be nodeNotFound")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplDeleted(t *testing.T) {
	params, mockClient := getReconcileContextForAddFlow(nil, true)
	mockClient.postfixGet = func(obj runtime.Object) {
		obj.(*iksv1.StaticRoute).SetDeletionTimestamp(&v1.Time{})
	}

	res, err := reconcileImpl(*params)

	if res != deletionFinished {
		t.Error("Result must be deletionFinished")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplDeletedIfRouteNotFound(t *testing.T) {
	params, mockClient := getReconcileContextForAddFlow(nil, true)
	params.options.RouteManager = routeManagerMock{
		deRegisterRouteErr: routemanager.ErrNotFound,
	}
	mockClient.postfixGet = func(obj runtime.Object) {
		obj.(*iksv1.StaticRoute).SetDeletionTimestamp(&v1.Time{})
	}

	res, err := reconcileImpl(*params)

	if res != deletionFinished {
		t.Error("Result must be deletionFinished")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplDeletedButCantDeregister(t *testing.T) {
	params, mockClient := getReconcileContextForAddFlow(nil, true)
	params.options.RouteManager = routeManagerMock{
		deRegisterRouteErr: errors.New("Couldn't deregister route"),
	}
	mockClient.postfixGet = func(obj runtime.Object) {
		obj.(*iksv1.StaticRoute).SetDeletionTimestamp(&v1.Time{})
	}

	res, err := reconcileImpl(*params)

	if res != deRegisterError {
		t.Error("Result must be deRegisterError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplDeletedButCantDeleteStatus(t *testing.T) {
	params, mockClient := getReconcileContextForAddFlow(nil, true)
	mockClient.postfixGet = func(obj runtime.Object) {
		obj.(*iksv1.StaticRoute).SetDeletionTimestamp(&v1.Time{})
	}
	mockClient.statusWriteMock = statusWriterMock{
		updateErr: errors.New("Couldn't update status"),
	}

	res, err := reconcileImpl(*params)

	if res != delStatusUpdateError {
		t.Error("Result must be delStatusUpdateError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplDeletedButCantEmptyFinalizers(t *testing.T) {
	params, mockClient := getReconcileContextForAddFlow(nil, true)
	mockClient.postfixGet = func(obj runtime.Object) {
		obj.(*iksv1.StaticRoute).SetDeletionTimestamp(&v1.Time{})
	}
	mockClient.updateErr = errors.New("Couldn't empty finalizers")

	res, err := reconcileImpl(*params)

	if res != emptyFinalizerError {
		t.Error("Result must be emptyFinalizerError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplIsNewButCantSetFinalizers(t *testing.T) {
	params, mockClient := getReconcileContextForAddFlow(nil, true)
	mockClient.updateErr = errors.New("Couldn't fill finalizers")

	res, err := reconcileImpl(*params)

	if res != setFinalizerError {
		t.Error("Result must be setFinalizerError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplIsNotRegisteredButCantParseSubnet(t *testing.T) {
	route := newStaticRouteWithValues(true, false)
	route.Spec.Subnet = "invalid-subnet"
	params, _ := getReconcileContextForAddFlow(route, false)

	res, err := reconcileImpl(*params)

	if res != parseSubnetError {
		t.Error("Result must be parseSubnetError")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplIsNotRegisteredButCantRegister(t *testing.T) {
	params, _ := getReconcileContextForAddFlow(nil, true)
	params.options.RouteManager = routeManagerMock{
		registerRouteErr: errors.New("Couldn't register route"),
	}

	res, err := reconcileImpl(*params)

	if res != registerRouteError {
		t.Error("Result must be registerRouteError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplIsRegisteredButCantAddStatus(t *testing.T) {
	params, mockClient := getReconcileContextForAddFlow(nil, true)
	params.options.Hostname = "hostname2"
	mockClient.statusWriteMock = statusWriterMock{
		updateErr: errors.New("Couldn't update status"),
	}

	res, err := reconcileImpl(*params)

	if res != addStatusUpdateError {
		t.Error("Result must be addStatusUpdateError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplInvalidGateway(t *testing.T) {
	route := newStaticRouteWithValues(true, true)
	route.Spec.Gateway = "invalid-gateway"
	params, _ := getReconcileContextForAddFlow(route, true)

	res, err := reconcileImpl(*params)

	if res != invalidGatewayError {
		t.Error("Result must be invalidGatewayError")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplCantDetermineGateway(t *testing.T) {
	route := newStaticRouteWithValues(true, true)
	route.Spec.Gateway = ""
	params, _ := getReconcileContextForAddFlow(route, true)
	params.options.RouteGet = func() (net.IP, error) {
		return nil, errors.New("Can't determine gateway")
	}

	res, err := reconcileImpl(*params)

	if res != routeGetError {
		t.Error("Result must be routeGetError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplDetermineGateway(t *testing.T) {
	var gatewayParam string

	route := newStaticRouteWithValues(true, true)
	route.Spec.Gateway = ""
	params, _ := getReconcileContextForAddFlow(route, false)
	params.options.RouteGet = func() (net.IP, error) {
		return net.IP{10, 0, 0, 1}, nil
	}
	params.options.RouteManager = routeManagerMock{
		isRegistered: false,
		registeredCallback: func(n string, r routemanager.Route) error {
			gatewayParam = r.Gw.String()
			return nil
		},
	}

	//nolint:errcheck
	reconcileImpl(*params)

	if gatewayParam != "10.0.0.1" {
		t.Errorf("Wrong gateway selected: %s", gatewayParam)
	}
}

func getReconcileContextForAddFlow(route *iksv1.StaticRoute, isRegistered bool) (*reconcileImplParams, *reconcileImplClientMock) {
	if route == nil {
		route = newStaticRouteWithValues(true, true)
	}
	mockClient := reconcileImplClientMock{
		client: newFakeClient(route),
	}
	params := newReconcileImplParams(&mockClient)
	params.options.Zone = "zone"
	params.options.Hostname = "hostname"
	params.options.RouteManager = routeManagerMock{
		isRegistered: isRegistered,
	}

	return params, &mockClient
}
