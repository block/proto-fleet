package mininggroups

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAutomaticBoardDefaultUsesMiningGroupExecutor(t *testing.T) {
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
	service := poolconfigurations.NewService(f.conn, repo, f.service.pools, reviewer)
	config, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Automatic", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://automatic.example:3333", Username: "operator"}}})
	require.NoError(t, err)
	var id uuid.UUID
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,pairing_cycle_id,configuration_id,revision_id) VALUES($1,$2,7,$3,'pairing',(SELECT cycle_id FROM pool_pairing_cycle WHERE device_id=$2),$4,$5) RETURNING transition_id`, f.target.OrganizationID, f.target.DeviceID, boards[0], config.ID, config.RevisionID).Scan(&id))
	worker := poolapplications.NewWorker(f.conn, repo, reviewer, guarded, f.service.secrets).WithRemovals(checker).WithBoardApplications(f.service)
	require.NoError(t, worker.ProcessLifecycle(ctx, id))
	row, err := sqlc.New(f.conn).GetPoolLifecycleTransition(ctx, id)
	require.NoError(t, err)
	require.True(t, row.OperationID.Valid)
	require.Equal(t, poolconfigurations.TargetInstallation, row.TargetKind)
	require.NoError(t, worker.Execute(ctx, f.target.OrganizationID, row.OperationID.String, poolconfigurations.AssignmentTarget{Kind: row.TargetKind, ID: row.TargetID, DeviceID: row.DeviceID}))
	for i := 0; i < 40 && active(f.state(t).Status); i++ {
		require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
	}
	require.Equal(t, "complete", f.state(t).Status, f.state(t).Message)
	operation, err := service.GetOperation(ctx, "", row.OperationID.String)
	require.NoError(t, err)
	require.Equal(t, "verified", operation.Targets[0].State)
	// Ownership transitions can clear the board without a saved destination configuration.
	assignment, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: row.OrganizationID, TargetKind: row.TargetKind, TargetID: row.TargetID})
	require.NoError(t, err)
	require.NoError(t, db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
		return repo.RetireAssignment(ctx, q, poolconfigurations.Scope{OrganizationID: row.OrganizationID}, poolconfigurations.AssignmentTarget{Kind: row.TargetKind, ID: row.TargetID, DeviceID: row.DeviceID}, assignment.Version)
	}))
	var clearID uuid.UUID
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason) VALUES($1,$2,7,$3,'ownership') RETURNING transition_id`, row.OrganizationID, row.DeviceID, boards[0]).Scan(&clearID))
	require.NoError(t, worker.ProcessLifecycle(ctx, clearID))
	for i := 0; i < 40 && active(f.state(t).Status); i++ {
		require.NoError(t, f.service.Reconcile(ctx, row.OrganizationID, row.DeviceID, f.target.DeviceIdentifier))
	}
	require.Equal(t, "complete", f.state(t).Status, f.state(t).Message)
	cleared, err := sqlc.New(f.conn).GetPoolLifecycleTransition(ctx, clearID)
	require.NoError(t, err)
	require.Equal(t, "completed", cleared.State)
}

func TestAutomaticClearMovesUnmanagedBoardOutOfDefaultGroup(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	_, boards := preparePermissionPoolAssignment(t, ctx, f)
	var sibling int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.target.OrganizationID).Scan(&sibling))
	_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata) SELECT $1,module_id,2,now(),now(),'{"macAddress":"02:00:00:00:00:02"}' FROM hashboard_installation WHERE hashboard_id=$2 AND removed_at IS NULL`, sibling, boards[0])
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) SELECT module_id,hashboard_id,position,'occupied',last_seen_at FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, sibling)
	require.NoError(t, err)
	publishSharedBoardInventory(t, ctx, f)
	const selectedMAC = "02:00:00:00:00:01"
	const siblingMAC = "02:00:00:00:00:02"
	firmware := &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 4, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, Mining: true, MACs: []string{selectedMAC, siblingMAC}, Pools: []sdk.MiningGroupPool{{URL: "stratum+tcp://previous.example:3333", Username: "previous"}}}}}, receipts: map[string]sdk.MiningGroupResult{}}
	guarded := &admittedGroups{firmware}
	f.service.miners = guarded
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	repo := poolconfigurations.NewRepository(f.service.secrets)
	reviewer := poolapplications.NewReviewer(f.service.pools, removalCapabilities{})
	checker := poolapplications.NewChecker(f.conn, reviewer, guarded, f.service.secrets, f.service.pools)
	f.service.WithPoolConfigurations(repo, reviewer).WithPoolRemovals(checker)
	var id uuid.UUID
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason) VALUES($1,$2,7,$3,'ownership') RETURNING transition_id`, f.target.OrganizationID, f.target.DeviceID, boards[0]).Scan(&id))
	worker := poolapplications.NewWorker(f.conn, repo, reviewer, guarded, f.service.secrets).WithRemovals(checker).WithBoardApplications(f.service)
	require.NoError(t, worker.ProcessLifecycle(ctx, id))
	for i := 0; i < 40 && active(f.state(t).Status); i++ {
		require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
	}
	require.Equal(t, "complete", f.state(t).Status, f.state(t).Message)
	cleared, err := sqlc.New(f.conn).GetPoolLifecycleTransition(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "completed", cleared.State)
	for _, group := range firmware.snapshot.Groups {
		if group.ID == "default" {
			require.Equal(t, "previous", group.Pools[0].Username)
			require.True(t, group.Mining)
			require.Equal(t, []string{siblingMAC}, group.MACs, "the unselected sibling retains its pools and mining state")
		}
		for _, mac := range group.MACs {
			if mac == selectedMAC {
				require.Empty(t, group.Pools)
				require.False(t, group.Mining)
			}
		}
	}
	// A later manual assignment followed by keep-device-settings removal must
	// never resurrect this automatic clear in future mining-group plans.
	scope := poolconfigurations.Scope{OrganizationID: f.target.OrganizationID}
	target := poolconfigurations.AssignmentTarget{Kind: cleared.TargetKind, ID: cleared.TargetID, DeviceID: cleared.DeviceID}
	require.NoError(t, db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
		config, err := repo.Create(ctx, q, scope, poolconfigurations.RevisionOptions{Name: "Manual", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://manual.example", Username: "manual"}}})
		if err != nil {
			return err
		}
		_, err = repo.Assign(ctx, q, scope, 1, config.ID, config.RevisionID, []poolconfigurations.AssignmentChange{{Target: target}})
		return err
	}))
	require.NoError(t, db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
		return repo.RetireAssignment(ctx, q, scope, target, 1)
	}))
	intents, err := sqlc.New(f.conn).ListClearedBoardPoolIntents(ctx, sqlc.ListClearedBoardPoolIntentsParams{OrganizationID: scope.OrganizationID, DeviceID: target.DeviceID})
	require.NoError(t, err)
	require.Empty(t, intents)
	visible, err := sqlc.New(f.conn).ListVisiblePoolLifecycleTransitions(ctx, sqlc.ListVisiblePoolLifecycleTransitionsParams{OrganizationID: scope.OrganizationID, InstallationIds: []int64{target.ID}})
	require.NoError(t, err)
	require.Empty(t, visible)
}
