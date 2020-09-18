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

	iksv1 "github.com/IBM/staticroute-operator/pkg/apis/iks/v1"
	"github.com/IBM/staticroute-operator/pkg/routemanager"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type reconcileImplClientMock struct {
	client          reconcileImplClient
	statusWriteMock client.StatusWriter
	postfixGet      func(runtime.Object)
	getErr          error
	updateErr       error
	listErr         error
}

func (m reconcileImplClientMock) Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
	if m.getErr != nil {
		return m.getErr
	}
	if m.postfixGet != nil {
		defer m.postfixGet(obj)
	}
	return m.client.Get(ctx, key, obj)
}

func (m reconcileImplClientMock) Update(ctx context.Context, obj runtime.Object, options ...client.UpdateOption) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	return m.client.Update(ctx, obj, options...)
}

func (m reconcileImplClientMock) List(ctx context.Context, list runtime.Object, options ...client.ListOption) error {
	if m.listErr != nil {
		return m.listErr
	}
	return m.client.List(ctx, list, options...)
}

func (m reconcileImplClientMock) Status() client.StatusWriter {
	if m.statusWriteMock != nil {
		return m.statusWriteMock
	}
	return m.client.Status()
}

type statusWriterMock struct {
	updateCounter int
	patchCounter  int
	updateErr     error
	patchErr      error
}

func (m *statusWriterMock) Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	m.updateCounter = m.updateCounter + 1
	return m.updateErr
}

func (m *statusWriterMock) Patch(context.Context, runtime.Object, client.Patch, ...client.PatchOption) error {
	m.patchCounter = m.patchCounter + 1
	return m.patchErr
}

type routeManagerMock struct {
	isRegistered       bool
	registeredCallback func(string, routemanager.Route) error
	registerRouteErr   error
	deRegisterRouteErr error
}

func (m routeManagerMock) IsRegistered(string) bool {
	return m.isRegistered
}

func (m routeManagerMock) RegisterRoute(n string, r routemanager.Route) error {
	if m.registeredCallback != nil {
		return m.registeredCallback(n, r)
	}
	return m.registerRouteErr
}

func (m routeManagerMock) DeRegisterRoute(string) error {
	return m.deRegisterRouteErr
}

func (m routeManagerMock) RegisterWatcher(routemanager.RouteWatcher) {
}

func (m routeManagerMock) DeRegisterWatcher(routemanager.RouteWatcher) {
}

func (m routeManagerMock) Run(chan struct{}) error {
	return nil
}

func newFakeClient(route *iksv1.StaticRoute) client.Client {
	s := runtime.NewScheme()
	s.AddKnownTypes(iksv1.SchemeGroupVersion, route)
	nodes := &corev1.NodeList{}
	s.AddKnownTypes(corev1.SchemeGroupVersion, nodes)
	return fake.NewFakeClientWithScheme(s, []runtime.Object{route}...)
}

func newReconcileImplParams(client reconcileImplClient) *reconcileImplParams {
	return &reconcileImplParams{
		request: reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "CR",
				Namespace: "default",
			},
		},
		client:  client,
		options: ManagerOptions{},
	}
}

func newStaticRouteWithValues(withSpec, withStatus bool) *iksv1.StaticRoute {
	route := iksv1.StaticRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "CR",
			Namespace: "default",
		},
	}
	if withSpec {
		route.Spec = iksv1.StaticRouteSpec{
			Gateway: "10.0.0.1",
			Subnet:  "10.0.0.1/16",
		}
	}
	if withStatus {
		route.Status = iksv1.StaticRouteStatus{
			NodeStatus: []iksv1.StaticRouteNodeStatus{
				iksv1.StaticRouteNodeStatus{
					Hostname: "hostname",
					State: iksv1.StaticRouteSpec{
						Subnet:  "10.0.0.1/16",
						Gateway: "10.0.0.1",
					},
				},
			},
		}
	}
	return &route
}
