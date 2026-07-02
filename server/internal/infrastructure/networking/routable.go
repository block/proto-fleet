package networking

import (
	"fmt"
	"net/netip"
)

// ParseRoutableMinerAddr validates a discovery-sourced miner address as a
// literal IP in a range fleet is willing to dial (no loopback, link-local,
// multicast, or unspecified — blocks SSRF via stale/poisoned records).
func ParseRoutableMinerAddr(ipAddress string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(ipAddress)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("miner address %q is not a valid IP", ipAddress)
	}
	addr = addr.Unmap()
	if addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() || addr.IsInterfaceLocalMulticast() || addr.IsUnspecified() {
		return netip.Addr{}, fmt.Errorf("miner address %q is not a routable miner address", ipAddress)
	}
	return addr, nil
}
