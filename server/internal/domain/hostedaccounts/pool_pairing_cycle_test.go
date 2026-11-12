package hostedaccounts_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/block/proto-fleet/server/internal/domain/devicewrite"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	hostedpb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRePairingDiscardsPreviousCyclePoolWork(t *testing.T) {
	for _, container := range []bool{false, true} {
		for _, stage := range []string{"queued", "prepared", "accepted"} {
			name := "miner/" + stage
			if container {
				name = "container/" + stage
			}
			t.Run(name, func(t *testing.T) {
				f := newPoolStepUpFixture(t)
				_, _, _, cipher, _ := poolFixture(t, f.fixture)
				ctx := t.Context()
				q := sqlc.New(f.db)
				repo := poolconfigurations.NewRepository(cipher)
				scope := poolconfigurations.Scope{OrganizationID: f.org}
				create := func(name string) poolconfigurations.Configuration {
					c, err := db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
						return repo.Create(ctx, q, scope, poolconfigurations.RevisionOptions{Name: name, Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: name}}})
					})
					require.NoError(t, err)
					return c
				}
				first := create("first")
				var controller int64
				if container {
					_, controller = f.miningModule(t)
					_, err := f.db.ExecContext(ctx, `UPDATE hashboard_installation SET metadata=jsonb_build_object('macAddress','02:00:00:00:00:0'||position) WHERE device_id=$1`, controller)
					require.NoError(t, err)
				} else {
					controller = f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
				}
				device := &poolContainer{result: sdk.MiningGroupResult{Supported: true}}
				reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
				worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
				latest := func(kind int) uuid.UUID {
					var id uuid.UUID
					require.NoError(t, f.db.QueryRowContext(ctx, `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='pairing' AND asset_kind=$2 ORDER BY created_at DESC,transition_id DESC LIMIT 1`, controller, kind).Scan(&id))
					return id
				}
				kind := 5
				if container {
					kind = 0
				}
				old := latest(kind)
				if container && stage != "queued" {
					require.NoError(t, worker.ProcessLifecycle(ctx, old))
					old = latest(7)
				}
				if stage == "prepared" {
					_, err := q.PreparePoolLifecycleTransition(ctx, sqlc.PreparePoolLifecycleTransitionParams{TransitionID: old, TargetKind: poolconfigurations.TargetDevice, TargetID: controller, ReviewedBinding: "{}"})
					require.NoError(t, err)
				} else if stage == "accepted" {
					require.NoError(t, worker.ProcessLifecycle(ctx, old))
				}
				before, err := q.GetPoolLifecycleTransition(ctx, old)
				require.NoError(t, err)
				if stage != "queued" || !container {
					require.Equal(t, first.ID, before.ConfigurationID.String)
				}
				// Authentication repair stays in the same cycle and leaves its
				// pending or accepted default eligible for execution.
				for _, status := range []sqlc.PairingStatusEnum{sqlc.PairingStatusEnumAUTHENTICATIONNEEDED, sqlc.PairingStatusEnumPAIRED} {
					_, err := q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{DeviceID: controller, PairingStatus: status})
					require.NoError(t, err)
				}
				repaired, err := q.GetPoolLifecycleTransition(ctx, old)
				require.NoError(t, err)
				require.True(t, repaired.CurrentPairingCycle)
				require.Equal(t, before.PairingCycleID, repaired.PairingCycleID)
				second := create("second")
				require.NoError(t, db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error { return repo.SetDefault(ctx, q, scope, second.ID, second.Version) }))
				for _, status := range []sqlc.PairingStatusEnum{sqlc.PairingStatusEnumUNPAIRED, sqlc.PairingStatusEnumAUTHENTICATIONNEEDED, sqlc.PairingStatusEnumPAIRED} {
					_, err := q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{DeviceID: controller, PairingStatus: status})
					require.NoError(t, err)
				}
				current := latest(kind)
				require.NotEqual(t, old, current)
				currentCycle, err := q.GetPoolLifecycleTransition(ctx, current)
				require.NoError(t, err)
				require.NotEqual(t, before.PairingCycleID, currentCycle.PairingCycleID)
				var staleQueued int
				require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM pool_lifecycle_transition WHERE device_id=$1 AND pairing_cycle_id=$2 AND state IN ('pending','ready')`, controller, before.PairingCycleID).Scan(&staleQueued))
				require.Zero(t, staleQueued, "unpair must retire every unclaimed child of the ended cycle")
				// A worker may already hold the old ID or an accepted operation when unpair commits.
				require.NoError(t, worker.ProcessLifecycle(ctx, old))
				if before.OperationID.Valid {
					require.NoError(t, worker.Execute(ctx, f.org, before.OperationID.String, poolconfigurations.AssignmentTarget{Kind: before.TargetKind, ID: before.TargetID, DeviceID: controller}))
				}
				require.Zero(t, device.writes, "previous-cycle work must never reach hardware after re-pairing")
				if stage != "accepted" {
					retired, err := q.GetPoolLifecycleTransition(ctx, old)
					require.NoError(t, err)
					require.Equal(t, "superseded", retired.State)
				}
				require.NoError(t, worker.ProcessLifecycle(ctx, current))
				if container {
					current = latest(7)
					require.NoError(t, worker.ProcessLifecycle(ctx, current))
				}
				row, err := q.GetPoolLifecycleTransition(ctx, current)
				require.NoError(t, err)
				require.Equal(t, second.ID, row.ConfigurationID.String)
				require.True(t, row.OperationID.Valid, "the old cycle must not supersede the new cycle")
				require.NoError(t, worker.Execute(ctx, f.org, row.OperationID.String, poolconfigurations.AssignmentTarget{Kind: row.TargetKind, ID: row.TargetID, DeviceID: controller}))
				require.Equal(t, 1, device.writes)
				require.Equal(t, "second", device.pools[0].Username)
			})
		}
	}
}

// Ending a cycle cancels future authority, not evidence that a prior device
// call may still be running. The next default must wait for recovery.
func TestRePairingPreservesUnknownPoolWrite(t *testing.T) {
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	ctx := t.Context()
	q := sqlc.New(f.db)
	repo := poolconfigurations.NewRepository(cipher)
	_, err := db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
		return repo.Create(ctx, q, poolconfigurations.Scope{OrganizationID: f.org}, poolconfigurations.RevisionOptions{Name: "Default", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "default"}}})
	})
	require.NoError(t, err)
	controller := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	device := &poolDevice{writeError: devicewrite.UncertainDelivery(context.DeadlineExceeded)}
	reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
	worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
	var id uuid.UUID
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='pairing'`, controller).Scan(&id))
	require.NoError(t, worker.ProcessLifecycle(ctx, id))
	old, err := q.GetPoolLifecycleTransition(ctx, id)
	require.NoError(t, err)
	target := poolconfigurations.AssignmentTarget{Kind: old.TargetKind, ID: old.TargetID, DeviceID: controller}
	require.NoError(t, worker.Execute(ctx, f.org, old.OperationID.String, target))
	var physical uuid.UUID
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT physical_attempt_id FROM pool_configuration_operation_target WHERE operation_id=$1`, old.OperationID.String).Scan(&physical))
	before, err := q.LockDeviceWriteAttemptForRecovery(ctx, physical)
	require.NoError(t, err)
	require.Equal(t, "unknown", before.Status)
	for _, status := range []sqlc.PairingStatusEnum{sqlc.PairingStatusEnumUNPAIRED, sqlc.PairingStatusEnumPAIRED} {
		_, err := q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{DeviceID: controller, PairingStatus: status})
		require.NoError(t, err)
	}
	var next uuid.UUID
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='pairing' AND transition_id<>$2`, controller, id).Scan(&next))
	require.Error(t, worker.ProcessLifecycle(ctx, next), "the next cycle must not bypass unresolved physical writes")
	require.NoError(t, q.ReconcilePoolLifecycleOperations(ctx))
	after, err := q.LockDeviceWriteAttemptForRecovery(ctx, physical)
	require.NoError(t, err)
	require.Equal(t, before, after, "cycle invalidation must not alter physical attempt evidence")
	require.Equal(t, 1, device.writes)
}

func TestPairingAssignmentAcceptanceSerializesWithUnpair(t *testing.T) {
	f := newPoolStepUpFixture(t)
	_, _, _, cipher, _ := poolFixture(t, f.fixture)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	q := sqlc.New(f.db)
	repo := poolconfigurations.NewRepository(cipher)
	_, err := db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
		return repo.Create(ctx, q, poolconfigurations.Scope{OrganizationID: f.org}, poolconfigurations.RevisionOptions{Name: "Default", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "default"}}})
	})
	require.NoError(t, err)
	controller := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	var id uuid.UUID
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='pairing'`, controller).Scan(&id))
	_, err = q.PreparePoolLifecycleTransition(ctx, sqlc.PreparePoolLifecycleTransitionParams{TransitionID: id, TargetKind: poolconfigurations.TargetDevice, TargetID: controller, ReviewedBinding: "{}"})
	require.NoError(t, err)
	// Pause after authorization but before the assignment is inserted. Unpair
	// must wait; otherwise this assignment could be newer than the next cycle.
	_, err = f.db.ExecContext(ctx, `CREATE FUNCTION pause_pairing_assignment() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN PERFORM pg_advisory_xact_lock(369,1074); RETURN NEW; END $$;
 CREATE TRIGGER pause_pairing_assignment BEFORE INSERT ON pool_configuration_assignment FOR EACH ROW EXECUTE FUNCTION pause_pairing_assignment();`)
	require.NoError(t, err)
	gate, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer gate.Rollback()
	_, err = gate.ExecContext(ctx, `SELECT pg_advisory_xact_lock(369,1074)`)
	require.NoError(t, err)
	var blocker int
	require.NoError(t, gate.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blocker))
	worker := poolapplications.NewWorker(f.db, repo, nil, nil, cipher)
	done := make(chan error, 1)
	go func() { done <- worker.ProcessLifecycle(ctx, id) }()
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '-- name: SetPoolConfigurationAssignment%')`, blocker).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond)
	unpair, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer unpair.Rollback()
	_, err = unpair.ExecContext(ctx, `SET LOCAL lock_timeout='100ms'`)
	require.NoError(t, err)
	_, err = unpair.ExecContext(ctx, `UPDATE device_pairing SET pairing_status='UNPAIRED' WHERE device_id=$1`, controller)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr, "unpair must wait until assignment acceptance commits")
	require.Equal(t, "55P03", pgErr.Code)
	require.NoError(t, unpair.Rollback())
	require.NoError(t, gate.Commit())
	require.NoError(t, <-done)
	_, err = q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{DeviceID: controller, PairingStatus: sqlc.PairingStatusEnumUNPAIRED})
	require.NoError(t, err)
	row, err := q.GetPoolLifecycleTransition(ctx, id)
	require.NoError(t, err)
	require.False(t, row.CurrentPairingCycle)
	require.True(t, row.OperationID.Valid, "accepted history remains available for recovery")
}

func TestPairingPolicyOrderingUsesAutomaticEventTime(t *testing.T) {
	for _, scenario := range []string{"ownership first", "pairing first", "later manual assignment", "later manual retirement"} {
		t.Run(scenario, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx := t.Context()
			q := sqlc.New(f.db)
			repo := poolconfigurations.NewRepository(cipher)
			scope := poolconfigurations.Scope{OrganizationID: f.org, AccountID: f.account.ID}
			create := func(name string) poolconfigurations.Configuration {
				c, err := db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
					return repo.Create(ctx, q, scope, poolconfigurations.RevisionOptions{Name: name, Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: name}}})
				})
				require.NoError(t, err)
				return c
			}
			first := create("first")
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			require.NoError(t, f.allocate(t, f.host, f.root, f.account.AccountID, hostedpb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
			second := create("second")
			require.NoError(t, db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error { return repo.SetDefault(ctx, q, scope, second.ID, second.Version) }))
			for _, status := range []sqlc.PairingStatusEnum{sqlc.PairingStatusEnumUNPAIRED, sqlc.PairingStatusEnumPAIRED} {
				_, err := q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{DeviceID: miner, PairingStatus: status})
				require.NoError(t, err)
			}
			var ownership, pairing uuid.UUID
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='ownership' AND state='pending'`, miner).Scan(&ownership))
			require.NoError(t, f.db.QueryRowContext(ctx, `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='pairing' AND state='pending'`, miner).Scan(&pairing))
			device := &poolDevice{}
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
			worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
			target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}
			apply := func(id uuid.UUID) {
				require.NoError(t, worker.ProcessLifecycle(ctx, id))
				row, err := q.GetPoolLifecycleTransition(ctx, id)
				require.NoError(t, err)
				if row.OperationID.Valid {
					require.NoError(t, worker.Execute(ctx, f.org, row.OperationID.String, target))
				}
			}
			assignment := func() sqlc.GetPoolConfigurationAssignmentRow {
				row, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.org, TargetKind: target.Kind, TargetID: miner})
				require.NoError(t, err)
				return row
			}
			if scenario == "pairing first" {
				apply(pairing)
				apply(ownership)
			} else {
				// Both events are queued before the older automatic assignment is created.
				apply(ownership)
				current := assignment()
				switch scenario {
				case "later manual assignment":
					_, err := db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (poolconfigurations.Operation, error) {
						return repo.Assign(ctx, q, scope, f.host, first.ID, first.RevisionID, []poolconfigurations.AssignmentChange{{Target: target, ExpectedVersion: current.Version}})
					})
					require.NoError(t, err)
				case "later manual retirement":
					require.NoError(t, db.WithTransactionNoResult(ctx, f.db, func(q sqlc.Querier) error { return repo.RetireAssignment(ctx, q, scope, target, current.Version) }))
				}
				apply(pairing)
			}
			current := assignment()
			if scenario == "later manual assignment" || scenario == "later manual retirement" {
				row, err := q.GetPoolLifecycleTransition(ctx, pairing)
				require.NoError(t, err)
				require.Equal(t, "superseded", row.State, "a later explicit choice must still win")
				require.Equal(t, first.ID, current.ConfigurationID)
				require.Equal(t, scenario == "later manual retirement", current.RetiredAt.Valid)
				require.Equal(t, 1, device.writes)
				return
			}
			require.Equal(t, second.ID, current.ConfigurationID, "worker timing must not change the winning policy")
			require.Equal(t, "second", device.pools[0].Username)
			work, err := q.GetPoolConfigurationWork(ctx, sqlc.GetPoolConfigurationWorkParams{OrganizationID: f.org, OperationID: current.OperationID.String, TargetKind: target.Kind, TargetID: miner})
			require.NoError(t, err)
			require.Equal(t, "verified", work.State)
		})
	}
}
