package firmware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// effectivePermissionResolver is the request-scoped subset of the authz
// resolver used by firmware's raw HTTP handlers.
type effectivePermissionResolver interface {
	LoadEffective(ctx context.Context, userID, organizationID int64) (*authz.EffectivePermissions, error)
}

// authorizeMutation applies the same effective-permission lookup and canonical
// permission gate used by Connect handlers. Firmware routes are registered
// directly on net/http, so they do not pass through the Connect auth
// interceptor that normally populates this context.
func authorizeMutation(
	r *http.Request,
	sessionService *session.Service,
	userStore interfaces.UserStore,
	permissionResolver effectivePermissionResolver,
) (context.Context, error) {
	ctx, err := authenticate(r, sessionService, userStore)
	if err != nil {
		return r.Context(), err
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		return r.Context(), fleeterror.NewUnauthenticatedError("authentication required")
	}
	if permissionResolver == nil {
		return r.Context(), fleeterror.NewInternalError(
			"authz: permission resolver not wired into firmware handler",
		)
	}

	effectivePermissions, err := permissionResolver.LoadEffective(ctx, info.UserID, info.OrganizationID)
	if err != nil {
		return r.Context(), fleeterror.NewInternalErrorf(
			"authz: effective permissions lookup failed: %v",
			err,
		)
	}

	ctx = middleware.WithEffectivePermissions(ctx, effectivePermissions)
	if _, err := middleware.RequirePermission(
		ctx,
		authz.PermMinerFirmwareUpdate,
		authz.ResourceContext{},
	); err != nil {
		return r.Context(), err
	}
	return ctx, nil
}

func requireMutationPermission(
	w http.ResponseWriter,
	r *http.Request,
	sessionService *session.Service,
	userStore interfaces.UserStore,
	permissionResolver effectivePermissionResolver,
	operation string,
) (context.Context, bool) {
	ctx, err := authorizeMutation(r, sessionService, userStore, permissionResolver)
	if err == nil {
		return ctx, true
	}

	switch {
	case fleeterror.IsAuthenticationError(err):
		slog.Warn("firmware mutation authentication failed", "operation", operation, "error", err)
		writeError(w, http.StatusUnauthorized, "authentication required")
	case fleeterror.IsForbiddenError(err):
		slog.Warn("firmware mutation authorization denied", "operation", operation)
		writeError(w, http.StatusForbidden, "permission denied")
	default:
		slog.Error("firmware mutation authorization failed", "operation", operation, "error", err)
		writeError(w, http.StatusInternalServerError, "authorization failed")
	}
	return r.Context(), false
}
