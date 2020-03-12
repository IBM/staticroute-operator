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
	"fmt"
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
	params, callbacks := getContextForHappyFlow()

	mainImpl(*params)

	expected := mockCallbacks{
		getConfigCalled:                true,
		newManagerCalled:               true,
		addToSchemeCalled:              true,
		serveCRMetricsCalled:           true,
		createMetricsServiceCalled:     true,
		createServiceMonitorsCalled:    true,
		newKubernetesConfigCalled:      true,
		newRouterManagerCalled:         true,
		addStaticRouteControllerCalled: true,
		addNodeControllerCalled:        true,
		routerGetCalled:                true,
		setupSignalHandlerCalled:       true,
	}

	if expected != *callbacks {
		t.Errorf("Not the right dependencies were called: expected: %v actial: %v", expected, callbacks)
	}
}

func TestMainImplServeCRMetricsFails(t *testing.T) {
	defer catchError(t)()
	params, _ := getContextForHappyFlow()
	params.serveCRMetrics = func(*rest.Config) error {
		return errors.New("fatal-error")
	}

	mainImpl(*params)
}

func TestMainImplCreateMetricsServiceFails(t *testing.T) {
	defer catchError(t)()
	params, _ := getContextForHappyFlow()
	params.createMetricsService = func(context.Context, *rest.Config, []v1.ServicePort) (*v1.Service, error) {
		return nil, errors.New("fatal-error")
	}

	mainImpl(*params)
}

func TestMainImplCreateServiceMonitorsFails(t *testing.T) {
	defer catchError(t)()
	params, _ := getContextForHappyFlow()
	params.createServiceMonitors = func(*rest.Config, string, []*v1.Service, ...metrics.ServiceMonitorUpdater) ([]*monitoringv1.ServiceMonitor, error) {
		return nil, errors.New("fatal-error")
	}

	mainImpl(*params)
}

func TestMainImplCreateServiceMonitorsFailsNotPreset(t *testing.T) {
	defer catchError(t)()
	params, _ := getContextForHappyFlow()
	params.createServiceMonitors = func(*rest.Config, string, []*v1.Service, ...metrics.ServiceMonitorUpdater) ([]*monitoringv1.ServiceMonitor, error) {
		return nil, metrics.ErrServiceMonitorNotPresent
	}

	mainImpl(*params)
}
func TestMainImplTargetTableOk(t *testing.T) {
	var actualTable int
	defer catchError(t)()
	params, _ := getContextForHappyFlow()
	params.getEnv = getEnvMock("", "hostname", "42", "")
	params.addStaticRouteController = func(mgr manager.Manager, options staticroute.ManagerOptions) error {
		actualTable = options.Table
		return nil
	}

	mainImpl(*params)

	if actualTable != 42 {
		t.Errorf("Target table not match 42 != %d", actualTable)
	}
}

func TestMainImplProtectedSubnetsOk(t *testing.T) {
	var actualSubnets []*net.IPNet
	defer catchError(t)()
	params, _ := getContextForHappyFlow()
	params.getEnv = getEnvMock("", "hostname", "", " 10.0.0.0/8 , 192.168.0.0/24 ")
	params.addStaticRouteController = func(mgr manager.Manager, options staticroute.ManagerOptions) error {
		actualSubnets = options.ProtectedSubnets
		return nil
	}

	mainImpl(*params)

	expectedSubnets := []*net.IPNet{
		&net.IPNet{IP: net.IP{10, 0, 0, 0}, Mask: net.IPv4Mask(0xff, 0, 0, 0)},
		&net.IPNet{IP: net.IP{192, 168, 0, 0}, Mask: net.IPv4Mask(0xff, 0xff, 0xff, 0)},
	}
	if fmt.Sprintf("%v", expectedSubnets) != fmt.Sprintf("%v", actualSubnets) {
		t.Errorf("Protected subnets are not match %v != %v", expectedSubnets, actualSubnets)
	}
}

func TestMainImplGetConfigFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params, _ := getContextForHappyFlow()
	params.getConfig = func() (*rest.Config, error) {
		return nil, err
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplNewManagerFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params, _ := getContextForHappyFlow()
	params.newManager = func(*rest.Config, manager.Options) (manager.Manager, error) {
		return mockManager{}, err
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplAddToSchemeFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params, _ := getContextForHappyFlow()
	params.addToScheme = func(s *runtime.Scheme) error {
		return err
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplHostnameMissing(t *testing.T) {
	defer validateRecovery(t, "Missing environment variable: NODE_HOSTNAME")()
	params, _ := getContextForHappyFlow()
	params.getEnv = getEnvMock("", "", "", "")

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
	params, _ := getContextForHappyFlow()
	params.newManager = func(*rest.Config, manager.Options) (manager.Manager, error) {
		return mockManager{client: fake.NewFakeClient()}, nil
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplTargetTableInvalid(t *testing.T) {
	defer validateRecovery(t, "Unable to parse custom table 'TARGET_TABLE=invalid-table' strconv.Atoi: parsing \"invalid-table\": invalid syntax")()
	params, _ := getContextForHappyFlow()
	params.getEnv = getEnvMock("", "hostname", "invalid-table", "")

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplTargetTableFewer(t *testing.T) {
	defer validateRecovery(t, "Target table must be between 0 and 254 'TARGET_TABLE=-1'")()
	params, _ := getContextForHappyFlow()
	params.getEnv = getEnvMock("", "hostname", "-1", "")

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplTargetTableGreater(t *testing.T) {
	defer validateRecovery(t, "Target table must be between 0 and 254 'TARGET_TABLE=255'")()
	params, _ := getContextForHappyFlow()
	params.getEnv = getEnvMock("", "hostname", "255", "")

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplProtectedSubnetsInvalid(t *testing.T) {
	defer validateRecovery(t, "invalid CIDR address: 10.0.0.0/8 ; 192.168.0.0/24")()
	params, _ := getContextForHappyFlow()
	params.getEnv = getEnvMock("", "hostname", "", " 10.0.0.0/8 ; 192.168.0.0/24 ")

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplNewKubernetesConfigFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params, _ := getContextForHappyFlow()
	params.newKubernetesConfig = func(c *rest.Config) (discoverable, error) {
		return mockDiscoverable{}, err
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplServerResourcesForGroupVersionFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params, _ := getContextForHappyFlow()
	params.newKubernetesConfig = func(c *rest.Config) (discoverable, error) {
		return mockDiscoverable{serverResourcesForGroupVersionErr: err}, nil
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplCrdNorFound(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params, _ := getContextForHappyFlow()
	params.newKubernetesConfig = func(c *rest.Config) (discoverable, error) {
		return mockDiscoverable{apiResourceList: &metav1.APIResourceList{}}, nil
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplAddStaticRouteControllerFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params, _ := getContextForHappyFlow()
	params.addStaticRouteController = func(manager.Manager, staticroute.ManagerOptions) error {
		return err
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplAddNodeControllerFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params, _ := getContextForHappyFlow()
	params.addNodeController = func(manager.Manager) error {
		return err
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func TestMainImplManagerStartFails(t *testing.T) {
	err := errors.New("fatal-error")
	defer validateRecovery(t, err)()
	params, _ := getContextForHappyFlow()
	params.newManager = func(*rest.Config, manager.Options) (manager.Manager, error) {
		return mockManager{startErr: err}, nil
	}

	mainImpl(*params)

	t.Error("Error didn't appear")
}

func getContextForHappyFlow() (*mainImplParams, *mockCallbacks) {
	callbacks := mockCallbacks{}
	return &mainImplParams{
		logger: mockLogger{},
		getEnv: getEnvMock("", "hostname", "", ""),
		getConfig: func() (*rest.Config, error) {
			callbacks.getConfigCalled = true
			return nil, nil
		},
		newManager: func(*rest.Config, manager.Options) (manager.Manager, error) {
			callbacks.newManagerCalled = true
			return mockManager{}, nil
		},
		addToScheme: func(s *runtime.Scheme) error {
			callbacks.addToSchemeCalled = true
			return nil
		},
		serveCRMetrics: func(*rest.Config) error {
			callbacks.serveCRMetricsCalled = true
			return nil
		},
		createMetricsService: func(context.Context, *rest.Config, []v1.ServicePort) (*v1.Service, error) {
			callbacks.createMetricsServiceCalled = true
			return nil, nil
		},
		createServiceMonitors: func(*rest.Config, string, []*v1.Service, ...metrics.ServiceMonitorUpdater) ([]*monitoringv1.ServiceMonitor, error) {
			callbacks.createServiceMonitorsCalled = true
			return nil, nil
		},
		newKubernetesConfig: func(c *rest.Config) (discoverable, error) {
			callbacks.newKubernetesConfigCalled = true
			return mockDiscoverable{}, nil
		},
		newRouterManager: func() routemanager.RouteManager {
			callbacks.newRouterManagerCalled = true
			return mockRouteManager{}
		},
		addStaticRouteController: func(mgr manager.Manager, options staticroute.ManagerOptions) error {
			//nolint:errcheck
			options.RouteGet()
			callbacks.addStaticRouteControllerCalled = true
			return nil
		},
		addNodeController: func(manager.Manager) error {
			callbacks.addNodeControllerCalled = true
			return nil
		},
		routerGet: func() (net.IP, error) {
			callbacks.routerGetCalled = true
			return net.IP{10, 0, 0, 1}, nil
		},
		setupSignalHandler: func() (stopCh <-chan struct{}) {
			callbacks.setupSignalHandlerCalled = true
			return make(chan struct{})
		},
	}, &callbacks
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
			if fmt.Sprintf("%v", r) != fmt.Sprintf("%v", expected) {
				t.Errorf("Error not match '%v' != '%v'", r, expected)
			}
		}
	}
}
