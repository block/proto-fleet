// Package handlerstest provides shared helpers for handler-layer
// unit tests across Connect-RPC packages. Helpers here build the
// minimum context any RequirePermission gate needs to evaluate
// without standing up the full auth interceptor pipeline.
package handlerstest

import (
	"context"
	"testing"

	"connectrpc.com/authn"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// CtxWithPermissions returns a context carrying session.Info for the
// supplied organization plus an org-scope EffectivePermissions
// assignment with the given permission keys. Use it from handler unit
// tests to satisfy middleware.RequirePermission without wiring the
// resolver against a real database.
//
// Caller identity fields (UserID, Username, ExternalUserID) are left
// zero — tests that need them should layer additional wiring on top.
func CtxWithPermissions(t *testing.T, orgID int64, permissions ...string) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), &session.Info{OrganizationID: orgID})
	eff := authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  permissions,
	}})
	return middleware.WithEffectivePermissions(ctx, eff)
}

// CtxWithSiteScopedPermissions is CtxWithPermissions with the grant
// attached to a single site-scope assignment instead of an org-scope
// one. Use it to exercise the *Anywhere gates from the perspective of
// a caller (e.g. a site-scoped FIELD_TECH) who holds no org-scope
// assignment at all.
func CtxWithSiteScopedPermissions(t *testing.T, orgID, siteID int64, permissions ...string) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), &session.Info{OrganizationID: orgID})
	eff := authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeSite,
		SiteID:       &siteID,
		Permissions:  permissions,
	}})
	return middleware.WithEffectivePermissions(ctx, eff)
}

// CtxWithSessionInfo carries a caller-supplied session.Info (tests
// that assert on identity fields like UserID/Username populate them
// here) plus the given assignments. Handlers that stamp authorship
// from the session need this richer variant.
func CtxWithSessionInfo(t *testing.T, info *session.Info, assignments ...authz.Assignment) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), info)
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions(assignments))
}
