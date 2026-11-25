package mininggroups

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type queuedReadinessMiner struct {
	*simulatedGroups
	observationErr error
	observations   int
}

func (m *queuedReadinessMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}

func (m *queuedReadinessMiner) MiningGroups(ctx context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	if request.Action == "observe" {
		m.observations++
		if m.observationErr != nil {
			return sdk.MiningGroupResult{}, m.observationErr
		}
	}
	return m.simulatedGroups.MiningGroups(ctx, request)
}

func TestSharedBoardReconciliationOwnsExecutionReadiness(t *testing.T) {
	for _, scenario := range []string{"unchanged", "connection failure", "mode changed", "suspended", "controller busy", "stale board"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, f := newIdleRetryFixture(t)
			_, boards := preparePermissionPoolAssignment(t, ctx, f)
			var module, secondBoard int64
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT module_id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, boards[0]).Scan(&module))
			require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.target.OrganizationID).Scan(&secondBoard))
			_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata) VALUES($1,$2,2,now(),now(),'{"macAddress":"02:00:00:00:00:02"}')`, secondBoard, module)
			require.NoError(t, err)
			_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) VALUES($1,$2,2,'occupied',now())`, module, secondBoard)
			require.NoError(t, err)
			boards = append(boards, secondBoard)
			publishSharedBoardInventory(t, ctx, f)
			firmware := &queuedReadinessMiner{simulatedGroups: &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 4, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01", "02:00:00:00:00:02"}}}}, receipts: map[string]sdk.MiningGroupResult{}}}
			f.service.miners = firmware
			_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
			require.NoError(t, err)
			repo := poolconfigurations.NewRepository(f.service.secrets)
			reviewer := poolapplications.NewReviewer(f.service.pools, sharedCapabilities{})
			f.service.WithPoolConfigurations(repo, reviewer)
			service := poolconfigurations.NewService(f.conn, repo, f.service.pools, reviewer)
			config, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Shared", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example:3333", Username: "worker"}}})
			require.NoError(t, err)
			review, err := service.ReviewBoards(ctx, "", config.ID, config.Version, poolconfigurations.BoardSelection{HashboardIDs: boards})
			require.NoError(t, err)
			info, err := session.GetInfo(ctx)
			require.NoError(t, err)
			saved, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (poolconfigurations.ReviewedSave, error) {
				return repo.ConfirmReview(ctx, q, poolconfigurations.Scope{OrganizationID: info.OrganizationID}, info.UserID, review.ID, reviewer, nil)
			})
			require.NoError(t, err)
			// Only the controller mode observation expires; inventory remains fresh.
			_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET observed_at=now()-INTERVAL '3 minutes' WHERE device_id=$1`, f.target.DeviceID)
			require.NoError(t, err)
			switch scenario {
			case "connection failure":
				firmware.observationErr = errors.New("temporary controller connection failure")
			case "mode changed":
				firmware.snapshot.Enabled = false
			case "suspended":
				firmware.snapshot.Suspended = true
			case "controller busy":
				firmware.snapshot.Pending = true
			case "stale board":
				_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_slot SET observed_at=now()-INTERVAL '3 minutes' WHERE hashboard_id=$1`, secondBoard)
				require.NoError(t, err)
			}
			firmware.calls = nil
			firmware.observations = 0
			require.NoError(t, f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, saved.Operation.ID, f.target.DeviceID))
			require.Zero(t, firmware.observations, "admission must not contact the controller or depend on its cached observation age")
			operation, err := service.GetOperation(ctx, "", saved.Operation.ID)
			require.NoError(t, err)
			if scenario == "stale board" {
				for _, target := range operation.Targets {
					require.Equal(t, "failed", target.State)
				}
				require.False(t, active(f.state(t).Status))
				return
			}
			for _, target := range operation.Targets {
				require.Equal(t, "applying", target.State)
			}
			require.Equal(t, "pending", f.state(t).Status)
			require.NoError(t, f.service.Reconcile(ctx, info.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
			require.Equal(t, 1, firmware.observations)
			for _, call := range firmware.calls {
				require.Equal(t, "observe", call.Action, "readiness never mutates hardware")
			}
			if scenario == "mode changed" || scenario == "suspended" {
				require.Equal(t, "failed", f.state(t).Status)
				operation, err = service.GetOperation(ctx, "", saved.Operation.ID)
				require.NoError(t, err)
				for _, target := range operation.Targets {
					require.Equal(t, "failed", target.State)
				}
				var running int
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND status IN ('running','unknown')`, f.target.DeviceID).Scan(&running))
				require.Zero(t, running, "a mode mismatch must release admission for corrective commands")
				return
			}
			if scenario == "connection failure" {
				require.Equal(t, "blocked", f.state(t).Status)
			}
			firmware.observationErr = nil
			firmware.snapshot.Pending = false
			for i := 0; i < 30 && active(f.state(t).Status); i++ {
				require.NoError(t, f.service.Reconcile(ctx, info.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
			}
			require.Equal(t, statusComplete, f.state(t).Status, "the same workflow proceeds after readiness returns")
		})
	}
}
