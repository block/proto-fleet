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

func TestReconcile_FreshInstall_AllBuiltinsCreated(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	require.NoError(t, authz.Reconcile(ctx, db))

	q := sqlc.New(db)
	roles, err := q.ListBuiltinRoles(ctx)
	require.NoError(t, err)
	require.Len(t, roles, 3, "expected three built-in roles after reconcile")

	keys := make([]string, len(roles))
	for i, r := range roles {
		require.True(t, r.IsBuiltin)
		require.True(t, r.BuiltinKey.Valid)
		keys[i] = r.BuiltinKey.String
	}
	sort.Strings(keys)
	require.Equal(t, []string{"ADMIN", "FIELD_TECH", "SUPER_ADMIN"}, keys)
}

func TestReconcile_FreshInstall_SuperAdminHasEveryCatalogPermission(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	require.NoError(t, authz.Reconcile(ctx, db))

	got := rolePermissionKeys(t, db, "SUPER_ADMIN")
	want := authz.AllPermissionsSorted()
	require.Equal(t, want, got)
}

func TestReconcile_FreshInstall_AdminExcludesUserAndRoleManagement(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	require.NoError(t, authz.Reconcile(ctx, db))

	got := rolePermissionKeys(t, db, "ADMIN")
	for _, forbidden := range []string{authz.PermUserRead, authz.PermUserManage, authz.PermRoleManage} {
		require.NotContains(t, got, forbidden,
			"ADMIN must not seed with %q", forbidden)
	}
	require.Contains(t, got, authz.PermMinerReboot, "ADMIN should still hold miner action permissions")
}

func TestReconcile_FreshInstall_FieldTechHasExactSeedSet(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	require.NoError(t, authz.Reconcile(ctx, db))

	got := rolePermissionKeys(t, db, "FIELD_TECH")
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

	require.NoError(t, authz.Reconcile(ctx, db))
	first := snapshotRolePermissions(t, db)

	require.NoError(t, authz.Reconcile(ctx, db))
	second := snapshotRolePermissions(t, db)

	require.Equal(t, first, second, "reconcile must be idempotent")
}

func TestReconcile_OperatorEditToAdminSurvivesRestart(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	require.NoError(t, authz.Reconcile(ctx, db))

	revokePermissionFromBuiltin(t, db, "ADMIN", authz.PermMinerFirmwareUpdate)

	// Restart-equivalent: invoke the reconciler again.
	require.NoError(t, authz.Reconcile(ctx, db))

	got := rolePermissionKeys(t, db, "ADMIN")
	require.NotContains(t, got, authz.PermMinerFirmwareUpdate,
		"additive-only reconcile must NOT re-add an operator-removed permission to ADMIN")
}

func TestReconcile_OperatorEditToFieldTechSurvivesRestart(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	require.NoError(t, authz.Reconcile(ctx, db))

	revokePermissionFromBuiltin(t, db, "FIELD_TECH", authz.PermRackManage)

	require.NoError(t, authz.Reconcile(ctx, db))

	got := rolePermissionKeys(t, db, "FIELD_TECH")
	require.NotContains(t, got, authz.PermRackManage,
		"additive-only reconcile must NOT re-add an operator-removed permission to FIELD_TECH")
}

func TestReconcile_OperatorAdditionToFieldTechSurvivesRestart(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	require.NoError(t, authz.Reconcile(ctx, db))

	addPermissionToBuiltin(t, db, "FIELD_TECH", authz.PermMinerReboot)

	require.NoError(t, authz.Reconcile(ctx, db))

	got := rolePermissionKeys(t, db, "FIELD_TECH")
	require.Contains(t, got, authz.PermMinerReboot,
		"additive-only reconcile must preserve operator-added permissions on FIELD_TECH")
}

func TestReconcile_SuperAdminTamperingRepaired(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	require.NoError(t, authz.Reconcile(ctx, db))

	revokePermissionFromBuiltin(t, db, "SUPER_ADMIN", authz.PermMinerReboot)

	require.NoError(t, authz.Reconcile(ctx, db))

	got := rolePermissionKeys(t, db, "SUPER_ADMIN")
	require.Contains(t, got, authz.PermMinerReboot,
		"full reconcile must restore tampered SUPER_ADMIN permissions")
	require.Equal(t, authz.AllPermissionsSorted(), got,
		"SUPER_ADMIN must converge back to the full catalog")
}

func TestReconcile_SuperAdminObsoletePermissionPruned(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	require.NoError(t, authz.Reconcile(ctx, db))

	// Insert a permission row that is not in the in-code catalog, then
	// attach it to SUPER_ADMIN. Reconcile must strip it back off.
	_, err := db.ExecContext(ctx,
		`INSERT INTO permission (key, description) VALUES ('legacy:obsolete', 'left behind by an older catalog')`,
	)
	require.NoError(t, err)
	addPermissionToBuiltin(t, db, "SUPER_ADMIN", "legacy:obsolete")

	require.NoError(t, authz.Reconcile(ctx, db))

	got := rolePermissionKeys(t, db, "SUPER_ADMIN")
	require.NotContains(t, got, "legacy:obsolete",
		"full reconcile must prune non-catalog permissions from SUPER_ADMIN")
}

func TestReconcile_ConcurrentRunsConverge(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	// Two reconciles racing the advisory lock; both must complete
	// without error and the final state must equal a single-pass
	// reconcile's state.
	errs := make(chan error, 2)
	go func() { errs <- authz.Reconcile(ctx, db) }()
	go func() { errs <- authz.Reconcile(ctx, db) }()
	for range 2 {
		require.NoError(t, <-errs)
	}

	got := rolePermissionKeys(t, db, "SUPER_ADMIN")
	require.Equal(t, authz.AllPermissionsSorted(), got)
}

// ---------------------------------------------------------------
// helpers
// ---------------------------------------------------------------

func rolePermissionKeys(t *testing.T, db *sql.DB, builtinKey string) []string {
	t.Helper()
	q := sqlc.New(db)
	role, err := q.GetRoleByBuiltinKey(t.Context(), sql.NullString{String: builtinKey, Valid: true})
	require.NoError(t, err)
	keys, err := q.ListRolePermissionKeys(t.Context(), role.ID)
	require.NoError(t, err)
	sort.Strings(keys)
	return keys
}

func snapshotRolePermissions(t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, key := range []string{"SUPER_ADMIN", "ADMIN", "FIELD_TECH"} {
		out[key] = rolePermissionKeys(t, db, key)
	}
	return out
}

func revokePermissionFromBuiltin(t *testing.T, db *sql.DB, builtinKey, permKey string) {
	t.Helper()
	q := sqlc.New(db)
	role, err := q.GetRoleByBuiltinKey(t.Context(), sql.NullString{String: builtinKey, Valid: true})
	require.NoError(t, err)
	perm, err := q.GetPermissionByKey(t.Context(), permKey)
	require.NoError(t, err)
	require.NoError(t, q.RevokePermissionFromRole(t.Context(), sqlc.RevokePermissionFromRoleParams{
		RoleID:       role.ID,
		PermissionID: perm.ID,
	}))
}

func addPermissionToBuiltin(t *testing.T, db *sql.DB, builtinKey, permKey string) {
	t.Helper()
	q := sqlc.New(db)
	role, err := q.GetRoleByBuiltinKey(t.Context(), sql.NullString{String: builtinKey, Valid: true})
	require.NoError(t, err)
	perm, err := q.GetPermissionByKey(t.Context(), permKey)
	require.NoError(t, err)
	require.NoError(t, q.AssignPermissionToRole(t.Context(), sqlc.AssignPermissionToRoleParams{
		RoleID:       role.ID,
		PermissionID: perm.ID,
	}))
}

// Compile-time guards: these names are public API the rest of the
// codebase reaches for. Failing this line means the package surface
// drifted and downstream callers won't compile.
var (
	_ = context.Background
	_ = authz.Reconcile
)
