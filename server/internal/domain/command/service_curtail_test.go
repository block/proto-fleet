package command

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// fakeMessageQueue is a queue.MessageQueue stub that captures Enqueue calls.
// Methods unused by Curtail/Uncurtail panic so a stray call surfaces clearly.
type fakeMessageQueue struct {
	enqueueCalls       int
	lastBatchUUID      string
	lastCommandType    commandtype.Type
	lastDeviceIDs      []int64
	lastPayload        interface{}
	enqueueReturnError error
}

func (f *fakeMessageQueue) Enqueue(_ context.Context, batchUUID string, ct commandtype.Type, deviceIDs []int64, payload interface{}) error {
	f.enqueueCalls++
	f.lastBatchUUID = batchUUID
	f.lastCommandType = ct
	f.lastDeviceIDs = append([]int64(nil), deviceIDs...)
	f.lastPayload = payload
	return f.enqueueReturnError
}

func (f *fakeMessageQueue) EnqueueMany(_ context.Context, batchUUID string, ct commandtype.Type, messages []queue.EnqueueMessage) error {
	f.enqueueCalls++
	f.lastBatchUUID = batchUUID
	f.lastCommandType = ct
	f.lastDeviceIDs = f.lastDeviceIDs[:0]
	for _, message := range messages {
		f.lastDeviceIDs = append(f.lastDeviceIDs, message.DeviceID)
		f.lastPayload = message.Payload
	}
	return f.enqueueReturnError
}

func (f *fakeMessageQueue) Dequeue(context.Context, int32) ([]queue.Message, error) {
	panic("Dequeue not used")
}
func (f *fakeMessageQueue) MarkSuccess(context.Context, int64) error {
	panic("MarkSuccess not used")
}
func (f *fakeMessageQueue) MarkFailed(context.Context, int64, string) error {
	panic("MarkFailed not used")
}
func (f *fakeMessageQueue) MarkPermanentlyFailed(context.Context, int64, string) error {
	panic("MarkPermanentlyFailed not used")
}
func (f *fakeMessageQueue) IsBatchFinished(context.Context, string) (bool, error) {
	return true, nil
}
func (f *fakeMessageQueue) IsBatchProcessing(context.Context, string) (bool, error) {
	return false, nil
}
func (f *fakeMessageQueue) MaxFailureRetries() int32 { return 0 }

// newCurtailDispatchService builds a Service wired against in-memory test
// doubles: stub DB-batch-save, no-op status routine, and a recording queue.
func newCurtailDispatchService(t *testing.T) (*Service, *fakeMessageQueue) {
	svc, q, _ := newCurtailDispatchServiceWithActivityStore(t)
	return svc, q
}

func newCurtailDispatchServiceWithActivityStore(t *testing.T) (*Service, *fakeMessageQueue, *recordingActivityStore) {
	t.Helper()
	q := &fakeMessageQueue{}
	store := &recordingActivityStore{}
	svc := &Service{
		config:           &Config{},
		executionService: &ExecutionService{run: newExecutionRun(context.Background())},
		messageQueue:     q,
		activitySvc:      activity.NewService(store),
	}
	svc.resolveDeviceIDsOverride = func(_ context.Context, ids []string) ([]int64, error) {
		out := make([]int64, len(ids))
		for i := range ids {
			// #nosec G115 -- test-only fake mapping.
			out[i] = int64(100 + i)
		}
		return out, nil
	}
	svc.saveCommandBatchLogOverride = func(context.Context, int64, int64, *Command, []byte, int) (string, error) {
		return "test-batch-uuid", nil
	}
	svc.startStatusUpdateRoutineOverride = func(string, onFinishedCallbackFunc) {}
	return svc, q, store
}

func TestCurtail_HappyPath_QueueReceivesCommand(t *testing.T) {
	svc, q := newCurtailDispatchService(t)

	selector := &pb.DeviceSelector{
		SelectionType: &pb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{"miner-1", "miner-2"}},
		},
	}

	result, err := svc.Curtail(manualSessionCtx(1), selector, sdk.CurtailLevelFull)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "test-batch-uuid", result.BatchIdentifier)
	assert.Equal(t, 2, result.DispatchedCount)

	require.Equal(t, 1, q.enqueueCalls)
	assert.Equal(t, commandtype.Curtail, q.lastCommandType)
	assert.Equal(t, []int64{100, 101}, q.lastDeviceIDs)

	payload, ok := q.lastPayload.(dto.CurtailPayload)
	require.True(t, ok, "payload should be CurtailPayload, got %T", q.lastPayload)
	assert.Equal(t, int32(sdk.CurtailLevelFull), payload.Level)

	// JSON serialization round-trip preserves the level.
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	var roundtrip dto.CurtailPayload
	require.NoError(t, json.Unmarshal(b, &roundtrip))
	assert.Equal(t, payload.Level, roundtrip.Level)
}

func TestUncurtail_HappyPath_QueueReceivesCommand(t *testing.T) {
	svc, q := newCurtailDispatchService(t)

	selector := &pb.DeviceSelector{
		SelectionType: &pb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{"miner-1"}},
		},
	}

	result, err := svc.Uncurtail(manualSessionCtx(1), selector)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.DispatchedCount)

	require.Equal(t, 1, q.enqueueCalls)
	assert.Equal(t, commandtype.Uncurtail, q.lastCommandType)
	// Uncurtail has no payload — should be nil.
	assert.Nil(t, q.lastPayload)
}

func TestUncurtail_DispatchedIdentifiersOnlyIncludeResolvedDevices(t *testing.T) {
	svc, q := newCurtailDispatchService(t)
	svc.resolveDevicesOverride = func(_ context.Context, ids []string) ([]resolvedDevice, error) {
		assert.Equal(t, []string{"miner-1", "miner-2"}, ids)
		return []resolvedDevice{{id: 202, identifier: "miner-2"}}, nil
	}

	selector := &pb.DeviceSelector{
		SelectionType: &pb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{"miner-1", "miner-2"}},
		},
	}

	result, err := svc.Uncurtail(manualSessionCtx(1), selector)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.DispatchedCount)
	assert.Equal(t, []string{"miner-2"}, result.DispatchedDeviceIdentifiers)
	assert.Equal(t, []int64{202}, q.lastDeviceIDs)
}

func TestCurtail_ManualDispatchLogsCommandActivity(t *testing.T) {
	svc, _, store := newCurtailDispatchServiceWithActivityStore(t)

	_, err := svc.Curtail(manualSessionCtx(1), includeSelector("miner-1"), sdk.CurtailLevelFull)
	require.NoError(t, err)

	ev := findActivity(t, store, "curtail")
	assert.Equal(t, "Curtail", ev.Description)
	require.NotNil(t, ev.ScopeCount)
	assert.Equal(t, 1, *ev.ScopeCount)
}

func TestCurtail_SuppressedActivityContextSkipsCommandActivity(t *testing.T) {
	svc, _, store := newCurtailDispatchServiceWithActivityStore(t)
	statusRoutineCalls := 0
	var callback onFinishedCallbackFunc
	svc.startStatusUpdateRoutineOverride = func(_ string, cb onFinishedCallbackFunc) {
		statusRoutineCalls++
		callback = cb
	}

	_, err := svc.Curtail(WithCommandActivitySuppressed(manualSessionCtx(1)), includeSelector("miner-1"), sdk.CurtailLevelFull)
	require.NoError(t, err)

	assert.Empty(t, store.inserts, "suppressed curtailment-owned dispatch must not write command activity rows")
	assert.Equal(t, 1, statusRoutineCalls, "batch tracking must still run when activity is suppressed")
	assert.Nil(t, callback, "activity completion finalizer must be omitted when activity is suppressed")
}
