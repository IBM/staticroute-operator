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

package main

import (
	"net/http"

	iksv1 "github.com/IBM/staticroute-operator/pkg/apis/iks/v1"
	"github.com/IBM/staticroute-operator/pkg/routemanager"
	openapi_v2 "github.com/googleapis/gnostic/OpenAPIv2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func getEnvMock(metricsNS, nodeHostName, targetTable, protectedSubnets string) func(string) string {
	return func(key string) string {
		switch key {
		case "METRICS_NS":
			return metricsNS
		case "NODE_HOSTNAME":
			return nodeHostName
		case "TARGET_TABLE":
			return targetTable
		case "PROTECTED_SUBNETS":
			return protectedSubnets
		default:
			return ""
		}
	}
}

func osEnvMock(envvars []string) func() []string {
	return func() []string {
		return envvars
	}
}

func newFakeClient() client.Client {
	s := runtime.NewScheme()
	route := &iksv1.StaticRoute{}
	s.AddKnownTypes(iksv1.SchemeGroupVersion, route)
	node := &corev1.Node{
		TypeMeta: v1.TypeMeta{
			Kind: "node",
		},
		ObjectMeta: v1.ObjectMeta{
			Name: "hostname",
		},
	}
	s.AddKnownTypes(corev1.SchemeGroupVersion, node)
	return fake.NewFakeClientWithScheme(s, []runtime.Object{node, route}...)
}

type mockLogger struct{}

func (l mockLogger) Info(string, ...interface{}) {}

func (l mockLogger) Error(error, string, ...interface{}) {}

type mockCallbacks struct {
	getConfigCalled                bool
	newManagerCalled               bool
	addToSchemeCalled              bool
	newKubernetesConfigCalled      bool
	newRouterManagerCalled         bool
	addStaticRouteControllerCalled bool
	addNodeControllerCalled        bool
	routerGetCalled                bool
	setupSignalHandlerCalled       bool
}

type mockManager struct {
	client   client.Client
	startErr error
}

func (m mockManager) Add(manager.Runnable) error {
	return nil
}

func (m mockManager) SetFields(interface{}) error {
	return nil
}

func (m mockManager) Start(<-chan struct{}) error {
	return m.startErr
}

func (m mockManager) GetConfig() *rest.Config {
	return nil
}

func (m mockManager) GetScheme() *runtime.Scheme {
	return nil
}

func (m mockManager) GetClient() client.Client {
	if m.client != nil {
		return m.client
	}
	return newFakeClient()
}

func (m mockManager) GetFieldIndexer() client.FieldIndexer {
	return nil
}

func (m mockManager) GetCache() cache.Cache {
	return nil
}

func (m mockManager) GetEventRecorderFor(name string) record.EventRecorder {
	return nil
}

func (m mockManager) GetRESTMapper() meta.RESTMapper {
	return nil
}

func (m mockManager) GetAPIReader() client.Reader {
	return nil
}

func (m mockManager) GetWebhookServer() *webhook.Server {
	return nil
}

func (m mockManager) AddHealthzCheck(string, healthz.Checker) error {
	return nil
}

func (m mockManager) AddReadyzCheck(string, healthz.Checker) error {
	return nil
}

func (m mockManager) AddMetricsExtraHandler(string, http.Handler) error {
	return nil
}

type mockRouteManager struct{}

func (m mockRouteManager) IsRegistered(string) bool {
	return false
}

func (m mockRouteManager) RegisterRoute(string, routemanager.Route) error {
	return nil
}

func (m mockRouteManager) DeRegisterRoute(string) error {
	return nil
}

func (m mockRouteManager) RegisterWatcher(routemanager.RouteWatcher) {

}

func (m mockRouteManager) DeRegisterWatcher(routemanager.RouteWatcher) {

}

func (m mockRouteManager) Run(stopChan chan struct{}) error {
	<-stopChan
	return nil
}

type mockDiscoverable struct {
	apiResourceList                   *metav1.APIResourceList
	serverResourcesForGroupVersionErr error
}

func (m mockDiscoverable) Discovery() discovery.DiscoveryInterface {
	resources := m.apiResourceList
	if resources == nil {
		resources = &metav1.APIResourceList{
			APIResources: []metav1.APIResource{metav1.APIResource{
				Kind: "StaticRoute",
			}},
		}
	}
	return mockDiscovery{
		apiResourceList:                   resources,
		serverResourcesForGroupVersionErr: m.serverResourcesForGroupVersionErr,
	}
}

type mockDiscovery struct {
	apiResourceList                   *metav1.APIResourceList
	serverResourcesForGroupVersionErr error
}

func (m mockDiscovery) OpenAPISchema() (*openapi_v2.Document, error) {
	return nil, nil
}

func (m mockDiscovery) RESTClient() restclient.Interface {
	return nil
}

func (m mockDiscovery) ServerGroups() (*metav1.APIGroupList, error) {
	return nil, nil
}

func (m mockDiscovery) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	return m.apiResourceList, m.serverResourcesForGroupVersionErr
}

func (m mockDiscovery) ServerResources() ([]*metav1.APIResourceList, error) {
	return nil, nil
}

func (m mockDiscovery) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return nil, nil, nil
}

func (m mockDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	return nil, nil
}

func (m mockDiscovery) ServerPreferredNamespacedResources() ([]*metav1.APIResourceList, error) {
	return nil, nil
}

func (m mockDiscovery) ServerVersion() (*version.Info, error) {
	return nil, nil
}
