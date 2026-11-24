package timescaledb

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDeferredReinstallationRecapturesCurrentDefaultBeforeReconciliation(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	ctx := t.Context()
	secrets, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	require.NoError(t, err)
	repo := poolconfigurations.NewRepository(secrets)
	scope := poolconfigurations.Scope{OrganizationID: r.orgID}
	create := func(name string) poolconfigurations.Configuration {
		c, err := db.WithTransaction(ctx, r.db, func(q sqlc.Querier) (poolconfigurations.Configuration, error) {
			return repo.Create(ctx, q, scope, poolconfigurations.RevisionOptions{Name: name, Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: name}}})
		})
		require.NoError(t, err)
		return c
	}
	original := create("original")
	require.NoError(t, r.store(t, replacementSnapshot(r.base, "old")))
	old := r.boardID(t, "old")
	_, err = r.db.ExecContext(ctx, `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,configuration_id,revision_id) VALUES($1,$2,7,$3,'ownership',$4,$5)`, r.orgID, r.deviceID, old, original.ID, original.RevisionID)
	require.NoError(t, err)
	require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(time.Second), "")))
	_, err = r.db.ExecContext(ctx, `INSERT INTO container_mining_group_state(device_id,organization_id,operation_kind,status) VALUES($1,$2,'deactivate','blocked')`, r.deviceID, r.orgID)
	require.NoError(t, err)
	require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(2*time.Second), "old")))
	require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(3*time.Second), "")))
	changed := create("changed")
	require.NoError(t, db.WithTransactionNoResult(ctx, r.db, func(q sqlc.Querier) error { return repo.SetDefault(ctx, q, scope, changed.ID, changed.Version) }))
	require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(4*time.Second), "old")))
	_, err = r.db.ExecContext(ctx, `UPDATE container_mining_group_state SET status='complete' WHERE device_id=$1`, r.deviceID)
	require.NoError(t, err)
	q := sqlc.New(r.db)
	require.NoError(t, q.ReconcilePoolLifecycleOperations(ctx))
	rows, err := q.ListPendingPoolLifecycleTransitions(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, changed.ID, rows[0].ConfigurationID.String, "the latest reinstallation must capture the current default")
}

func TestDeferredOwnershipPolicySupersedesBeforeReconciliation(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	require.NoError(t, r.store(t, replacementSnapshot(r.base, "old")))
	old := r.boardID(t, "old")
	first, second := replacementAccount(t, r, 0), replacementAccount(t, r, 0)
	_, err := r.db.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id,operation_kind,status) VALUES($1,$2,'deactivate','blocked')`, r.deviceID, r.orgID)
	require.NoError(t, err)
	assignReplacementBoard(t, r, old, first, 0, r.base.Add(time.Millisecond))
	var original uuid.UUID
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition WHERE asset_id=$1 AND state='deferred'`, old).Scan(&original))
	assignReplacementBoard(t, r, old, second, 0, r.base.Add(2*time.Millisecond))
	policy, err := sqlc.New(r.db).GetPoolLifecycleTransition(t.Context(), original)
	require.NoError(t, err)
	require.Equal(t, "superseded", policy.State, "ownership change must retire the old policy in the same transaction")
	assignReplacementBoard(t, r, old, first, 0, r.base.Add(3*time.Millisecond))
	var count int
	var revision int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*),max(allocation_revision) FROM pool_lifecycle_transition WHERE asset_id=$1 AND state='deferred'`, old).Scan(&count, &revision))
	require.Equal(t, 1, count)
	require.EqualValues(t, 3, revision, "returning to the original owner must retain only its new allocation generation")
}

func TestDeferredPairingCycleRetiresBeforeReconciliation(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	_, err := r.db.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id,operation_kind,status) VALUES($1,$2,'deactivate','blocked')`, r.deviceID, r.orgID)
	require.NoError(t, err)
	q := sqlc.New(r.db)
	_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: r.deviceID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
	require.NoError(t, err)
	var original uuid.UUID
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT transition_id FROM pool_lifecycle_transition WHERE device_id=$1 AND state='deferred'`, r.deviceID).Scan(&original))
	_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: r.deviceID, PairingStatus: sqlc.PairingStatusEnumUNPAIRED})
	require.NoError(t, err)
	policy, err := q.GetPoolLifecycleTransition(t.Context(), original)
	require.NoError(t, err)
	require.Equal(t, "superseded", policy.State)
	require.Equal(t, "pairing_cycle_ended", policy.ResultCode)
	_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: r.deviceID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
	require.NoError(t, err)
	var count int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_lifecycle_transition WHERE device_id=$1 AND state='deferred'`, r.deviceID).Scan(&count))
	require.Equal(t, 1, count, "only the new pairing cycle may remain queued")
}

func TestAbsentBoardRejectsDeferredPolicyBeforeReconciliation(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	require.NoError(t, r.store(t, replacementSnapshot(r.base, "old")))
	old := r.boardID(t, "old")
	require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(time.Second), "")))
	var original uuid.UUID
	require.NoError(t, r.db.QueryRowContext(t.Context(), `INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,state) VALUES($1,$2,7,$3,'ownership','deferred') RETURNING transition_id`, r.orgID, r.deviceID, old).Scan(&original))
	policy, err := sqlc.New(r.db).GetPoolLifecycleTransition(t.Context(), original)
	require.NoError(t, err)
	require.Equal(t, "superseded", policy.State)
	require.Equal(t, "board_removed", policy.ResultCode)
}

func TestDeferredControllerMoveDoesNotDuplicateReinstallationPolicy(t *testing.T) {
	source, destination := replacementControllers(t)
	require.NoError(t, source.store(t, replacementSnapshot(source.base, "old")))
	old := source.boardID(t, "old")
	client := replacementAccount(t, source, 0)
	assignReplacementBoard(t, source, old, client, 0, source.base.Add(time.Millisecond))
	require.NoError(t, source.store(t, replacementSnapshot(source.base.Add(time.Second), "")))
	_, err := destination.db.ExecContext(t.Context(), `INSERT INTO container_mining_group_state(device_id,organization_id,operation_kind,status) VALUES($1,$2,'deactivate','blocked')`, destination.deviceID, destination.orgID)
	require.NoError(t, err)
	returned := replacementSnapshot(source.base.Add(2*time.Second), "old")
	returned.Tanks[0].Modules[0].ReportedID = "destination-psu"
	require.NoError(t, destination.store(t, returned))
	var count int
	require.NoError(t, source.db.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_lifecycle_transition WHERE device_id=$1 AND asset_kind=7 AND asset_id=$2 AND state='deferred' AND owner_account_id=$3`, destination.deviceID, old, client).Scan(&count))
	require.Equal(t, 1, count, "placement and reinstallation must retain one deferred policy")
}
