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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type reconcileImplClientMock struct {
	client reconcileImplClient
	get    func(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error
	list   func(context.Context, runtime.Object, ...client.ListOption) error
	status func() client.StatusWriter
}

func (m reconcileImplClientMock) Get(ctx context.Context, key client.ObjectKey, obj client.Object, options ...client.GetOption) error {
	if m.get != nil {
		return m.get(ctx, key, obj, options...)
	}
	return m.client.Get(ctx, key, obj, options...)
}

func (m reconcileImplClientMock) List(ctx context.Context, obj client.ObjectList, options ...client.ListOption) error {
	if m.list != nil {
		return m.list(ctx, obj, options...)
	}
	return m.client.List(ctx, obj, options...)
}

func (m reconcileImplClientMock) Status() client.StatusWriter {
	if m.status != nil {
		return m.status()
	}
	return m.client.Status()
}

type statusWriterMock struct {
	createErr error
	updateErr error
	patchErr  error
}

func (m statusWriterMock) Create(ctx context.Context, obj, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return m.createErr
}

func (m statusWriterMock) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return m.updateErr
}

func (m statusWriterMock) Patch(context.Context, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
	return m.patchErr
}

func newReconcileImplParams(client reconcileImplClient) *reconcileImplParams {
	return &reconcileImplParams{
		request: reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "CR",
				Namespace: "default",
			},
		},
		client: client,
	}
}

func newFakeClient(routes *staticroutev1.StaticRouteList) client.Client {
	s := runtime.NewScheme()
	s.AddKnownTypes(staticroutev1.GroupVersion, routes)
	s.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Node{})
	return fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects([]runtime.Object{routes}...).Build()
}
