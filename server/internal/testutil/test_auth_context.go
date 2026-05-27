package testutil

import (
	"context"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// MockAuthContextForTesting creates a context with session info for testing.
// This sets up the session-based authentication context expected by domain services.
// EffectivePermissions is populated with the full catalog at org scope so unit
// tests that exercise handler logic clear every RequirePermission gate without
// each test having to opt in.
func MockAuthContextForTesting(ctx context.Context, userID, orgID int64) context.Context {
	info := &session.Info{
		SessionID:      "test-session-id",
		UserID:         userID,
		OrganizationID: orgID,
	}
	return middleware.WithEffectivePermissions(authn.SetInfo(ctx, info), allCatalogEffective())
}

// MockAuthContextWithSessionID creates a context with a custom session ID for testing.
// Use this when testing session-specific behavior like stream deduplication.
func MockAuthContextWithSessionID(ctx context.Context, sessionID string, userID, orgID int64) context.Context {
	info := &session.Info{
		SessionID:      sessionID,
		UserID:         userID,
		OrganizationID: orgID,
	}
	return middleware.WithEffectivePermissions(authn.SetInfo(ctx, info), allCatalogEffective())
}

// allCatalogEffective returns an org-scope EffectivePermissions that
// grants every catalog key — the SUPER_ADMIN equivalent for unit
// tests that don't want to declare a permission set per call site.
func allCatalogEffective() *authz.EffectivePermissions {
	return authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  authz.AllPermissions(),
	}})
}
