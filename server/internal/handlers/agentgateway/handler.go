package agentgateway

import (
	"github.com/block/proto-fleet/server/generated/grpc/agentgateway/v1/agentgatewayv1connect"
)

type Handler struct {
	agentgatewayv1connect.UnimplementedAgentGatewayServiceHandler
}

var _ agentgatewayv1connect.AgentGatewayServiceHandler = &Handler{}

func NewHandler() *Handler {
	return &Handler{}
}
