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
	"errors"
	"fmt"
	"net"
	"runtime/debug"
	"testing"

	"github.com/IBM/staticroute-operator/pkg/controller/staticroute"
	"github.com/IBM/staticroute-operator/pkg/routemanager"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func TestDefaultRouteTable(t *testing.T) {
	if defaultRouteTable < 0 || defaultRouteTable > 254 {
		t.Errorf("defaultRouteTable is not between 0 and 254: %d", defaultRouteTable)
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
	params.osEnv = osEnvMock([]string{
		"METRICS_NS=",
		"NODE_HOSTNAME=",
		"PROTECTED_SUBNET_CALICO=10.0.0.0/8,20.0.0.0/8",
		"PROTECTED_SUBNET_HOST=192.168.0.0/24",
		"TARGET_TABLE=",
	})
	params.addStaticRouteController = func(mgr manager.Manager, options staticroute.ManagerOptions) error {
		actualSubnets = options.ProtectedSubnets
		return nil
	}

	mainImpl(*params)

	expectedSubnets := []*net.IPNet{
		&net.IPNet{IP: net.IP{10, 0, 0, 0}, Mask: net.IPv4Mask(0xff, 0, 0, 0)},
		&net.IPNet{IP: net.IP{20, 0, 0, 0}, Mask: net.IPv4Mask(0xff, 0, 0, 0)},
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
	defer validateRecovery(t, "invalid CIDR address: 987.654.321.012")()
	params, _ := getContextForHappyFlow()
	params.osEnv = osEnvMock([]string{
		"PROTECTED_SUBNET_MYNET=987.654.321.012",
	})

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
		osEnv:  osEnvMock([]string{}),
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
			options.GetGw(net.IP{10, 0, 0, 1})
			callbacks.addStaticRouteControllerCalled = true
			return nil
		},
		addNodeController: func(manager.Manager) error {
			callbacks.addNodeControllerCalled = true
			return nil
		},
		getGw: func(ip net.IP) (net.IP, error) {
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
