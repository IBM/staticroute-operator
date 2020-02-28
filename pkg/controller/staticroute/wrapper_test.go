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
	"net"
	"testing"

	iksv1 "github.com/IBM/staticroute-operator/pkg/apis/iks/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsSameZone(t *testing.T) {
	var testData = []struct {
		zone   string
		route  *iksv1.StaticRoute
		result bool
	}{
		{
			"a",
			&iksv1.StaticRoute{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{ZoneLabel: "a"},
				},
			},
			true,
		},
		{
			"a",
			&iksv1.StaticRoute{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{ZoneLabel: ""},
				},
			},
			true,
		},
		{
			"",
			&iksv1.StaticRoute{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{ZoneLabel: ""},
				},
			},
			true,
		},
		{
			"a",
			&iksv1.StaticRoute{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{ZoneLabel: "b"},
				},
			},
			false,
		},
	}

	for i, td := range testData {
		rw := routeWrapper{instance: td.route}

		res := rw.isSameZone(td.zone, ZoneLabel)

		if res != td.result {
			t.Errorf("Result must be %t, it is %t ad %d", td.result, res, i)
		}
	}
}

func TestIsChanged(t *testing.T) {
	var testData = []struct {
		hostname string
		gateway  string
		route    *iksv1.StaticRoute
		result   bool
	}{
		{
			"hostname",
			"gateway",
			&iksv1.StaticRoute{
				Spec: iksv1.StaticRouteSpec{
					Subnet: "subnet",
				},
			},
			false,
		},
		{
			"hostname",
			"gateway",
			&iksv1.StaticRoute{
				Spec: iksv1.StaticRouteSpec{
					Subnet: "subnet",
				},
				Status: iksv1.StaticRouteStatus{
					NodeStatus: []iksv1.StaticRouteNodeStatus{
						iksv1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: iksv1.StaticRouteSpec{
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
			&iksv1.StaticRoute{
				Spec: iksv1.StaticRouteSpec{
					Subnet: "subnet",
				},
				Status: iksv1.StaticRouteStatus{
					NodeStatus: []iksv1.StaticRouteNodeStatus{
						iksv1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: iksv1.StaticRouteSpec{
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
			&iksv1.StaticRoute{
				Spec: iksv1.StaticRouteSpec{
					Subnet: "subnet2",
				},
				Status: iksv1.StaticRouteStatus{
					NodeStatus: []iksv1.StaticRouteNodeStatus{
						iksv1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: iksv1.StaticRouteSpec{
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
			&iksv1.StaticRoute{
				Spec: iksv1.StaticRouteSpec{
					Subnet: "subnet2",
				},
				Status: iksv1.StaticRouteStatus{
					NodeStatus: []iksv1.StaticRouteNodeStatus{
						iksv1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: iksv1.StaticRouteSpec{
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
			&iksv1.StaticRoute{
				Spec: iksv1.StaticRouteSpec{
					Subnet: "subnet2",
				},
				Status: iksv1.StaticRouteStatus{
					NodeStatus: []iksv1.StaticRouteNodeStatus{
						iksv1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: iksv1.StaticRouteSpec{
								Subnet:  "subnet",
								Gateway: "gateway",
							},
						},
						iksv1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: iksv1.StaticRouteSpec{
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
			&iksv1.StaticRoute{
				Spec: iksv1.StaticRouteSpec{
					Subnet: "subnet",
				},
				Status: iksv1.StaticRouteStatus{
					NodeStatus: []iksv1.StaticRouteNodeStatus{
						iksv1.StaticRouteNodeStatus{
							Hostname: "hostname",
							State: iksv1.StaticRouteSpec{
								Subnet:  "subnet",
								Gateway: "gateway",
							},
						},
						iksv1.StaticRouteNodeStatus{
							Hostname: "hostname2",
							State: iksv1.StaticRouteSpec{
								Subnet:  "subnet2",
								Gateway: "gateway2",
							},
						},
					},
				},
			},
			false,
		},
	}

	for i, td := range testData {
		rw := routeWrapper{instance: td.route}

		res := rw.isChanged(td.hostname, td.gateway)

		if res != td.result {
			t.Errorf("Result must be %t, it is %t ad %d", td.result, res, i)
		}
	}

}

func TestRouteWrapperSetFinalizer(t *testing.T) {
	route := newStaticRouteWithValues(true)
	rw := routeWrapper{instance: route}

	done := rw.setFinalizer()

	if !done || len(route.GetFinalizers()) == 0 {
		t.Error("Finalizer must be added")
	} else if route.GetFinalizers()[0] != "finalizer.iks.ibm.com" {
		t.Errorf("`finalizer.iks.ibm.com` not setted as finalizer: %v", route.GetFinalizers())
	}
}

func TestRouteWrapperSetFinalizerNotEmpty(t *testing.T) {
	route := newStaticRouteWithValues(true)
	route.SetFinalizers([]string{"finalizer"})
	rw := routeWrapper{instance: route}

	done := rw.setFinalizer()

	if done {
		t.Error("Finalizer must be not added")
	}
}

func TestRouteWrapperGetGateway(t *testing.T) {
	route := newStaticRouteWithValues(true)
	rw := routeWrapper{instance: route}

	gw := rw.getGateway()

	if gw.String() != "10.0.0.1" {
		t.Errorf("Gateway must be `10.0.0.1`: %s", gw.String())
	}
}

func TestRouteWrapperGetGatewayMissing(t *testing.T) {
	route := newStaticRouteWithValues(false)

	rw := routeWrapper{instance: route}

	gw := rw.getGateway()

	if gw != nil {
		t.Errorf("Gateway must be nil: %s", gw.String())
	}
}

func TestRouteWrapperGetGatewayInvalid(t *testing.T) {
	route := newStaticRouteWithValues(false)
	route.Spec.Gateway = "invalid-gateway"
	rw := routeWrapper{instance: route}

	gw := rw.getGateway()

	if gw != nil {
		t.Errorf("Gateway must be nil: %s", gw.String())
	}
}

func TestRouteWrapperAddToStatus(t *testing.T) {
	route := newStaticRouteWithValues(false)
	rw := routeWrapper{instance: route}

	added := rw.addToStatus("hostname", net.IP{10, 0, 0, 1})

	if !added {
		t.Error("Status must be added")
	} else if len(route.Status.NodeStatus) != 1 {
		t.Errorf("Status not added: %v", route.Status.NodeStatus)
	} else if route.Status.NodeStatus[0].Hostname != "hostname" {
		t.Errorf("First status must be `hostname`: %s", route.Status.NodeStatus[0].Hostname)
	} else if route.Status.NodeStatus[0].State.Gateway != "10.0.0.1" {
		t.Errorf("First status gateway must be `10.0.0.1`: %s", route.Status.NodeStatus[0].State.Gateway)
	}
}

func TestRouteWrapperAddToStatusNotAdded(t *testing.T) {
	route := newStaticRouteWithValues(false)
	route.Status = iksv1.StaticRouteStatus{
		NodeStatus: []iksv1.StaticRouteNodeStatus{
			iksv1.StaticRouteNodeStatus{
				Hostname: "hostname",
			},
		},
	}
	rw := routeWrapper{instance: route}

	added := rw.addToStatus("hostname", net.IP{10, 0, 0, 1})

	if added {
		t.Error("Status must be not added")
	}
}

func TestRouteWrapperRemoveFromStatusNotRemoved(t *testing.T) {
	route := newStaticRouteWithValues(false)
	route.Status = iksv1.StaticRouteStatus{
		NodeStatus: []iksv1.StaticRouteNodeStatus{
			iksv1.StaticRouteNodeStatus{
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
	route := newStaticRouteWithValues(false)
	route.Status = iksv1.StaticRouteStatus{
		NodeStatus: []iksv1.StaticRouteNodeStatus{
			iksv1.StaticRouteNodeStatus{
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
