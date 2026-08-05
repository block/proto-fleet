package ha

import (
	"net"
	"net/netip"
	"os"
	"time"
)

// EndpointHeartbeatFile is the local keepalived-to-Fleet liveness contract.
const EndpointHeartbeatFile = "/run/proto-fleet-ha/endpoint-heartbeat"

func newEndpointHealth(endpointIP netip.Addr, heartbeatFile string, timeout time.Duration) func() bool {
	return func() bool {
		if !localAddressAssigned(endpointIP) {
			return false
		}
		info, err := os.Stat(heartbeatFile)
		if err != nil {
			return false
		}
		age := time.Since(info.ModTime())
		return age >= 0 && age <= timeout
	}
}

func localAddressAssigned(want netip.Addr) bool {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil && prefix.Addr() == want {
			return true
		}
	}
	return false
}
