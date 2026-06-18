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
	return CtxWithAssignments(t, orgID, OrgAssignment(permissions...))
}

// CtxWithAssignments returns a context carrying session.Info plus the
// supplied effective permission assignments.
func CtxWithAssignments(t *testing.T, orgID int64, assignments ...authz.Assignment) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), &session.Info{OrganizationID: orgID})
	for i := range assignments {
		if assignments[i].AssignmentID == 0 {
			assignments[i].AssignmentID = int64(i + 1)
		}
	}
	eff := authz.NewEffectivePermissions(assignments)
	return middleware.WithEffectivePermissions(ctx, eff)
}

// OrgAssignment builds an org-scoped assignment for handler tests.
func OrgAssignment(permissions ...string) authz.Assignment {
	return authz.Assignment{
		ScopeType:   authz.ScopeOrg,
		Permissions: permissions,
	}
}

// SiteAssignment builds a site-scoped assignment for handler tests.
func SiteAssignment(siteID int64, permissions ...string) authz.Assignment {
	return authz.Assignment{
		ScopeType:   authz.ScopeSite,
		SiteID:      &siteID,
		Permissions: permissions,
	}
}
