package ha

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"time"
)

// EndpointHeartbeatFile is the local keepalived-to-Fleet liveness contract.
const EndpointHeartbeatFile = "/run/proto-fleet-ha-endpoint-heartbeat"

func newEndpointHealth(rawIP, heartbeatFile string, timeout time.Duration) (func() bool, error) {
	endpointIP, err := netip.ParseAddr(rawIP)
	if err != nil {
		return nil, fmt.Errorf("parse HA endpoint IP: %w", err)
	}
	return func() bool {
		if !localAddressAssigned(endpointIP) {
			return false
		}
		info, err := os.Stat(heartbeatFile)
		return err == nil && time.Since(info.ModTime()) <= timeout
	}, nil
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
