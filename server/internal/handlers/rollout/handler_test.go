package rollout

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

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
	group   rollout.ModelGroup
	rollout *rollout.Rollout
	preview *rollout.ScopePreview

	lastOrgID        int64
	lastUserID       int64
	lastActor        rollout.Actor
	lastMutation     rollout.Mutation
	lastSpec         rollout.ChannelSpec
	lastScope        rollout.Scope
	lastAssigned     []rollout.Assignment
	lastOverride     *rollout.Behavior
	lastID           int64
	lastFilter       rollout.RolloutFilter
	lastManufacturer string
	lastModel        string
	lastPage         int32
	lastCursor       string
	// err, when set, is returned by every mutation.
	err error
}

func (f *fakeService) ListChannels(_ context.Context, orgID int64) ([]rollout.Channel, error) {
	f.lastOrgID = orgID
	return []rollout.Channel{*f.channel}, nil
}

func (f *fakeService) GetChannel(_ context.Context, orgID, channelID int64) (*rollout.Channel, error) {
	f.lastOrgID, f.lastID = orgID, channelID
	return f.channel, nil
}

func (f *fakeService) ListChannelMiners(_ context.Context, orgID, channelID int64, manufacturer, model string, pageSize int32, cursor string) ([]rollout.ChannelMiner, string, error) {
	f.lastOrgID, f.lastID, f.lastManufacturer, f.lastModel, f.lastPage, f.lastCursor = orgID, channelID, manufacturer, model, pageSize, cursor
	return []rollout.ChannelMiner{{DeviceID: 500, DeviceIdentifier: "miner-0", Manufacturer: "Proto", Model: "Rig", FirmwareVersion: "1.0.0", Conflicted: true, LastDeployedFirmwareChecksum: checksum}}, "more-miners", nil
}

func (f *fakeService) ListChannelModelGroups(_ context.Context, orgID, channelID int64, pageSize int32, cursor string) ([]rollout.ModelGroup, string, error) {
	f.lastOrgID, f.lastID, f.lastPage, f.lastCursor = orgID, channelID, pageSize, cursor
	return []rollout.ModelGroup{f.group}, "more-groups", nil
}

func (f *fakeService) ListMembershipConflicts(_ context.Context, orgID, channelID int64, pageSize int32, cursor string) ([]rollout.MembershipConflict, string, error) {
	f.lastOrgID, f.lastID, f.lastPage, f.lastCursor = orgID, channelID, pageSize, cursor
	return []rollout.MembershipConflict{{
		DeviceID: 500, DeviceIdentifier: "miner-0", Manufacturer: "Proto", Model: "Rig",
		ChannelID: 3, ChannelName: "Canary", Specificity: 3, Resolution: rollout.ResolutionExcludedTie,
	}}, "", nil
}

func (f *fakeService) PreviewFirmware(_ context.Context, orgID, channelID int64, assignments []rollout.Assignment, override *rollout.Behavior) ([]rollout.FirmwarePlan, error) {
	f.lastOrgID, f.lastID, f.lastAssigned, f.lastOverride = orgID, channelID, assignments, override
	return []rollout.FirmwarePlan{{
		Pair: rollout.PairKey{Manufacturer: "Proto", Model: "Rig"}, FirmwareFileID: "fw-2", FirmwareVersion: "2.0.0", FirmwareChecksum: checksum,
		TargetCount: 4, OnTargetCount: 1, BatchCount: 2, Behavior: f.channel.Behavior,
	}}, nil
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

func (f *fakeService) ApplyFirmware(_ context.Context, orgID int64, actor rollout.Actor, channelID int64, assignments []rollout.Assignment, override *rollout.Behavior) ([]rollout.Rollout, error) {
	f.lastOrgID, f.lastActor, f.lastID, f.lastAssigned, f.lastOverride = orgID, actor, channelID, assignments, override
	if f.err != nil {
		return nil, f.err
	}
	return []rollout.Rollout{*f.rollout}, nil
}

func (f *fakeService) RollbackFirmware(_ context.Context, orgID, rolloutID int64, m rollout.Mutation) (int64, []rollout.Rollout, error) {
	f.lastOrgID, f.lastID, f.lastMutation = orgID, rolloutID, m
	return f.channel.ID, []rollout.Rollout{*f.rollout}, nil
}

func (f *fakeService) ListRollouts(_ context.Context, orgID int64, filter rollout.RolloutFilter) ([]rollout.Rollout, string, string, error) {
	f.lastOrgID, f.lastFilter = orgID, filter
	return []rollout.Rollout{*f.rollout}, "next-cursor", "next-poll-cursor", nil
}

func (f *fakeService) GetRollout(_ context.Context, orgID, rolloutID int64) (*rollout.Rollout, error) {
	f.lastOrgID, f.lastID = orgID, rolloutID
	return f.rollout, nil
}

func (f *fakeService) ListRolloutDevices(_ context.Context, orgID, rolloutID int64, pageSize int32, cursor string) ([]rollout.RolloutDevice, string, error) {
	f.lastOrgID, f.lastID, f.lastPage, f.lastCursor = orgID, rolloutID, pageSize, cursor
	return f.rollout.Devices, "more-devices", nil
}

func (f *fakeService) mutate(orgID, rolloutID int64, m rollout.Mutation) (*rollout.Rollout, error) {
	f.lastOrgID, f.lastID, f.lastMutation = orgID, rolloutID, m
	if f.err != nil {
		return nil, f.err
	}
	return f.rollout, nil
}

func (f *fakeService) ContinueRollout(_ context.Context, orgID, rolloutID int64, m rollout.Mutation) (*rollout.Rollout, error) {
	return f.mutate(orgID, rolloutID, m)
}

func (f *fakeService) PauseRollout(_ context.Context, orgID, rolloutID int64, m rollout.Mutation) (*rollout.Rollout, error) {
	return f.mutate(orgID, rolloutID, m)
}

func (f *fakeService) ResumeRollout(_ context.Context, orgID, rolloutID int64, m rollout.Mutation) (*rollout.Rollout, error) {
	return f.mutate(orgID, rolloutID, m)
}

func (f *fakeService) CancelRollout(_ context.Context, orgID, rolloutID int64, m rollout.Mutation) (*rollout.Rollout, *rollout.Channel, error) {
	r, err := f.mutate(orgID, rolloutID, m)
	return r, f.channel, err
}

func (f *fakeService) RetryFailedDevices(_ context.Context, orgID, rolloutID int64, m rollout.Mutation) (*rollout.Rollout, error) {
	return f.mutate(orgID, rolloutID, m)
}

const checksum = "abababababababababababababababababababababababababababababababab"

func ctxWithPermissions(t *testing.T, permissions ...string) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), &session.Info{OrganizationID: 7, UserID: 42, Username: "ops", AuthMethod: session.AuthMethodSession})
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
		{"ListReleaseChannelModelGroups", func() error {
			_, err := h.ListReleaseChannelModelGroups(ctx, connect.NewRequest(&pb.ListReleaseChannelModelGroupsRequest{}))
			return err
		}},
		{"ListReleaseChannelMembershipConflicts", func() error {
			_, err := h.ListReleaseChannelMembershipConflicts(ctx, connect.NewRequest(&pb.ListReleaseChannelMembershipConflictsRequest{}))
			return err
		}},
		{"PreviewReleaseChannelFirmware", func() error {
			_, err := h.PreviewReleaseChannelFirmware(ctx, connect.NewRequest(&pb.PreviewReleaseChannelFirmwareRequest{}))
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
			Scope:           rollout.Scope{RackIDs: []int64{10}, DeviceIdentifiers: []string{"miner-0"}},
			Behavior:        rollout.Behavior{Method: rollout.MethodBatched, Order: rollout.OrderRandom, BatchSize: 5, ReviewAfterEachBatch: true, AutoContinue: true, Thresholds: rollout.Thresholds{MaxHashrateDropPercent: ptr(10.0), MaxNewErrors: ptr(int32(0))}, MaxConcurrentOffline: 20},
			ModelGroupCount: 1, MinerCount: 1, CreatedAt: now, UpdatedAt: now,
		},
		group: rollout.ModelGroup{
			Manufacturer: "proto", Model: "Rig", FirmwareFileID: "fw-2", FirmwareChecksum: checksum, FirmwareAvailable: true,
			FirmwareVersion: "2.0.0", FirmwareTargetManufacturer: "Proto", FirmwareTargetModel: "Rig", AssignmentGeneration: 2,
			ActiveRolloutID: 9, MinerCount: 1, OnTargetCount: 0, ReportedVersions: []string{"1.0.0"}, ReportedVersionCount: 1,
		},
		rollout: &rollout.Rollout{
			ID: 9, ChannelID: 3, ChannelName: "Canary", Manufacturer: "Proto", Model: "Rig",
			FirmwareChecksum: checksum, FirmwareFileID: "fw-2", FirmwareVersion: "2.0.0", AssignmentGeneration: 2,
			Status: rollout.StatusCompletedWithFailures, State: rollout.StateCompletedWithFailures, Stage: rollout.StageRest,
			Behavior:   rollout.Behavior{Method: rollout.MethodPilotThenContinue, Order: rollout.OrderLeastEfficientFirst, PilotSize: 1, ReviewAfterEachBatch: true},
			BatchCount: 1, CurrentBatch: 0, StageChangedAt: now, CreatedAt: now, FinishedAt: &finished,
			PreviousFirmwareChecksum: "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd", PreviousFirmwareVersion: "1.5.0",
			Revision: 4, UpdatedAt: finished,
			StartedBy:    rollout.Actor{Type: rollout.ActorTypeUser, ID: 42, Name: "ops"},
			LastActionBy: rollout.SystemActor,
			Devices: []rollout.RolloutDevice{{
				DeviceID: 500, DeviceIdentifier: "miner-0", IPAddress: "10.0.0.1", FirmwareVersion: "1.0.0",
				Phase: rollout.PhaseFailed, Batch: 1, Status: "OFFLINE", Attempts: 3, LastSentAt: &now, LastError: "Did not report 2.0.0 after 3 update attempts",
				HashRateHs: rollout.Metric{Baseline: ptr(100.0)}, LastDeployedFirmwareChecksum: checksum,
			}},
			DeviceCounts:       rollout.DeviceCounts{Failed: 1},
			CurrentBatchCounts: rollout.DeviceCounts{},
			Evidence: &rollout.Evidence{
				DevicesTotal: 1, Failed: 1, HoldReason: "1 miners failed to update",
				HashRateHs: rollout.Aggregate{Metric: rollout.Metric{Baseline: ptr(100.0), Current: ptr(90.0)}, SampledDevices: 1},
			},
		},
		preview: &rollout.ScopePreview{
			MinerCount:    4,
			Models:        []rollout.ModelCount{{Manufacturer: "Proto", Model: "Rig", MinerCount: 4}},
			Conflicts:     []rollout.ScopeConflict{{ChannelID: 2, ChannelName: "Stable", MinerCount: 1}},
			ModelCount:    1,
			ConflictCount: 1,
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
	assert.Equal(t, int32(1), ch.ModelGroupCount)
	assert.Equal(t, int32(1), ch.MinerCount)

	// Model groups are paged separately and carry the assignment.
	groups, err := h.ListReleaseChannelModelGroups(ctx, connect.NewRequest(&pb.ListReleaseChannelModelGroupsRequest{ChannelId: 3, PageSize: 20, Cursor: "g0"}))
	require.NoError(t, err)
	assert.Equal(t, int64(3), svc.lastID)
	assert.Equal(t, int32(20), svc.lastPage)
	assert.Equal(t, "g0", svc.lastCursor)
	assert.Equal(t, "more-groups", groups.Msg.Cursor)
	require.Len(t, groups.Msg.ModelGroups, 1)
	g := groups.Msg.ModelGroups[0]
	assert.Equal(t, "proto", g.Manufacturer)
	assert.Equal(t, checksum, g.FirmwareChecksum)
	assert.True(t, g.FirmwareAvailable)
	assert.Equal(t, "Proto", g.FirmwareTargetManufacturer)
	assert.Equal(t, int64(2), g.AssignmentGeneration)
	assert.Equal(t, int64(9), g.ActiveRolloutId)
	assert.Equal(t, int32(1), g.ReportedVersionCount)

	// A summary list carries counts and no scope.
	list, err := h.ListReleaseChannels(ctx, connect.NewRequest(&pb.ListReleaseChannelsRequest{}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Channels, 1)
	assert.Equal(t, int32(1), list.Msg.Channels[0].ModelGroupCount)

	// The delegated method is refused until its slice lands.
	_, err = h.CreateReleaseChannel(ctx, connect.NewRequest(&pb.CreateReleaseChannelRequest{
		Name: "Controlled", Behavior: &pb.RolloutBehavior{Method: pb.RolloutMethod_ROLLOUT_METHOD_DELEGATED},
	}))
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeUnimplemented, fleetErr.GRPCCode)
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
	assert.Equal(t, "Proto", resp.Msg.Models[0].Manufacturer)
	assert.Equal(t, "Rig", resp.Msg.Models[0].Model)
	assert.Equal(t, int32(1), resp.Msg.ModelCount)
	require.Len(t, resp.Msg.Conflicts, 1)
	assert.Equal(t, "Stable", resp.Msg.Conflicts[0].ChannelName)
	assert.Equal(t, int32(1), resp.Msg.ConflictCount)

	conflicts, err := h.ListReleaseChannelMembershipConflicts(ctx, connect.NewRequest(&pb.ListReleaseChannelMembershipConflictsRequest{ChannelId: 3}))
	require.NoError(t, err)
	require.Len(t, conflicts.Msg.Conflicts, 1)
	assert.Equal(t, pb.ReleaseChannelSelectorSpecificity_RELEASE_CHANNEL_SELECTOR_SPECIFICITY_RACK, conflicts.Msg.Conflicts[0].SelectorSpecificity)
	assert.Equal(t, pb.ReleaseChannelConflictResolution_RELEASE_CHANNEL_CONFLICT_RESOLUTION_EXCLUDED_TIE, conflicts.Msg.Conflicts[0].Resolution)

	plans, err := h.PreviewReleaseChannelFirmware(ctx, connect.NewRequest(&pb.PreviewReleaseChannelFirmwareRequest{
		ChannelId:        3,
		Assignments:      []*pb.FirmwareAssignment{{Manufacturer: "Proto", Model: "Rig", FirmwareFileId: "fw-2"}},
		BehaviorOverride: &pb.RolloutBehavior{Method: pb.RolloutMethod_ROLLOUT_METHOD_BATCHED, BatchSize: 2},
	}))
	require.NoError(t, err)
	require.NotNil(t, svc.lastOverride)
	assert.Equal(t, rollout.MethodBatched, svc.lastOverride.Method)
	require.Len(t, plans.Msg.Plans, 1)
	assert.Equal(t, checksum, plans.Msg.Plans[0].FirmwareChecksum)
	assert.Equal(t, int32(4), plans.Msg.Plans[0].TargetCount)
	assert.Equal(t, int32(2), plans.Msg.Plans[0].BatchCount)
}

func TestRolloutViewTranslatesStatesPhasesAndEvidence(t *testing.T) {
	t.Parallel()
	svc := newFakeService()
	h := NewHandler(svc)
	ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)

	resp, err := h.ApplyReleaseChannelFirmware(ctx, connect.NewRequest(&pb.ApplyReleaseChannelFirmwareRequest{
		ChannelId:   3,
		Assignments: []*pb.FirmwareAssignment{{Manufacturer: "Proto", Model: "Rig", FirmwareFileId: "fw-2"}},
	}))
	require.NoError(t, err)
	assert.Equal(t, []rollout.Assignment{{Manufacturer: "Proto", Model: "Rig", FirmwareFileID: "fw-2"}}, svc.lastAssigned)
	assert.Equal(t, rollout.Actor{Type: rollout.ActorTypeUser, ID: 42, Name: "ops"}, svc.lastActor)
	assert.Nil(t, svc.lastOverride)
	assert.Equal(t, int64(3), resp.Msg.Channel.Id)
	require.Len(t, resp.Msg.StartedRollouts, 1)

	r := resp.Msg.StartedRollouts[0]
	assert.Equal(t, pb.RolloutStatus_ROLLOUT_STATUS_COMPLETED_WITH_FAILURES, r.Status)
	assert.Equal(t, pb.RolloutState_ROLLOUT_STATE_COMPLETED_WITH_FAILURES, r.State)
	assert.Equal(t, pb.RolloutStage_ROLLOUT_STAGE_REST, r.Stage)
	assert.Equal(t, pb.RolloutCancelReason_ROLLOUT_CANCEL_REASON_UNSPECIFIED, r.CancelReason)
	assert.Equal(t, pb.RolloutMethod_ROLLOUT_METHOD_PILOT_THEN_CONTINUE, r.Behavior.Method)
	assert.Equal(t, int32(1), r.Behavior.PilotSize)
	assert.Equal(t, "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd", r.PreviousFirmwareChecksum)
	assert.Equal(t, checksum, r.FirmwareChecksum)
	assert.Equal(t, "Proto", r.Manufacturer)
	assert.Equal(t, int64(2), r.AssignmentGeneration)
	assert.Equal(t, int64(4), r.Revision)
	assert.Equal(t, pb.RolloutActorType_ROLLOUT_ACTOR_TYPE_USER, r.StartedBy.Type)
	assert.Equal(t, int64(42), r.StartedBy.Id)
	assert.Equal(t, pb.RolloutActorType_ROLLOUT_ACTOR_TYPE_SYSTEM, r.LastActionBy.Type)
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
	assert.Equal(t, int32(1), r.Evidence.HashRateHs.SampledDevices)
	assert.Nil(t, r.Evidence.HashrateChangePercent, "absent change stays absent")
	assert.Equal(t, checksum, d.LastDeployedFirmwareChecksum)
}

func TestLifecycleRPCsForwardIdentity(t *testing.T) {
	t.Parallel()
	svc := newFakeService()
	h := NewHandler(svc)
	ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)

	_, err := h.CancelRollout(ctx, connect.NewRequest(&pb.CancelRolloutRequest{RolloutId: 9, ExpectedRevision: 4, Note: "bad batch"}))
	require.NoError(t, err)
	assert.Equal(t, int64(9), svc.lastID)
	assert.Equal(t, rollout.Mutation{Actor: rollout.Actor{Type: rollout.ActorTypeUser, ID: 42, Name: "ops"}, ExpectedRevision: 4, Note: "bad batch"}, svc.lastMutation)

	_, err = h.RetryFailedRolloutDevices(ctx, connect.NewRequest(&pb.RetryFailedRolloutDevicesRequest{RolloutId: 9}))
	require.NoError(t, err)
	assert.Equal(t, int64(42), svc.lastMutation.Actor.ID)

	rb, err := h.RollbackReleaseChannelFirmware(ctx, connect.NewRequest(&pb.RollbackReleaseChannelFirmwareRequest{RolloutId: 9}))
	require.NoError(t, err)
	assert.Equal(t, int64(3), rb.Msg.Channel.Id)
	require.Len(t, rb.Msg.StartedRollouts, 1)

	// API keys are attributed as such.
	keyCtx := middleware.WithEffectivePermissions(
		authn.SetInfo(t.Context(), &session.Info{OrganizationID: 7, UserID: 8, Username: "controller", AuthMethod: session.AuthMethodAPIKey, APIKeyID: "k1"}),
		authz.NewEffectivePermissions([]authz.Assignment{{AssignmentID: 1, ScopeType: authz.ScopeOrg, Permissions: []string{authz.PermMinerFirmwareUpdate}}}),
	)
	_, err = h.PauseRollout(keyCtx, connect.NewRequest(&pb.PauseRolloutRequest{RolloutId: 9}))
	require.NoError(t, err)
	assert.Equal(t, rollout.Actor{Type: rollout.ActorTypeAPIKey, ID: 8, Name: "controller"}, svc.lastMutation.Actor)

	// Domain reasons travel as RolloutErrorInfo details.
	svc.err = fleeterror.NewFailedPreconditionErrorf("stale: %w", &rollout.ErrorInfo{Reason: rollout.ReasonStaleRevision, CurrentRevision: 5})
	_, err = h.ResumeRollout(ctx, connect.NewRequest(&pb.ResumeRolloutRequest{RolloutId: 9, ExpectedRevision: 4}))
	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, connect.CodeFailedPrecondition, ce.Code())
	var info *pb.RolloutErrorInfo
	for _, d := range ce.Details() {
		if msg, derr := d.Value(); derr == nil {
			if v, ok := msg.(*pb.RolloutErrorInfo); ok {
				info = v
			}
		}
	}
	require.NotNil(t, info)
	assert.Equal(t, pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_STALE_REVISION, info.Reason)
	assert.Equal(t, int64(5), info.CurrentRevision)
	svc.err = nil

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

	since := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	_, err = h.ListRollouts(ctx, connect.NewRequest(&pb.ListRolloutsRequest{UpdatedAfter: timestamppb.New(since)}))
	require.NoError(t, err)
	require.NotNil(t, svc.lastFilter.UpdatedAfter)
	assert.True(t, svc.lastFilter.UpdatedAfter.Equal(since))
	assert.Equal(t, "next-cursor", resp.Msg.Cursor)
	assert.Equal(t, "next-poll-cursor", resp.Msg.PollCursor)
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

func TestListRolloutsTranslatesPollCursor(t *testing.T) {
	t.Parallel()
	svc := newFakeService()
	h := NewHandler(svc)
	ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)

	resp, err := h.ListRollouts(ctx, connect.NewRequest(&pb.ListRolloutsRequest{
		ChannelId: 3, Status: pb.RolloutStatus_ROLLOUT_STATUS_ACTIVE, PageSize: 25,
		Cursor: "current-page", PollCursor: "previous-poll-cursor",
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(7), svc.lastOrgID)
	assert.Equal(t, rollout.RolloutFilter{
		ChannelID: 3, Status: rollout.StatusActive, PageSize: 25,
		Cursor: "current-page", PollCursor: "previous-poll-cursor",
	}, svc.lastFilter)
	assert.Equal(t, "next-cursor", resp.Msg.Cursor)
	assert.Equal(t, "next-poll-cursor", resp.Msg.PollCursor)
}

func TestListReleaseChannelMinersTranslatesPage(t *testing.T) {
	t.Parallel()
	svc := newFakeService()
	h := NewHandler(svc)
	ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)

	resp, err := h.ListReleaseChannelMiners(ctx, connect.NewRequest(&pb.ListReleaseChannelMinersRequest{
		ChannelId: 3, Manufacturer: "proto", Model: "Rig", PageSize: 10, Cursor: "c0",
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(7), svc.lastOrgID)
	assert.Equal(t, int64(3), svc.lastID)
	assert.Equal(t, "proto", svc.lastManufacturer)
	assert.Equal(t, "Rig", svc.lastModel)
	assert.Equal(t, int32(10), svc.lastPage)
	assert.Equal(t, "c0", svc.lastCursor)
	assert.Equal(t, "more-miners", resp.Msg.Cursor)
	require.Len(t, resp.Msg.Miners, 1)
	assert.Equal(t, "miner-0", resp.Msg.Miners[0].DeviceIdentifier)
	assert.True(t, resp.Msg.Miners[0].Conflicted)
	assert.Equal(t, checksum, resp.Msg.Miners[0].LastDeployedFirmwareChecksum)
}

// The contract's remaining RPCs are declared but answer Unimplemented until
// the delegated-control and events slices land.
func TestDelegatedControlAndEventsAreUnimplemented(t *testing.T) {
	t.Parallel()
	h := NewHandler(newFakeService())
	ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)

	_, err := h.AdvanceRollout(ctx, connect.NewRequest(&pb.AdvanceRolloutRequest{RolloutId: 9}))
	assert.Equal(t, connect.CodeUnimplemented, connect.CodeOf(err))
	_, err = h.SkipRolloutDevices(ctx, connect.NewRequest(&pb.SkipRolloutDevicesRequest{RolloutId: 9}))
	assert.Equal(t, connect.CodeUnimplemented, connect.CodeOf(err))
	_, err = h.CompleteRollout(ctx, connect.NewRequest(&pb.CompleteRolloutRequest{RolloutId: 9}))
	assert.Equal(t, connect.CodeUnimplemented, connect.CodeOf(err))
	_, err = h.ListRolloutEvents(ctx, connect.NewRequest(&pb.ListRolloutEventsRequest{}))
	assert.Equal(t, connect.CodeUnimplemented, connect.CodeOf(err))
}
