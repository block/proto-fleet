package discovery

import (
	"net/netip"
	"strconv"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
)

// buildReportScope derives, from the validated request, a matcher that accepts
// a reported (ipAddress, port) only when it falls within what the node was asked
// to scan. The gateway checks every reported device against it so a compromised
// node can't report (or claim) devices outside the requested scope.
func buildReportScope(req *pairingpb.DiscoverRequest) control.ReportScope {
	switch m := req.GetMode().(type) {
	case *pairingpb.DiscoverRequest_IpList:
		inIP := ipListMatcher(m.IpList.GetIpAddresses())
		inPort := portMatcher(m.IpList.GetPorts())
		return func(ip, port string) bool {
			return inPort(port) && inIP(ip)
		}
	case *pairingpb.DiscoverRequest_IpRange:
		target, err := validatedIPv4Range(m.IpRange.GetStartIp(), m.IpRange.GetEndIp())
		if err != nil {
			return func(string, string) bool { return false }
		}
		inPort := portMatcher(m.IpRange.GetPorts())
		return func(ip, port string) bool {
			if !inPort(port) {
				return false
			}
			addr, ok := parseScopeAddr(ip)
			if !ok || !addr.Is4() {
				return false
			}
			return target.Contains(addr)
		}
	case *pairingpb.DiscoverRequest_NetworkScan:
		inPort := portMatcher(m.NetworkScan.GetPorts())
		// The LocalSubnetTarget sentinel lets the agent pick its own subnet, so
		// the server can't predict the IPs. Degrade the IP scope to the
		// private-only invariant (RFC1918/RFC4193) that validateReport
		// independently enforces; port scoping is still applied.
		if m.NetworkScan.GetTarget() == netscan.LocalSubnetTarget {
			return func(ip, port string) bool {
				if !inPort(port) {
					return false
				}
				a, ok := parseScopeAddr(ip)
				return ok && a.IsPrivate()
			}
		}
		inTarget := networkScanTargetMatcher(m.NetworkScan.GetTarget())
		return func(ip, port string) bool {
			return inPort(port) && inTarget(ip)
		}
	default:
		// ValidateRequest rejects other modes; fail closed.
		return func(string, string) bool { return false }
	}
}

// parseScopeAddr parses an address and unmaps IPv4-mapped IPv6 (e.g.
// ::ffff:192.168.1.10) to its IPv4 form, so a requested literal and the address
// the agent actually reports compare in the same representation: the agent's
// ResolveAddr already collapses mapped literals before probing.
func parseScopeAddr(s string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

// ipListMatcher accepts the listed literal IPs. A hostname entry resolves
// agent-side (via ResolveAddr) to an IP the server can't predict, so if
// any entry is a hostname the IP scope can't be enforced and only ports constrain
// the report (matching the network scan hostname path); otherwise a reported IP must be
// one of the listed addresses (compared in canonical, unmapped form).
func ipListMatcher(entries []string) func(string) bool {
	set := make(map[string]bool, len(entries))
	for _, e := range entries {
		addr, ok := parseScopeAddr(e)
		if !ok {
			return func(string) bool { return true }
		}
		set[addr.String()] = true
	}
	return func(ip string) bool {
		a, ok := parseScopeAddr(ip)
		return ok && set[a.String()]
	}
}

// portMatcher accepts the listed ports. An empty list means the request didn't
// constrain ports (the agent uses its default set), so any port is in scope.
func portMatcher(ports []string) func(string) bool {
	if len(ports) == 0 {
		return func(string) bool { return true }
	}
	set := make(map[int]bool, len(ports))
	for _, p := range ports {
		if n, err := strconv.Atoi(p); err == nil {
			set[n] = true
		}
	}
	return func(port string) bool {
		n, err := strconv.Atoi(port)
		return err == nil && set[n]
	}
}

// networkScanTargetMatcher scopes reports using the same target parser as execution.
// Hostnames resolve on the node, so their reports remain constrained by ports
// and the report validator's private-address policy.
func networkScanTargetMatcher(raw string) func(string) bool {
	target, err := netscan.ParseTarget(raw)
	return func(ip string) bool {
		addr, ok := parseScopeAddr(ip)
		return err == nil && ok && target.Contains(addr)
	}
}
