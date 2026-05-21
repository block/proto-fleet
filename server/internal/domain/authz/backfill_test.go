package authz_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// On a fresh install (no existing data, no backfill migration to lean
// on), the founding-user creation path must dual-write into both
// user_organization (legacy, still consumed by some callers) AND
// user_organization_role (the new resolver's source of truth).
// Without the dual-write, the founding SUPER_ADMIN would never appear
// in the assignment table and the resolver would deny everything once
// it goes live.
func TestOnboarding_FoundingUserGetsOrgScopeSuperAdminAssignment(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	store := sqlstores.NewSQLUserStore(db)
	require.NoError(t, store.CreateAdminUserWithOrganization(
		ctx,
		uniqueToken("ext-user"),
		uniqueToken("admin"),
		"dummy-hash",
		"Onboarding Test Org",
		uniqueToken("ext-org"),
		"dummy-private-key",
		"SUPER_ADMIN",
		"Super admin role",
	))

	q := sqlc.New(db)
	user, err := q.GetUserByUsername(ctx, anyUsernameMatching(t, db))
	require.NoError(t, err)

	orgs, err := q.GetOrganizationsForUser(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	orgID := orgs[0].ID

	assignments, err := q.ListAssignmentsForUser(ctx, sqlc.ListAssignmentsForUserParams{
		UserID:         user.ID,
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1, "founding user must have exactly one assignment row")

	a := assignments[0]
	require.Equal(t, "org", a.ScopeType)
	require.False(t, a.ScopeID.Valid, "org-scope assignment must have NULL scope_id")

	superAdmin, err := q.GetBuiltinRoleForOrg(ctx, sqlc.GetBuiltinRoleForOrgParams{
		OrganizationID: sql.NullInt64{Int64: orgID, Valid: true},
		BuiltinKey:     sql.NullString{String: "SUPER_ADMIN", Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, superAdmin.ID, a.RoleID,
		"founding user's assignment must point at their org's SUPER_ADMIN row, not a legacy global one")
}

// anyUsernameMatching returns the single username from the "user"
// table. Used by the onboarding test which doesn't know the username
// it generated in advance.
func anyUsernameMatching(t *testing.T, db *sql.DB) string {
	t.Helper()
	var name string
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT username FROM "user" ORDER BY id DESC LIMIT 1`,
	).Scan(&name))
	return name
}

// TestBackfill_ExistingAdminUserGetsOrgScopeAssignment verifies that
// migration 000053 mirrors every active user_organization row into
// user_organization_role as an org-scope assignment.
func TestBackfill_ExistingAdminUserGetsOrgScopeAssignment(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	orgID := insertTestOrganization(t, db)
	userID := insertTestUser(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))
	adminRoleID := getBuiltinRoleID(t, db, orgID, "ADMIN")

	// Insert via the legacy table directly to simulate a row that
	// existed before 000053 ran.
	_, err := db.ExecContext(ctx,
		`INSERT INTO user_organization (user_id, organization_id, role_id) VALUES ($1, $2, $3)`,
		userID, orgID, adminRoleID,
	)
	require.NoError(t, err)

	// Re-run the backfill statement. Migration 000053 already executed
	// during ConnectAndMigrate, but the user_organization row we just
	// inserted post-dates that pass — re-running the same idempotent
	// statement covers it and exercises the ON CONFLICT path.
	runBackfill(t, db)

	q := sqlc.New(db)
	assignments, err := q.ListAssignmentsForUser(ctx, sqlc.ListAssignmentsForUserParams{
		UserID:         userID,
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)

	a := assignments[0]
	require.Equal(t, "org", a.ScopeType)
	require.False(t, a.ScopeID.Valid, "org-scope assignment must have NULL scope_id")
	require.Equal(t, adminRoleID, a.RoleID)
}

// A soft-deleted organization can still have active user_organization
// rows (org soft-delete does not cascade to membership). 000052 must
// seed per-org built-ins for that org so the repoint and the 000053
// backfill produce rows that satisfy the composite role FK; otherwise
// the deploy fails.
func TestSeed_SoftDeletedOrgStillGetsBuiltins(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	// Insert a soft-deleted organization directly.
	var deletedOrgID int64
	require.NoError(t,
		db.QueryRowContext(ctx,
			`INSERT INTO organization (org_id, name, miner_auth_private_key, deleted_at)
             VALUES ($1, $2, $3, CURRENT_TIMESTAMP) RETURNING id`,
			uniqueToken("deleted-org"), "Soft Deleted Org", "dummy-key",
		).Scan(&deletedOrgID),
	)

	// Reconcile finds this org via SeedOrgBuiltins's invocation from
	// the boot reconciler — but ListActiveOrganizationIDs excludes
	// deleted orgs, so we exercise SeedOrgBuiltins directly. Migration
	// 000052 covers existing soft-deleted orgs at upgrade time; this
	// also exercises the same seed path.
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck

	_, err = authz.SeedOrgBuiltins(ctx, sqlc.New(tx), deletedOrgID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	q := sqlc.New(db)
	for _, key := range []string{"SUPER_ADMIN", "ADMIN", "FIELD_TECH"} {
		_, err := q.GetBuiltinRoleForOrg(ctx, sqlc.GetBuiltinRoleForOrgParams{
			OrganizationID: sql.NullInt64{Int64: deletedOrgID, Valid: true},
			BuiltinKey:     sql.NullString{String: key, Valid: true},
		})
		require.NoError(t, err, "soft-deleted org must still have a per-org %s row", key)
	}
}

func TestBackfill_SoftDeletedUserOrganizationRowsAreNotCopied(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)
	userID := insertTestUser(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))
	adminRoleID := getBuiltinRoleID(t, db, orgID, "ADMIN")

	_, err := db.ExecContext(ctx,
		`INSERT INTO user_organization (user_id, organization_id, role_id, deleted_at) VALUES ($1, $2, $3, CURRENT_TIMESTAMP)`,
		userID, orgID, adminRoleID,
	)
	require.NoError(t, err)

	runBackfill(t, db)

	q := sqlc.New(db)
	assignments, err := q.ListAssignmentsForUser(ctx, sqlc.ListAssignmentsForUserParams{
		UserID:         userID,
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	require.Empty(t, assignments, "soft-deleted user_organization rows must not produce assignments")
}

func TestAssignment_SoftDeletedRowDoesNotBlockReassign(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)
	userID := insertTestUser(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))
	adminRoleID := getBuiltinRoleID(t, db, orgID, "ADMIN")

	q := sqlc.New(db)

	// Initial assignment.
	first, err := q.AssignRole(ctx, sqlc.AssignRoleParams{
		UserID:         userID,
		OrganizationID: orgID,
		RoleID:         adminRoleID,
		ScopeType:      "org",
		ScopeID:        sql.NullInt64{},
	})
	require.NoError(t, err)

	// Soft-delete it.
	require.NoError(t, q.UnassignRole(ctx, first.ID))

	// Re-assigning the same (user, org, role, scope) tuple must
	// succeed because the partial unique index only covers live rows.
	// Under the old global UNIQUE constraint this would have failed.
	_, err = q.AssignRole(ctx, sqlc.AssignRoleParams{
		UserID:         userID,
		OrganizationID: orgID,
		RoleID:         adminRoleID,
		ScopeType:      "org",
		ScopeID:        sql.NullInt64{},
	})
	require.NoError(t, err, "re-assigning after soft-delete must be allowed")
}

func TestAssignment_DuplicateLiveOrgScopeRejected(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)
	userID := insertTestUser(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))
	adminRoleID := getBuiltinRoleID(t, db, orgID, "ADMIN")

	q := sqlc.New(db)

	_, err := q.AssignRole(ctx, sqlc.AssignRoleParams{
		UserID:         userID,
		OrganizationID: orgID,
		RoleID:         adminRoleID,
		ScopeType:      "org",
		ScopeID:        sql.NullInt64{},
	})
	require.NoError(t, err)

	// Second live insert with the same (user, org, role, 'org', NULL)
	// must fail despite scope_id being NULL — the partial unique index
	// closes the NULL-distinct loophole.
	_, err = q.AssignRole(ctx, sqlc.AssignRoleParams{
		UserID:         userID,
		OrganizationID: orgID,
		RoleID:         adminRoleID,
		ScopeType:      "org",
		ScopeID:        sql.NullInt64{},
	})
	require.Error(t, err, "duplicate live org-scope assignment must be rejected")
}

func TestBackfill_Idempotent(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID := insertTestOrganization(t, db)
	userID := insertTestUser(t, db)
	require.NoError(t, authz.Reconcile(ctx, db))
	adminRoleID := getBuiltinRoleID(t, db, orgID, "ADMIN")

	_, err := db.ExecContext(ctx,
		`INSERT INTO user_organization (user_id, organization_id, role_id) VALUES ($1, $2, $3)`,
		userID, orgID, adminRoleID,
	)
	require.NoError(t, err)

	runBackfill(t, db)
	runBackfill(t, db)

	q := sqlc.New(db)
	assignments, err := q.ListAssignmentsForUser(ctx, sqlc.ListAssignmentsForUserParams{
		UserID:         userID,
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1, "running the backfill twice must produce exactly one assignment row")
}

// ---------------------------------------------------------------
// helpers
// ---------------------------------------------------------------

func runBackfill(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO user_organization_role (user_id, organization_id, role_id, scope_type, scope_id)
		SELECT user_id, organization_id, role_id, 'org', NULL
		FROM user_organization
		WHERE deleted_at IS NULL
		ON CONFLICT (user_id, organization_id, role_id)
		    WHERE scope_type = 'org' AND deleted_at IS NULL
		    DO NOTHING
	`)
	require.NoError(t, err)
}

func insertTestOrganization(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var id int64
	require.NoError(t,
		db.QueryRowContext(t.Context(),
			`INSERT INTO organization (org_id, name, miner_auth_private_key) VALUES ($1, $2, $3) RETURNING id`,
			uniqueToken("org"), "Backfill Test Org", "dummy-key",
		).Scan(&id),
	)
	return id
}

func insertTestUser(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var id int64
	require.NoError(t,
		db.QueryRowContext(t.Context(),
			`INSERT INTO "user" (user_id, username, password_hash) VALUES ($1, $2, $3) RETURNING id`,
			uniqueToken("user"), uniqueToken("user-name"), "dummy-hash",
		).Scan(&id),
	)
	return id
}

func getBuiltinRoleID(t *testing.T, db *sql.DB, orgID int64, builtinKey string) int64 {
	t.Helper()
	q := sqlc.New(db)
	role, err := q.GetBuiltinRoleForOrg(t.Context(), sqlc.GetBuiltinRoleForOrgParams{
		OrganizationID: sql.NullInt64{Int64: orgID, Valid: true},
		BuiltinKey:     sql.NullString{String: builtinKey, Valid: true},
	})
	require.NoError(t, err)
	return role.ID
}

// uniqueToken produces a unique identifier per call. testutil.GetTestDB
// gives us a fresh schema, so the only uniqueness concern is within a
// single test invocation; nanosecond timestamps are plenty.
func uniqueToken(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
