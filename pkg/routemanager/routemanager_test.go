package routemanager

import (
	"net"
	"sync"
	"testing"
)

type MockRouteWatcher struct {
}

func (m MockRouteWatcher) RouteDeleted(r Route) {
}

func TestNothingBlocks(t *testing.T) {
	rm := Init()
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
