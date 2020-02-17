package routemanager

import (
	"errors"
	"net"
	"reflect"
	"runtime"
	"sync"
	"syscall"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type MockRouteWatcher struct {
	routeDeletedCalledWith chan Route
}

func (m MockRouteWatcher) RouteDeleted(r Route) {
	m.routeDeletedCalledWith <- r
}

var gMockUpdateChan chan<- netlink.RouteUpdate
var gTestRoute = Route{Dst: net.IPNet{IP: net.IP{192, 168, 1, 0}, Mask: net.CIDRMask(24, 32)}, Gw: net.IP{192, 168, 1, 254}, Table: 254}
var gTestRouteName = "name"

func mockRouteSubscribe(u chan<- netlink.RouteUpdate, c <-chan struct{}) error {
	gMockUpdateChan = u
	return nil
}

func dummyRouteAdd(route *netlink.Route) error {
	return nil
}

func dummyRouteDel(route *netlink.Route) error {
	return nil
}

type testableRouteManager struct {
	rm       RouteManager
	wg       sync.WaitGroup
	stopChan chan struct{}
}

func (m *testableRouteManager) start() {
	m.wg.Add(1)
	go func() {
		m.rm.Run(m.stopChan)
		m.wg.Done()
	}()
}

func (m *testableRouteManager) stop() {
	close(m.stopChan)
	m.wg.Wait()
}

func newTestableRouteManager() testableRouteManager {
	return testableRouteManager{
		rm: &routeManagerImpl{
			managedRoutes:         make(map[string]Route),
			nlRouteSubscribeFunc:  mockRouteSubscribe,
			nlRouteAddFunc:        dummyRouteAdd,
			nlRouteDelFunc:        dummyRouteDel,
			registerRouteChan:     make(chan routeManagerImplRegisterRouteParams),
			deRegisterRouteChan:   make(chan routeManagerImplDeRegisterRouteParams),
			registerWatcherChan:   make(chan RouteWatcher),
			deRegisterWatcherChan: make(chan RouteWatcher),
		},
		wg:       sync.WaitGroup{},
		stopChan: make(chan struct{}),
	}
}

func TestNewDoesReturnValidManager(t *testing.T) {
	rm := New()
	//Pretty intuitive way to check if two function pointers are identical. Thanks for: https://github.com/stretchr/testify/issues/182#issuecomment-495359313
	if runtime.FuncForPC(reflect.ValueOf(rm.(*routeManagerImpl).nlRouteAddFunc).Pointer()).Name() != runtime.FuncForPC(reflect.ValueOf(netlink.RouteAdd).Pointer()).Name() {
		t.Error("nlRouteAddFunc function is not pointing to netlink package")
	}
	if runtime.FuncForPC(reflect.ValueOf(rm.(*routeManagerImpl).nlRouteDelFunc).Pointer()).Name() != runtime.FuncForPC(reflect.ValueOf(netlink.RouteDel).Pointer()).Name() {
		t.Error("nlRouteDelFunc function is not pointing to netlink package")
	}
	if runtime.FuncForPC(reflect.ValueOf(rm.(*routeManagerImpl).nlRouteSubscribeFunc).Pointer()).Name() != runtime.FuncForPC(reflect.ValueOf(netlink.RouteSubscribe).Pointer()).Name() {
		t.Error("nlRouteSubscribeFunc function is not pointing to netlink package")
	}
	if rm.(*routeManagerImpl).registerRouteChan == nil {
		t.Error("registerRoute channel is not initialized")
	}
	if rm.(*routeManagerImpl).deRegisterRouteChan == nil {
		t.Error("deRegisterRoute channel is not initialized")
	}
	if rm.(*routeManagerImpl).registerWatcherChan == nil {
		t.Error("registerWatcher channel is not initialized")
	}
	if rm.(*routeManagerImpl).deRegisterWatcherChan == nil {
		t.Error("deRegisterWatcher channel is not initialized")
	}
}

func TestNothingBlocksInRun(t *testing.T) {
	testable := newTestableRouteManager()
	testable.start()
	defer testable.stop()

	mockWatcher := MockRouteWatcher{}
	testable.rm.RegisterRoute(gTestRouteName, gTestRoute)
	testable.rm.RegisterWatcher(mockWatcher)

	testable.rm.DeRegisterRoute(gTestRouteName)
	testable.rm.DeRegisterWatcher(mockWatcher)
}

func TestWatchNewRouteDoesNotTrigger(t *testing.T) {
	testable := newTestableRouteManager()
	testable.start()
	mockWatcher := MockRouteWatcher{routeDeletedCalledWith: make(chan Route)}

	testable.rm.RegisterWatcher(mockWatcher)
	if gMockUpdateChan == nil {
		t.Error("Update channel did not populate")
	}
	gMockUpdateChan <- netlink.RouteUpdate{Type: unix.RTM_NEWROUTE, Route: gTestRoute.toNetLinkRoute()}
	testable.rm.DeRegisterWatcher(mockWatcher)
	testable.stop()

	select {
	case <-mockWatcher.routeDeletedCalledWith:
		t.Error("Mock must not be triggered with new route, only del")
	default:
	}
}

func TestWatchDelRouteDoesNotTriggerIfNotWatched(t *testing.T) {
	testable := newTestableRouteManager()
	testable.start()
	mockWatcher := MockRouteWatcher{routeDeletedCalledWith: make(chan Route)}

	testable.rm.RegisterWatcher(mockWatcher)
	if gMockUpdateChan == nil {
		t.Error("Update channel did not populate")
	}
	gMockUpdateChan <- netlink.RouteUpdate{Type: unix.RTM_DELROUTE, Route: gTestRoute.toNetLinkRoute()}

	testable.stop()
	select {
	case <-mockWatcher.routeDeletedCalledWith:
		t.Error("Mock must not be triggered with a route which is not watched")
	default:
	}
}

func TestWatch(t *testing.T) {
	testable := newTestableRouteManager()
	testable.start()
	defer testable.stop()

	mockWatcher := MockRouteWatcher{routeDeletedCalledWith: make(chan Route)}
	testable.rm.RegisterRoute(gTestRouteName, gTestRoute)
	testable.rm.RegisterWatcher(mockWatcher)

	if gMockUpdateChan == nil {
		t.Error("Update channel did not populate")
	}
	gMockUpdateChan <- netlink.RouteUpdate{Type: unix.RTM_DELROUTE, Route: gTestRoute.toNetLinkRoute()}

	fromUpdate := <-mockWatcher.routeDeletedCalledWith
	if !fromUpdate.toNetLinkRoute().Equal(gTestRoute.toNetLinkRoute()) {
		t.Error("Route in update event must be the same which we sent in")
	}

	testable.rm.DeRegisterRoute(gTestRouteName)
	testable.rm.DeRegisterWatcher(mockWatcher)
}

func TestWatchCloseUpdateChan(t *testing.T) {
	testable := newTestableRouteManager()
	testable.start()

	mockWatcher := MockRouteWatcher{routeDeletedCalledWith: make(chan Route)}
	testable.rm.RegisterWatcher(mockWatcher)

	close(gMockUpdateChan)

	testable.wg.Wait()
}

func TestRegisterRouteSuccess(t *testing.T) {
	testable := newTestableRouteManager()
	addCalledWith := make(chan *netlink.Route)
	testable.rm.(*routeManagerImpl).nlRouteAddFunc = func(route *netlink.Route) error {
		addCalledWith <- route
		return nil
	}
	testable.start()

	go testable.rm.RegisterRoute(gTestRouteName, gTestRoute)
	addedRoute := <-addCalledWith
	testable.stop()
	if !addedRoute.Equal(gTestRoute.toNetLinkRoute()) {
		t.Error("Route sent to netlink does not match with the original")
	}
	if len(testable.rm.(*routeManagerImpl).managedRoutes) != 1 {
		t.Error("managedRoute slice must contain one element")
	} else {
		if !testable.rm.(*routeManagerImpl).managedRoutes[gTestRouteName].toNetLinkRoute().Equal(gTestRoute.toNetLinkRoute()) {
			t.Error("Route stored in managedRoutes must be equal to the one which we created in the test")
		}
	}

}

func TestRegisterRouteFail(t *testing.T) {
	testable := newTestableRouteManager()
	addCalledWith := make(chan *netlink.Route)
	testable.rm.(*routeManagerImpl).nlRouteAddFunc = func(route *netlink.Route) error {
		addCalledWith <- route
		return errors.New("bla")
	}
	testable.start()

	go testable.rm.RegisterRoute(gTestRouteName, gTestRoute)
	addedRoute := <-addCalledWith
	if !addedRoute.Equal(gTestRoute.toNetLinkRoute()) {
		t.Error("Route sent to netlink does not match with the original")
	}
	if len(testable.rm.(*routeManagerImpl).managedRoutes) > 0 {
		t.Error("managedRoute slice must be empty")
	}

	testable.stop()
}

func TestSameRegisterRouteTwiceFail(t *testing.T) {
	testable := newTestableRouteManager()
	testable.start()

	err := testable.rm.RegisterRoute(gTestRouteName, gTestRoute)
	if err != nil {
		t.Error("First RegisterRoute must pass")
	}
	if len(testable.rm.(*routeManagerImpl).managedRoutes) != 1 {
		t.Error("managedRoute slice must contain the added route")
	}
	err = testable.rm.RegisterRoute(gTestRouteName, gTestRoute)
	if err == nil {
		t.Error("Adding the same route for the second time shall fail")
	}
	if len(testable.rm.(*routeManagerImpl).managedRoutes) != 1 {
		t.Error("managedRoute slice must still contain the added route")
	}

	testable.stop()
}

func TestDeRegisterRouteAlreadyDeleted(t *testing.T) {
	testable := newTestableRouteManager()
	delCalledWith := make(chan *netlink.Route)
	testable.rm.(*routeManagerImpl).nlRouteDelFunc = func(route *netlink.Route) error {
		delCalledWith <- route
		return errors.New(syscall.ESRCH.Error())
	}
	testable.start()
	testable.rm.RegisterRoute(gTestRouteName, gTestRoute)

	go testable.rm.DeRegisterRoute(gTestRouteName)
	deletedRoute := <-delCalledWith
	testable.stop()
	if !deletedRoute.Equal(gTestRoute.toNetLinkRoute()) {
		t.Error("Route sent to netlink does not match with the original")
	}
	if len(testable.rm.(*routeManagerImpl).managedRoutes) > 0 {
		t.Error("managedRoute slice must be empty")
	}

}

func TestDeRegisterRouteUnknownError(t *testing.T) {
	testable := newTestableRouteManager()
	delCalledWith := make(chan *netlink.Route)
	testable.rm.(*routeManagerImpl).nlRouteDelFunc = func(route *netlink.Route) error {
		delCalledWith <- route
		return errors.New("bla")
	}
	testable.start()
	testable.rm.RegisterRoute(gTestRouteName, gTestRoute)

	go testable.rm.DeRegisterRoute(gTestRouteName)
	deletedRoute := <-delCalledWith
	if !deletedRoute.Equal(gTestRoute.toNetLinkRoute()) {
		t.Error("Route sent to netlink does not match with the original")
	}
	if len(testable.rm.(*routeManagerImpl).managedRoutes) != 1 {
		t.Error("managedRoute slice must still contain the route, which couldn't be removed due to an unknown error")
	}

	testable.stop()
}

func TestDeRegisterRouteWhichIsNotRegistered(t *testing.T) {
	testable := newTestableRouteManager()
	testable.start()
	err := testable.rm.DeRegisterRoute(gTestRouteName)
	if err.Error() != "Route could not found" {
		t.Error("Deregistration shall fail due to asking for a non-managed route")
	}
	testable.stop()
}
