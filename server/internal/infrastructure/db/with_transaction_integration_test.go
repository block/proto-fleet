package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// Integration coverage for db.WithReadOnlyTransaction. These tests run against
// a real Postgres instance because the behavior we want to verify -- that the
// sql.TxOptions{ReadOnly: true} option is actually being propagated to
// BeginTx -- is only observable from Postgres rejecting a write with SQLSTATE
// 25006. A unit-level assertion would require mocking *sql.DB, which would
// re-introduce the exact "did TxOptions get through?" question we're trying
// to answer.

// TestWithReadOnlyTransaction_RejectsWrites verifies that WithReadOnlyTransaction
// runs its callback under a truly read-only transaction: a write executed
// inside the callback must fail with Postgres's 25006 (read_only_sql_transaction)
// error. This proves the sql.TxOptions is being applied by BeginTx -- a
// regression where WithReadOnlyTransaction silently ran a read-write
// transaction would let the write succeed.
func TestWithReadOnlyTransaction_RejectsWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	dbService := testutil.NewDatabaseService(t, cfg)

	ctx := context.Background()
	_, err = db.WithReadOnlyTransaction(ctx, dbService.DB, func(q *sqlc.Queries) (int64, error) {
		// Any DDL or DML reaches the server. CreateUser avoids needing a
		// pre-existing row; keep the user_id <= 36 chars so pq's client-side
		// width check doesn't short-circuit before the server-side read-only
		// rejection fires. The username width is far more generous.
		return q.CreateUser(ctx, sqlc.CreateUserParams{
			UserID:       "ro-reject-test",
			Username:     "ro-reject-test@example.com",
			PasswordHash: "irrelevant",
			CreatedAt:    time.Now(),
		})
	})
	require.Error(t, err, "write inside a read-only transaction must fail")

	errMsg := err.Error()
	// The retry wrapper re-wraps the underlying pgconn.PgError via %v, so we
	// assert on the string rather than errors.As(*pgconn.PgError). The
	// SQLSTATE code is the stable identifier across pg/timescale versions;
	// the human message ("cannot execute INSERT in a read-only transaction")
	// is secondary.
	assert.Truef(t,
		strings.Contains(errMsg, "25006") || strings.Contains(errMsg, "read-only transaction"),
		"expected read-only SQLSTATE 25006 in error, got: %s", errMsg)
}

// TestWithReadOnlyTransaction_SuccessfulRead verifies the happy path: reads
// inside the callback round-trip correctly and the returned value is handed
// back to the caller. This complements the rejection test by showing that
// the read-only option isn't a full "disable everything" foot-gun.
func TestWithReadOnlyTransaction_SuccessfulRead(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	dbService := testutil.NewDatabaseService(t, cfg)
	user := dbService.CreateSuperAdminUser()

	ctx := context.Background()
	got, err := db.WithReadOnlyTransaction(ctx, dbService.DB, func(q *sqlc.Queries) (sqlc.User, error) {
		return q.GetUserByUsername(ctx, user.Username)
	})
	require.NoError(t, err)
	assert.Equal(t, user.Username, got.Username)
	assert.Equal(t, user.DatabaseID, got.ID)
	assert.Equal(t, user.ExternalUserID, got.UserID)
}
