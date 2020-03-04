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
	"flag"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	"github.com/IBM/staticroute-operator/pkg/apis"
	"github.com/IBM/staticroute-operator/pkg/controller/node"
	"github.com/IBM/staticroute-operator/pkg/controller/staticroute"
	"github.com/IBM/staticroute-operator/pkg/routemanager"
	"github.com/IBM/staticroute-operator/version"
	"github.com/vishvananda/netlink"

	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	kubemetrics "github.com/operator-framework/operator-sdk/pkg/kube-metrics"
	"github.com/operator-framework/operator-sdk/pkg/log/zap"
	"github.com/operator-framework/operator-sdk/pkg/metrics"
	"github.com/operator-framework/operator-sdk/pkg/restmapper"
	sdkVersion "github.com/operator-framework/operator-sdk/version"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

// Change below variables to serve metrics on different host or port.
var (
	metricsHost                   = "0.0.0.0"
	metricsPort             int32 = 8383
	operatorMetricsPort     int32 = 8686
	defaultMetricsNamespace       = "default"
	defaultRouteTable             = 254
)
var log = logf.Log.WithName("cmd")

func printVersion() {
	log.Info(fmt.Sprintf("Operator Version: %s", version.Version))
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	log.Info(fmt.Sprintf("Version of operator-sdk: %v", sdkVersion.Version))
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Error(fmt.Errorf("%v", r), "")
			os.Exit(1)
		}
	}()

	// Add the zap logger flag set to the CLI. The flag set must
	// be added before calling pflag.Parse().
	pflag.CommandLine.AddFlagSet(zap.FlagSet())

	// Add flags registered by imported packages (e.g. glog and
	// controller-runtime)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	pflag.Parse()

	// Use a zap logr.Logger implementation. If none of the zap
	// flags are configured (or if the zap flag set is not being
	// used), this defaults to a production zap logger.
	//
	// The logger instantiated here can be changed to any logger
	// implementing the logr.Logger interface. This logger will
	// be propagated through the whole operator, generating
	// uniform and structured logs.
	logf.SetLogger(zap.Logger())

	printVersion()

	metricsNamespace, found := os.LookupEnv("METRICS_NS")
	if !found {
		metricsNamespace = defaultMetricsNamespace
		log.Info("METRICS_NS not defined.", "using", defaultMetricsNamespace)
	}

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		panic(err)
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, manager.Options{
		Namespace:          "",
		MapperProvider:     restmapper.NewDynamicRESTMapper,
		MetricsBindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
	})
	if err != nil {
		panic(err)
	}

	log.Info("Registering Components.")

	// Setup Scheme for all resources
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		panic(err)
	}

	if err = serveCRMetrics(cfg); err != nil {
		log.Info("Could not generate and serve custom resource metrics", "error", err.Error())
	}

	// Add to the below struct any other metrics ports you want to expose.
	servicePorts := []v1.ServicePort{
		{Port: metricsPort, Name: metrics.OperatorPortName, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: metricsPort}},
		{Port: operatorMetricsPort, Name: metrics.CRPortName, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: operatorMetricsPort}},
	}
	// Create Service object to expose the metrics port(s).
	service, err := metrics.CreateMetricsService(context.Background(), cfg, servicePorts)
	if err != nil {
		log.Info("Could not create metrics Service", "error", err.Error())
	}

	// CreateServiceMonitors will automatically create the prometheus-operator ServiceMonitor resources
	// necessary to configure Prometheus to scrape metrics from this operator.
	services := []*v1.Service{service}
	_, err = metrics.CreateServiceMonitors(cfg, metricsNamespace, services)
	if err != nil {
		log.Info("Could not create ServiceMonitor object", "error", err.Error())
		// If this operator is deployed to a cluster without the prometheus-operator running, it will return
		// ErrServiceMonitorNotPresent, which can be used to safely skip ServiceMonitor creation.
		if err == metrics.ErrServiceMonitorNotPresent {
			log.Info("Install prometheus-operator in your cluster to create ServiceMonitor objects", "error", err.Error())
		}
	}

	hostname := os.Getenv("NODE_HOSTNAME")
	if hostname == "" {
		panic("Missing environment variable: NODE_HOSTNAME")
	}

	// get the zone using the API, first get the matching Node
	c := mgr.GetClient()

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Kind:    "Node",
		Version: "v1",
	})
	err = c.Get(context.Background(), client.ObjectKey{
		Name: hostname,
	}, u)

	if err != nil {
		panic(err)
	}

	labels := u.GetLabels()
	zone := labels[staticroute.ZoneLabel]

	log.Info(fmt.Sprintf("Node Hostname: %s", hostname))
	log.Info(fmt.Sprintf("Node Zone: %s", zone))
	log.Info("Registering Components.")

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(err)
	}

	resources, err := clientset.Discovery().ServerResourcesForGroupVersion("iks.ibm.com/v1")
	if err != nil {
		panic(err)
	}

	table := defaultRouteTable
	targetTableEnv := os.Getenv("TARGET_TABLE")
	if len(targetTableEnv) != 0 {
		if customTable, err := strconv.Atoi(targetTableEnv); err != nil {
			panic(fmt.Sprintf("Unable to parse custom table 'TARGET_TABLE=%s' %s", targetTableEnv, err.Error()))
		} else if customTable < 0 || customTable > 254 {
			panic(fmt.Sprintf("Target table must be between 0 and 254 'TARGET_TABLE=%s'", targetTableEnv))
		} else {
			table = customTable
		}
	}
	log.Info("Table selected", "value", table)

	crdFound := false
	for _, resource := range resources.APIResources {
		if resource.Kind != "StaticRoute" {
			continue
		}

		// Create RouteManager
		routeManager := routemanager.New()
		stopChan := make(chan struct{})
		go func() {
			panic(routeManager.Run(stopChan))
		}()

		// Start static route controller
		if err := staticroute.Add(mgr, staticroute.ManagerOptions{
			Hostname:     hostname,
			Zone:         zone,
			Table:        table,
			RouteManager: routeManager,
			RouteGet: func() (net.IP, error) {
				route, err := netlink.RouteGet(net.IP{10, 0, 0, 1})
				if err != nil {
					return nil, err
				}
				return route[0].Gw, nil
			},
		}); err != nil {
			panic(err)
		}
		crdFound = true
		break
	}
	if !crdFound {
		log.Info("CRD not found: staticroutes.iks.ibm.com")
		panic(err)
	}

	// Start node controller
	if err := node.Add(mgr); err != nil {
		panic(err)
	}

	log.Info("Starting the Cmd.")
	// Start the Cmd
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "Manager exited non-zero")
		panic(err)
	}
}

// serveCRMetrics gets the Operator/CustomResource GVKs and generates metrics based on those types.
// It serves those metrics on "http://metricsHost:operatorMetricsPort".
func serveCRMetrics(cfg *rest.Config) error {
	// Below function returns filtered operator/CustomResource specific GVKs.
	// For more control override the below GVK list with your own custom logic.
	filteredGVK, err := k8sutil.GetGVKsFromAddToScheme(apis.AddToScheme)
	if err != nil {
		return err
	}
	// Get the namespace the operator is currently deployed in.
	/*
		As suggested in: https://github.com/operator-framework/operator-sdk/issues/1858#issuecomment-548323725
		operatorNs, err := k8sutil.GetOperatorNamespace()
		if err != nil {
			return err
		} */
	operatorNs := ""
	// To generate metrics in other namespaces, add the values below.
	ns := []string{operatorNs}
	// Generate and serve custom resource specific metrics.
	err = kubemetrics.GenerateAndServeCRMetrics(cfg, ns, filteredGVK, metricsHost, operatorMetricsPort)
	if err != nil {
		return err
	}
	return nil
}
