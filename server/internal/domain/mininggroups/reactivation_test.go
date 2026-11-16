package mininggroups

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type modeCyclePoolMiner struct {
	*activationPoolMiner
}

func (m *modeCyclePoolMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}

func (m *modeCyclePoolMiner) MiningGroups(ctx context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	switch request.Action {
	case operationDeactivate:
		m.snapshot.Enabled = false
		m.snapshot.Groups = nil
		return sdk.MiningGroupResult{Deactivated: true}, nil
	case operationActivate:
		m.snapshot.Groups = []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01"}}}
	}
	return m.activationPoolMiner.MiningGroups(ctx, request)
}

func TestReactivationAfterDeactivationReleasesOldGroupBindings(t *testing.T) {
	for _, newSource := range []bool{false, true} {
		name := "same installations"
		if newSource {
			name = "new installation and container assignment"
		}
		t.Run(name, func(t *testing.T) {
			ctx, f, miner, service, config, _, confirm := newActivationPoolFixture(t)
			f.service.miners = &modeCyclePoolMiner{miner}
			activationID := uuid.New()
			_, err := f.service.Activate(ctx, f.target.DeviceIdentifier, activationID.String())
			require.NoError(t, err)
			finish := func() {
				t.Helper()
				for iteration := 0; iteration < 12 && active(f.state(t).Status); iteration++ {
					f.reconcile(t, ctx)
				}
				require.Equal(t, statusComplete, f.state(t).Status)
			}
			finish()
			bindings := map[string]binding{}
			require.NoError(t, decode(f.state(t).Bindings, &bindings))
			require.True(t, bindings["default"].Inherited)
			priorSnapshot := bindings["default"].ConfigID
			// The fixture's original pairing policy is separate from its completed
			// pool application. Settle it before admitting deactivation.
			_, err = f.conn.ExecContext(ctx, `UPDATE pool_lifecycle_transition SET state='completed' WHERE device_id=$1`, f.target.DeviceID)
			require.NoError(t, err)
			deactivationID := uuid.New()
			_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, deactivationID.String())
			require.NoError(t, err)
			finish()
			require.False(t, miner.snapshot.Enabled)
			require.JSONEq(t, `{}`, string(f.state(t).Bindings))
			_, err = sqlc.New(f.conn).GetPoolGroupSnapshot(ctx, sqlc.GetPoolGroupSnapshotParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID, SnapshotID: priorSnapshot})
			require.NoError(t, err, "deactivation retains encrypted pool evidence")
			handoff, err := sqlc.New(f.conn).GetMiningGroupPoolHandoff(ctx, sqlc.GetMiningGroupPoolHandoffParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID, OperationID: activationID})
			require.NoError(t, err)
			require.True(t, handoff.CompletedAt.Valid)
			_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, deactivationID.String())
			require.NoError(t, err, "completed request replay keeps its original identity")

			if newSource {
				// Accepted removal retires the old installation's assignment. Its
				// returning board receives a new installation generation.
				var oldInstallation int64
				require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT id FROM hashboard_installation WHERE device_id=$1 AND removed_at IS NULL`, f.target.DeviceID).Scan(&oldInstallation))
				_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_installation SET removed_at=clock_timestamp() WHERE id=$1`, oldInstallation)
				require.NoError(t, err)
				require.NoError(t, sqlc.New(f.conn).RetirePoolConfigurationEquipmentAssignments(ctx, sqlc.RetirePoolConfigurationEquipmentAssignmentsParams{OrganizationID: f.target.OrganizationID, InstallationIds: []int64{oldInstallation}}))
				_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata)
 SELECT hashboard_id,module_id,position,clock_timestamp(),clock_timestamp(),metadata FROM hashboard_installation WHERE id=$1`, oldInstallation)
				require.NoError(t, err)
				publishSharedBoardInventory(t, ctx, f)
				review, err := service.ReviewDevices(ctx, "", config.ID, config.Version, []string{f.target.DeviceIdentifier})
				require.NoError(t, err)
				saved := confirm(review)
				worker := poolapplications.NewWorker(f.conn, f.service.sharedRepository, f.service.sharedReviewer, miner, f.service.secrets)
				require.NoError(t, worker.Execute(ctx, f.target.OrganizationID, saved.Operation.ID, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: f.target.DeviceID, DeviceID: f.target.DeviceID}))
			}
			_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
			require.NoError(t, err)
			_, err = f.service.Activate(ctx, f.target.DeviceIdentifier, uuid.NewString())
			require.NoError(t, err)
			finish()
			require.True(t, miner.snapshot.Enabled)
			if newSource {
				require.NoError(t, decode(f.state(t).Bindings, &bindings))
				require.True(t, bindings["default"].Inherited)
				require.NotEqual(t, priorSnapshot, bindings["default"].ConfigID)
			}
		})
	}
}

func TestDeactivationKeepsBindingsUntilSavedRequestSettles(t *testing.T) {
	ctx, f := deactivationFixture(t)
	const bindings = `{"default":{"config_id":"retained","configured":true,"inherited":true},"old-empty-group":{"config_id":"retained","configured":false}}`
	_, err := f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET bindings=$2 WHERE device_id=$1`, f.target.DeviceID, bindings)
	require.NoError(t, err)
	f.miner.loseDeactivate = true
	_, err = f.service.Deactivate(ctx, f.target.DeviceIdentifier, uuid.NewString())
	require.NoError(t, err)
	f.reconcile(t, ctx)
	f.reconcile(t, ctx)
	require.Equal(t, "blocked", f.state(t).Status)
	require.False(t, f.miner.enabled)
	require.True(t, f.state(t).Step.Valid)
	require.JSONEq(t, bindings, string(f.state(t).Bindings))
	f.reconcile(t, ctx)
	require.False(t, f.state(t).Step.Valid)
	require.JSONEq(t, bindings, string(f.state(t).Bindings))
	f.reconcile(t, ctx)
	require.Equal(t, statusComplete, f.state(t).Status)
	require.JSONEq(t, `{}`, string(f.state(t).Bindings))
}
