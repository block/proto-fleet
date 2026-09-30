package firmware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// RequestAuthenticator supplies the same identity and current permissions used
// by Connect RPCs. The production implementation is the shared AuthInterceptor.
type RequestAuthenticator interface {
	AuthenticateRequest(ctx context.Context, headers http.Header) (context.Context, error)
}

func requireReadPermission(w http.ResponseWriter, r *http.Request, authenticator RequestAuthenticator, operation string) (context.Context, bool) {
	return requirePermission(w, r, authenticator, []string{authz.PermFleetRead, authz.PermMinerFirmwareUpdate}, operation)
}

func requireMutationPermission(w http.ResponseWriter, r *http.Request, authenticator RequestAuthenticator, operation string) (context.Context, bool) {
	return requirePermission(w, r, authenticator, []string{authz.PermMinerFirmwareUpdate}, operation)
}

func requirePermission(w http.ResponseWriter, r *http.Request, authenticator RequestAuthenticator, permissions []string, operation string) (context.Context, bool) {
	ctx, err := authorize(r, authenticator, permissions)
	if err == nil {
		return ctx, true
	}
	switch {
	case fleeterror.IsAuthenticationError(err):
		slog.Warn("firmware authentication failed", "operation", operation, "error", err)
		writeError(w, http.StatusUnauthorized, "authentication required")
	case fleeterror.IsForbiddenError(err):
		slog.Warn("firmware authorization denied", "operation", operation)
		writeError(w, http.StatusForbidden, "permission denied")
	default:
		slog.Error("firmware authorization failed", "operation", operation, "error", err)
		writeError(w, http.StatusInternalServerError, "authorization failed")
	}
	return r.Context(), false
}

func authorize(r *http.Request, authenticator RequestAuthenticator, permissions []string) (context.Context, error) {
	if authenticator == nil {
		return r.Context(), fleeterror.NewInternalError("auth: authenticator not wired into firmware handler")
	}
	ctx, err := authenticator.AuthenticateRequest(r.Context(), r.Header)
	if err != nil {
		return r.Context(), err
	}
	if _, err := middleware.RequireAnyPermission(ctx, permissions, authz.ResourceContext{}); err != nil {
		return r.Context(), err
	}
	return ctx, nil
}
