package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestCT1MiningGroupGuardsMigrationRoundTrip(t *testing.T) {
	for _, state := range []string{"empty", "pending", "complete", "deferred"} {
		t.Run(state, func(t *testing.T) {
			conn, _, device := miningGroupRollbackFixture(t)
			if state == "deferred" {
				_, err := conn.ExecContext(t.Context(), `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,state)
				 SELECT org_id,id,5,id,'ownership','deferred' FROM device WHERE id=$1`, device)
				require.NoError(t, err)
			} else if state != "empty" {
				_, err := conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET operation_kind='deactivate',status=$2 WHERE device_id=$1`, device, state)
				require.NoError(t, err)
			}
			down, err := migrations.Current.ReadFile("current/001082_internal_ct1_mining_group_guards.down.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(t.Context(), string(down))
			if state != "empty" {
				if state == "deferred" {
					require.ErrorContains(t, err, "retain deferred pool lifecycle work")
				} else {
					require.ErrorContains(t, err, "retain deactivation journals")
				}
				var retained bool
				require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM permission WHERE key='miner:deactivate_mining_groups') AND to_regprocedure('ct1_hashboard_allocation_ready(bigint,bigint)') IS NOT NULL`).Scan(&retained))
				require.True(t, retained)
				if state != "deferred" {
					return
				}
				_, err = conn.ExecContext(t.Context(), `UPDATE pool_lifecycle_transition SET state='pending' WHERE device_id=$1 AND state='deferred'`, device)
				require.NoError(t, err)
				_, err = conn.ExecContext(t.Context(), string(down))
			}
			require.NoError(t, err)
			var removed bool
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT NOT EXISTS(SELECT 1 FROM permission WHERE key='miner:deactivate_mining_groups') AND to_regprocedure('ct1_hashboard_allocation_ready(bigint,bigint)') IS NULL`).Scan(&removed))
			require.True(t, removed)
			up, err := migrations.Current.ReadFile("current/001082_internal_ct1_mining_group_guards.up.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(t.Context(), string(up))
			require.NoError(t, err)
			_, err = conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET operation_kind='deactivate' WHERE device_id=$1`, device)
			require.NoError(t, err)
		})
	}
}
