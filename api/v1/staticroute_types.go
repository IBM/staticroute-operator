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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// StaticRouteSpec defines the desired state of StaticRoute
type StaticRouteSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Subnet defines the required IP subnet in the form of: "x.x.x.x/x"
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}(\/([0-9]|[1-2][0-9]|3[0-2]))?$`
	Subnet string `json:"subnet"`

	// Gateway the gateway the subnet is routed through (optional, discovered if not set)
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}$`
	Gateway string `json:"gateway,omitempty"`

	// Table the route will be installed in (optional, uses default table if not set)
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=254
	Table *int `json:"table,omitempty"`

	// Selector defines the target nodes by requirement (optional, default is apply to all)
	Selectors []metav1.LabelSelectorRequirement `json:"selectors,omitempty"`
}

// StaticRouteNodeStatus defines the observed state of one IKS node, related to the StaticRoute
type StaticRouteNodeStatus struct {
	Hostname string          `json:"hostname"`
	State    StaticRouteSpec `json:"state"`
	Error    string          `json:"error"`
}

// StaticRouteStatus defines the observed state of StaticRoute
type StaticRouteStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	NodeStatus []StaticRouteNodeStatus `json:"nodeStatus"`
}

// +kubebuilder:object:root=true

// StaticRoute is the Schema for the staticroutes API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=staticroutes,scope=Cluster
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,priority=0
// +kubebuilder:printcolumn:name="Network",type=string,JSONPath=`.spec.subnet`,priority=1
// +kubebuilder:printcolumn:name="Gateway",type=string,JSONPath=`.spec.gateway`,description="empty field means default gateway",priority=1
// +kubebuilder:printcolumn:name="Table",type=integer,JSONPath=`.spec.table`,description="empty field means default table",priority=1
type StaticRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StaticRouteSpec   `json:"spec,omitempty"`
	Status StaticRouteStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// StaticRouteList contains a list of StaticRoute
type StaticRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StaticRoute `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StaticRoute{}, &StaticRouteList{})
}
