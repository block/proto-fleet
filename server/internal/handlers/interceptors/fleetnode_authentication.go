package interceptors

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
)

type FleetNodeAuthInterceptor struct {
	auth         *auth.Service
	procedureSet map[string]struct{}
}

var _ connect.Interceptor = &FleetNodeAuthInterceptor{}

func NewFleetNodeAuthInterceptor(auth *auth.Service, procedures []string) *FleetNodeAuthInterceptor {
	set := make(map[string]struct{}, len(procedures))
	for _, p := range procedures {
		set[p] = struct{}{}
	}
	return &FleetNodeAuthInterceptor{auth: auth, procedureSet: set}
}

func (i *FleetNodeAuthInterceptor) appliesTo(procedure string) bool {
	_, ok := i.procedureSet[procedure]
	return ok
}

func (i *FleetNodeAuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if !i.appliesTo(req.Spec().Procedure) {
			return next(ctx, req)
		}
		newCtx, _, err := i.authenticate(ctx, req.Header().Get("Authorization"))
		if err != nil {
			return nil, err
		}
		return next(newCtx, req)
	}
}

func (i *FleetNodeAuthInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *FleetNodeAuthInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if !i.appliesTo(conn.Spec().Procedure) {
			return next(ctx, conn)
		}
		newCtx, expiresAt, err := i.authenticate(ctx, conn.RequestHeader().Get("Authorization"))
		if err != nil {
			return err
		}
		sessionCtx, cancel := context.WithDeadline(newCtx, expiresAt)
		defer cancel()
		return next(sessionCtx, conn)
	}
}

func (i *FleetNodeAuthInterceptor) authenticate(ctx context.Context, authHeader string) (context.Context, time.Time, error) {
	rawToken, ok := parseBearerToken(authHeader)
	if !ok {
		return ctx, time.Time{}, fleeterror.NewUnauthenticatedError("invalid Authorization header format, expected: Bearer <session_token>")
	}
	resolved, err := i.auth.ResolveSession(ctx, rawToken)
	if err != nil {
		var fe fleeterror.FleetError
		if errors.As(err, &fe) {
			return ctx, time.Time{}, err
		}
		slog.Error("fleet node auth: session lookup failed", "error", err)
		return ctx, time.Time{}, fleeterror.NewInternalError("fleet node authentication failed")
	}
	return authn.SetInfo(ctx, &auth.Subject{
		FleetNodeID:         resolved.FleetNodeID,
		OrgID:               resolved.OrgID,
		Name:                resolved.Name,
		IdentityFingerprint: enrollment.IdentityFingerprint(resolved.IdentityPubkey),
		SessionFingerprint:  resolved.SessionFingerprint,
	}), resolved.SessionExpiresAt, nil
}
