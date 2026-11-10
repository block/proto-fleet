package timescaledb

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRemovedBoardPoolPolicyRetiresAndReinstallationCapturesCurrentDefault(t *testing.T) {
	for _, reinstall := range []string{"operator default", "operator empty", "client replacement", "legacy upgrade"} {
		t.Run(reinstall, func(t *testing.T) {
			r := newModulePSUIdentityRig(t)
			migrate := func(direction string) {
				t.Helper()
				script, err := migrations.Current.ReadFile("current/001077_internal_removed_board_pool_lifecycle." + direction + ".sql")
				require.NoError(t, err)
				_, err = r.db.ExecContext(t.Context(), string(script))
				require.NoError(t, err)
			}
			if reinstall == "legacy upgrade" {
				migrate("down")
			}
			cipher, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
			require.NoError(t, err)
			repo := poolconfigurations.NewRepository(cipher)
			scope := poolconfigurations.Scope{OrganizationID: r.orgID}
			create := func(account int64, name string) poolconfigurations.Configuration {
				config, err := db.WithTransaction(t.Context(), r.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
					return repo.Create(t.Context(), q, poolconfigurations.Scope{OrganizationID: r.orgID, AccountID: account}, poolconfigurations.RevisionOptions{Name: name, Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: name}}})
				})
				require.NoError(t, err)
				return config
			}
			original := create(0, "original")
			client := replacementAccount(t, r, 0)
			clientConfig := create(client, "client")
			require.NoError(t, r.store(t, replacementSnapshot(r.base, "old")))
			old := r.boardID(t, "old")
			assignReplacementBoard(t, r, old, client, 0, r.base.Add(time.Millisecond))
			require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(time.Second), "new")))
			require.Equal(t, [2]int64{}, replacementOwner(t, r, old))
			require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, r.boardID(t, "new")))
			q := sqlc.New(r.db)
			var retired uuid.UUID
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition WHERE asset_kind=7 AND asset_id=$1 AND owner_account_id=0 ORDER BY created_at DESC LIMIT 1`, old).Scan(&retired))
			row, err := q.GetPoolLifecycleTransition(t.Context(), retired)
			require.NoError(t, err)
			if reinstall == "legacy upgrade" {
				require.Equal(t, "pending", row.State, "reproduce the older backend's stranded policy")
				migrate("up")
				row, err = q.GetPoolLifecycleTransition(t.Context(), retired)
				require.NoError(t, err)
			}
			require.Equal(t, "superseded", row.State)
			require.Equal(t, "board_removed", row.ResultCode)
			require.Equal(t, original.ID, row.ConfigurationID.String)
			require.NoError(t, db.WithTransactionNoResult(t.Context(), r.db, func(q sqlc.Querier) error {
				return repo.Archive(t.Context(), q, scope, original.ID, original.Version)
			}), "removed hardware must not pin the captured default")
			wantConfig, wantOwner := "", int64(0)
			if reinstall == "operator default" || reinstall == "legacy upgrade" {
				wantConfig = create(0, "current").ID
			}
			returned := replacementSnapshot(r.base.Add(2*time.Second), "new", "old")
			if reinstall == "client replacement" {
				returned = replacementSnapshot(r.base.Add(2*time.Second), "old")
				wantOwner, wantConfig = client, clientConfig.ID
			}
			require.NoError(t, r.store(t, returned))
			var active int
			var owner int64
			var captured string
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*),max(owner_account_id),COALESCE(max(configuration_id),'') FROM pool_lifecycle_transition WHERE asset_kind=7 AND asset_id=$1 AND state='pending'`, old).Scan(&active, &owner, &captured))
			require.Equal(t, 1, active)
			require.Equal(t, wantOwner, owner)
			require.Equal(t, wantConfig, captured)
			// Replayed inventory cannot enqueue the same returned board twice.
			require.NoError(t, r.store(t, returned))
			require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_lifecycle_transition WHERE asset_kind=7 AND asset_id=$1 AND state='pending'`, old).Scan(&active))
			require.Equal(t, 1, active)
		})
	}
}

func TestRemovedBoardPoolPolicyPreservesUncertainWritesAndStaleInventory(t *testing.T) {
	for _, state := range []string{"pending", "ready", "unknown write"} {
		t.Run(state, func(t *testing.T) {
			r := newModulePSUIdentityRig(t)
			require.NoError(t, r.store(t, replacementSnapshot(r.base, "old")))
			board := r.boardID(t, "old")
			q := sqlc.New(r.db)
			var event uuid.UUID
			require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason) VALUES($1,$2,7,$3,'ownership') RETURNING transition_id`, r.orgID, r.deviceID, board).Scan(&event))
			if state != "pending" {
				_, err := q.PreparePoolLifecycleTransition(t.Context(), sqlc.PreparePoolLifecycleTransitionParams{TransitionID: event, TargetKind: "device", TargetID: r.deviceID, ReviewedBinding: "{}"})
				require.NoError(t, err)
			}
			var attempt uuid.UUID
			if state == "unknown write" {
				err := devicewrite.NewService(r.db).WithRecordedHostAttempt(devicewrite.WithPoolOperation(t.Context(), event), r.orgID, r.deviceID, 0, commandtype.UpdateMiningPools, func(q sqlc.Querier, physical uuid.UUID) error {
					attempt = physical
					_, err := q.AcceptPoolRemoval(t.Context(), sqlc.AcceptPoolRemovalParams{RemovalID: event, PhysicalAttemptID: uuid.NullUUID{UUID: physical, Valid: true}})
					return err
				}, func(ctx context.Context) error {
					return devicewrite.Dispatch(ctx, func(context.Context) error { return devicewrite.UncertainDelivery(context.DeadlineExceeded) })
				})
				require.Error(t, err)
			}
			before, err := q.GetPoolLifecycleTransition(t.Context(), event)
			require.NoError(t, err)
			// Unknown inventory is not proof that the board was removed.
			stale := replacementSnapshot(r.base.Add(time.Second), "old")
			stale.TopologyComplete = false
			stale.Tanks[0].Modules[0].Hashboards[0].Occupancy = "unknown"
			require.NoError(t, r.store(t, stale))
			require.NoError(t, q.ReconcilePoolLifecycleOperations(t.Context()))
			after, err := q.GetPoolLifecycleTransition(t.Context(), event)
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(2*time.Second), "")))
			require.NoError(t, q.ReconcilePoolLifecycleOperations(t.Context()))
			after, err = q.GetPoolLifecycleTransition(t.Context(), event)
			require.NoError(t, err)
			if state == "unknown write" {
				require.Equal(t, before, after, "removal must retain the unresolved clear workflow")
				physical, err := q.LockDeviceWriteAttemptForRecovery(t.Context(), attempt)
				require.NoError(t, err)
				require.Equal(t, "unknown", physical.Status)
				drained, err := q.IsDeviceWriteFenceDrained(t.Context(), sqlc.IsDeviceWriteFenceDrainedParams{DeviceID: r.deviceID, OrganizationID: r.orgID, Epoch: physical.Epoch})
				require.NoError(t, err)
				require.False(t, drained)
			} else {
				require.Equal(t, "superseded", after.State)
				require.Equal(t, "board_removed", after.ResultCode)
			}
		})
	}
}

func TestReinstalledBoardDoesNotDuplicateControllerMovePoolPolicy(t *testing.T) {
	source, destination := replacementControllers(t)
	require.NoError(t, source.store(t, replacementSnapshot(source.base, "old")))
	old := source.boardID(t, "old")
	client := replacementAccount(t, source, 0)
	assignReplacementBoard(t, source, old, client, 0, source.base.Add(time.Millisecond))
	require.NoError(t, source.store(t, replacementSnapshot(source.base.Add(time.Second), "")))
	returned := replacementSnapshot(source.base.Add(2*time.Second), "old")
	returned.Tanks[0].Modules[0].ReportedID = "destination-psu"
	require.NoError(t, destination.store(t, returned))
	var active int
	require.NoError(t, source.db.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_lifecycle_transition WHERE device_id=$1 AND asset_kind=7 AND asset_id=$2 AND state='pending' AND owner_account_id=$3`, destination.deviceID, old, client).Scan(&active))
	require.Equal(t, 1, active, "placement and reinstallation must share one current policy")
}
