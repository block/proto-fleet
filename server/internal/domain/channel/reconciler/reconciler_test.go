package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	channel "github.com/block/proto-fleet/server/internal/domain/channel"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	telemetrymodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsv2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

type fakeStore struct {
	rows     []channel.Enforcement
	outcomes map[string]channel.CommandOutcome

	claimCalls       int
	holdCalls        int
	pendingCalls     int
	dispatchedCalls  int
	verifyingCalls   int
	attentionCalls   int
	observationCalls []observationCall
	confirmCalls     int
}

type observationCall struct {
	observation     channel.Observation
	nextReconcileAt time.Time
}

func (f *fakeStore) ListForReconcile(context.Context, int32) ([]channel.Enforcement, error) {
	return append([]channel.Enforcement(nil), f.rows...), nil
}

func (f *fakeStore) Claim(
	_ context.Context,
	row channel.Enforcement,
	batchID string,
	at time.Time,
) (channel.Enforcement, error) {
	f.claimCalls++
	row.State = channel.EnforcementStateDispatching
	row.CommandBatchUUID = batchID
	row.ClaimedAt = &at
	row.Revision++
	return row, nil
}

func (f *fakeStore) Hold(
	_ context.Context,
	row channel.Enforcement,
	_ string,
	at time.Time,
) error {
	f.holdCalls++
	f.updateRow(row.ID, func(current *channel.Enforcement) {
		current.State = channel.EnforcementStateHeld
		current.HeldAt = &at
		current.CommandBatchUUID = ""
		current.AttemptCount = 0
	})
	return nil
}

func (f *fakeStore) ReturnPending(context.Context, channel.Enforcement, string) error {
	f.pendingCalls++
	return nil
}

func (f *fakeStore) MarkDispatched(
	_ context.Context,
	row channel.Enforcement,
	at time.Time,
) error {
	f.dispatchedCalls++
	f.updateRow(row.ID, func(current *channel.Enforcement) {
		current.State = channel.EnforcementStateDispatched
		current.EnqueuedAt = &at
		current.AttemptCount = 1
	})
	return nil
}

func (f *fakeStore) CommandOutcome(
	_ context.Context,
	row channel.Enforcement,
) (channel.CommandOutcome, error) {
	if outcome, ok := f.outcomes[row.CommandBatchUUID]; ok {
		return outcome, nil
	}
	return channel.CommandOutcome{Status: channel.CommandOutcomeMissing}, nil
}

func (f *fakeStore) MarkVerifying(
	_ context.Context,
	row channel.Enforcement,
	completedAt time.Time,
) error {
	f.verifyingCalls++
	f.updateRow(row.ID, func(current *channel.Enforcement) {
		current.State = channel.EnforcementStateVerifying
		current.CommandCompletedAt = &completedAt
	})
	return nil
}

func (f *fakeStore) RecordObservation(
	_ context.Context,
	_ channel.Enforcement,
	observation channel.Observation,
	nextReconcileAt time.Time,
) error {
	f.observationCalls = append(f.observationCalls, observationCall{
		observation:     observation,
		nextReconcileAt: nextReconcileAt,
	})
	return nil
}

func (f *fakeStore) Confirm(
	context.Context,
	channel.Enforcement,
	channel.Observation,
) error {
	f.confirmCalls++
	return nil
}

func (f *fakeStore) MarkAttentionRequired(
	context.Context,
	channel.Enforcement,
	string,
	time.Time,
) error {
	f.attentionCalls++
	return nil
}

func (f *fakeStore) updateRow(id int64, update func(*channel.Enforcement)) {
	for i := range f.rows {
		if f.rows[i].ID == id {
			update(&f.rows[i])
			return
		}
	}
}

type fakeDispatcher struct {
	result    *command.CommandResult
	err       error
	calls     int
	actor     session.Actor
	deviceIDs []string
	batchID   string
}

func (f *fakeDispatcher) ChannelFirmwareUpdate(
	ctx context.Context,
	selector *pb.DeviceSelector,
	_ string,
	batchID string,
) (*command.CommandResult, error) {
	f.calls++
	f.batchID = batchID
	if info, infoErr := session.GetInfo(ctx); infoErr == nil {
		f.actor = info.Actor
	}
	f.deviceIDs = append(
		[]string(nil),
		selector.GetIncludeDevices().GetDeviceIdentifiers()...,
	)
	return f.result, f.err
}

type fakeSampler struct {
	results  []telemetry.SampleResult
	requests []telemetry.SampleRequest
}

func (f *fakeSampler) SampleDeviceMetrics(
	_ context.Context,
	requests []telemetry.SampleRequest,
) []telemetry.SampleResult {
	f.requests = append([]telemetry.SampleRequest(nil), requests...)
	return append([]telemetry.SampleResult(nil), f.results...)
}

func TestReconcileRestartedDispatchingNeverRedispatches(t *testing.T) {
	store := &fakeStore{rows: []channel.Enforcement{testEnforcement(channel.EnforcementStateDispatching)}}
	dispatcher := &fakeDispatcher{}
	r := newTestReconciler(store, dispatcher, &fakeSampler{})

	r.reconcile(t.Context())

	assert.Zero(t, dispatcher.calls)
	assert.Equal(t, 1, store.attentionCalls)
}

func TestReconcileDispatchedQueueWorkNeverRedispatches(t *testing.T) {
	row := testEnforcement(channel.EnforcementStateDispatched)
	row.AttemptCount = 1
	store := &fakeStore{
		rows: []channel.Enforcement{row},
		outcomes: map[string]channel.CommandOutcome{
			row.CommandBatchUUID: {Status: channel.CommandOutcomeProcessing},
		},
	}
	dispatcher := &fakeDispatcher{}
	r := newTestReconciler(store, dispatcher, &fakeSampler{})

	r.reconcile(t.Context())

	assert.Zero(t, dispatcher.calls)
	assert.Zero(t, store.attentionCalls)
	assert.Zero(t, store.verifyingCalls)
}

func TestReconcileCurtailmentHoldDoesNotConsumeAttempt(t *testing.T) {
	row := testEnforcement(channel.EnforcementStatePending)
	store := &fakeStore{rows: []channel.Enforcement{row}}
	dispatcher := &fakeDispatcher{result: &command.CommandResult{
		Skipped: []command.SkippedDevice{{
			DeviceIdentifier: row.DeviceIdentifier,
			FilterName:       command.CurtailmentActiveFilterName,
		}},
	}}
	r := newTestReconciler(store, dispatcher, &fakeSampler{})

	r.reconcile(t.Context())

	assert.Equal(t, 1, dispatcher.calls)
	assert.Equal(t, session.ActorChannelEnforcement, dispatcher.actor)
	assert.Equal(t, []string{row.DeviceIdentifier}, dispatcher.deviceIDs)
	assert.Equal(t, 1, store.holdCalls)
	assert.Zero(t, store.dispatchedCalls)
	assert.Zero(t, store.rows[0].AttemptCount)
}

func TestReconcilePreEnqueueFailureReturnsPending(t *testing.T) {
	store := &fakeStore{rows: []channel.Enforcement{testEnforcement(channel.EnforcementStatePending)}}
	dispatcher := &fakeDispatcher{err: errors.New("atomic enqueue rolled back")}
	r := newTestReconciler(store, dispatcher, &fakeSampler{})

	r.reconcile(t.Context())

	assert.Equal(t, 1, store.pendingCalls)
	assert.Zero(t, store.attentionCalls)
	assert.Zero(t, store.dispatchedCalls)
}

func TestReconcileAmbiguousEnqueueFailureNeedsAttention(t *testing.T) {
	store := &fakeStore{rows: []channel.Enforcement{testEnforcement(channel.EnforcementStatePending)}}
	dispatcher := &fakeDispatcher{
		err: command.NewEnqueueUncertainError(errors.New("commit result unknown")),
	}
	r := newTestReconciler(store, dispatcher, &fakeSampler{})

	r.reconcile(t.Context())

	assert.Equal(t, 1, store.attentionCalls)
	assert.Zero(t, store.pendingCalls)
	assert.Zero(t, store.dispatchedCalls)
}

func TestReconcilePostEnqueueFailureNeedsAttention(t *testing.T) {
	row := testEnforcement(channel.EnforcementStateDispatched)
	row.AttemptCount = 1
	store := &fakeStore{
		rows: []channel.Enforcement{row},
		outcomes: map[string]channel.CommandOutcome{
			row.CommandBatchUUID: {
				Status: channel.CommandOutcomeFailed,
				Error:  "upload timed out",
			},
		},
	}
	r := newTestReconciler(store, &fakeDispatcher{}, &fakeSampler{})

	r.reconcile(t.Context())

	assert.Equal(t, 1, store.attentionCalls)
	assert.Zero(t, store.claimCalls)
}

func TestReconcileUnusableVerificationSamplesPersistRetry(t *testing.T) {
	completedAt := time.Date(2026, 8, 18, 1, 0, 0, 0, time.UTC)
	row := testEnforcement(channel.EnforcementStateVerifying)
	row.AttemptCount = 1
	row.CommandCompletedAt = &completedAt
	fresh := telemetry.SampleResult{
		DeviceID:    telemetrymodels.DeviceIdentifier(row.DeviceIdentifier),
		OrgID:       row.OrgID,
		FlightStart: completedAt.Add(time.Second),
		Metrics: modelsv2.DeviceMetrics{
			DeviceIdentifier: row.DeviceIdentifier,
			FirmwareVersion:  row.DesiredFirmwareVersion,
			Health:           modelsv2.HealthHealthyActive,
			HashrateHS:       &modelsv2.MetricValue{Value: 100},
		},
	}

	tests := []struct {
		name      string
		results   []telemetry.SampleResult
		errorPart string
	}{
		{
			name:      "zero results",
			errorPart: "no results",
		},
		{
			name:      "multiple results",
			results:   []telemetry.SampleResult{fresh, fresh},
			errorPart: "returned 2 results",
		},
		{
			name: "sampler error",
			results: []telemetry.SampleResult{func() telemetry.SampleResult {
				result := fresh
				result.Err = errors.New("telemetry unavailable")
				return result
			}()},
			errorPart: "telemetry unavailable",
		},
		{
			name: "wrong organization",
			results: []telemetry.SampleResult{func() telemetry.SampleResult {
				result := fresh
				result.OrgID++
				return result
			}()},
			errorPart: "organization mismatch",
		},
		{
			name: "wrong device",
			results: []telemetry.SampleResult{func() telemetry.SampleResult {
				result := fresh
				result.DeviceID = "other-miner"
				return result
			}()},
			errorPart: "device mismatch",
		},
		{
			name: "stale sample",
			results: []telemetry.SampleResult{func() telemetry.SampleResult {
				result := fresh
				result.FlightStart = completedAt
				return result
			}()},
			errorPart: "sample is stale",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeStore{rows: []channel.Enforcement{row}}
			r := newTestReconciler(
				store,
				&fakeDispatcher{},
				&fakeSampler{results: tt.results},
			)

			r.reconcile(t.Context())

			assert.Zero(t, store.confirmCalls)
			if assert.Len(t, store.observationCalls, 1) {
				call := store.observationCalls[0]
				assert.Contains(t, call.observation.Error, tt.errorPart)
				assert.Equal(
					t,
					time.Date(2026, 8, 18, 2, 0, 30, 0, time.UTC),
					call.nextReconcileAt,
				)
			}
			assert.Equal(t, int32(1), store.rows[0].AttemptCount)
		})
	}
}

func TestReconcileFreshNonConfirmingObservationDefersRetry(t *testing.T) {
	completedAt := time.Date(2026, 8, 18, 1, 0, 0, 0, time.UTC)
	row := testEnforcement(channel.EnforcementStateVerifying)
	row.AttemptCount = 1
	row.CommandCompletedAt = &completedAt
	store := &fakeStore{rows: []channel.Enforcement{row}}
	sampler := &fakeSampler{results: []telemetry.SampleResult{{
		DeviceID:    telemetrymodels.DeviceIdentifier(row.DeviceIdentifier),
		OrgID:       row.OrgID,
		FlightStart: completedAt.Add(time.Second),
		Metrics: modelsv2.DeviceMetrics{
			DeviceIdentifier: row.DeviceIdentifier,
			FirmwareVersion:  "still-old",
			Health:           modelsv2.HealthHealthyActive,
			HashrateHS:       &modelsv2.MetricValue{Value: 100},
		},
	}}}
	r := newTestReconciler(store, &fakeDispatcher{}, sampler)

	r.reconcile(t.Context())

	assert.Zero(t, store.confirmCalls)
	if assert.Len(t, store.observationCalls, 1) {
		call := store.observationCalls[0]
		assert.Empty(t, call.observation.Error)
		assert.Equal(t, "still-old", call.observation.FirmwareVersion)
		assert.Equal(
			t,
			time.Date(2026, 8, 18, 2, 0, 30, 0, time.UTC),
			call.nextReconcileAt,
		)
	}
	assert.Equal(t, int32(1), store.rows[0].AttemptCount)
}

func TestReconcileConfirmationRequiresFreshTargetAndHashing(t *testing.T) {
	base := testEnforcement(channel.EnforcementStateVerifying)
	completedAt := time.Date(2026, 8, 18, 1, 0, 0, 0, time.UTC)
	base.CommandCompletedAt = &completedAt

	tests := []struct {
		name        string
		flightStart time.Time
		version     string
		hashrate    *modelsv2.MetricValue
		wantConfirm bool
	}{
		{
			name:        "stale target observation",
			flightStart: completedAt.Add(-time.Second),
			version:     base.DesiredFirmwareVersion,
			hashrate:    &modelsv2.MetricValue{Value: 100},
		},
		{
			name:        "old firmware",
			flightStart: completedAt.Add(time.Second),
			version:     "old",
			hashrate:    &modelsv2.MetricValue{Value: 100},
		},
		{
			name:        "missing hashing",
			flightStart: completedAt.Add(time.Second),
			version:     base.DesiredFirmwareVersion,
		},
		{
			name:        "fresh target and hashing",
			flightStart: completedAt.Add(time.Second),
			version:     base.DesiredFirmwareVersion,
			hashrate:    &modelsv2.MetricValue{Value: 100},
			wantConfirm: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeStore{rows: []channel.Enforcement{base}}
			sampler := &fakeSampler{results: []telemetry.SampleResult{{
				DeviceID:    telemetrymodels.DeviceIdentifier(base.DeviceIdentifier),
				OrgID:       base.OrgID,
				FlightStart: tt.flightStart,
				Metrics: modelsv2.DeviceMetrics{
					DeviceIdentifier: base.DeviceIdentifier,
					FirmwareVersion:  tt.version,
					Health:           modelsv2.HealthHealthyActive,
					HashrateHS:       tt.hashrate,
				},
			}}}
			r := newTestReconciler(store, &fakeDispatcher{}, sampler)

			r.reconcile(t.Context())

			assert.Equal(t, completedAt, sampler.requests[0].SampledAfter)
			if tt.wantConfirm {
				assert.Equal(t, 1, store.confirmCalls)
			} else {
				assert.Zero(t, store.confirmCalls)
			}
		})
	}
}

func newTestReconciler(
	store Store,
	dispatcher CommandDispatcher,
	sampler TelemetrySampler,
) *Reconciler {
	r := New(Config{BatchSize: 10}, store, dispatcher, sampler)
	r.now = func() time.Time {
		return time.Date(2026, 8, 18, 2, 0, 0, 0, time.UTC)
	}
	r.newBatchID = func() string { return "batch-enforcement-1" }
	return r
}

func testEnforcement(state channel.EnforcementState) channel.Enforcement {
	return channel.Enforcement{
		ID:                     1,
		OrgID:                  7,
		DeviceID:               11,
		DeviceIdentifier:       "miner-1",
		DesiredFirmwareFileID:  "firmware-1",
		DesiredFirmwareVersion: "2.0.0",
		State:                  state,
		CommandBatchUUID:       "batch-existing",
		Revision:               3,
		CreatedByUserID:        42,
	}
}
