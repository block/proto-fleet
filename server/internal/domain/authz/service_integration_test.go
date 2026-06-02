package authz_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// setupOrgWithSuperAdmin returns (orgID, superAdminUserID). The user is
// assigned the org's seeded SUPER_ADMIN role so the Service treats them
// as fully privileged for the privilege-parity check.
func setupOrgWithSuperAdmin(t *testing.T, db *sql.DB) (int64, int64) {
	t.Helper()
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))
	userID := insertTestUser(t, db)
	roleID := getBuiltinRoleID(t, db, orgID, "SUPER_ADMIN")

	q := sqlc.New(db)
	_, err := q.AssignRole(ctx, sqlc.AssignRoleParams{
		UserID:         userID,
		OrganizationID: orgID,
		RoleID:         roleID,
		ScopeType:      "org",
		ScopeID:        sql.NullInt64{},
	})
	require.NoError(t, err)
	return orgID, userID
}

func TestService_CreateCustomRole_Succeeds(t *testing.T) {
	db := testutil.GetTestDB(t)
	orgID, userID := setupOrgWithSuperAdmin(t, db)
	svc := authz.NewService(db)

	view, err := svc.CreateCustomRole(t.Context(), userID, orgID,
		"Floor Manager", "  trim me  ",
		[]string{authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerReboot},
	)
	require.NoError(t, err)
	require.Equal(t, "Floor Manager", view.Name)
	require.Equal(t, "trim me", view.Description, "description should be trimmed before persist")
	require.False(t, view.Builtin)
	require.Equal(t, int32(0), view.MemberCount)
	require.ElementsMatch(t,
		[]string{authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerReboot},
		view.PermissionKeys,
	)
}

func TestService_CreateCustomRole_PrivilegeParityRejectsBeyondCaller(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))
	callerID := insertTestUser(t, db)

	// Caller holds role:manage (so the role:manage gate passes) plus the
	// read keys required by the read-pairing validator, but explicitly NOT
	// miner:reboot — so the per-permission parity check is what rejects.
	callerRoleID := createCustomRoleWithPermissions(t, db, orgID, "Parity Test Caller",
		[]string{authz.PermRoleManage, authz.PermFleetRead, authz.PermMinerRead},
	)
	q := sqlc.New(db)
	_, err := q.AssignRole(ctx, sqlc.AssignRoleParams{
		UserID:         callerID,
		OrganizationID: orgID,
		RoleID:         callerRoleID,
		ScopeType:      "org",
		ScopeID:        sql.NullInt64{},
	})
	require.NoError(t, err)

	svc := authz.NewService(db)
	_, err = svc.CreateCustomRole(ctx, callerID, orgID,
		"Reboot Plus", "",
		[]string{authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerReboot},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), authz.PermMinerReboot)
	require.Contains(t, err.Error(), "does not hold")
}

func TestService_CreateCustomRole_DeactivatedCallerCannotPersistGrants(t *testing.T) {
	// Codex MED-1 regression: a soft-deleted caller's user_organization_role
	// rows must not surface in the in-tx LoadEffectiveTx, so the parity
	// check denies even if the request slipped through the auth gate.
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID, userID := setupOrgWithSuperAdmin(t, db)

	_, err := db.ExecContext(ctx,
		`UPDATE "user" SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, userID,
	)
	require.NoError(t, err)

	svc := authz.NewService(db)
	_, err = svc.CreateCustomRole(ctx, userID, orgID,
		"Should Fail", "",
		[]string{authz.PermFleetRead},
	)
	require.Error(t, err, "deactivated caller must not persist grants")
	require.Contains(t, err.Error(), "does not hold")
}

func TestService_UpdateCustomRole_RejectsBuiltins(t *testing.T) {
	db := testutil.GetTestDB(t)
	orgID, userID := setupOrgWithSuperAdmin(t, db)
	svc := authz.NewService(db)

	for _, key := range []string{"SUPER_ADMIN", "ADMIN", "FIELD_TECH"} {
		builtinRoleID := getBuiltinRoleID(t, db, orgID, key)
		_, err := svc.UpdateCustomRole(t.Context(), userID, orgID, builtinRoleID,
			"Renamed", "",
			[]string{authz.PermFleetRead},
		)
		require.Error(t, err, "built-in %s must reject update", key)
		require.Contains(t, err.Error(), "built-in roles cannot be modified")
	}
}

func TestService_DeleteCustomRole_RejectsBuiltins(t *testing.T) {
	db := testutil.GetTestDB(t)
	orgID, callerID := setupOrgWithSuperAdmin(t, db)
	svc := authz.NewService(db)

	for _, key := range []string{"SUPER_ADMIN", "ADMIN", "FIELD_TECH"} {
		builtinRoleID := getBuiltinRoleID(t, db, orgID, key)
		err := svc.DeleteCustomRole(t.Context(), callerID, orgID, builtinRoleID)
		require.Error(t, err, "built-in %s must reject delete", key)
		require.Contains(t, err.Error(), "built-in roles cannot be deleted")
	}
}

func TestService_DeleteCustomRole_RejectsRoleWithActiveAssignments(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID, callerID := setupOrgWithSuperAdmin(t, db)
	svc := authz.NewService(db)

	view, err := svc.CreateCustomRole(ctx, callerID, orgID,
		"Operator", "",
		[]string{authz.PermFleetRead, authz.PermMinerRead},
	)
	require.NoError(t, err)

	// Give the role to another user so the count is > 0.
	otherUserID := insertTestUser(t, db)
	q := sqlc.New(db)
	_, err = q.AssignRole(ctx, sqlc.AssignRoleParams{
		UserID:         otherUserID,
		OrganizationID: orgID,
		RoleID:         view.ID,
		ScopeType:      "org",
		ScopeID:        sql.NullInt64{},
	})
	require.NoError(t, err)

	err = svc.DeleteCustomRole(ctx, callerID, orgID, view.ID)
	require.Error(t, err, "delete must refuse while assignments exist")
	require.Contains(t, err.Error(), "active assignment")
}

// TestService_DeleteCustomRole_DeactivatedAssigneeDoesNotBlockDeletion
// covers the live-user filter on CountActiveAssignmentsForRole — a
// role only assigned to soft-deleted users is effectively unassigned,
// so the admin can clean it up.
func TestService_DeleteCustomRole_DeactivatedAssigneeDoesNotBlockDeletion(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID, callerID := setupOrgWithSuperAdmin(t, db)
	svc := authz.NewService(db)

	view, err := svc.CreateCustomRole(ctx, callerID, orgID,
		"Old Operator", "",
		[]string{authz.PermFleetRead, authz.PermMinerRead},
	)
	require.NoError(t, err)

	otherUserID := insertTestUser(t, db)
	q := sqlc.New(db)
	_, err = q.AssignRole(ctx, sqlc.AssignRoleParams{
		UserID:         otherUserID,
		OrganizationID: orgID,
		RoleID:         view.ID,
		ScopeType:      "org",
		ScopeID:        sql.NullInt64{},
	})
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`UPDATE "user" SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, otherUserID,
	)
	require.NoError(t, err)

	require.NoError(t,
		svc.DeleteCustomRole(ctx, callerID, orgID, view.ID),
		"deactivated assignees must not block role deletion",
	)
}

func TestService_DeleteCustomRole_CrossOrgRoleIDMaskedAsInvalidArgument(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgA, callerA := setupOrgWithSuperAdmin(t, db)
	orgB, callerB := setupOrgWithSuperAdmin(t, db)

	svc := authz.NewService(db)
	roleInB, err := svc.CreateCustomRole(ctx, callerB, orgB,
		"OrgB Role", "",
		[]string{authz.PermFleetRead},
	)
	require.NoError(t, err)

	// Caller in orgA attempts to delete a role belonging to orgB. Must
	// surface as InvalidArgument (not NotFound or PermissionDenied) so
	// existence isn't leaked across tenants.
	err = svc.DeleteCustomRole(ctx, callerA, orgA, roleInB.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid role_id")
}

func TestService_ListRoles_BuiltinOrderAndCustomMemberCount(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID, callerID := setupOrgWithSuperAdmin(t, db)

	svc := authz.NewService(db)
	custom, err := svc.CreateCustomRole(ctx, callerID, orgID,
		"Site Lead", "",
		[]string{authz.PermFleetRead, authz.PermSiteRead},
	)
	require.NoError(t, err)

	// Give the custom role one member.
	otherUserID := insertTestUser(t, db)
	q := sqlc.New(db)
	_, err = q.AssignRole(ctx, sqlc.AssignRoleParams{
		UserID:         otherUserID,
		OrganizationID: orgID,
		RoleID:         custom.ID,
		ScopeType:      "org",
		ScopeID:        sql.NullInt64{},
	})
	require.NoError(t, err)

	roles, err := svc.ListRoles(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, roles, 4, "expect 3 builtins + 1 custom")

	require.True(t, roles[0].Builtin && roles[0].BuiltinKey == string(authz.BuiltinKeySuperAdmin),
		"SUPER_ADMIN must be first; got %+v", roles[0])
	require.True(t, roles[1].Builtin && roles[1].BuiltinKey == string(authz.BuiltinKeyAdmin),
		"ADMIN must be second")
	require.True(t, roles[2].Builtin && roles[2].BuiltinKey == string(authz.BuiltinKeyFieldTech),
		"FIELD_TECH must be third")
	require.False(t, roles[3].Builtin)
	require.Equal(t, "Site Lead", roles[3].Name)
	require.Equal(t, int32(1), roles[3].MemberCount,
		"custom role member_count reflects its one assignment")
	require.ElementsMatch(t,
		[]string{authz.PermFleetRead, authz.PermSiteRead},
		roles[3].PermissionKeys,
	)
}

// createCustomRoleWithPermissions inserts a custom role directly via
// sqlc, bypassing Service.CreateCustomRole's caller-authorization
// check. Use when a test needs a caller with a precise permission set
// that no built-in provides — e.g. role:manage without miner:reboot to
// isolate the per-key parity branch from the role:manage gate.
func createCustomRoleWithPermissions(t *testing.T, dbConn *sql.DB, orgID int64, name string, keys []string) int64 {
	t.Helper()
	ctx := t.Context()
	q := sqlc.New(dbConn)
	role, err := q.CreateCustomRole(ctx, sqlc.CreateCustomRoleParams{
		Name:           name,
		Description:    sql.NullString{},
		OrganizationID: sql.NullInt64{Int64: orgID, Valid: true},
	})
	require.NoError(t, err)
	perms, err := q.GetPermissionsByKeys(ctx, keys)
	require.NoError(t, err)
	require.Len(t, perms, len(keys), "missing permission rows for one of: %v", keys)
	for _, p := range perms {
		require.NoError(t, q.AssignPermissionToRole(ctx, sqlc.AssignPermissionToRoleParams{
			RoleID:       role.ID,
			PermissionID: p.ID,
		}))
	}
	return role.ID
}
