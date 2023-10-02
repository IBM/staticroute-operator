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

package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)

	kRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	"github.com/IBM/staticroute-operator/controllers/node"
	"github.com/IBM/staticroute-operator/controllers/staticroute"
	"github.com/IBM/staticroute-operator/pkg/routemanager"
	"github.com/IBM/staticroute-operator/pkg/types"
	"github.com/IBM/staticroute-operator/version"
	"github.com/vishvananda/netlink"

	staticroutev1 "github.com/IBM/staticroute-operator/api/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Change below variables to serve metrics on different host or port.
var (
	defaultRouteTable = 254
	defaultFallbackIP = net.IP{10, 0, 0, 1}
)
var log = logf.Log.WithName("cmd")

var scheme = kRuntime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(staticroutev1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func printVersion() {
	log.Info(fmt.Sprintf("Operator Version: %s", version.Version))
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Error(fmt.Errorf("%v", r), "")
			os.Exit(1)
		}
	}()

	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	logger := zap.New(zap.UseFlagOptions(&opts))

	// Use a zap logr.Logger implementation. If none of the zap
	// flags are configured (or if the zap flag set is not being
	// used), this defaults to a production zap params.logger.
	//
	// The logger instantiated here can be changed to any logger
	// implementing the logr.Logger interface. This logger will
	// be propagated through the whole operator, generating
	// uniform and structured logs.
	logf.SetLogger(logger)

	printVersion()

	mainImpl(mainImplParams{
		logger:      log,
		getEnv:      os.Getenv,
		osEnv:       os.Environ,
		getConfig:   config.GetConfig,
		newManager:  manager.New,
		addToScheme: staticroutev1.AddToScheme,
		newKubernetesConfig: func(config *rest.Config) (discoverable, error) {
			clientSet, err := kubernetes.NewForConfig(config)
			return clientSet, err
		},
		newRouterManager:         routemanager.New,
		addStaticRouteController: staticroute.Add,
		addNodeController:        node.Add,
		getGw: func(ip net.IP) (net.IP, error) {
			route, err := netlink.RouteGet(ip)
			if err != nil {
				return nil, err
			}
			return route[0].Gw, nil
		},
		setupSignalHandler: func() context.Context {
			return signals.SetupSignalHandler()
		},
	})
}

type mainImplParams struct {
	logger                   types.Logger
	getEnv                   func(string) string
	osEnv                    func() []string
	getConfig                func() (*rest.Config, error)
	newManager               func(*rest.Config, manager.Options) (manager.Manager, error)
	addToScheme              func(s *kRuntime.Scheme) error
	newKubernetesConfig      func(*rest.Config) (discoverable, error)
	newRouterManager         func() routemanager.RouteManager
	addStaticRouteController func(manager.Manager, staticroute.ManagerOptions) error
	addNodeController        func(manager.Manager) error
	getGw                    func(net.IP) (net.IP, error)
	setupSignalHandler       func() context.Context
}

type discoverable interface {
	Discovery() discovery.DiscoveryInterface
}

func mainImpl(params mainImplParams) {
	// Get a config to talk to the apiserver
	cfg, err := params.getConfig()
	if err != nil {
		panic(err)
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := params.newManager(cfg, manager.Options{
		MapperProvider: apiutil.NewDiscoveryRESTMapper,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	if err != nil {
		panic(err)
	}

	params.logger.Info("Registering Components.")

	// Setup Scheme for all resources
	if err := params.addToScheme(mgr.GetScheme()); err != nil {
		panic(err)
	}

	hostname := params.getEnv("NODE_HOSTNAME")
	if hostname == "" {
		panic("Missing environment variable: NODE_HOSTNAME")
	}

	params.logger.Info(fmt.Sprintf("Node Hostname: %s", hostname))
	params.logger.Info("Registering Components.")

	clientset, err := params.newKubernetesConfig(cfg)
	if err != nil {
		panic(err)
	}

	resources, err := clientset.Discovery().ServerResourcesForGroupVersion("static-route.ibm.com/v1")
	if err != nil {
		panic(err)
	}

	table := defaultRouteTable
	targetTableEnv := params.getEnv("TARGET_TABLE")
	if len(targetTableEnv) != 0 {
		table = parseTargetTable(targetTableEnv)
	}
	params.logger.Info("Table selected", "value", table)

	fallbackIP := defaultFallbackIP
	fallbackIPEnv := params.getEnv("FALLBACK_IP_FOR_GW_SELECTION")
	if len(fallbackIPEnv) != 0 {
		fallbackIP = net.ParseIP(fallbackIPEnv)
		if fallbackIP == nil || strings.Contains(fallbackIPEnv, ":") {
			panic("Environment variable parse error: FALLBACK_IP_FOR_GW_SELECTION.")
		}
	}
	params.logger.Info("Fallback IP for gateway selection:", "value", fallbackIP)

	protectedSubnets := collectProtectedSubnets(params.osEnv())

	crdFound := false
	for _, resource := range resources.APIResources {
		if resource.Kind != "StaticRoute" {
			continue
		}

		// Create RouteManager
		routeManager := params.newRouterManager()
		stopChan := make(chan struct{})
		go func() {
			panic(routeManager.Run(stopChan))
		}()

		// Start static route controller
		if err := params.addStaticRouteController(mgr, staticroute.ManagerOptions{
			Hostname:                 hostname,
			Table:                    table,
			ProtectedSubnets:         protectedSubnets,
			FallbackIPForGwSelection: fallbackIP,
			RouteManager:             routeManager,
			GetGw:                    params.getGw,
		}); err != nil {
			panic(err)
		}
		crdFound = true
		break
	}
	if !crdFound {
		params.logger.Info("CRD not found: staticroutes.static-route.ibm.com")
		panic(err)
	}

	// Start node controller
	if err := params.addNodeController(mgr); err != nil {
		panic(err)
	}

	params.logger.Info("Starting the Cmd.")
	// Start the Cmd

	if err := mgr.Start(params.setupSignalHandler()); err != nil {
		params.logger.Error(err, "Manager exited non-zero")
		panic(err)
	}
}

func parseTargetTable(targetTableEnv string) int {
	if customTable, err := strconv.Atoi(targetTableEnv); err != nil {
		panic(fmt.Sprintf("Unable to parse custom table 'TARGET_TABLE=%s' %s", targetTableEnv, err.Error()))
	} else if customTable < 0 || customTable > 254 {
		panic(fmt.Sprintf("Target table must be between 0 and 254 'TARGET_TABLE=%s'", targetTableEnv))
	} else {
		return customTable
	}
}

func collectProtectedSubnets(envVars []string) []*net.IPNet {
	protectedSubnets := []*net.IPNet{}
	for _, e := range envVars {
		if v := strings.SplitN(e, "=", 2); strings.Contains(v[0], "PROTECTED_SUBNET_") {
			for _, subnet := range strings.Split(v[1], ",") {
				_, subnetNet, err := net.ParseCIDR(strings.Trim(subnet, " "))
				if err != nil {
					panic(err)
				}
				protectedSubnets = append(protectedSubnets, subnetNet)
			}
		}
	}
	return protectedSubnets
}
