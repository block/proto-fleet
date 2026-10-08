package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestCurtailmentMigrationPreservesLegacyAttemptsAndRestrictsScope(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	down, err := migrations.Current.ReadFile("current/001013_internal_hosted_curtailment_control.down.sql")
	require.NoError(t, err)
	up, err := migrations.Current.ReadFile("current/001013_internal_hosted_curtailment_control.up.sql")
	require.NoError(t, err)
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginHost(t.Context(), user.OrganizationID, device.DatabaseID, 0)
	require.NoError(t, err)
	require.NoError(t, attempt.Finish(t.Context(), nil))
	_, err = database.DB.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(up))
	require.NoError(t, err)
	var ordinary int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM device_write_attempt WHERE curtailment_command IS NULL AND status='complete'`).Scan(&ordinary))
	require.Equal(t, 1, ordinary)
	_, err = database.DB.ExecContext(t.Context(), `UPDATE device_write_attempt SET curtailment_command='Reboot'`)
	require.Error(t, err)
	for _, kind := range []commandtype.Type{commandtype.Curtail, commandtype.Uncurtail, commandtype.ApplyCurtailmentConfig} {
		_, err = database.DB.ExecContext(t.Context(), `UPDATE device_write_attempt SET curtailment_command=$1`, kind.String())
		require.NoError(t, err)
	}
	_, err = database.DB.ExecContext(t.Context(), string(down))
	require.ErrorContains(t, err, "rollback is unsafe")
	var retained string
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT curtailment_command FROM device_write_attempt`).Scan(&retained))
	require.Equal(t, "ApplyCurtailmentConfig", retained)
}

func TestCurtailmentRollbackRefusesPendingAndProcessingControl(t *testing.T) {
	for _, kind := range []commandtype.Type{commandtype.Curtail, commandtype.Uncurtail, commandtype.ApplyCurtailmentConfig} {
		for _, state := range []string{"PENDING", "PROCESSING"} {
			t.Run(kind.String()+"/"+state, func(t *testing.T) {
				cfg, err := testutil.GetTestConfig()
				require.NoError(t, err)
				database := testutil.NewDatabaseService(t, cfg)
				user := database.CreateSuperAdminUser()
				device := database.CreateDevice(user.OrganizationID, "proto")
				_, err = database.DB.ExecContext(t.Context(), `INSERT INTO queue_message(command_batch_log_uuid,device_id,command_type,status,retry_count) VALUES('curtailment-rollback',$1,$2,$3,0)`, device.DatabaseID, kind.String(), state)
				require.NoError(t, err)
				down, err := migrations.Current.ReadFile("current/001013_internal_hosted_curtailment_control.down.sql")
				require.NoError(t, err)
				_, err = database.DB.ExecContext(t.Context(), string(down))
				require.ErrorContains(t, err, "rollback is unsafe")
				var count int
				require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM queue_message WHERE status=$1`, state).Scan(&count))
				require.Equal(t, 1, count)
			})
		}
	}
}
