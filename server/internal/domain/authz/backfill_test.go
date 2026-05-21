package authz_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// TestBackfill_ExistingAdminUserGetsOrgScopeAssignment verifies that
// migration 000053 mirrors every active user_organization row into
// user_organization_role as an org-scope assignment.
func TestBackfill_ExistingAdminUserGetsOrgScopeAssignment(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()

	require.NoError(t, authz.Reconcile(ctx, db))

	orgID := insertTestOrganization(t, db)
	userID := insertTestUser(t, db)
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

func TestBackfill_SoftDeletedUserOrganizationRowsAreNotCopied(t *testing.T) {
	db := testutil.GetTestDB(t)
	ctx := t.Context()
	require.NoError(t, authz.Reconcile(ctx, db))

	orgID := insertTestOrganization(t, db)
	userID := insertTestUser(t, db)
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
	require.NoError(t, authz.Reconcile(ctx, db))

	orgID := insertTestOrganization(t, db)
	userID := insertTestUser(t, db)
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
	require.NoError(t, authz.Reconcile(ctx, db))

	orgID := insertTestOrganization(t, db)
	userID := insertTestUser(t, db)
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
	require.NoError(t, authz.Reconcile(ctx, db))

	orgID := insertTestOrganization(t, db)
	userID := insertTestUser(t, db)
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
