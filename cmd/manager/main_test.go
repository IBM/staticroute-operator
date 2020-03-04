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
	"context"
	"net"
	"regexp"
	"runtime/debug"
	"testing"

	"github.com/IBM/staticroute-operator/pkg/controller/staticroute"
	"github.com/IBM/staticroute-operator/pkg/routemanager"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/operator-framework/operator-sdk/pkg/metrics"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func TestMetricHost(t *testing.T) {
	r, _ := regexp.Compile(`^([0-9]{1,3}\.){3}[0-9]{1,3}$`)
	if !r.MatchString(metricsHost) {
		t.Errorf("metricsHost is not a valid IP address: %s", metricsHost)
	}
}

func TestMetricsPort(t *testing.T) {
	if metricsPort < 0 || metricsPort > 65535 {
		t.Errorf("metricsPort is not between 0 and 65535: %s", metricsHost)
	}
}

func TestOperatorMetricsPort(t *testing.T) {
	if operatorMetricsPort < 0 || operatorMetricsPort > 65535 {
		t.Errorf("operatorMetricsPort is not between 0 and 65535: %s", metricsHost)
	}
}

func TestDefaultRouteTable(t *testing.T) {
	if defaultRouteTable < 0 || defaultRouteTable > 254 {
		t.Errorf("defaultRouteTable is not between 0 and 254: %s", metricsHost)
	}
}

func TestMainImpl(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Fatal error: %v", r)
			debug.PrintStack()
		}
	}()

	mainImpl(*getContextForHappyFlow())
}

func getContextForHappyFlow() *mainImplParams {
	return &mainImplParams{
		logger: mockLogger{},
		getEnv: getEnvMock("", "hostname", ""),
		getConfig: func() (*rest.Config, error) {
			return nil, nil
		},
		newManager: func(*rest.Config, manager.Options) (manager.Manager, error) {
			return mockManager{}, nil
		},
		addToScheme: func(s *runtime.Scheme) error {
			return nil
		},
		serveCRMetrics: func(*rest.Config) error {
			return nil
		},
		createMetricsService: func(context.Context, *rest.Config, []v1.ServicePort) (*v1.Service, error) {
			return nil, nil
		},
		createServiceMonitors: func(*rest.Config, string, []*v1.Service, ...metrics.ServiceMonitorUpdater) ([]*monitoringv1.ServiceMonitor, error) {
			return nil, nil
		},
		newKubernetesConfig: func(c *rest.Config) (discoverable, error) {
			return mockDiscoverable{}, nil
		},
		newRouterManager: func() routemanager.RouteManager {
			return mockRouteManager{}
		},
		addStaticRouteController: func(manager.Manager, staticroute.ManagerOptions) error {
			return nil
		},
		addNodeController: func(manager.Manager) error {
			return nil
		},
		routerGet: func() (net.IP, error) {
			return net.IP{10, 0, 0, 1}, nil
		},
		setupSignalHandler: func() (stopCh <-chan struct{}) {
			return make(chan struct{})
		},
	}
}
