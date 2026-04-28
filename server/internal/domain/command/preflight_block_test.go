package command

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// recordingActivityStore records inserts; other ActivityStore methods are not
// used by these tests. Insert honours ctx cancellation and snapshots ctx.Err()
// at insert time (callers typically defer-cancel the audit ctx right after the
// write returns).
type recordingActivityStore struct {
	inserts       []*activitymodels.Event
	failErr       error
	insertCtxErrs []error
}

func (s *recordingActivityStore) Insert(ctx context.Context, event *activitymodels.Event) error {
	if s.failErr != nil {
		return s.failErr
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("recording activity store insert: %w", err)
	}
	s.insertCtxErrs = append(s.insertCtxErrs, ctx.Err())
	clone := *event
	s.inserts = append(s.inserts, &clone)
	return nil
}

func (s *recordingActivityStore) List(context.Context, activitymodels.Filter) ([]activitymodels.Entry, error) {
	panic("not used in preflight_block_test")
}
func (s *recordingActivityStore) Count(context.Context, activitymodels.Filter) (int64, error) {
	panic("not used in preflight_block_test")
}
func (s *recordingActivityStore) GetDistinctUsers(context.Context, int64) ([]activitymodels.UserInfo, error) {
	panic("not used in preflight_block_test")
}
func (s *recordingActivityStore) GetDistinctEventTypes(context.Context, int64) ([]activitymodels.EventTypeInfo, error) {
	panic("not used in preflight_block_test")
}
func (s *recordingActivityStore) GetDistinctScopeTypes(context.Context, int64) ([]string, error) {
	panic("not used in preflight_block_test")
}

// newPreflightTestService leaves queue/DB nil so tests prove blocked paths
// short-circuit before enqueue.
func newPreflightTestService(t *testing.T, filter CommandFilter) (*Service, *recordingActivityStore) {
	t.Helper()
	store := &recordingActivityStore{}
	svc := &Service{
		config:           &Config{},
		executionService: &ExecutionService{queueProcessorRunning: true},
		activitySvc:      activity.NewService(store),
		filters:          []CommandFilter{filter},
	}
	return svc, store
}

func manualSessionCtx(orgID int64) context.Context {
	return authn.SetInfo(context.Background(), &session.Info{
		SessionID:      "manual-test",
		UserID:         42,
		OrganizationID: orgID,
		ExternalUserID: "user-1",
		Username:       "test-user",
		// Actor empty, Source zero → external manual caller.
	})
}

func schedulerSessionCtx(orgID int64) context.Context {
	return authn.SetInfo(context.Background(), &session.Info{
		SessionID:      "scheduler",
		UserID:         42,
		OrganizationID: orgID,
		ExternalUserID: "scheduler",
		Username:       "scheduler",
		Actor:          session.ActorScheduler,
		Source:         session.Source{ScheduleID: 99, SchedulePriority: 5},
	})
}

func includeSelector(ids ...string) *pb.DeviceSelector {
	return &pb.DeviceSelector{
		SelectionType: &pb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: ids},
		},
	}
}

func findActivity(t *testing.T, store *recordingActivityStore, eventType string) *activitymodels.Event {
	t.Helper()
	var found *activitymodels.Event
	for _, ev := range store.inserts {
		if ev.Type == eventType {
			require.Nil(t, found, "expected exactly one %q activity, found another", eventType)
			found = ev
		}
	}
	require.NotNil(t, found, "expected one %q activity, got %d events of other types", eventType, len(store.inserts))
	return found
}

// --- Manual-origin block path: HIGH finding ---

func TestProcessCommand_ManualPartialSkip_Blocks(t *testing.T) {
	svc, store := newPreflightTestService(t, newFakeFilter("test_block", "miner-1"))
	svc.resolveDeviceIDsOverride = func(_ context.Context, identifiers []string) ([]int64, error) {
		assert.Equal(t, []string{"miner-1", "miner-2", "miner-3"}, identifiers)
		return []int64{101, 102, 103}, nil
	}

	_, err := svc.processCommand(manualSessionCtx(1), &Command{
		commandType:    commandtype.SetPowerTarget,
		deviceSelector: includeSelector("miner-1", "miner-2", "miner-3"),
	})

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError, got %T", err)
	require.Equal(t, connect.CodeFailedPrecondition, fleetErr.GRPCCode)

	ev := findActivity(t, store, "command_preflight_blocked")
	assert.Equal(t, activitymodels.CategoryDeviceCommand, ev.Category)
	assert.Equal(t, activitymodels.ResultFailure, ev.Result)
	assert.Equal(t, "set_power_target", ev.Metadata["command_type"])
	assert.Equal(t, 3, ev.Metadata["requested_count"])
	assert.Equal(t, 1, ev.Metadata["skipped_count"])
	assert.Equal(t, []string{"miner-1"}, ev.Metadata["skipped_identifiers"])
	assert.Equal(t, []string{"test_block"}, ev.Metadata["filters"])
}

// Mixed-stale selector: filter skips one stale ID, the other stays in kept,
// but neither resolves to a live device. Should be InvalidArgument, not a
// misleading FailedPrecondition.
func TestProcessCommand_ManualPartialSkipWithNoLiveDevices_ReturnsInvalidArgument(t *testing.T) {
	svc, store := newPreflightTestService(t, newFakeFilter("test_block", "stale-A"))
	svc.resolveDeviceIDsOverride = func(_ context.Context, identifiers []string) ([]int64, error) {
		assert.Equal(t, []string{"stale-A", "stale-B"}, identifiers)
		return nil, nil
	}

	_, err := svc.processCommand(manualSessionCtx(1), &Command{
		commandType:    commandtype.SetPowerTarget,
		deviceSelector: includeSelector("stale-A", "stale-B"),
	})

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError, got %T", err)
	require.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
	assert.Contains(t, err.Error(), "no devices matched selector")
	for _, ev := range store.inserts {
		assert.NotEqual(t, "command_preflight_blocked", ev.Type, "stale-mixed selector is not a preflight block")
	}
}

func TestProcessCommand_ManualFullSkip_Blocks(t *testing.T) {
	svc, store := newPreflightTestService(t, newFakeFilter("test_block", "miner-1", "miner-2"))
	svc.resolveDeviceIDsOverride = func(_ context.Context, identifiers []string) ([]int64, error) {
		assert.Equal(t, []string{"miner-1", "miner-2"}, identifiers)
		return []int64{101, 102}, nil
	}

	_, err := svc.processCommand(manualSessionCtx(1), &Command{
		commandType:    commandtype.SetPowerTarget,
		deviceSelector: includeSelector("miner-1", "miner-2"),
	})

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr))
	require.Equal(t, connect.CodeFailedPrecondition, fleetErr.GRPCCode)

	ev := findActivity(t, store, "command_preflight_blocked")
	assert.Equal(t, 2, ev.Metadata["requested_count"])
	assert.Equal(t, 2, ev.Metadata["skipped_count"])
	assert.Equal(t, []string{"miner-1", "miner-2"}, ev.Metadata["skipped_identifiers"])
}

func TestProcessCommand_ManualFullSkipWithNoLiveDevices_ReturnsInvalidArgument(t *testing.T) {
	svc, store := newPreflightTestService(t, newFakeFilter("test_block", "stale-miner"))
	svc.resolveDeviceIDsOverride = func(_ context.Context, identifiers []string) ([]int64, error) {
		assert.Equal(t, []string{"stale-miner"}, identifiers)
		return nil, nil
	}

	result, err := svc.processCommand(manualSessionCtx(1), &Command{
		commandType:    commandtype.SetPowerTarget,
		deviceSelector: includeSelector("stale-miner"),
	})

	require.Nil(t, result)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError, got %T", err)
	require.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
	assert.Contains(t, err.Error(), "no devices matched selector")
	for _, ev := range store.inserts {
		assert.NotEqual(t, "command_preflight_blocked", ev.Type, "zero-live-device selector is not a preflight block")
	}
}

// --- Scheduler-origin: block path must NOT fire ---

func TestProcessCommand_SchedulerFullSkip_NoBlockActivity(t *testing.T) {
	svc, store := newPreflightTestService(t, newFakeFilter("test_block", "miner-1", "miner-2"))

	result, err := svc.processCommand(schedulerSessionCtx(1), &Command{
		commandType:    commandtype.SetPowerTarget,
		deviceSelector: includeSelector("miner-1", "miner-2"),
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "", result.BatchIdentifier)
	assert.Equal(t, 0, result.DispatchedCount)
	require.Equal(t, 2, len(result.Skipped))
	assert.Equal(t, "miner-1", result.Skipped[0].DeviceIdentifier)
	assert.Equal(t, "test_block", result.Skipped[0].FilterName)
	assert.Equal(t, "excluded by test_block", result.Skipped[0].Reason)
	assert.Equal(t, "miner-2", result.Skipped[1].DeviceIdentifier)
	assert.Equal(t, "test_block", result.Skipped[1].FilterName)
	assert.Equal(t, "excluded by test_block", result.Skipped[1].Reason)
	for _, ev := range store.inserts {
		assert.NotEqual(t, "command_preflight_blocked", ev.Type, "scheduler must not trigger the block path")
	}
}

func TestProcessCommand_ExternalEmptySelector_ReturnsInvalidArgument(t *testing.T) {
	svc, store := newPreflightTestService(t, newFakeFilter("test_block"))

	result, err := svc.processCommand(manualSessionCtx(1), &Command{
		commandType:    commandtype.SetPowerTarget,
		deviceSelector: includeSelector(),
	})

	require.Nil(t, result)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError, got %T", err)
	require.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
	assert.Contains(t, err.Error(), "no devices matched selector")
	for _, ev := range store.inserts {
		assert.NotEqual(t, "command_preflight_blocked", ev.Type, "zero-target selector is not a preflight block")
	}
}

// --- Audit-failure path: must NOT degrade into a normal FailedPrecondition ---

func TestProcessCommand_ManualBlock_AuditFailure_ReturnsInternal(t *testing.T) {
	svc, store := newPreflightTestService(t, newFakeFilter("test_block", "miner-1"))
	svc.resolveDeviceIDsOverride = func(_ context.Context, _ []string) ([]int64, error) {
		return []int64{101, 102}, nil
	}
	store.failErr = errors.New("activity_log: connection refused")

	_, err := svc.processCommand(manualSessionCtx(1), &Command{
		commandType:    commandtype.SetPowerTarget,
		deviceSelector: includeSelector("miner-1", "miner-2"),
	})

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr))
	require.Equal(t, connect.CodeInternal, fleetErr.GRPCCode)
	assert.Contains(t, err.Error(), "logging preflight block")
}

// A request-ctx cancel at the deny-point must not suppress the audit row or
// degrade FailedPrecondition into Internal.
func TestProcessCommand_ManualBlock_AuditWritesOnCanceledRequestCtx(t *testing.T) {
	svc, store := newPreflightTestService(t, newFakeFilter("test_block", "miner-1"))
	svc.resolveDeviceIDsOverride = func(_ context.Context, _ []string) ([]int64, error) {
		return []int64{101, 102}, nil
	}

	reqCtx, cancel := context.WithCancel(manualSessionCtx(1))
	cancel()

	_, err := svc.processCommand(reqCtx, &Command{
		commandType:    commandtype.SetPowerTarget,
		deviceSelector: includeSelector("miner-1", "miner-2"),
	})

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr))
	require.Equal(t, connect.CodeFailedPrecondition, fleetErr.GRPCCode,
		"audit write on a bounded background ctx should not turn the deny into Internal")

	findActivity(t, store, "command_preflight_blocked")

	require.NotEmpty(t, store.insertCtxErrs)
	insertErr := store.insertCtxErrs[len(store.insertCtxErrs)-1]
	require.NoError(t, insertErr, "Insert must receive a live context, not the cancelled request ctx")
}

// The handler passes wrapper errors through; ErrorMappingInterceptor maps this
// FleetError.GRPCCode to the wire-level connect.Code.

func TestSetPowerTarget_ManualBlock_PropagatesFailedPrecondition(t *testing.T) {
	svc, _ := newPreflightTestService(t, newFakeFilter("test_block", "miner-1"))
	svc.resolveDeviceIDsOverride = func(_ context.Context, _ []string) ([]int64, error) {
		return []int64{101, 102}, nil
	}

	resp, err := svc.SetPowerTarget(
		manualSessionCtx(1),
		includeSelector("miner-1", "miner-2"),
		pb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY,
	)

	require.Nil(t, resp)
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError, got %T", err)
	require.Equal(t, connect.CodeFailedPrecondition, fleetErr.GRPCCode,
		"FleetError.GRPCCode is what ErrorMappingInterceptor uses to set the wire-level connect.Code")
}

// --- Skip metadata helper unit tests ---

func TestSkipMetadata_DeduplicatesFilterNames(t *testing.T) {
	skipped := []SkippedDevice{
		{DeviceIdentifier: "a", FilterName: "f1"},
		{DeviceIdentifier: "b", FilterName: "f2"},
		{DeviceIdentifier: "c", FilterName: "f1"}, // duplicate filter name
	}
	md := skipMetadata("set_power_target", 5, skipped)

	assert.Equal(t, "set_power_target", md["command_type"])
	assert.Equal(t, 5, md["requested_count"])
	assert.Equal(t, 3, md["skipped_count"])
	assert.Equal(t, []string{"a", "b", "c"}, md["skipped_identifiers"])
	// filters deduplicated and sorted
	assert.Equal(t, []string{"f1", "f2"}, md["filters"])
}
