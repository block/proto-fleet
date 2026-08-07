package ha

import (
	"net"
	"net/netip"
	"os"
	"time"
)

// EndpointHeartbeatFile is refreshed only after keepalived's local proxy check succeeds.
const EndpointHeartbeatFile = "/run/proto-fleet-ha/endpoint-heartbeat"

// Endpoint health requires both local VIP ownership and proof that keepalived
// is still successfully probing the active client path.
func newEndpointHealth(endpointIP netip.Addr, endpointInterface, heartbeatFile string, timeout time.Duration) func() bool {
	return func() bool {
		if !localAddressAssigned(endpointIP, endpointInterface) {
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

func localAddressAssigned(want netip.Addr, interfaceName string) bool {
	// Re-read every sample because keepalived can move the VIP asynchronously.
	networkInterface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return false
	}
	addresses, err := networkInterface.Addrs()
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
