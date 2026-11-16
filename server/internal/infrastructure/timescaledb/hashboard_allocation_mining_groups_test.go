package timescaledb

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/mininggroups"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestAutomaticHashboardReplacementRequiresReadyMiningGroups(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
	}{
		{name: "disabled", query: `UPDATE container_mining_group_state SET observed=observed||'{"enabled":false}' WHERE device_id=$1`},
		{name: "unknown", query: `DELETE FROM container_mining_group_state WHERE device_id=$1`},
		{name: "stale", query: `UPDATE container_mining_group_state SET observed_at=now()-interval '3 minutes' WHERE device_id=$1`},
		{name: "suspended", query: `UPDATE container_mining_group_state SET observed=observed||'{"suspended":true}' WHERE device_id=$1`},
		{name: "pending", query: `UPDATE container_mining_group_state SET status='pending',operation_kind='deactivate' WHERE device_id=$1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newModulePSUIdentityRig(t)
			require.NoError(t, r.store(t, replacementSnapshot(r.base, "old", "other", "spare")))
			old := r.boardID(t, "old")
			client := replacementAccount(t, r, 0)
			assignReplacementBoard(t, r, old, client, 0, r.base.Add(time.Millisecond))
			_, err := r.db.ExecContext(t.Context(), tc.query, r.deviceID)
			require.NoError(t, err)
			require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(time.Second), "new", "other", "spare")), "mode restrictions must not discard live inventory")
			require.Equal(t, [2]int64{client, 0}, replacementOwner(t, r, old))
			require.Equal(t, [2]int64{}, replacementOwner(t, r, r.boardID(t, "new")))
		})
	}
}

type replacementModeMiner struct {
	interfaces.Miner
	enabled  bool
	readErr  error
	actions  []string
	observed chan struct{}
}

func (m *replacementModeMiner) GetMinerForCommand(context.Context, int64) (interfaces.Miner, error) {
	return m, nil
}

func (m *replacementModeMiner) MiningGroups(_ context.Context, request sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	m.actions = append(m.actions, request.Action)
	if request.Action != "observe" {
		return sdk.MiningGroupResult{}, fmt.Errorf("unexpected mining-group mutation: %s", request.Action)
	}
	select {
	case m.observed <- struct{}{}:
	default:
	}
	return sdk.MiningGroupResult{Supported: true, Enabled: m.enabled, Revision: 1, MaxGroups: 289, ObservedAt: time.Now().UTC()}, m.readErr
}

func TestAutomaticHashboardReplacementRefreshesIdleMiningGroups(t *testing.T) {
	for _, tc := range []struct {
		name    string
		missing bool
		enabled bool
		readErr error
	}{
		{name: "stale enabled", enabled: true},
		{name: "missing enabled", missing: true, enabled: true},
		{name: "disabled"},
		{name: "unavailable", readErr: errors.New("controller unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newModulePSUIdentityRig(t)
			_, err := r.db.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, r.deviceID)
			require.NoError(t, err)
			require.NoError(t, r.store(t, replacementSnapshot(r.base, "old", "other", "spare")))
			old, other := r.boardID(t, "old"), r.boardID(t, "other")
			client := replacementAccount(t, r, 0)
			child := replacementAccount(t, r, client)
			neighborClient := replacementAccount(t, r, 0)
			assignReplacementBoard(t, r, old, client, child, r.base.Add(time.Millisecond))
			assignReplacementBoard(t, r, other, neighborClient, 0, r.base.Add(time.Millisecond))
			_, err = r.db.ExecContext(t.Context(), `UPDATE pool_lifecycle_transition SET state='completed' WHERE organization_id=$1`, r.orgID)
			require.NoError(t, err)
			if tc.missing {
				_, err = r.db.ExecContext(t.Context(), `DELETE FROM container_mining_group_state WHERE device_id=$1`, r.deviceID)
			} else {
				_, err = r.db.ExecContext(t.Context(), `UPDATE container_mining_group_state SET observed_at=now()-interval '3 minutes' WHERE device_id=$1`, r.deviceID)
			}
			require.NoError(t, err)
			require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(time.Second), "new", "other", "spare")))
			incoming := r.boardID(t, "new")
			require.Equal(t, [2]int64{client, child}, replacementOwner(t, r, old))
			require.Equal(t, [2]int64{}, replacementOwner(t, r, incoming))
			require.Equal(t, [2]int64{neighborClient, 0}, replacementOwner(t, r, other))
			require.Zero(t, replacementEventCount(t, r))

			miner := &replacementModeMiner{enabled: tc.enabled, readErr: tc.readErr, observed: make(chan struct{}, 1)}
			service := mininggroups.NewService(r.db, miner, nil, nil)
			runCtx, cancel := context.WithCancel(t.Context())
			finished := make(chan struct{})
			startedAt := time.Now()
			go func() {
				service.Run(runCtx)
				close(finished)
			}()
			stop := func() {
				cancel()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Fatal("mining-group readiness worker did not stop")
				}
			}
			t.Cleanup(stop)
			select {
			case <-miner.observed:
			case <-time.After(5 * time.Second):
				t.Fatal("background readiness did not observe the allocated container")
			}
			require.Eventually(t, func() bool {
				state, err := sqlc.New(r.db).GetMiningGroupState(t.Context(), sqlc.GetMiningGroupStateParams{OrganizationID: r.orgID, DeviceID: r.deviceID})
				if err != nil {
					return false
				}
				if tc.readErr != nil {
					return !state.ObservedAt.Valid
				}
				return state.ObservedAt.Valid && state.ObservedAt.Time.After(startedAt)
			}, 5*time.Second, 10*time.Millisecond, "wait for observation persistence before stopping the worker")
			stop()
			require.Equal(t, []string{"observe"}, miner.actions, "idle refresh must only read controller mode")
			require.Equal(t, [2]int64{client, child}, replacementOwner(t, r, old), "refresh must leave ownership for the next accepted inventory")
			require.NoError(t, r.store(t, replacementSnapshot(r.base.Add(2*time.Second), "new", "other", "spare")))
			if tc.enabled && tc.readErr == nil {
				require.Equal(t, [2]int64{}, replacementOwner(t, r, old))
				require.Equal(t, [2]int64{client, child}, replacementOwner(t, r, incoming))
				require.Equal(t, 1, replacementEventCount(t, r))
			} else {
				require.Equal(t, [2]int64{client, child}, replacementOwner(t, r, old))
				require.Equal(t, [2]int64{}, replacementOwner(t, r, incoming))
				require.Zero(t, replacementEventCount(t, r))
			}
			require.Equal(t, [2]int64{neighborClient, 0}, replacementOwner(t, r, other))
			state, err := sqlc.New(r.db).GetMiningGroupState(t.Context(), sqlc.GetMiningGroupStateParams{OrganizationID: r.orgID, DeviceID: r.deviceID})
			require.NoError(t, err)
			if tc.readErr != nil {
				require.False(t, state.ObservedAt.Valid, "failed reads must invalidate cached allocation authority")
			} else {
				require.True(t, state.ObservedAt.Valid)
				require.WithinDuration(t, time.Now(), state.ObservedAt.Time, 10*time.Second)
			}
		})
	}
}
