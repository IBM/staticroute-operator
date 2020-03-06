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
	"errors"
	"net"
	"regexp"
	"runtime/debug"
	"testing"

	"github.com/IBM/staticroute-operator/pkg/controller/staticroute"
	"github.com/IBM/staticroute-operator/pkg/routemanager"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/operator-framework/operator-sdk/pkg/metrics"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
	defer catchError(t)()

	mainImpl(*getContextForHappyFlow())
}

func TestMainImplServeCRMetricsFails(t *testing.T) {
	defer catchError(t)()
	params := getContextForHappyFlow()
	params.serveCRMetrics = func(*rest.Config) error {
		return errors.New("fatal-error")
	}

	mainImpl(*params)
}

func TestMainImplCreateMetricsServiceFails(t *testing.T) {
	defer catchError(t)()
	params := getContextForHappyFlow()
	params.createMetricsService = func(context.Context, *rest.Config, []v1.ServicePort) (*v1.Service, error) {
		return nil, errors.New("fatal-error")
	}

	mainImpl(*params)
}

func TestMainImplCreateServiceMonitorsFails(t *testing.T) {
	defer catchError(t)()
	params := getContextForHappyFlow()
	params.createServiceMonitors = func(*rest.Config, string, []*v1.Service, ...metrics.ServiceMonitorUpdater) ([]*monitoringv1.ServiceMonitor, error) {
		return nil, errors.New("fatal-error")
	}

	mainImpl(*params)
}

func TestMainImplCreateServiceMonitorsFailsNotPreset(t *testing.T) {
	defer catchError(t)()
	params := getContextForHappyFlow()
	params.createServiceMonitors = func(*rest.Config, string, []*v1.Service, ...metrics.ServiceMonitorUpdater) ([]*monitoringv1.ServiceMonitor, error) {
		return nil, metrics.ErrServiceMonitorNotPresent
	}

	mainImpl(*params)
}
func TestMainImplTargetTableMore(t *testing.T) {
	defer catchError(t)()
	params := getContextForHappyFlow()
	params.getEnv = getEnvMock("", "hostname", "42")

	mainImpl(*params)
}

func TestMainImplGetConfigFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params := getContextForHappyFlow()
	params.getConfig = func() (*rest.Config, error) {
		return nil, err
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplNewManagerFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params := getContextForHappyFlow()
	params.newManager = func(*rest.Config, manager.Options) (manager.Manager, error) {
		return mockManager{}, err
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplAddToSchemeFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params := getContextForHappyFlow()
	params.addToScheme = func(s *runtime.Scheme) error {
		return err
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplHostnameMissing(t *testing.T) {
	defer validateRecovery(t, "Missing environment variable: NODE_HOSTNAME")()
	params := getContextForHappyFlow()
	params.getEnv = getEnvMock("", "", "")

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplNodeGetFails(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			switch rt := r.(type) {
			case error:
				if rt.Error() != "nodes \"hostname\" not found" {
					t.Errorf("Error not match, current %v", rt)
				}
			default:
				t.Errorf("Wrong error did appear: %v", r)
			}
		}
	}()
	params := getContextForHappyFlow()
	params.newManager = func(*rest.Config, manager.Options) (manager.Manager, error) {
		return mockManager{client: fake.NewFakeClient()}, nil
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplTargetTableInvalid(t *testing.T) {
	defer validateRecovery(t, "Unable to parse custom table 'TARGET_TABLE=invalid-table' strconv.Atoi: parsing \"invalid-table\": invalid syntax")()
	params := getContextForHappyFlow()
	params.getEnv = getEnvMock("", "hostname", "invalid-table")

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplTargetTableLessThan(t *testing.T) {
	defer validateRecovery(t, "Target table must be between 0 and 254 'TARGET_TABLE=-1'")()
	params := getContextForHappyFlow()
	params.getEnv = getEnvMock("", "hostname", "-1")

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplTargetTableMoreThan(t *testing.T) {
	defer validateRecovery(t, "Target table must be between 0 and 254 'TARGET_TABLE=255'")()
	params := getContextForHappyFlow()
	params.getEnv = getEnvMock("", "hostname", "255")

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplNewKubernetesConfigFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params := getContextForHappyFlow()
	params.newKubernetesConfig = func(c *rest.Config) (discoverable, error) {
		return mockDiscoverable{}, err
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplServerResourcesForGroupVersionFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params := getContextForHappyFlow()
	params.newKubernetesConfig = func(c *rest.Config) (discoverable, error) {
		return mockDiscoverable{serverResourcesForGroupVersionErr: err}, nil
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplCrdNorFound(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params := getContextForHappyFlow()
	params.newKubernetesConfig = func(c *rest.Config) (discoverable, error) {
		return mockDiscoverable{apiResourceList: &metav1.APIResourceList{}}, nil
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplAddStaticRouteControllerFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params := getContextForHappyFlow()
	params.addStaticRouteController = func(manager.Manager, staticroute.ManagerOptions) error {
		return err
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplAddNodeControllerFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params := getContextForHappyFlow()
	params.addNodeController = func(manager.Manager) error {
		return err
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplManagerStartFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params := getContextForHappyFlow()
	params.newManager = func(*rest.Config, manager.Options) (manager.Manager, error) {
		return mockManager{startErr: err}, nil
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
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

func catchError(t *testing.T) func() {
	return func() {
		if r := recover(); r != nil {
			t.Errorf("Fatal error: %v", r)
			debug.PrintStack()
		}
	}
}

func validateRecovery(t *testing.T, expected interface{}) func() {
	return func() {
		if r := recover(); r != nil {
			if r != expected {
				t.Errorf("Error not match, current %v", r)
			}
		}
	}
}
