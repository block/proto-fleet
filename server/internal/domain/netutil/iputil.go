package netutil

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// Narrow interface so tests can stub DNS without a real resolver.
// *net.Resolver satisfies it as-is.
type IPListResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Unexported because no caller branches on cause; pairing and the agent
// just skip-and-log. Tests use these for clearer assertions.
var (
	errEmptyTarget        = errors.New("empty IP/hostname")
	errScopedIPv6         = errors.New("scoped IPv6 (%zone) is not supported")
	errLinkLocalIPv6      = errors.New("link-local IPv6 requires interface scope")
	errHostnameUnresolved = errors.New("hostname did not resolve to a usable address")
)

// Scoped ("%zone") and link-local fe80::/10 IPv6 are rejected: the TCP
// stack can't dial them without interface scope, and net.IP.String() doesn't
// round-trip scope through DNS. Hostnames prefer IPv4, fall back to non-
// link-local IPv6. Callers skip-and-log on error; partial scan beats none.
func NormalizeIPListEntry(ctx context.Context, raw string, resolver IPListResolver) (string, error) {
	if raw == "" {
		return "", errEmptyTarget
	}
	if strings.Contains(raw, "%") {
		return "", fmt.Errorf("%w: %s", errScopedIPv6, raw)
	}
	if ip := net.ParseIP(raw); ip != nil {
		// Collapse IPv4-mapped IPv6 (::ffff:a.b.c.d) so v4-only checks see v4.
		if v4 := ip.To4(); v4 != nil {
			return v4.String(), nil
		}
		if ip.IsLinkLocalUnicast() {
			return "", fmt.Errorf("%w: %s", errLinkLocalIPv6, raw)
		}
		return ip.String(), nil
	}
	addrs, err := resolver.LookupIPAddr(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", raw, err)
	}
	var ipv4, ipv6 string
	for _, a := range addrs {
		if a.IP.To4() != nil {
			ipv4 = a.IP.String()
			break
		}
		if ipv6 == "" && !a.IP.IsLinkLocalUnicast() {
			ipv6 = a.IP.String()
		}
	}
	if ipv4 != "" {
		return ipv4, nil
	}
	if ipv6 != "" {
		return ipv6, nil
	}
	return "", fmt.Errorf("%w: %s", errHostnameUnresolved, raw)
}

// ParseIPv4 parses an IPv4 literal. IPv4-mapped IPv6 (::ffff:a.b.c.d) is
// accepted via Unmap; pure IPv6 is rejected.
func ParseIPv4(s string) (netip.Addr, error) {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("not a valid IP address: %q", s)
	}
	a = a.Unmap()
	if !a.Is4() {
		return netip.Addr{}, fmt.Errorf("IPv4 required: %q", s)
	}
	return a, nil
}

// Caller must ensure a.Is4(); As4 panics on IPv6.
func IPv4ToUint32(a netip.Addr) uint32 {
	b := a.As4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func Uint32ToIPv4(n uint32) string {
	return netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}).String()
}

// AdjustIPv4RangeStart skips .0 (network) and .1 (gateway) at the start of an
// IPv4 range, except inside 127.0.0.0/8 where dev fixtures bind those slots.
func AdjustIPv4RangeStart(n uint32) uint32 {
	const loopbackMask uint32 = 0xff000000
	const loopbackPrefix uint32 = 0x7f000000
	if n&loopbackMask == loopbackPrefix {
		return n
	}
	switch n & 0xff {
	case 0:
		return n + 2
	case 1:
		return n + 1
	}
	return n
}

// Reports whether addr is the IPv4 .0/.1 of its subnet. Loopback 127/8 is
// exempt because dev fixtures bind .0/.1 there.
func IsIPv4NetworkOrGateway(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.Is4() {
		return false
	}
	n := IPv4ToUint32(addr)
	if n&0xff000000 == 0x7f000000 {
		return false
	}
	last := n & 0xff
	return last == 0 || last == 1
}
