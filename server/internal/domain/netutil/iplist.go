package netutil

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// IPListResolver narrows net.Resolver to the one method NormalizeIPListEntry
// needs, so callers can stub DNS in tests without spinning up a real
// resolver. *net.Resolver satisfies the interface as-is.
type IPListResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Unexported because no caller branches on cause -- both pairing and the
// agent just skip-and-log. The tests use these for clearer assertions.
var (
	errEmptyTarget        = errors.New("empty IP/hostname")
	errScopedIPv6         = errors.New("scoped IPv6 (%zone) is not supported")
	errLinkLocalIPv6      = errors.New("link-local IPv6 requires interface scope")
	errHostnameUnresolved = errors.New("hostname did not resolve to a usable address")
)

// NormalizeIPListEntry returns the canonical IP literal for an entry in
// pairing.v1.IPListModeRequest.ip_addresses. Hostnames are resolved via the
// supplied resolver, preferring IPv4 and falling back to non-link-local
// IPv6. Scoped IPv6 ("%zone") and link-local IPv6 ("fe80::/10") are
// rejected: the TCP stack can't dial them without interface scope, and
// net.IP.String() does not round-trip scope through DNS resolution.
//
// Callers should skip-and-log on error rather than fail the whole command;
// partial scan beats no scan when one entry in a long list is bad.
func NormalizeIPListEntry(ctx context.Context, raw string, resolver IPListResolver) (string, error) {
	if raw == "" {
		return "", errEmptyTarget
	}
	if strings.Contains(raw, "%") {
		return "", fmt.Errorf("%w: %s", errScopedIPv6, raw)
	}
	if ip := net.ParseIP(raw); ip != nil {
		if ip.To4() == nil && ip.IsLinkLocalUnicast() {
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
