package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMiningGroupDispatchRollbackPreservesSavedRequests(t *testing.T) {
	for _, status := range []string{"pending", "applying", "blocked", "complete"} {
		t.Run(status, func(t *testing.T) {
			conn, _, device := miningGroupRollbackFixture(t)
			key := uuid.New()
			_, err := conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET status=$1,step_key=$2 WHERE device_id=$3`, status, key, device)
			require.NoError(t, err)
			down, err := migrations.Current.ReadFile("current/001084_internal_mining_group_step_dispatch.down.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(t.Context(), string(down))
			if status != "complete" {
				require.ErrorContains(t, err, "Settle saved mining-group requests")
				var exists bool
				require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT to_regclass('mining_group_step_dispatch') IS NOT NULL`).Scan(&exists))
				require.True(t, exists)
				return
			}
			require.NoError(t, err)
			up, err := migrations.Current.ReadFile("current/001084_internal_mining_group_step_dispatch.up.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(t.Context(), string(up))
			require.NoError(t, err)
			var records int
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM mining_group_step_dispatch WHERE step_key=$1`, key).Scan(&records))
			require.Zero(t, records, "upgrade must not infer delivery for an older request")
		})
	}
}
