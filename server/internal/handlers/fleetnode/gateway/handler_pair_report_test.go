package gateway_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
)

func TestReportPairedDevices_RejectsMissingCommandID(t *testing.T) {
	// Arrange
	handler, _, fleetNodeID := newHeartbeatHandler(t)
	ctx := authn.SetInfo(t.Context(), &auth.Subject{FleetNodeID: fleetNodeID, OrgID: 1, Name: "agent-pair"})
	req := connect.NewRequest(&pb.ReportPairedDevicesRequest{
		Results: []*pb.FleetNodePairResult{{DeviceIdentifier: "x", Outcome: pb.PairOutcome_PAIR_OUTCOME_PAIRED}},
	})

	// Act
	_, err := handler.ReportPairedDevices(ctx, req)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command_id")
}

func TestReportPairedDevices_RejectsUnknownCommandID(t *testing.T) {
	// Arrange
	h := newControlHarness(t)
	subject := &auth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1, Name: "agent-pair-unknown"}
	stream := h.registry.Register(h.fleetNodeID)
	defer stream.Unregister()
	ctx := authn.SetInfo(context.Background(), subject)

	// Act
	_, err := h.handler.ReportPairedDevices(ctx, connect.NewRequest(&pb.ReportPairedDevicesRequest{
		CommandId: "never-sent",
		Results:   []*pb.FleetNodePairResult{{DeviceIdentifier: "x", Outcome: pb.PairOutcome_PAIR_OUTCOME_PAIRED}},
	}))

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "in-flight server-issued command")
}

func TestReportPairedDevices_PersistsAuthoritativelyAndForwards(t *testing.T) {
	// Arrange: a node-discovered device and an in-flight pair command scoped to it.
	h := newControlHarness(t)
	_, err := h.db.Exec(
		`INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active, discovered_by_fleet_node_id)
		 VALUES (1, $1, '10.0.0.7', '80', 'http', 'virtual', TRUE, $2)`,
		"pair-corr-1", h.fleetNodeID)
	require.NoError(t, err)
	subject := &auth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1, Name: "agent-pair-correlation"}
	stream := h.registry.Register(h.fleetNodeID)
	defer stream.Unregister()
	pair := &control.PairMeta{OrgID: 1, Targets: map[string]struct{}{"pair-corr-1": {}}}
	session, err := h.registry.Send(context.Background(), h.fleetNodeID, &pb.ControlCommand{CommandId: "pair-cmd"}, nil, control.ReportKindPair, pair)
	require.NoError(t, err)
	defer session.Close()
	<-stream.Outgoing
	ctx := authn.SetInfo(context.Background(), subject)

	// Act
	resp, err := h.handler.ReportPairedDevices(ctx, connect.NewRequest(&pb.ReportPairedDevicesRequest{
		CommandId: "pair-cmd",
		Results: []*pb.FleetNodePairResult{
			{DeviceIdentifier: "pair-corr-1", Outcome: pb.PairOutcome_PAIR_OUTCOME_PAIRED},
		},
	}))

	// Assert: persisted authoritatively in the gateway path (independent of the
	// operator stream) and also forwarded for live display.
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp.Msg.GetAcceptedCount())
	var status string
	require.NoError(t, h.db.QueryRow(
		`SELECT dp.pairing_status FROM device d JOIN device_pairing dp ON dp.device_id = d.id WHERE d.device_identifier=$1 AND d.org_id=1`,
		"pair-corr-1").Scan(&status))
	assert.Equal(t, "PAIRED", status)
	select {
	case ev := <-session.Events():
		require.Len(t, ev.PairResults, 1)
		assert.Equal(t, "pair-corr-1", ev.PairResults[0].GetDeviceIdentifier())
	case <-time.After(time.Second):
		t.Fatal("expected pair results on events channel")
	}
}

func TestReportPairedDevices_ForwardsPersistedStatusOnStaleAuthNeeded(t *testing.T) {
	// Arrange: a device already PAIRED (e.g. paired by a re-issued command) with an
	// in-flight pair command still scoped to it. A stale AUTH_NEEDED arrives.
	h := newControlHarness(t)
	var ddID int64
	require.NoError(t, h.db.QueryRow(
		`INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active, discovered_by_fleet_node_id)
		 VALUES (1, $1, '10.0.0.7', '80', 'http', 'virtual', TRUE, $2) RETURNING id`,
		"mac:race", h.fleetNodeID).Scan(&ddID))
	var devID int64
	require.NoError(t, h.db.QueryRow(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, 'aa:bb:cc:00:ra:01', 'sn-race', 1, $2) RETURNING id`,
		"mac:race", ddID).Scan(&devID))
	_, err := h.db.Exec(`INSERT INTO device_pairing (device_id, pairing_status) VALUES ($1, 'PAIRED')`, devID)
	require.NoError(t, err)

	stream := h.registry.Register(h.fleetNodeID)
	defer stream.Unregister()
	pair := &control.PairMeta{OrgID: 1, Targets: map[string]struct{}{"mac:race": {}}}
	session, err := h.registry.Send(context.Background(), h.fleetNodeID, &pb.ControlCommand{CommandId: "race-cmd"}, nil, control.ReportKindPair, pair)
	require.NoError(t, err)
	defer session.Close()
	<-stream.Outgoing
	ctx := authn.SetInfo(context.Background(), &auth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1, Name: "agent-race"})

	// Act: a stale AUTH_NEEDED for the already-paired device.
	_, err = h.handler.ReportPairedDevices(ctx, connect.NewRequest(&pb.ReportPairedDevicesRequest{
		CommandId: "race-cmd",
		Results: []*pb.FleetNodePairResult{{
			DeviceIdentifier: "mac:race",
			Outcome:          pb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED,
		}},
	}))
	require.NoError(t, err)

	// Assert: the forwarded result reflects the persisted PAIRED status, not the
	// raw AUTH_NEEDED, so the operator display matches the DB.
	select {
	case ev := <-session.Events():
		require.Len(t, ev.PairResults, 1)
		assert.Equal(t, pb.PairOutcome_PAIR_OUTCOME_PAIRED, ev.PairResults[0].GetOutcome())
	case <-time.After(time.Second):
		t.Fatal("expected forwarded pair result")
	}
}

func TestReportPairedDevices_RejectsEmptyBatch(t *testing.T) {
	// Arrange: an in-flight pair command with no results reported.
	h := newControlHarness(t)
	subject := &auth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1, Name: "agent-pair-empty"}
	stream := h.registry.Register(h.fleetNodeID)
	defer stream.Unregister()
	pair := &control.PairMeta{OrgID: 1, Targets: map[string]struct{}{"x": {}}}
	session, err := h.registry.Send(context.Background(), h.fleetNodeID, &pb.ControlCommand{CommandId: "pair-empty"}, nil, control.ReportKindPair, pair)
	require.NoError(t, err)
	defer session.Close()
	<-stream.Outgoing

	// Act: an empty report (allowed by the proto, consumes no quota).
	_, err = h.handler.ReportPairedDevices(authn.SetInfo(context.Background(), subject), connect.NewRequest(&pb.ReportPairedDevicesRequest{
		CommandId: "pair-empty",
	}))

	// Assert: rejected as InvalidArgument rather than acked.
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "empty pair report must be InvalidArgument, got %v", err)
}

func TestReportPairedDevices_RejectsMissingSubject(t *testing.T) {
	// Arrange
	handler, _, _ := newHeartbeatHandler(t)
	req := connect.NewRequest(&pb.ReportPairedDevicesRequest{
		Results: []*pb.FleetNodePairResult{{DeviceIdentifier: "x", Outcome: pb.PairOutcome_PAIR_OUTCOME_PAIRED}},
	})

	// Act
	_, err := handler.ReportPairedDevices(context.Background(), req)

	// Assert
	require.Error(t, err)
}
