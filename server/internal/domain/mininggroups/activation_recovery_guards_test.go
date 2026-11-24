package mininggroups

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/stretchr/testify/require"
)

func TestPartialActivationRecoveryKeepsIntentWhenEvidenceChanges(t *testing.T) {
	for _, change := range []string{
		"unchanged evidence", "MAC", "serial", "owner", "allocation revision",
		"default membership", "inventory timestamp", "group observation timestamp", "default binding",
	} {
		t.Run(change, func(t *testing.T) {
			ctx, f, miner, _, _, boards, _ := newActivationPoolFixture(t)
			q := sqlc.New(f.conn)
			completePendingActivation(t, ctx, f, boards[0])
			pending, err := q.ListActivationPoolBoardIntents(ctx, sqlc.ListActivationPoolBoardIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			require.Len(t, pending, 1)
			source, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
			require.NoError(t, err)
			require.True(t, source.RetiredAt.Valid, "partial transfer replaces the container assignment with board intent")
			_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) SELECT module_id,hashboard_id,position,'occupied',now() FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, boards[0])
			require.NoError(t, err)

			switch change {
			case "MAC":
				miner.inventory.Boards[0].MACAddress = "02:00:00:00:fe:01"
				_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_identity SET value=$1 WHERE hashboard_id=$2 AND kind='mac'`, miner.inventory.Boards[0].MACAddress, boards[0])
			case "serial":
				miner.inventory.Boards[0].Serial = "changed-serial"
				_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_identity SET value=$1 WHERE hashboard_id=$2 AND kind='serial'`, miner.inventory.Boards[0].Serial, boards[0])
			case "owner":
				var account int64
				require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT gen_random_uuid()::text,organization_id,id,1,'New client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, f.target.OrganizationID).Scan(&account))
				_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id) VALUES($1,7,$2,$2,$3,$4)`, f.target.OrganizationID, boards[0], f.target.DeviceID, account)
			case "allocation revision":
				_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,revision) VALUES($1,7,$2,$2,$3,2)`, f.target.OrganizationID, boards[0], f.target.DeviceID)
			case "default membership":
				miner.snapshot.Groups[0].MACs = nil
				var observed []byte
				observed, err = encode(miner.snapshot)
				require.NoError(t, err)
				_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET observed=$1 WHERE device_id=$2`, observed, f.target.DeviceID)
			case "inventory timestamp":
				miner.inventory.ObservedAt = time.Now().Add(-3 * time.Minute)
			case "group observation timestamp":
				_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET observed_at=now()-interval '3 minutes' WHERE device_id=$1`, f.target.DeviceID)
			case "default binding":
				_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET bindings=jsonb_set(bindings::jsonb,'{default,config_id}','"changed-snapshot"') WHERE device_id=$1`, f.target.DeviceID)
			}
			require.NoError(t, err)
			calls := len(miner.calls)
			err = f.service.finishPendingActivationPlacement(ctx, f.target.OrganizationID, f.target.DeviceID)
			if change == "default binding" {
				require.ErrorContains(t, err, "inherited pool settings changed")
			} else {
				require.NoError(t, err)
			}
			remaining, err := q.ListActivationPoolBoardIntents(ctx, sqlc.ListActivationPoolBoardIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			fenced, err := q.HasPendingMiningGroupPoolHandoff(ctx, sqlc.HasPendingMiningGroupPoolHandoffParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
			require.NoError(t, err)
			var assignments int
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_assignment a JOIN hashboard_installation i ON a.target_kind='hashboard_installation' AND a.target_id=i.id WHERE i.hashboard_id=$1 AND a.retired_at IS NULL`, boards[0]).Scan(&assignments))
			if change == "unchanged evidence" {
				require.Empty(t, remaining)
				require.False(t, fenced)
				require.Equal(t, 1, assignments)
			} else {
				require.Equal(t, pending, remaining, "rejected evidence must preserve saved board intent")
				require.True(t, fenced)
				require.Zero(t, assignments, "rejected evidence must not create a board assignment")
			}
			currentSource, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
			require.NoError(t, err)
			require.Equal(t, source, currentSource, "recovery must not restore or alter the retired container assignment")
			for _, call := range miner.calls[calls:] {
				require.True(t, call.ReadOnly(), "placement recovery must not change controller pools")
			}
		})
	}
}
