package middleware

import (
	"context"
	"github.com/btc-mining/miner-firmware/fleet/internal/domain/auth"
	"github.com/btc-mining/miner-firmware/fleet/internal/domain/token"
	"log/slog"
	"net/http"

	"connectrpc.com/authn"
)

func NewAuthMiddleware(ts *token.Service, allowedProcedures []string) *authn.Middleware {

	allowList := make(map[string]struct{})
	for _, item := range allowedProcedures {
		allowList[item] = struct{}{}
	}

	return authn.NewMiddleware(func(_ context.Context, r *http.Request) (any, error) {
		// Infer the procedure from the request URL.
		procedure, _ := authn.InferProcedure(r.URL)
		// Extract the bearer bearerToken from the Authorization header.
		bearerToken, ok := authn.BearerToken(r)
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
		return auth.UserID(claims.UserID), nil
	})
}
