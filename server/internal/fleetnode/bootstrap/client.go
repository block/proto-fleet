package bootstrap

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/http2"

	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/transportguard"
)

// A shared AllowHTTP+DialTLSContext shim would silently downgrade https to
// plaintext, defeating ValidateServerURL's https-required policy. The https
// branch uses net/http's Transport so ALPN can negotiate H2 when available
// and fall back to HTTP/1.1 — packaged nginx terminates TLS and proxies
// upstream over HTTP/1.1, so a bare http2.Transport refuses to talk to it.
func newGatewayHTTPClient(serverURL string) (*http.Client, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("parse server-url: %w", err)
	}
	var tr http.RoundTripper
	switch u.Scheme {
	case "http":
		tr = &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		}
	case "https":
		tr = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
		}
	default:
		return nil, fmt.Errorf("server-url scheme must be http or https; got %q", u.Scheme)
	}
	return &http.Client{
		CheckRedirect: transportguard.RejectRedirect,
		Transport:     tr,
	}, nil
}

func NewGatewayClient(serverURL string) (fleetnodegatewayv1connect.FleetNodeGatewayServiceClient, error) {
	httpClient, err := newGatewayHTTPClient(serverURL)
	if err != nil {
		return nil, err
	}
	return fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(httpClient, serverURL), nil
}
