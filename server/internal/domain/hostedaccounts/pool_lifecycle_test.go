package hostedaccounts_test

import (
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/stretchr/testify/require"
)

func TestOwnershipChangeDurablySchedulesPoolTransition(t *testing.T) {
	f := newPoolStepUpFixture(t)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	var owner, revision int64
	var clearing bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT owner_account_id,allocation_revision,configuration_id IS NULL FROM pool_lifecycle_transition WHERE device_id=$1 AND state='pending'`, miner).Scan(&owner, &revision, &clearing))
	require.Equal(t, f.account.ID, owner)
	require.EqualValues(t, 1, revision)
	require.True(t, clearing)
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(5, miner, 1)))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT owner_account_id,allocation_revision FROM pool_lifecycle_transition WHERE device_id=$1 AND state='pending'`, miner).Scan(&owner, &revision))
	require.Zero(t, owner)
	require.EqualValues(t, 2, revision)
	var superseded int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_lifecycle_transition WHERE device_id=$1 AND state='superseded' AND reason='ownership'`, miner).Scan(&superseded))
	require.Equal(t, 1, superseded)
}

func TestAutomaticPoolsApplyDestinationDefaultOrClearUnmanagedSettings(t *testing.T) {
	for _, scenario := range []string{"allocation default", "allocation empty", "pairing default", "pairing empty"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			repo := poolconfigurations.NewRepository(cipher)
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolClear: true})
			device := &poolDevice{pools: []dto.MiningPool{{URL: "stratum+tcp://previous.example", Username: "previous-owner", Password: "secret"}}}
			getter := poolMinerGetter{device}
			checker := poolapplications.NewChecker(f.db, reviewer, getter, cipher, f.pools)
			worker := poolapplications.NewWorker(f.db, repo, reviewer, getter, cipher).WithRemovals(checker)
			scope := poolconfigurations.Scope{OrganizationID: f.org, AccountID: f.account.ID}
			pairing := scenario == "pairing default" || scenario == "pairing empty"
			if pairing {
				scope.AccountID = 0
			}
			if scenario == "allocation default" || scenario == "pairing default" {
				_, err := db.WithTransaction(t.Context(), f.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
					return repo.Create(t.Context(), q, scope, poolconfigurations.RevisionOptions{Name: "Default", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://destination.example", Username: "destination"}}})
				})
				require.NoError(t, err)
			}
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			if !pairing {
				require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
			}
			var id uuid.UUID
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND state='pending'`, miner).Scan(&id))
			require.NoError(t, worker.ProcessLifecycle(t.Context(), id))
			row, err := sqlc.New(f.db).GetPoolLifecycleTransition(t.Context(), id)
			require.NoError(t, err)
			if row.OperationID.Valid {
				require.NoError(t, worker.Execute(t.Context(), f.org, row.OperationID.String, poolconfigurations.AssignmentTarget{Kind: row.TargetKind, ID: row.TargetID, DeviceID: miner}))
			}
			if scenario == "pairing empty" {
				require.Zero(t, device.writes)
				require.Equal(t, "previous-owner", device.pools[0].Username)
			} else if scenario == "allocation empty" {
				require.Equal(t, 1, device.writes)
				require.Empty(t, device.pools)
			} else {
				require.Equal(t, 1, device.writes)
				require.Equal(t, "destination", device.pools[0].Username)
			}
			// Reprocessing a committed event never sends another write.
			require.NoError(t, worker.ProcessLifecycle(t.Context(), id))
			if scenario != "pairing empty" {
				require.Equal(t, 1, device.writes)
			}
		})
	}
}

func TestAutomaticClearRetriesOnlyBeforeDispatch(t *testing.T) {
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	repo := poolconfigurations.NewRepository(cipher)
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolClear: true})
	device := &poolDevice{readError: errors.New("offline")}
	getter := poolMinerGetter{device}
	checker := poolapplications.NewChecker(f.db, reviewer, getter, cipher, f.pools)
	worker := poolapplications.NewWorker(f.db, repo, reviewer, getter, cipher).WithRemovals(checker)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	var id uuid.UUID
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND state='pending'`, miner).Scan(&id))
	require.NoError(t, worker.ProcessLifecycle(t.Context(), id))
	require.Zero(t, device.writes)
	var running int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND status='running'`, miner).Scan(&running))
	require.Zero(t, running, "a failed preflight must release its physical fence")
	row, err := sqlc.New(f.db).GetPoolLifecycleTransition(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, "ready", row.State)
	device.readError = nil
	device.onWrite = func() { device.readError = errors.New("lost verification") }
	require.NoError(t, worker.ProcessLifecycle(t.Context(), id))
	row, err = sqlc.New(f.db).GetPoolLifecycleTransition(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, "uncertain", row.State)
	require.Error(t, worker.ProcessLifecycle(t.Context(), id), "recovery reports unavailable readback")
	require.Equal(t, 1, device.writes, "uncertain writes must never retry automatically")
	device.readError = nil
	require.NoError(t, worker.ProcessLifecycle(t.Context(), id))
	row, err = sqlc.New(f.db).GetPoolLifecycleTransition(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, "completed", row.State)
	require.Equal(t, 1, device.writes, "readback recovery must not resend the clear")
}

func TestAutomaticContainerPoolsRequireHomogeneousOwnershipWhenGroupsDisabled(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "whole container", true: "mixed owners"}[mixed], func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			repo := poolconfigurations.NewRepository(cipher)
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
			device := &poolContainer{result: sdk.MiningGroupResult{Supported: true}}
			worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
			_, err := db.WithTransaction(t.Context(), f.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
				return repo.Create(t.Context(), q, poolconfigurations.Scope{OrganizationID: f.org, AccountID: f.account.ID}, poolconfigurations.RevisionOptions{Name: "Client default", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://client.example", Username: "client"}}})
			})
			require.NoError(t, err)
			module, controller := f.miningModule(t)
			_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET metadata=jsonb_build_object('macAddress','02:00:00:00:00:0'||position) WHERE device_id=$1`, controller)
			require.NoError(t, err)
			asset := selected(6, module, 0)
			if mixed {
				var board int64
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1 ORDER BY position LIMIT 1`, module).Scan(&board))
				asset = selected(7, board, 0)
			}
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, asset))
			var pairingID uuid.UUID
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='pairing' AND asset_kind=0`, controller).Scan(&pairingID))
			require.NoError(t, worker.ProcessLifecycle(t.Context(), pairingID))
			var id uuid.UUID
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='ownership' ORDER BY created_at DESC,transition_id DESC LIMIT 1`, controller).Scan(&id))
			err = worker.ProcessLifecycle(t.Context(), id)
			if mixed {
				require.Error(t, err)
				require.Zero(t, device.writes)
				row, err := sqlc.New(f.db).GetPoolLifecycleTransition(t.Context(), id)
				require.NoError(t, err)
				require.Equal(t, "pending", row.State)
				return
			}
			require.NoError(t, err)
			row, err := sqlc.New(f.db).GetPoolLifecycleTransition(t.Context(), id)
			require.NoError(t, err)
			require.Equal(t, poolconfigurations.TargetDevice, row.TargetKind)
			require.NoError(t, worker.Execute(t.Context(), f.org, row.OperationID.String, poolconfigurations.AssignmentTarget{Kind: row.TargetKind, ID: row.TargetID, DeviceID: controller}))
			require.Equal(t, 1, device.writes)
			require.Equal(t, "client", device.pools[0].Username)
			var pending int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_lifecycle_transition WHERE device_id=$1 AND state IN ('pending','ready')`, controller).Scan(&pending))
			require.Zero(t, pending, "one whole-container operation covers all allocated boards")
		})
	}
}

func TestQueuedAutomaticPolicyPinsDefaultAndRejectsStaleOwner(t *testing.T) {
	for _, recalled := range []bool{false, true} {
		t.Run(map[bool]string{false: "default changed", true: "ownership changed"}[recalled], func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			repo := poolconfigurations.NewRepository(cipher)
			scope := poolconfigurations.Scope{OrganizationID: f.org, AccountID: f.account.ID}
			create := func(name string) poolconfigurations.Configuration {
				c, err := db.WithTransaction(t.Context(), f.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
					return repo.Create(t.Context(), q, scope, poolconfigurations.RevisionOptions{Name: name, Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: name}}})
				})
				require.NoError(t, err)
				return c
			}
			first, second := create("first"), create("second")
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
			var id uuid.UUID
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='ownership'`, miner).Scan(&id))
			require.NoError(t, db.WithTransactionNoResult(t.Context(), f.db, func(q sqlc.Querier) error { return repo.SetDefault(t.Context(), q, scope, second.ID, second.Version) }))
			current, err := repo.Get(t.Context(), sqlc.New(f.db), scope, first.ID)
			require.NoError(t, err)
			require.Error(t, db.WithTransactionNoResult(t.Context(), f.db, func(q sqlc.Querier) error { return repo.Archive(t.Context(), q, scope, first.ID, current.Version) }), "a queued policy must retain its pinned configuration")
			device := &poolDevice{}
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
			worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
			require.NoError(t, worker.ProcessLifecycle(t.Context(), id))
			row, err := sqlc.New(f.db).GetPoolLifecycleTransition(t.Context(), id)
			require.NoError(t, err)
			require.Equal(t, first.ID, row.ConfigurationID.String)
			if recalled {
				require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(5, miner, 1)))
			}
			require.NoError(t, worker.Execute(t.Context(), f.org, row.OperationID.String, poolconfigurations.AssignmentTarget{Kind: row.TargetKind, ID: row.TargetID, DeviceID: miner}))
			if recalled {
				require.Zero(t, device.writes)
			} else {
				require.Equal(t, 1, device.writes)
				require.Equal(t, "first", device.pools[0].Username)
			}
		})
	}
}

func TestBoardOwnershipCanSettleCompletedWholeContainerClear(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "unknown"}[unknown], func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			repo := poolconfigurations.NewRepository(cipher)
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true, sdk.CapabilityPoolClear: true})
			device := &poolContainer{result: sdk.MiningGroupResult{Supported: true}}
			device.onWrite = func() { device.poolDevice.readError = errors.New("verification unavailable") }
			getter := poolMinerGetter{device}
			checker := poolapplications.NewChecker(f.db, reviewer, getter, cipher, f.pools)
			worker := poolapplications.NewWorker(f.db, repo, reviewer, getter, cipher).WithRemovals(checker)
			module, controller := f.miningModule(t)
			_, err := f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET metadata=jsonb_build_object('macAddress','02:00:00:00:00:0'||position) WHERE device_id=$1`, controller)
			require.NoError(t, err)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
			var id uuid.UUID
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='ownership' ORDER BY created_at DESC,transition_id DESC LIMIT 1`, controller).Scan(&id))
			require.NoError(t, worker.ProcessLifecycle(t.Context(), id))
			q := sqlc.New(f.db)
			old, err := q.GetPoolLifecycleTransition(t.Context(), id)
			require.NoError(t, err)
			require.Equal(t, "uncertain", old.State)
			require.Equal(t, poolconfigurations.TargetDevice, old.TargetKind)
			require.Equal(t, 1, device.writes)
			if unknown {
				_, err = f.db.ExecContext(t.Context(), `UPDATE device_write_attempt SET status='unknown' WHERE attempt_id=$1`, old.PhysicalAttemptID)
				require.NoError(t, err)
			}
			var sibling int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1 AND hashboard_id<>$2 ORDER BY position LIMIT 1`, module, old.AssetID).Scan(&sibling))
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(7, sibling, 1)))
			var replacement uuid.UUID
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND asset_id=$2 AND owner_account_id=0 AND reason='ownership'`, controller, sibling).Scan(&replacement))
			device.poolDevice.readError = nil
			require.Error(t, worker.ProcessLifecycle(t.Context(), replacement), "mixed ownership still waits for mining-group activation")
			old, err = q.GetPoolLifecycleTransition(t.Context(), id)
			require.NoError(t, err)
			if unknown {
				require.Equal(t, "uncertain", old.State, "unknown delivery retains its reservation")
			} else {
				require.Equal(t, "superseded", old.State, "current board authority can observe the completed obsolete clear")
			}
			require.Equal(t, 1, device.writes, "settling obsolete work must never write")
		})
	}
}

func TestContainerPairingKeepsCapturedPolicyAndYieldsToLaterManualAssignment(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "captured default", true: "manual assignment"}[manual], func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			repo := poolconfigurations.NewRepository(cipher)
			scope := poolconfigurations.Scope{OrganizationID: f.org}
			create := func(name string) poolconfigurations.Configuration {
				c, err := db.WithTransaction(t.Context(), f.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
					return repo.Create(t.Context(), q, scope, poolconfigurations.RevisionOptions{Name: name, Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: name}}})
				})
				require.NoError(t, err)
				return c
			}
			first := create("first")
			_, controller := f.miningModule(t)
			second := create("second")
			require.NoError(t, db.WithTransactionNoResult(t.Context(), f.db, func(q sqlc.Querier) error { return repo.SetDefault(t.Context(), q, scope, second.ID, second.Version) }))
			if manual {
				_, err := db.WithTransaction(t.Context(), f.db, func(q sqlc.Querier) (poolconfigurations.Operation, error) {
					return repo.Assign(t.Context(), q, scope, f.host, second.ID, second.RevisionID, []poolconfigurations.AssignmentChange{{Target: poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: controller, DeviceID: controller}}})
				})
				require.NoError(t, err)
			}
			device := &poolContainer{result: sdk.MiningGroupResult{Supported: true}}
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
			worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
			var parent uuid.UUID
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND asset_kind=0`, controller).Scan(&parent))
			require.NoError(t, worker.ProcessLifecycle(t.Context(), parent))
			var child uuid.UUID
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND asset_kind=7 ORDER BY asset_id LIMIT 1`, controller).Scan(&child))
			q := sqlc.New(f.db)
			row, err := q.GetPoolLifecycleTransition(t.Context(), child)
			require.NoError(t, err)
			require.Equal(t, first.ID, row.ConfigurationID.String)
			if manual {
				require.NoError(t, worker.ProcessLifecycle(t.Context(), child))
				row, err = q.GetPoolLifecycleTransition(t.Context(), child)
				require.NoError(t, err)
				require.Equal(t, "superseded", row.State)
				require.Zero(t, device.writes)
			}
		})
	}
}
