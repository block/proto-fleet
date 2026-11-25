package timescaledb

import (
	"context"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAutomaticReplacementDefersForMutationAdmission(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	require.NoError(t, r.store(t, replacementSnapshot(r.base, "old", "other", "spare")))
	old := r.boardID(t, "old")
	client := replacementAccount(t, r, 0)
	assignReplacementBoard(t, r, old, client, 0, r.base.Add(time.Millisecond))
	admission, err := r.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer admission.Rollback()
	q := sqlc.New(admission)
	keys, err := q.GetDeviceWriteIdentityKeys(t.Context(), r.deviceID)
	require.NoError(t, err)
	require.NoError(t, q.LockDeviceWriteIdentities(t.Context(), keys))
	fences, err := q.LockDeviceWriteFences(t.Context(), []int64{r.deviceID})
	require.NoError(t, err)
	require.Len(t, fences, 1)
	attemptID := uuid.New()
	require.NoError(t, q.CreateDeviceWriteAttempt(t.Context(), sqlc.CreateDeviceWriteAttemptParams{AttemptID: attemptID, OrganizationID: r.orgID, DeviceID: r.deviceID, Epoch: fences[0].Epoch, OwnerInstanceID: uuid.New(), IdentityKeys: keys, OperationKind: "mutation", CommandType: "Reboot"}))
	// The uncommitted admission is invisible to a readiness read. Its fence
	// must still defer replacement without blocking the accepted inventory.
	done := make(chan error, 1)
	go func() { done <- r.store(t, replacementSnapshot(r.base.Add(time.Second), "new", "other", "spare")) }()
	stored := false
	defer func() {
		_ = admission.Rollback()
		if !stored {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("inventory did not finish after releasing the admission fence")
			}
		}
	}()
	select {
	case err = <-done:
		stored = true
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("mutation admission blocked accepted inventory")
	}
	incoming := r.boardID(t, "new")
	require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, old))
	require.Equal(t, [2]int64{}, replacementOwner(t, r, incoming))
	require.NoError(t, admission.Commit())
	// Committed unresolved attempts still fail readiness after acquiring the
	// now-free fence. Completion permits the next accepted poll to transfer.
	require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(2*time.Second), "new", "other", "spare")))
	require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, old))
	require.Equal(t, [2]int64{}, replacementOwner(t, r, incoming))
	changed, err := sqlc.New(r.db).FinishDeviceWriteAttempt(t.Context(), sqlc.FinishDeviceWriteAttemptParams{AttemptID: attemptID, Status: "complete", WriteOutcome: []byte(`{}`)})
	require.NoError(t, err)
	require.EqualValues(t, 1, changed)
	require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(3*time.Second), "new", "other", "spare")))
	require.Equal(t, [2]int64{}, replacementOwner(t, r, old))
	require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, incoming))
	require.Equal(t, 1, replacementEventCount(t, r))
}

func TestAutomaticReplacementDefersForModeObservationOrMissingFence(t *testing.T) {
	for _, missingFence := range []bool{false, true} {
		name := "mode observation"
		if missingFence {
			name = "missing fence"
		}
		t.Run(name, func(t *testing.T) {
			r := newModulePSUIdentityRig(t)
			require.NoError(t, r.store(t, replacementSnapshot(r.base, "old", "other", "spare")))
			old := r.boardID(t, "old")
			client := replacementAccount(t, r, 0)
			assignReplacementBoard(t, r, old, client, 0, r.base.Add(time.Millisecond))
			observation, err := r.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer observation.Rollback()
			if missingFence {
				_, err = r.db.ExecContext(t.Context(), `DELETE FROM device_write_fence WHERE device_id=$1`, r.deviceID)
			} else {
				_, err = observation.ExecContext(t.Context(), `UPDATE container_mining_group_state SET observed=observed||'{"enabled":false}' WHERE device_id=$1`, r.deviceID)
			}
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() { done <- r.store(t, replacementSnapshot(r.base.Add(time.Second), "new", "other", "spare")) }()
			stored := false
			defer func() {
				_ = observation.Rollback()
				if !stored {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("inventory did not finish after releasing mode observation")
					}
				}
			}()
			select {
			case err = <-done:
				stored = true
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("unavailable allocation readiness blocked accepted inventory")
			}
			incoming := r.boardID(t, "new")
			require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, old))
			require.Equal(t, [2]int64{}, replacementOwner(t, r, incoming))
			require.NoError(t, observation.Rollback())
			require.NoError(t, sqlc.New(r.db).EnsureDeviceWriteFences(t.Context(), []int64{r.deviceID}))
			_, err = r.db.ExecContext(t.Context(), `UPDATE container_mining_group_state SET observed_at=clock_timestamp() WHERE device_id=$1`, r.deviceID)
			require.NoError(t, err)
			require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(2*time.Second), "new", "other", "spare")))
			require.Equal(t, [2]int64{}, replacementOwner(t, r, old))
			require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, incoming))
		})
	}
}

func TestAutomaticReplacementDefersBeforeFenceWhenOrganizationIsBusy(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	require.NoError(t, r.store(t, replacementSnapshot(r.base, "old", "other", "spare")))
	old := r.boardID(t, "old")
	client := replacementAccount(t, r, 0)
	assignReplacementBoard(t, r, old, client, 0, r.base.Add(time.Millisecond))
	// Save accepted inventory while a separate admission defers its transfer.
	// The readiness worker later retries replacement outside telemetry's org lock.
	inventoryFence, err := r.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer inventoryFence.Rollback()
	_, err = sqlc.New(inventoryFence).LockDeviceWriteFences(t.Context(), []int64{r.deviceID})
	require.NoError(t, err)
	observed := r.base.Add(time.Second)
	require.NoError(t, r.store(t, replacementSnapshot(observed, "new", "other", "spare")))
	require.NoError(t, inventoryFence.Commit())
	incoming := r.boardID(t, "new")
	admission, err := r.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer admission.Rollback()
	q := sqlc.New(admission)
	_, err = q.LockHostedAccountOrganization(t.Context(), r.orgID)
	require.NoError(t, err)
	done := make(chan error, 1)
	reconcile := func() error {
		return sqlc.New(r.db).ReconcileHostedHashboardReplacements(t.Context(), sqlc.ReconcileHostedHashboardReplacementsParams{OrganizationID: r.orgID, DeviceID: r.deviceID, ObservedAt: observed})
	}
	go func() { done <- reconcile() }()
	finished := false
	defer func() {
		_ = admission.Rollback()
		if !finished {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("replacement did not finish after organization admission ended")
			}
		}
	}()
	select {
	case err = <-done:
		finished = true
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("replacement waited for organization admission")
	}
	// Organization admission must still acquire the fence. Taking it first in
	// replacement would create the reverse dependency and a database deadlock.
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	fences, err := q.LockDeviceWriteFences(ctx, []int64{r.deviceID})
	require.NoError(t, err)
	require.Len(t, fences, 1)
	require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, old))
	require.Equal(t, [2]int64{}, replacementOwner(t, r, incoming))
	require.NoError(t, admission.Commit())
	require.NoError(t, reconcile())
	require.Equal(t, [2]int64{}, replacementOwner(t, r, old))
	require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, incoming))
	require.Equal(t, 1, replacementEventCount(t, r))
}
