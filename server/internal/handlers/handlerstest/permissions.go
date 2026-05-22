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
