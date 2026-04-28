package command

import (
	"context"
	"errors"
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

// recordingActivityStore implements just enough of the ActivityStore interface
// to record inserts. The other methods panic — every test in this file must
// take a path that only calls Insert.
type recordingActivityStore struct {
	inserts []*activitymodels.Event
	failErr error
}

func (s *recordingActivityStore) Insert(_ context.Context, event *activitymodels.Event) error {
	if s.failErr != nil {
		return s.failErr
	}
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

// blockingFilter excludes a fixed set of device identifiers. Drives the
// preflight chain into "skipped" states without needing the real
// ScheduleConflictFilter and its store dependencies.
type blockingFilter struct {
	exclude map[string]struct{}
}

func newBlockingFilter(exclude ...string) *blockingFilter {
	set := make(map[string]struct{}, len(exclude))
	for _, e := range exclude {
		set[e] = struct{}{}
	}
	return &blockingFilter{exclude: set}
}

func (f *blockingFilter) Name() string { return "test_block" }

func (f *blockingFilter) Apply(_ context.Context, in CommandFilterInput) (CommandFilterOutput, error) {
	var kept []string
	var skipped []SkippedDevice
	for _, id := range in.DeviceIdentifiers {
		if _, drop := f.exclude[id]; drop {
			skipped = append(skipped, SkippedDevice{DeviceIdentifier: id, FilterName: f.Name(), Reason: "test"})
			continue
		}
		kept = append(kept, id)
	}
	return CommandFilterOutput{Kept: kept, Skipped: skipped}, nil
}

// newPreflightTestService wires the minimum surface processCommand needs to
// reach (or NOT reach) the manual-block path. messageQueue/conn are
// intentionally nil — every test must short-circuit before they're touched.
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

// findActivity returns the single event with the given type, or fails.
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
	svc, store := newPreflightTestService(t, newBlockingFilter("miner-1"))

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

func TestProcessCommand_ManualFullSkip_Blocks(t *testing.T) {
	svc, store := newPreflightTestService(t, newBlockingFilter("miner-1", "miner-2"))

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

// --- Scheduler-origin: block path must NOT fire ---

func TestProcessCommand_SchedulerFullSkip_NoBlockActivity(t *testing.T) {
	svc, store := newPreflightTestService(t, newBlockingFilter("miner-1", "miner-2"))

	result, err := svc.processCommand(schedulerSessionCtx(1), &Command{
		commandType:    commandtype.SetPowerTarget,
		deviceSelector: includeSelector("miner-1", "miner-2"),
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "", result.BatchIdentifier)
	assert.Equal(t, 0, result.DispatchedCount)
	assert.Equal(t, 2, len(result.Skipped))
	for _, ev := range store.inserts {
		assert.NotEqual(t, "command_preflight_blocked", ev.Type, "scheduler must not trigger the block path")
	}
}

// --- Audit-failure path: must NOT degrade into a normal FailedPrecondition ---

func TestProcessCommand_ManualBlock_AuditFailure_ReturnsInternal(t *testing.T) {
	svc, store := newPreflightTestService(t, newBlockingFilter("miner-1"))
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

// --- Wrapper-level boundary check ---
//
// The Connect handler is a 3-line passthrough that returns the wrapper's
// error verbatim; ErrorMappingInterceptor.mapError translates FleetError →
// connect.Error using the FleetError.GRPCCode. Verifying the wrapper
// returns a FleetError with CodeFailedPrecondition is the meaningful
// boundary check at the unit test layer.

func TestSetPowerTarget_ManualBlock_PropagatesFailedPrecondition(t *testing.T) {
	svc, _ := newPreflightTestService(t, newBlockingFilter("miner-1"))

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
