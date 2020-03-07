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

package node

import (
	"context"
	"errors"
	"testing"

	iksv1 "github.com/IBM/staticroute-operator/pkg/apis/iks/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFindNodeFound(t *testing.T) {
	nf := nodeFinder{
		nodeName: "foo",
	}
	route := &iksv1.StaticRoute{
		Status: iksv1.StaticRouteStatus{
			NodeStatus: []iksv1.StaticRouteNodeStatus{
				iksv1.StaticRouteNodeStatus{Hostname: "bar"},
				iksv1.StaticRouteNodeStatus{Hostname: "foo"},
			},
		},
	}

	index := nf.findNode(route)

	if 1 != index {
		t.Errorf("Index not match 1 == %d", index)
	}
}

func TestFindNodeNotFound(t *testing.T) {
	nf := nodeFinder{
		nodeName: "not-found",
	}
	route := &iksv1.StaticRoute{
		Status: iksv1.StaticRouteStatus{
			NodeStatus: []iksv1.StaticRouteNodeStatus{
				iksv1.StaticRouteNodeStatus{Hostname: "bar"},
				iksv1.StaticRouteNodeStatus{Hostname: "foo"},
			},
		},
	}

	index := nf.findNode(route)

	if -1 != index {
		t.Errorf("Index not match -1 == %d", index)
	}
}

func TestDelete(t *testing.T) {
	var updateInputParam *iksv1.StaticRoute
	nf := nodeFinder{
		nodeName: "to-delete",
		updateCallback: func(r *iksv1.StaticRoute) error {
			updateInputParam = r
			return nil
		},
		infoLogger: func(string, ...interface{}) {},
	}
	routes := &iksv1.StaticRouteList{
		Items: []iksv1.StaticRoute{
			iksv1.StaticRoute{
				Status: iksv1.StaticRouteStatus{
					NodeStatus: []iksv1.StaticRouteNodeStatus{
						iksv1.StaticRouteNodeStatus{Hostname: "foo"},
						iksv1.StaticRouteNodeStatus{Hostname: "to-delete"},
						iksv1.StaticRouteNodeStatus{Hostname: "bar"},
					},
				},
			},
		},
	}

	//nolint:errcheck
	nf.delete(routes)

	if updateInputParam == nil {
		t.Errorf("Update clannback was not called or called with nil")
	} else if len(updateInputParam.Status.NodeStatus) != 2 {
		t.Errorf("Node deletion went fail")
	} else if act := updateInputParam.Status.NodeStatus[0].Hostname + updateInputParam.Status.NodeStatus[1].Hostname; act != "foobar" {
		t.Errorf("Not the right status was deleted 'foobar' == %s", act)
	}
}

func TestReconcileImpl(t *testing.T) {
	var statusUpdateCalled bool
	statusUpdateCallback := func() client.StatusWriter {
		statusUpdateCalled = true
		return nil
	}
	params, _ := getReconcileContextForHappyFlow(statusUpdateCallback)

	res, err := reconcileImpl(*params)

	if res != finished {
		t.Error("Result must be finished")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
	if statusUpdateCalled {
		t.Error("Status update called")
	}
}

func TestReconcileImplNodeGetNodeFound(t *testing.T) {
	params, mockClient := getReconcileContextForHappyFlow(nil)
	mockClient.get = func(context.Context, client.ObjectKey, runtime.Object) error {
		return nil
	}

	res, err := reconcileImpl(*params)

	if res != nodeStillExists {
		t.Error("Result must be nodeStillExists")
	}
	if err != nil {
		t.Errorf("Error must be nil: %s", err.Error())
	}
}

func TestReconcileImplNodeGetNodeFatalError(t *testing.T) {
	params, mockClient := getReconcileContextForHappyFlow(nil)
	mockClient.get = func(context.Context, client.ObjectKey, runtime.Object) error {
		return errors.New("fatal error")
	}

	res, err := reconcileImpl(*params)

	if res != nodeGetError {
		t.Error("Result must be nodeGetError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileImplNodeCRListError(t *testing.T) {
	//err "no kind is registered for the type v1."" because fake client doesn't have CRD
	params, mockClient := getReconcileContextForHappyFlow(nil)
	mockClient.client = fake.NewFakeClient()
	mockClient.get = func(context.Context, client.ObjectKey, runtime.Object) error {
		return kerrors.NewNotFound(schema.GroupResource{}, "name")
	}

	res, err := reconcileImpl(*params)

	if res != staticRouteListError {
		t.Error("Result must be staticRouteListError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
}

func TestReconcileDeleteError(t *testing.T) {
	var statusUpdateCalled bool
	statusWriteMock := statusWriterMock{
		updateErr: errors.New("update failed"),
	}
	params, mockClient := getReconcileContextForHappyFlow(func() client.StatusWriter {
		statusUpdateCalled = true
		return statusWriteMock
	})
	mockClient.get = func(context.Context, client.ObjectKey, runtime.Object) error {
		return kerrors.NewNotFound(schema.GroupResource{}, "name")
	}
	mockClient.list = func(ctx context.Context, obj runtime.Object, options ...client.ListOption) error {
		iface := obj.(interface{})
		routes := iface.(*iksv1.StaticRouteList)
		routes.Items = []iksv1.StaticRoute{
			iksv1.StaticRoute{
				Status: iksv1.StaticRouteStatus{
					NodeStatus: []iksv1.StaticRouteNodeStatus{
						iksv1.StaticRouteNodeStatus{
							Hostname: "CR",
						},
					},
				},
			},
		}
		return nil
	}

	res, err := reconcileImpl(*params)

	if res != deleteRouteError {
		t.Error("Result must be deleteRouteError")
	}
	if err == nil {
		t.Error("Error must be not nil")
	}
	if !statusUpdateCalled {
		t.Error("Status update called")
	}
}

func getReconcileContextForHappyFlow(statusUpdateCallback func() client.StatusWriter) (*reconcileImplParams, *reconcileImplClientMock) {
	routes := &iksv1.StaticRouteList{}
	mockClient := reconcileImplClientMock{
		client: newFakeClient(routes),
		get: func(context.Context, client.ObjectKey, runtime.Object) error {
			return kerrors.NewNotFound(schema.GroupResource{}, "name")
		},
	}
	if statusUpdateCallback != nil {
		mockClient.status = statusUpdateCallback
	}

	return newReconcileImplParams(&mockClient), &mockClient
}
