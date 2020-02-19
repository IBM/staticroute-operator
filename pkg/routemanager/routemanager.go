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
	"errors"
	"reflect"
	"syscall"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type routeManagerImpl struct {
	managedRoutes         map[string]Route
	watchers              []RouteWatcher
	nlRouteSubscribeFunc  func(chan<- netlink.RouteUpdate, <-chan struct{}) error
	nlRouteAddFunc        func(route *netlink.Route) error
	nlRouteDelFunc        func(route *netlink.Route) error
	registerRouteChan     chan routeManagerImplRegisterRouteParams
	deRegisterRouteChan   chan routeManagerImplDeRegisterRouteParams
	registerWatcherChan   chan RouteWatcher
	deRegisterWatcherChan chan RouteWatcher
}

type routeManagerImplRegisterRouteParams struct {
	name  string
	route Route
	err   chan<- error
}

type routeManagerImplDeRegisterRouteParams struct {
	name string
	err  chan<- error
}

//New creates a RouteManager for production use. It populates the routeManagerImpl structure with the final pointers to netlink package's functions.
func New() RouteManager {
	return &routeManagerImpl{
		managedRoutes:         make(map[string]Route),
		nlRouteSubscribeFunc:  netlink.RouteSubscribe,
		nlRouteAddFunc:        netlink.RouteAdd,
		nlRouteDelFunc:        netlink.RouteDel,
		registerRouteChan:     make(chan routeManagerImplRegisterRouteParams),
		deRegisterRouteChan:   make(chan routeManagerImplDeRegisterRouteParams),
		registerWatcherChan:   make(chan RouteWatcher),
		deRegisterWatcherChan: make(chan RouteWatcher),
	}
}

func (r *routeManagerImpl) RegisterRoute(name string, route Route) error {
	errChan := make(chan error)
	r.registerRouteChan <- routeManagerImplRegisterRouteParams{name, route, errChan}
	return <-errChan
}

func (r *routeManagerImpl) IsRegistered(name string) bool {
	_, exists := r.managedRoutes[name]
	return exists
}

func (r *routeManagerImpl) registerRoute(params routeManagerImplRegisterRouteParams) {
	if r.IsRegistered(params.name) {
		params.err <- errors.New("Route with the same Name already registered")
		return
	}
	nlRoute := params.route.toNetLinkRoute()
	/* If syscall returns EEXIST (file exists), it means the route already existing.
	   There is no evidence that we created is before a crash, or someone else.
	   We assume we created it and so start managing it again. */
	if err := r.nlRouteAddFunc(&nlRoute); err != nil && syscall.EEXIST.Error() != err.Error() {
		params.err <- err
		return
	}
	r.managedRoutes[params.name] = params.route
	params.err <- nil
}

func (r *routeManagerImpl) DeRegisterRoute(name string) error {
	errChan := make(chan error)
	r.deRegisterRouteChan <- routeManagerImplDeRegisterRouteParams{name, errChan}
	return <-errChan
}

func (r *routeManagerImpl) deRegisterRoute(params routeManagerImplDeRegisterRouteParams) {
	item, found := r.managedRoutes[params.name]
	if !found {
		params.err <- errors.New("Route could not found")
		return
	}
	nlRoute := item.toNetLinkRoute()
	/* We remove the route from the managed ones, regardless of the ESRCH (no such process) error from the lower layer.
	   Error supposed to happen only when the route is already missing, which was reported to the watchers, so they know. */
	if err := r.nlRouteDelFunc(&nlRoute); err != nil && syscall.ESRCH.Error() != err.Error() {
		params.err <- err
		return
	}
	delete(r.managedRoutes, params.name)
	params.err <- nil
}

func (r *routeManagerImpl) RegisterWatcher(w RouteWatcher) {
	r.registerWatcherChan <- w
}

func (r *routeManagerImpl) registerWatcher(w RouteWatcher) {
	r.watchers = append(r.watchers, w)
}

func (r *routeManagerImpl) DeRegisterWatcher(w RouteWatcher) {
	r.deRegisterWatcherChan <- w
}

func (r *routeManagerImpl) deRegisterWatcher(w RouteWatcher) {
	for index, item := range r.watchers {
		if reflect.DeepEqual(item, w) {
			r.watchers = append(r.watchers[:index], r.watchers[index+1:]...)
			break
		}
	}
}

func (r Route) toNetLinkRoute() netlink.Route {
	return netlink.Route{
		Dst:   &r.Dst,
		Gw:    r.Gw,
		Table: r.Table,
	}
}

/* This version of equal shall be used everywhere in this package.
   Netlink also does have an Equal function, however if we use that with
   mixing netlink.Route and routemanager.Route input, it will report false.
   The reason is that netlink.Route has a lot of additional properties which
   routemanager.Route doesn't (type, protocol, linkindex, etc.). So we need
   to convert back and forth the netlink.Route instances before comparing them
   to zero out the fields which we do not store in this package. */
func (r Route) equal(x Route) bool {
	return r.toNetLinkRoute().Equal(x.toNetLinkRoute())
}

func fromNetLinkRoute(netlinkRoute netlink.Route) Route {
	return Route{
		Dst:   *netlinkRoute.Dst,
		Gw:    netlinkRoute.Gw,
		Table: netlinkRoute.Table,
	}
}

func (r *routeManagerImpl) notifyWatchers(update netlink.RouteUpdate) {
	if update.Type != unix.RTM_DELROUTE {
		return
	}
	for _, route := range r.managedRoutes {
		if route.equal(fromNetLinkRoute(update.Route)) {
			for _, watcher := range r.watchers {
				watcher.RouteDeleted(fromNetLinkRoute(update.Route))
			}
			break
		}
	}
}

func (r *routeManagerImpl) Run(stopChan chan struct{}) error {
	updateChan := make(chan netlink.RouteUpdate)
	if err := r.nlRouteSubscribeFunc(updateChan, stopChan); err != nil {
		return err
	}
loop:
	for {
		select {
		case update, ok := <-updateChan:
			if !ok {
				break loop
			}
			r.notifyWatchers(update)
		case <-stopChan:
			break loop
		case watcher := <-r.registerWatcherChan:
			r.registerWatcher(watcher)
		case watcher := <-r.deRegisterWatcherChan:
			r.deRegisterWatcher(watcher)
		case params := <-r.registerRouteChan:
			r.registerRoute(params)
		case params := <-r.deRegisterRouteChan:
			r.deRegisterRoute(params)
		}
	}
	return nil
}
