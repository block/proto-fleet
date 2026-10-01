package rollout

import (
	"context"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

func TestAdvanceRolloutTranslatesSelectionAndDispatchOrder(t *testing.T) {
	t.Parallel()
	for _, named := range []bool{false, true} {
		name := "count"
		if named {
			name = "named devices"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc := newFakeService()
			svc.rollout.Status, svc.rollout.State = rollout.StatusActive, rollout.StateInProgress
			svc.dispatched = []string{"miner-b", "miner-a"}
			h := NewHandler(svc)
			ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)
			request := &pb.AdvanceRolloutRequest{
				RolloutId: 9, ExpectedRevision: 3, Note: "canary health is stable",
				Selection: &pb.AdvanceRolloutRequest_Count{Count: 2},
			}
			selection := rollout.DeviceSelection{Count: 2}
			if named {
				request.Selection = &pb.AdvanceRolloutRequest_Devices{Devices: &pb.RolloutDeviceSelection{DeviceIdentifiers: []string{"miner-a", "miner-b"}}}
				selection = rollout.DeviceSelection{DeviceIdentifiers: []string{"miner-a", "miner-b"}}
			}

			resp, err := h.AdvanceRollout(ctx, connect.NewRequest(request))
			require.NoError(t, err)
			assert.Equal(t, int64(7), svc.lastOrgID)
			assert.Equal(t, int64(9), svc.lastID)
			assert.Equal(t, selection, svc.lastSelection)
			assert.Equal(t, rollout.Mutation{
				Actor:            rollout.Actor{Type: rollout.ActorTypeUser, ID: 42, Name: "ops"},
				ExpectedRevision: 3, Note: request.Note,
			}, svc.lastMutation)
			assert.Equal(t, int64(4), resp.Msg.Rollout.Revision)
			assert.Equal(t, pb.RolloutState_ROLLOUT_STATE_IN_PROGRESS, resp.Msg.Rollout.State)
			assert.Equal(t, svc.dispatched, resp.Msg.DeviceIdentifiers, "return domain dispatch order rather than request order")
		})
	}
}

func TestDelegatedBehaviorReachesChannelAndAssignmentServices(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"create", "update", "preview", "apply"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc := newFakeService()
			h := NewHandler(svc)
			ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)
			behavior := &pb.RolloutBehavior{
				Method: pb.RolloutMethod_ROLLOUT_METHOD_DELEGATED, Order: pb.RolloutOrder_ROLLOUT_ORDER_RANDOM,
				MaxConcurrentOffline: 5, ControllerTimeoutSeconds: 600,
			}
			var got rollout.Behavior
			switch name {
			case "create":
				_, err := h.CreateReleaseChannel(ctx, connect.NewRequest(&pb.CreateReleaseChannelRequest{Name: "Controlled", Behavior: behavior}))
				require.NoError(t, err)
				got = svc.lastSpec.Behavior
			case "update":
				_, err := h.UpdateReleaseChannel(ctx, connect.NewRequest(&pb.UpdateReleaseChannelRequest{ChannelId: 3, Name: "Controlled", Behavior: behavior}))
				require.NoError(t, err)
				got = svc.lastSpec.Behavior
			case "preview":
				_, err := h.PreviewReleaseChannelFirmware(ctx, connect.NewRequest(&pb.PreviewReleaseChannelFirmwareRequest{ChannelId: 3, BehaviorOverride: behavior}))
				require.NoError(t, err)
				require.NotNil(t, svc.lastOverride)
				got = *svc.lastOverride
			case "apply":
				_, err := h.ApplyReleaseChannelFirmware(ctx, connect.NewRequest(&pb.ApplyReleaseChannelFirmwareRequest{ChannelId: 3, BehaviorOverride: behavior}))
				require.NoError(t, err)
				require.NotNil(t, svc.lastOverride)
				got = *svc.lastOverride
			}
			assert.Equal(t, rollout.Behavior{
				Method: rollout.MethodDelegated, Order: rollout.OrderRandom, MaxConcurrentOffline: 5, ControllerTimeoutSeconds: 600,
			}, got)
		})
	}
}

func TestDelegatedMutationsPreserveAPIKeyActorRevisionAndNote(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"advance", "skip", "complete"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc := newFakeService()
			h := NewHandler(svc)
			ctx := authn.SetInfo(ctxWithPermissions(t, authz.PermMinerFirmwareUpdate), &session.Info{
				OrganizationID: 17, UserID: 8, Username: "owner", AuthMethod: session.AuthMethodAPIKey,
				APIKeyDatabaseID: 101, APIKeyName: "deployment key",
			})
			const note = "controller decision"
			var got *pb.Rollout
			switch name {
			case "advance":
				resp, err := h.AdvanceRollout(ctx, connect.NewRequest(&pb.AdvanceRolloutRequest{
					RolloutId: 9, ExpectedRevision: 4, Note: note, Selection: &pb.AdvanceRolloutRequest_Count{Count: 1},
				}))
				require.NoError(t, err)
				got = resp.Msg.Rollout
			case "skip":
				resp, err := h.SkipRolloutDevices(ctx, connect.NewRequest(&pb.SkipRolloutDevicesRequest{
					RolloutId: 9, ExpectedRevision: 4, Note: note, Devices: &pb.RolloutDeviceSelection{DeviceIdentifiers: []string{"miner-0"}},
				}))
				require.NoError(t, err)
				assert.Equal(t, []string{"miner-0"}, svc.lastIdentifiers)
				got = resp.Msg.Rollout
			case "complete":
				resp, err := h.CompleteRollout(ctx, connect.NewRequest(&pb.CompleteRolloutRequest{RolloutId: 9, ExpectedRevision: 4, Note: note}))
				require.NoError(t, err)
				got = resp.Msg.Rollout
			}
			assert.Equal(t, int64(17), svc.lastOrgID)
			assert.Equal(t, int64(9), svc.lastID)
			assert.Equal(t, rollout.Mutation{
				Actor:            rollout.Actor{Type: rollout.ActorTypeAPIKey, ID: 101, Name: "deployment key", OwnerUserID: 8},
				ExpectedRevision: 4, Note: note,
			}, svc.lastMutation)
			assert.Equal(t, int64(9), got.Id)
			assert.Equal(t, int64(4), got.Revision)
		})
	}
}

func TestDelegatedMutationsExposeDomainErrorDetails(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		call func(context.Context, *Handler) error
	}{
		{"advance", func(ctx context.Context, h *Handler) error {
			_, err := h.AdvanceRollout(ctx, connect.NewRequest(&pb.AdvanceRolloutRequest{RolloutId: 9, Selection: &pb.AdvanceRolloutRequest_Count{Count: 1}}))
			return err
		}},
		{"skip", func(ctx context.Context, h *Handler) error {
			_, err := h.SkipRolloutDevices(ctx, connect.NewRequest(&pb.SkipRolloutDevicesRequest{RolloutId: 9, Devices: &pb.RolloutDeviceSelection{DeviceIdentifiers: []string{"miner-0"}}}))
			return err
		}},
		{"complete", func(ctx context.Context, h *Handler) error {
			_, err := h.CompleteRollout(ctx, connect.NewRequest(&pb.CompleteRolloutRequest{RolloutId: 9}))
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := newFakeService()
			svc.err = fleeterror.NewFailedPreconditionErrorf("stale: %w", &rollout.ErrorInfo{
				Reason: rollout.ReasonStaleRevision, CurrentRevision: 8, DeviceIdentifiers: []string{"miner-0"},
			})
			err := tc.call(ctxWithPermissions(t, authz.PermMinerFirmwareUpdate), NewHandler(svc))
			var ce *connect.Error
			require.ErrorAs(t, err, &ce)
			assert.Equal(t, connect.CodeFailedPrecondition, ce.Code())
			var reasons []*pb.RolloutErrorInfo
			for _, detail := range ce.Details() {
				message, err := detail.Value()
				require.NoError(t, err)
				if info, ok := message.(*pb.RolloutErrorInfo); ok {
					reasons = append(reasons, info)
				}
			}
			require.Len(t, reasons, 1)
			assert.True(t, proto.Equal(&pb.RolloutErrorInfo{
				Reason: pb.RolloutErrorReason_ROLLOUT_ERROR_REASON_STALE_REVISION, CurrentRevision: 8, DeviceIdentifiers: []string{"miner-0"},
			}, reasons[0]))
		})
	}
}

func TestListRolloutEventsPreservesFilterAuditAndCursor(t *testing.T) {
	t.Parallel()
	svc := newFakeService()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	svc.events = []rollout.Event{{
		ID: 21, RolloutID: 9, ChannelID: 3, Type: rollout.EventRolloutAdvanced, OccurredAt: now,
		Actor:           rollout.Actor{Type: rollout.ActorTypeAPIKey, ID: 101, Name: "deployment key", OwnerUserID: 8},
		RolloutRevision: 4, Note: "healthy canary", DeviceIdentifiers: []string{"miner-b", "miner-a"},
	}}
	svc.nextEventCursor = "after-event-21"
	h := NewHandler(svc)
	ctx := ctxWithPermissions(t, authz.PermMinerFirmwareUpdate)
	resp, err := h.ListRolloutEvents(ctx, connect.NewRequest(&pb.ListRolloutEventsRequest{
		RolloutId: 9, ChannelId: 3, PageSize: 20, Cursor: "after-event-20",
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(7), svc.lastOrgID)
	assert.Equal(t, rollout.EventFilter{RolloutID: 9, ChannelID: 3, PageSize: 20, Cursor: "after-event-20"}, svc.lastEventFilter)
	assert.Equal(t, "after-event-21", resp.Msg.Cursor)
	require.Len(t, resp.Msg.Events, 1)
	event := resp.Msg.Events[0]
	assert.Equal(t, int64(21), event.Id)
	assert.Equal(t, int64(9), event.RolloutId)
	assert.Equal(t, int64(3), event.ChannelId)
	assert.Equal(t, pb.RolloutEventType_ROLLOUT_EVENT_TYPE_ADVANCED, event.Type)
	assert.Equal(t, now, event.OccurredAt.AsTime())
	assert.Equal(t, pb.RolloutActorType_ROLLOUT_ACTOR_TYPE_API_KEY, event.Actor.Type)
	assert.Equal(t, int64(101), event.Actor.Id)
	assert.Equal(t, "deployment key", event.Actor.Name)
	assert.Equal(t, int64(4), event.RolloutRevision)
	assert.Equal(t, "healthy canary", event.Note)
	assert.Equal(t, []string{"miner-b", "miner-a"}, event.DeviceIdentifiers)
	require.NoError(t, protovalidate.Validate(resp.Msg))

	// An idle poll retains the service's cursor so a controller can resume.
	svc.events = nil
	resp, err = h.ListRolloutEvents(ctx, connect.NewRequest(&pb.ListRolloutEventsRequest{Cursor: resp.Msg.Cursor}))
	require.NoError(t, err)
	assert.Empty(t, resp.Msg.Events)
	assert.Equal(t, "after-event-21", resp.Msg.Cursor)

	svc.err = fleeterror.NewInvalidArgumentError("invalid cursor")
	_, err = h.ListRolloutEvents(ctx, connect.NewRequest(&pb.ListRolloutEventsRequest{Cursor: "bad"}))
	require.ErrorIs(t, err, svc.err)
}

func TestRolloutEventTypesTranslateToDefinedContractValues(t *testing.T) {
	t.Parallel()
	assert.Len(t, eventTypeToProto, len(pb.RolloutEventType_name)-1, "each contract event type needs a domain mapping")
	for name, eventType := range eventTypeToProto {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			event := eventToProto(&rollout.Event{
				ID: 1, RolloutID: 2, ChannelID: 3, Type: name,
				OccurredAt: time.Now(), Actor: rollout.SystemActor, RolloutRevision: 1,
			})
			assert.Equal(t, eventType, event.Type)
			require.NoError(t, protovalidate.Validate(event))
		})
	}
}
