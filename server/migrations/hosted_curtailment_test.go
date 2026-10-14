package migrations_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestCurtailmentMigrationPreservesLegacyAttemptsAndRestrictsScope(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	down, err := migrations.Current.ReadFile("current/001018_internal_hosted_curtailment_control.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001018_internal_hosted_curtailment_control.up.sql")
	require.NoError(t, err)
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginHost(t.Context(), user.OrganizationID, device.DatabaseID, 0)
	require.NoError(t, err)
	// Dispatched mutations retain history independently of command acceptance.
	require.NoError(t, devicewrite.Dispatch(attempt.Context(t.Context()), func(context.Context) error { return nil }))
	require.NoError(t, attempt.Finish(t.Context(), nil))
	// Seed explicitly qualified legacy completion history for this migration.
	// The unclassified BeginHost admission retains unresolved mutation evidence.
	_, err = database.DB.ExecContext(t.Context(), "UPDATE device_write_attempt SET status='complete' WHERE device_id=$1", device.DatabaseID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(up))
	require.NoError(t, err)
	var ordinary int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM device_write_attempt WHERE curtailment_command IS NULL AND status='complete'`).Scan(&ordinary))
	require.Equal(t, 1, ordinary)
	// The enum is contiguous; stop at its first undefined value so newly added
	// commands participate without maintaining another curtailment-kind list.
	for kind := commandtype.StartMining; kind.String() != "Undefined"; kind++ {
		t.Run(kind.String(), func(t *testing.T) {
			tx, err := database.DB.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = tx.ExecContext(t.Context(), `UPDATE device_write_attempt SET curtailment_command=$1`, kind.String())
			if !kind.IsCurtailmentControl() {
				var constraintError *pgconn.PgError
				require.ErrorAs(t, err, &constraintError)
				require.Equal(t, "23514", constraintError.Code)
				require.Equal(t, "device_write_attempt_curtailment_command_check", constraintError.ConstraintName)
				return
			}
			require.NoError(t, err)
			_, err = tx.ExecContext(t.Context(), "SAVEPOINT before_rollback")
			require.NoError(t, err)
			_, err = tx.ExecContext(t.Context(), string(down))
			require.ErrorContains(t, err, "rollback is unsafe")
			_, err = tx.ExecContext(t.Context(), "ROLLBACK TO SAVEPOINT before_rollback")
			require.NoError(t, err)
			var retained string
			require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT curtailment_command FROM device_write_attempt WHERE status='complete'`).Scan(&retained))
			require.Equal(t, kind.String(), retained, "refused rollback must preserve completed curtailment history")
		})
	}
}

func TestCurtailmentRollbackMatchesCommandClassification(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	down, err := migrations.Current.ReadFile("current/001018_internal_hosted_curtailment_control.down.sql")
	require.NoError(t, err)
	for kind := commandtype.StartMining; kind.String() != "Undefined"; kind++ {
		for _, state := range []string{"PENDING", "PROCESSING"} {
			t.Run(kind.String()+"/"+state, func(t *testing.T) {
				tx, err := database.DB.BeginTx(t.Context(), nil)
				require.NoError(t, err)
				defer tx.Rollback()
				_, err = tx.ExecContext(t.Context(), `INSERT INTO queue_message(command_batch_log_uuid,device_id,command_type,status,retry_count) VALUES('curtailment-rollback',$1,$2,$3,0)`, device.DatabaseID, kind.String(), state)
				require.NoError(t, err)
				// Preserve the queued command even when the rollback guard refuses.
				_, err = tx.ExecContext(t.Context(), "SAVEPOINT before_rollback")
				require.NoError(t, err)
				_, err = tx.ExecContext(t.Context(), string(down))
				if kind.IsCurtailmentControl() {
					require.ErrorContains(t, err, "rollback is unsafe")
					_, err = tx.ExecContext(t.Context(), "ROLLBACK TO SAVEPOINT before_rollback")
					require.NoError(t, err)
				} else {
					require.NoError(t, err, "ordinary commands must not block curtailment rollback")
				}
				var count int
				require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT count(*) FROM queue_message WHERE status=$1 AND command_type=$2`, state, kind.String()).Scan(&count))
				require.Equal(t, 1, count)
			})
		}
	}
}

func TestCurtailmentPendingCancellationMatchesCommandClassification(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	queries := sqlc.New(database.DB)
	messages := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 3}, database.DB)
	for kind := commandtype.StartMining; kind.String() != "Undefined"; kind++ {
		batch := id.GenerateID()
		_, err = queries.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{
			Uuid: batch, Type: kind.String(), CreatedBy: user.DatabaseID, CreatedAt: time.Now(),
			Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1,
			OrganizationID: sql.NullInt64{Int64: user.OrganizationID, Valid: true},
		})
		require.NoError(t, err)
		require.NoError(t, messages.Enqueue(t.Context(), batch, kind, []int64{device.DatabaseID}, nil))
	}
	require.NoError(t, queries.CancelPendingDeviceWrites(t.Context(), device.DatabaseID))
	for kind := commandtype.StartMining; kind.String() != "Undefined"; kind++ {
		t.Run(kind.String(), func(t *testing.T) {
			var status string
			var result sql.NullString
			require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT m.status,l.status
			 FROM queue_message m
			 JOIN command_batch_log b ON b.uuid=m.command_batch_log_uuid
			 LEFT JOIN command_on_device_log l ON l.command_batch_log_id=b.id AND l.device_id=m.device_id
			 WHERE m.device_id=$1 AND m.command_type=$2`, device.DatabaseID, kind.String()).Scan(&status, &result))
			if kind.IsCurtailmentControl() {
				require.Equal(t, "PENDING", status)
				require.False(t, result.Valid, "preserved control must not acquire a cancellation result")
			} else {
				require.Equal(t, "FAILED", status)
				require.Equal(t, sql.NullString{String: "FAILED", Valid: true}, result)
			}
		})
	}
}
