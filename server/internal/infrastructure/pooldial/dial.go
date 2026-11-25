// Package pooldial applies request-scoped destination restrictions to pool probes.
package pooldial

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"
)

type publicOnlyKey struct{}

// PublicOnly restricts pool probes in this context to public destination IPs.
// Operator probes and device connections do not opt into this customer policy.
func PublicOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, publicOnlyKey{}, true)
}

var ErrPrivateDestination = errors.New("pool connection tests require a public destination")

func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := newDialer(ctx).DialContext(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("dial pool: %w", err)
	}
	return conn, nil
}

func newDialer(ctx context.Context) *net.Dialer {
	dialer := &net.Dialer{}
	if ctx.Value(publicOnlyKey{}) == true {
		// ControlContext receives the resolved numeric address before connect, for
		// every candidate. No second DNS lookup can change the destination after validation.
		dialer.ControlContext = checkDestination
	}
	return dialer
}

// Additional non-public ranges not covered by IsPrivate/IsGlobalUnicast.
var nonPublicRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("fec0::/10"),
}

func checkDestination(_ context.Context, network, address string, _ syscall.RawConn) error {
	if network != "tcp4" && network != "tcp6" {
		return ErrPrivateDestination
	}
	endpoint, err := netip.ParseAddrPort(address)
	if err != nil {
		return ErrPrivateDestination
	}
	ip := endpoint.Addr().Unmap()
	if ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return ErrPrivateDestination
	}
	for _, prefix := range nonPublicRanges {
		if prefix.Contains(ip) {
			return ErrPrivateDestination
		}
	}
	return nil
}
