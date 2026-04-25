package sv2

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// DefaultTCPDialTimeout is the fallback timeout for TCPDial when callers
// pass a zero duration. Matches the SV1 ValidatePool default behavior
// (the SV1 authenticator has its own internal timeout; we ceiling at
// this duration to keep ValidatePool responsive).
const DefaultTCPDialTimeout = 10 * time.Second

// dialTCP opens and closes a TCP connection to host:port; returns nil
// when the handshake succeeded. Shared helper used by TCPDial (once it
// has peeled the URL scheme off) and by HealthMonitor (which is
// configured with a raw host:port).
func dialTCP(ctx context.Context, addr string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultTCPDialTimeout
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("tcp dial %s: %w", addr, err)
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("tcp close %s: %w", addr, err)
	}
	return nil
}

// TCPDial verifies that a stratum URL's host:port is reachable by opening
// a TCP connection and closing it. Accepts the SV1 scheme
// (stratum+tcp://...) and the SV2 scheme (stratum2+tcp://...);
// anything else is rejected as a malformed
// input error rather than a reachability failure.
//
// Returns (true, nil) when the TCP handshake completed. Returns
// (false, err) for every other outcome: unreachable host, filtered port,
// DNS failure, or malformed URL. Callers that want to distinguish those
// should inspect err; SV2 ValidatePool collapses them all into
// reachable=false because the distinction doesn't inform the operator's
// next action (check the URL, check the firewall — same two answers).
//
// This probe does NOT speak the Noise handshake or SetupConnection —
// callers surface the shallowness via ValidationMode_SV2_TCP_DIAL so
// the UI renders "reachable but credentials unverified" rather than
// pretending to have authenticated. A deeper handshake probe is
// available via HandshakeProbe when the operator supplies the pool's
// Noise authority key.
func TCPDial(ctx context.Context, stratumURL string, timeout time.Duration) (bool, error) {
	addr, err := addressFromStratumURL(stratumURL)
	if err != nil {
		return false, err
	}
	if err := dialTCP(ctx, addr, timeout); err != nil {
		return false, err
	}
	return true, nil
}

// addressFromStratumURL extracts host:port from a stratum+(tcp|ssl|ws)
// or sv2+(tcp|ssl) URL. net/url tolerates the "scheme" part but then
// requires us to default the port if the operator omitted it; stratum
// servers have no canonical default, so we reject URLs without an
// explicit port rather than guess.
func addressFromStratumURL(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("stratum URL is empty")
	}
	if !isSupportedScheme(raw) {
		return "", fmt.Errorf("unsupported stratum URL scheme: %q", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parsing stratum URL %q: %w", raw, err)
	}
	host := u.Hostname()
	port := u.Port()
	if host == "" {
		return "", fmt.Errorf("stratum URL %q has no host", raw)
	}
	if port == "" {
		return "", fmt.Errorf("stratum URL %q requires an explicit port", raw)
	}
	return net.JoinHostPort(host, port), nil
}

func isSupportedScheme(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.HasPrefix(lower, "stratum+tcp://") ||
		strings.HasPrefix(lower, "stratum2+tcp://")
}
