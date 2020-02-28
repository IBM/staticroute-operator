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

	iksv1 "github.com/IBM/staticroute-operator/pkg/apis/iks/v1"
)

type routeWrapper struct {
	instance *iksv1.StaticRoute
}

//addFinalizer will add this attribute to the CR
func (rw *routeWrapper) setFinalizer() bool {
	if len(rw.instance.GetFinalizers()) != 0 {
		return false
	}
	rw.instance.SetFinalizers([]string{"finalizer.iks.ibm.com"})
	return true
}

func (rw *routeWrapper) isSameZone(zone, label string) bool {
	instanceZone := rw.instance.GetLabels()[label]

	return instanceZone == "" || instanceZone == zone
}

func (rw *routeWrapper) isChanged(hostname string) bool {
	for _, s := range rw.instance.Status.NodeStatus {
		if s.Hostname != hostname {
			continue
		} else if s.State.Subnet != rw.instance.Spec.Subnet || s.State.Gateway != rw.instance.Spec.Gateway {
			return true
		}
	}
	return false
}

// Returns nil like the underlaying net.ParseIP()
func (rw *routeWrapper) getGateway() net.IP {
	gateway := rw.instance.Spec.Gateway
	if len(gateway) == 0 {
		return nil
	}
	return net.ParseIP(gateway)
}

func (rw *routeWrapper) addToStatus(hostname string, gateway net.IP) bool {
	// Update the status if necessary
	for _, val := range rw.instance.Status.NodeStatus {
		if val.Hostname == hostname {
			return false
		}
	}

	spec := rw.instance.Spec
	spec.Gateway = gateway.String()
	rw.instance.Status.NodeStatus = append(rw.instance.Status.NodeStatus, iksv1.StaticRouteNodeStatus{
		Hostname: hostname,
		State:    spec,
		Error:    "",
	})
	return true
}

func (rw *routeWrapper) removeFromStatus(hostname string) (existed bool) {
	// Update the status if necessary
	statusArr := []iksv1.StaticRouteNodeStatus{}
	for _, val := range rw.instance.Status.NodeStatus {
		valCopy := val.DeepCopy()

		if valCopy.Hostname == hostname {
			existed = true
			continue
		}

		statusArr = append(statusArr, *valCopy)
	}

	rw.instance.Status = iksv1.StaticRouteStatus{NodeStatus: statusArr}

	return
}
