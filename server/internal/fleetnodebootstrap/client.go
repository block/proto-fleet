package fleetnodebootstrap

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"

	"golang.org/x/net/http2"

	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
)

// Refusing 3xx stops a downgrade redirect from replaying the signed POST
// body (enrollment token, api_key, signature) to an attacker-chosen target.
var errRedirectNotAllowed = errors.New("redirects are not allowed for connect-rpc calls")

// Timeout is intentionally unset -- it would cap long-lived bidi streams.
// Callers wrap individual RPCs in per-call context deadlines instead.
func newGatewayHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errRedirectNotAllowed
		},
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}
}

func NewGatewayClient(serverURL string) fleetnodegatewayv1connect.FleetNodeGatewayServiceClient {
	return fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(newGatewayHTTPClient(), serverURL)
}
