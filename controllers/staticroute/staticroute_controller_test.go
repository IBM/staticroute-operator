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

package staticroute

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	staticroutev1 "github.com/IBM/staticroute-operator/api/v1"
	"github.com/IBM/staticroute-operator/pkg/routemanager"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
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
	params, _ := getReconcileContextForAddFlow(nil, true, false)

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
	params, _ := getReconcileContextForAddFlow(nil, true, false)
	params.client = fake.NewClientBuilder().Build()

	res, err := reconcileImpl(*params)

	if res != crGetError {
		t.Error("Result must be notFound")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplCRGetNotFound(t *testing.T) {
	route := &staticroutev1.StaticRoute{}
	params, _ := getReconcileContextForAddFlow(route, true, false)

	res, err := reconcileImpl(*params)

	if res != crNotFound {
		t.Error("Result must be crNotFound")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplProtected(t *testing.T) {
	params, _ := getReconcileContextForAddFlow(nil, true, false)
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
	params, _ := getReconcileContextForAddFlow(route, true, true)

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
	route.Status = staticroutev1.StaticRouteStatus{
		NodeStatus: []staticroutev1.StaticRouteNodeStatus{
			staticroutev1.StaticRouteNodeStatus{
				Hostname: "hostname",
				State: staticroutev1.StaticRouteSpec{
					Subnet:  "11.1.1.1/16",
					Gateway: "10.0.0.1",
				},
			},
		},
	}
	params, _ := getReconcileContextForAddFlow(route, true, false)

	res, err := reconcileImpl(*params)

	if res != updateFinished {
		t.Error("Result must be updateFinished")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplUpdatedDoubleReconcile(t *testing.T) {
	route := newStaticRouteWithValues(true, false)
	route.Status = staticroutev1.StaticRouteStatus{
		NodeStatus: []staticroutev1.StaticRouteNodeStatus{
			staticroutev1.StaticRouteNodeStatus{
				Hostname: "hostname",
				State: staticroutev1.StaticRouteSpec{
					Subnet:  "11.1.1.1/16",
					Gateway: "10.0.0.1",
				},
			},
		},
	}
	params, mock := getReconcileContextForDoubleReconcile(route, true)

	res, err := reconcileImpl(*params)

	if res != updateFinished {
		t.Error("Result must be updateFinished")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}

	res, err = reconcileImpl(*params)
	if res != finished {
		t.Error("Result must be finished")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}

	patchCounterBefore := mock.statusWriteMock.(*statusWriterMock).patchCounter
	updateCounterBefore := mock.statusWriteMock.(*statusWriterMock).updateCounter
	_, _ = reconcileImpl(*params)
	_, _ = reconcileImpl(*params)
	_, _ = reconcileImpl(*params)
	_, _ = reconcileImpl(*params)
	_, _ = reconcileImpl(*params)
	patchCounterAfter := mock.statusWriteMock.(*statusWriterMock).patchCounter
	updateCounterAfter := mock.statusWriteMock.(*statusWriterMock).updateCounter

	if patchCounterBefore != patchCounterAfter {
		t.Errorf("Status should not be patched for the third (or more) reconciliation. Before: %d, after: %d", patchCounterBefore, patchCounterAfter)
	}

	if updateCounterBefore != updateCounterAfter {
		t.Errorf("Status should not be updated for the third (or more) reconciliation. Before: %d, after: %d", updateCounterBefore, updateCounterAfter)
	}

	if res != finished {
		t.Error("Result must be finished")
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
	params, mockClient := getReconcileContextForAddFlow(route, true, false)
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
	params, mockClient := getReconcileContextForAddFlow(route, true, false)
	mockClient.listErr = errors.New("Couldn't fetch nodes")

	res, err := reconcileImpl(*params)

	if res != wrongSelectorErr {
		t.Error("Result must be wrongSelectorErr")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplUpdateStatus(t *testing.T) {
	// Initialization
	route := newStaticRouteWithValues(true, false)
	params, mockClient := getReconcileContextForAddFlow(route, true, false)
	params.options.RouteManager = routeManagerMock{
		registerRouteErr: errors.New("Couldn't register route"),
	}

	instance := &staticroutev1.StaticRoute{}
	instanceID := &types.NamespacedName{
		Name:      route.Name,
		Namespace: route.Namespace,
	}

	// Part 1 - route creation fails on purpose, error is set
	res, err := reconcileImpl(*params)
	if res != registerRouteError {
		t.Error("Result must be registerRouteError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
	err = mockClient.Get(context.Background(), *instanceID, instance)
	if err != nil {
		t.Errorf("Failed to read the CR: %s", err.Error())
	}
	if instance.Status.NodeStatus[0].Error != "Couldn't register route" {
		t.Errorf("Error message must be: Couldn't register route: %s", instance.Status.NodeStatus[0].Error)
	}

	// Part 2 - route creation succeeds, error is cleared
	params.options.RouteManager = routeManagerMock{
		isRegistered: true,
	}
	res, err = reconcileImpl(*params)
	if res != finished {
		t.Error("Result must be finished")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
	err = mockClient.Get(context.Background(), *instanceID, instance)
	if err != nil {
		t.Errorf("Failed to read the CR: %s", err.Error())
	}
	if instance.Status.NodeStatus[0].Error != "" {
		t.Errorf("Error message must be empty %s", instance.Status.NodeStatus[0].Error)
	}
}

func TestReconcileImplNodeSelectorFatalError(t *testing.T) {
	route := newStaticRouteWithValues(true, false)
	route.Spec.Selectors = []metav1.LabelSelectorRequirement{metav1.LabelSelectorRequirement{
		Key:      "key",
		Operator: metav1.LabelSelectorOpIn,
		Values:   []string{"value"},
	}}
	params, mockClient := getReconcileContextForAddFlow(route, true, false)
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
	params, _ := getReconcileContextForAddFlow(route, true, false)

	res, err := reconcileImpl(*params)

	if res != nodeNotFound {
		t.Error("Result must be nodeNotFound")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplDeleted(t *testing.T) {
	params, _ := getReconcileContextForAddFlow(nil, true, true)

	res, err := reconcileImpl(*params)

	if res != deletionFinished {
		t.Error("Result must be deletionFinished")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplDeletedIfRouteNotFound(t *testing.T) {
	params, _ := getReconcileContextForAddFlow(nil, true, true)
	params.options.RouteManager = routeManagerMock{
		deRegisterRouteErr: routemanager.ErrNotFound,
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
	params, _ := getReconcileContextForAddFlow(nil, true, true)
	params.options.RouteManager = routeManagerMock{
		deRegisterRouteErr: errors.New("Couldn't deregister route"),
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
	params, mockClient := getReconcileContextForAddFlow(nil, true, true)
	mockClient.statusWriteMock = &statusWriterMock{
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
	params, mockClient := getReconcileContextForAddFlow(nil, true, true)
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
	params, mockClient := getReconcileContextForAddFlow(nil, true, false)
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
	params, _ := getReconcileContextForAddFlow(route, false, false)

	res, err := reconcileImpl(*params)

	if res != parseSubnetError {
		t.Error("Result must be parseSubnetError")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplIsNotRegisteredButCantRegister(t *testing.T) {
	params, _ := getReconcileContextForAddFlow(nil, true, false)
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
	params, mockClient := getReconcileContextForAddFlow(nil, true, false)
	params.options.Hostname = "hostname2"
	mockClient.statusWriteMock = &statusWriterMock{
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
	params, _ := getReconcileContextForAddFlow(route, true, false)

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
	params, _ := getReconcileContextForAddFlow(route, true, false)
	params.options.GetGw = func(net.IP) (net.IP, error) {
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
	params, _ := getReconcileContextForAddFlow(route, false, false)
	params.options.GetGw = func(net.IP) (net.IP, error) {
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

func TestReconcileImplGatewayNotDirectlyRoutable(t *testing.T) {
	route := newStaticRouteWithValues(true, true)
	route.Spec.Gateway = "10.0.10.1"
	params, _ := getReconcileContextForAddFlow(route, true, false)
	params.options.GetGw = func(net.IP) (net.IP, error) {
		return net.IP{10, 0, 0, 1}, nil
	}

	res, err := reconcileImpl(*params)

	if res != gatewayNotDirectlyRoutableError {
		t.Error("Result must be gatewayNotDirectlyRoutableError")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplDeletingWhileGatewayNotDirectlyRoutable(t *testing.T) {
	route := newStaticRouteWithValues(true, true)
	route.Spec.Gateway = "10.0.10.1"
	params, _ := getReconcileContextForAddFlow(route, true, true)
	params.options.GetGw = func(net.IP) (net.IP, error) {
		return net.IP{10, 0, 0, 1}, nil
	}

	res, err := reconcileImpl(*params)

	if res != deletionFinished {
		t.Error("Result must be deletionFinished")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func getReconcileContextForAddFlow(route *staticroutev1.StaticRoute, isRegistered bool, isDeleting bool) (*reconcileImplParams, *reconcileImplClientMock) {
	if route == nil {
		route = newStaticRouteWithValues(true, true)
	}

	if isDeleting {
		route.DeletionTimestamp = &metav1.Time{Time: time.Date(2021, time.Month(2), 21, 1, 10, 30, 0, time.UTC)}
		route.Finalizers = append(route.Finalizers, "finalizer.static-route.ibm.com")
	}
	mockClient := reconcileImplClientMock{
		client: newFakeClient(route),
	}
	params := newReconcileImplParams(&mockClient)
	params.options.Hostname = "hostname"
	params.options.RouteManager = routeManagerMock{
		isRegistered: isRegistered,
	}
	params.options.GetGw = func(net.IP) (net.IP, error) {
		return nil, nil
	}

	return params, &mockClient
}

func getReconcileContextForDoubleReconcile(route *staticroutev1.StaticRoute, isRegistered bool) (*reconcileImplParams, *reconcileImplClientMock) {
	if route == nil {
		route = newStaticRouteWithValues(true, true)
	}
	client := newFakeClient(route)
	mockClient := reconcileImplClientMock{
		client:          client,
		statusWriteMock: &statusWriterMock{client: client},
	}
	params := newReconcileImplParams(&mockClient)
	params.options.Hostname = "hostname"
	params.options.RouteManager = routeManagerMock{
		isRegistered: isRegistered,
	}
	params.options.GetGw = func(net.IP) (net.IP, error) {
		return nil, nil
	}

	return params, &mockClient
}
