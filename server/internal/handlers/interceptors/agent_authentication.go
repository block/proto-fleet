package interceptors

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/authn"
	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/internal/domain/agentauth"
	"github.com/block/proto-fleet/server/internal/domain/agentenrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// AgentAuthInterceptor authenticates agent traffic using an Authorization
// bearer session_token issued by CompleteAuthHandshake. It only fires on
// procedures registered in AgentAuthenticatedProcedures; other procedures
// pass through.
type AgentAuthInterceptor struct {
	auth         *agentauth.Service
	procedureSet map[string]struct{}
}

var _ connect.Interceptor = &AgentAuthInterceptor{}

func NewAgentAuthInterceptor(auth *agentauth.Service, procedures []string) *AgentAuthInterceptor {
	set := make(map[string]struct{}, len(procedures))
	for _, p := range procedures {
		set[p] = struct{}{}
	}
	return &AgentAuthInterceptor{auth: auth, procedureSet: set}
}

func (i *AgentAuthInterceptor) appliesTo(procedure string) bool {
	_, ok := i.procedureSet[procedure]
	return ok
}

func (i *AgentAuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if !i.appliesTo(req.Spec().Procedure) {
			return next(ctx, req)
		}
		newCtx, err := i.authenticate(ctx, req.Header().Get("Authorization"))
		if err != nil {
			return nil, err
		}
		return next(newCtx, req)
	}
}

func (i *AgentAuthInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *AgentAuthInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if !i.appliesTo(conn.Spec().Procedure) {
			return next(ctx, conn)
		}
		newCtx, err := i.authenticate(ctx, conn.RequestHeader().Get("Authorization"))
		if err != nil {
			return err
		}
		return next(newCtx, conn)
	}
}

func (i *AgentAuthInterceptor) authenticate(ctx context.Context, authHeader string) (context.Context, error) {
	rawToken, ok := parseBearerToken(authHeader)
	if !ok {
		return ctx, fleeterror.NewUnauthenticatedError("invalid Authorization header format, expected: Bearer <session_token>")
	}
	resolved, err := i.auth.ResolveSession(ctx, rawToken)
	if err != nil {
		var fe fleeterror.FleetError
		if errors.As(err, &fe) {
			return ctx, err
		}
		slog.Error("agent auth: session lookup failed", "error", err)
		return ctx, fleeterror.NewInternalError("agent authentication failed")
	}
	return authn.SetInfo(ctx, &agentauth.Subject{
		AgentID:             resolved.AgentID,
		OrgID:               resolved.OrgID,
		Name:                resolved.Name,
		IdentityFingerprint: agentenrollment.IdentityFingerprint(resolved.IdentityPubkey),
	}), nil
}
