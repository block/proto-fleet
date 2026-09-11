package rollout_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/protovalidate"
	rolloutv1 "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type observedIdentityValidationCase struct {
	observedName         string
	filterName           string
	manufacturer         string
	model                string
	observedManufacturer string
	observedModel        string
	wantErr              bool
	// filterWantErr marks values that responses may carry but that request
	// filters must reject because stored text cannot represent them.
	filterWantErr bool
}

var observedIdentityValidationCases = []observedIdentityValidationCase{
	{
		observedName: "unknown manufacturer and model are valid",
		filterName:   "empty filters are valid",
	},
	{
		observedName: "unknown manufacturer is valid",
		filterName:   "model-only filter is valid",
		model:        "S21",
	},
	{
		observedName: "unknown model is valid",
		filterName:   "manufacturer-only filter is valid",
		manufacturer: "Bitmain",
	},
	{
		observedName: "canonical identities are valid",
		manufacturer: "Bitmain",
		model:        "S21",
	},
	{
		observedName: "internal printable spaces are valid",
		filterName:   "internal printable spaces are valid",
		manufacturer: "Bit Main",
		model:        "S 21 Pro",
	},
	{
		observedName: "maximum Unicode lengths are valid",
		filterName:   "maximum Unicode lengths are valid",
		manufacturer: strings.Repeat("界", 255),
		model:        strings.Repeat("型", 255),
	},
	{
		observedName:  "whitespace-only manufacturer is valid",
		filterName:    "whitespace-only manufacturer filter is valid",
		manufacturer:  " \t\n",
		observedModel: "S21",
	},
	{
		observedName:         "whitespace-only model is valid",
		filterName:           "whitespace-only model filter is valid",
		model:                " \t\n",
		observedManufacturer: "Bitmain",
	},
	{
		observedName:  "non-ASCII manufacturer is valid",
		filterName:    "non-ASCII manufacturer is valid",
		manufacturer:  "Bítmain",
		observedModel: "S21",
	},
	{
		observedName:         "non-ASCII model is valid",
		filterName:           "non-ASCII model is valid",
		model:                "S２1",
		observedManufacturer: "Bitmain",
	},
	{
		observedName:  "leading manufacturer space is valid",
		filterName:    "leading manufacturer space is valid",
		manufacturer:  " Bitmain",
		observedModel: "S21",
	},
	{
		observedName:  "trailing manufacturer space is valid",
		filterName:    "trailing manufacturer space is valid",
		manufacturer:  "Bitmain ",
		observedModel: "S21",
	},
	{
		observedName:         "leading model space is valid",
		filterName:           "leading model space is valid",
		model:                " S21",
		observedManufacturer: "Bitmain",
	},
	{
		observedName:         "trailing model space is valid",
		filterName:           "trailing model space is valid",
		model:                "S21 ",
		observedManufacturer: "Bitmain",
	},
	{
		observedName:  "internal control character is valid",
		filterName:    "internal control character is valid",
		manufacturer:  "Bit\x01main",
		observedModel: "S21",
	},
	{
		observedName:  "NUL character is valid",
		filterName:    "NUL character is rejected",
		manufacturer:  "Bit\x00main",
		observedModel: "S21",
		filterWantErr: true,
	},
	{
		observedName:  "oversized manufacturer is rejected",
		filterName:    "oversized manufacturer is rejected",
		manufacturer:  strings.Repeat("界", 256),
		observedModel: "S21",
		wantErr:       true,
	},
	{
		observedName:         "oversized model is rejected",
		filterName:           "oversized model is rejected",
		model:                strings.Repeat("型", 256),
		observedManufacturer: "Bitmain",
		wantErr:              true,
	},
}

func (test observedIdentityValidationCase) observedValues() (string, string) {
	manufacturer := test.manufacturer
	if test.observedManufacturer != "" {
		manufacturer = test.observedManufacturer
	}
	model := test.model
	if test.observedModel != "" {
		model = test.observedModel
	}
	return manufacturer, model
}

const testChecksum = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
const testPreviousChecksum = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

// activeRollout returns a minimal Rollout with a target and a consistent
// ACTIVE lifecycle so tests can exercise one rule at a time.
func activeRollout() *rolloutv1.Rollout {
	return &rolloutv1.Rollout{
		Manufacturer:         "Bitmain",
		Model:                "S21",
		FirmwareFileId:       "file-1",
		FirmwareChecksum:     testChecksum,
		FirmwareVersion:      "2.0",
		AssignmentGeneration: 1,
		Revision:             1,
		Status:               rolloutv1.RolloutStatus_ROLLOUT_STATUS_ACTIVE,
		State:                rolloutv1.RolloutState_ROLLOUT_STATE_IN_PROGRESS,
		Stage:                rolloutv1.RolloutStage_ROLLOUT_STAGE_REST,
	}
}

// finishedRollout returns a minimal Rollout in the given terminal status with
// the state the server derives for it and the cancel reason and finished_at
// the contract pairs with it.
func finishedRollout(status rolloutv1.RolloutStatus) *rolloutv1.Rollout {
	rollout := activeRollout()
	rollout.Status = status
	rollout.FinishedAt = timestamppb.Now()
	switch status {
	case rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED:
		rollout.State = rolloutv1.RolloutState_ROLLOUT_STATE_COMPLETED
	case rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED_WITH_FAILURES:
		rollout.State = rolloutv1.RolloutState_ROLLOUT_STATE_COMPLETED_WITH_FAILURES
	case rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELED:
		rollout.State = rolloutv1.RolloutState_ROLLOUT_STATE_CANCELED
		rollout.CancelReason = rolloutv1.RolloutCancelReason_ROLLOUT_CANCEL_REASON_CANCELED_REMAINING
	case rolloutv1.RolloutStatus_ROLLOUT_STATUS_ACTIVE, rolloutv1.RolloutStatus_ROLLOUT_STATUS_UNSPECIFIED:
		panic("finishedRollout requires a terminal status")
	}
	return rollout
}

// batchedRollout returns an ACTIVE rollout in its first of batchCount batches.
func batchedRollout(batchCount int32) *rolloutv1.Rollout {
	rollout := activeRollout()
	rollout.Behavior = &rolloutv1.RolloutBehavior{
		Method:    rolloutv1.RolloutMethod_ROLLOUT_METHOD_BATCHED,
		BatchSize: 1,
	}
	rollout.BatchCount = batchCount
	rollout.Stage = rolloutv1.RolloutStage_ROLLOUT_STAGE_BATCH
	return rollout
}

func queuedDevice() *rolloutv1.RolloutDevice {
	return &rolloutv1.RolloutDevice{Phase: rolloutv1.RolloutDevicePhase_ROLLOUT_DEVICE_PHASE_QUEUED}
}

func delegatedBehavior() *rolloutv1.RolloutBehavior {
	return &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_DELEGATED}
}

func assignmentOf(manufacturer, model, fileID string) *rolloutv1.FirmwareAssignment {
	return &rolloutv1.FirmwareAssignment{Manufacturer: manufacturer, Model: model, FirmwareFileId: fileID}
}

func applyRequest(assignments ...*rolloutv1.FirmwareAssignment) *rolloutv1.ApplyReleaseChannelFirmwareRequest {
	return &rolloutv1.ApplyReleaseChannelFirmwareRequest{ChannelId: 1, Assignments: assignments}
}

// conflict returns a valid relation for a miner that also matches another
// channel.
func conflict() *rolloutv1.ReleaseChannelMembershipConflict {
	return &rolloutv1.ReleaseChannelMembershipConflict{
		DeviceId:            1,
		DeviceIdentifier:    "miner-1",
		Manufacturer:        "Bitmain",
		Model:               "S21",
		ChannelId:           1,
		ChannelName:         "stable",
		SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
		Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
	}
}

// rolloutEvent returns a valid ADVANCED event with full audit metadata.
func rolloutEvent() *rolloutv1.RolloutEvent {
	return &rolloutv1.RolloutEvent{
		Id: 1, RolloutId: 1, ChannelId: 1,
		Type:              rolloutv1.RolloutEventType_ROLLOUT_EVENT_TYPE_ADVANCED,
		OccurredAt:        timestamppb.Now(),
		Actor:             &rolloutv1.RolloutActor{Type: rolloutv1.RolloutActorType_ROLLOUT_ACTOR_TYPE_API_KEY, Id: 4, Name: "regression-bot"},
		RolloutRevision:   3,
		Note:              "batch 2 passed",
		DeviceIdentifiers: []string{"miner-1"},
	}
}

func requireProtoValidation(t *testing.T, message proto.Message, wantErr bool) {
	t.Helper()

	err := protovalidate.Validate(message)
	if wantErr {
		require.Error(t, err)
		return
	}
	require.NoError(t, err)
}

func TestRolloutBehaviorValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		behavior *rolloutv1.RolloutBehavior
		wantErr  bool
	}{
		{
			name:     "unspecified defaults are valid",
			behavior: &rolloutv1.RolloutBehavior{},
		},
		{
			name: "batched requires a positive batch size",
			behavior: &rolloutv1.RolloutBehavior{
				Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_BATCHED,
			},
			wantErr: true,
		},
		{
			name: "batched accepts one miner",
			behavior: &rolloutv1.RolloutBehavior{
				Method:    rolloutv1.RolloutMethod_ROLLOUT_METHOD_BATCHED,
				BatchSize: 1,
			},
		},
		{
			name:     "a batch size with an unspecified method is rejected rather than ignored",
			behavior: &rolloutv1.RolloutBehavior{BatchSize: 50},
			wantErr:  true,
		},
		{
			name:     "a batch size with all-at-once is rejected",
			behavior: &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_ALL_AT_ONCE, BatchSize: 50},
			wantErr:  true,
		},
		{
			name:     "a pilot size with the batched method is rejected",
			behavior: &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_BATCHED, BatchSize: 2, PilotSize: 1},
			wantErr:  true,
		},
		{
			name:     "a batch size with the pilot method is rejected",
			behavior: &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_PILOT_THEN_CONTINUE, PilotSize: 1, BatchSize: 2},
			wantErr:  true,
		},
		{
			name: "pilot requires a positive pilot size",
			behavior: &rolloutv1.RolloutBehavior{
				Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_PILOT_THEN_CONTINUE,
			},
			wantErr: true,
		},
		{
			name: "pilot accepts one miner",
			behavior: &rolloutv1.RolloutBehavior{
				Method:    rolloutv1.RolloutMethod_ROLLOUT_METHOD_PILOT_THEN_CONTINUE,
				PilotSize: 1,
			},
		},
		{
			name: "unknown method is rejected",
			behavior: &rolloutv1.RolloutBehavior{
				Method: rolloutv1.RolloutMethod(99),
			},
			wantErr: true,
		},
		{
			name: "unknown order is rejected",
			behavior: &rolloutv1.RolloutBehavior{
				Order: rolloutv1.RolloutOrder(99),
			},
			wantErr: true,
		},
		{name: "delegated with no engine pacing is valid", behavior: delegatedBehavior()},
		{
			name: "delegated may set a controller timeout and an offline budget",
			behavior: &rolloutv1.RolloutBehavior{
				Method:                   rolloutv1.RolloutMethod_ROLLOUT_METHOD_DELEGATED,
				Order:                    rolloutv1.RolloutOrder_ROLLOUT_ORDER_RANDOM,
				ControllerTimeoutSeconds: 900,
				MaxConcurrentOffline:     5,
			},
		},
		{
			name:     "delegated rejects a batch size",
			behavior: &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_DELEGATED, BatchSize: 2},
			wantErr:  true,
		},
		{
			name:     "delegated rejects a pilot size",
			behavior: &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_DELEGATED, PilotSize: 1},
			wantErr:  true,
		},
		{
			name:     "delegated rejects per-batch review",
			behavior: &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_DELEGATED, ReviewAfterEachBatch: true},
			wantErr:  true,
		},
		{
			name: "delegated rejects auto-continue",
			behavior: &rolloutv1.RolloutBehavior{
				Method:                         rolloutv1.RolloutMethod_ROLLOUT_METHOD_DELEGATED,
				AutoContinueOnHealthyTelemetry: true,
			},
			wantErr: true,
		},
		{
			name: "delegated rejects thresholds",
			behavior: &rolloutv1.RolloutBehavior{
				Method:     rolloutv1.RolloutMethod_ROLLOUT_METHOD_DELEGATED,
				Thresholds: &rolloutv1.RolloutAutomationThresholds{},
			},
			wantErr: true,
		},
		{
			name:     "review on all-at-once is rejected rather than ignored",
			behavior: &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_ALL_AT_ONCE, ReviewAfterEachBatch: true},
			wantErr:  true,
		},
		{
			name:     "pilot may state its implied review gate",
			behavior: &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_PILOT_THEN_CONTINUE, PilotSize: 1, ReviewAfterEachBatch: true},
		},
		{
			name:     "wait between batches needs unreviewed batches",
			behavior: &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_BATCHED, BatchSize: 2, ReviewAfterEachBatch: true, WaitBetweenBatchesSeconds: 60},
			wantErr:  true,
		},
		{
			name:     "wait between batches on all-at-once is rejected",
			behavior: &rolloutv1.RolloutBehavior{WaitBetweenBatchesSeconds: 60},
			wantErr:  true,
		},
		{
			name:     "auto-continue without a gate is rejected",
			behavior: &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_BATCHED, BatchSize: 2, AutoContinueOnHealthyTelemetry: true},
			wantErr:  true,
		},
		{
			name: "auto-continue with thresholds at a batch review gate is valid",
			behavior: &rolloutv1.RolloutBehavior{
				Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_BATCHED, BatchSize: 2, ReviewAfterEachBatch: true,
				AutoContinueOnHealthyTelemetry: true, StabilizationSeconds: 600,
				Thresholds: &rolloutv1.RolloutAutomationThresholds{MaxNewErrors: proto.Int32(0)},
			},
		},
		{
			name:     "stabilization without auto-continue is rejected",
			behavior: &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_PILOT_THEN_CONTINUE, PilotSize: 1, StabilizationSeconds: 600},
			wantErr:  true,
		},
		{
			name:     "thresholds without auto-continue are rejected",
			behavior: &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_PILOT_THEN_CONTINUE, PilotSize: 1, Thresholds: &rolloutv1.RolloutAutomationThresholds{}},
			wantErr:  true,
		},
		{
			name:     "controller timeout requires the delegated method",
			behavior: &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_ALL_AT_ONCE, ControllerTimeoutSeconds: 60},
			wantErr:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.behavior, test.wantErr)
		})
	}
}

func TestRolloutFirmwareVersionsValidation(t *testing.T) {
	t.Parallel()

	fields := []struct {
		name string
		set  func(*rolloutv1.Rollout, string)
	}{
		{
			name: "target",
			set: func(rollout *rolloutv1.Rollout, version string) {
				rollout.FirmwareVersion = version
			},
		},
		{
			name: "lineage",
			set: func(rollout *rolloutv1.Rollout, version string) {
				rollout.PreviousFirmwareChecksum = testPreviousChecksum
				rollout.PreviousFirmwareVersion = version
			},
		},
	}

	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			t.Parallel()

			newRollout := func(version string) *rolloutv1.Rollout {
				rollout := activeRollout()
				field.set(rollout, version)
				return rollout
			}

			requireProtoValidation(t, newRollout(strings.Repeat("界", 255)), false)
			requireProtoValidation(t, newRollout(strings.Repeat("界", 256)), true)
			requireProtoValidation(t, newRollout("v1\x00custom"), true)
		})
	}
}

func TestRolloutLineageValidation(t *testing.T) {
	t.Parallel()

	newRollout := func(checksum, version, previousChecksum, previousVersion string) *rolloutv1.Rollout {
		rollout := activeRollout()
		rollout.FirmwareChecksum = checksum
		rollout.FirmwareVersion = version
		rollout.PreviousFirmwareChecksum = previousChecksum
		rollout.PreviousFirmwareVersion = previousVersion
		return rollout
	}
	withoutGeneration := newRollout(testChecksum, "2.0", "", "")
	withoutGeneration.AssignmentGeneration = 0
	withoutRevision := newRollout(testChecksum, "2.0", "", "")
	withoutRevision.Revision = 0
	// The artifact may be gone from the store while the rollout still names it.
	withoutFile := newRollout(testChecksum, "2.0", "", "")
	withoutFile.FirmwareFileId = ""
	tests := []struct {
		name    string
		rollout *rolloutv1.Rollout
		wantErr bool
	}{
		{name: "first assignment has an empty lineage", rollout: newRollout(testChecksum, "2.0", "", "")},
		{name: "later assignment records the replaced one", rollout: newRollout(testChecksum, "2.0", testPreviousChecksum, "1.0")},
		{name: "rollout whose artifact is not currently uploaded is valid", rollout: withoutFile},
		{name: "rollout without a target checksum is rejected", rollout: newRollout("", "2.0", "", ""), wantErr: true},
		{name: "malformed target checksum is rejected", rollout: newRollout("file-1", "2.0", "", ""), wantErr: true},
		{name: "uppercase target checksum is rejected", rollout: newRollout(strings.ToUpper(testChecksum), "2.0", "", ""), wantErr: true},
		{name: "rollout without a target version is rejected", rollout: newRollout(testChecksum, "", "", ""), wantErr: true},
		{name: "lineage checksum without version is rejected", rollout: newRollout(testChecksum, "2.0", testPreviousChecksum, ""), wantErr: true},
		{name: "lineage version without checksum is rejected", rollout: newRollout(testChecksum, "2.0", "", "1.0"), wantErr: true},
		{name: "malformed lineage checksum is rejected", rollout: newRollout(testChecksum, "2.0", "file-0", "1.0"), wantErr: true},
		{name: "lineage equal to the target is rejected", rollout: newRollout(testChecksum, "2.0", testChecksum, "2.0"), wantErr: true},
		{name: "rollout without assignment generation is rejected", rollout: withoutGeneration, wantErr: true},
		{name: "rollout without a revision is rejected", rollout: withoutRevision, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.rollout, test.wantErr)
		})
	}
}

func TestRolloutLifecycleValidation(t *testing.T) {
	t.Parallel()

	mutate := func(edit func(*rolloutv1.Rollout)) *rolloutv1.Rollout {
		rollout := activeRollout()
		edit(rollout)
		return rollout
	}
	tests := []struct {
		name    string
		rollout *rolloutv1.Rollout
		wantErr bool
	}{
		{name: "active in progress is valid", rollout: activeRollout()},
		{name: "completed is valid", rollout: finishedRollout(rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED)},
		{name: "completed with failures is valid", rollout: finishedRollout(rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED_WITH_FAILURES)},
		{name: "canceled with a reason is valid", rollout: finishedRollout(rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELED)},
		{
			name: "paused with paused_at is valid",
			rollout: mutate(func(r *rolloutv1.Rollout) {
				r.State = rolloutv1.RolloutState_ROLLOUT_STATE_PAUSED
				r.PausedAt = timestamppb.Now()
			}),
		},
		{name: "unspecified status is rejected", rollout: &rolloutv1.Rollout{Manufacturer: "Bitmain", Model: "S21"}, wantErr: true},
		{name: "unknown status is rejected", rollout: mutate(func(r *rolloutv1.Rollout) { r.Status = rolloutv1.RolloutStatus(99) }), wantErr: true},
		{name: "unspecified state is rejected", rollout: mutate(func(r *rolloutv1.Rollout) { r.State = rolloutv1.RolloutState_ROLLOUT_STATE_UNSPECIFIED }), wantErr: true},
		{name: "unspecified stage is rejected", rollout: mutate(func(r *rolloutv1.Rollout) { r.Stage = rolloutv1.RolloutStage_ROLLOUT_STAGE_UNSPECIFIED }), wantErr: true},
		{name: "unknown cancel reason is rejected", rollout: mutate(func(r *rolloutv1.Rollout) { r.CancelReason = rolloutv1.RolloutCancelReason(99) }), wantErr: true},
		{name: "active rollout with a cancel reason is rejected", rollout: mutate(func(r *rolloutv1.Rollout) {
			r.CancelReason = rolloutv1.RolloutCancelReason_ROLLOUT_CANCEL_REASON_CLEARED
		}), wantErr: true},
		{
			name: "canceled rollout without a reason is rejected",
			rollout: func() *rolloutv1.Rollout {
				r := finishedRollout(rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELED)
				r.CancelReason = rolloutv1.RolloutCancelReason_ROLLOUT_CANCEL_REASON_UNSPECIFIED
				return r
			}(),
			wantErr: true,
		},
		{name: "active rollout with finished_at is rejected", rollout: mutate(func(r *rolloutv1.Rollout) { r.FinishedAt = timestamppb.Now() }), wantErr: true},
		{
			name: "finished rollout without finished_at is rejected",
			rollout: func() *rolloutv1.Rollout {
				r := finishedRollout(rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED)
				r.FinishedAt = nil
				return r
			}(),
			wantErr: true,
		},
		{name: "paused state without paused_at is rejected", rollout: mutate(func(r *rolloutv1.Rollout) { r.State = rolloutv1.RolloutState_ROLLOUT_STATE_PAUSED }), wantErr: true},
		{name: "paused_at while in progress is rejected", rollout: mutate(func(r *rolloutv1.Rollout) { r.PausedAt = timestamppb.Now() }), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.rollout, test.wantErr)
		})
	}
}

func TestRolloutDeviceCountsValidation(t *testing.T) {
	t.Parallel()

	withCounts := func(total int32, counts, batch *rolloutv1.RolloutDeviceCounts, evidence *rolloutv1.RolloutEvidence) *rolloutv1.Rollout {
		rollout := activeRollout()
		if batch != nil {
			rollout = batchedRollout(1)
		}
		rollout.DeviceCount = total
		rollout.DeviceCounts = counts
		rollout.CurrentBatchCounts = batch
		rollout.Evidence = evidence
		return rollout
	}
	allPhases := &rolloutv1.RolloutDeviceCounts{Queued: 1, InProgress: 1, Retrying: 1, Done: 1, Failed: 1, Excluded: 1}
	tests := []struct {
		name    string
		rollout *rolloutv1.Rollout
		wantErr bool
	}{
		{name: "no targets and no counts is valid", rollout: withCounts(0, nil, nil, nil)},
		{
			name:    "phases summing to device_count with matching batch evidence are valid",
			rollout: withCounts(6, allPhases, &rolloutv1.RolloutDeviceCounts{Done: 1, Failed: 1}, &rolloutv1.RolloutEvidence{DevicesTotal: 2, Verified: 1, Online: 1, Failed: 1}),
		},
		{
			name:    "all-at-once evidence covering every target is valid",
			rollout: withCounts(3, &rolloutv1.RolloutDeviceCounts{Done: 2, Failed: 1}, nil, &rolloutv1.RolloutEvidence{DevicesTotal: 3, Verified: 2, Online: 2, Failed: 1}),
		},
		{name: "targets without phase counts are rejected", rollout: withCounts(3, nil, nil, nil), wantErr: true},
		{name: "phases exceeding device_count are rejected", rollout: withCounts(2, &rolloutv1.RolloutDeviceCounts{Done: 2, Failed: 1}, nil, nil), wantErr: true},
		{name: "phases below device_count are rejected", rollout: withCounts(3, &rolloutv1.RolloutDeviceCounts{Done: 2}, nil, nil), wantErr: true},
		{name: "batch phase exceeding the rollout phase is rejected", rollout: withCounts(2, &rolloutv1.RolloutDeviceCounts{Done: 2}, &rolloutv1.RolloutDeviceCounts{Done: 3}, nil), wantErr: true},
		{name: "batch phase absent from the rollout phases is rejected", rollout: withCounts(1, &rolloutv1.RolloutDeviceCounts{Done: 1}, &rolloutv1.RolloutDeviceCounts{Queued: 1}, nil), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.rollout, test.wantErr)
		})
	}
}

func TestRolloutBatchConsistencyValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rollout func() *rolloutv1.Rollout
		wantErr bool
	}{
		{name: "all-at-once without batches in the rest stage is valid", rollout: activeRollout},
		{name: "batched rollout in its first batch is valid", rollout: func() *rolloutv1.Rollout { return batchedRollout(2) }},
		{
			name: "batched rollout on its last batch is valid",
			rollout: func() *rolloutv1.Rollout {
				r := batchedRollout(2)
				r.CurrentBatch = 1
				return r
			},
		},
		{
			name: "batched rollout resting after its batches is valid",
			rollout: func() *rolloutv1.Rollout {
				r := batchedRollout(2)
				r.CurrentBatch = 1
				r.Stage = rolloutv1.RolloutStage_ROLLOUT_STAGE_REST
				return r
			},
		},
		{
			name: "current batch at batch count is rejected",
			rollout: func() *rolloutv1.Rollout {
				r := batchedRollout(2)
				r.CurrentBatch = 2
				return r
			},
			wantErr: true,
		},
		{
			name: "current batch without batches is rejected",
			rollout: func() *rolloutv1.Rollout {
				r := activeRollout()
				r.CurrentBatch = 1
				return r
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.rollout(), test.wantErr)
		})
	}
}

func TestRolloutEvidencePresenceValidation(t *testing.T) {
	t.Parallel()

	reviewed := batchedRollout(1)
	reviewed.Stage = rolloutv1.RolloutStage_ROLLOUT_STAGE_AWAITING_REVIEW
	reviewed.State = rolloutv1.RolloutState_ROLLOUT_STATE_PAUSED_AT_BATCH_REVIEW
	reviewed.DeviceCount = 2
	reviewed.DeviceCounts = &rolloutv1.RolloutDeviceCounts{Queued: 1, Done: 1}
	reviewed.CurrentBatchCounts = &rolloutv1.RolloutDeviceCounts{Done: 1}
	reviewed.Evidence = &rolloutv1.RolloutEvidence{DevicesTotal: 1, Verified: 1, Online: 1}
	requireProtoValidation(t, reviewed, false)

	finished := finishedRollout(rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED)
	finished.Evidence = &rolloutv1.RolloutEvidence{}
	requireProtoValidation(t, finished, true)
}

func TestRolloutAutomationThresholdsCoverageValidation(t *testing.T) {
	t.Parallel()

	withLimit := func(coverage *float64) *rolloutv1.RolloutAutomationThresholds {
		return &rolloutv1.RolloutAutomationThresholds{MaxHashrateDropPercent: proto.Float64(10), MinSampleCoveragePercent: coverage}
	}
	tests := []struct {
		name       string
		thresholds *rolloutv1.RolloutAutomationThresholds
		wantErr    bool
	}{
		{name: "unset coverage defaults to full", thresholds: withLimit(nil)},
		{name: "full coverage is valid", thresholds: withLimit(proto.Float64(100))},
		{name: "fractional coverage is valid", thresholds: withLimit(proto.Float64(0.5))},
		{name: "zero coverage is rejected", thresholds: withLimit(proto.Float64(0)), wantErr: true},
		{name: "coverage above 100 is rejected", thresholds: withLimit(proto.Float64(100.5)), wantErr: true},
		{name: "an error limit alone is valid", thresholds: &rolloutv1.RolloutAutomationThresholds{MaxNewErrors: proto.Int32(0)}},
		// Error counts have no coverage rule, so coverage without a sampled
		// metric limit would never be evaluated.
		{name: "coverage alone is rejected", thresholds: &rolloutv1.RolloutAutomationThresholds{MinSampleCoveragePercent: proto.Float64(80)}, wantErr: true},
		{
			name:       "coverage with only an error limit is rejected",
			thresholds: &rolloutv1.RolloutAutomationThresholds{MinSampleCoveragePercent: proto.Float64(80), MaxNewErrors: proto.Int32(0)},
			wantErr:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.thresholds, test.wantErr)
		})
	}
}

func TestRolloutAutomationThresholdsRejectNonFiniteLimits(t *testing.T) {
	t.Parallel()

	fields := []struct {
		name string
		set  func(*rolloutv1.RolloutAutomationThresholds, float64)
	}{
		{"hashrate drop", func(th *rolloutv1.RolloutAutomationThresholds, v float64) {
			th.MaxHashrateDropPercent = proto.Float64(v)
		}},
		{"efficiency increase", func(th *rolloutv1.RolloutAutomationThresholds, v float64) {
			th.MaxEfficiencyIncreasePercent = proto.Float64(v)
		}},
		{"temperature increase", func(th *rolloutv1.RolloutAutomationThresholds, v float64) {
			th.MaxTemperatureIncreaseCelsius = proto.Float64(v)
		}},
		{"sample coverage", func(th *rolloutv1.RolloutAutomationThresholds, v float64) {
			th.MinSampleCoveragePercent = proto.Float64(v)
		}},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			t.Parallel()

			// A temperature limit keeps the coverage case meaningful without
			// touching the field under test.
			for _, value := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
				thresholds := &rolloutv1.RolloutAutomationThresholds{MaxTemperatureIncreaseCelsius: proto.Float64(5)}
				field.set(thresholds, value)
				requireProtoValidation(t, thresholds, true)
			}
			thresholds := &rolloutv1.RolloutAutomationThresholds{MaxTemperatureIncreaseCelsius: proto.Float64(5)}
			field.set(thresholds, 5)
			requireProtoValidation(t, thresholds, false)
		})
	}
}

// sampledAggregate is a 100 -> 90 aggregate, a -10 percent or -10 unit change.
func sampledAggregate(devices int32) *rolloutv1.AggregateMetricComparison {
	return aggregateOf(100, 90, devices)
}

func aggregateOf(baseline, current float64, devices int32) *rolloutv1.AggregateMetricComparison {
	return &rolloutv1.AggregateMetricComparison{
		Baseline:       proto.Float64(baseline),
		Current:        proto.Float64(current),
		SampledDevices: devices,
	}
}

func TestAggregateMetricComparisonValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		aggregate *rolloutv1.AggregateMetricComparison
		wantErr   bool
	}{
		{
			name:      "absent aggregate samples no miners",
			aggregate: &rolloutv1.AggregateMetricComparison{},
		},
		{
			name:      "sampled aggregate carries both halves",
			aggregate: sampledAggregate(3),
		},
		{
			name: "sampled aggregate missing current is rejected",
			aggregate: &rolloutv1.AggregateMetricComparison{
				Baseline:       proto.Float64(100),
				SampledDevices: 1,
			},
			wantErr: true,
		},
		{
			name: "sampled aggregate missing baseline is rejected",
			aggregate: &rolloutv1.AggregateMetricComparison{
				Current:        proto.Float64(90),
				SampledDevices: 1,
			},
			wantErr: true,
		},
		{
			name:      "both halves without samples are rejected",
			aggregate: sampledAggregate(0),
			wantErr:   true,
		},
		{
			name:      "one half without samples is rejected",
			aggregate: &rolloutv1.AggregateMetricComparison{Baseline: proto.Float64(100)},
			wantErr:   true,
		},
		{
			name:      "negative sample count is rejected",
			aggregate: &rolloutv1.AggregateMetricComparison{SampledDevices: -1},
			wantErr:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.aggregate, test.wantErr)
		})
	}
}

func TestRolloutEvidenceValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		evidence *rolloutv1.RolloutEvidence
		wantErr  bool
	}{
		{
			name:     "empty evidence is valid",
			evidence: &rolloutv1.RolloutEvidence{},
		},
		{
			name: "consistent evidence is valid",
			evidence: &rolloutv1.RolloutEvidence{
				DevicesTotal:             4,
				Verified:                 3,
				Failed:                   1,
				Online:                   4,
				Hashing:                  3,
				BaselineHashing:          4,
				HashRateHs:               sampledAggregate(3),
				TempC:                    sampledAggregate(2),
				HashrateChangePercent:    proto.Float64(-10),
				TemperatureChangeCelsius: proto.Float64(-10),
				HoldReason:               strings.Repeat("h", 1024),
			},
		},
		{
			name: "zero baseline leaves the percent change unset",
			evidence: &rolloutv1.RolloutEvidence{
				DevicesTotal: 1,
				Verified:     1,
				Online:       1,
				HashRateHs: &rolloutv1.AggregateMetricComparison{
					Baseline:       proto.Float64(0),
					Current:        proto.Float64(50),
					SampledDevices: 1,
				},
			},
		},
		{
			// The server derives the change fields from the aggregates; the
			// contract does not re-check that arithmetic.
			name:     "sampled aggregate without its change is structurally valid",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Verified: 1, Online: 1, HashRateHs: sampledAggregate(1), TempC: sampledAggregate(1)},
		},
		{
			name:     "verified plus failed above total is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 4, Verified: 3, Online: 4, Failed: 1, Excluded: 1},
			wantErr:  true,
		},
		{
			name:     "persisted verification can exceed current online count",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 2, Verified: 2, Online: 1},
		},
		{
			name:     "unverified baseline hashers need not be hashing",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 2, Verified: 1, Online: 2, Hashing: 0, BaselineHashing: 1},
		},
		{
			name: "sub-zero temperature aggregate is valid",
			evidence: &rolloutv1.RolloutEvidence{
				DevicesTotal: 1, Verified: 1, Online: 1,
				TempC:                    aggregateOf(-5, 3, 1),
				TemperatureChangeCelsius: proto.Float64(8),
			},
		},
		{
			name:     "hashing above online is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 2, Hashing: 2, Online: 1},
			wantErr:  true,
		},
		{
			name:     "online above total is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Online: 2},
			wantErr:  true,
		},
		{
			name:     "hashing above total is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Hashing: 2},
			wantErr:  true,
		},
		{
			name:     "baseline hashing above total is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, BaselineHashing: 2},
			wantErr:  true,
		},
		{
			name: "changes without their aggregates are structurally valid",
			evidence: &rolloutv1.RolloutEvidence{
				DevicesTotal: 1, Verified: 1, Online: 1,
				HashrateChangePercent:    proto.Float64(0),
				EfficiencyChangePercent:  proto.Float64(0),
				TemperatureChangeCelsius: proto.Float64(0),
			},
		},
		{
			name:     "negative count is rejected",
			evidence: &rolloutv1.RolloutEvidence{NewErrors: -1},
			wantErr:  true,
		},
		{
			name:     "oversized hold reason is rejected",
			evidence: &rolloutv1.RolloutEvidence{HoldReason: strings.Repeat("h", 1025)},
			wantErr:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.evidence, test.wantErr)
		})
	}
}

// Every string carries a max_len, every list a max_items, every integer a
// lower bound, every enum defined_only, and every double finite, so no payload
// in the contract can grow without an explicit limit, expose an impossible
// negative id or count, carry an enum value clients cannot interpret, or smuggle
// an infinite or NaN threshold or measurement.
func TestEveryContractFieldIsConstrained(t *testing.T) {
	t.Parallel()

	hasLowerBound := func(rules *validate.FieldRules) bool {
		return rules.GetInt32().GetGreaterThan() != nil || rules.GetInt64().GetGreaterThan() != nil
	}

	var visit func(messages protoreflect.MessageDescriptors)
	visit = func(messages protoreflect.MessageDescriptors) {
		for i := range messages.Len() {
			message := messages.Get(i)
			visit(message.Messages())
			fields := message.Fields()
			for j := range fields.Len() {
				field := fields.Get(j)
				rules, _ := proto.GetExtension(field.Options(), validate.E_Field).(*validate.FieldRules)
				elementRules := rules
				if field.IsList() {
					if rules.GetRepeated().GetMaxItems() == 0 {
						t.Errorf("%s must set max_items", field.FullName())
					}
					elementRules = rules.GetRepeated().GetItems()
				}
				kind := field.Kind()
				if kind == protoreflect.StringKind && elementRules.GetString_().GetMaxLen() == 0 {
					t.Errorf("%s must set max_len", field.FullName())
				}
				if (kind == protoreflect.Int32Kind || kind == protoreflect.Int64Kind) && !hasLowerBound(elementRules) {
					t.Errorf("%s must set gte or gt", field.FullName())
				}
				if kind == protoreflect.EnumKind && !elementRules.GetEnum().GetDefinedOnly() {
					t.Errorf("%s must set defined_only", field.FullName())
				}
				if kind == protoreflect.DoubleKind && !elementRules.GetDouble().GetFinite() {
					t.Errorf("%s must set finite", field.FullName())
				}
			}
		}
	}
	visit(rolloutv1.File_rollout_v1_rollout_proto.Messages())
}

func TestApplyReleaseChannelFirmwareRequestValidation(t *testing.T) {
	t.Parallel()

	oversizedAssignments := make([]*rolloutv1.FirmwareAssignment, 101)
	for index := range oversizedAssignments {
		oversizedAssignments[index] = assignmentOf("Bitmain", fmt.Sprintf("model-%d", index), "firmware")
	}
	tests := []struct {
		name    string
		request *rolloutv1.ApplyReleaseChannelFirmwareRequest
		wantErr bool
	}{
		{name: "duplicate manufacturer and model pair is rejected", request: applyRequest(assignmentOf("Bitmain", "S21", "firmware-a"), assignmentOf("Bitmain", "S21", "firmware-b")), wantErr: true},
		{name: "case variants of one manufacturer and model pair are rejected", request: applyRequest(assignmentOf("Bitmain", "S21", "firmware-a"), assignmentOf("bitMAIN", "s21", "firmware-b")), wantErr: true},
		{name: "surrounding whitespace in an assignment target is rejected", request: applyRequest(assignmentOf("Bitmain", "S21", "firmware-a"), assignmentOf(" Bitmain ", "\tS21\n", "firmware-b")), wantErr: true},
		{name: "distinct manufacturer and model pairs are accepted", request: applyRequest(assignmentOf("Bitmain", "S21", "firmware-a"), assignmentOf("bitMAIN", "S19", "firmware-b"))},
		{name: "same model under different manufacturers is accepted", request: applyRequest(assignmentOf("Bitmain", "S21", "firmware-a"), assignmentOf("MicroBT", "S21", "firmware-b"))},
		{name: "different models under one manufacturer are accepted", request: applyRequest(assignmentOf("Bitmain", "S21", "firmware-a"), assignmentOf("Bitmain", "S19", "firmware-b"))},
		{name: "length-prefixed composite keys do not collide", request: applyRequest(assignmentOf("A", "BC", "firmware-a"), assignmentOf("AB", "C", "firmware-b"))},
		{name: "assignment count is bounded", request: applyRequest(oversizedAssignments...), wantErr: true},
		{name: "maximum firmware file id is accepted", request: applyRequest(assignmentOf("Bitmain", "S21", strings.Repeat("f", 255)))},
		{name: "oversized firmware file id is rejected", request: applyRequest(assignmentOf("Bitmain", "S21", strings.Repeat("f", 256))), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.request, test.wantErr)
		})
	}
}

func TestReleaseChannelModelGroupReportedVersionsValidation(t *testing.T) {
	t.Parallel()

	reporting := func(minerCount, versionCount int32, versions ...string) *rolloutv1.ReleaseChannelModelGroup {
		return &rolloutv1.ReleaseChannelModelGroup{
			Manufacturer: "Bitmain", Model: "S21", MinerCount: minerCount,
			ReportedVersions: versions, ReportedVersionCount: versionCount,
		}
	}
	tenVersions := []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0", "1.5.0", "1.6.0", "1.7.0", "1.8.0", "1.9.0"}
	tests := []struct {
		name       string
		modelGroup *rolloutv1.ReleaseChannelModelGroup
		wantErr    bool
	}{
		{name: "bounded truncated list is valid", modelGroup: reporting(20, 12, tenVersions...)},
		{name: "complete list below the cap is valid", modelGroup: reporting(20, 2, "1.0.0", "2.0.0")},
		{name: "oversized list is rejected", modelGroup: reporting(20, 11, append(append([]string{}, tenVersions...), "2.0.0")...), wantErr: true},
		{name: "count below returned list length is rejected", modelGroup: reporting(20, 1, "1.0.0", "2.0.0"), wantErr: true},
		{name: "partial list below the cap is rejected", modelGroup: reporting(20, 5, "1.0.0", "2.0.0"), wantErr: true},
		{name: "more versions than miners is rejected", modelGroup: reporting(1, 2, "1.0.0", "2.0.0"), wantErr: true},
		{name: "duplicate versions are rejected", modelGroup: reporting(20, 2, "1.0.0", "1.0.0"), wantErr: true},
		{name: "oversized version is rejected", modelGroup: reporting(20, 1, strings.Repeat("v", 256)), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.modelGroup, test.wantErr)
		})
	}
}

func TestReleaseChannelModelGroupAssignmentValidation(t *testing.T) {
	t.Parallel()

	type edit = func(*rolloutv1.ReleaseChannelModelGroup)
	unassigned := func(edits ...edit) *rolloutv1.ReleaseChannelModelGroup {
		group := &rolloutv1.ReleaseChannelModelGroup{Manufacturer: "Bitmain", Model: "S21", MinerCount: 3}
		for _, apply := range edits {
			apply(group)
		}
		return group
	}
	assigned := func(edits ...edit) *rolloutv1.ReleaseChannelModelGroup {
		group := unassigned(func(g *rolloutv1.ReleaseChannelModelGroup) {
			g.FirmwareFileId = "file-1"
			g.FirmwareChecksum = testChecksum
			g.FirmwareAvailable = true
			g.FirmwareTargetManufacturer = "Bitmain"
			g.FirmwareTargetModel = "S21"
			g.FirmwareVersion = "2.0"
			g.AssignmentGeneration = 1
		})
		for _, apply := range edits {
			apply(group)
		}
		return group
	}
	noncanonicalIdentity := func(g *rolloutv1.ReleaseChannelModelGroup) {
		g.Manufacturer = "Bít main "
		g.Model = " S２1\t"
	}
	tests := []struct {
		name    string
		group   *rolloutv1.ReleaseChannelModelGroup
		wantErr bool
	}{
		{name: "unassigned unknown identity is valid", group: &rolloutv1.ReleaseChannelModelGroup{}},
		{name: "unassigned pair is valid", group: unassigned()},
		{name: "unassigned observed identity may be noncanonical", group: unassigned(noncanonicalIdentity)},
		{name: "cleared pair keeps its generation", group: unassigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.AssignmentGeneration = 3 })},
		{name: "unassigned pair with on-target members is rejected", group: unassigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.OnTargetCount = 1 }), wantErr: true},
		{name: "unassigned pair with an active rollout is rejected", group: unassigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.ActiveRolloutId = 5 }), wantErr: true},
		{name: "negative generation is rejected", group: unassigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.AssignmentGeneration = -1 }), wantErr: true},
		{name: "checksum without the rest of the assignment is rejected", group: unassigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareChecksum = testChecksum }), wantErr: true},
		{name: "assigned and uploaded is valid", group: assigned()},
		{name: "assigned with a noncanonical observed identity is valid", group: assigned(noncanonicalIdentity)},
		{
			name: "assigned but not uploaded has no file id and is unavailable",
			group: assigned(func(g *rolloutv1.ReleaseChannelModelGroup) {
				g.FirmwareFileId = ""
				g.FirmwareAvailable = false
			}),
		},
		{name: "a file id without availability is rejected", group: assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareAvailable = false }), wantErr: true},
		{name: "availability without a file id is rejected", group: assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareFileId = "" }), wantErr: true},
		{name: "assignment without a checksum is rejected", group: assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareChecksum = "" }), wantErr: true},
		{name: "malformed checksum is rejected", group: assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareChecksum = "sha256:" + testChecksum }), wantErr: true},
		{name: "assigned without a generation is rejected", group: assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.AssignmentGeneration = 0 }), wantErr: true},
		{name: "assigned without a version is rejected", group: assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareVersion = "" }), wantErr: true},
		{name: "version containing NUL is rejected", group: assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareVersion = "v1\x00custom" }), wantErr: true},
		{name: "assigned without a target manufacturer is rejected", group: assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareTargetManufacturer = "" }), wantErr: true},
		{name: "assigned without a target model is rejected", group: assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareTargetModel = "" }), wantErr: true},
		{name: "noncanonical target manufacturer is rejected", group: assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareTargetManufacturer = "Bítmain" }), wantErr: true},
		{name: "noncanonical target model is rejected", group: assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareTargetModel = " S21 " }), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.group, test.wantErr)
		})
	}
}

func TestRolloutPaginationValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request proto.Message
		wantErr bool
	}{
		{
			name:    "channel list accepts default page size",
			request: &rolloutv1.ListReleaseChannelsRequest{},
		},
		{
			name:    "channel list accepts maximum page",
			request: &rolloutv1.ListReleaseChannelsRequest{PageSize: 1000},
		},
		{
			name:    "channel list rejects oversized page",
			request: &rolloutv1.ListReleaseChannelsRequest{PageSize: 1001},
			wantErr: true,
		},
		{
			name:    "rollout list accepts default page size",
			request: &rolloutv1.ListRolloutsRequest{},
		},
		{
			name:    "rollout list rejects oversized page",
			request: &rolloutv1.ListRolloutsRequest{PageSize: 1001},
			wantErr: true,
		},
		{
			name:    "rollout list accepts a poll cursor",
			request: &rolloutv1.ListRolloutsRequest{PollCursor: "MTIz"},
		},
		{
			name:    "rollout list accepts a page within a polling cycle",
			request: &rolloutv1.ListRolloutsRequest{PollCursor: "MTIz", Cursor: "page"},
		},
		{
			name:    "rollout list rejects poll cursor with timestamp filter",
			request: &rolloutv1.ListRolloutsRequest{PollCursor: "MTIz", UpdatedAfter: timestamppb.Now()},
			wantErr: true,
		},
		{
			name:    "rollout list rejects oversized poll cursor",
			request: &rolloutv1.ListRolloutsRequest{PollCursor: strings.Repeat("c", 101)},
			wantErr: true,
		},
		{
			name:    "rollout devices accept maximum page",
			request: &rolloutv1.ListRolloutDevicesRequest{RolloutId: 1, PageSize: 1000},
		},
		{
			name:    "rollout devices reject oversized page",
			request: &rolloutv1.ListRolloutDevicesRequest{RolloutId: 1, PageSize: 1001},
			wantErr: true,
		},
		{
			name:    "channel miners accept maximum page",
			request: &rolloutv1.ListReleaseChannelMinersRequest{ChannelId: 1, PageSize: 1000},
		},
		{
			name:    "channel miners reject oversized page",
			request: &rolloutv1.ListReleaseChannelMinersRequest{ChannelId: 1, PageSize: 1001},
			wantErr: true,
		},
		{
			name:    "channel model groups accept default page size",
			request: &rolloutv1.ListReleaseChannelModelGroupsRequest{ChannelId: 1},
		},
		{
			name:    "channel model groups accept maximum page",
			request: &rolloutv1.ListReleaseChannelModelGroupsRequest{ChannelId: 1, PageSize: 100},
		},
		{
			name:    "channel model groups reject negative page",
			request: &rolloutv1.ListReleaseChannelModelGroupsRequest{ChannelId: 1, PageSize: -1},
			wantErr: true,
		},
		{
			name:    "channel model groups reject oversized page",
			request: &rolloutv1.ListReleaseChannelModelGroupsRequest{ChannelId: 1, PageSize: 101},
			wantErr: true,
		},
		{
			name:    "channel model groups accept maximum cursor length",
			request: &rolloutv1.ListReleaseChannelModelGroupsRequest{ChannelId: 1, Cursor: strings.Repeat("c", 8192)},
		},
		{
			name:    "channel model groups reject oversized cursor",
			request: &rolloutv1.ListReleaseChannelModelGroupsRequest{ChannelId: 1, Cursor: strings.Repeat("c", 8193)},
			wantErr: true,
		},
		{
			name:    "membership conflicts accept organization-wide default page",
			request: &rolloutv1.ListReleaseChannelMembershipConflictsRequest{},
		},
		{
			name:    "membership conflicts accept channel filter and maximum page",
			request: &rolloutv1.ListReleaseChannelMembershipConflictsRequest{ChannelId: 1, PageSize: 100},
		},
		{
			name:    "membership conflicts reject negative channel filter",
			request: &rolloutv1.ListReleaseChannelMembershipConflictsRequest{ChannelId: -1},
			wantErr: true,
		},
		{
			name:    "membership conflicts reject negative page",
			request: &rolloutv1.ListReleaseChannelMembershipConflictsRequest{PageSize: -1},
			wantErr: true,
		},
		{
			name:    "membership conflicts reject oversized page",
			request: &rolloutv1.ListReleaseChannelMembershipConflictsRequest{PageSize: 101},
			wantErr: true,
		},
		{
			name:    "membership conflicts accept maximum cursor length",
			request: &rolloutv1.ListReleaseChannelMembershipConflictsRequest{Cursor: strings.Repeat("c", 100)},
		},
		{
			name:    "membership conflicts reject oversized cursor",
			request: &rolloutv1.ListReleaseChannelMembershipConflictsRequest{Cursor: strings.Repeat("c", 101)},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.request, test.wantErr)
		})
	}
}

func TestOptionalReleaseChannelIDValidation(t *testing.T) {
	t.Parallel()

	requests := []struct {
		name string
		new  func(channelID int64) proto.Message
	}{
		{
			name: "scope preview",
			new: func(channelID int64) proto.Message {
				return &rolloutv1.PreviewReleaseChannelScopeRequest{ChannelId: channelID}
			},
		},
		{
			name: "rollout list",
			new: func(channelID int64) proto.Message {
				return &rolloutv1.ListRolloutsRequest{ChannelId: channelID}
			},
		},
	}
	tests := []struct {
		name      string
		channelID int64
		wantErr   bool
	}{
		{name: "zero is valid"},
		{name: "positive is valid", channelID: 1},
		{name: "negative is rejected", channelID: -1, wantErr: true},
	}

	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			t.Parallel()

			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					t.Parallel()

					requireProtoValidation(t, request.new(test.channelID), test.wantErr)
				})
			}
		})
	}
}

func TestBoundedListResponseValidation(t *testing.T) {
	t.Parallel()

	newRollout := func() proto.Message { return activeRollout() }
	tests := []struct {
		name            string
		newResponse     func() proto.Message
		newElement      func() proto.Message
		collectionField protoreflect.Name
		maxItems        int
		cursorMaxLen    int
	}{
		{
			name:            "release channels",
			newResponse:     func() proto.Message { return &rolloutv1.ListReleaseChannelsResponse{} },
			newElement:      func() proto.Message { return &rolloutv1.ReleaseChannelSummary{} },
			collectionField: "channels",
			maxItems:        1000,
			cursorMaxLen:    100,
		},
		{
			name:            "release channel miners",
			newResponse:     func() proto.Message { return &rolloutv1.ListReleaseChannelMinersResponse{} },
			newElement:      func() proto.Message { return &rolloutv1.ReleaseChannelMiner{} },
			collectionField: "miners",
			maxItems:        1000,
			cursorMaxLen:    100,
		},
		{
			name:            "rollouts",
			newResponse:     func() proto.Message { return &rolloutv1.ListRolloutsResponse{} },
			newElement:      newRollout,
			collectionField: "rollouts",
			maxItems:        1000,
			cursorMaxLen:    100,
		},
		{
			name:            "rollout devices",
			newResponse:     func() proto.Message { return &rolloutv1.ListRolloutDevicesResponse{} },
			newElement:      func() proto.Message { return queuedDevice() },
			collectionField: "devices",
			maxItems:        1000,
			cursorMaxLen:    100,
		},
		{
			name:            "release channel model groups",
			newResponse:     func() proto.Message { return &rolloutv1.ListReleaseChannelModelGroupsResponse{} },
			newElement:      func() proto.Message { return &rolloutv1.ReleaseChannelModelGroup{} },
			collectionField: "model_groups",
			maxItems:        100,
			cursorMaxLen:    8192,
		},
		{
			name:            "membership conflicts",
			newResponse:     func() proto.Message { return &rolloutv1.ListReleaseChannelMembershipConflictsResponse{} },
			newElement:      func() proto.Message { return conflict() },
			collectionField: "conflicts",
			maxItems:        100,
			cursorMaxLen:    100,
		},
		{
			// The events cursor is never empty, so the fixture carries one.
			name:            "rollout events",
			newResponse:     func() proto.Message { return &rolloutv1.ListRolloutEventsResponse{Cursor: "c"} },
			newElement:      func() proto.Message { return rolloutEvent() },
			collectionField: "events",
			maxItems:        1000,
			cursorMaxLen:    100,
		},
		{
			name:            "applied rollouts",
			newResponse:     func() proto.Message { return &rolloutv1.ApplyReleaseChannelFirmwareResponse{} },
			newElement:      newRollout,
			collectionField: "started_rollouts",
			maxItems:        100,
		},
		{
			name:            "rollback rollout",
			newResponse:     func() proto.Message { return &rolloutv1.RollbackReleaseChannelFirmwareResponse{} },
			newElement:      newRollout,
			collectionField: "started_rollouts",
			maxItems:        1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			response := test.newResponse()
			responseMessage := response.ProtoReflect()
			collection := responseMessage.Mutable(
				responseMessage.Descriptor().Fields().ByName(test.collectionField),
			).List()
			for range test.maxItems {
				collection.Append(protoreflect.ValueOfMessage(test.newElement().ProtoReflect()))
			}
			requireProtoValidation(t, response, false)

			collection.Append(protoreflect.ValueOfMessage(test.newElement().ProtoReflect()))
			requireProtoValidation(t, response, true)

			if test.cursorMaxLen == 0 {
				return
			}
			response = test.newResponse()
			responseMessage = response.ProtoReflect()
			cursor := responseMessage.Descriptor().Fields().ByName("cursor")
			responseMessage.Set(cursor, protoreflect.ValueOfString(strings.Repeat("c", test.cursorMaxLen)))
			requireProtoValidation(t, response, false)
			responseMessage.Set(cursor, protoreflect.ValueOfString(strings.Repeat("c", test.cursorMaxLen+1)))
			requireProtoValidation(t, response, true)
		})
	}
}

func TestReleaseChannelMembershipConflictValidation(t *testing.T) {
	t.Parallel()

	resolved := func(specificity rolloutv1.ReleaseChannelSelectorSpecificity, resolution rolloutv1.ReleaseChannelConflictResolution) *rolloutv1.ReleaseChannelMembershipConflict {
		c := conflict()
		c.SelectorSpecificity = specificity
		c.Resolution = resolution
		return c
	}
	edited := func(edit func(*rolloutv1.ReleaseChannelMembershipConflict)) *rolloutv1.ReleaseChannelMembershipConflict {
		c := conflict()
		edit(c)
		return c
	}
	tests := []struct {
		name     string
		conflict *rolloutv1.ReleaseChannelMembershipConflict
		wantErr  bool
	}{
		{name: "winner is valid", conflict: conflict()},
		{name: "loser is valid", conflict: resolved(rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_SITE, rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_LOSER)},
		{name: "excluded tie is valid", conflict: resolved(rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_RACK, rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_EXCLUDED_TIE)},
		{name: "unspecified selector specificity is rejected", conflict: resolved(rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_UNSPECIFIED, rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER), wantErr: true},
		{name: "unknown selector specificity is rejected", conflict: resolved(rolloutv1.ReleaseChannelSelectorSpecificity(99), rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER), wantErr: true},
		{name: "unspecified resolution is rejected", conflict: resolved(rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER, rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_UNSPECIFIED), wantErr: true},
		{name: "unknown resolution is rejected", conflict: resolved(rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER, rolloutv1.ReleaseChannelConflictResolution(99)), wantErr: true},
		{name: "oversized device identifier is rejected", conflict: edited(func(c *rolloutv1.ReleaseChannelMembershipConflict) { c.DeviceIdentifier = strings.Repeat("d", 256) }), wantErr: true},
		{name: "oversized channel name is rejected", conflict: edited(func(c *rolloutv1.ReleaseChannelMembershipConflict) { c.ChannelName = strings.Repeat("c", 101) }), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.conflict, test.wantErr)
		})
	}
}

func TestPreviewReleaseChannelScopeResponseValidation(t *testing.T) {
	t.Parallel()

	models := make([]*rolloutv1.ReleaseChannelScopeModelCount, 101)
	conflicts := make([]*rolloutv1.ReleaseChannelScopeConflict, 101)
	for index := range models {
		models[index] = &rolloutv1.ReleaseChannelScopeModelCount{
			Manufacturer: "manufacturer",
			Model:        fmt.Sprintf("model-%d", index),
			MinerCount:   1,
		}
		conflicts[index] = &rolloutv1.ReleaseChannelScopeConflict{
			ChannelId:   int64(index + 1),
			ChannelName: fmt.Sprintf("channel-%d", index),
			MinerCount:  1,
		}
	}

	tests := []struct {
		name     string
		response *rolloutv1.PreviewReleaseChannelScopeResponse
		wantErr  bool
	}{
		{
			name: "bounded truncated results are valid",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				MinerCount:    101,
				Models:        models[:100],
				Conflicts:     conflicts[:100],
				ModelCount:    101,
				ConflictCount: 101,
			},
		},
		{
			name: "complete results below the limit are valid",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				MinerCount:    2,
				Models:        models[:2],
				Conflicts:     conflicts[:2],
				ModelCount:    2,
				ConflictCount: 2,
			},
		},
		{
			name: "empty model group is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				MinerCount: 1,
				Models:     []*rolloutv1.ReleaseChannelScopeModelCount{{Manufacturer: "Bitmain", Model: "S21"}},
				ModelCount: 1,
			},
			wantErr: true,
		},
		{
			name: "empty conflict is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				MinerCount:    1,
				Conflicts:     []*rolloutv1.ReleaseChannelScopeConflict{{ChannelId: 1, ChannelName: "stable"}},
				ConflictCount: 1,
			},
			wantErr: true,
		},
		{
			name: "more model groups than miners is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				MinerCount: 1,
				Models:     models[:2],
				ModelCount: 2,
			},
			wantErr: true,
		},
		{
			name: "model group larger than the preview is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				MinerCount: 1,
				Models:     []*rolloutv1.ReleaseChannelScopeModelCount{{Manufacturer: "Bitmain", Model: "S21", MinerCount: 2}},
				ModelCount: 1,
			},
			wantErr: true,
		},
		{
			name: "101 models are rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Models:     models,
				ModelCount: 101,
			},
			wantErr: true,
		},
		{
			name: "101 conflicts are rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Conflicts:     conflicts,
				ConflictCount: 101,
			},
			wantErr: true,
		},
		{
			name: "model count below returned list length is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Models:     models[:2],
				ModelCount: 1,
			},
			wantErr: true,
		},
		{
			name: "conflict count below returned list length is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Conflicts:     conflicts[:2],
				ConflictCount: 1,
			},
			wantErr: true,
		},
		{
			name: "underfilled model list is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Models:     models[:1],
				ModelCount: 2,
			},
			wantErr: true,
		},
		{
			name: "underfilled conflict list is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Conflicts:     conflicts[:1],
				ConflictCount: 2,
			},
			wantErr: true,
		},
		{
			name: "oversized conflict channel name is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Conflicts: []*rolloutv1.ReleaseChannelScopeConflict{{
					ChannelId:   1,
					ChannelName: strings.Repeat("c", 101),
				}},
				ConflictCount: 1,
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.response, test.wantErr)
		})
	}
}

func TestDeploymentProvenanceValidation(t *testing.T) {
	t.Parallel()

	carriers := []struct {
		name string
		new  func(checksum string) proto.Message
	}{
		{name: "release channel miner", new: func(checksum string) proto.Message {
			return &rolloutv1.ReleaseChannelMiner{LastDeployedFirmwareChecksum: checksum}
		}},
		{name: "rollout device", new: func(checksum string) proto.Message {
			device := queuedDevice()
			device.LastDeployedFirmwareChecksum = checksum
			return device
		}},
	}
	for _, carrier := range carriers {
		t.Run(carrier.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, carrier.new(""), false)
			requireProtoValidation(t, carrier.new(testChecksum), false)
			requireProtoValidation(t, carrier.new(strings.ToUpper(testChecksum)), true)
			requireProtoValidation(t, carrier.new(testChecksum[:63]), true)
			requireProtoValidation(t, carrier.new("file-1"), true)
		})
	}
}

// Summaries carry counts; the lists behind them have their own paged RPCs.
func TestRolloutDetailsArePagedSeparately(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		message proto.Message
		lists   []protoreflect.Name
	}{
		{message: &rolloutv1.Rollout{}, lists: []protoreflect.Name{"devices"}},
		{message: &rolloutv1.ReleaseChannelModelGroup{}, lists: []protoreflect.Name{"miners"}},
		{message: &rolloutv1.ReleaseChannelSummary{}, lists: []protoreflect.Name{"scope", "model_groups"}},
		{message: &rolloutv1.ReleaseChannel{}, lists: []protoreflect.Name{"model_groups"}},
	} {
		descriptor := test.message.ProtoReflect().Descriptor()
		for _, list := range test.lists {
			require.Nil(t, descriptor.Fields().ByName(list), "%s.%s", descriptor.Name(), list)
		}
	}
}

// Read RPCs are safe for HTTP GET, and every mutating RPC that names a rollout
// is conditional under the revision rule. Both derive from the descriptors so
// a new RPC cannot skip either.
func TestServiceMethodContracts(t *testing.T) {
	t.Parallel()

	methods := rolloutv1.File_rollout_v1_rollout_proto.Services().ByName("RolloutService").Methods()
	for i := range methods.Len() {
		method := methods.Get(i)
		name := string(method.Name())
		options, ok := method.Options().(*descriptorpb.MethodOptions)
		require.True(t, ok, name)
		readOnly := strings.HasPrefix(name, "List") || strings.HasPrefix(name, "Get") || strings.HasPrefix(name, "Preview")
		require.Equal(t, readOnly, options.GetIdempotencyLevel() == descriptorpb.MethodOptions_NO_SIDE_EFFECTS, name)

		input := method.Input().Fields()
		if !readOnly && input.ByName("rollout_id") != nil {
			require.NotNil(t, input.ByName("expected_revision"), name)
		}
	}
}

func TestRequiredManufacturerModelTargetKeyValidation(t *testing.T) {
	t.Parallel()

	messages := []struct {
		name string
		new  func(manufacturer, model string) proto.Message
	}{
		{
			name: "firmware assignment",
			new: func(manufacturer, model string) proto.Message {
				return &rolloutv1.FirmwareAssignment{Manufacturer: manufacturer, Model: model}
			},
		},
		{
			name: "rollout",
			new: func(manufacturer, model string) proto.Message {
				rollout := activeRollout()
				rollout.Manufacturer = manufacturer
				rollout.Model = model
				return rollout
			},
		},
	}
	tests := []struct {
		name         string
		manufacturer string
		model        string
		wantErr      bool
	}{
		{name: "simple keys are valid", manufacturer: "Bitmain", model: "S21"},
		{name: "internal printable spaces are valid", manufacturer: "Bit Main", model: "S 21 Pro"},
		{name: "maximum lengths are valid", manufacturer: strings.Repeat("m", 255), model: strings.Repeat("n", 255)},
		{name: "empty manufacturer is rejected", model: "S21", wantErr: true},
		{name: "empty model is rejected", manufacturer: "Bitmain", wantErr: true},
		{name: "whitespace-only manufacturer is rejected", manufacturer: " \t\n", model: "S21", wantErr: true},
		{name: "whitespace-only model is rejected", manufacturer: "Bitmain", model: " \t\n", wantErr: true},
		{name: "non-ASCII manufacturer is rejected", manufacturer: "Bítmain", model: "S21", wantErr: true},
		{name: "non-ASCII model is rejected", manufacturer: "Bitmain", model: "S２1", wantErr: true},
		{name: "leading manufacturer space is rejected", manufacturer: " Bitmain", model: "S21", wantErr: true},
		{name: "trailing manufacturer space is rejected", manufacturer: "Bitmain ", model: "S21", wantErr: true},
		{name: "leading model space is rejected", manufacturer: "Bitmain", model: " S21", wantErr: true},
		{name: "trailing model space is rejected", manufacturer: "Bitmain", model: "S21 ", wantErr: true},
		{name: "internal control whitespace is rejected", manufacturer: "Bit\tmain", model: "S21", wantErr: true},
		{name: "oversized manufacturer is rejected", manufacturer: strings.Repeat("m", 256), model: "S21", wantErr: true},
		{name: "oversized model is rejected", manufacturer: "Bitmain", model: strings.Repeat("m", 256), wantErr: true},
	}

	for _, message := range messages {
		t.Run(message.name, func(t *testing.T) {
			t.Parallel()

			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					t.Parallel()

					requireProtoValidation(t, message.new(test.manufacturer, test.model), test.wantErr)
				})
			}
		})
	}
}

func TestObservedManufacturerModelIdentityValidation(t *testing.T) {
	t.Parallel()

	messages := []struct {
		name string
		new  func(manufacturer, model string) proto.Message
	}{
		{
			name: "model group",
			new: func(manufacturer, model string) proto.Message {
				return &rolloutv1.ReleaseChannelModelGroup{Manufacturer: manufacturer, Model: model}
			},
		},
		{
			name: "channel miner",
			new: func(manufacturer, model string) proto.Message {
				return &rolloutv1.ReleaseChannelMiner{Manufacturer: manufacturer, Model: model}
			},
		},
		{
			name: "membership conflict",
			new: func(manufacturer, model string) proto.Message {
				c := conflict()
				c.Manufacturer = manufacturer
				c.Model = model
				return c
			},
		},
		{
			name: "scope model count",
			new: func(manufacturer, model string) proto.Message {
				return &rolloutv1.ReleaseChannelScopeModelCount{Manufacturer: manufacturer, Model: model, MinerCount: 1}
			},
		},
	}
	for _, message := range messages {
		t.Run(message.name, func(t *testing.T) {
			t.Parallel()

			for _, test := range observedIdentityValidationCases {
				t.Run(test.observedName, func(t *testing.T) {
					t.Parallel()

					manufacturer, model := test.observedValues()
					requireProtoValidation(
						t,
						message.new(manufacturer, model),
						test.wantErr,
					)
				})
			}
		})
	}
}

func TestOptionalManufacturerModelFilterValidation(t *testing.T) {
	t.Parallel()

	for _, test := range observedIdentityValidationCases {
		if test.filterName == "" {
			continue
		}

		t.Run(test.filterName, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, &rolloutv1.ListReleaseChannelMinersRequest{
				ChannelId:    1,
				Manufacturer: test.manufacturer,
				Model:        test.model,
			}, test.wantErr || test.filterWantErr)
		})
	}
}

func TestPersistedRequestStringsRejectNUL(t *testing.T) {
	t.Parallel()

	fields := []struct {
		name string
		new  func(value string) proto.Message
	}{
		{
			name: "scope device identifier",
			new: func(value string) proto.Message {
				return &rolloutv1.ReleaseChannelScope{DeviceIdentifiers: []string{value}}
			},
		},
		{
			name: "create channel name",
			new: func(value string) proto.Message {
				return &rolloutv1.CreateReleaseChannelRequest{Name: value}
			},
		},
		{
			name: "create channel description",
			new: func(value string) proto.Message {
				return &rolloutv1.CreateReleaseChannelRequest{Name: "stable", Description: value}
			},
		},
		{
			name: "update channel name",
			new: func(value string) proto.Message {
				return &rolloutv1.UpdateReleaseChannelRequest{ChannelId: 1, Name: value}
			},
		},
		{
			name: "update channel description",
			new: func(value string) proto.Message {
				return &rolloutv1.UpdateReleaseChannelRequest{ChannelId: 1, Name: "stable", Description: value}
			},
		},
	}

	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, field.new("miner-01"), false)
			requireProtoValidation(t, field.new("miner\x0001"), true)
		})
	}
}

func TestRolloutDeviceValidation(t *testing.T) {
	t.Parallel()

	withLastError := func(length int) *rolloutv1.RolloutDevice {
		device := queuedDevice()
		device.LastError = strings.Repeat("e", length)
		return device
	}
	tests := []struct {
		name    string
		device  *rolloutv1.RolloutDevice
		wantErr bool
	}{
		{name: "queued device is valid", device: queuedDevice()},
		{name: "maximum last error is valid", device: withLastError(2048)},
		{name: "unspecified phase is rejected", device: &rolloutv1.RolloutDevice{}, wantErr: true},
		{name: "unknown phase is rejected", device: &rolloutv1.RolloutDevice{Phase: rolloutv1.RolloutDevicePhase(99)}, wantErr: true},
		{name: "oversized last error is rejected", device: withLastError(2049), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.device, test.wantErr)
		})
	}
}

func TestSkippedTargetsCountValidation(t *testing.T) {
	t.Parallel()

	sums := activeRollout()
	sums.DeviceCount = 4
	sums.DeviceCounts = &rolloutv1.RolloutDeviceCounts{Queued: 1, Done: 2, Skipped: 1}
	requireProtoValidation(t, sums, false)

	sums.DeviceCounts.Skipped = 2
	requireProtoValidation(t, sums, true)

	// A skipped target is neutral: it does not turn a completed rollout into a
	// failed one, and evidence must account for it.
	completed := finishedRollout(rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED)
	completed.DeviceCount = 2
	completed.DeviceCounts = &rolloutv1.RolloutDeviceCounts{Done: 1, Skipped: 1}
	requireProtoValidation(t, completed, false)

	evidence := activeRollout()
	evidence.DeviceCount = 2
	evidence.DeviceCounts = &rolloutv1.RolloutDeviceCounts{Done: 1, Skipped: 1}
	evidence.Evidence = &rolloutv1.RolloutEvidence{DevicesTotal: 2, Verified: 1, Online: 1, Skipped: 1}
	requireProtoValidation(t, evidence, false)
	// verified + skipped may not exceed devices_total.
	evidence.Evidence.Skipped = 2
	requireProtoValidation(t, evidence, true)
}

func TestAdvanceRolloutRequestValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request *rolloutv1.AdvanceRolloutRequest
		wantErr bool
	}{
		{
			name: "advance by count",
			request: &rolloutv1.AdvanceRolloutRequest{
				RolloutId: 1, Selection: &rolloutv1.AdvanceRolloutRequest_Count{Count: 5}, ExpectedRevision: 3, Note: "batch 2 passed",
			},
		},
		{
			name: "advance named devices",
			request: &rolloutv1.AdvanceRolloutRequest{
				RolloutId: 1,
				Selection: &rolloutv1.AdvanceRolloutRequest_Devices{
					Devices: &rolloutv1.RolloutDeviceSelection{DeviceIdentifiers: []string{"miner-1", "miner-2"}},
				},
			},
		},
		{name: "a selection is required", request: &rolloutv1.AdvanceRolloutRequest{RolloutId: 1}, wantErr: true},
		{
			name:    "count must be positive",
			request: &rolloutv1.AdvanceRolloutRequest{RolloutId: 1, Selection: &rolloutv1.AdvanceRolloutRequest_Count{Count: 0}},
			wantErr: true,
		},
		{
			name:    "count is bounded",
			request: &rolloutv1.AdvanceRolloutRequest{RolloutId: 1, Selection: &rolloutv1.AdvanceRolloutRequest_Count{Count: 1001}},
			wantErr: true,
		},
		{
			name: "named devices must not be empty",
			request: &rolloutv1.AdvanceRolloutRequest{
				RolloutId: 1, Selection: &rolloutv1.AdvanceRolloutRequest_Devices{Devices: &rolloutv1.RolloutDeviceSelection{}},
			},
			wantErr: true,
		},
		{
			name: "named devices must be unique",
			request: &rolloutv1.AdvanceRolloutRequest{
				RolloutId: 1,
				Selection: &rolloutv1.AdvanceRolloutRequest_Devices{
					Devices: &rolloutv1.RolloutDeviceSelection{DeviceIdentifiers: []string{"miner-1", "miner-1"}},
				},
			},
			wantErr: true,
		},
		{
			name: "notes cannot contain NUL",
			request: &rolloutv1.AdvanceRolloutRequest{
				RolloutId: 1, Selection: &rolloutv1.AdvanceRolloutRequest_Count{Count: 1}, Note: "bad\x00note",
			},
			wantErr: true,
		},
		{
			name:    "expected revision cannot be negative",
			request: &rolloutv1.AdvanceRolloutRequest{RolloutId: 1, Selection: &rolloutv1.AdvanceRolloutRequest_Count{Count: 1}, ExpectedRevision: -1},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			requireProtoValidation(t, test.request, test.wantErr)
		})
	}
}

func TestSkipAndCompleteRequestValidation(t *testing.T) {
	t.Parallel()

	requireProtoValidation(t, &rolloutv1.SkipRolloutDevicesRequest{
		RolloutId: 1, Devices: &rolloutv1.RolloutDeviceSelection{DeviceIdentifiers: []string{"miner-1"}}, Note: "flaky PSU",
	}, false)
	requireProtoValidation(t, &rolloutv1.SkipRolloutDevicesRequest{RolloutId: 1}, true)
	requireProtoValidation(t, &rolloutv1.SkipRolloutDevicesRequest{
		RolloutId: 1, Devices: &rolloutv1.RolloutDeviceSelection{DeviceIdentifiers: []string{""}},
	}, true)

	requireProtoValidation(t, &rolloutv1.CompleteRolloutRequest{RolloutId: 1, ExpectedRevision: 7}, false)
	requireProtoValidation(t, &rolloutv1.CompleteRolloutRequest{RolloutId: 0}, true)
	requireProtoValidation(t, &rolloutv1.CompleteRolloutRequest{RolloutId: 1, Note: strings.Repeat("x", 1025)}, true)
}

func TestRolloutEventsValidation(t *testing.T) {
	t.Parallel()

	requireProtoValidation(t, &rolloutv1.ListRolloutEventsRequest{RolloutId: 1, PageSize: 1000}, false)
	requireProtoValidation(t, &rolloutv1.ListRolloutEventsRequest{PageSize: 1001}, true)

	event := rolloutEvent()
	requireProtoValidation(t, &rolloutv1.ListRolloutEventsResponse{Events: []*rolloutv1.RolloutEvent{event}, Cursor: "c1"}, false)
	// The events feed cursor is never empty, so a poller can always resume.
	requireProtoValidation(t, &rolloutv1.ListRolloutEventsResponse{}, true)
	requireProtoValidation(t, &rolloutv1.RolloutEvent{Id: 1, RolloutId: 1}, true)
	// Audit metadata is mandatory: when, by whom, and against which revision.
	for name, mutate := range map[string]func(*rolloutv1.RolloutEvent){
		"missing occurred_at":    func(e *rolloutv1.RolloutEvent) { e.OccurredAt = nil },
		"missing actor":          func(e *rolloutv1.RolloutEvent) { e.Actor = nil },
		"unspecified actor type": func(e *rolloutv1.RolloutEvent) { e.Actor = &rolloutv1.RolloutActor{Id: 4, Name: "x"} },
		"user actor without an id": func(e *rolloutv1.RolloutEvent) {
			e.Actor = &rolloutv1.RolloutActor{Type: rolloutv1.RolloutActorType_ROLLOUT_ACTOR_TYPE_USER, Name: "x"}
		},
		"system actor with an id": func(e *rolloutv1.RolloutEvent) {
			e.Actor = &rolloutv1.RolloutActor{Type: rolloutv1.RolloutActorType_ROLLOUT_ACTOR_TYPE_SYSTEM, Id: 4}
		},
		"zero rollout revision":  func(e *rolloutv1.RolloutEvent) { e.RolloutRevision = 0 },
		"unspecified event type": func(e *rolloutv1.RolloutEvent) { e.Type = rolloutv1.RolloutEventType_ROLLOUT_EVENT_TYPE_UNSPECIFIED },
	} {
		broken, ok := proto.Clone(event).(*rolloutv1.RolloutEvent)
		require.True(t, ok)
		mutate(broken)
		require.Error(t, protovalidate.Validate(broken), name)
	}
	requireProtoValidation(t, &rolloutv1.RolloutActor{Type: rolloutv1.RolloutActorType_ROLLOUT_ACTOR_TYPE_SYSTEM, Name: "enforcement"}, false)

	requireProtoValidation(t, &rolloutv1.RolloutErrorInfo{
		Reason: rolloutv1.RolloutErrorReason_ROLLOUT_ERROR_REASON_STALE_REVISION, CurrentRevision: 8,
	}, false)
	requireProtoValidation(t, &rolloutv1.RolloutErrorInfo{}, true)
}

func TestPreviewReleaseChannelFirmwareRequestValidation(t *testing.T) {
	t.Parallel()

	assignment := assignmentOf("Bitmain", "S21", "file-1")
	requireProtoValidation(t, &rolloutv1.PreviewReleaseChannelFirmwareRequest{
		ChannelId: 1, Assignments: []*rolloutv1.FirmwareAssignment{assignment}, BehaviorOverride: delegatedBehavior(),
	}, false)
	requireProtoValidation(t, &rolloutv1.PreviewReleaseChannelFirmwareRequest{ChannelId: 1}, true)
	// The offline budget is channel-wide; overrides cannot carry one.
	capped := &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_DELEGATED, MaxConcurrentOffline: 1}
	requireProtoValidation(t, &rolloutv1.PreviewReleaseChannelFirmwareRequest{
		ChannelId: 1, Assignments: []*rolloutv1.FirmwareAssignment{assignment}, BehaviorOverride: capped,
	}, true)
	cappedApply := applyRequest(assignment)
	cappedApply.BehaviorOverride = capped
	requireProtoValidation(t, cappedApply, true)
	delegatedApply := applyRequest(assignment)
	delegatedApply.BehaviorOverride = delegatedBehavior()
	requireProtoValidation(t, delegatedApply, false)
	requireProtoValidation(t, &rolloutv1.PreviewReleaseChannelFirmwareRequest{
		ChannelId: 1, Assignments: []*rolloutv1.FirmwareAssignment{assignment, assignment},
	}, true)
	plan := func(fileID, version, checksum string) *rolloutv1.PreviewReleaseChannelFirmwareResponse {
		return &rolloutv1.PreviewReleaseChannelFirmwareResponse{
			Plans: []*rolloutv1.ReleaseChannelFirmwarePlan{{
				Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: fileID, FirmwareVersion: version, FirmwareChecksum: checksum,
				TargetCount: 40, OnTargetCount: 10, Behavior: delegatedBehavior(),
			}},
		}
	}
	requireProtoValidation(t, plan("file-1", "2.0", testChecksum), false)
	requireProtoValidation(t, plan("", "", ""), false)
	requireProtoValidation(t, plan("file-1", "2.0", ""), true)
	requireProtoValidation(t, plan("", "2.0", testChecksum), true)
	requireProtoValidation(t, plan("file-1", "", testChecksum), true)
}

// The unmerged contract carries no history: nothing reserved, contiguous field
// numbers, and none of the retired retry-chain fields or error reasons.
func TestContractCarriesNoHistory(t *testing.T) {
	t.Parallel()

	file := rolloutv1.File_rollout_v1_rollout_proto
	messages := file.Messages()
	for i := range messages.Len() {
		message := messages.Get(i)
		require.Zero(t, message.ReservedNames().Len(), message.Name())
		require.Zero(t, message.ReservedRanges().Len(), message.Name())
		fields := message.Fields()
		for j := range fields.Len() {
			require.LessOrEqual(t, int(fields.Get(j).Number()), fields.Len(), "%s.%s", message.Name(), fields.Get(j).Name())
		}
	}
	enums := file.Enums()
	for i := range enums.Len() {
		require.Zero(t, enums.Get(i).ReservedRanges().Len(), enums.Get(i).Name())
	}

	rolloutFields := (&rolloutv1.Rollout{}).ProtoReflect().Descriptor().Fields()
	for _, retired := range []protoreflect.Name{"previous_firmware_file_id", "retry_of_rollout_id", "successor_rollout_id"} {
		require.Nil(t, rolloutFields.ByName(retired), retired)
	}
	reasons := rolloutv1.RolloutErrorReason(0).Descriptor().Values()
	for _, retired := range []protoreflect.Name{"ROLLOUT_ERROR_REASON_NOT_LATEST", "ROLLOUT_ERROR_REASON_ALREADY_RETRIED", "ROLLOUT_ERROR_REASON_ARTIFACT_PROTECTED"} {
		require.Nil(t, reasons.ByName(retired), retired)
	}
}
