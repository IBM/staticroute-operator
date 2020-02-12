package routemanager

import "testing"

import "sync"

import "net"

func TestNothingBlocks(t *testing.T) {
	rm := Init()
	stopChan := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		rm.Run(stopChan)
		wg.Done()
	}()
	rm.RegisterRoute(Route{Dst: net.IPNet{IP: net.IP{192, 168, 1, 0}, Mask: net.IPMask{24}}, Gw: net.IP{192, 168, 1, 254}, Table: 254})
	rm.DeRegisterRoute(Route{Dst: net.IPNet{IP: net.IP{192, 168, 1, 0}, Mask: net.IPMask{24}}, Gw: net.IP{192, 168, 1, 254}, Table: 254})
	close(stopChan)
	wg.Wait()
}
