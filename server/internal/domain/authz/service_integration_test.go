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
	fieldTechUserID := insertTestUser(t, db)
	fieldTechRoleID := getBuiltinRoleID(t, db, orgID, "FIELD_TECH")

	q := sqlc.New(db)
	_, err := q.AssignRole(ctx, sqlc.AssignRoleParams{
		UserID:         fieldTechUserID,
		OrganizationID: orgID,
		RoleID:         fieldTechRoleID,
		ScopeType:      "org",
		ScopeID:        sql.NullInt64{},
	})
	require.NoError(t, err)

	svc := authz.NewService(db)
	// FIELD_TECH does not hold miner:reboot — the parity check must reject.
	_, err = svc.CreateCustomRole(ctx, fieldTechUserID, orgID,
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
	orgID, _ := setupOrgWithSuperAdmin(t, db)
	svc := authz.NewService(db)

	for _, key := range []string{"SUPER_ADMIN", "ADMIN", "FIELD_TECH"} {
		builtinRoleID := getBuiltinRoleID(t, db, orgID, key)
		err := svc.DeleteCustomRole(t.Context(), orgID, builtinRoleID)
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

	err = svc.DeleteCustomRole(ctx, orgID, view.ID)
	require.Error(t, err, "delete must refuse while assignments exist")
	require.Contains(t, err.Error(), "active assignment")
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
	err = svc.DeleteCustomRole(ctx, orgA, roleInB.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid role_id")
	// Sanity: callerA exists and is SUPER_ADMIN; this is purely the
	// cross-org guard rejecting us, not auth.
	_ = callerA
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
