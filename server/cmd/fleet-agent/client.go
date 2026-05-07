package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/block/proto-fleet/server/generated/grpc/agentgateway/v1/agentgatewayv1connect"
)

const httpClientTimeout = 30 * time.Second

// errRedirectNotAllowed is returned by CheckRedirect to refuse any 30x
// response from the server. Connect-RPC does not use redirects; a 307/308
// would otherwise replay the POST body (containing the enrollment token,
// api_key, or signature) to the redirect target, defeating the
// non-loopback https requirement on a downgrade redirect.
var errRedirectNotAllowed = errors.New("redirects are not allowed for connect-rpc calls")

func newGatewayHTTPClient() *http.Client {
	return &http.Client{
		Timeout: httpClientTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errRedirectNotAllowed
		},
	}
}

func newGatewayClient(serverURL string) agentgatewayv1connect.AgentGatewayServiceClient {
	return agentgatewayv1connect.NewAgentGatewayServiceClient(newGatewayHTTPClient(), serverURL)
}
