package routemanager

import (
	"reflect"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type routeManagerImpl struct {
	managedRoutes     []Route
	watchers          []RouteWatcher
	nlSubscribeFunc   func(chan<- netlink.RouteUpdate, <-chan struct{}) error
	nlAddRouteFunc    func(route *netlink.Route) error
	nlDelRouteFunc    func(route *netlink.Route) error
	registerRoute     chan routeManagerImplRegisterRouteParams
	deRegisterRoute   chan routeManagerImplRegisterRouteParams
	registerWatcher   chan RouteWatcher
	deRegisterWatcher chan RouteWatcher
}

type routeManagerImplRegisterRouteParams struct {
	route Route
	err   chan<- error
}

//New creates a RouteManager for production use. It populates the routeManagerImpl structure with the final pointers to netlink package's functions.
func New() RouteManager {
	return &routeManagerImpl{
		nlSubscribeFunc:   netlink.RouteSubscribe,
		nlAddRouteFunc:    netlink.RouteAdd,
		nlDelRouteFunc:    netlink.RouteDel,
		registerRoute:     make(chan routeManagerImplRegisterRouteParams),
		deRegisterRoute:   make(chan routeManagerImplRegisterRouteParams),
		registerWatcher:   make(chan RouteWatcher),
		deRegisterWatcher: make(chan RouteWatcher),
	}
}

func (r *routeManagerImpl) RegisterRoute(route Route) error {
	errChan := make(chan error)
	r.registerRoute <- routeManagerImplRegisterRouteParams{route, errChan}
	return <-errChan
}

func (r routeManagerImpl) DeRegisterRoute(route Route) error {
	errChan := make(chan error)
	r.deRegisterRoute <- routeManagerImplRegisterRouteParams{route, errChan}
	return <-errChan
}

func (r *routeManagerImpl) RegisterWatcher(w RouteWatcher) error {
	r.registerWatcher <- w
	return nil
}

func (r *routeManagerImpl) DeRegisterWatcher(w RouteWatcher) error {
	r.deRegisterWatcher <- w
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

func (r *routeManagerImpl) Run(stopChan chan struct{}) {
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
		case watcher := <-r.deRegisterWatcher:
			for index, item := range r.watchers {
				if reflect.DeepEqual(item, watcher) {
					r.watchers = append(r.watchers[:index], r.watchers[index+1:]...)
					break
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
					break
				}
			}
		}
	}
}
