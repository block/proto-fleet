package mininggroups

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAutomaticClearRepreparesWholeContainerAsBoardAfterModeChange(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	_, boards := preparePermissionPoolAssignment(t, ctx, f)
	publishSharedBoardInventory(t, ctx, f)
	firmware := &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 4, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01"}}}}, receipts: map[string]sdk.MiningGroupResult{}}
	guarded := &admittedGroups{firmware}
	f.service.miners = guarded
	_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	repo := poolconfigurations.NewRepository(f.service.secrets)
	reviewer := poolapplications.NewReviewer(f.service.pools, removalCapabilities{})
	checker := poolapplications.NewChecker(f.conn, reviewer, guarded, f.service.secrets, f.service.pools)
	f.service.WithPoolConfigurations(repo, reviewer).WithPoolRemovals(checker)
	var id uuid.UUID
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason) VALUES($1,$2,7,$3,'ownership') RETURNING transition_id`, f.target.OrganizationID, f.target.DeviceID, boards[0]).Scan(&id))
	// A previous preparation selected the whole container with groups disabled,
	// but no attempt was admitted. Groups are now enabled; the original asset is
	// still a single board, so retry must discard the whole-container target.
	_, err = sqlc.New(f.conn).PreparePoolLifecycleTransition(ctx, sqlc.PreparePoolLifecycleTransitionParams{TransitionID: id, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID, ReviewedBinding: "{}"})
	require.NoError(t, err)
	worker := poolapplications.NewWorker(f.conn, repo, reviewer, guarded, f.service.secrets).WithRemovals(checker).WithBoardApplications(f.service)
	require.NoError(t, worker.ProcessLifecycle(ctx, id))
	for i := 0; i < 40 && active(f.state(t).Status); i++ {
		require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
	}
	row, err := sqlc.New(f.conn).GetPoolLifecycleTransition(ctx, id)
	require.NoError(t, err)
	require.Equal(t, poolconfigurations.TargetInstallation, row.TargetKind)
	require.Equal(t, "completed", row.State)
	require.Equal(t, "complete", f.state(t).Status)
}
