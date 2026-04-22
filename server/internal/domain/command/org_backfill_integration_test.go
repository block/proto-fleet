package command_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/block/proto-fleet/server/generated/sqlc"
	db2 "github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// Integration coverage for the 000036 organization_id backfill rule. The
// migration's UPDATE statement only populates organization_id for creators
// with exactly one live user_organization row; multi-org creators stay NULL
// so they cannot be silently attributed to the wrong tenant. The test
// re-executes the exact backfill UPDATE from the migration against seeded
// NULL-org batches and asserts the outcome.
//
// If the backfill UPDATE in
// server/migrations/000036_add_org_id_to_command_batch_log.up.sql is ever
// changed, update the statement embedded below to match.

// backfillStatement is copied verbatim from the migration so the test
// protects the behavior, not the file.
const backfillStatement = `
UPDATE command_batch_log cbl
SET organization_id = (
    SELECT uo.organization_id
    FROM user_organization uo
    WHERE uo.user_id = cbl.created_by
      AND uo.deleted_at IS NULL
    LIMIT 1
)
WHERE cbl.organization_id IS NULL
  AND (
    SELECT COUNT(*)
    FROM user_organization uo
    WHERE uo.user_id = cbl.created_by
      AND uo.deleted_at IS NULL
  ) = 1`

// seedNullOrgBatch inserts a command_batch_log row with organization_id
// explicitly NULL so the backfill UPDATE is the only thing that can set it.
func seedNullOrgBatch(t *testing.T, conn *sql.DB, uuid string, userID int64, createdAt time.Time) {
	t.Helper()
	ctx := context.Background()
	err := db2.WithTransactionNoResult(ctx, conn, func(q *sqlc.Queries) error {
		_, err := q.CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
			Uuid:           uuid,
			Type:           "REBOOT",
			CreatedBy:      userID,
			CreatedAt:      createdAt,
			Status:         sqlc.BatchStatusEnumFINISHED,
			DevicesCount:   1,
			Payload:        pqtype.NullRawMessage{Valid: false},
			OrganizationID: sql.NullInt64{Valid: false},
		})
		return err
	})
	require.NoError(t, err)
}

// addOrgMembership adds a second live user_organization row for an existing
// test user, making them a multi-org creator. Used to simulate the
// ambiguous case the backfill rule refuses to guess at.
func addOrgMembership(t *testing.T, conn *sql.DB, userID int64, orgName string, minerAuthPrivateKey string) int64 {
	t.Helper()
	ctx := context.Background()
	var orgID int64
	err := db2.WithTransactionNoResult(ctx, conn, func(q *sqlc.Queries) error {
		newOrgID, err := q.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
			Name:                orgName,
			OrgID:               orgName,
			MinerAuthPrivateKey: minerAuthPrivateKey,
		})
		if err != nil {
			return err
		}
		roleID, err := q.UpsertRole(ctx, sqlc.UpsertRoleParams{
			Name: "SUPER_ADMIN",
			Description: sql.NullString{
				String: "Super admin role for testing",
				Valid:  true,
			},
		})
		if err != nil {
			return err
		}
		if err := q.CreateUserOrganization(ctx, sqlc.CreateUserOrganizationParams{
			UserID:         userID,
			OrganizationID: newOrgID,
			RoleID:         roleID,
		}); err != nil {
			return err
		}
		orgID = newOrgID
		return nil
	})
	require.NoError(t, err)
	return orgID
}

// readBatchOrgID returns the current organization_id column value for a
// batch. nil means NULL.
func readBatchOrgID(t *testing.T, conn *sql.DB, uuid string) *int64 {
	t.Helper()
	var orgID sql.NullInt64
	err := conn.QueryRowContext(context.Background(),
		`SELECT organization_id FROM command_batch_log WHERE uuid = $1`, uuid).Scan(&orgID)
	require.NoError(t, err)
	if !orgID.Valid {
		return nil
	}
	return &orgID.Int64
}

// TestOrgIDBackfill_SingleMembership verifies that a batch whose creator has
// exactly one live user_organization row gets its organization_id populated
// by the migration's backfill statement.
func TestOrgIDBackfill_SingleMembership(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	dbService := testutil.NewDatabaseService(t, cfg)
	user := dbService.CreateSuperAdminUser()
	conn := dbService.DB

	batchUUID := "single-org-backfill"
	seedNullOrgBatch(t, conn, batchUUID, user.DatabaseID, time.Now())

	_, err = conn.ExecContext(context.Background(), backfillStatement)
	require.NoError(t, err)

	got := readBatchOrgID(t, conn, batchUUID)
	require.NotNil(t, got, "single-org creator's batch must be populated by the backfill")
	assert.Equal(t, user.OrganizationID, *got,
		"single-org creator's batch must be attributed to their only org")
}

// TestOrgIDBackfill_MultiOrgStaysNull verifies that a batch whose creator
// has more than one live user_organization row is left NULL by the backfill
// statement. Guessing would silently assign historical batches to the wrong
// tenant (see Codex security review, PR #25, finding S1/R15).
func TestOrgIDBackfill_MultiOrgStaysNull(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	dbService := testutil.NewDatabaseService(t, cfg)
	user := dbService.CreateSuperAdminUser()

	// Add a second live membership so the creator now belongs to two orgs.
	// MinerAuthPrivateKey is taken from the shared test config so the
	// CreateOrganization constraint is satisfied.
	keyHash, err := bcrypt.GenerateFromPassword([]byte("fleet-test-key"), bcrypt.DefaultCost)
	require.NoError(t, err)
	secondOrgID := addOrgMembership(t, dbService.DB, user.DatabaseID, "second-org-backfill", string(keyHash))
	require.NotEqual(t, user.OrganizationID, secondOrgID,
		"second org must be distinct so the creator is genuinely multi-org")

	conn := dbService.DB
	batchUUID := "multi-org-backfill"
	seedNullOrgBatch(t, conn, batchUUID, user.DatabaseID, time.Now())

	_, err = conn.ExecContext(context.Background(), backfillStatement)
	require.NoError(t, err)

	got := readBatchOrgID(t, conn, batchUUID)
	assert.Nil(t, got,
		"multi-org creator's batch must stay NULL so GetBatchHeaderForOrg cannot attribute it to a guessed tenant")
}
