package routemanager

import (
	"net"
	"sync"
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

func mockRouteSubscribe(u chan<- netlink.RouteUpdate, c <-chan struct{}) error {
	gMockUpdateChan = u
	return nil
}

func mockRouteAdd(route *netlink.Route) error {
	return nil
}

func mockRouteDel(route *netlink.Route) error {
	return nil
}

func newMock() RouteManager {
	return &routeManagerImpl{
		nlSubscribeFunc:   mockRouteSubscribe,
		nlAddRouteFunc:    mockRouteAdd,
		nlDelRouteFunc:    mockRouteDel,
		registerRoute:     make(chan routeManagerImplRegisterRouteParams),
		deRegisterRoute:   make(chan routeManagerImplRegisterRouteParams),
		registerWatcher:   make(chan RouteWatcher),
		deRegisterWatcher: make(chan RouteWatcher),
	}
}

func TestNothingBlocks(t *testing.T) {
	rm := New()
	stopChan := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		rm.Run(stopChan)
		wg.Done()
	}()
	mockWatcher := MockRouteWatcher{}
	rm.RegisterRoute(Route{Dst: net.IPNet{IP: net.IP{192, 168, 1, 0}, Mask: net.IPMask{24}}, Gw: net.IP{192, 168, 1, 254}, Table: 254})
	rm.RegisterWatcher(mockWatcher)

	rm.DeRegisterRoute(Route{Dst: net.IPNet{IP: net.IP{192, 168, 1, 0}, Mask: net.IPMask{24}}, Gw: net.IP{192, 168, 1, 254}, Table: 254})
	rm.DeRegisterWatcher(mockWatcher)
	close(stopChan)
	wg.Wait()
}

func TestWatchNewRouteDoesNotTrigger(t *testing.T) {
	rm := newMock()
	stopChan := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		rm.Run(stopChan)
		wg.Done()
	}()
	mockWatcher := MockRouteWatcher{routeDeletedCalledWith: make(chan Route)}
	rm.RegisterWatcher(mockWatcher)

	if gMockUpdateChan == nil {
		t.Error("Update channel did not populate")
	}
	gMockUpdateChan <- netlink.RouteUpdate{Type: unix.RTM_NEWROUTE, Route: Route{Dst: net.IPNet{IP: net.IP{192, 168, 1, 0}, Mask: net.IPMask{24}}, Gw: net.IP{192, 168, 1, 254}, Table: 254}.toNetLinkRoute()}

	rm.DeRegisterWatcher(mockWatcher)
	close(stopChan)
	wg.Wait()
	select {
	case <-mockWatcher.routeDeletedCalledWith:
		t.Error("Mock must not be triggered with new route, only del")
	default:
	}
}

func TestWatch(t *testing.T) {
	rm := newMock()
	stopChan := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		rm.Run(stopChan)
		wg.Done()
	}()
	mockWatcher := MockRouteWatcher{routeDeletedCalledWith: make(chan Route)}
	testRoute := Route{Dst: net.IPNet{IP: net.IP{192, 168, 1, 0}, Mask: net.IPMask{24}}, Gw: net.IP{192, 168, 1, 254}, Table: 254}
	rm.RegisterRoute(testRoute)
	rm.RegisterWatcher(mockWatcher)

	if gMockUpdateChan == nil {
		t.Error("Update channel did not populate")
	}
	gMockUpdateChan <- netlink.RouteUpdate{Type: unix.RTM_DELROUTE, Route: testRoute.toNetLinkRoute()}

	fromUpdate := <-mockWatcher.routeDeletedCalledWith
	if !fromUpdate.toNetLinkRoute().Equal(testRoute.toNetLinkRoute()) {
		t.Error("Route in update event must be the same which we sent in")
	}

	rm.DeRegisterRoute(Route{Dst: net.IPNet{IP: net.IP{192, 168, 1, 0}, Mask: net.IPMask{24}}, Gw: net.IP{192, 168, 1, 254}, Table: 254})
	rm.DeRegisterWatcher(mockWatcher)
	close(stopChan)
	wg.Wait()
}

func TestWatchCloseUpdateChan(t *testing.T) {
	rm := newMock()
	stopChan := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		rm.Run(stopChan)
		wg.Done()
	}()
	mockWatcher := MockRouteWatcher{routeDeletedCalledWith: make(chan Route)}
	rm.RegisterWatcher(mockWatcher)

	close(gMockUpdateChan)

	wg.Wait()
}
