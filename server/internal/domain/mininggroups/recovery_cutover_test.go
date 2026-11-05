package mininggroups

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations/migration"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type recoveryCutoverGroups struct {
	*simulatedGroups
	partial, failedTransfer bool
}

func (m *recoveryCutoverGroups) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}

func (m *recoveryCutoverGroups) MiningGroups(ctx context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	if request.Action != "create" && request.Action != "transfer" {
		return m.simulatedGroups.MiningGroups(ctx, request)
	}
	m.calls = append(m.calls, request)
	if prior, ok := m.receipts[request.Key]; ok {
		return prior, nil
	}
	var result sdk.MiningGroupResult
	if request.Action == "create" {
		group := sdk.MiningGroup{ID: fmt.Sprintf("fleet-group-%d", len(m.snapshot.Groups)), Revision: 1}
		m.snapshot.Groups = append(m.snapshot.Groups, group)
		result.Group = &group
	} else {
		macs := request.MACs
		phase := transferPhaseComplete
		if m.partial {
			phase = transferPhaseFailed
			macs = nil
			m.failedTransfer = true
		}
		for index := range m.snapshot.Groups {
			group := &m.snapshot.Groups[index]
			if group.ID == request.SourceID {
				group.MACs = slices.DeleteFunc(group.MACs, func(mac string) bool { return slices.Contains(macs, mac) })
				group.Revision++
			}
			if group.ID == request.GroupID {
				group.MACs = append(group.MACs, macs...)
				group.Revision++
			}
		}
		result.Operation = &sdk.MiningGroupOperation{ID: request.Key, Phase: phase}
	}
	m.receipts[request.Key] = result
	return result, nil
}

func TestPostCutoverRecoveryRetainsPartialReplacementDuringUnrelatedBoardUpdate(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	_, boards := preparePermissionPoolAssignment(t, ctx, f)
	for position := 2; position <= 3; position++ {
		var board int64
		require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.target.OrganizationID).Scan(&board))
		_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata)
 SELECT $1,module_id,$3,now(),now(),jsonb_build_object('macAddress',$4::text) FROM hashboard_installation WHERE hashboard_id=$2 AND removed_at IS NULL`, board, boards[0], position, fmt.Sprintf("02:00:00:00:00:%02d", position))
		require.NoError(t, err)
		_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at)
 SELECT module_id,$1,position,'occupied',now() FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, board)
		require.NoError(t, err)
		boards = append(boards, board)
	}
	publishSharedBoardInventory(t, ctx, f)
	firmware := &recoveryCutoverGroups{simulatedGroups: &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 8, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, Mining: true, MACs: []string{"02:00:00:00:00:01", "02:00:00:00:00:02", "02:00:00:00:00:03"}}}}, receipts: map[string]sdk.MiningGroupResult{}}}
	f.service.miners = firmware
	_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	repo := poolconfigurations.NewRepository(f.service.secrets)
	reviewer := poolapplications.NewReviewer(f.service.pools, sharedCapabilities{})
	f.service.WithPoolConfigurations(repo, reviewer)
	service := poolconfigurations.NewService(f.conn, repo, f.service.pools, reviewer)
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	confirm := func(config poolconfigurations.Configuration, selected []int64) poolconfigurations.ReviewedSave {
		t.Helper()
		review, err := service.ReviewBoards(ctx, "", config.ID, config.Version, poolconfigurations.BoardSelection{HashboardIDs: selected})
		require.NoError(t, err)
		saved, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (poolconfigurations.ReviewedSave, error) {
			return repo.ConfirmReview(ctx, q, poolconfigurations.Scope{OrganizationID: info.OrganizationID}, info.UserID, review.ID, reviewer, nil)
		})
		require.NoError(t, err)
		require.NoError(t, f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, saved.Operation.ID, f.target.DeviceID))
		return saved
	}
	finish := func() {
		t.Helper()
		for iteration := 0; iteration < 40 && active(f.state(t).Status); iteration++ {
			f.reconcile(t, ctx)
		}
		require.Equal(t, "complete", f.state(t).Status)
	}
	canonical := func() map[int64]string {
		t.Helper()
		intents, err := f.service.queries.ListPoolGroupIntents(ctx, sqlc.ListPoolGroupIntentsParams{OrganizationID: info.OrganizationID, DeviceID: f.target.DeviceID})
		require.NoError(t, err)
		result := map[int64]string{}
		for _, intent := range intents {
			result[intent.HashboardID] = intent.GroupSnapshotID.String
		}
		return result
	}
	oldConfig, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Originally applied", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://original.example:3333", Username: "old"}}})
	require.NoError(t, err)
	confirm(oldConfig, boards[:1])
	finish()
	otherOldConfig, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Other original group", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://other-original.example:3333", Username: "other-old"}}})
	require.NoError(t, err)
	confirm(otherOldConfig, boards[1:2])
	finish()
	original := canonical()
	require.Len(t, original, 2)
	require.NotEqual(t, original[boards[0]], original[boards[1]])
	require.NoError(t, db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error {
		if _, err := migration.Import(ctx, q, f.service.secrets, info.OrganizationID); err != nil {
			return err
		}
		if err := migration.FreezeCutover(ctx, q, info.OrganizationID); err != nil {
			return err
		}
		_, err := migration.CompleteCutover(ctx, q, info.OrganizationID)
		return err
	}))
	complete, err := f.service.queries.HasPoolConfigurationCutover(ctx, info.OrganizationID)
	require.NoError(t, err)
	require.True(t, complete)
	newConfig, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Replacement", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://replacement.example:3333", Username: "new"}}})
	require.NoError(t, err)
	firmware.partial = true
	replacement := confirm(newConfig, boards[:2])
	for iteration := 0; iteration < 30 && !firmware.failedTransfer; iteration++ {
		f.reconcile(t, ctx)
	}
	require.True(t, firmware.failedTransfer)
	f.reconcile(t, ctx) // Observe A's successful replacement while B's move failed.
	interrupted := f.state(t)
	require.True(t, active(interrupted.Status))
	require.True(t, interrupted.Step.Valid, "the next unapplied transfer is saved before recovery")
	calls := len(firmware.calls)
	_, err = f.service.writes.ResolveOffline(ctx, recoveryEvidence(t, ctx, f), true)
	require.NoError(t, err)
	require.Len(t, firmware.calls, calls, "offline recovery must not perform controller I/O")
	recovered, err := service.GetOperation(ctx, "", replacement.Operation.ID)
	require.NoError(t, err)
	for _, target := range recovered.Targets {
		require.Equal(t, "failed", target.State)
	}
	afterRecovery := canonical()
	require.Len(t, afterRecovery, 2)
	require.NotEqual(t, original[boards[0]], afterRecovery[boards[0]], "the successfully replaced board must retain its new execution snapshot")
	require.Equal(t, original[boards[1]], afterRecovery[boards[1]], "the board not moved must retain its previous applied configuration")
	f.reconcile(t, ctx)
	require.Len(t, firmware.calls, calls, "the recovered saved step must not replay")
	firmware.partial = false
	unrelated := confirm(newConfig, boards[2:])
	finish()
	operation, err := service.GetOperation(ctx, "", unrelated.Operation.ID)
	require.NoError(t, err)
	require.Equal(t, "verified", operation.Targets[0].State)
	after := canonical()
	require.Equal(t, afterRecovery[boards[0]], after[boards[0]])
	require.Equal(t, afterRecovery[boards[1]], after[boards[1]])
	require.Equal(t, afterRecovery[boards[0]], after[boards[2]])
	for _, call := range firmware.calls[calls:] {
		require.NotEqual(t, interrupted.StepKey.UUID.String(), call.Key, "a fresh request must not replay the recovered mutation identity")
		if call.Action == "transfer" {
			require.Equal(t, []string{"02:00:00:00:00:03"}, call.MACs, "unrelated board work must not restore superseded settings on recovered boards")
		} else {
			require.Contains(t, []string{"observe", "operation"}, call.Action, "retained groups must not be recreated or reconfigured")
		}
	}
	desired, err := f.service.liveIntent(ctx, sqlc.NewRetryingQuerier(f.service.queries, canonicalQueryGuard{}), f.state(t), nil)
	require.NoError(t, err)
	require.Len(t, desired, 3)
}
