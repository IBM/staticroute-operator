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

package node

import (
	"testing"

	iksv1 "github.com/IBM/staticroute-operator/pkg/apis/iks/v1"
)

func TestFindNodeFound(t *testing.T) {
	nf := nodeFinder{
		nodeName: "foo",
	}
	route := &iksv1.StaticRoute{
		Status: iksv1.StaticRouteStatus{
			NodeStatus: []iksv1.StaticRouteNodeStatus{
				iksv1.StaticRouteNodeStatus{Hostname: "bar"},
				iksv1.StaticRouteNodeStatus{Hostname: "foo"},
			},
		},
	}

	index := nf.findNode(route)

	if 1 != index {
		t.Errorf("Index not match 1 == %d", index)
	}
}

func TestFindNodeNotFound(t *testing.T) {
	nf := nodeFinder{
		nodeName: "not-found",
	}
	route := &iksv1.StaticRoute{
		Status: iksv1.StaticRouteStatus{
			NodeStatus: []iksv1.StaticRouteNodeStatus{
				iksv1.StaticRouteNodeStatus{Hostname: "bar"},
				iksv1.StaticRouteNodeStatus{Hostname: "foo"},
			},
		},
	}

	index := nf.findNode(route)

	if -1 != index {
		t.Errorf("Index not match -1 == %d", index)
	}
}

func TestDelete(t *testing.T) {
	var updateInputParam *iksv1.StaticRoute
	nf := nodeFinder{
		nodeName: "to-delete",
		updateCallback: func(r *iksv1.StaticRoute) error {
			updateInputParam = r
			return nil
		},
		infoLogger: func(string, ...interface{}) {},
	}
	routes := &iksv1.StaticRouteList{
		Items: []iksv1.StaticRoute{
			iksv1.StaticRoute{
				Status: iksv1.StaticRouteStatus{
					NodeStatus: []iksv1.StaticRouteNodeStatus{
						iksv1.StaticRouteNodeStatus{Hostname: "foo"},
						iksv1.StaticRouteNodeStatus{Hostname: "to-delete"},
						iksv1.StaticRouteNodeStatus{Hostname: "bar"},
					},
				},
			},
		},
	}

	nf.delete(routes)

	if updateInputParam == nil {
		t.Errorf("Update clannback was not called or called with nil")
	} else if len(updateInputParam.Status.NodeStatus) != 2 {
		t.Errorf("Node deletion went fail")
	} else if act := updateInputParam.Status.NodeStatus[0].Hostname + updateInputParam.Status.NodeStatus[1].Hostname; act != "foobar" {
		t.Errorf("Not the right status was deleted 'foobar' == %s", act)
	}
}
