package routemanager

import (
	"net"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type Route struct {
	Dst   net.IPNet
	Gw    net.IP
	Table int
}

type RouteEvent int

type RouteWatcher interface {
	RouteDeleted(Route)
}

type RouteManager interface {
	RegisterRoute(Route) error
	DeRegisterRoute(Route) error
	RegisterWatcher(RouteWatcher) error
	DeRegisterWatcher(RouteWatcher) error
	Run(chan struct{})
}

type RouteManagerImpl struct {
	managedRoutes     []Route
	watchers          []RouteWatcher
	nlSubscribeFunc   func(chan<- netlink.RouteUpdate, <-chan struct{}) error
	registerRoute     chan RouteManagerImplRegisterRouteParams
	deRegisterRoute   chan RouteManagerImplRegisterRouteParams
	registerWatcher   chan RouteWatcher
	deregisterWatcher chan RouteWatcher
}

type RouteManagerImplRegisterRouteParams struct {
	route Route
	err   chan<- error
}

func Init() RouteManager {
	return &RouteManagerImpl{
		nlSubscribeFunc:   netlink.RouteSubscribe,
		registerRoute:     make(chan RouteManagerImplRegisterRouteParams),
		deRegisterRoute:   make(chan RouteManagerImplRegisterRouteParams),
		registerWatcher:   make(chan RouteWatcher),
		deregisterWatcher: make(chan RouteWatcher),
	}
}

func (r *RouteManagerImpl) RegisterRoute(route Route) error {
	errChan := make(chan error)
	r.registerRoute <- RouteManagerImplRegisterRouteParams{route, errChan}
	return <-errChan
}

func (r RouteManagerImpl) DeRegisterRoute(route Route) error {
	errChan := make(chan error)
	r.deRegisterRoute <- RouteManagerImplRegisterRouteParams{route, errChan}
	return <-errChan
}

func (r *RouteManagerImpl) RegisterWatcher(w RouteWatcher) error {
	r.registerWatcher <- w
	return nil
}

func (r *RouteManagerImpl) DeRegisterWatcher(w RouteWatcher) error {
	r.deregisterWatcher <- w
	return nil
}

func (r Route) toNetLinkRoute() netlink.Route {
	return netlink.Route{
		Dst:   &r.Dst,
		Gw:    r.Gw,
		Table: r.Table,
	}
}

func fromNetLinkRoute(netlinkRoute netlink.Route) Route {
	return Route{
		Dst:   *netlinkRoute.Dst,
		Gw:    netlinkRoute.Gw,
		Table: netlinkRoute.Table,
	}
}

func (r *RouteManagerImpl) Run(stopChan chan struct{}) {
	updateChan := make(chan netlink.RouteUpdate)
	r.nlSubscribeFunc(updateChan, stopChan)
loop:
	for {
		select {
		case update, ok := <-updateChan:
			if !ok {
				break loop
			}
			if update.Type != unix.RTM_DELROUTE {
				break
			}
			for _, route := range r.managedRoutes {
				if route.toNetLinkRoute().Equal(update.Route) {
					for _, watcher := range r.watchers {
						watcher.RouteDeleted(fromNetLinkRoute(update.Route))
					}
					break
				}
			}
		case <-stopChan:
			break loop
		case watcher := <-r.registerWatcher:
			r.watchers = append(r.watchers, watcher)
		case watcher := <-r.deregisterWatcher:
			for index, item := range r.watchers {
				if item == watcher {
					r.watchers = append(r.watchers[:index], r.watchers[index+1:]...)
				}
			}
		case params := <-r.registerRoute:
			//TODO create route
			r.managedRoutes = append(r.managedRoutes, params.route)
			params.err <- nil
		case params := <-r.deRegisterRoute:
			for index, item := range r.managedRoutes {
				if params.route.toNetLinkRoute().Equal(item.toNetLinkRoute()) {
					//TODO remove route
					r.managedRoutes = append(r.managedRoutes[:index], r.managedRoutes[index+1:]...)
					params.err <- nil
				}
			}
		}
	}
}
