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

package staticroute

import (
	"errors"
	"net"
	"testing"

	staticroutev1 "github.com/IBM/staticroute-operator/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsProtected(t *testing.T) {
	var testData = []struct {
		protecteds []*net.IPNet
		route      *staticroutev1.StaticRoute
		result     bool
	}{
		{
			nil,
			&staticroutev1.StaticRoute{
				Spec: staticroutev1.StaticRouteSpec{
					Subnet: "invalid-subnet",
				},
			},
			false,
		},
		{
			nil,
			&staticroutev1.StaticRoute{
				Spec: staticroutev1.StaticRouteSpec{
					Subnet: "10.0.0.1/16",
				},
			},
			false,
		},
		{
			[]*net.IPNet{&net.IPNet{IP: net.IP{192, 168, 0, 0}, Mask: net.IPv4Mask(0xff, 0xff, 0xff, 0)}},
			&staticroutev1.StaticRoute{
				Spec: staticroutev1.StaticRouteSpec{
					Subnet: "192.168.0.1/24",
				},
			},
			true,
		},
	}

	for i, td := range testData {
		rw := routeWrapper{instance: td.route}

		res := rw.isProtected(td.protecteds)

		if res != td.result {
			t.Errorf("Result must be %t, it is %t at %d", td.result, res, i)
		}
	}
}

func TestIsChanged(t *testing.T) {
	var testData = []struct {
		hostname  string
		gateway   string
		selectors []metav1.LabelSelectorRequirement
		route     *staticroutev1.StaticRoute
		result    bool
	}{
		{
			"hostname",
			"gateway",
			nil,
			&staticroutev1.StaticRoute{
				Spec: staticroutev1.StaticRouteSpec{
					Subnet: "subnet",
				},
			},
			false,
		},
		{
			"hostname",
			"gateway",
			nil,
			&staticroutev1.StaticRoute{
				Spec: staticroutev1.StaticRouteSpec{
					Subnet: "subnet",
				},
				Status: staticroutev1.StaticRouteStatus{
					NodeStatus: []staticroutev1.StaticRouteNodeStatus{
						staticroutev1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: staticroutev1.StaticRouteSpec{
								Subnet:  "subnet",
								Gateway: "gateway",
							},
						},
					},
				},
			},
			false,
		},
		{
			"hostname",
			"gateway2",
			nil,
			&staticroutev1.StaticRoute{
				Spec: staticroutev1.StaticRouteSpec{
					Subnet: "subnet",
				},
				Status: staticroutev1.StaticRouteStatus{
					NodeStatus: []staticroutev1.StaticRouteNodeStatus{
						staticroutev1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: staticroutev1.StaticRouteSpec{
								Subnet:  "subnet",
								Gateway: "gateway",
							},
						},
					},
				},
			},
			true,
		},
		{
			"hostname",
			"gateway",
			nil,
			&staticroutev1.StaticRoute{
				Spec: staticroutev1.StaticRouteSpec{
					Subnet: "subnet2",
				},
				Status: staticroutev1.StaticRouteStatus{
					NodeStatus: []staticroutev1.StaticRouteNodeStatus{
						staticroutev1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: staticroutev1.StaticRouteSpec{
								Subnet:  "subnet",
								Gateway: "gateway",
							},
						},
					},
				},
			},
			true,
		},
		{
			"hostname",
			"gateway2",
			nil,
			&staticroutev1.StaticRoute{
				Spec: staticroutev1.StaticRouteSpec{
					Subnet: "subnet2",
				},
				Status: staticroutev1.StaticRouteStatus{
					NodeStatus: []staticroutev1.StaticRouteNodeStatus{
						staticroutev1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: staticroutev1.StaticRouteSpec{
								Subnet:  "subnet",
								Gateway: "gateway",
							},
						},
					},
				},
			},
			true,
		},
		{
			"hostname",
			"gateway2",
			nil,
			&staticroutev1.StaticRoute{
				Spec: staticroutev1.StaticRouteSpec{
					Subnet: "subnet2",
				},
				Status: staticroutev1.StaticRouteStatus{
					NodeStatus: []staticroutev1.StaticRouteNodeStatus{
						staticroutev1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: staticroutev1.StaticRouteSpec{
								Subnet:  "subnet",
								Gateway: "gateway",
							},
						},
						staticroutev1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: staticroutev1.StaticRouteSpec{
								Subnet:  "subnet2",
								Gateway: "gateway2",
							},
						},
					},
				},
			},
			true,
		},
		{
			"hostname",
			"gateway",
			nil,
			&staticroutev1.StaticRoute{
				Spec: staticroutev1.StaticRouteSpec{
					Subnet: "subnet",
				},
				Status: staticroutev1.StaticRouteStatus{
					NodeStatus: []staticroutev1.StaticRouteNodeStatus{
						staticroutev1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: staticroutev1.StaticRouteSpec{
								Subnet:  "subnet",
								Gateway: "gateway",
							},
						},
						staticroutev1.StaticRouteNodeStatus{
							Hostname: "hostname2",
							State: staticroutev1.StaticRouteSpec{
								Subnet:  "subnet2",
								Gateway: "gateway2",
							},
						},
					},
				},
			},
			false,
		},
		{
			"hostname",
			"gateway",
			[]metav1.LabelSelectorRequirement{metav1.LabelSelectorRequirement{
				Key:      HostNameLabel,
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"hostname"},
			}},
			&staticroutev1.StaticRoute{
				Spec: staticroutev1.StaticRouteSpec{
					Subnet: "subnet",
				},
				Status: staticroutev1.StaticRouteStatus{
					NodeStatus: []staticroutev1.StaticRouteNodeStatus{
						staticroutev1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: staticroutev1.StaticRouteSpec{
								Subnet:  "subnet",
								Gateway: "gateway",
							},
						},
					},
				},
			},
			true,
		},
	}

	for i, td := range testData {
		rw := routeWrapper{instance: td.route}

		res := rw.isChanged(td.hostname, td.gateway, td.selectors)

		if res != td.result {
			t.Errorf("Result must be %t, it is %t at %d", td.result, res, i)
		}
	}
}

func TestRouteWrapperSetFinalizer(t *testing.T) {
	route := newStaticRouteWithValues(true, false)
	rw := routeWrapper{instance: route}

	done := rw.setFinalizer()

	if !done || len(route.GetFinalizers()) == 0 {
		t.Error("Finalizer must be added")
	} else if route.GetFinalizers()[0] != "finalizer.static-route.ibm.com" {
		t.Errorf("`finalizer.static-route.ibm.com` not setted as finalizer: %v", route.GetFinalizers())
	}
}

func TestRouteWrapperSetFinalizerNotEmpty(t *testing.T) {
	route := newStaticRouteWithValues(true, false)
	route.SetFinalizers([]string{"finalizer"})
	rw := routeWrapper{instance: route}

	done := rw.setFinalizer()

	if done {
		t.Error("Finalizer must be not added")
	}
}

func TestRouteWrapperGetGateway(t *testing.T) {
	route := newStaticRouteWithValues(true, false)
	rw := routeWrapper{instance: route}

	gw := rw.getGateway()

	if gw.String() != "10.0.0.1" {
		t.Errorf("Gateway must be `10.0.0.1`: %s", gw.String())
	}
}

func TestRouteWrapperGetGatewayMissing(t *testing.T) {
	route := newStaticRouteWithValues(false, false)

	rw := routeWrapper{instance: route}

	gw := rw.getGateway()

	if gw != nil {
		t.Errorf("Gateway must be nil: %s", gw.String())
	}
}

func TestRouteWrapperGetGatewayInvalid(t *testing.T) {
	route := newStaticRouteWithValues(false, false)
	route.Spec.Gateway = "invalid-gateway"
	rw := routeWrapper{instance: route}

	gw := rw.getGateway()

	if gw != nil {
		t.Errorf("Gateway must be nil: %s", gw.String())
	}
}

func TestRouteWrapperAddToStatus(t *testing.T) {
	route := newStaticRouteWithValues(false, false)
	rw := routeWrapper{instance: route}

	added := rw.addToStatus("hostname", net.IP{10, 0, 0, 1}, errors.New("failure"))

	if !added {
		t.Error("Status must be added")
	} else if len(route.Status.NodeStatus) != 1 {
		t.Errorf("Status not added: %v", route.Status.NodeStatus)
	} else if route.Status.NodeStatus[0].Hostname != "hostname" {
		t.Errorf("First status must be `hostname`: %s", route.Status.NodeStatus[0].Hostname)
	} else if route.Status.NodeStatus[0].State.Gateway != "10.0.0.1" {
		t.Errorf("First status gateway must be `10.0.0.1`: %s", route.Status.NodeStatus[0].State.Gateway)
	} else if route.Status.NodeStatus[0].Error != "failure" {
		t.Errorf("Error field in status shall be filled with the string `failure`: %s", route.Status.NodeStatus[0].Error)
	}
}

func TestRouteWrapperAddToStatusNotAdded(t *testing.T) {
	route := newStaticRouteWithValues(false, false)
	route.Status = staticroutev1.StaticRouteStatus{
		NodeStatus: []staticroutev1.StaticRouteNodeStatus{
			staticroutev1.StaticRouteNodeStatus{
				Hostname: "hostname",
			},
		},
	}
	rw := routeWrapper{instance: route}

	added := rw.addToStatus("hostname", net.IP{10, 0, 0, 1}, nil)

	if added {
		t.Error("Status must be not added")
	}
}

func TestRouteWrapperRemoveFromStatusNotRemoved(t *testing.T) {
	route := newStaticRouteWithValues(false, false)
	route.Status = staticroutev1.StaticRouteStatus{
		NodeStatus: []staticroutev1.StaticRouteNodeStatus{
			staticroutev1.StaticRouteNodeStatus{
				Hostname: "hostname",
			},
		},
	}
	rw := routeWrapper{instance: route}

	removed := rw.removeFromStatus("hostname2")

	if removed {
		t.Error("Status must be not removed")
	}
}

func TestRouteWrapperRemoveFromStatusRemoved(t *testing.T) {
	route := newStaticRouteWithValues(false, false)
	route.Status = staticroutev1.StaticRouteStatus{
		NodeStatus: []staticroutev1.StaticRouteNodeStatus{
			staticroutev1.StaticRouteNodeStatus{
				Hostname: "hostname",
			},
		},
	}
	rw := routeWrapper{instance: route}

	removed := rw.removeFromStatus("hostname")

	if !removed {
		t.Error("Status must be removed")
	} else if len(route.Status.NodeStatus) != 0 {
		t.Errorf("Statuses must be empty: %v", route.Status.NodeStatus)
	}
}
