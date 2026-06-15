package authz_test

import (
	"context"
	"database/sql"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestReconcile_FreshInstall_OrgGetsAllBuiltins(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	orgID := insertTestOrganization(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))

	q := sqlc.New(db)
	roles, err := q.ListBuiltinRolesForOrg(ctx, sql.NullInt64{Int64: orgID, Valid: true})
	require.NoError(t, err)
	require.Len(t, roles, 3, "expected three built-in roles per org after reconcile")

	keys := make([]string, len(roles))
	for i, r := range roles {
		require.True(t, r.IsBuiltin)
		require.True(t, r.BuiltinKey.Valid)
		require.True(t, r.OrganizationID.Valid, "per-org built-in must carry organization_id")
		require.Equal(t, orgID, r.OrganizationID.Int64)
		keys[i] = r.BuiltinKey.String
	}
	sort.Strings(keys)
	require.Equal(t, []string{"ADMIN", "FIELD_TECH", "SUPER_ADMIN"}, keys)
}

func TestReconcile_FreshInstall_SuperAdminHasEveryCatalogPermission(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))

	got := orgRolePermissionKeys(t, db, orgID, "SUPER_ADMIN")
	want := authz.AllPermissionsSorted()
	require.Equal(t, want, got)
}

func TestReconcile_FreshInstall_AdminExcludesRoleManagement(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))

	got := orgRolePermissionKeys(t, db, orgID, "ADMIN")
	require.NotContains(t, got, authz.PermRoleManage, "ADMIN must not seed with role:manage")
	require.Contains(t, got, authz.PermUserRead, "ADMIN holds user:read so org admins can view the team roster")
	require.Contains(t, got, authz.PermUserManage, "ADMIN holds user:manage; hierarchy check blocks elevated targets at the domain layer")
	require.Contains(t, got, authz.PermMinerReboot, "ADMIN should still hold miner action permissions")
	require.Contains(t, got, authz.PermMinerFirmwareUpdate, "ADMIN should seed with firmware update permissions")
}

func TestReconcile_FreshInstall_FieldTechHasExactSeedSet(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))

	got := orgRolePermissionKeys(t, db, orgID, "FIELD_TECH")
	want := []string{
		authz.PermFleetRead,
		authz.PermMinerBlinkLED,
		authz.PermMinerDownloadLogs,
		authz.PermMinerRead,
		authz.PermRackManage,
		authz.PermRackRead,
	}
	sort.Strings(want)
	require.Equal(t, want, got)
}

func TestReconcile_Idempotent(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)

	require.NoError(t, authz.Reconcile(ctx, db))
	first := snapshotOrgRolePermissions(t, db, orgID)

	require.NoError(t, authz.Reconcile(ctx, db))
	second := snapshotOrgRolePermissions(t, db, orgID)

	require.Equal(t, first, second, "reconcile must be idempotent")
}

func TestReconcile_PerOrgIsolation_EditingOneOrgDoesNotAffectAnother(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgA := insertTestOrganization(t, db)
	orgB := insertTestOrganization(t, db)

	require.NoError(t, authz.Reconcile(ctx, db))

	// Operator in org A removes miner:firmware_update from their ADMIN.
	revokeOrgPermission(t, db, orgA, "ADMIN", authz.PermMinerFirmwareUpdate)

	require.NoError(t, authz.Reconcile(ctx, db))

	require.NotContains(t, orgRolePermissionKeys(t, db, orgA, "ADMIN"), authz.PermMinerFirmwareUpdate,
		"org A's ADMIN edit must persist")
	require.Contains(t, orgRolePermissionKeys(t, db, orgB, "ADMIN"), authz.PermMinerFirmwareUpdate,
		"org B's ADMIN must NOT be affected by an org A edit")
}

func TestReconcile_OperatorEditToAdminSurvivesRestart(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))

	revokeOrgPermission(t, db, orgID, "ADMIN", authz.PermMinerFirmwareUpdate)
	require.NoError(t, authz.Reconcile(ctx, db))

	got := orgRolePermissionKeys(t, db, orgID, "ADMIN")
	require.NotContains(t, got, authz.PermMinerFirmwareUpdate,
		"reconcile must NOT re-add an operator-removed permission to ADMIN")
}

// Reconcile must not silently restore a sensitive permission that an
// operator explicitly revoked from ADMIN. Anything that re-asserts the
// seed set on every boot would re-enable pool changes or firmware
// flashes for users who were deliberately restricted.
func TestReconcile_OperatorRevokedSensitivePermStaysRevokedOnRestart(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))

	for _, perm := range []string{authz.PermMinerUpdatePools, authz.PermMinerFirmwareUpdate, authz.PermMinerReboot} {
		revokeOrgPermission(t, db, orgID, "ADMIN", perm)
	}

	// Two extra reconciles for good measure — none of them should
	// touch the revoked entries.
	require.NoError(t, authz.Reconcile(ctx, db))
	require.NoError(t, authz.Reconcile(ctx, db))

	got := orgRolePermissionKeys(t, db, orgID, "ADMIN")
	for _, perm := range []string{authz.PermMinerUpdatePools, authz.PermMinerFirmwareUpdate, authz.PermMinerReboot} {
		require.NotContains(t, got, perm,
			"sensitive permission %q must stay revoked across restarts", perm)
	}
}

// Catalog growth (a new permission added in a future release) must NOT
// silently propagate to an existing org's ADMIN or FIELD_TECH. Once
// the role exists the operator owns it; new permissions only show up
// when the operator adds them via the role editor.
func TestReconcile_NewCatalogPermissionDoesNotPropagateToExistingBuiltins(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))

	beforeAdmin := orgRolePermissionKeys(t, db, orgID, "ADMIN")
	beforeFieldTech := orgRolePermissionKeys(t, db, orgID, "FIELD_TECH")

	// Simulate catalog growth by inserting a brand-new permission row
	// directly. (In production this would happen via a code change in
	// catalog.go plus a redeploy.)
	_, err := db.ExecContext(ctx,
		`INSERT INTO permission (key, description) VALUES ('synthetic:new_key', 'simulated future catalog addition')`,
	)
	require.NoError(t, err)
	require.NoError(t, authz.Reconcile(ctx, db))

	require.Equal(t, beforeAdmin, orgRolePermissionKeys(t, db, orgID, "ADMIN"),
		"new catalog permissions must not auto-add to an existing org's ADMIN; seed-formula changes that need to reach existing rows ship as explicit one-off migrations")
	require.Equal(t, beforeFieldTech, orgRolePermissionKeys(t, db, orgID, "FIELD_TECH"),
		"new catalog permissions must not auto-add to an existing org's FIELD_TECH")
}

func TestReconcile_OperatorAdditionToFieldTechSurvivesRestart(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))

	addOrgPermission(t, db, orgID, "FIELD_TECH", authz.PermMinerReboot)
	require.NoError(t, authz.Reconcile(ctx, db))

	got := orgRolePermissionKeys(t, db, orgID, "FIELD_TECH")
	require.Contains(t, got, authz.PermMinerReboot,
		"additive-only reconcile must preserve operator-added permissions on FIELD_TECH")
}

func TestReconcile_SuperAdminTamperingRepaired(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))

	revokeOrgPermission(t, db, orgID, "SUPER_ADMIN", authz.PermMinerReboot)
	require.NoError(t, authz.Reconcile(ctx, db))

	got := orgRolePermissionKeys(t, db, orgID, "SUPER_ADMIN")
	require.Contains(t, got, authz.PermMinerReboot,
		"full reconcile must restore tampered SUPER_ADMIN permissions")
	require.Equal(t, authz.AllPermissionsSorted(), got,
		"SUPER_ADMIN must converge back to the full catalog")
}

func TestReconcile_ConcurrentRunsConverge(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)

	errs := make(chan error, 2)
	go func() { errs <- authz.Reconcile(ctx, db) }()
	go func() { errs <- authz.Reconcile(ctx, db) }()
	for range 2 {
		require.NoError(t, <-errs)
	}

	require.Equal(t, authz.AllPermissionsSorted(), orgRolePermissionKeys(t, db, orgID, "SUPER_ADMIN"))
}

// ---------------------------------------------------------------
// helpers
// ---------------------------------------------------------------

func orgRolePermissionKeys(t *testing.T, db *sql.DB, orgID int64, builtinKey string) []string {
	t.Helper()
	q := sqlc.New(db)
	role, err := q.GetBuiltinRoleForOrg(t.Context(), sqlc.GetBuiltinRoleForOrgParams{
		OrganizationID: sql.NullInt64{Int64: orgID, Valid: true},
		BuiltinKey:     sql.NullString{String: builtinKey, Valid: true},
	})
	require.NoError(t, err)
	keys, err := q.ListRolePermissionKeys(t.Context(), role.ID)
	require.NoError(t, err)
	sort.Strings(keys)
	return keys
}

func snapshotOrgRolePermissions(t *testing.T, db *sql.DB, orgID int64) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, key := range []string{"SUPER_ADMIN", "ADMIN", "FIELD_TECH"} {
		out[key] = orgRolePermissionKeys(t, db, orgID, key)
	}
	return out
}

func revokeOrgPermission(t *testing.T, db *sql.DB, orgID int64, builtinKey, permKey string) {
	t.Helper()
	q := sqlc.New(db)
	role, err := q.GetBuiltinRoleForOrg(t.Context(), sqlc.GetBuiltinRoleForOrgParams{
		OrganizationID: sql.NullInt64{Int64: orgID, Valid: true},
		BuiltinKey:     sql.NullString{String: builtinKey, Valid: true},
	})
	require.NoError(t, err)
	perm, err := q.GetPermissionByKey(t.Context(), permKey)
	require.NoError(t, err)
	require.NoError(t, q.RevokePermissionFromRole(t.Context(), sqlc.RevokePermissionFromRoleParams{
		RoleID:       role.ID,
		PermissionID: perm.ID,
	}))
}

func addOrgPermission(t *testing.T, db *sql.DB, orgID int64, builtinKey, permKey string) {
	t.Helper()
	q := sqlc.New(db)
	role, err := q.GetBuiltinRoleForOrg(t.Context(), sqlc.GetBuiltinRoleForOrgParams{
		OrganizationID: sql.NullInt64{Int64: orgID, Valid: true},
		BuiltinKey:     sql.NullString{String: builtinKey, Valid: true},
	})
	require.NoError(t, err)
	perm, err := q.GetPermissionByKey(t.Context(), permKey)
	require.NoError(t, err)
	require.NoError(t, q.AssignPermissionToRole(t.Context(), sqlc.AssignPermissionToRoleParams{
		RoleID:       role.ID,
		PermissionID: perm.ID,
	}))
}

var (
	_ = context.Background
	_ = authz.Reconcile
)
