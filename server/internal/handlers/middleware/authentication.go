package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/btc-mining/proto-fleet/server/internal/domain/token"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/server"

	"connectrpc.com/authn"
)

type AuthMiddleware struct {
	auth *authn.Middleware
}

func (c AuthMiddleware) Wrap(handler http.Handler) http.Handler {
	return c.auth.Wrap(handler)
}

var _ server.Middleware = AuthMiddleware{}

func NewAuthMiddleware(ts *token.Service, allowedProcedures []string) *AuthMiddleware {
	allowList := make(map[string]struct{})
	for _, item := range allowedProcedures {
		allowList[item] = struct{}{}
	}

	middleware := authn.NewMiddleware(func(_ context.Context, r *http.Request) (any, error) {
		// Infer the procedure from the request URL.
		procedure, _ := authn.InferProcedure(r.URL)
		// Extract the bearer bearerToken from the Authorization header.
		bearerToken, ok := authn.BearerToken(r)
		slog.Debug("bearer token", slog.String("token", bearerToken))
		if !ok {
			// We'll allow unauthenticated access to the ping procedure.
			if _, ok := allowList[procedure]; ok {
				return "", nil // no authentication required
			}
			slog.Warn("authentication required", slog.String("procedure", procedure))
			err := authn.Errorf("invalid authorization")
			err.Meta().Set("WWW-Authenticate", "Bearer")
			return "", err
		}
		claims, err := ts.VerifyJWT(bearerToken)
		if err != nil {
			slog.Warn("invalid bearerToken", slog.String("procedure", procedure))
			return "", authn.Errorf("error validating bearerToken: %w", err)
		}
		// The request is authenticated. middle ware will make
		// the UserID available in the context automatically.
		return *claims, nil
	})

	return &AuthMiddleware{auth: middleware}
}
