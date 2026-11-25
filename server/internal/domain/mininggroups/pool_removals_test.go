package mininggroups

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type removalCapabilities struct{}

func (removalCapabilities) GetRawCapabilitiesForDevice(context.Context, string, string, string) sdk.Capabilities {
	return sdk.Capabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolPriority: true, sdk.CapabilityMiningGroupPoolClear: true}
}

// Exercise the same committed-admission boundary as the production plugin.
// Bare controller fakes cannot catch reads made before a workflow commits.
type admittedGroups struct{ *simulatedGroups }

func (m *admittedGroups) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}
func (m *admittedGroups) MiningGroups(ctx context.Context, r sdk.MiningGroupRequest) (result sdk.MiningGroupResult, err error) {
	call := func(ctx context.Context) error { result, err = m.simulatedGroups.MiningGroups(ctx, r); return err }
	if r.ReadOnly() {
		err = devicewrite.Observe(ctx, call)
	} else {
		err = devicewrite.Dispatch(ctx, call)
	}
	return result, err
}

func TestBoardPoolRemovalRegroupsAndPreservesOtherAssignments(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	_, boards := preparePermissionPoolAssignment(t, ctx, f)
	var tank, module int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT m.tank_id,m.id FROM container_module m JOIN hashboard_installation i ON i.module_id=m.id WHERE i.hashboard_id=$1`, boards[0]).Scan(&tank, &module))
	for i := 2; i <= 4; i++ {
		if i == 4 {
			require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at) VALUES($1,2,'module-b',now(),now()) RETURNING id`, tank).Scan(&module))
		}
		var board int64
		require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.target.OrganizationID).Scan(&board))
		position := i
		if i == 4 {
			position = 1
		}
		_, err := f.conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata) VALUES($1,$2,$3,now(),now(),$4)`, board, module, position, fmt.Sprintf(`{"macAddress":"02:00:00:00:00:%02d"}`, i))
		require.NoError(t, err)
		_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) VALUES($1,$2,$3,'occupied',now())`, module, board, position)
		require.NoError(t, err)
		boards = append(boards, board)
	}
	// Publish the exact current physical site as production inventory does.
	_, err := f.conn.ExecContext(ctx, `UPDATE hashboard_installation i SET site_id=d.site_id FROM device d WHERE i.device_id=d.id AND d.id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE container_module m SET observation_site_id=d.site_id FROM container_tank tank,device d WHERE m.tank_id=tank.id AND tank.device_id=d.id AND d.id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	publishSharedBoardInventory(t, ctx, f)
	const macA, macB, macC, macD = "02:00:00:00:00:01", "02:00:00:00:00:02", "02:00:00:00:00:03", "02:00:00:00:00:04"
	macs := []string{macA, macB, macC, macD}
	firmware := &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 5, Groups: []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: slices.Clone(macs)}}}, receipts: map[string]sdk.MiningGroupResult{}}
	guarded := &admittedGroups{firmware}
	f.service.miners = guarded
	_, err = f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	repo := poolconfigurations.NewRepository(f.service.secrets)
	reviewer := poolapplications.NewReviewer(f.service.pools, removalCapabilities{})
	checker := poolapplications.NewChecker(f.conn, reviewer, guarded, f.service.secrets, f.service.pools)
	f.service.WithPoolConfigurations(repo, reviewer).WithPoolRemovals(checker)
	service := poolconfigurations.NewService(f.conn, repo, f.service.pools, reviewer).WithChecker(checker)
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	scope := poolconfigurations.Scope{OrganizationID: info.OrganizationID}
	settle := func() {
		for i := 0; i < 40 && active(f.state(t).Status); i++ {
			require.NoError(t, f.service.Reconcile(ctx, info.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
		}
		require.Equal(t, "complete", f.state(t).Status, f.state(t).Message)
	}
	applyReview := func(review poolconfigurations.Review) {
		saved, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (poolconfigurations.ReviewedSave, error) {
			return repo.ConfirmReview(ctx, q, scope, info.UserID, review.ID, reviewer, nil)
		})
		require.NoError(t, err)
		require.NoError(t, f.service.ExecutePoolConfiguration(ctx, info.OrganizationID, saved.Operation.ID, f.target.DeviceID))
		settle()
	}
	config1, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "One", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://one.example:3333", Username: "one"}}})
	require.NoError(t, err)
	config2, err := service.Create(ctx, "", poolconfigurations.RevisionOptions{Name: "Two", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://two.example:3333", Username: "two"}}})
	require.NoError(t, err)
	review, err := service.ReviewBoards(ctx, "", config1.ID, config1.Version, poolconfigurations.BoardSelection{HashboardIDs: boards[:2]})
	require.NoError(t, err)
	applyReview(review)
	review, err = service.ReviewBoards(ctx, "", config2.ID, config2.Version, poolconfigurations.BoardSelection{HashboardIDs: boards[2:]})
	require.NoError(t, err)
	applyReview(review)
	groupFor := func(mac string) sdk.MiningGroup {
		for _, g := range firmware.snapshot.Groups {
			for _, member := range g.MACs {
				if member == mac {
					return g
				}
			}
		}
		t.Fatalf("missing group for %s", mac)
		return sdk.MiningGroup{}
	}
	first, second := groupFor(macA), groupFor(macC)
	require.ElementsMatch(t, []string{macA, macB}, first.MACs)
	require.ElementsMatch(t, []string{macC, macD}, second.MACs)
	var installation int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL`, boards[1]).Scan(&installation))
	target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetInstallation, ID: installation, DeviceID: f.target.DeviceID}
	removal, err := service.ReviewRemoval(ctx, "", target)
	require.NoError(t, err)
	require.True(t, removal.ClearSupported)
	require.Equal(t, "verified", removal.VerificationState)
	p, err := f.service.pools.AuthorizePoolConfiguration(ctx, sqlc.New(f.conn), "", poolconfigurations.Read)
	require.NoError(t, err)
	require.NoError(t, db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error { return checker.EnqueueRemoval(ctx, q, p, removal.ID, false) }))
	firmware.lostCreate = false // lose this creation receipt too, exercising exact-key replay.
	require.NoError(t, poolapplications.NewWorker(f.conn, repo, reviewer, guarded, f.service.secrets).WithRemovals(checker).Execute(ctx, info.OrganizationID, removal.ID, target))
	firmware.runtimePoolsRemain = true
	for range 40 {
		require.NoError(t, f.service.Reconcile(ctx, info.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
		if mustRemoval(t, ctx, service, removal.ID).ResultCode == "runtime_pools_remain" {
			break
		}
	}
	pending := mustRemoval(t, ctx, service, removal.ID)
	require.Equal(t, "uncertain", pending.State)
	require.Equal(t, "runtime_pools_remain", pending.ResultCode)
	journal, err := sqlc.New(f.conn).GetMiningGroupState(ctx, sqlc.GetMiningGroupStateParams{OrganizationID: info.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Equal(t, "blocked", journal.Status)
	require.Contains(t, journal.Message, "Runtime pools are still present")
	var physicalStatus string
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT status FROM device_write_attempt WHERE attempt_id=$1`, journal.AttemptID).Scan(&physicalStatus))
	require.Equal(t, "running", physicalStatus, "verification retains the workflow reservation")
	// Repeated verification leaves an actionable result, then completes normally
	// when firmware catches up. The planner must not issue another clear.
	require.NoError(t, f.service.Reconcile(ctx, info.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
	a, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: info.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
	require.NoError(t, err)
	require.False(t, a.RetiredAt.Valid, "keep assignment until runtime verification")
	require.Equal(t, "uncertain", mustRemoval(t, ctx, service, removal.ID).State)
	mutations := func() int {
		count := 0
		for _, call := range firmware.calls {
			if !call.ReadOnly() {
				count++
			}
		}
		return count
	}
	beforeRecovery := mutations()
	firmware.runtimeDiagnosticsError = fmt.Errorf("private firmware error")
	require.NoError(t, f.service.Reconcile(ctx, info.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
	require.Equal(t, "clear_verification_unavailable", mustRemoval(t, ctx, service, removal.ID).ResultCode)
	require.NotContains(t, f.state(t).Message, "private firmware error")
	firmware.runtimeDiagnosticsError = nil
	firmware.runtimePoolsRemain = false
	settle()
	require.Equal(t, beforeRecovery, mutations(), "recovery only observes; no extra clearing or regrouping")
	require.Equal(t, "completed", mustRemoval(t, ctx, service, removal.ID).State)
	var auditEvent string
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT event_type FROM activity_log WHERE metadata->>'removal_id'=$1`, removal.ID).Scan(&auditEvent))
	require.Equal(t, "clear_device_pools_and_unassign", auditEvent)
	require.Equal(t, []string{macA}, groupFor(macA).MACs)
	require.Equal(t, first.Pools, groupFor(macA).Pools)
	require.Equal(t, second, groupFor(macC), "C/D group must not be touched")
	cleared := groupFor(macB)
	require.Empty(t, cleared.Pools)
	require.False(t, cleared.Mining)
	require.Equal(t, []string{macB}, cleared.MACs)
	require.Len(t, firmware.snapshot.Groups, 4, "three desired groups plus firmware's empty default group")
	// Editing the old configuration must not restore pools on the cleared board.
	emptyPassword := ""
	edit, err := service.ReviewEdit(ctx, "", config1.ID, config1.Version, poolconfigurations.RevisionOptions{Name: "One", Slots: []poolconfigurations.SlotOptions{{ID: config1.Slots[0].ID, URL: "stratum+tcp://updated.example:3333", Username: "one", Password: &emptyPassword}}})
	require.NoError(t, err)
	require.Len(t, edit.Targets, 1)
	applyReview(edit)
	require.Equal(t, cleared, groupFor(macB))
	// Explicit reassignment rejoins A and removes the now-empty pool-free group.
	updated, err := service.Get(ctx, "", config1.ID)
	require.NoError(t, err)
	review, err = service.ReviewBoards(ctx, "", config1.ID, updated.Version, poolconfigurations.BoardSelection{HashboardIDs: boards[1:2]})
	require.NoError(t, err)
	applyReview(review)
	require.Equal(t, groupFor(macA).ID, groupFor(macB).ID)
	require.Len(t, firmware.snapshot.Groups, 3)
}

func mustRemoval(t *testing.T, ctx context.Context, service *poolconfigurations.Service, id string) poolconfigurations.Removal {
	t.Helper()
	r, err := service.GetRemoval(ctx, "", id)
	require.NoError(t, err)
	return r
}

func TestBoardPoolRemovalInPlaceAndRevokedAuthority(t *testing.T) {
	for _, scenario := range []string{"verified", "revoked", "default group", "default group without diagnostics", "default group after review"} {
		t.Run(scenario, func(t *testing.T) {
			revoke := scenario == "revoked"
			ctx, f := newIdleRetryFixture(t)
			service, saved, firmware := newConfirmedSharedBoardApplication(t, ctx, f)
			guarded := &admittedGroups{firmware}
			f.service.miners = guarded
			reviewer := poolapplications.NewReviewer(f.service.pools, removalCapabilities{})
			checker := poolapplications.NewChecker(f.conn, reviewer, guarded, f.service.secrets, f.service.pools)
			f.service.WithPoolRemovals(checker)
			service.WithChecker(checker)
			require.NoError(t, f.service.ExecutePoolConfiguration(ctx, f.target.OrganizationID, saved.Operation.ID, f.target.DeviceID))
			for i := 0; i < 30 && active(f.state(t).Status); i++ {
				require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
			}
			require.Equal(t, "complete", f.state(t).Status)
			target := poolconfigurations.AssignmentTarget{Kind: saved.Operation.Targets[0].Kind, ID: saved.Operation.Targets[0].ID, DeviceID: f.target.DeviceID}
			if !revoke {
				firmware.runtimeDiagnosticsError = fleeterror.NewUnimplementedError("group diagnostics unsupported")
				review, err := service.ReviewRemoval(ctx, "", target)
				require.NoError(t, err)
				require.Equal(t, "verified", review.VerificationState, "missing clear diagnostics must not invalidate matching pool settings")
				require.False(t, review.ClearSupported)
				_, err = service.ConfirmReview(ctx, "", review.ID, poolconfigurations.Confirmation{})
				require.Error(t, err, "unsupported clear must remain blocked")
				firmware.runtimeDiagnosticsError = nil
			}
			beforeReview := len(firmware.calls)
			if scenario == "default group" || scenario == "default group without diagnostics" {
				// The simulator reset its groups while Fleet retained the assignment.
				firmware.snapshot.Groups = []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01"}}}
			}
			if scenario == "default group without diagnostics" {
				firmware.runtimeDiagnosticsError = fleeterror.NewUnimplementedError("group diagnostics unsupported")
			}
			removal, err := service.ReviewRemoval(ctx, "", target)
			require.NoError(t, err)
			if scenario == "default group" || scenario == "default group without diagnostics" {
				require.False(t, removal.ClearSupported)
				require.Equal(t, "different", removal.VerificationState)
				_, err = service.ConfirmReview(ctx, "", removal.ID, poolconfigurations.Confirmation{})
				require.Error(t, err)
				for _, call := range firmware.calls[beforeReview:] {
					require.True(t, call.ReadOnly(), "unsupported scope must not mutate the controller")
				}
				return
			}
			require.True(t, removal.ClearSupported)
			p, err := f.service.pools.AuthorizePoolConfiguration(ctx, sqlc.New(f.conn), "", poolconfigurations.Read)
			require.NoError(t, err)
			row, err := sqlc.New(f.conn).GetPoolRemoval(ctx, sqlc.GetPoolRemovalParams{RemovalID: uuid.MustParse(removal.ID), OrganizationID: p.Scope.OrganizationID, ActorID: p.ActorID})
			require.NoError(t, err)
			require.NoError(t, db.WithTransactionNoResult(ctx, f.conn, func(q sqlc.Querier) error { return checker.EnqueueRemoval(ctx, q, p, removal.ID, false) }))
			ctx = devicewrite.WithPoolOperation(ctx, uuid.MustParse(removal.ID))
			if scenario == "default group after review" {
				firmware.snapshot.Groups = []sdk.MiningGroup{{ID: "default", Revision: 1, MACs: []string{"02:00:00:00:00:01"}}}
				before := len(firmware.calls)
				require.Error(t, f.service.BeginBoardRemoval(ctx, p, row, func(context.Context, sqlc.Querier) error { return nil }))
				for _, call := range firmware.calls[before:] {
					require.True(t, call.ReadOnly())
				}
				require.Equal(t, "pending", mustRemoval(t, ctx, service, removal.ID).State)
				return
			}
			require.NoError(t, f.service.BeginBoardRemoval(ctx, p, row, func(context.Context, sqlc.Querier) error { return nil }))
			require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
			require.True(t, f.state(t).Step.Valid)
			before := len(firmware.calls)
			if revoke {
				setMiningGroupPermissions(t, ctx, f, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermPoolRead, authz.PermMinerStartMining, authz.PermMinerStopMining})
			}
			for i := 0; i < 20 && active(f.state(t).Status); i++ {
				require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
			}
			if revoke {
				require.Equal(t, "blocked", f.state(t).Status)
				for _, call := range firmware.calls[before:] {
					require.True(t, call.ReadOnly(), "revoked authority dispatched %s", call.Action)
				}
				assignment, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: p.Scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
				require.NoError(t, err)
				require.False(t, assignment.RetiredAt.Valid)
				return
			}
			require.Equal(t, "complete", f.state(t).Status)
			require.Equal(t, "completed", mustRemoval(t, ctx, service, removal.ID).State)
			require.Len(t, firmware.snapshot.Groups, 2, "reuse the fully selected managed group")
			require.Empty(t, firmware.snapshot.Groups[1].Pools)
			require.False(t, firmware.snapshot.Groups[1].Mining)
		})
	}
}
