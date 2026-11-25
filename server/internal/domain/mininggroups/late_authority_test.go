package mininggroups

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func TestSharedHistoricalBoardAuthorityLossRetiresOnlyUnsentWork(t *testing.T) {
	for _, action := range []string{"start", actionReplacePools} {
		for _, stage := range []string{"planning", "prepared", "uncertain"} {
			t.Run(action+"/"+stage, func(t *testing.T) {
				ctx, f := newIdleRetryFixture(t)
				_, saved, firmware := newConfirmedSharedBoardApplication(t, ctx, f)
				require.NoError(t, f.service.ExecutePoolConfiguration(ctx, f.target.OrganizationID, saved.Operation.ID, f.target.DeviceID))
				var selected, historical, installation int64
				var snapshot string
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT i.hashboard_id,t.group_snapshot_id FROM pool_configuration_operation_target t JOIN hashboard_installation i ON i.id=t.target_id WHERE t.organization_id=$1 AND t.operation_id=$2`, f.target.OrganizationID, saved.Operation.ID).Scan(&selected, &snapshot))
				require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.target.OrganizationID).Scan(&historical))
				require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,site_id,metadata)
 SELECT $1,module_id,2,now(),now(),site_id,jsonb_build_object('macAddress',$3::text) FROM hashboard_installation WHERE hashboard_id=$2 AND removed_at IS NULL RETURNING id`, historical, selected, historicalBoardMAC).Scan(&installation))
				_, err := f.conn.ExecContext(ctx, `UPDATE hashboard_slot SET hashboard_id=$1,occupancy='occupied' WHERE module_id=(SELECT module_id FROM hashboard_installation WHERE id=$2) AND position=2`, historical, installation)
				require.NoError(t, err)
				publishSharedBoardInventory(t, ctx, f)
				// This board has an established assignment, but it is not a target
				// of the current operation. Only a group-wide step must authorize it.
				_, err = f.conn.ExecContext(ctx, `INSERT INTO pool_configuration_assignment(organization_id,target_kind,target_id,device_id,configuration_id,revision_id,version,adopted_at)
 SELECT organization_id,target_kind,$1,device_id,configuration_id,revision_id,1,now() FROM pool_configuration_assignment WHERE organization_id=$2 AND target_id=$3 AND target_kind='hashboard_installation'`, installation, f.target.OrganizationID, saved.Operation.Targets[0].ID)
				require.NoError(t, err)
				_, err = f.conn.ExecContext(ctx, `INSERT INTO pool_configuration_board_intent(organization_id,installation_id,device_id,hashboard_id,mac_address,group_snapshot_id,adopted_at) VALUES($1,$2,$3,$4,$5,$6,now())`, f.target.OrganizationID, installation, f.target.DeviceID, historical, historicalBoardMAC, snapshot)
				require.NoError(t, err)
				group := sdk.MiningGroup{ID: "configured", Revision: 1, MACs: []string{"02:00:00:00:00:01", historicalBoardMAC}, Pools: []sdk.MiningGroupPool{{ID: 1, URL: "stratum+tcp://pool.example:3333", Username: "worker", Enabled: true}}}
				if action == actionReplacePools {
					group.Pools = nil
				}
				firmware.snapshot.Groups = []sdk.MiningGroup{{ID: "default", Revision: 1}, group}
				bindings, err := encode(map[string]binding{"configured": {ConfigID: snapshot, Configured: true}})
				require.NoError(t, err)
				_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET bindings=$1 WHERE device_id=$2`, bindings, f.target.DeviceID)
				require.NoError(t, err)
				if stage != "planning" {
					f.reconcile(t, ctx)
					prepared := f.state(t)
					require.True(t, prepared.Step.Valid)
					var step savedStep
					require.NoError(t, decode(prepared.Step.RawMessage, &step))
					require.Contains(t, step.AffectedMACs, historicalBoardMAC)
					if stage == "uncertain" {
						marked, err := f.service.queries.MarkMiningGroupStepDispatch(ctx, sqlc.MarkMiningGroupStepDispatchParams{StepKey: prepared.StepKey.UUID, AttemptID: prepared.AttemptID.UUID, DispatchToken: uuid.New()})
						require.NoError(t, err)
						require.True(t, marked)
					}
				}
				var client int64
				require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities)
 SELECT gen_random_uuid()::text,organization_id,id,1,'Other client',31 FROM hosted_account WHERE organization_id=$1 AND depth=0 RETURNING id`, f.target.OrganizationID).Scan(&client))
				_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id) VALUES($1,7,$2,$2,$3,$4)`, f.target.OrganizationID, historical, f.target.DeviceID, client)
				require.NoError(t, err)
				before := f.state(t)
				shared, err := f.service.authorizeSharedWorkflow(ctx, f.service.queries, before)
				require.NoError(t, err, "the current selected board must retain its host authority")
				require.True(t, shared, "the fixture must exercise shared assignment authority")
				_, err = f.service.liveIntent(ctx, f.service.queries, before, nil)
				require.NoError(t, err, "unaffected historical intent must pass the early checks")
				firmware.calls = nil
				f.reconcile(t, ctx)
				after := f.state(t)
				if stage == "uncertain" {
					require.Equal(t, "blocked", after.Status)
					require.Equal(t, before.Step, after.Step)
					require.Equal(t, before.StepKey, after.StepKey)
					require.Equal(t, before.AttemptID, after.AttemptID)
				} else {
					require.Equal(t, "failed", after.Status, "late authority loss must commit interruption instead of rolling back forever")
					require.False(t, after.Step.Valid)
					require.False(t, after.StepKey.Valid)
				}
				for _, call := range firmware.calls {
					require.True(t, call.ReadOnly(), "a group-wide write cannot touch a different client's board")
				}
				require.Empty(t, firmware.receipts)
			})
		}
	}
}
