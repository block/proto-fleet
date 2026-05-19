package command

import (
	"context"
	"errors"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// fakeCurtailmentActiveQuerier records the last (orgID) call and returns the
// configured device set.
type fakeCurtailmentActiveQuerier struct {
	active    []string
	err       error
	calls     int
	lastOrgID int64
}

func (f *fakeCurtailmentActiveQuerier) ListActiveCurtailedDevices(_ context.Context, orgID int64) ([]string, error) {
	f.calls++
	f.lastOrgID = orgID
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.active...), nil
}

func TestCurtailmentActiveFilter_BypassesReconcilerCurtail(t *testing.T) {
	q := &fakeCurtailmentActiveQuerier{active: []string{"miner-1"}}
	f := NewCurtailmentActiveFilter(q)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.Curtail,
		OrganizationID:    1,
		Actor:             session.ActorCurtailment,
		DeviceIdentifiers: []string{"miner-1", "miner-2"},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"miner-1", "miner-2"}, out.Kept)
	assert.Equal(t, 0, len(out.Skipped))
	assert.Equal(t, 0, q.calls, "reconciler self-bypass must not consult the store")
}

func TestCurtailmentActiveFilter_BypassesReconcilerUncurtail(t *testing.T) {
	q := &fakeCurtailmentActiveQuerier{active: []string{"miner-1"}}
	f := NewCurtailmentActiveFilter(q)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.Uncurtail,
		OrganizationID:    1,
		Actor:             session.ActorCurtailment,
		DeviceIdentifiers: []string{"miner-1"},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"miner-1"}, out.Kept)
	assert.Equal(t, 0, len(out.Skipped))
	assert.Equal(t, 0, q.calls)
}

func TestCurtailmentActiveFilter_NonReconcilerCurtailIsGated(t *testing.T) {
	// A Curtail command from a user/API caller (Actor empty) must still be
	// gated against the active-event set; only the reconciler bypasses.
	q := &fakeCurtailmentActiveQuerier{active: []string{"miner-1"}}
	f := NewCurtailmentActiveFilter(q)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.Curtail,
		OrganizationID:    1,
		DeviceIdentifiers: []string{"miner-1", "miner-2"},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"miner-2"}, out.Kept)
	assert.Equal(t, 1, len(out.Skipped))
	assert.Equal(t, "miner-1", out.Skipped[0].DeviceIdentifier)
}

func TestCurtailmentActiveFilter_NoActiveEventsFastPath(t *testing.T) {
	q := &fakeCurtailmentActiveQuerier{active: nil}
	f := NewCurtailmentActiveFilter(q)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.Reboot,
		OrganizationID:    1,
		DeviceIdentifiers: []string{"miner-1", "miner-2"},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"miner-1", "miner-2"}, out.Kept)
	assert.Equal(t, 0, len(out.Skipped))
	assert.Equal(t, 1, q.calls, "fast path still reads the store once to confirm emptiness")
}

func TestCurtailmentActiveFilter_PartialSkipPreservesOrder(t *testing.T) {
	q := &fakeCurtailmentActiveQuerier{active: []string{"miner-1", "miner-3"}}
	f := NewCurtailmentActiveFilter(q)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.SetPowerTarget,
		OrganizationID:    7,
		DeviceIdentifiers: []string{"miner-1", "miner-2", "miner-3", "miner-4"},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"miner-2", "miner-4"}, out.Kept)
	assert.Equal(t, 2, len(out.Skipped))
	assert.Equal(t, "miner-1", out.Skipped[0].DeviceIdentifier)
	assert.Equal(t, CurtailmentActiveFilterName, out.Skipped[0].FilterName)
	assert.Equal(t, curtailmentActiveSkipReason, out.Skipped[0].Reason)
	assert.Equal(t, "miner-3", out.Skipped[1].DeviceIdentifier)
	assert.Equal(t, int64(7), q.lastOrgID)
}

func TestCurtailmentActiveFilter_EmptyInputPassesThrough(t *testing.T) {
	q := &fakeCurtailmentActiveQuerier{active: []string{"miner-1"}}
	f := NewCurtailmentActiveFilter(q)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:    commandtype.Reboot,
		OrganizationID: 1,
	})
	assert.NoError(t, err)
	assert.Equal(t, 0, len(out.Kept))
	assert.Equal(t, 0, len(out.Skipped))
	assert.Equal(t, 0, q.calls, "empty input should skip the store call")
}

func TestCurtailmentActiveFilter_StoreErrorBubblesUp(t *testing.T) {
	q := &fakeCurtailmentActiveQuerier{err: errors.New("db down")}
	f := NewCurtailmentActiveFilter(q)

	out, err := f.Apply(context.Background(), CommandFilterInput{
		CommandType:       commandtype.Reboot,
		OrganizationID:    1,
		DeviceIdentifiers: []string{"miner-1"},
	})
	assert.Error(t, err)
	assert.Equal(t, 0, len(out.Kept))
	assert.Equal(t, 0, len(out.Skipped))
}
