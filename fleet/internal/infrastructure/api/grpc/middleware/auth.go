package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"connectrpc.com/authn"
	"github.com/btc-mining/miner-firmware/fleet/internal/domain"
)

func NewAuthMiddleware(ts *domain.TokenService, allowedProcedures []string) *authn.Middleware {

	allowList := make(map[string]struct{})
	for _, item := range allowedProcedures {
		allowList[item] = struct{}{}
	}

	return authn.NewMiddleware(func(_ context.Context, r *http.Request) (any, error) {
		// Infer the procedure from the request URL.
		procedure, _ := authn.InferProcedure(r.URL)
		// Extract the bearer token from the Authorization header.
		token, ok := authn.BearerToken(r)
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
		claims, err := ts.VerifyJWT(token)
		if err != nil {
			slog.Warn("invalid token", slog.String("procedure", procedure))
			return "", authn.Errorf("error validating token: %w", err)
		}
		// The request is authenticated. middle ware will make
		// the UserID available in the context automatically.
		return domain.UserID(claims.UserID), nil
	})
}
