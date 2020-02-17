package routemanager

import (
	"reflect"
	"syscall"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type routeManagerImpl struct {
	managedRoutes        []Route
	watchers             []RouteWatcher
	nlRouteSubscribeFunc func(chan<- netlink.RouteUpdate, <-chan struct{}) error
	nlRouteAddFunc       func(route *netlink.Route) error
	nlRouteDelFunc       func(route *netlink.Route) error
	registerRoute        chan routeManagerImplRegisterRouteParams
	deRegisterRoute      chan routeManagerImplRegisterRouteParams
	registerWatcher      chan RouteWatcher
	deRegisterWatcher    chan RouteWatcher
}

type routeManagerImplRegisterRouteParams struct {
	route Route
	err   chan<- error
}

//New creates a RouteManager for production use. It populates the routeManagerImpl structure with the final pointers to netlink package's functions.
func New() RouteManager {
	return &routeManagerImpl{
		nlRouteSubscribeFunc: netlink.RouteSubscribe,
		nlRouteAddFunc:       netlink.RouteAdd,
		nlRouteDelFunc:       netlink.RouteDel,
		registerRoute:        make(chan routeManagerImplRegisterRouteParams),
		deRegisterRoute:      make(chan routeManagerImplRegisterRouteParams),
		registerWatcher:      make(chan RouteWatcher),
		deRegisterWatcher:    make(chan RouteWatcher),
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

func (r *routeManagerImpl) Run(stopChan chan struct{}) {
	updateChan := make(chan netlink.RouteUpdate)
	r.nlRouteSubscribeFunc(updateChan, stopChan)
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
				if route.equal(fromNetLinkRoute(update.Route)) {
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
			nlRoute := params.route.toNetLinkRoute()
			/* If syscall returns EEXIST (file exists), it means the route already existing.
			   There is no evidence that we created is before a crash, or someone else.
			   We assume we created it and so start managing it again. */
			if err := r.nlRouteAddFunc(&nlRoute); err != nil && syscall.EEXIST.Error() != err.Error() {
				params.err <- err
				break
			}
			r.managedRoutes = append(r.managedRoutes, params.route)
			params.err <- nil
		case params := <-r.deRegisterRoute:
			for index, item := range r.managedRoutes {
				if params.route.equal(item) {
					nlRoute := params.route.toNetLinkRoute()
					/* We remove the route from the managed ones, regardless of the ESRCH (no such process) error from the lower layer.
					   Error supposed to happen only when the route is already missing, which was reported to the watchers, so they know. */
					if err := r.nlRouteDelFunc(&nlRoute); err != nil && syscall.ESRCH.Error() != err.Error() {
						params.err <- err
						break
					}
					r.managedRoutes = append(r.managedRoutes[:index], r.managedRoutes[index+1:]...)
					params.err <- nil
					break
				}
			}
		}
	}
}
