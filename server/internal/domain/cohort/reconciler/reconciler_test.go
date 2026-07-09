package reconciler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

func TestProcessCandidate_ConfirmsMatchingFreshFirmware(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	store := &fakeFirmwareStore{}
	r := New(Config{}, store, &fakeFirmwareDispatcher{}, fakeFirmwareMetadata{versions: map[string]string{"fw-1": "v2"}})
	r.now = func() time.Time { return now }

	r.processCandidate(t.Context(), firmwareCandidate(now, nil, "v2"))

	require.Len(t, store.confirmed, 1)
	assert.Equal(t, "miner-1", store.confirmed[0].DeviceIdentifier)
	assert.Empty(t, store.claimed)
}

func TestProcessCandidate_DispatchesDriftedFirmware(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	store := &fakeFirmwareStore{claimResult: true}
	dispatcher := &fakeFirmwareDispatcher{
		result: &command.CommandResult{
			BatchIdentifier:             "batch-1",
			DispatchedDeviceIdentifiers: []string{"miner-1"},
			DispatchedCount:             1,
		},
	}
	r := New(Config{}, store, dispatcher, fakeFirmwareMetadata{versions: map[string]string{"fw-1": "v2"}})
	r.now = func() time.Time { return now }

	state := models.EnforcementStateDrifted
	r.processCandidate(t.Context(), firmwareCandidate(now, &state, "v1"))

	require.Len(t, store.claimed, 1)
	require.Len(t, dispatcher.calls, 1)
	assert.Equal(t, "fw-1", dispatcher.calls[0].firmwareFileID)
	require.Len(t, store.dispatched, 1)
	assert.Equal(t, "batch-1", store.dispatched[0].LastBatchUUID)
}

func TestProcessCandidate_DispatchesWhenTargetChangesDuringOldDispatch(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	store := &fakeFirmwareStore{claimResult: true}
	dispatcher := &fakeFirmwareDispatcher{
		result: &command.CommandResult{
			BatchIdentifier:             "batch-2",
			DispatchedDeviceIdentifiers: []string{"miner-1"},
			DispatchedCount:             1,
		},
	}
	r := New(Config{RedispatchCooldown: time.Hour}, store, dispatcher, fakeFirmwareMetadata{versions: map[string]string{"fw-2": "v3"}})
	r.now = func() time.Time { return now }

	state := models.EnforcementStateDispatched
	oldTarget := "fw-1"
	oldVersion := "v2"
	lastDispatchedAt := now.Add(-time.Minute)
	candidate := firmwareCandidate(now, &state, "v1")
	candidate.FirmwareFileID = "fw-2"
	candidate.StateDesiredFirmwareFileID = &oldTarget
	candidate.StateDesiredFirmwareVersion = &oldVersion
	candidate.LastDispatchedAt = &lastDispatchedAt

	r.processCandidate(t.Context(), candidate)

	require.Len(t, store.claimed, 1)
	assert.Equal(t, "fw-2", store.claimed[0].DesiredFirmwareFileID)
	assert.Equal(t, "v3", store.claimed[0].DesiredFirmwareVersion)
	require.Len(t, dispatcher.calls, 1)
	assert.Equal(t, "fw-2", dispatcher.calls[0].firmwareFileID)
}

func TestProcessCandidate_HoldsStaleObservation(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	store := &fakeFirmwareStore{claimResult: true}
	dispatcher := &fakeFirmwareDispatcher{}
	r := New(Config{ObservationMaxAge: time.Minute}, store, dispatcher, fakeFirmwareMetadata{versions: map[string]string{"fw-1": "v2"}})
	r.now = func() time.Time { return now }

	r.processCandidate(t.Context(), firmwareCandidate(now.Add(-2*time.Minute), nil, "v1"))

	assert.Empty(t, store.confirmed)
	assert.Empty(t, store.claimed)
	assert.Empty(t, dispatcher.calls)
}

func TestProcessCandidate_ClearsMissingFirmwareTarget(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	store := &fakeFirmwareStore{}
	dispatcher := &fakeFirmwareDispatcher{}
	metadata := fakeFirmwareMetadata{
		errs: map[string]error{
			"fw-1": fleeterror.NewNotFoundError("firmware file not found"),
		},
	}
	r := New(Config{}, store, dispatcher, metadata)
	r.now = func() time.Time { return now }

	candidate := firmwareCandidate(now, nil, "v1")
	candidate.DesiredFirmwareVersion = ""
	r.processCandidate(t.Context(), candidate)

	require.Len(t, store.clearedMissingTargets, 1)
	assert.Equal(t, int64(7), store.clearedMissingTargets[0].orgID)
	assert.Equal(t, "fw-1", store.clearedMissingTargets[0].firmwareFileID)
	assert.Empty(t, store.claimed)
	assert.Empty(t, dispatcher.calls)
}

func TestProcessCandidate_ClearsMissingFirmwareTargetDespiteCachedDesiredVersion(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	store := &fakeFirmwareStore{}
	dispatcher := &fakeFirmwareDispatcher{}
	metadata := fakeFirmwareMetadata{
		errs: map[string]error{
			"fw-1": fleeterror.NewNotFoundError("firmware file not found"),
		},
	}
	r := New(Config{}, store, dispatcher, metadata)
	r.now = func() time.Time { return now }

	state := models.EnforcementStateDispatched
	candidate := firmwareCandidate(now, &state, "v1")
	candidate.StateDesiredFirmwareFileID = ptrString("fw-1")
	candidate.StateDesiredFirmwareVersion = ptrString("v2")
	r.processCandidate(t.Context(), candidate)

	require.Len(t, store.clearedMissingTargets, 1)
	assert.Equal(t, "fw-1", store.clearedMissingTargets[0].firmwareFileID)
	assert.Empty(t, store.claimed)
	assert.Empty(t, dispatcher.calls)
}

func TestProcessCandidate_HoldsSkippedFirmwareDispatchWithoutRetry(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	store := &fakeFirmwareStore{claimResult: true}
	dispatcher := &fakeFirmwareDispatcher{
		result: &command.CommandResult{
			Skipped: []command.SkippedDevice{
				{
					DeviceIdentifier: "miner-1",
					FilterName:       "curtailment_active",
					Reason:           "device is part of an active curtailment event",
				},
			},
		},
	}
	r := New(Config{}, store, dispatcher, fakeFirmwareMetadata{versions: map[string]string{"fw-1": "v2"}})
	r.now = func() time.Time { return now }

	state := models.EnforcementStateDrifted
	r.processCandidate(t.Context(), firmwareCandidate(now, &state, "v1"))

	require.Len(t, store.held, 1)
	assert.Equal(t, models.EnforcementStateDrifted, store.held[0].RetryState)
	assert.Equal(t, "fw-1", store.held[0].DesiredFirmwareFileID)
	assert.Equal(t, "v2", store.held[0].DesiredFirmwareVersion)
	assert.Contains(t, store.held[0].LastError, "curtailment")
	assert.Empty(t, store.failures)
	assert.Empty(t, store.dispatched)
}

type fakeFirmwareMetadata struct {
	versions map[string]string
	errs     map[string]error
}

func (f fakeFirmwareMetadata) GetFirmwareMetadata(fileID string) (files.FirmwareMetadata, error) {
	if err := f.errs[fileID]; err != nil {
		return files.FirmwareMetadata{}, err
	}
	return files.FirmwareMetadata{FirmwareVersion: f.versions[fileID]}, nil
}

func firmwareCandidate(observedAt time.Time, state *models.EnforcementState, observedVersion string) models.FirmwareEnforcementCandidate {
	return models.FirmwareEnforcementCandidate{
		OrgID:                       7,
		DeviceIdentifier:            "miner-1",
		ActorUserID:                 42,
		ActorExternalUserID:         "user-42",
		ActorUsername:               "operator",
		FirmwareFileID:              "fw-1",
		StateDesiredFirmwareFileID:  ptrString("fw-1"),
		StateDesiredFirmwareVersion: ptrString("v2"),
		ObservedFirmwareVersion:     &observedVersion,
		FirmwareObservedAt:          &observedAt,
		State:                       state,
	}
}

func ptrString(value string) *string {
	return &value
}

type fakeFirmwareDispatcher struct {
	calls  []firmwareDispatchCall
	result *command.CommandResult
	err    error
}

type firmwareDispatchCall struct {
	selector       *pb.DeviceSelector
	firmwareFileID string
}

func (f *fakeFirmwareDispatcher) FirmwareUpdate(ctx context.Context, selector *pb.DeviceSelector, firmwareFileID string) (*command.CommandResult, error) {
	f.calls = append(f.calls, firmwareDispatchCall{selector: selector, firmwareFileID: firmwareFileID})
	return f.result, f.err
}

type fakeFirmwareStore struct {
	claimResult bool

	confirmed             []models.MarkFirmwareConfirmedParams
	claimed               []models.ClaimFirmwareDispatchParams
	dispatched            []models.MarkFirmwareDispatchedParams
	drifted               []models.MarkFirmwareDriftedParams
	failures              []models.MarkFirmwareDispatchFailureParams
	held                  []models.MarkFirmwareDispatchHeldParams
	clearedMissingTargets []clearMissingTargetCall
}

type clearMissingTargetCall struct {
	orgID          int64
	firmwareFileID string
}

func (f *fakeFirmwareStore) ListOrgsWithFirmwareTargets(context.Context) ([]int64, error) {
	return nil, nil
}

func (f *fakeFirmwareStore) ListFirmwareEnforcementCandidates(context.Context, int64) ([]models.FirmwareEnforcementCandidate, error) {
	return nil, nil
}

func (f *fakeFirmwareStore) ClearMissingFirmwareTarget(_ context.Context, orgID int64, firmwareFileID string) (int64, error) {
	f.clearedMissingTargets = append(f.clearedMissingTargets, clearMissingTargetCall{orgID: orgID, firmwareFileID: firmwareFileID})
	return 1, nil
}

func (f *fakeFirmwareStore) ClaimFirmwareDispatch(_ context.Context, params models.ClaimFirmwareDispatchParams) (bool, error) {
	f.claimed = append(f.claimed, params)
	return f.claimResult, nil
}

func (f *fakeFirmwareStore) MarkFirmwareDispatched(_ context.Context, params models.MarkFirmwareDispatchedParams) (bool, error) {
	f.dispatched = append(f.dispatched, params)
	return true, nil
}

func (f *fakeFirmwareStore) MarkFirmwareConfirmed(_ context.Context, params models.MarkFirmwareConfirmedParams) (bool, error) {
	f.confirmed = append(f.confirmed, params)
	return true, nil
}

func (f *fakeFirmwareStore) MarkFirmwareDrifted(_ context.Context, params models.MarkFirmwareDriftedParams) (bool, error) {
	f.drifted = append(f.drifted, params)
	return true, nil
}

func (f *fakeFirmwareStore) MarkFirmwareDispatchFailure(_ context.Context, params models.MarkFirmwareDispatchFailureParams) (bool, error) {
	f.failures = append(f.failures, params)
	return true, nil
}

func (f *fakeFirmwareStore) MarkFirmwareDispatchHeld(_ context.Context, params models.MarkFirmwareDispatchHeldParams) (bool, error) {
	f.held = append(f.held, params)
	return true, nil
}

func (f *fakeFirmwareStore) IsCommandBatchFinished(context.Context, string) (bool, error) {
	return true, nil
}

func (f *fakeFirmwareStore) UpsertCohortReconcilerHeartbeat(context.Context, time.Time, uuid.UUID, *int32, int32) error {
	return nil
}
