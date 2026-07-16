package chat

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

const providerRequestTimeout = 90 * time.Second

type providerEgressPolicy struct {
	allowPrivate bool
}

func providerEgressPolicyFor(provider Provider) providerEgressPolicy {
	return providerEgressPolicy{allowPrivate: provider == ProviderOllama}
}

// newProviderHTTPClient validates the address that is actually dialed so a
// hostname cannot pass configuration validation and later rebind to an
// internal service. Redirects and environment proxies are disabled because
// either would move resolution outside this transport's destination policy.
func newProviderHTTPClient(policy providerEgressPolicy) *http.Client {
	dialer := &net.Dialer{Timeout: providerRequestTimeout}
	transport, _ := http.DefaultTransport.(*http.Transport)
	transport = transport.Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("split provider destination: %w", err)
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve provider destination: %w", err)
		}
		lastErr := errors.New("provider destination has no dialable address")
		for _, ip := range ips {
			if !providerIPAllowed(policy, ip) {
				lastErr = errors.New("provider destination resolves to a disallowed internal address")
				continue
			}
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr != nil {
				lastErr = dialErr
				continue
			}
			return conn, nil
		}
		return nil, lastErr
	}

	return &http.Client{
		Timeout:   providerRequestTimeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func providerIPAllowed(policy providerEgressPolicy, ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || providerReservedIP(ip) {
		return false
	}
	if !policy.allowPrivate && (ip.IsLoopback() || ip.IsPrivate()) {
		return false
	}
	return true
}

// Ranges not classified as private by net.IP but unsuitable for provider
// egress: CGNAT, benchmarking, and reserved future-use IPv4 space.
var providerReservedCIDRs = parseProviderCIDRs("100.64.0.0/10", "198.18.0.0/15", "240.0.0.0/4")

func parseProviderCIDRs(specs ...string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(specs))
	for _, spec := range specs {
		_, network, err := net.ParseCIDR(spec)
		if err != nil {
			panic(err)
		}
		networks = append(networks, network)
	}
	return networks
}

func providerReservedIP(ip net.IP) bool {
	for _, network := range providerReservedCIDRs {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
