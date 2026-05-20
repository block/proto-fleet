package fleetnodebootstrap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"golang.org/x/net/http2"

	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
)

// Refusing 3xx stops a downgrade redirect from replaying the signed POST
// body (enrollment token, api_key, signature) to an attacker-chosen target.
var errRedirectNotAllowed = errors.New("redirects are not allowed for connect-rpc calls")

// A shared AllowHTTP+DialTLSContext shim would silently downgrade https to
// plaintext, defeating ValidateServerURL's https-required policy.
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
		tr = &http2.Transport{}
	default:
		return nil, fmt.Errorf("server-url scheme must be http or https; got %q", u.Scheme)
	}
	return &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errRedirectNotAllowed
		},
		Transport: tr,
	}, nil
}

func NewGatewayClient(serverURL string) (fleetnodegatewayv1connect.FleetNodeGatewayServiceClient, error) {
	httpClient, err := newGatewayHTTPClient(serverURL)
	if err != nil {
		return nil, err
	}
	return fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(httpClient, serverURL), nil
}
