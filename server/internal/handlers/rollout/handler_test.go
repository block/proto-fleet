package rollout

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// fakeService records the arguments of the last call and returns canned
// views, so the handler's translation can be checked without a database.
type fakeService struct {
	channel *rollout.Channel
	rollout *rollout.Rollout
	preview *rollout.ScopePreview

	lastOrgID    int64
	lastUserID   int64
	lastSpec     rollout.ChannelSpec
	lastScope    rollout.Scope
	lastAssigned []rollout.Assignment
	lastID       int64
	lastFilter   rollout.RolloutFilter
	lastModel    string
	lastPage     int32
	lastCursor   string
}

func (f *fakeService) ListChannels(_ context.Context, orgID int64) ([]rollout.Channel, error) {
	f.lastOrgID = orgID
	return []rollout.Channel{*f.channel}, nil
}

func (f *fakeService) GetChannel(_ context.Context, orgID, channelID int64) (*rollout.Channel, error) {
	f.lastOrgID, f.lastID = orgID, channelID
	return f.channel, nil
}

func (f *fakeService) ListChannelMiners(_ context.Context, orgID, channelID int64, model string, pageSize int32, cursor string) ([]rollout.ChannelMiner, string, error) {
	f.lastOrgID, f.lastID, f.lastModel, f.lastPage, f.lastCursor = orgID, channelID, model, pageSize, cursor
	return []rollout.ChannelMiner{{DeviceID: 500, DeviceIdentifier: "miner-0", Model: "Rig", FirmwareVersion: "1.0.0", Conflicted: true}}, "more-miners", nil
}

func (f *fakeService) CreateChannel(_ context.Context, orgID, userID int64, spec rollout.ChannelSpec) (*rollout.Channel, error) {
	f.lastOrgID, f.lastUserID, f.lastSpec = orgID, userID, spec
	return f.channel, nil
}

func (f *fakeService) UpdateChannel(_ context.Context, orgID, channelID int64, spec rollout.ChannelSpec) (*rollout.Channel, error) {
	f.lastOrgID, f.lastID, f.lastSpec = orgID, channelID, spec
	return f.channel, nil
}

func (f *fakeService) DeleteChannel(_ context.Context, orgID, channelID int64) error {
	f.lastOrgID, f.lastID = orgID, channelID
	return nil
}

func (f *fakeService) PreviewScope(_ context.Context, orgID int64, scope rollout.Scope, excludeChannelID int64) (*rollout.ScopePreview, error) {
	f.lastOrgID, f.lastScope, f.lastID = orgID, scope, excludeChannelID
	return f.preview, nil
}

func (f *fakeService) ApplyFirmware(_ context.Context, orgID, userID, channelID int64, assignments []rollout.Assignment) ([]rollout.Rollout, error) {
	f.lastOrgID, f.lastUserID, f.lastID, f.lastAssigned = orgID, userID, channelID, assignments
	return []rollout.Rollout{*f.rollout}, nil
}

func (f *fakeService) RollbackFirmware(_ context.Context, orgID, userID, rolloutID int64) (int64, []rollout.Rollout, error) {
	f.lastOrgID, f.lastUserID, f.lastID = orgID, userID, rolloutID
	return f.channel.ID, []rollout.Rollout{*f.rollout}, nil
}

func (f *fakeService) ListRollouts(_ context.Context, orgID int64, filter rollout.RolloutFilter) ([]rollout.Rollout, string, error) {
	f.lastOrgID, f.lastFilter = orgID, filter
	return []rollout.Rollout{*f.rollout}, "next-cursor", nil
}

func (f *fakeService) GetRollout(_ context.Context, orgID, rolloutID int64) (*rollout.Rollout, error) {
	f.lastOrgID, f.lastID = orgID, rolloutID
	return f.rollout, nil
}

func (f *fakeService) ListRolloutDevices(_ context.Context, orgID, rolloutID int64, pageSize int32, cursor string) ([]rollout.RolloutDevice, string, error) {
	f.lastOrgID, f.lastID, f.lastPage, f.lastCursor = orgID, rolloutID, pageSize, cursor
	return f.rollout.Devices, "more-devices", nil
}

func (f *fakeService) ContinueRollout(_ context.Context, orgID, rolloutID int64) (*rollout.Rollout, error) {
	f.lastOrgID, f.lastID = orgID, rolloutID
	return f.rollout, nil
}

func (f *fakeService) PauseRollout(_ context.Context, orgID, rolloutID int64) (*rollout.Rollout, error) {
	f.lastOrgID, f.lastID = orgID, rolloutID
	return f.rollout, nil
}

func (f *fakeService) ResumeRollout(_ context.Context, orgID, rolloutID int64) (*rollout.Rollout, error) {
	f.lastOrgID, f.lastID = orgID, rolloutID
	return f.rollout, nil
}

func (f *fakeService) CancelRollout(_ context.Context, orgID, rolloutID int64) (*rollout.Rollout, *rollout.Channel, error) {
	f.lastOrgID, f.lastID = orgID, rolloutID
	return f.rollout, f.channel, nil
}

func (f *fakeService) RetryFailedDevices(_ context.Context, orgID, userID, rolloutID int64) (*rollout.Rollout, error) {
	f.lastOrgID, f.lastUserID, f.lastID = orgID, userID, rolloutID
	return f.rollout, nil
}

func ctxWithPermissions(t *testing.T, permissions ...string) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), &session.Info{OrganizationID: 7, UserID: 42})
	eff := authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  permissions,
	}})
	return middleware.WithEffectivePermissions(ctx, eff)
}

// Every RolloutService RPC must reject sessions without the firmware-update
// permission before any service work runs.
func TestHandlerGatesEveryRPC(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil)
	ctx := ctxWithPermissions(t) // no permissions

	cases := []struct {
		name string
		call func() error
	}{
		{"ListReleaseChannels", func() error {
			_, err := h.ListReleaseChannels(ctx, connect.NewRequest(&pb.ListReleaseChannelsRequest{}))
			return err
		}},
		{"GetReleaseChannel", func() error {
			_, err := h.GetReleaseChannel(ctx, connect.NewRequest(&pb.GetReleaseChannelRequest{}))
			return err
		}},
		{"ListReleaseChannelMiners", func() error {
			_, err := h.ListReleaseChannelMiners(ctx, connect.NewRequest(&pb.ListReleaseChannelMinersRequest{}))
			return err
		}},
		{"CreateReleaseChannel", func() error {
			_, err := h.CreateReleaseChannel(ctx, connect.NewRequest(&pb.CreateReleaseChannelRequest{}))
			return err
		}},
		{"UpdateReleaseChannel", func() error {
			_, err := h.UpdateReleaseChannel(ctx, connect.NewRequest(&pb.UpdateReleaseChannelRequest{}))
			return err
		}},
		{"DeleteReleaseChannel", func() error {
			_, err := h.DeleteReleaseChannel(ctx, connect.NewRequest(&pb.DeleteReleaseChannelRequest{}))
			return err
		}},
		{"PreviewReleaseChannelScope", func() error {
			_, err := h.PreviewReleaseChannelScope(ctx, connect.NewRequest(&pb.PreviewReleaseChannelScopeRequest{}))
			return err
		}},
		{"ApplyReleaseChannelFirmware", func() error {
			_, err := h.ApplyReleaseChannelFirmware(ctx, connect.NewRequest(&pb.ApplyReleaseChannelFirmwareRequest{}))
			return err
		}},
		{"RollbackReleaseChannelFirmware", func() error {
			_, err := h.RollbackReleaseChannelFirmware(ctx, connect.NewRequest(&pb.RollbackReleaseChannelFirmwareRequest{}))
			return err
		}},
		{"ListRollouts", func() error {
			_, err := h.ListRollouts(ctx, connect.NewRequest(&pb.ListRolloutsRequest{}))
			return err
		}},
		{"GetRollout", func() error {
			_, err := h.GetRollout(ctx, connect.NewRequest(&pb.GetRolloutRequest{}))
			return err
		}},
		{"ListRolloutDevices", func() error {
			_, err := h.ListRolloutDevices(ctx, connect.NewRequest(&pb.ListRolloutDevicesRequest{}))
			return err
		}},
		{"ContinueRollout", func() error {
			_, err := h.ContinueRollout(ctx, connect.NewRequest(&pb.ContinueRolloutRequest{}))
			return err
		}},
		{"PauseRollout", func() error {
			_, err := h.PauseRollout(ctx, connect.NewRequest(&pb.PauseRolloutRequest{}))
			return err
		}},
		{"ResumeRollout", func() error {
			_, err := h.ResumeRollout(ctx, connect.NewRequest(&pb.ResumeRolloutRequest{}))
			return err
		}},
		{"CancelRollout", func() error {
			_, err := h.CancelRollout(ctx, connect.NewRequest(&pb.CancelRolloutRequest{}))
			return err
		}},
		{"RetryFailedRolloutDevices", func() error {
			_, err := h.RetryFailedRolloutDevices(ctx, connect.NewRequest(&pb.RetryFailedRolloutDevicesRequest{}))
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.call()
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
		})
	}
}

func newFakeService() *fakeService {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	finished := now.Add(time.Hour)
	return &fakeService{
		channel: &rollout.Channel{
			ID: 3, Name: "Canary", Description: "First wave",
			Scope:    rollout.Scope{RackIDs: []int64{10}, DeviceIdentifiers: []string{"miner-0"}},
			Behavior: rollout.Behavior{Method: rollout.MethodBatched, Order: rollout.OrderRandom, BatchSize: 5, ReviewAfterEachBatch: true, AutoContinue: true, Thresholds: rollout.Thresholds{MaxHashrateDropPercent: ptr(10.0), MaxNewErrors: ptr(int32(0))}, MaxConcurrentOffline: 20},
			ModelGroups: []rollout.ModelGroup{{
				Model: "Rig", FirmwareFileID: "fw-2", FirmwareVersion: "2.0.0", ActiveRolloutID: 9,
				MinerCount: 1, OnTargetCount: 0, ReportedVersions: []string{"1.0.0"},
			}},
			MinerCount: 1, CreatedAt: now, UpdatedAt: now,
		},
		rollout: &rollout.Rollout{
			ID: 9, ChannelID: 3, ChannelName: "Canary", Model: "Rig", FirmwareFileID: "fw-2", FirmwareVersion: "2.0.0",
			Status: rollout.StatusCompletedWithFailures, State: rollout.StateCompletedWithFailures, Stage: rollout.StageRest,
			Behavior:   rollout.Behavior{Method: rollout.MethodPilotThenContinue, Order: rollout.OrderLeastEfficientFirst, PilotSize: 1, ReviewAfterEachBatch: true},
			BatchCount: 1, CurrentBatch: 0, StageChangedAt: now, CreatedAt: now, FinishedAt: &finished,
			PreviousFirmwareFileID: "fw-1", PreviousFirmwareVersion: "1.5.0",
			Devices: []rollout.RolloutDevice{{
				DeviceID: 500, DeviceIdentifier: "miner-0", IPAddress: "10.0.0.1", FirmwareVersion: "1.0.0",
				Phase: rollout.PhaseFailed, Batch: 1, Status: "OFFLINE", Attempts: 3, LastSentAt: &now, LastError: "Did not report 2.0.0 after 3 update attempts",
				HashRateHs: rollout.Metric{Baseline: ptr(100.0)},
			}},
			DeviceCounts:       rollout.DeviceCounts{Failed: 1},
			CurrentBatchCounts: rollout.DeviceCounts{},
			Evidence:           &rollout.Evidence{DevicesTotal: 1, Failed: 1, HoldReason: "1 miners failed to update"},
		},
		preview: &rollout.ScopePreview{
			MinerCount: 4,
			Models:     []rollout.ModelCount{{Model: "Rig", MinerCount: 4}},
			Conflicts:  []rollout.ScopeConflict{{ChannelID: 2, ChannelName: "Stable", MinerCount: 1}},
		},
	}
}

func TestCreateReleaseChannelTranslatesSpecAndView(t *testing.T) {
	t.Parallel()
	svc := newFakeService()
	h := NewHandler(svc)
	ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)

	resp, err := h.CreateReleaseChannel(ctx, connect.NewRequest(&pb.CreateReleaseChannelRequest{
		Name: "Canary", Description: "First wave",
		Scope: &pb.ReleaseChannelScope{RackIds: []int64{10}, DeviceIdentifiers: []string{"miner-0"}},
		Behavior: &pb.RolloutBehavior{
			Method: pb.RolloutMethod_ROLLOUT_METHOD_BATCHED, Order: pb.RolloutOrder_ROLLOUT_ORDER_RANDOM,
			BatchSize: 5, ReviewAfterEachBatch: true, AutoContinueOnHealthyTelemetry: true, MaxConcurrentOffline: 20,
			Thresholds: &pb.RolloutAutomationThresholds{MaxHashrateDropPercent: ptr(10.0), MaxNewErrors: ptr(int32(0))},
		},
	}))
	require.NoError(t, err)

	assert.Equal(t, int64(7), svc.lastOrgID)
	assert.Equal(t, int64(42), svc.lastUserID)
	assert.Equal(t, rollout.ChannelSpec{
		Name: "Canary", Description: "First wave",
		Scope:    rollout.Scope{RackIDs: []int64{10}, DeviceIdentifiers: []string{"miner-0"}},
		Behavior: svc.channel.Behavior,
	}, svc.lastSpec)

	ch := resp.Msg.Channel
	assert.Equal(t, int64(3), ch.Id)
	assert.Equal(t, []int64{10}, ch.Scope.RackIds)
	assert.Equal(t, pb.RolloutMethod_ROLLOUT_METHOD_BATCHED, ch.Behavior.Method)
	assert.Equal(t, pb.RolloutOrder_ROLLOUT_ORDER_RANDOM, ch.Behavior.Order)
	assert.Equal(t, int32(20), ch.Behavior.MaxConcurrentOffline)
	require.NotNil(t, ch.Behavior.Thresholds.MaxHashrateDropPercent)
	assert.Equal(t, 10.0, *ch.Behavior.Thresholds.MaxHashrateDropPercent)
	assert.Nil(t, ch.Behavior.Thresholds.MaxTemperatureIncreaseCelsius)
	require.Len(t, ch.ModelGroups, 1)
	assert.Equal(t, int64(9), ch.ModelGroups[0].ActiveRolloutId)
	assert.Equal(t, int32(1), ch.ModelGroups[0].MinerCount)
	assert.Equal(t, int32(0), ch.ModelGroups[0].OnTargetCount)
	assert.Equal(t, []string{"1.0.0"}, ch.ModelGroups[0].ReportedVersions)
	assert.Equal(t, int32(1), ch.MinerCount)
}

func TestUnknownBehaviorEnumsAreNotSilentlyDefaulted(t *testing.T) {
	t.Parallel()
	b := behaviorFromProto(&pb.RolloutBehavior{Method: pb.RolloutMethod(99)})
	assert.Equal(t, "99", b.Method, "carried through non-empty so the domain rejects it as an unknown method")
	assert.Equal(t, "", behaviorFromProto(&pb.RolloutBehavior{}).Method, "unspecified defaults in the domain")
	assert.Equal(t, rollout.Behavior{}, behaviorFromProto(nil))
}

func TestPreviewScopeTranslatesCountsAndConflicts(t *testing.T) {
	t.Parallel()
	svc := newFakeService()
	h := NewHandler(svc)
	ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)

	resp, err := h.PreviewReleaseChannelScope(ctx, connect.NewRequest(&pb.PreviewReleaseChannelScopeRequest{
		Scope: &pb.ReleaseChannelScope{SiteIds: []int64{1}}, ChannelId: 3,
	}))
	require.NoError(t, err)
	assert.Equal(t, rollout.Scope{SiteIDs: []int64{1}}, svc.lastScope)
	assert.Equal(t, int64(3), svc.lastID)
	assert.Equal(t, int32(4), resp.Msg.MinerCount)
	assert.Equal(t, "Rig", resp.Msg.Models[0].Model)
	require.Len(t, resp.Msg.Conflicts, 1)
	assert.Equal(t, "Stable", resp.Msg.Conflicts[0].ChannelName)
}

func TestRolloutViewTranslatesStatesPhasesAndEvidence(t *testing.T) {
	t.Parallel()
	svc := newFakeService()
	h := NewHandler(svc)
	ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)

	resp, err := h.ApplyReleaseChannelFirmware(ctx, connect.NewRequest(&pb.ApplyReleaseChannelFirmwareRequest{
		ChannelId:   3,
		Assignments: []*pb.FirmwareAssignment{{Model: "Rig", FirmwareFileId: "fw-2"}},
	}))
	require.NoError(t, err)
	assert.Equal(t, []rollout.Assignment{{Model: "Rig", FirmwareFileID: "fw-2"}}, svc.lastAssigned)
	assert.Equal(t, int64(3), resp.Msg.Channel.Id)
	require.Len(t, resp.Msg.StartedRollouts, 1)

	r := resp.Msg.StartedRollouts[0]
	assert.Equal(t, pb.RolloutStatus_ROLLOUT_STATUS_COMPLETED_WITH_FAILURES, r.Status)
	assert.Equal(t, pb.RolloutState_ROLLOUT_STATE_COMPLETED_WITH_FAILURES, r.State)
	assert.Equal(t, pb.RolloutStage_ROLLOUT_STAGE_REST, r.Stage)
	assert.Equal(t, pb.RolloutCancelReason_ROLLOUT_CANCEL_REASON_UNSPECIFIED, r.CancelReason)
	assert.Equal(t, pb.RolloutMethod_ROLLOUT_METHOD_PILOT_THEN_CONTINUE, r.Behavior.Method)
	assert.Equal(t, int32(1), r.Behavior.PilotSize)
	assert.Equal(t, "fw-1", r.PreviousFirmwareFileId)
	require.NotNil(t, r.FinishedAt)
	assert.Nil(t, r.PausedAt)

	assert.Equal(t, int32(1), r.DeviceCount)
	assert.Equal(t, int32(1), r.DeviceCounts.Failed)
	assert.Equal(t, int32(0), r.DeviceCounts.Done)
	require.NotNil(t, r.CurrentBatchCounts)

	// Devices are paged separately and translated the same way.
	devices, err := h.ListRolloutDevices(ctx, connect.NewRequest(&pb.ListRolloutDevicesRequest{RolloutId: 9, PageSize: 50, Cursor: "c1"}))
	require.NoError(t, err)
	assert.Equal(t, int64(9), svc.lastID)
	assert.Equal(t, int32(50), svc.lastPage)
	assert.Equal(t, "c1", svc.lastCursor)
	assert.Equal(t, "more-devices", devices.Msg.Cursor)
	require.Len(t, devices.Msg.Devices, 1)
	d := devices.Msg.Devices[0]
	assert.Equal(t, pb.RolloutDevicePhase_ROLLOUT_DEVICE_PHASE_FAILED, d.Phase)
	assert.Equal(t, int32(3), d.Attempts)
	assert.Contains(t, d.LastError, "after 3 update attempts")
	require.NotNil(t, d.LastSentAt)
	require.NotNil(t, d.HashRateHs.Baseline)
	assert.Nil(t, d.HashRateHs.Current)

	require.NotNil(t, r.Evidence)
	assert.Equal(t, int32(1), r.Evidence.Failed)
	assert.Equal(t, "1 miners failed to update", r.Evidence.HoldReason)
}

func TestLifecycleRPCsForwardIdentity(t *testing.T) {
	t.Parallel()
	svc := newFakeService()
	h := NewHandler(svc)
	ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)

	_, err := h.CancelRollout(ctx, connect.NewRequest(&pb.CancelRolloutRequest{RolloutId: 9}))
	require.NoError(t, err)
	assert.Equal(t, int64(9), svc.lastID)

	_, err = h.RetryFailedRolloutDevices(ctx, connect.NewRequest(&pb.RetryFailedRolloutDevicesRequest{RolloutId: 9}))
	require.NoError(t, err)
	assert.Equal(t, int64(42), svc.lastUserID)

	rb, err := h.RollbackReleaseChannelFirmware(ctx, connect.NewRequest(&pb.RollbackReleaseChannelFirmwareRequest{RolloutId: 9}))
	require.NoError(t, err)
	assert.Equal(t, int64(3), rb.Msg.Channel.Id)
	require.Len(t, rb.Msg.StartedRollouts, 1)

	_, err = h.DeleteReleaseChannel(ctx, connect.NewRequest(&pb.DeleteReleaseChannelRequest{ChannelId: 3}))
	require.NoError(t, err)
	assert.Equal(t, int64(3), svc.lastID)
}

func ptr[T any](v T) *T { return &v }

func TestListRolloutsTranslatesFilterAndCursor(t *testing.T) {
	t.Parallel()
	svc := newFakeService()
	h := NewHandler(svc)
	ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)

	resp, err := h.ListRollouts(ctx, connect.NewRequest(&pb.ListRolloutsRequest{
		ChannelId: 3, Status: pb.RolloutStatus_ROLLOUT_STATUS_ACTIVE, PageSize: 25, Cursor: "abc",
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(7), svc.lastOrgID)
	assert.Equal(t, rollout.RolloutFilter{ChannelID: 3, Status: rollout.StatusActive, PageSize: 25, Cursor: "abc"}, svc.lastFilter)
	assert.Equal(t, "next-cursor", resp.Msg.Cursor)
	require.Len(t, resp.Msg.Rollouts, 1)
	assert.Equal(t, int64(9), resp.Msg.Rollouts[0].Id)

	// An unspecified status is "any status", not a filter for the zero value.
	_, err = h.ListRollouts(ctx, connect.NewRequest(&pb.ListRolloutsRequest{}))
	require.NoError(t, err)
	assert.Equal(t, rollout.RolloutFilter{}, svc.lastFilter)

	_, err = h.ListRollouts(ctx, connect.NewRequest(&pb.ListRolloutsRequest{Status: pb.RolloutStatus(99)}))
	require.ErrorContains(t, err, "unknown rollout status")

	got, err := h.GetRollout(ctx, connect.NewRequest(&pb.GetRolloutRequest{RolloutId: 9}))
	require.NoError(t, err)
	assert.Equal(t, int64(9), svc.lastID)
	assert.Equal(t, pb.RolloutStatus_ROLLOUT_STATUS_COMPLETED_WITH_FAILURES, got.Msg.Rollout.Status)

	ch, err := h.GetReleaseChannel(ctx, connect.NewRequest(&pb.GetReleaseChannelRequest{ChannelId: 3}))
	require.NoError(t, err)
	assert.Equal(t, int64(3), svc.lastID)
	assert.Equal(t, "Canary", ch.Msg.Channel.Name)
}

func TestListReleaseChannelMinersTranslatesPage(t *testing.T) {
	t.Parallel()
	svc := newFakeService()
	h := NewHandler(svc)
	ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)

	resp, err := h.ListReleaseChannelMiners(ctx, connect.NewRequest(&pb.ListReleaseChannelMinersRequest{
		ChannelId: 3, Model: "Rig", PageSize: 10, Cursor: "c0",
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(7), svc.lastOrgID)
	assert.Equal(t, int64(3), svc.lastID)
	assert.Equal(t, "Rig", svc.lastModel)
	assert.Equal(t, int32(10), svc.lastPage)
	assert.Equal(t, "c0", svc.lastCursor)
	assert.Equal(t, "more-miners", resp.Msg.Cursor)
	require.Len(t, resp.Msg.Miners, 1)
	assert.Equal(t, "miner-0", resp.Msg.Miners[0].DeviceIdentifier)
	assert.True(t, resp.Msg.Miners[0].Conflicted)
}
