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

// Refusing every 30x stops a downgrade redirect from replaying the POST body
// (enrollment token, api_key, signature) to a plaintext target; Connect-RPC
// itself never expects redirects.
var errRedirectNotAllowed = errors.New("redirects are not allowed for connect-rpc calls")

// newGatewayHTTPClient returns a client speaking HTTP/2. The gateway's
// ControlStream is a bidi RPC, which the standard HTTP/1.1 transport can't
// carry. The h2c (plaintext HTTP/2) pattern below mirrors what the server's
// h2c.NewHandler accepts; production HTTPS support is a follow-up. Timeout
// is omitted on purpose -- it would cap long-lived streams; callers wrap
// individual RPCs in per-call context deadlines.
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
