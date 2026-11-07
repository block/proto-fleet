package migrations_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestErrorsOpenDedupUpgradePreservesOldestOccurrencesAndRepairsFutureObservations(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	org := database.CreateSuperAdminUser().OrganizationID
	otherOrg := database.CreateSuperAdminUser2().OrganizationID
	device := database.CreateDevice(org, "proto")
	otherDevice := database.CreateDevice(org, "proto")
	ctx := t.Context()
	down, err := migrations.Current.ReadFile("current/001066_internal_errors_open_dedup_unique.down.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(ctx, string(down))
	require.NoError(t, err)
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	future := base.Add(48 * time.Hour)
	historyClosed := base.Add(-time.Minute)
	type occurrence struct {
		name          string
		org, device   int64
		miner         int
		component     any
		componentType any
		first, last   time.Time
		closed        *time.Time
		duplicate     bool
		id            int64
	}
	rows := []occurrence{
		{name: "null oldest", last: future},
		{name: "null newer", duplicate: true},
		{name: "type oldest", componentType: 1},
		{name: "type newer", componentType: 1, duplicate: true},
		{name: "component oldest", component: "fan"},
		{name: "component newer", component: "fan", duplicate: true},
		{name: "both oldest", component: "board", componentType: 2},
		{name: "both newer", component: "board", componentType: 2, duplicate: true},
		{name: "different miner", miner: 2},
		{name: "different component", component: "other", componentType: 2},
		{name: "different type", component: "board", componentType: 3},
		{name: "different device", device: otherDevice.DatabaseID},
		{name: "different org", org: otherOrg},
		{name: "closed history", last: future, closed: &historyClosed},
	}
	for i := range rows {
		row := &rows[i]
		if row.org == 0 {
			row.org = org
		}
		if row.device == 0 {
			row.device = device.DatabaseID
		}
		if row.miner == 0 {
			row.miner = 1
		}
		row.first = base
		if row.duplicate {
			// Earlier first_seen_at must not displace the older numeric ID.
			row.first = base.Add(-time.Hour)
		}
		if row.last.IsZero() {
			row.last = base
		}
		require.NoError(t, database.DB.QueryRowContext(ctx, `INSERT INTO errors
			(error_id,org_id,device_id,miner_error,severity,summary,component_id,component_type,first_seen_at,last_seen_at,closed_at)
			VALUES($1,$2,$3,$4,1,'legacy',$5,$6,$7,$8,$9) RETURNING id`,
			uuid.NewString(), row.org, row.device, row.miner, row.component, row.componentType,
			row.first, row.last, row.closed).Scan(&row.id))
	}

	up, err := migrations.Current.ReadFile("current/001066_internal_errors_open_dedup_unique.up.sql")
	require.NoError(t, err)
	tx, err := database.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	var migrationAt time.Time
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT CURRENT_TIMESTAMP").Scan(&migrationAt))
	_, err = tx.ExecContext(ctx, string(up))
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	for _, row := range rows {
		var first, last time.Time
		var closed sql.NullTime
		require.NoError(t, database.DB.QueryRowContext(ctx, "SELECT first_seen_at,last_seen_at,closed_at FROM errors WHERE id=$1", row.id).Scan(&first, &last, &closed))
		require.True(t, row.first.Equal(first), row.name)
		wantLast := row.last
		if row.closed == nil && wantLast.After(migrationAt) {
			wantLast = migrationAt
		}
		require.True(t, wantLast.Equal(last), "%s: last_seen_at = %s, want %s", row.name, last, wantLast)
		switch {
		case row.duplicate:
			require.True(t, closed.Valid && migrationAt.Equal(closed.Time), row.name)
		case row.closed != nil:
			require.True(t, closed.Valid && row.closed.Equal(closed.Time), row.name)
		default:
			require.False(t, closed.Valid, row.name)
			_, err = database.DB.ExecContext(ctx, `INSERT INTO errors
				(error_id,org_id,device_id,miner_error,severity,summary,component_id,component_type,first_seen_at,last_seen_at)
				VALUES($1,$2,$3,$4,1,'legacy',$5,$6,$7,$7)`,
				uuid.NewString(), row.org, row.device, row.miner, row.component, row.componentType, base)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr, row.name)
			require.Equal(t, "23505", pgErr.Code, row.name)
			require.Equal(t, "idx_errors_open_dedup", pgErr.ConstraintName, row.name)
		}
	}

	// A corrected poll must refresh the repaired survivor, then confirmed absence
	// must close it instead of retaining the old future timestamp indefinitely.
	observedAt := migrationAt.Add(time.Minute)
	store := sqlstores.NewSQLErrorStore(database.DB, sqlstores.NewSQLTransactor(database.DB))
	require.NoError(t, store.UpsertErrors(ctx, org, device.ID, []models.ErrorMessage{{
		MinerError: 1, Severity: 1, Summary: "legacy", FirstSeenAt: base, LastSeenAt: observedAt.Add(-time.Second),
	}}, observedAt, 0))
	var last time.Time
	require.NoError(t, database.DB.QueryRowContext(ctx, "SELECT last_seen_at FROM errors WHERE id=$1", rows[0].id).Scan(&last))
	require.True(t, observedAt.Equal(last))
	cutoff := observedAt.Add(2 * time.Minute)
	_, err = database.DB.ExecContext(ctx, "INSERT INTO device_status(device_id,status,status_timestamp) VALUES($1,'ACTIVE',$2)", device.DatabaseID, cutoff.Add(time.Second))
	require.NoError(t, err)
	queries, err := sqlc.Prepare(ctx, database.DB)
	require.NoError(t, err)
	defer queries.Close()
	_, err = queries.CloseStaleErrors(ctx, sqlc.CloseStaleErrorsParams{CutoffTime: cutoff, StatusCutoffTime: sql.NullTime{Time: cutoff, Valid: true}})
	require.NoError(t, err)
	var closed bool
	require.NoError(t, database.DB.QueryRowContext(ctx, "SELECT closed_at IS NOT NULL FROM errors WHERE id=$1", rows[0].id).Scan(&closed))
	require.True(t, closed)
}

func TestErrorsOpenDedupUpgradeFencesWritersBeforeCleanup(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	org := database.CreateSuperAdminUser().OrganizationID
	device := database.CreateDevice(org, "proto")
	ctx := t.Context()
	down, err := migrations.Current.ReadFile("current/001066_internal_errors_open_dedup_unique.down.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(ctx, string(down))
	require.NoError(t, err)
	insert := `INSERT INTO errors(error_id,org_id,device_id,miner_error,severity,summary,first_seen_at,last_seen_at)
		VALUES($1,$2,$3,1,1,'legacy',now(),now()) RETURNING id`
	var duplicateID int64
	for range 2 {
		require.NoError(t, database.DB.QueryRowContext(ctx, insert, uuid.NewString(), org, device.DatabaseID).Scan(&duplicateID))
	}
	blocker, err := database.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer blocker.Rollback()
	_, err = blocker.ExecContext(ctx, "SELECT id FROM errors WHERE id=$1 FOR UPDATE", duplicateID)
	require.NoError(t, err)
	upgrade, err := database.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	// Release the row lock before rolling the upgrade back on an assertion failure.
	defer func() {
		_ = blocker.Rollback()
		_ = upgrade.Rollback()
	}()
	var upgradePID int
	require.NoError(t, upgrade.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&upgradePID))
	up, err := migrations.Current.ReadFile("current/001066_internal_errors_open_dedup_unique.up.sql")
	require.NoError(t, err)
	finished := make(chan error, 1)
	go func() {
		_, err := upgrade.ExecContext(ctx, string(up))
		finished <- err
	}()
	// Park the real migration in duplicate cleanup, before index creation.
	require.Eventually(t, func() bool {
		var waiting bool
		err := database.DB.QueryRowContext(ctx, "SELECT cardinality(pg_blocking_pids($1)) > 0", upgradePID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	writer, err := database.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer writer.Rollback()
	_, err = writer.ExecContext(ctx, "SET LOCAL lock_timeout = '100ms'")
	require.NoError(t, err)
	// An unrelated key avoids both row-lock and dedup conflicts: only the
	// migration's table fence should stop an old writer during cleanup.
	_, err = writer.ExecContext(ctx, `INSERT INTO errors(error_id,org_id,device_id,miner_error,severity,summary,first_seen_at,last_seen_at)
		VALUES($1,$2,$3,2,1,'new key',now(),now())`, uuid.NewString(), org, device.DatabaseID)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "55P03", pgErr.Code)
	require.NoError(t, writer.Rollback())
	require.NoError(t, blocker.Commit())
	require.NoError(t, <-finished)
	require.NoError(t, upgrade.Commit())
}
