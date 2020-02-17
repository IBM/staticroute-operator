package routemanager

import (
	"net"
)

//Route structure represents just-enough data to manage IP routes from user code
type Route struct {
	Dst   net.IPNet
	Gw    net.IP
	Table int
}

//RouteWatcher is a user-implemented interface, where RouteManager will call back if a managed route is damaged
type RouteWatcher interface {
	RouteDeleted(Route)
}

//RouteManager is the main interface, which is implemented by the package
type RouteManager interface {
	//RegisterRoute creates and start watching the route. If the route is deleted after the registration, RouteWatchers will be notified.
	RegisterRoute(string, Route) error
	//DeRegisterRoute removed the route from the kernel and also stop watching it.
	DeRegisterRoute(string) error
	//RegisterWatcher registers a new RouteWatcher, which will be notified if the managed routes are deleted.
	RegisterWatcher(RouteWatcher) error
	//DeRegisterWatcher removes watchers
	DeRegisterWatcher(RouteWatcher) error
	//Run is the main event loop, shall run in it's own go-routine. Returns when the channel sent in got closed.
	Run(chan struct{})
}
