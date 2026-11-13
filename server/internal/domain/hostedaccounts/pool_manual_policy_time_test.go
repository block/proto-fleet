package hostedaccounts_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestManualPoolPolicyUsesSaveTimeAfterPairing(t *testing.T) {
	for _, action := range []string{"insert", "replace", "retire"} {
		t.Run(action, func(t *testing.T) {
			f := newPoolStepUpFixture(t)
			_, _, _, cipher, _ := poolFixture(t, f.fixture)
			ctx := t.Context()
			q := sqlc.New(f.db)
			repo := poolconfigurations.NewRepository(cipher)
			scope := poolconfigurations.Scope{OrganizationID: f.org}
			create := func(name string) poolconfigurations.Configuration {
				t.Helper()
				c, err := db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
					return repo.Create(ctx, q, scope, poolconfigurations.RevisionOptions{Name: name, Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: name}}})
				})
				require.NoError(t, err)
				return c
			}
			defaultConfig, manualConfig := create("default"), create("manual")
			miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			target := poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: miner, DeviceID: miner}
			var version int64
			if action == "replace" || action == "retire" {
				_, err := db.WithTransaction(ctx, f.db, func(q sqlc.Querier) (poolconfigurations.Operation, error) {
					return repo.Assign(ctx, q, scope, f.host, manualConfig.ID, manualConfig.RevisionID, []poolconfigurations.AssignmentChange{{Target: target}})
				})
				require.NoError(t, err)
				version = 1
			}
			pair := func() uuid.UUID {
				t.Helper()
				for _, status := range []sqlc.PairingStatusEnum{sqlc.PairingStatusEnumUNPAIRED, sqlc.PairingStatusEnumPAIRED} {
					_, err := q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{DeviceID: miner, PairingStatus: status})
					require.NoError(t, err)
				}
				var id uuid.UUID
				require.NoError(t, f.db.QueryRowContext(ctx, `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND reason='pairing' AND state='pending'`, miner).Scan(&id))
				return id
			}
			// The manual transaction starts before pairing, but saves its decision
			// afterward. No sleeps are needed: a second connection queues pairing
			// before this transaction acquires the assignment's serialization locks.
			var pairing uuid.UUID
			require.NoError(t, db.WithTransactionNoResult(ctx, f.db, func(txq sqlc.Querier) error {
				// Any query establishes PostgreSQL's transaction-start timestamp.
				_, err := txq.GetPoolConfiguration(ctx, sqlc.GetPoolConfigurationParams{OrganizationID: f.org, ConfigurationID: manualConfig.ID})
				if err != nil {
					return err
				}
				pairing = pair()
				switch action {
				case "retire":
					return repo.RetireAssignment(ctx, txq, scope, target, version)
				default:
					_, err := repo.Assign(ctx, txq, scope, f.host, manualConfig.ID, manualConfig.RevisionID, []poolconfigurations.AssignmentChange{{Target: target, ExpectedVersion: version}})
					return err
				}
			}))
			device := &poolDevice{}
			reviewer := poolapplications.NewReviewer(f.pools, poolCapabilities{sdk.CapabilityPoolConfig: true})
			worker := poolapplications.NewWorker(f.db, repo, reviewer, poolMinerGetter{device}, cipher)
			require.NoError(t, worker.ProcessLifecycle(ctx, pairing))
			event, err := q.GetPoolLifecycleTransition(ctx, pairing)
			require.NoError(t, err)
			require.Equal(t, "superseded", event.State, "manual decisions saved after pairing must win even when their transaction began earlier")
			assignment, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.org, TargetKind: target.Kind, TargetID: miner})
			require.NoError(t, err)
			require.Equal(t, manualConfig.ID, assignment.ConfigurationID)
			require.Equal(t, action == "retire", assignment.RetiredAt.Valid)
			require.True(t, assignment.UpdatedAt.After(event.CreatedAt))
			if action == "retire" {
				require.True(t, assignment.RetiredAt.Time.After(event.CreatedAt))
			}
			// Deliberate pairing after the saved manual decision still applies default.
			later := pair()
			require.NoError(t, worker.ProcessLifecycle(ctx, later))
			next, err := q.GetPoolLifecycleTransition(ctx, later)
			require.NoError(t, err)
			require.Equal(t, "applying", next.State)
			assignment, err = q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.org, TargetKind: target.Kind, TargetID: miner})
			require.NoError(t, err)
			require.Equal(t, defaultConfig.ID, assignment.ConfigurationID)
			require.False(t, assignment.RetiredAt.Valid)
			require.Zero(t, device.writes)
		})
	}
}
