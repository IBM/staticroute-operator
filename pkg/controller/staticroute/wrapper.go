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
	"reflect"

	iksv1 "github.com/IBM/staticroute-operator/pkg/apis/iks/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func (rw *routeWrapper) isProtected(protecteds []*net.IPNet) bool {
	_, subnetNet, err := net.ParseCIDR(rw.instance.Spec.Subnet)
	if err != nil {
		return false
	}
	inc := func(ip net.IP) {
		for i := len(ip) - 1; i >= 0; i-- {
			ip[i]++
			if ip[i] > 0 {
				break
			}
		}
	}
	for _, protected := range protecteds {
		for ip := protected.IP.Mask(protected.Mask); protected.Contains(ip); inc(ip) {
			if subnetNet.Contains(ip) {
				return true
			}
		}
	}

	return false
}

func (rw *routeWrapper) isChanged(hostname, gateway string, selectors []metav1.LabelSelectorRequirement) bool {
	for _, s := range rw.instance.Status.NodeStatus {
		if s.Hostname != hostname {
			continue
		} else if s.State.Subnet != rw.instance.Spec.Subnet || s.State.Gateway != gateway || !reflect.DeepEqual(s.State.Selectors, selectors) {
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

func (rw *routeWrapper) addToStatus(hostname string, gateway net.IP, err error) bool {
	// Update the status if necessary
	for _, val := range rw.instance.Status.NodeStatus {
		if val.Hostname == hostname {
			return false
		}
	}
	spec := rw.instance.Spec
	spec.Gateway = gateway.String()
	errorString := ""
	if err != nil {
		errorString = err.Error()
	}
	rw.instance.Status.NodeStatus = append(rw.instance.Status.NodeStatus, iksv1.StaticRouteNodeStatus{
		Hostname: hostname,
		State:    spec,
		Error:    errorString,
	})
	return true
}

func (rw *routeWrapper) alreadyInStatus(hostname string) bool {
	for _, val := range rw.instance.Status.NodeStatus {
		if val.Hostname == hostname {
			return true
		}
	}

	return false
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
