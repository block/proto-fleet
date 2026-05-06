package main

import (
	"net/http"

	"github.com/block/proto-fleet/server/generated/grpc/agentgateway/v1/agentgatewayv1connect"
)

func newGatewayClient(serverURL string) agentgatewayv1connect.AgentGatewayServiceClient {
	return agentgatewayv1connect.NewAgentGatewayServiceClient(http.DefaultClient, serverURL)
}
