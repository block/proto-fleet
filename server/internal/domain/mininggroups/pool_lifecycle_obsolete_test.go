package mininggroups

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type obsoleteClearGroups struct {
	*admittedGroups
	unavailable bool
}

func (m *obsoleteClearGroups) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}

func (m *obsoleteClearGroups) MiningGroups(ctx context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	if request.Action == "observe" && m.unavailable {
		return sdk.MiningGroupResult{}, errors.New("controller observation unavailable")
	}
	return m.admittedGroups.MiningGroups(ctx, request)
}

func TestObsoleteAutomaticBoardClearSettlesWithoutReplayingOldPolicy(t *testing.T) {
	for _, stage := range []string{"no outstanding request", "transfer settles", "ownership returns", "pending controller", "unavailable controller", "unknown saved request"} {
		t.Run(stage, func(t *testing.T) {
			ctx, f := newIdleRetryFixture(t)
			_, boards := preparePermissionPoolAssignment(t, ctx, f)
			publishSharedBoardInventory(t, ctx, f)
			firmware := &simulatedGroups{
				snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 4,
					Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, Mining: true, MACs: []string{"02:00:00:00:00:01"}, Pools: []sdk.MiningGroupPool{{URL: "stratum+tcp://previous.example:3333", Username: "previous"}}}}},
				receipts: map[string]sdk.MiningGroupResult{},
			}
			guarded := &obsoleteClearGroups{admittedGroups: &admittedGroups{firmware}}
			f.service.miners = guarded
			_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
			require.NoError(t, err)
			repo := poolconfigurations.NewRepository(f.service.secrets)
			reviewer := poolapplications.NewReviewer(f.service.pools, removalCapabilities{})
			checker := poolapplications.NewChecker(f.conn, reviewer, guarded, f.service.secrets, f.service.pools)
			f.service.WithPoolConfigurations(repo, reviewer).WithPoolRemovals(checker)
			var id uuid.UUID
			require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason) VALUES($1,$2,7,$3,'ownership') RETURNING transition_id`, f.target.OrganizationID, f.target.DeviceID, boards[0]).Scan(&id))
			worker := poolapplications.NewWorker(f.conn, repo, reviewer, guarded, f.service.secrets).WithRemovals(checker).WithBoardApplications(f.service)
			require.NoError(t, worker.ProcessLifecycle(ctx, id))
			initial := f.state(t)
			require.True(t, initial.AttemptID.Valid)
			require.False(t, initial.Step.Valid)
			switch stage {
			case "transfer settles":
				for i := 0; i < 30 && f.state(t).ControllerOperationID == ""; i++ {
					require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
				}
				require.NotEmpty(t, f.state(t).ControllerOperationID)
			case "pending controller":
				firmware.snapshot.Pending = true
			case "unavailable controller":
				guarded.unavailable = true
			case "unknown saved request":
				require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
				require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
				require.True(t, f.state(t).Step.Valid, "the controller accepted create but its response was lost")
				require.Empty(t, f.state(t).ControllerOperationID)
			}
			before := f.state(t)
			// Accepted inventory can change allocation without AssignAssets. Keep
			// the installation present while its next owner invalidates old policy.
			var owner int64
			require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities)
				SELECT $1,organization_id,id,1,'Next owner',31 FROM hosted_account WHERE organization_id=$2 AND depth=0 RETURNING id`, uuid.NewString(), f.target.OrganizationID).Scan(&owner))
			_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id)
				VALUES($1,7,$2,$2,$3,$4)`, f.target.OrganizationID, boards[0], f.target.DeviceID, owner)
			require.NoError(t, err)
			if stage == "ownership returns" {
				_, err = f.conn.ExecContext(ctx, `UPDATE hosted_account_asset SET client_id=NULL,revision=revision+1 WHERE organization_id=$1 AND kind=7 AND asset_id=$2`, f.target.OrganizationID, boards[0])
				require.NoError(t, err)
				owner = 0
			}
			allocation := func() string {
				var value string
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT jsonb_build_array(COALESCE(client_id,0),COALESCE(subclient_id,0),revision)::text FROM hosted_account_asset WHERE organization_id=$1 AND kind=7 AND asset_id=$2`, f.target.OrganizationID, boards[0]).Scan(&value))
				return value
			}
			allocationBefore := allocation()
			var nextPolicyID uuid.UUID
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT transition_id FROM pool_lifecycle_transition WHERE organization_id=$1 AND asset_kind=7 AND asset_id=$2 AND transition_id<>$3 ORDER BY created_at DESC LIMIT 1`, f.target.OrganizationID, boards[0], id).Scan(&nextPolicyID))
			firmware.calls = nil
			// A known transfer may finish after ownership changes. Receipt polling
			// must still run, then retire the obsolete policy from settled state.
			for i := 0; i < 3 && active(f.state(t).Status); i++ {
				err = f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier)
				if stage == "no outstanding request" || stage == "transfer settles" || stage == "ownership returns" {
					require.NoError(t, err)
				}
			}
			for _, call := range firmware.calls {
				require.True(t, call.ReadOnly(), "obsolete ownership must never dispatch another mutation: %s", call.Action)
			}
			transition, err := sqlc.New(f.conn).GetPoolLifecycleTransition(ctx, id)
			require.NoError(t, err)
			attempt, err := sqlc.New(f.conn).LockDeviceWriteAttemptForRecovery(ctx, initial.AttemptID.UUID)
			require.NoError(t, err)
			if stage == "pending controller" || stage == "unavailable controller" || stage == "unknown saved request" {
				require.True(t, active(f.state(t).Status), "unresolved controller work retains its workflow")
				require.Equal(t, "running", attempt.Status)
				require.Equal(t, "applying", transition.State)
				require.Equal(t, before.Step, f.state(t).Step)
				require.Equal(t, before.StepKey, f.state(t).StepKey)
				_, err = f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
				require.Error(t, err, "unknown work must keep custody fenced")
				return
			}
			require.Equal(t, "superseded", transition.State, "settled obsolete clearing must release its reservation")
			require.False(t, active(f.state(t).Status), f.state(t).Message)
			require.Equal(t, "complete", attempt.Status)
			require.Equal(t, before.Bindings, f.state(t).Bindings, "preserve controller effects already observed")
			require.Equal(t, allocationBefore, allocation(), "retirement preserves current allocation and generation")
			var currentOwner int64
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT COALESCE(client_id,0) FROM hosted_account_asset WHERE organization_id=$1 AND kind=7 AND asset_id=$2`, f.target.OrganizationID, boards[0]).Scan(&currentOwner))
			require.Equal(t, owner, currentOwner, "retiring old policy preserves the new owner's allocation")
			var nextPolicy string
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT state FROM pool_lifecycle_transition WHERE transition_id=$1`, nextPolicyID).Scan(&nextPolicy))
			require.Equal(t, "pending", nextPolicy, "the next owner's policy remains available to execute")
			epoch, err := f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
			require.NoError(t, err, "settled obsolete workflow must permit future configuration")
			require.NoError(t, f.service.writes.ReleaseHost(ctx, f.target.OrganizationID, f.target.DeviceID, epoch))
		})
	}
}
