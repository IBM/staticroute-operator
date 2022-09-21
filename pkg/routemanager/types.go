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

package routemanager

import (
	"net"
)

// Route structure represents just-enough data to manage IP routes from user code
type Route struct {
	Dst   net.IPNet
	Gw    net.IP
	Table int
}

// RouteWatcher is a user-implemented interface, where RouteManager will call back if a managed route is damaged
type RouteWatcher interface {
	RouteDeleted(Route)
}

// RouteManager is the main interface, which is implemented by the package
type RouteManager interface {
	//IsRegistered returns true if a Route (by it's name) is already managed
	IsRegistered(string) bool
	//RegisterRoute creates and start watching the route. If the route is deleted after the registration, RouteWatchers will be notified.
	RegisterRoute(string, Route) error
	//DeRegisterRoute removed the route from the kernel and also stop watching it.
	DeRegisterRoute(string) error
	//RegisterWatcher registers a new RouteWatcher, which will be notified if the managed routes are deleted.
	RegisterWatcher(RouteWatcher)
	//DeRegisterWatcher removes watchers
	DeRegisterWatcher(RouteWatcher)
	//Run is the main event loop, shall run in it's own go-routine. Returns when the channel sent in got closed.
	Run(chan struct{}) error
}
