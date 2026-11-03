package mininggroups

import (
	"context"
	"slices"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

// A terminal controller failure can follow a partial physical move. Later
// observations disclose which board reached the configured destination.
type partiallyAppliedGroups struct {
	*simulatedGroups
	moved bool
}

func (m *partiallyAppliedGroups) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}

func (m *partiallyAppliedGroups) MiningGroups(ctx context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	if request.Action != "transfer" {
		return m.simulatedGroups.MiningGroups(ctx, request)
	}
	m.calls = append(m.calls, request)
	if prior, ok := m.receipts[request.Key]; ok {
		return prior, nil
	}
	if !m.moved {
		mac := request.MACs[0]
		for index := range m.snapshot.Groups {
			group := &m.snapshot.Groups[index]
			if group.ID == request.SourceID {
				group.MACs = slices.DeleteFunc(group.MACs, func(member string) bool { return member == mac })
				group.Revision++
			}
			if group.ID == request.GroupID {
				group.MACs = append(group.MACs, mac)
				group.Revision++
			}
		}
		m.moved = true
	}
	result := sdk.MiningGroupResult{Operation: &sdk.MiningGroupOperation{ID: request.Key, Phase: transferPhaseFailed}}
	m.receipts[request.Key] = result
	return result, nil
}

func TestCanonicalAppliedBoardSurvivesFailedWorkflowAndLaterRejectedRequest(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	_, boards := preparePermissionPoolAssignment(t, ctx, f)
	var secondBoard int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.target.OrganizationID).Scan(&secondBoard))
	_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata)
 SELECT $1,module_id,2,now(),now(),'{"macAddress":"02:00:00:00:00:02"}' FROM hashboard_installation WHERE hashboard_id=$2 AND removed_at IS NULL`, secondBoard, boards[0])
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at)
 SELECT module_id,$1,2,'occupied',now() FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, secondBoard)
	require.NoError(t, err)
	boards = append(boards, secondBoard)
	publishSharedBoardInventory(t, ctx, f)
	firmware := &partiallyAppliedGroups{simulatedGroups: &simulatedGroups{
		snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 4, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01", "02:00:00:00:00:02"}}}},
		receipts: map[string]sdk.MiningGroupResult{}, lostCreate: true,
	}}
	f.service.miners = firmware
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	repo := poolconfigurations.NewRepository(f.service.secrets)
	reviewer := poolapplications.NewReviewer(f.service.pools, sharedCapabilities{})
	f.service.WithPoolConfigurations(repo, reviewer)
	service := poolconfigurations.NewService(f.conn, repo, f.service.pools, reviewer)
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	scope := poolconfigurations.Scope{OrganizationID: info.OrganizationID}
	confirm := func(config poolconfigurations.Configuration, selected []int64) poolconfigurations.ReviewedSave {
		t.Helper()
		review, err := service.ReviewBoards(ctx, "", config.ID, config.Version, poolconfigurations.BoardSelection{HashboardIDs: selected})
		require.NoError(t, err)
		saved, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (poolconfigurations.ReviewedSave, error) {
			return repo.ConfirmReview(ctx, q, scope, info.UserID, review.ID, reviewer)
		})
		require.NoError(t, err)
		return saved
	}
	config, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Partially applied", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://applied.example:3333", Username: "worker"}}})
	require.NoError(t, err)
	first := confirm(config, boards)
	require.NoError(t, f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, first.Operation.ID, f.target.DeviceID))
	for iteration := 0; iteration < 40 && active(f.state(t).Status); iteration++ {
		f.reconcile(t, ctx)
	}
	require.Equal(t, "failed", f.state(t).Status)
	operation, err := service.GetOperation(ctx, "", first.Operation.ID)
	require.NoError(t, err)
	require.Len(t, operation.Targets, 2)
	for _, target := range operation.Targets {
		require.Equal(t, "failed", target.State, "workflow outcome must not be the only record of physical effects")
	}
	require.Equal(t, []string{"02:00:00:00:00:01"}, firmware.snapshot.Groups[1].MACs)
	canonical := func() []sqlc.ListPoolGroupIntentsRow {
		t.Helper()
		intents, err := f.service.queries.ListPoolGroupIntents(ctx, sqlc.ListPoolGroupIntentsParams{OrganizationID: info.OrganizationID, DeviceID: f.target.DeviceID})
		require.NoError(t, err)
		return intents
	}
	applied := canonical()
	require.Len(t, applied, 1, "terminal failure must retain the successfully configured board and discard unapplied desire")
	require.Equal(t, boards[0], applied[0].HashboardID)
	require.True(t, applied[0].GroupSnapshotID.Valid)
	material, err := f.service.queries.GetPoolGroupSnapshot(ctx, sqlc.GetPoolGroupSnapshotParams{OrganizationID: info.OrganizationID, DeviceID: f.target.DeviceID, SnapshotID: applied[0].GroupSnapshotID.String})
	require.NoError(t, err)
	require.Equal(t, config.RevisionID, material.RevisionID)

	nextConfig, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Later request", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://later.example:3333", Username: "later"}}})
	require.NoError(t, err)
	next := confirm(nextConfig, boards[:1])
	require.Equal(t, "pending", next.Operation.Targets[0].State)
	require.Equal(t, applied, canonical(), "a newly saved request must not erase previously applied board state")
	setMiningGroupPermissions(t, ctx, f, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermPoolRead, authz.PermMinerStartMining, authz.PermMinerStopMining})
	calls := len(firmware.calls)
	require.NoError(t, f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, next.Operation.ID, f.target.DeviceID))
	rejected, err := service.GetOperation(ctx, "", next.Operation.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", rejected.Targets[0].State)
	require.Equal(t, applied, canonical(), "rejection of a later request must retain the last confirmed execution material")
	for _, call := range firmware.calls[calls:] {
		require.Equal(t, "observe", call.Action, "rejected admission cannot mutate the applied board")
	}
	require.NoError(t, db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
		return repo.RetireAssignment(ctx, q, scope, poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: applied[0].InstallationID, DeviceID: f.target.DeviceID}, next.Operation.Targets[0].AssignmentVersion)
	}))
	require.Empty(t, canonical(), "explicit retirement removes retained intent instead of resurrecting previous assignments")
	setMiningGroupPermissions(t, ctx, f, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermPoolRead, authz.PermMinerUpdatePools, authz.PermMinerStartMining, authz.PermMinerStopMining})
	reactivated := confirm(nextConfig, boards[:1])
	require.Equal(t, "pending", reactivated.Operation.Targets[0].State)
	require.Empty(t, canonical(), "reactivating retired equipment must wait for new confirmed effects instead of reviving old execution material")
}
