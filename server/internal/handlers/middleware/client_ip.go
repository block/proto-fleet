package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type clientIPKey struct{}

// ClientIP is resolved at the HTTP boundary, never from caller-controlled headers alone.
func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(string)
	return ip
}

type ClientIPMiddleware struct{ trusted []netip.Prefix }

// NewClientIPMiddleware accepts comma-separated proxy CIDRs. Empty trusts no proxies.
func NewClientIPMiddleware(cidrs string) (*ClientIPMiddleware, error) {
	m := &ClientIPMiddleware{}
	if cidrs == "" {
		return m, nil
	}
	for _, value := range strings.Split(cidrs, ",") {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("HTTP_TRUSTED_PROXY_CIDRS: %w", err)
		}
		m.trusted = append(m.trusted, prefix)
	}
	return m, nil
}

func (m *ClientIPMiddleware) trusts(ip netip.Addr) bool {
	for _, prefix := range m.trusted {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func (m *ClientIPMiddleware) resolve(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	peer = peer.Unmap()
	if !m.trusts(peer) {
		return peer.String()
	}
	forwarded := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	if forwarded == "" {
		return peer.String()
	}
	var chain []netip.Addr
	for _, value := range strings.Split(forwarded, ",") {
		ip, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil || ip.Zone() != "" {
			return peer.String()
		}
		chain = append(chain, ip.Unmap())
	}
	client := peer
	for i := len(chain) - 1; i >= 0 && m.trusts(client); i-- {
		client = chain[i]
	}
	return client.String()
}

func (m *ClientIPMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, m.resolve(r))))
	})
}
