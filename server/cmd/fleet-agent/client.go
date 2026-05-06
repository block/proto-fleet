package main

import (
	"net/http"
	"time"

	"github.com/block/proto-fleet/server/generated/grpc/agentgateway/v1/agentgatewayv1connect"
)

const httpClientTimeout = 30 * time.Second

func newGatewayClient(serverURL string) agentgatewayv1connect.AgentGatewayServiceClient {
	return agentgatewayv1connect.NewAgentGatewayServiceClient(
		&http.Client{Timeout: httpClientTimeout},
		serverURL,
	)
}
