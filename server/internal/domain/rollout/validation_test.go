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

type deploymentProvenanceValidationCase struct {
	name            string
	response        func(string) proto.Message
	collectionField protoreflect.Name
	fieldNumber     protoreflect.FieldNumber
}

var deploymentProvenanceValidationCases = []deploymentProvenanceValidationCase{
	{
		name: "release channel miners",
		response: func(checksum string) proto.Message {
			return &rolloutv1.ListReleaseChannelMinersResponse{
				Miners: []*rolloutv1.ReleaseChannelMiner{{
					LastDeployedFirmwareChecksum: checksum,
				}},
			}
		},
		collectionField: "miners",
		fieldNumber:     7,
	},
	{
		name: "rollout devices",
		response: func(checksum string) proto.Message {
			device := queuedDevice()
			device.LastDeployedFirmwareChecksum = checksum
			return &rolloutv1.ListRolloutDevicesResponse{Devices: []*rolloutv1.RolloutDevice{device}}
		},
		collectionField: "devices",
		fieldNumber:     21,
	},
}

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
// the state, cancel reason, and finished_at that status requires.
func finishedRollout(status rolloutv1.RolloutStatus) *rolloutv1.Rollout {
	rollout := activeRollout()
	rollout.Status = status
	rollout.FinishedAt = timestamppb.Now()
	switch status {
	case rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED:
		rollout.State = rolloutv1.RolloutState_ROLLOUT_STATE_COMPLETED
	case rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED_WITH_FAILURES:
		rollout.State = rolloutv1.RolloutState_ROLLOUT_STATE_COMPLETED_WITH_FAILURES
		rollout.DeviceCount = 1
		rollout.DeviceCounts = &rolloutv1.RolloutDeviceCounts{Failed: 1}
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
		{name: "active rollout with a terminal state is rejected", rollout: mutate(func(r *rolloutv1.Rollout) { r.State = rolloutv1.RolloutState_ROLLOUT_STATE_COMPLETED }), wantErr: true},
		{
			name: "completed rollout with a mismatched terminal state is rejected",
			rollout: func() *rolloutv1.Rollout {
				r := finishedRollout(rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED)
				r.State = rolloutv1.RolloutState_ROLLOUT_STATE_CANCELED
				return r
			}(),
			wantErr: true,
		},
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

func TestRolloutTerminalStatusPhaseValidation(t *testing.T) {
	t.Parallel()

	finishedWith := func(status rolloutv1.RolloutStatus, counts *rolloutv1.RolloutDeviceCounts) *rolloutv1.Rollout {
		rollout := finishedRollout(status)
		rollout.DeviceCounts = counts
		rollout.DeviceCount = counts.GetQueued() + counts.GetInProgress() + counts.GetRetrying() + counts.GetDone() + counts.GetFailed() + counts.GetExcluded()
		return rollout
	}
	completed := rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED
	completedWithFailures := rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED_WITH_FAILURES
	canceled := rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELED
	tests := []struct {
		name    string
		rollout *rolloutv1.Rollout
		wantErr bool
	}{
		{name: "completed with done and excluded is valid", rollout: finishedWith(completed, &rolloutv1.RolloutDeviceCounts{Done: 2, Excluded: 1})},
		{name: "completed with a failure is rejected", rollout: finishedWith(completed, &rolloutv1.RolloutDeviceCounts{Done: 2, Failed: 1}), wantErr: true},
		{name: "completed with a queued target is rejected", rollout: finishedWith(completed, &rolloutv1.RolloutDeviceCounts{Done: 2, Queued: 1}), wantErr: true},
		{name: "completed with failures and settled targets is valid", rollout: finishedWith(completedWithFailures, &rolloutv1.RolloutDeviceCounts{Done: 1, Failed: 1, Excluded: 1})},
		{name: "completed with failures but none failed is rejected", rollout: finishedWith(completedWithFailures, &rolloutv1.RolloutDeviceCounts{Done: 2}), wantErr: true},
		{name: "completed with failures and an in-progress target is rejected", rollout: finishedWith(completedWithFailures, &rolloutv1.RolloutDeviceCounts{Failed: 1, InProgress: 1}), wantErr: true},
		{name: "canceled with unsettled targets is valid", rollout: finishedWith(canceled, &rolloutv1.RolloutDeviceCounts{Queued: 1, Done: 1, Failed: 1})},
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
		{
			name: "all-at-once rollout with batches is rejected",
			rollout: func() *rolloutv1.Rollout {
				r := activeRollout()
				r.BatchCount = 1
				return r
			},
			wantErr: true,
		},
		{
			name: "batched rollout without batches is rejected",
			rollout: func() *rolloutv1.Rollout {
				r := batchedRollout(0)
				r.Stage = rolloutv1.RolloutStage_ROLLOUT_STAGE_REST
				return r
			},
			wantErr: true,
		},
		{
			name: "all-at-once rollout outside the rest stage is rejected",
			rollout: func() *rolloutv1.Rollout {
				r := activeRollout()
				r.Stage = rolloutv1.RolloutStage_ROLLOUT_STAGE_BATCH
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

func TestRolloutGateStateValidation(t *testing.T) {
	t.Parallel()

	atGate := func(method rolloutv1.RolloutMethod, review, autoContinue bool, state rolloutv1.RolloutState) *rolloutv1.Rollout {
		rollout := activeRollout()
		rollout.Behavior = &rolloutv1.RolloutBehavior{
			Method:                         method,
			BatchSize:                      1,
			PilotSize:                      1,
			ReviewAfterEachBatch:           review,
			AutoContinueOnHealthyTelemetry: autoContinue,
		}
		rollout.BatchCount = 1
		rollout.Stage = rolloutv1.RolloutStage_ROLLOUT_STAGE_AWAITING_REVIEW
		rollout.State = state
		return rollout
	}
	batched := rolloutv1.RolloutMethod_ROLLOUT_METHOD_BATCHED
	pilot := rolloutv1.RolloutMethod_ROLLOUT_METHOD_PILOT_THEN_CONTINUE
	tests := []struct {
		name    string
		rollout func() *rolloutv1.Rollout
		wantErr bool
	}{
		{name: "pilot gate is valid", rollout: func() *rolloutv1.Rollout {
			return atGate(pilot, false, false, rolloutv1.RolloutState_ROLLOUT_STATE_PAUSED_AT_PILOT_GATE)
		}},
		{name: "batch review gate is valid", rollout: func() *rolloutv1.Rollout {
			return atGate(batched, true, false, rolloutv1.RolloutState_ROLLOUT_STATE_PAUSED_AT_BATCH_REVIEW)
		}},
		{name: "stabilizing at a pilot gate is valid", rollout: func() *rolloutv1.Rollout {
			return atGate(pilot, false, true, rolloutv1.RolloutState_ROLLOUT_STATE_STABILIZING_TELEMETRY)
		}},
		{name: "operator pause at a gate is valid", rollout: func() *rolloutv1.Rollout {
			r := atGate(batched, true, false, rolloutv1.RolloutState_ROLLOUT_STATE_PAUSED)
			r.PausedAt = timestamppb.Now()
			return r
		}},
		{name: "waiting between ungated batches is valid", rollout: func() *rolloutv1.Rollout {
			r := batchedRollout(2)
			r.Stage = rolloutv1.RolloutStage_ROLLOUT_STAGE_WAITING
			return r
		}},
		{name: "review gate with a settled batch is valid", rollout: func() *rolloutv1.Rollout {
			r := atGate(batched, true, false, rolloutv1.RolloutState_ROLLOUT_STATE_PAUSED_AT_BATCH_REVIEW)
			r.DeviceCount = 2
			r.DeviceCounts = &rolloutv1.RolloutDeviceCounts{Queued: 1, Done: 1}
			r.CurrentBatchCounts = &rolloutv1.RolloutDeviceCounts{Done: 1}
			r.Evidence = &rolloutv1.RolloutEvidence{DevicesTotal: 1, Verified: 1, Online: 1}
			return r
		}},
		{name: "ready to advance outside the review stage is rejected", rollout: func() *rolloutv1.Rollout {
			r := activeRollout()
			r.Behavior = &rolloutv1.RolloutBehavior{AutoContinueOnHealthyTelemetry: true}
			r.Evidence = &rolloutv1.RolloutEvidence{ReadyToAdvance: true}
			return r
		}, wantErr: true},
		{name: "evidence on a finished rollout is rejected", rollout: func() *rolloutv1.Rollout {
			r := finishedRollout(rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED)
			r.Evidence = &rolloutv1.RolloutEvidence{}
			return r
		}, wantErr: true},
		{name: "ready to advance without auto-continue is rejected", rollout: func() *rolloutv1.Rollout {
			r := atGate(batched, true, false, rolloutv1.RolloutState_ROLLOUT_STATE_PAUSED_AT_BATCH_REVIEW)
			r.Evidence = &rolloutv1.RolloutEvidence{ReadyToAdvance: true}
			return r
		}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.rollout(), test.wantErr)
		})
	}
}

func TestReleaseChannelModelGroupUnassignedDerivedStateValidation(t *testing.T) {
	t.Parallel()

	requireProtoValidation(t, &rolloutv1.ReleaseChannelModelGroup{MinerCount: 3}, false)
	requireProtoValidation(t, &rolloutv1.ReleaseChannelModelGroup{MinerCount: 3, OnTargetCount: 1}, true)
	requireProtoValidation(t, &rolloutv1.ReleaseChannelModelGroup{ActiveRolloutId: 5}, true)
}

func TestReleaseChannelModelGroupFirmwareVersionRejectsNUL(t *testing.T) {
	t.Parallel()

	group := &rolloutv1.ReleaseChannelModelGroup{
		FirmwareFileId:             "file-1",
		FirmwareChecksum:           testChecksum,
		FirmwareAvailable:          true,
		FirmwareTargetManufacturer: "Bitmain",
		FirmwareTargetModel:        "S21",
		FirmwareVersion:            "v1\x00custom",
		AssignmentGeneration:       1,
	}
	requireProtoValidation(t, group, true)
	group.FirmwareVersion = "v1-custom"
	requireProtoValidation(t, group, false)
}

func TestReleaseChannelModelGroupAssignmentGenerationValidation(t *testing.T) {
	t.Parallel()

	assigned := func(generation int64) *rolloutv1.ReleaseChannelModelGroup {
		return &rolloutv1.ReleaseChannelModelGroup{
			FirmwareFileId:             "file-1",
			FirmwareChecksum:           testChecksum,
			FirmwareAvailable:          true,
			FirmwareTargetManufacturer: "Bitmain",
			FirmwareTargetModel:        "S21",
			FirmwareVersion:            "2.0",
			AssignmentGeneration:       generation,
		}
	}
	requireProtoValidation(t, assigned(1), false)
	requireProtoValidation(t, assigned(0), true)
	requireProtoValidation(t, &rolloutv1.ReleaseChannelModelGroup{AssignmentGeneration: 3}, false)
	requireProtoValidation(t, &rolloutv1.ReleaseChannelModelGroup{AssignmentGeneration: -1}, true)
}

func TestRolloutAutomationThresholdsCoverageValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		coverage *float64
		wantErr  bool
	}{
		{name: "unset coverage defaults to full"},
		{name: "full coverage is valid", coverage: proto.Float64(100)},
		{name: "fractional coverage is valid", coverage: proto.Float64(0.5)},
		{name: "zero coverage is rejected", coverage: proto.Float64(0), wantErr: true},
		{name: "coverage above 100 is rejected", coverage: proto.Float64(100.5), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, &rolloutv1.RolloutAutomationThresholds{
				MinSampleCoveragePercent: test.coverage,
			}, test.wantErr)
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

			for _, value := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
				thresholds := &rolloutv1.RolloutAutomationThresholds{}
				field.set(thresholds, value)
				requireProtoValidation(t, thresholds, true)
			}
			thresholds := &rolloutv1.RolloutAutomationThresholds{}
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
			name:     "sampled hashrate without its change is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Verified: 1, HashRateHs: sampledAggregate(1)},
			wantErr:  true,
		},
		{
			name:     "sampled temperature without its change is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Verified: 1, TempC: sampledAggregate(1)},
			wantErr:  true,
		},
		{
			name:     "verified plus failed above total is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 4, Verified: 3, Online: 4, Failed: 1, Excluded: 1},
			wantErr:  true,
		},
		{
			name:     "verified above online is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 2, Verified: 2, Online: 1},
			wantErr:  true,
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
			name:     "aggregate sampling more than verified is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 3, Verified: 1, PowerW: sampledAggregate(2)},
			wantErr:  true,
		},
		{
			name:     "hashrate change without samples is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Verified: 1, HashrateChangePercent: proto.Float64(0)},
			wantErr:  true,
		},
		{
			name:     "efficiency change without samples is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Verified: 1, EfficiencyChangePercent: proto.Float64(0)},
			wantErr:  true,
		},
		{
			name:     "temperature change without samples is rejected",
			evidence: &rolloutv1.RolloutEvidence{DevicesTotal: 1, Verified: 1, TemperatureChangeCelsius: proto.Float64(0)},
			wantErr:  true,
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
		oversizedAssignments[index] = &rolloutv1.FirmwareAssignment{
			Manufacturer:   "Bitmain",
			Model:          fmt.Sprintf("model-%d", index),
			FirmwareFileId: "firmware",
		}
	}

	tests := []struct {
		name    string
		request *rolloutv1.ApplyReleaseChannelFirmwareRequest
		wantErr bool
	}{
		{
			name: "duplicate manufacturer and model pair is rejected",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "firmware-a"},
					{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "firmware-b"},
				},
			},
			wantErr: true,
		},
		{
			name: "case variants of one manufacturer and model pair are rejected",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "firmware-a"},
					{Manufacturer: "bitMAIN", Model: "s21", FirmwareFileId: "firmware-b"},
				},
			},
			wantErr: true,
		},
		{
			name: "surrounding whitespace in an assignment target is rejected",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "firmware-a"},
					{Manufacturer: " Bitmain ", Model: "\tS21\n", FirmwareFileId: "firmware-b"},
				},
			},
			wantErr: true,
		},
		{
			name: "distinct manufacturer and model pairs are accepted",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "firmware-a"},
					{Manufacturer: "bitMAIN", Model: "S19", FirmwareFileId: "firmware-b"},
				},
			},
		},
		{
			name: "same model under different manufacturers is accepted",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "firmware-a"},
					{Manufacturer: "MicroBT", Model: "S21", FirmwareFileId: "firmware-b"},
				},
			},
		},
		{
			name: "different models under one manufacturer are accepted",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "firmware-a"},
					{Manufacturer: "Bitmain", Model: "S19", FirmwareFileId: "firmware-b"},
				},
			},
		},
		{
			name: "length-prefixed composite keys do not collide",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{
					{Manufacturer: "A", Model: "BC", FirmwareFileId: "firmware-a"},
					{Manufacturer: "AB", Model: "C", FirmwareFileId: "firmware-b"},
				},
			},
		},
		{
			name: "assignment count is bounded",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId:   1,
				Assignments: oversizedAssignments,
			},
			wantErr: true,
		},
		{
			name: "maximum firmware file id is accepted",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{{
					Manufacturer:   "Bitmain",
					Model:          "S21",
					FirmwareFileId: strings.Repeat("f", 255),
				}},
			},
		},
		{
			name: "oversized firmware file id is rejected",
			request: &rolloutv1.ApplyReleaseChannelFirmwareRequest{
				ChannelId: 1,
				Assignments: []*rolloutv1.FirmwareAssignment{{
					Manufacturer:   "Bitmain",
					Model:          "S21",
					FirmwareFileId: strings.Repeat("f", 256),
				}},
			},
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

func TestReleaseChannelModelGroupReportedVersionsValidation(t *testing.T) {
	t.Parallel()

	boundedVersions := []string{
		"1.0.0",
		"1.1.0",
		"1.2.0",
		"1.3.0",
		"1.4.0",
		"1.5.0",
		"1.6.0",
		"1.7.0",
		"1.8.0",
		"1.9.0",
	}
	oversizedVersions := append(append([]string{}, boundedVersions...), "2.0.0")

	tests := []struct {
		name       string
		modelGroup *rolloutv1.ReleaseChannelModelGroup
		wantErr    bool
	}{
		{
			name: "bounded truncated list is valid",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:         "Bitmain",
				Model:                "S21",
				MinerCount:           20,
				ReportedVersions:     boundedVersions,
				ReportedVersionCount: 12,
			},
		},
		{
			name: "oversized list is rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:         "Bitmain",
				Model:                "S21",
				MinerCount:           20,
				ReportedVersions:     oversizedVersions,
				ReportedVersionCount: 11,
			},
			wantErr: true,
		},
		{
			name: "count below returned list length is rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:         "Bitmain",
				Model:                "S21",
				MinerCount:           20,
				ReportedVersions:     []string{"1.0.0", "2.0.0"},
				ReportedVersionCount: 1,
			},
			wantErr: true,
		},
		{
			name: "partial list below the cap is rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:         "Bitmain",
				Model:                "S21",
				MinerCount:           20,
				ReportedVersions:     []string{"1.0.0", "2.0.0"},
				ReportedVersionCount: 5,
			},
			wantErr: true,
		},
		{
			name: "complete list below the cap is valid",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:         "Bitmain",
				Model:                "S21",
				MinerCount:           20,
				ReportedVersions:     []string{"1.0.0", "2.0.0"},
				ReportedVersionCount: 2,
			},
		},
		{
			name: "more versions than miners is rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:         "Bitmain",
				Model:                "S21",
				MinerCount:           1,
				ReportedVersions:     []string{"1.0.0", "2.0.0"},
				ReportedVersionCount: 2,
			},
			wantErr: true,
		},
		{
			name: "duplicate versions are rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:         "Bitmain",
				Model:                "S21",
				MinerCount:           20,
				ReportedVersions:     []string{"1.0.0", "1.0.0"},
				ReportedVersionCount: 2,
			},
			wantErr: true,
		},
		{
			name: "oversized version is rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:         "Bitmain",
				Model:                "S21",
				MinerCount:           20,
				ReportedVersions:     []string{strings.Repeat("v", 256)},
				ReportedVersionCount: 1,
			},
			wantErr: true,
		},
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

	tests := []struct {
		name       string
		modelGroup *rolloutv1.ReleaseChannelModelGroup
		wantErr    bool
	}{
		{
			name:       "unassigned unknown identity is valid",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{},
		},
		{
			name: "unassigned observed identity may be noncanonical",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer: "Bít main ",
				Model:        " S２1\t",
			},
		},
		{
			name: "complete assignment with noncanonical observed identity is valid",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:               "Bít main ",
				Model:                      " S２1\t",
				FirmwareFileId:             "firmware",
				FirmwareChecksum:           testChecksum,
				FirmwareAvailable:          true,
				FirmwareVersion:            "1.0.0",
				FirmwareTargetManufacturer: "Bitmain",
				FirmwareTargetModel:        "S21",
				AssignmentGeneration:       1,
			},
		},
		{
			name: "assignment fields without firmware file are rejected",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				FirmwareVersion:            "1.0.0",
				FirmwareTargetManufacturer: "Bitmain",
				FirmwareTargetModel:        "S21",
			},
			wantErr: true,
		},
		{
			name: "assigned firmware requires target manufacturer",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:        "Bitmain",
				Model:               "S21",
				FirmwareFileId:      "firmware",
				FirmwareChecksum:    testChecksum,
				FirmwareAvailable:   true,
				FirmwareVersion:     "1.0.0",
				FirmwareTargetModel: "S21",
			},
			wantErr: true,
		},
		{
			name: "assigned firmware requires target model",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:               "Bitmain",
				Model:                      "S21",
				FirmwareFileId:             "firmware",
				FirmwareChecksum:           testChecksum,
				FirmwareAvailable:          true,
				FirmwareVersion:            "1.0.0",
				FirmwareTargetManufacturer: "Bitmain",
			},
			wantErr: true,
		},
		{
			name: "assigned firmware requires version",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:               "Bitmain",
				Model:                      "S21",
				FirmwareFileId:             "firmware",
				FirmwareChecksum:           testChecksum,
				FirmwareAvailable:          true,
				FirmwareTargetManufacturer: "Bitmain",
				FirmwareTargetModel:        "S21",
			},
			wantErr: true,
		},
		{
			name: "assigned firmware requires canonical target manufacturer",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:               "Bitmain",
				Model:                      "S21",
				FirmwareFileId:             "firmware",
				FirmwareChecksum:           testChecksum,
				FirmwareAvailable:          true,
				FirmwareVersion:            "1.0.0",
				FirmwareTargetManufacturer: "Bítmain",
				FirmwareTargetModel:        "S21",
			},
			wantErr: true,
		},
		{
			name: "assigned firmware requires canonical target model",
			modelGroup: &rolloutv1.ReleaseChannelModelGroup{
				Manufacturer:               "Bitmain",
				Model:                      "S21",
				FirmwareFileId:             "firmware",
				FirmwareChecksum:           testChecksum,
				FirmwareAvailable:          true,
				FirmwareVersion:            "1.0.0",
				FirmwareTargetManufacturer: "Bitmain",
				FirmwareTargetModel:        " S21 ",
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.modelGroup, test.wantErr)
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

func TestListReleaseChannelMembershipConflictsResponseValidation(t *testing.T) {
	t.Parallel()

	conflicts := make([]*rolloutv1.ReleaseChannelMembershipConflict, 101)
	for index := range conflicts {
		conflicts[index] = &rolloutv1.ReleaseChannelMembershipConflict{
			DeviceId:            int64(index + 1),
			DeviceIdentifier:    fmt.Sprintf("device-%d", index),
			Manufacturer:        "Bitmain",
			Model:               "S21",
			ChannelId:           int64(index + 1),
			ChannelName:         fmt.Sprintf("channel-%d", index),
			SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
			Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
		}
	}

	tests := []struct {
		name     string
		response *rolloutv1.ListReleaseChannelMembershipConflictsResponse
		wantErr  bool
	}{
		{
			name: "maximum page and cursor length are valid",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: conflicts[:100],
				Cursor:    strings.Repeat("c", 100),
			},
		},
		{
			name: "winner resolution is valid",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}},
			},
		},
		{
			name: "loser resolution is valid",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_SITE,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_LOSER,
				}},
			},
		},
		{
			name: "excluded tie resolution is valid",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_RACK,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_EXCLUDED_TIE,
				}},
			},
		},
		{
			name: "oversized page is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: conflicts,
			},
			wantErr: true,
		},
		{
			name: "oversized cursor is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Cursor: strings.Repeat("c", 101),
			},
			wantErr: true,
		},
		{
			name: "oversized device identifier is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					DeviceIdentifier:    strings.Repeat("d", 256),
					Manufacturer:        "Bitmain",
					Model:               "S21",
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}},
			},
			wantErr: true,
		},
		{
			name: "oversized manufacturer is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					Manufacturer:        strings.Repeat("m", 256),
					Model:               "S21",
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}},
			},
			wantErr: true,
		},
		{
			name: "oversized model is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					Manufacturer:        "Bitmain",
					Model:               strings.Repeat("m", 256),
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}},
			},
			wantErr: true,
		},
		{
			name: "oversized channel name is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					Manufacturer:        "Bitmain",
					Model:               "S21",
					ChannelName:         strings.Repeat("c", 101),
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}},
			},
			wantErr: true,
		},
		{
			name: "unknown selector specificity is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					Manufacturer:        "Bitmain",
					Model:               "S21",
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity(99),
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}},
			},
			wantErr: true,
		},
		{
			name: "unspecified selector specificity is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					Manufacturer: "Bitmain",
					Model:        "S21",
					Resolution:   rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}},
			},
			wantErr: true,
		},
		{
			name: "unknown resolution is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					Manufacturer:        "Bitmain",
					Model:               "S21",
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution(99),
				}},
			},
			wantErr: true,
		},
		{
			name: "unspecified resolution is rejected",
			response: &rolloutv1.ListReleaseChannelMembershipConflictsResponse{
				Conflicts: []*rolloutv1.ReleaseChannelMembershipConflict{{
					Manufacturer:        "Bitmain",
					Model:               "S21",
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
				}},
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

func TestListReleaseChannelModelGroupsResponseValidation(t *testing.T) {
	t.Parallel()

	modelGroups := make([]*rolloutv1.ReleaseChannelModelGroup, 101)
	for index := range modelGroups {
		modelGroups[index] = &rolloutv1.ReleaseChannelModelGroup{
			Manufacturer: "Bitmain",
			Model:        fmt.Sprintf("model-%d", index),
		}
	}

	requireProtoValidation(t, &rolloutv1.ListReleaseChannelModelGroupsResponse{
		ModelGroups: modelGroups[:100],
	}, false)
	requireProtoValidation(t, &rolloutv1.ListReleaseChannelModelGroupsResponse{
		ModelGroups: modelGroups,
	}, true)
	requireProtoValidation(t, &rolloutv1.ListReleaseChannelModelGroupsResponse{
		Cursor: strings.Repeat("c", 8192),
	}, false)
	requireProtoValidation(t, &rolloutv1.ListReleaseChannelModelGroupsResponse{
		Cursor: strings.Repeat("c", 8193),
	}, true)
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
			name: "oversized manufacturer is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Models: []*rolloutv1.ReleaseChannelScopeModelCount{{
					Manufacturer: strings.Repeat("m", 256),
					Model:        "S21",
				}},
				ModelCount: 1,
			},
			wantErr: true,
		},
		{
			name: "oversized model is rejected",
			response: &rolloutv1.PreviewReleaseChannelScopeResponse{
				Models: []*rolloutv1.ReleaseChannelScopeModelCount{{
					Manufacturer: "Bitmain",
					Model:        strings.Repeat("m", 256),
				}},
				ModelCount: 1,
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

func TestDeploymentProvenanceResponseDescriptors(t *testing.T) {
	t.Parallel()

	for _, test := range deploymentProvenanceValidationCases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			items := test.response("").ProtoReflect().Descriptor().Fields().ByName(test.collectionField)
			require.NotNil(t, items)
			require.True(t, items.IsList())

			provenance := items.Message().Fields().ByName("last_deployed_firmware_checksum")
			require.NotNil(t, provenance)
			require.Equal(t, test.fieldNumber, provenance.Number())
			require.Equal(t, protoreflect.StringKind, provenance.Kind())
		})
	}
}

func TestDeploymentProvenanceResponseValidation(t *testing.T) {
	t.Parallel()

	for _, test := range deploymentProvenanceValidationCases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requireProtoValidation(t, test.response(""), false)
			requireProtoValidation(t, test.response(testChecksum), false)
			requireProtoValidation(t, test.response(strings.ToUpper(testChecksum)), true)
			requireProtoValidation(t, test.response(testChecksum[:63]), true)
			requireProtoValidation(t, test.response("file-1"), true)
		})
	}
}

func TestRolloutDetailsArePagedSeparately(t *testing.T) {
	t.Parallel()

	rolloutFields := (&rolloutv1.Rollout{}).ProtoReflect().Descriptor().Fields()
	require.Nil(t, rolloutFields.ByName("devices"))
	require.NotNil(t, rolloutFields.ByName("device_count"))

	modelGroup := (&rolloutv1.ReleaseChannelModelGroup{}).ProtoReflect().Descriptor()
	modelGroupFields := modelGroup.Fields()
	require.Nil(t, modelGroupFields.ByName("miners"))
	require.NotNil(t, modelGroupFields.ByName("miner_count"))

	channelSummary := (&rolloutv1.ReleaseChannelSummary{}).ProtoReflect().Descriptor()
	require.Nil(t, channelSummary.Fields().ByName("scope"))
	require.Nil(t, channelSummary.Fields().ByName("model_groups"))
	require.NotNil(t, channelSummary.Fields().ByName("model_group_count"))

	channel := (&rolloutv1.ReleaseChannel{}).ProtoReflect().Descriptor()
	require.Nil(t, channel.Fields().ByName("model_groups"))
	require.NotNil(t, channel.Fields().ByName("model_group_count"))

	methods := rolloutv1.File_rollout_v1_rollout_proto.
		Services().
		ByName(protoreflect.Name("RolloutService")).
		Methods()
	listReleaseChannels := methods.ByName("ListReleaseChannels")
	require.NotNil(t, listReleaseChannels)
	channelsField := listReleaseChannels.Output().Fields().ByName("channels")
	require.NotNil(t, channelsField)
	require.Equal(t, channelSummary.FullName(), channelsField.Message().FullName())

	listReleaseChannelModelGroups := methods.ByName("ListReleaseChannelModelGroups")
	require.NotNil(t, listReleaseChannelModelGroups)
	modelGroupsField := listReleaseChannelModelGroups.Output().Fields().ByName("model_groups")
	require.NotNil(t, modelGroupsField)
	require.True(t, modelGroupsField.IsList())
	require.Equal(t, modelGroup.FullName(), modelGroupsField.Message().FullName())
	require.NotNil(t, listReleaseChannelModelGroups.Output().Fields().ByName("cursor"))

	require.NotNil(t, methods.ByName("ListReleaseChannelMiners"))
	require.NotNil(t, methods.ByName("ListRolloutDevices"))
}

func TestListReleaseChannelMembershipConflictsDescriptor(t *testing.T) {
	t.Parallel()

	method := rolloutv1.File_rollout_v1_rollout_proto.
		Services().
		ByName(protoreflect.Name("RolloutService")).
		Methods().
		ByName("ListReleaseChannelMembershipConflicts")
	require.NotNil(t, method)

	request := (&rolloutv1.ListReleaseChannelMembershipConflictsRequest{}).ProtoReflect().Descriptor()
	response := (&rolloutv1.ListReleaseChannelMembershipConflictsResponse{}).ProtoReflect().Descriptor()
	conflict := (&rolloutv1.ReleaseChannelMembershipConflict{}).ProtoReflect().Descriptor()

	require.Equal(t, request.FullName(), method.Input().FullName())
	require.Equal(t, response.FullName(), method.Output().FullName())
	require.NotNil(t, request.Fields().ByName("channel_id"))
	require.NotNil(t, request.Fields().ByName("page_size"))
	require.NotNil(t, request.Fields().ByName("cursor"))

	conflictsField := response.Fields().ByName("conflicts")
	require.NotNil(t, conflictsField)
	require.True(t, conflictsField.IsList())
	require.Equal(t, conflict.FullName(), conflictsField.Message().FullName())
	require.NotNil(t, response.Fields().ByName("cursor"))

	specificityField := conflict.Fields().ByName("selector_specificity")
	require.NotNil(t, specificityField)
	require.Equal(t, protoreflect.FullName("rollout.v1.ReleaseChannelSelectorSpecificity"), specificityField.Enum().FullName())

	resolutionField := conflict.Fields().ByName("resolution")
	require.NotNil(t, resolutionField)
	resolutionEnum := resolutionField.Enum()
	require.Equal(t, protoreflect.FullName("rollout.v1.ReleaseChannelConflictResolution"), resolutionEnum.FullName())
	require.NotNil(t, resolutionEnum.Values().ByName("RELEASE_CHANNEL_CONFLICT_RESOLUTION_UNSPECIFIED"))
	require.NotNil(t, resolutionEnum.Values().ByName("RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER"))
	require.NotNil(t, resolutionEnum.Values().ByName("RELEASE_CHANNEL_CONFLICT_RESOLUTION_LOSER"))
	require.NotNil(t, resolutionEnum.Values().ByName("RELEASE_CHANNEL_CONFLICT_RESOLUTION_EXCLUDED_TIE"))

	options, ok := method.Options().(*descriptorpb.MethodOptions)
	require.True(t, ok)
	require.Equal(t, descriptorpb.MethodOptions_NO_SIDE_EFFECTS, options.GetIdempotencyLevel())
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
				return &rolloutv1.ReleaseChannelMembershipConflict{
					Manufacturer:        manufacturer,
					Model:               model,
					SelectorSpecificity: rolloutv1.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_MINER,
					Resolution:          rolloutv1.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_WINNER,
				}
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

func TestRolloutDeviceLastErrorValidation(t *testing.T) {
	t.Parallel()

	device := queuedDevice()
	device.LastError = strings.Repeat("e", 2048)
	requireProtoValidation(t, device, false)
	device.LastError = strings.Repeat("e", 2049)
	requireProtoValidation(t, device, true)
}

func TestRolloutDevicePhaseValidation(t *testing.T) {
	t.Parallel()

	requireProtoValidation(t, queuedDevice(), false)
	requireProtoValidation(t, &rolloutv1.RolloutDevice{}, true)
	requireProtoValidation(t, &rolloutv1.RolloutDevice{Phase: rolloutv1.RolloutDevicePhase(99)}, true)
}

// --- Delegated control, revisions, skips, events ---

func delegatedBehavior() *rolloutv1.RolloutBehavior {
	return &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_DELEGATED}
}

// delegatedRollout returns an ACTIVE DELEGATED rollout with one QUEUED target,
// waiting for its controller.
func delegatedRollout() *rolloutv1.Rollout {
	rollout := activeRollout()
	rollout.Behavior = delegatedBehavior()
	rollout.State = rolloutv1.RolloutState_ROLLOUT_STATE_WAITING_FOR_CONTROLLER
	rollout.DeviceCount = 1
	rollout.DeviceCounts = &rolloutv1.RolloutDeviceCounts{Queued: 1}
	rollout.Revision = 1
	return rollout
}

func TestDelegatedBehaviorValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		behavior *rolloutv1.RolloutBehavior
		wantErr  bool
	}{
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

func TestDelegatedRolloutStateValidation(t *testing.T) {
	t.Parallel()

	inProgress := delegatedRollout()
	inProgress.State = rolloutv1.RolloutState_ROLLOUT_STATE_IN_PROGRESS
	inProgress.DeviceCounts = &rolloutv1.RolloutDeviceCounts{InProgress: 1}

	paused := delegatedRollout()
	paused.State = rolloutv1.RolloutState_ROLLOUT_STATE_PAUSED
	paused.PausedAt = timestamppb.Now()

	gated := delegatedRollout()
	gated.State = rolloutv1.RolloutState_ROLLOUT_STATE_PAUSED_AT_PILOT_GATE
	gated.Stage = rolloutv1.RolloutStage_ROLLOUT_STAGE_AWAITING_REVIEW

	batched := delegatedRollout()
	batched.BatchCount = 1
	batched.Stage = rolloutv1.RolloutStage_ROLLOUT_STAGE_BATCH
	batched.State = rolloutv1.RolloutState_ROLLOUT_STATE_IN_PROGRESS

	notDelegatedWaiting := activeRollout()
	notDelegatedWaiting.State = rolloutv1.RolloutState_ROLLOUT_STATE_WAITING_FOR_CONTROLLER

	completedWithSkips := finishedRollout(rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED)
	completedWithSkips.Behavior = delegatedBehavior()
	completedWithSkips.DeviceCount = 3
	completedWithSkips.DeviceCounts = &rolloutv1.RolloutDeviceCounts{Done: 2, Skipped: 1}

	tests := []struct {
		name    string
		rollout *rolloutv1.Rollout
		wantErr bool
	}{
		{name: "delegated waiting for controller with queued targets", rollout: delegatedRollout()},
		{name: "delegated in progress while updates are in flight", rollout: inProgress},
		{name: "delegated paused by an operator", rollout: paused},
		{name: "completed delegated rollout may contain skipped targets", rollout: completedWithSkips},
		{name: "delegated rollouts never hold at a review gate", rollout: gated, wantErr: true},
		{name: "delegated rollouts have no batches", rollout: batched, wantErr: true},
		{name: "only delegated rollouts wait for a controller", rollout: notDelegatedWaiting, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			requireProtoValidation(t, test.rollout, test.wantErr)
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

func TestConditionalLifecycleRequestsCarryExpectedRevision(t *testing.T) {
	t.Parallel()

	for _, message := range []proto.Message{
		&rolloutv1.ContinueRolloutRequest{},
		&rolloutv1.PauseRolloutRequest{},
		&rolloutv1.ResumeRolloutRequest{},
		&rolloutv1.CancelRolloutRequest{},
		&rolloutv1.RetryFailedRolloutDevicesRequest{},
		&rolloutv1.RollbackReleaseChannelFirmwareRequest{},
		&rolloutv1.AdvanceRolloutRequest{},
		&rolloutv1.SkipRolloutDevicesRequest{},
		&rolloutv1.CompleteRolloutRequest{},
	} {
		fields := message.ProtoReflect().Descriptor().Fields()
		require.NotNil(t, fields.ByName("expected_revision"), "%s", message.ProtoReflect().Descriptor().FullName())
	}
	for _, message := range []proto.Message{
		&rolloutv1.ContinueRolloutRequest{},
		&rolloutv1.CancelRolloutRequest{},
		&rolloutv1.RollbackReleaseChannelFirmwareRequest{},
		&rolloutv1.AdvanceRolloutRequest{},
		&rolloutv1.SkipRolloutDevicesRequest{},
		&rolloutv1.CompleteRolloutRequest{},
	} {
		fields := message.ProtoReflect().Descriptor().Fields()
		require.NotNil(t, fields.ByName("note"), "%s", message.ProtoReflect().Descriptor().FullName())
	}

	rolloutFields := (&rolloutv1.Rollout{}).ProtoReflect().Descriptor().Fields()
	for _, name := range []string{"revision", "updated_at", "started_by", "last_action_by"} {
		require.NotNil(t, rolloutFields.ByName(protoreflect.Name(name)), name)
	}
	require.NotNil(t, (&rolloutv1.ListRolloutsRequest{}).ProtoReflect().Descriptor().Fields().ByName("updated_after"))
	require.NotNil(t, (&rolloutv1.ApplyReleaseChannelFirmwareRequest{}).ProtoReflect().Descriptor().Fields().ByName("behavior_override"))

	methods := rolloutv1.File_rollout_v1_rollout_proto.
		Services().
		ByName(protoreflect.Name("RolloutService")).
		Methods()
	for _, name := range []string{"AdvanceRollout", "SkipRolloutDevices", "CompleteRollout", "ListRolloutEvents", "PreviewReleaseChannelFirmware"} {
		require.NotNil(t, methods.ByName(protoreflect.Name(name)), name)
	}
}

func TestRolloutEventsValidation(t *testing.T) {
	t.Parallel()

	requireProtoValidation(t, &rolloutv1.ListRolloutEventsRequest{RolloutId: 1, PageSize: 1000}, false)
	requireProtoValidation(t, &rolloutv1.ListRolloutEventsRequest{PageSize: 1001}, true)

	event := &rolloutv1.RolloutEvent{
		Id: 1, RolloutId: 1, ChannelId: 1,
		Type:              rolloutv1.RolloutEventType_ROLLOUT_EVENT_TYPE_ADVANCED,
		OccurredAt:        timestamppb.Now(),
		Actor:             &rolloutv1.RolloutActor{Type: rolloutv1.RolloutActorType_ROLLOUT_ACTOR_TYPE_API_KEY, Id: 4, Name: "regression-bot"},
		RolloutRevision:   3,
		Note:              "batch 2 passed",
		DeviceIdentifiers: []string{"miner-1"},
	}
	requireProtoValidation(t, &rolloutv1.ListRolloutEventsResponse{Events: []*rolloutv1.RolloutEvent{event}, Cursor: "c1"}, false)
	requireProtoValidation(t, &rolloutv1.RolloutEvent{Id: 1, RolloutId: 1}, true)
	// Audit metadata is mandatory: when, by whom, and against which revision.
	for name, mutate := range map[string]func(*rolloutv1.RolloutEvent){
		"missing occurred_at":    func(e *rolloutv1.RolloutEvent) { e.OccurredAt = nil },
		"missing actor":          func(e *rolloutv1.RolloutEvent) { e.Actor = nil },
		"unspecified actor type": func(e *rolloutv1.RolloutEvent) { e.Actor = &rolloutv1.RolloutActor{Id: 4, Name: "x"} },
		"zero rollout revision":  func(e *rolloutv1.RolloutEvent) { e.RolloutRevision = 0 },
		"unspecified event type": func(e *rolloutv1.RolloutEvent) { e.Type = rolloutv1.RolloutEventType_ROLLOUT_EVENT_TYPE_UNSPECIFIED },
	} {
		broken, ok := proto.Clone(event).(*rolloutv1.RolloutEvent)
		require.True(t, ok)
		mutate(broken)
		require.Error(t, protovalidate.Validate(broken), name)
	}

	events := make([]*rolloutv1.RolloutEvent, 1001)
	for i := range events {
		events[i] = event
	}
	requireProtoValidation(t, &rolloutv1.ListRolloutEventsResponse{Events: events}, true)

	requireProtoValidation(t, &rolloutv1.RolloutErrorInfo{
		Reason: rolloutv1.RolloutErrorReason_ROLLOUT_ERROR_REASON_STALE_REVISION, CurrentRevision: 8,
	}, false)
	requireProtoValidation(t, &rolloutv1.RolloutErrorInfo{}, true)
}

func TestPreviewReleaseChannelFirmwareRequestValidation(t *testing.T) {
	t.Parallel()

	assignment := &rolloutv1.FirmwareAssignment{Manufacturer: "Bitmain", Model: "S21", FirmwareFileId: "file-1"}
	requireProtoValidation(t, &rolloutv1.PreviewReleaseChannelFirmwareRequest{
		ChannelId: 1, Assignments: []*rolloutv1.FirmwareAssignment{assignment}, BehaviorOverride: delegatedBehavior(),
	}, false)
	requireProtoValidation(t, &rolloutv1.PreviewReleaseChannelFirmwareRequest{ChannelId: 1}, true)
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

func TestArtifactIdentityValidation(t *testing.T) {
	t.Parallel()

	assigned := func(mutate func(*rolloutv1.ReleaseChannelModelGroup)) *rolloutv1.ReleaseChannelModelGroup {
		group := &rolloutv1.ReleaseChannelModelGroup{
			FirmwareFileId:             "file-1",
			FirmwareChecksum:           testChecksum,
			FirmwareAvailable:          true,
			FirmwareTargetManufacturer: "Bitmain",
			FirmwareTargetModel:        "S21",
			FirmwareVersion:            "2.0",
			AssignmentGeneration:       1,
		}
		mutate(group)
		return group
	}
	tests := []struct {
		name    string
		group   *rolloutv1.ReleaseChannelModelGroup
		wantErr bool
	}{
		{name: "assigned and uploaded", group: assigned(func(*rolloutv1.ReleaseChannelModelGroup) {})},
		{
			name: "assigned but the artifact is not uploaded: no file id, not available",
			group: assigned(func(g *rolloutv1.ReleaseChannelModelGroup) {
				g.FirmwareFileId = ""
				g.FirmwareAvailable = false
			}),
		},
		{
			name:    "a file id without availability is rejected",
			group:   assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareAvailable = false }),
			wantErr: true,
		},
		{
			name:    "availability without a file id is rejected",
			group:   assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareFileId = "" }),
			wantErr: true,
		},
		{
			name:    "an assignment without a checksum is rejected",
			group:   assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareChecksum = "" }),
			wantErr: true,
		},
		{
			name:    "a malformed checksum is rejected",
			group:   assigned(func(g *rolloutv1.ReleaseChannelModelGroup) { g.FirmwareChecksum = "sha256:" + testChecksum }),
			wantErr: true,
		},
		{
			name: "an unassigned pair carries no artifact fields",
			group: &rolloutv1.ReleaseChannelModelGroup{
				FirmwareChecksum: testChecksum,
			},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			requireProtoValidation(t, test.group, test.wantErr)
		})
	}

	// The contract identifies artifacts by checksum everywhere a file was
	// referenced before, and no longer tracks retry chains.
	rolloutFields := (&rolloutv1.Rollout{}).ProtoReflect().Descriptor().Fields()
	require.NotNil(t, rolloutFields.ByName("firmware_checksum"))
	require.NotNil(t, rolloutFields.ByName("previous_firmware_checksum"))
	require.Nil(t, rolloutFields.ByName("previous_firmware_file_id"))
	require.Nil(t, rolloutFields.ByName("retry_of_rollout_id"))
	require.Nil(t, rolloutFields.ByName("successor_rollout_id"))
	require.NotNil(t, (&rolloutv1.ReleaseChannelFirmwarePlan{}).ProtoReflect().Descriptor().Fields().ByName("firmware_checksum"))
	reasons := rolloutv1.RolloutErrorReason(0).Descriptor().Values()
	require.NotNil(t, reasons.ByName("ROLLOUT_ERROR_REASON_ARTIFACT_MISSING"))
	require.Nil(t, reasons.ByName("ROLLOUT_ERROR_REASON_NOT_LATEST"))
	require.Nil(t, reasons.ByName("ROLLOUT_ERROR_REASON_ARTIFACT_PROTECTED"))

	// The events feed cursor is never empty.
	requireProtoValidation(t, &rolloutv1.ListRolloutEventsResponse{Cursor: ""}, true)
	requireProtoValidation(t, &rolloutv1.ListRolloutEventsResponse{Cursor: "c1"}, false)
}
