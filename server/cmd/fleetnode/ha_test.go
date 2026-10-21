package main

import (
	"context"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
)

func TestHALeaseUsesRequestSendTimeAndFencesWorkerStart(t *testing.T) {
	now := time.Now()
	authority := newHAAuthority(7, "incarnation", "session")
	req := authority.request(now)
	response := &pb.HALeaseResponse{RequestId: req.GetRequestId(), OwnerNodeId: 7, ActiveNodeId: 7, LeaseEpoch: 3, LeaseDurationMs: 30000, Granted: true, InstanceId: "incarnation", ControlSessionId: "session", OrgId: 1}
	require.True(t, authority.apply(response, now.Add(time.Second)))
	cmd := &pb.ControlCommand{HaOwnerNodeId: 7, HaLeaseEpoch: 3, IntendedInstanceId: "incarnation", ControlSessionId: "session"}
	ctx, done, ok := authority.start(context.Background(), cmd, now.Add(2*time.Second))
	require.True(t, ok)
	defer done()
	// Receiving late must not grant 25 seconds from receipt.
	_, _, ok = authority.start(context.Background(), cmd, now.Add(26*time.Second))
	require.False(t, ok)
	require.Error(t, ctx.Err())
	authority.close()
}

func TestHALeaseRejectsUncorrelatedAndWrongSessionGrants(t *testing.T) {
	now := time.Now()
	for _, wrong := range []string{"request", "instance", "session", "expired", "owner", "org"} {
		t.Run(wrong, func(t *testing.T) {
			a := newHAAuthority(7, "i", "s")
			a.accept(&pb.ControlAccepted{HaOwnerNodeId: 7, OrgId: 1})
			defer a.close()
			request := a.request(now)
			resp := &pb.HALeaseResponse{RequestId: request.GetRequestId(), OwnerNodeId: 7, ActiveNodeId: 7, LeaseEpoch: 1, LeaseDurationMs: 30000, Granted: true, InstanceId: "i", ControlSessionId: "s", OrgId: 1}
			at := now
			switch wrong {
			case "request":
				resp.RequestId = "unknown"
			case "instance":
				resp.InstanceId = "old-process"
			case "session":
				resp.ControlSessionId = "old-session"
			case "owner":
				resp.OwnerNodeId = 8
			case "org":
				resp.OrgId = 2
			case "expired":
				at = now.Add(26 * time.Second)
			}
			require.False(t, a.apply(resp, at))
			_, _, ok := a.start(context.Background(), &pb.ControlCommand{HaOwnerNodeId: 7, HaLeaseEpoch: 1, IntendedInstanceId: "i", ControlSessionId: "s"}, at)
			require.False(t, ok)
		})
	}
}

func TestHAPreparingAndStandbyCancelExistingWork(t *testing.T) {
	now := time.Now()
	a := newHAAuthority(7, "i", "s")
	defer a.close()
	grant := func(preparing, granted bool, epoch int64) {
		request := a.request(now)
		require.True(t, a.apply(&pb.HALeaseResponse{RequestId: request.GetRequestId(), OwnerNodeId: 7, ActiveNodeId: 7, LeaseEpoch: epoch, LeaseDurationMs: 30000, Granted: granted, Preparing: preparing, InstanceId: "i", ControlSessionId: "s", OrgId: 1}, now))
	}
	grant(false, true, 1)
	cmd := &pb.ControlCommand{HaOwnerNodeId: 7, HaLeaseEpoch: 1, IntendedInstanceId: "i", ControlSessionId: "s"}
	ctx, done, ok := a.start(context.Background(), cmd, now)
	require.True(t, ok)
	defer done()
	grant(true, false, 2)
	require.Error(t, ctx.Err())
	_, _, ok = a.start(context.Background(), cmd, now)
	require.False(t, ok)
	require.Zero(t, a.acknowledgedEpoch())
}

func TestHAStandaloneHasUntimedAuthorityAndTransferScope(t *testing.T) {
	now := time.Now()
	a := newHAAuthority(7, "i", "s")
	defer a.close()
	a.accept(&pb.ControlAccepted{OrgId: 1})
	require.True(t, a.allowed(&pb.ControlCommand{}, now.Add(time.Hour)))
	require.False(t, a.allowed(&pb.ControlCommand{HaOwnerNodeId: 7, HaLeaseEpoch: 1}, now))
	scope, ok := a.transferScope(now.Add(time.Hour))
	require.True(t, ok)
	require.EqualValues(t, 1, scope.GetOrgId())
	require.Zero(t, scope.GetOwnerNodeId())
	require.Zero(t, a.acknowledgedEpoch())
}

func TestHAOutOfOrderResponsesAndIncarnations(t *testing.T) {
	now := time.Now()
	authority := newHAAuthority(7, "i", "s")
	defer authority.close()
	first, second := authority.request(now), authority.request(now)
	reply := func(id string, epoch int64) *pb.HALeaseResponse {
		return &pb.HALeaseResponse{RequestId: id, OwnerNodeId: 7, ActiveNodeId: 7, LeaseEpoch: epoch, Granted: true, InstanceId: "i", ControlSessionId: "s", OrgId: 1, LeaseDurationMs: 30000}
	}
	require.True(t, authority.apply(reply(second.GetRequestId(), 4), now))
	require.False(t, authority.apply(reply(first.GetRequestId(), 3), now))
	require.EqualValues(t, 4, authority.request(now).GetAcknowledgedEpoch())
	lower := authority.request(now)
	require.False(t, authority.apply(reply(lower.GetRequestId(), 3), now), "newer replies cannot roll back a lease epoch")
	replacement := newHAAuthority(7, "i-new", "s-new")
	defer replacement.close()
	require.False(t, replacement.allowed(&pb.ControlCommand{HaOwnerNodeId: 7, HaLeaseEpoch: 4, IntendedInstanceId: "i", ControlSessionId: "s"}, now))
	process := &RunCmd{}
	id, err := process.processInstanceID()
	require.NoError(t, err)
	same, err := process.processInstanceID()
	require.NoError(t, err)
	require.Equal(t, id, same)
	other, err := (&RunCmd{}).processInstanceID()
	require.NoError(t, err)
	require.NotEqual(t, id, other)
}

func TestHADiscoveryIdentifiersRetainLogicalOwnerAfterPromotion(t *testing.T) {
	ctx := context.WithValue(t.Context(), haOwnerNodeIDKey{}, int64(7))
	for _, device := range []*discoverymodels.DiscoveredDevice{
		{Device: pairingpb.Device{DriverName: "antminer", Model: "S21"}},
		{Device: pairingpb.Device{DriverName: "proto", Model: "CT1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER}},
	} {
		original := reportFromDiscovered(device, "10.0.0.1", "4028", 7)
		promoted := reportFromDiscovered(device, "10.0.0.1", "4028", discoveryOwnerNodeID(ctx, 9))
		require.Equal(t, original.GetDeviceIdentifier(), promoted.GetDeviceIdentifier())
	}
	require.EqualValues(t, 9, discoveryOwnerNodeID(t.Context(), 9))
}

func TestHACompletionPreservesAcceptedResultAndOrdinaryTimeout(t *testing.T) {
	now := time.Now()
	authority := newHAAuthority(7, "i", "s")
	defer authority.close()
	request := authority.request(now)
	require.True(t, authority.apply(&pb.HALeaseResponse{RequestId: request.GetRequestId(), OwnerNodeId: 7, ActiveNodeId: 7, Granted: true, LeaseEpoch: 1, InstanceId: "i", ControlSessionId: "s", OrgId: 1, LeaseDurationMs: 30000}, now))
	command := &pb.ControlCommand{HaOwnerNodeId: 7, HaLeaseEpoch: 1, IntendedInstanceId: "i", ControlSessionId: "s"}
	ctx, end, ok := authority.start(t.Context(), command, now)
	require.True(t, ok)
	defer end()
	captured := &capturingAcker{}
	completed := &haCommandAcker{ctx: ctx, inner: captured}
	original := &pb.ControlAck{CommandId: "completed", Succeeded: true, Code: pb.AckCode_ACK_CODE_OK, Payload: []byte("known result")}
	require.NoError(t, completed.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: original}}))
	require.False(t, authority.allowed(command, now.Add(26*time.Second)))
	require.ErrorIs(t, context.Cause(ctx), errHAAuthorityLost)
	require.Len(t, captured.sent, 1)
	require.Same(t, original, captured.sent[0].GetAck(), "authority loss must not replace an already accepted terminal result")
	// A positive device completion remains conclusive even if its delivery is
	// scheduled after cancellation, as do known refusals before device execution.
	for _, ack := range []*pb.ControlAck{
		{CommandId: "late-success", Succeeded: true, Code: pb.AckCode_ACK_CODE_OK},
		{CommandId: "bad-request", Code: pb.AckCode_ACK_CODE_BAD_REQUEST},
		{CommandId: "incapable", Code: pb.AckCode_ACK_CODE_AGENT_INCAPABLE},
	} {
		require.NoError(t, completed.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: ack}}))
		require.Same(t, ack, captured.sent[len(captured.sent)-1].GetAck())
	}
	partial := &pb.ControlAck{CommandId: "partial", Code: pb.AckCode_ACK_CODE_PARTIAL, Payload: []byte("incomplete physical result")}
	require.NoError(t, completed.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: partial}}))
	interrupted := captured.sent[len(captured.sent)-1].GetAck()
	require.Equal(t, pb.AckCode_ACK_CODE_COMMAND_INTERRUPTED, interrupted.GetCode())
	require.Empty(t, interrupted.GetPayload())

	timeoutCtx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	timeoutCapture := &capturingAcker{}
	timeoutAcker := &haCommandAcker{ctx: timeoutCtx, inner: timeoutCapture}
	timeoutAck := &pb.ControlAck{CommandId: "timeout", Code: pb.AckCode_ACK_CODE_INTERNAL, ErrorMessage: context.DeadlineExceeded.Error()}
	require.NoError(t, timeoutAcker.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: timeoutAck}}))
	require.Same(t, timeoutAck, timeoutCapture.sent[0].GetAck(), "ordinary command timeouts must keep their existing classification")
}
