package migrations_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFleetNodeHARollbackPreservesOperationalState(t *testing.T) {
	for _, state := range []string{"pair", "PENDING", "AWAITING_CONFIRMATION", "expired pending", "CONFIRMED", "CANCELLED", "EXPIRED"} {
		t.Run(state, func(t *testing.T) {
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, cfg)
			user := database.CreateSuperAdminUser()
			conn, ctx := database.DB, t.Context()
			node := func() int64 {
				var id int64
				require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status) VALUES($1,$2,$3,$4,'CONFIRMED') RETURNING id`, user.OrganizationID, uuid.NewString(), []byte(uuid.NewString()), make([]byte, 32)).Scan(&id))
				return id
			}
			active, standby := node(), node()
			var enrollmentID int64
			if state == "pair" {
				_, err = conn.ExecContext(ctx, `INSERT INTO fleet_node_ha_pair(org_id,active_node_id,standby_node_id,standby_ready,pending_credential_key) VALUES($1,$2,$3,false,$4)`, user.OrganizationID, active, standby, []byte("pending sealed key"))
				require.NoError(t, err)
			} else {
				status, expires := state, time.Now().Add(time.Hour)
				if state == "expired pending" {
					status, expires = "PENDING", time.Now().Add(-time.Hour)
				}
				var enrollingNode any
				if status == "AWAITING_CONFIRMATION" || status == "CONFIRMED" {
					enrollingNode = standby
				}
				require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO pending_enrollment(org_id,created_by,code_hash,fleet_node_id,status,expires_at,standby_for_node_id) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, user.OrganizationID, user.DatabaseID, uuid.NewString(), enrollingNode, status, expires, active).Scan(&enrollmentID))
			}
			down, err := migrations.Current.ReadFile("current/001041_internal_fleet_node_standby_ha.down.sql")
			require.NoError(t, err)
			// Preparing enforces one atomic statement, so a failed guard cannot leave
			// part of the schema dropped without an enclosing migration transaction.
			rollback, err := conn.PrepareContext(ctx, string(down))
			require.NoError(t, err)
			defer rollback.Close()
			_, err = rollback.ExecContext(ctx)
			if state == "pair" || state == "PENDING" || state == "AWAITING_CONFIRMATION" {
				require.Error(t, err)
				var intact bool
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT to_regclass('fleet_node_ha_pair') IS NOT NULL AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='pending_enrollment' AND column_name='standby_for_node_id')`).Scan(&intact))
				require.True(t, intact)
				if state == "pair" {
					require.ErrorContains(t, err, "remove Fleet Node HA pairs")
					var key []byte
					require.NoError(t, conn.QueryRowContext(ctx, `SELECT pending_credential_key FROM fleet_node_ha_pair WHERE active_node_id=$1`, active).Scan(&key))
					require.Equal(t, []byte("pending sealed key"), key)
					_, err = conn.ExecContext(ctx, `DELETE FROM fleet_node_ha_pair`)
				} else {
					require.ErrorContains(t, err, "cancel pending standby enrollments")
					var target int64
					var status string
					require.NoError(t, conn.QueryRowContext(ctx, `SELECT standby_for_node_id,status FROM pending_enrollment WHERE id=$1`, enrollmentID).Scan(&target, &status))
					require.Equal(t, active, target)
					require.Equal(t, state, status)
					_, err = conn.ExecContext(ctx, `UPDATE pending_enrollment SET status='CANCELLED' WHERE id=$1`, enrollmentID)
				}
				require.NoError(t, err)
				_, err = rollback.ExecContext(ctx)
			}
			require.NoError(t, err, "resolved, expired, or terminal state must permit rollback")
			var removed bool
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT to_regclass('fleet_node_ha_pair') IS NULL AND NOT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='pending_enrollment' AND column_name='standby_for_node_id')`).Scan(&removed))
			require.True(t, removed)
			if enrollmentID != 0 {
				var count int
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pending_enrollment WHERE id=$1`, enrollmentID).Scan(&count))
				require.Equal(t, 1, count, "rollback preserves enrollment history")
			}
			up, err := migrations.Current.ReadFile("current/001041_internal_fleet_node_standby_ha.up.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, string(up))
			require.NoError(t, err, "HA schema must reapply after safe teardown")
		})
	}
}
