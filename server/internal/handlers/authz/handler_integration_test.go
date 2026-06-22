package authz_test

import (
	"context"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/authz/v1"
	authzDomain "github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/authz"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// TestHandler_ListRoles_AcceptsUserManageOnlyCaller is the regression
// for the AddTeamMemberModal P1: the built-in ADMIN role holds
// user:manage but intentionally lacks role:manage, so the modal could
// open but never load the assignable-role list. The handler must
// accept either gate; this exercises the user:manage-only path.
func TestHandler_ListRoles_AcceptsUserManageOnlyCaller(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	// Reconcile seeds the built-in roles so ListRoles returns a
	// non-empty payload. The caller's permission set is independent of
	// any assignment in the DB — the in-memory EffectivePermissions
	// below is what the gate consults.
	_, err := db.ExecContext(ctx,
		`INSERT INTO organization (org_id, name) VALUES ($1, $2) RETURNING id`,
		"listroles-gate-org", "ListRoles Gate Org",
	)
	require.NoError(t, err)
	var orgID int64
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT id FROM organization WHERE org_id = $1`, "listroles-gate-org").Scan(&orgID))
	require.NoError(t, authzDomain.Reconcile(ctx, db))

	svc := authzDomain.NewService(db)
	handler := authz.NewHandler(svc)

	resp, err := handler.ListRoles(
		ctxWithEffectivePerms(orgID, authzDomain.PermUserManage),
		connect.NewRequest(&pb.ListRolesRequest{}),
	)
	require.NoError(t, err, "ADMIN-equivalent caller (user:manage, no role:manage) must read the role list")
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Msg.Roles, "seeded built-in roles should be present")
}

// TestHandler_ListRoles_DeniesWhenNeitherKeyHeld confirms the gate
// still refuses callers who hold neither role:manage nor user:manage.
func TestHandler_ListRoles_DeniesWhenNeitherKeyHeld(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	_, err := db.ExecContext(ctx,
		`INSERT INTO organization (org_id, name) VALUES ($1, $2)`,
		"listroles-deny-org", "ListRoles Deny Org",
	)
	require.NoError(t, err)
	var orgID int64
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT id FROM organization WHERE org_id = $1`, "listroles-deny-org").Scan(&orgID))
	require.NoError(t, authzDomain.Reconcile(ctx, db))

	svc := authzDomain.NewService(db)
	handler := authz.NewHandler(svc)

	_, err = handler.ListRoles(
		ctxWithEffectivePerms(orgID, authzDomain.PermFleetRead),
		connect.NewRequest(&pb.ListRolesRequest{}),
	)
	require.Error(t, err)
}

// ctxWithEffectivePerms mirrors the pattern in handlerstest.CtxWithPermissions
// but is inlined here to avoid pulling that helper into a context (t.Context)
// the helper doesn't currently parameterize, and to keep the test self-contained.
func ctxWithEffectivePerms(orgID int64, perms ...string) context.Context {
	ctx := authn.SetInfo(context.Background(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: orgID,
		UserID:         1,
	})
	return middleware.WithEffectivePermissions(ctx, authzDomain.NewEffectivePermissions([]authzDomain.Assignment{{
		AssignmentID: 1,
		ScopeType:    authzDomain.ScopeOrg,
		Permissions:  perms,
	}}))
}
