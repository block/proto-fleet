package command

import (
	"context"
	"errors"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
)

// fakeFilter is a minimal CommandFilter for exercising applyFilters in
// isolation from the full Service. Each instance excludes a fixed set of
// device identifiers and records its invocations so tests can assert
// chaining/ordering behaviour.
type fakeFilter struct {
	name      string
	exclude   map[string]struct{}
	calls     int
	lastInput CommandFilterInput
	err       error
}

func newFakeFilter(name string, exclude ...string) *fakeFilter {
	set := make(map[string]struct{}, len(exclude))
	for _, e := range exclude {
		set[e] = struct{}{}
	}
	return &fakeFilter{name: name, exclude: set}
}

func (f *fakeFilter) Name() string { return f.name }

func (f *fakeFilter) Apply(_ context.Context, in CommandFilterInput) (CommandFilterOutput, error) {
	f.calls++
	f.lastInput = in
	if f.err != nil {
		return CommandFilterOutput{}, f.err
	}
	var kept []string
	var skipped []SkippedDevice
	for _, id := range in.DeviceIdentifiers {
		if _, drop := f.exclude[id]; drop {
			skipped = append(skipped, SkippedDevice{
				DeviceIdentifier: id,
				FilterName:       f.name,
				Reason:           "excluded by " + f.name,
			})
			continue
		}
		kept = append(kept, id)
	}
	return CommandFilterOutput{Kept: kept, Skipped: skipped}, nil
}

func TestApplyFilters_NoFiltersIsPassThrough(t *testing.T) {
	in := CommandFilterInput{
		CommandType:       commandtype.SetPowerTarget,
		DeviceIdentifiers: []string{"a", "b", "c"},
	}
	kept, skipped, err := applyFilters(context.Background(), nil, in)
	assert.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, kept)
	assert.Equal(t, 0, len(skipped))
}

func TestApplyFilters_SingleFilterPartitions(t *testing.T) {
	f := newFakeFilter("f1", "b")
	in := CommandFilterInput{
		CommandType:       commandtype.SetPowerTarget,
		DeviceIdentifiers: []string{"a", "b", "c"},
	}
	kept, skipped, err := applyFilters(context.Background(), []CommandFilter{f}, in)
	assert.NoError(t, err)
	assert.Equal(t, []string{"a", "c"}, kept)
	assert.Equal(t, 1, len(skipped))
	assert.Equal(t, "b", skipped[0].DeviceIdentifier)
	assert.Equal(t, "f1", skipped[0].FilterName)
}

func TestApplyFilters_OrderedChainAccumulatesSkips(t *testing.T) {
	// f1 excludes "b"; f2 excludes "c". Expect f2 to see ["a", "c"] (post-f1)
	// and the final skipped slice to record both rejections with their
	// respective filter names.
	f1 := newFakeFilter("f1", "b")
	f2 := newFakeFilter("f2", "c")
	in := CommandFilterInput{
		CommandType:       commandtype.SetPowerTarget,
		DeviceIdentifiers: []string{"a", "b", "c"},
	}
	kept, skipped, err := applyFilters(context.Background(), []CommandFilter{f1, f2}, in)
	assert.NoError(t, err)
	assert.Equal(t, []string{"a"}, kept)
	assert.Equal(t, []string{"a", "c"}, f2.lastInput.DeviceIdentifiers)
	assert.Equal(t, 2, len(skipped))
	assert.Equal(t, "b", skipped[0].DeviceIdentifier)
	assert.Equal(t, "f1", skipped[0].FilterName)
	assert.Equal(t, "c", skipped[1].DeviceIdentifier)
	assert.Equal(t, "f2", skipped[1].FilterName)
}

func TestApplyFilters_ShortCircuitsWhenKeptEmpty(t *testing.T) {
	// If filter 1 leaves nothing, filter 2 is never asked.
	f1 := newFakeFilter("f1", "a", "b", "c")
	f2 := newFakeFilter("f2")
	in := CommandFilterInput{
		CommandType:       commandtype.SetPowerTarget,
		DeviceIdentifiers: []string{"a", "b", "c"},
	}
	kept, skipped, err := applyFilters(context.Background(), []CommandFilter{f1, f2}, in)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(kept))
	assert.Equal(t, 3, len(skipped))
	assert.Equal(t, 1, f1.calls)
	assert.Equal(t, 0, f2.calls, "filter 2 must not be invoked once kept goes empty")
}

func TestApplyFilters_ErrorBubblesUpAndStopsChain(t *testing.T) {
	f1 := &fakeFilter{name: "f1", err: errors.New("boom")}
	f2 := newFakeFilter("f2")
	in := CommandFilterInput{
		CommandType:       commandtype.SetPowerTarget,
		DeviceIdentifiers: []string{"a"},
	}
	_, _, err := applyFilters(context.Background(), []CommandFilter{f1, f2}, in)
	assert.Error(t, err)
	assert.Equal(t, 0, f2.calls)
}

func TestApplyFilters_Idempotent(t *testing.T) {
	// Re-running a filter on its own output is a no-op: nothing further
	// gets skipped, kept slice is stable.
	f := newFakeFilter("f1", "b")
	in := CommandFilterInput{
		CommandType:       commandtype.SetPowerTarget,
		DeviceIdentifiers: []string{"a", "b", "c"},
	}
	kept1, _, _ := applyFilters(context.Background(), []CommandFilter{f}, in)
	in2 := in
	in2.DeviceIdentifiers = kept1
	kept2, skipped2, err := applyFilters(context.Background(), []CommandFilter{f}, in2)
	assert.NoError(t, err)
	assert.Equal(t, kept1, kept2)
	assert.Equal(t, 0, len(skipped2))
}
