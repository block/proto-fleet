package agentbootstrap

import (
	"errors"
	"net/http"
	"time"

	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
)

const httpClientTimeout = 30 * time.Second

// Refusing every 30x stops a downgrade redirect from replaying the POST body
// (enrollment token, api_key, signature) to a plaintext target; Connect-RPC
// itself never expects redirects.
var errRedirectNotAllowed = errors.New("redirects are not allowed for connect-rpc calls")

func newGatewayHTTPClient() *http.Client {
	return &http.Client{
		Timeout: httpClientTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errRedirectNotAllowed
		},
	}
}

func NewGatewayClient(serverURL string) fleetnodegatewayv1connect.FleetNodeGatewayServiceClient {
	return fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(newGatewayHTTPClient(), serverURL)
}
