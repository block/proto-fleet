package middleware

import (
	"context"

	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// RequireAdmin returns the authenticated session info or a Connect
// error. Both ADMIN and SUPER_ADMIN can write; both can read. Handlers
// that previously duplicated this check call into here so the gate
// stays consistent across services. The action verb is embedded in the
// Forbidden error message ("only admins can <action>").
func RequireAdmin(ctx context.Context, action string) (*session.Info, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}
	if info.Role != domainAuth.SuperAdminRoleName && info.Role != domainAuth.AdminRoleName {
		return nil, fleeterror.NewForbiddenErrorf("only admins can %s", action)
	}
	return info, nil
}

// RequireSuperAdmin returns the authenticated session info only for
// organization-wide override actions.
func RequireSuperAdmin(ctx context.Context, action string) (*session.Info, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}
	if info.Role != domainAuth.SuperAdminRoleName {
		return nil, fleeterror.NewForbiddenErrorf("only super admins can %s", action)
	}
	return info, nil
}
