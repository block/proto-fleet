package reconciler

import (
	"context"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/minerchannel/models"
	storemocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

type fakeConfigAdapter struct {
	dispatches int
}

func (*fakeConfigAdapter) Dimension() models.MinerChannelConfigDimension          { return "test_dimension" }
func (*fakeConfigAdapter) HasDesiredState(*models.MinerChannelDesiredConfig) bool { return true }
func (*fakeConfigAdapter) Policy() ConfigDimensionPolicy                          { return ConfigDimensionPolicy{} }
func (*fakeConfigAdapter) Supported(context.Context, models.ConfigEnforcementCandidate) bool {
	return true
}
func (*fakeConfigAdapter) Desired(context.Context, models.ConfigEnforcementCandidate) (DesiredDimensionState, error) {
	return DesiredDimensionState{ComparableHash: "comparable", RevisionHash: "revision", Value: "fake"}, nil
}
func (*fakeConfigAdapter) Observe(context.Context, models.ConfigEnforcementCandidate) (ObservedDimensionState, error) {
	return ObservedDimensionState{NormalizedJSON: []byte(`{"fake":true}`), ComparableHash: "comparable"}, nil
}
func (a *fakeConfigAdapter) Dispatch(context.Context, models.ConfigEnforcementCandidate, DesiredDimensionState) (*command.CommandResult, error) {
	a.dispatches++
	return &command.CommandResult{BatchIdentifier: "batch-1", DispatchedDeviceIdentifiers: []string{"miner-1"}}, nil
}

func TestConfigEnforcerNewRevisionDispatchesEvenWhenComparableStateMatches(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := storemocks.NewMockMinerChannelConfigEnforcementStore(ctrl)
	adapter := &fakeConfigAdapter{}
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	candidate := models.ConfigEnforcementCandidate{
		OrgID: 1, DeviceIdentifier: "miner-1", Dimension: adapter.Dimension(),
		ObservedStateHash: stringPointer("comparable"), ConfigObservedAt: timePointer(now.Add(-time.Minute)),
		DesiredStateHash: stringPointer("old-revision"),
	}

	store.EXPECT().ListOrgsWithDesiredConfig(gomock.Any()).Return([]int64{1}, nil)
	store.EXPECT().ListConfigEnforcementCandidates(gomock.Any(), int64(1), adapter.Dimension()).Return([]models.ConfigEnforcementCandidate{candidate}, nil)
	store.EXPECT().ClaimConfigDispatch(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, params models.ConfigEnforcementMutationParams) (bool, error) {
		if params.DesiredStateHash != "revision" {
			t.Fatalf("desired hash = %q", params.DesiredStateHash)
		}
		return true, nil
	})
	store.EXPECT().MarkConfigDispatched(gomock.Any(), gomock.Any()).Return(true, nil)

	enforcer := NewConfigEnforcer(store, adapter)
	enforcer.now = func() time.Time { return now }
	enforcer.Reconcile(context.Background())
	if adapter.dispatches != 1 {
		t.Fatalf("dispatches = %d, want 1", adapter.dispatches)
	}
}

func TestConfigEnforcerConfirmsOnlyPostDispatchObservation(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := storemocks.NewMockMinerChannelConfigEnforcementStore(ctrl)
	adapter := &fakeConfigAdapter{}
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	dispatched := models.EnforcementStateDispatched
	candidate := models.ConfigEnforcementCandidate{
		OrgID: 1, DeviceIdentifier: "miner-1", Dimension: adapter.Dimension(), State: &dispatched,
		ObservedStateHash: stringPointer("comparable"), ConfigObservedAt: timePointer(now.Add(-time.Minute)),
		DesiredStateHash: stringPointer("revision"), LastDispatchedAt: timePointer(now.Add(-2 * time.Minute)),
	}

	store.EXPECT().ListOrgsWithDesiredConfig(gomock.Any()).Return([]int64{1}, nil)
	store.EXPECT().ListConfigEnforcementCandidates(gomock.Any(), int64(1), adapter.Dimension()).Return([]models.ConfigEnforcementCandidate{candidate}, nil)
	store.EXPECT().MarkConfigConfirmed(gomock.Any(), gomock.Any()).Return(true, nil)

	enforcer := NewConfigEnforcer(store, adapter)
	enforcer.now = func() time.Time { return now }
	enforcer.Reconcile(context.Background())
	if adapter.dispatches != 0 {
		t.Fatalf("dispatches = %d, want 0", adapter.dispatches)
	}
}

func TestConfigEnforcerNewRevisionWaitsForPreviousBatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := storemocks.NewMockMinerChannelConfigEnforcementStore(ctrl)
	adapter := &fakeConfigAdapter{}
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	candidate := models.ConfigEnforcementCandidate{
		OrgID: 1, DeviceIdentifier: "miner-1", Dimension: adapter.Dimension(),
		ObservedStateHash: stringPointer("comparable"), ConfigObservedAt: timePointer(now.Add(-time.Minute)),
		DesiredStateHash: stringPointer("old-revision"), LastBatchUUID: stringPointer("old-batch"),
	}

	store.EXPECT().ListOrgsWithDesiredConfig(gomock.Any()).Return([]int64{1}, nil)
	store.EXPECT().ListConfigEnforcementCandidates(gomock.Any(), int64(1), adapter.Dimension()).Return([]models.ConfigEnforcementCandidate{candidate}, nil)
	store.EXPECT().IsCommandBatchFinished(gomock.Any(), "old-batch").Return(false, nil)

	enforcer := NewConfigEnforcer(store, adapter)
	enforcer.now = func() time.Time { return now }
	enforcer.Reconcile(context.Background())
	if adapter.dispatches != 0 {
		t.Fatalf("dispatches = %d, want 0", adapter.dispatches)
	}
}

func TestConfigEnforcerDriftedStateWaitsForPreviousBatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	store := storemocks.NewMockMinerChannelConfigEnforcementStore(ctrl)
	adapter := &fakeConfigAdapter{}
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	drifted := models.EnforcementStateDrifted
	candidate := models.ConfigEnforcementCandidate{
		OrgID: 1, DeviceIdentifier: "miner-1", Dimension: adapter.Dimension(), State: &drifted,
		ObservedStateHash: stringPointer("different"), ConfigObservedAt: timePointer(now.Add(-time.Minute)),
		DesiredStateHash: stringPointer("revision"), LastBatchUUID: stringPointer("open-batch"),
		LastDispatchedAt: timePointer(now.Add(-10 * time.Minute)),
	}

	store.EXPECT().ListOrgsWithDesiredConfig(gomock.Any()).Return([]int64{1}, nil)
	store.EXPECT().ListConfigEnforcementCandidates(gomock.Any(), int64(1), adapter.Dimension()).Return([]models.ConfigEnforcementCandidate{candidate}, nil)
	store.EXPECT().IsCommandBatchFinished(gomock.Any(), "open-batch").Return(false, nil)

	enforcer := NewConfigEnforcer(store, adapter)
	enforcer.now = func() time.Time { return now }
	enforcer.Reconcile(context.Background())
	if adapter.dispatches != 0 {
		t.Fatalf("dispatches = %d, want 0", adapter.dispatches)
	}
}

func stringPointer(value string) *string     { return &value }
func timePointer(value time.Time) *time.Time { return &value }
