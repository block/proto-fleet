package admin

import (
	"net/netip"
	"strconv"
	"strings"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/netutil"
	"github.com/block/proto-fleet/server/internal/domain/nmaptarget"
)

// buildReportScope derives, from the normalized request, a matcher that accepts
// a reported (ipAddress, port) only when it falls within what the node was asked
// to scan. The gateway checks every reported device against it so a compromised
// node can't report (or claim) devices outside the requested scope. IpRange is
// already expanded to IpList upstream, so only IpList and Nmap reach here.
func buildReportScope(req *pairingpb.DiscoverRequest) control.ReportScope {
	switch m := req.GetMode().(type) {
	case *pairingpb.DiscoverRequest_IpList:
		inIP := ipListMatcher(m.IpList.GetIpAddresses())
		inPort := portMatcher(m.IpList.GetPorts())
		return func(ip, port string) bool {
			return inPort(port) && inIP(ip)
		}
	case *pairingpb.DiscoverRequest_Nmap:
		inTarget := nmapTargetMatcher(m.Nmap.GetTarget())
		inPort := portMatcher(m.Nmap.GetPorts())
		return func(ip, port string) bool {
			return inPort(port) && inTarget(ip)
		}
	default:
		// normalizeDiscoverRequest rejects other modes; fail closed.
		return func(string, string) bool { return false }
	}
}

// parseScopeAddr parses an address and unmaps IPv4-mapped IPv6 (e.g.
// ::ffff:192.168.1.10) to its IPv4 form, so a requested literal and the address
// the agent actually reports compare in the same representation: the agent's
// NormalizeIPListEntry already collapses mapped literals before probing.
func parseScopeAddr(s string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

// ipListMatcher accepts the listed literal IPs. A hostname entry resolves
// agent-side (via NormalizeIPListEntry) to an IP the server can't predict, so if
// any entry is a hostname the IP scope can't be enforced and only ports constrain
// the report (matching the Nmap hostname path); otherwise a reported IP must be
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

// nmapTargetMatcher accepts IPs covered by the nmap target: a CIDR's hosts, an
// "A.B.C.D-N" range, or a literal address. A hostname target resolves agent-side
// to IP(s) the server can't predict, so it is constrained by ports only.
func nmapTargetMatcher(target string) func(string) bool {
	if prefix, err := netip.ParsePrefix(target); err == nil {
		return func(ip string) bool {
			a, ok := parseScopeAddr(ip)
			return ok && prefix.Contains(a)
		}
	}
	if nmaptarget.IsIPv4Range(target) {
		if start, end, ok := parseIPv4Range(target); ok {
			return func(ip string) bool {
				a, ok := parseScopeAddr(ip)
				return ok && a.Is4() && !a.Less(start) && !end.Less(a)
			}
		}
	}
	if want, ok := parseScopeAddr(target); ok {
		return func(ip string) bool {
			a, ok := parseScopeAddr(ip)
			return ok && a == want
		}
	}
	return func(string) bool { return true }
}

// nmapTargetIsPrivate reports whether every address the nmap target can cover is
// private, mirroring validateReport's addr.IsPrivate() so a public target fails
// fast at dispatch instead of as a late REPORT_FAILED ack. A hostname resolves
// agent-side to an IP the server can't predict, so it returns true and the report
// validator guards what comes back.
func nmapTargetIsPrivate(target string) bool {
	if prefix, err := netip.ParsePrefix(target); err == nil {
		// The /22 IPv4 CIDR cap keeps a prefix inside one RFC1918 block, so the
		// network address's range decides the whole block.
		return prefix.Addr().Unmap().IsPrivate()
	}
	if nmaptarget.IsIPv4Range(target) {
		start, end, ok := parseIPv4Range(target)
		return ok && start.IsPrivate() && end.IsPrivate()
	}
	if a, ok := parseScopeAddr(target); ok {
		return a.IsPrivate()
	}
	return true // hostname: resolved agent-side; report validator enforces private-only
}

// parseIPv4Range parses an "A.B.C.D-N" nmap range into the inclusive [start, end]
// bounds (end shares A.B.C and uses N as its last octet).
func parseIPv4Range(s string) (start, end netip.Addr, ok bool) {
	head, tail, found := strings.Cut(s, "-")
	if !found {
		return netip.Addr{}, netip.Addr{}, false
	}
	start, err := netutil.ParseIPv4(head)
	if err != nil {
		return netip.Addr{}, netip.Addr{}, false
	}
	if n, atoiErr := strconv.Atoi(tail); atoiErr != nil || n < 0 || n > 255 {
		return netip.Addr{}, netip.Addr{}, false
	}
	octets := strings.Split(head, ".")
	if len(octets) != 4 {
		return netip.Addr{}, netip.Addr{}, false
	}
	end, err = netutil.ParseIPv4(octets[0] + "." + octets[1] + "." + octets[2] + "." + tail)
	if err != nil || end.Less(start) {
		return netip.Addr{}, netip.Addr{}, false
	}
	return start, end, true
}
