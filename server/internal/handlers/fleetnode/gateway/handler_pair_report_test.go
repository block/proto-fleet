package gateway_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/ha"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/gateway"
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
	pair := &control.PairMeta{OrgID: 1, Targets: map[string]struct{}{"pair-corr-1": {}}, Snapshots: map[string]*pairingpb.FleetNodePairTarget{
		"pair-corr-1": {DeviceIdentifier: "pair-corr-1", IpAddress: "10.0.0.7", Port: "80", UrlScheme: "http", DriverName: "virtual"},
	}}
	session, err := h.registry.Send(context.Background(), h.fleetNodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "pair-cmd"}, nil, control.ReportKindPair, pair)
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

func TestReportPairedDevices_DefaultPasswordActivePersistsAndForwards(t *testing.T) {
	// Arrange: a node-discovered device reports a successful pair while still on
	// its factory-default password.
	h := newControlHarness(t)
	_, err := h.db.Exec(
		`INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active, discovered_by_fleet_node_id)
		 VALUES (1, $1, '10.0.0.7', '80', 'http', 'proto', TRUE, $2)`,
		"pair-default-1", h.fleetNodeID)
	require.NoError(t, err)
	subject := &auth.Subject{FleetNodeID: h.fleetNodeID, OrgID: 1, Name: "agent-pair-default"}
	stream := h.registry.Register(h.fleetNodeID)
	defer stream.Unregister()
	pair := &control.PairMeta{OrgID: 1, Targets: map[string]struct{}{"pair-default-1": {}}, Snapshots: map[string]*pairingpb.FleetNodePairTarget{
		"pair-default-1": {DeviceIdentifier: "pair-default-1", IpAddress: "10.0.0.7", Port: "80", UrlScheme: "http", DriverName: "proto"},
	}}
	session, err := h.registry.Send(context.Background(), h.fleetNodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "pair-default-cmd"}, nil, control.ReportKindPair, pair)
	require.NoError(t, err)
	defer session.Close()
	<-stream.Outgoing
	ctx := authn.SetInfo(context.Background(), subject)
	active := true

	// Act
	resp, err := h.handler.ReportPairedDevices(ctx, connect.NewRequest(&pb.ReportPairedDevicesRequest{
		CommandId: "pair-default-cmd",
		Results: []*pb.FleetNodePairResult{{
			DeviceIdentifier:      "pair-default-1",
			Outcome:               pb.PairOutcome_PAIR_OUTCOME_PAIRED,
			DefaultPasswordActive: &active,
		}},
	}))

	// Assert
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp.Msg.GetAcceptedCount())
	var status string
	require.NoError(t, h.db.QueryRow(
		`SELECT dp.pairing_status FROM device d JOIN device_pairing dp ON dp.device_id = d.id WHERE d.device_identifier=$1 AND d.org_id=1`,
		"pair-default-1").Scan(&status))
	assert.Equal(t, "DEFAULT_PASSWORD", status)
	select {
	case ev := <-session.Events():
		require.Len(t, ev.PairResults, 1)
		assert.Equal(t, pb.PairOutcome_PAIR_OUTCOME_PAIRED, ev.PairResults[0].GetOutcome())
		require.NotNil(t, ev.PairResults[0].DefaultPasswordActive)
		assert.True(t, ev.PairResults[0].GetDefaultPasswordActive())
	case <-time.After(time.Second):
		t.Fatal("expected pair results on events channel")
	}
}

func TestReportPairedDevices_ForwardsPersistedStatusOnStaleAuthNeeded(t *testing.T) {
	// Arrange: a device already DEFAULT_PASSWORD (e.g. paired by a re-issued
	// command) with an in-flight pair command still scoped to it. A stale
	// AUTH_NEEDED arrives.
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
	_, err := h.db.Exec(`INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id) VALUES ($1, $2, 1)`, h.fleetNodeID, devID)
	require.NoError(t, err)
	_, err = h.db.Exec(`INSERT INTO device_pairing (device_id, pairing_status) VALUES ($1, 'DEFAULT_PASSWORD')`, devID)
	require.NoError(t, err)

	stream := h.registry.Register(h.fleetNodeID)
	defer stream.Unregister()
	pair := &control.PairMeta{OrgID: 1, Targets: map[string]struct{}{"mac:race": {}}, Snapshots: map[string]*pairingpb.FleetNodePairTarget{
		"mac:race": {DeviceIdentifier: "mac:race", IpAddress: "10.0.0.7", Port: "80", UrlScheme: "http", DriverName: "virtual"},
	}}
	session, err := h.registry.Send(context.Background(), h.fleetNodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "race-cmd"}, nil, control.ReportKindPair, pair)
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
			ErrorMessage:     "stale authentication failure",
		}},
	}))
	require.NoError(t, err)

	// Assert: the forwarded result reflects the persisted DEFAULT_PASSWORD
	// status, not the raw AUTH_NEEDED, and clears the stale auth error so the
	// operator display matches the successful persisted state.
	select {
	case ev := <-session.Events():
		require.Len(t, ev.PairResults, 1)
		assert.Equal(t, pb.PairOutcome_PAIR_OUTCOME_PAIRED, ev.PairResults[0].GetOutcome())
		require.NotNil(t, ev.PairResults[0].DefaultPasswordActive)
		assert.True(t, ev.PairResults[0].GetDefaultPasswordActive())
		assert.Empty(t, ev.PairResults[0].GetErrorMessage())
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
	session, err := h.registry.Send(context.Background(), h.fleetNodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "pair-empty"}, nil, control.ReportKindPair, pair)
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

func TestReportPairedDevices_PendingPromotedPairRevalidatesAuthority(t *testing.T) {
	for _, tc := range []struct {
		name     string
		accepted int64
	}{
		{name: "revoke_reporting_member"},
		{name: "durable_lease_expired"},
		{name: "durable_session_replaced"},
		{name: "revoke_original_owner", accepted: 1},
		{name: "renew_while_waiting", accepted: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newControlHarness(t)
			ctx := t.Context()
			ownerID := h.fleetNodeID
			var physicalID int64
			require.NoError(t, h.db.QueryRow(`INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status)
		VALUES(1,'promoted-standby',$1,$1,'CONFIRMED') RETURNING id`, make([]byte, 32)).Scan(&physicalID))
			fingerprint := bytes.Repeat([]byte{7}, 32)
			_, err := h.db.Exec(`INSERT INTO fleet_node_ha_group(owner_node_id,org_id,credential_key_fingerprint,enabled,active_node_id,instance_id,control_session_id,lease_epoch,lease_expires_at,acknowledged)
		VALUES($1,1,$2,true,$3,'promoted-instance','promoted-session',2,clock_timestamp()+interval '1 minute',true)`, ownerID, fingerprint, physicalID)
			require.NoError(t, err)
			_, err = h.db.Exec(`INSERT INTO fleet_node_ha_member(node_id,org_id,owner_node_id,ready,status_reason)
		VALUES($1,1,$1,true,''),($2,1,$1,true,'')`, ownerID, physicalID)
			require.NoError(t, err)
			haService := ha.NewService(h.db, h.registry)
			stream := h.registry.Register(physicalID)
			defer stream.Unregister()
			require.NoError(t, haService.ConnectStream(ctx, physicalID, 1, &pb.ControlHello{HaCapable: true, InstanceId: "promoted-instance", CredentialKeyFingerprint: fingerprint}, "promoted-session", stream))
			h.registry.SetHARoute(control.RouteBinding{OwnerNodeID: ownerID, PhysicalNodeID: physicalID, Epoch: 2, InstanceID: "promoted-instance", ControlSessionID: "promoted-session", EncryptionPublicKey: make([]byte, 32), ExpiresAt: time.Now().Add(time.Minute)})
			h.registry.SetHARouteResolver(func(ctx context.Context, ownerID int64) (control.RouteBinding, error) {
				return haService.Resolve(ctx, ownerID, 1)
			})

			apiKeys := apikey.NewService(sqlstores.NewSQLApiKeyStore(h.db), nil)
			enrollmentStore := sqlstores.NewSQLFleetNodeEnrollmentStore(h.db)
			transactor := sqlstores.NewSQLTransactor(h.db)
			enrollmentService := enrollment.NewService(enrollmentStore, apiKeys, transactor, nil)
			enrollmentService.WithHA(haService)
			pairingService := pairing.NewService(sqlstores.NewSQLFleetNodePairingStore(h.db), enrollmentStore, transactor).
				WithProvisioning(sqlstores.NewSQLDeviceStore(h.db), sqlstores.NewSQLDiscoveredDeviceStore(h.db), h.registry).
				WithHAOwnerResolver(func(ctx context.Context, nodeID, orgID int64) error {
					_, err := haService.Resolve(ctx, nodeID, orgID)
					return err
				})
			h.handler = gateway.NewHandler(enrollmentService, nil, pairingService, h.registry, h.files).WithHA(haService)

			var discoveredID, deviceID int64
			require.NoError(t, h.db.QueryRow(`INSERT INTO discovered_device(org_id,device_identifier,serial_number,ip_address,port,url_scheme,driver_name,is_active,discovered_by_fleet_node_id)
		VALUES(1,'pending-promoted-pair','original-serial','10.0.0.7','80','http','virtual',true,$1) RETURNING id`, ownerID).Scan(&discoveredID))
			require.NoError(t, h.db.QueryRow(`INSERT INTO device(org_id,device_identifier,mac_address,serial_number,discovered_device_id)
		VALUES(1,'pending-promoted-pair','aa:bb:cc:dd:ee:ff','original-serial',$1) RETURNING id`, discoveredID).Scan(&deviceID))
			_, err = h.db.Exec(`INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)`, ownerID, deviceID)
			require.NoError(t, err)
			_, err = h.db.Exec(`INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, deviceID)
			require.NoError(t, err)
			_, err = h.db.Exec(`INSERT INTO miner_credentials(device_id,username_enc,password_enc) VALUES($1,'original-user','original-password')`, deviceID)
			require.NoError(t, err)
			session, err := h.registry.Send(context.Background(), ownerID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "pending-promoted-pair"}, nil, control.ReportKindPair, &control.PairMeta{OrgID: 1, Targets: map[string]struct{}{"pending-promoted-pair": {}}})
			require.NoError(t, err)
			defer session.Close()
			<-stream.Outgoing

			blocker, err := h.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = blocker.Rollback() }()
			_, err = blocker.ExecContext(ctx, `SELECT id FROM device WHERE id=$1 FOR UPDATE`, deviceID)
			require.NoError(t, err)
			type reportOutcome struct {
				accepted int64
				err      error
			}
			done := make(chan reportOutcome, 1)
			go func() {
				resp, err := h.handler.ReportPairedDevices(authn.SetInfo(ctx, &auth.Subject{FleetNodeID: physicalID, OrgID: 1}), connect.NewRequest(&pb.ReportPairedDevicesRequest{CommandId: "pending-promoted-pair", Results: []*pb.FleetNodePairResult{{DeviceIdentifier: "pending-promoted-pair", SerialNumber: "stale-serial", Outcome: pb.PairOutcome_PAIR_OUTCOME_PAIRED, EncryptedCredentials: &pb.EncryptedCredentials{Username: []byte("stale-user"), Password: []byte("stale-password")}}}}))
				outcome := reportOutcome{err: err}
				if resp != nil {
					outcome.accepted = resp.Msg.GetAcceptedCount()
				}
				done <- outcome
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := h.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%name: LockFleetNodePairingDevice%')`).Scan(&waiting)
				return err == nil && waiting
			}, 5*time.Second, 10*time.Millisecond)

			changeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			switch tc.name {
			case "revoke_reporting_member":
				err = enrollmentService.RevokeFleetNode(changeCtx, physicalID, 1)
			case "revoke_original_owner":
				err = enrollmentService.RevokeFleetNode(changeCtx, ownerID, 1)
			case "durable_lease_expired":
				_, err = h.db.ExecContext(changeCtx, `UPDATE fleet_node_ha_group SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE owner_node_id=$1`, ownerID)
			case "durable_session_replaced":
				_, err = h.db.ExecContext(changeCtx, `UPDATE fleet_node_ha_group SET control_session_id='replacement-session' WHERE owner_node_id=$1`, ownerID)
			case "renew_while_waiting":
				var lease *pb.HALeaseResponse
				lease, err = haService.Lease(changeCtx, physicalID, 1, "promoted-instance", "promoted-session", &pb.HALeaseRequest{RequestId: "renew", AcknowledgedEpoch: 2})
				if err == nil {
					require.True(t, lease.GetGranted(), "pending device work must not block independent lease renewal")
				}
			}
			require.NoError(t, err, "authority changes must not wait for the device lock")
			require.NoError(t, blocker.Commit())
			select {
			case result := <-done:
				require.NoError(t, result.err)
				require.Equal(t, tc.accepted, result.accepted, "pending persistence must follow the reporting member's authority")
			case <-time.After(5 * time.Second):
				t.Fatal("pair persistence did not finish")
			}
			var discoveredSerial, deviceSerial, username, password string
			require.NoError(t, h.db.QueryRow(`SELECT dd.serial_number,d.serial_number,mc.username_enc,mc.password_enc FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id JOIN miner_credentials mc ON mc.device_id=d.id WHERE d.id=$1`, deviceID).Scan(&discoveredSerial, &deviceSerial, &username, &password))
			if tc.accepted == 0 {
				assert.Equal(t, "original-serial", discoveredSerial)
				assert.Equal(t, "original-serial", deviceSerial)
				assert.Equal(t, "original-user", username)
				assert.Equal(t, "original-password", password)
			} else {
				assert.Equal(t, "stale-serial", discoveredSerial)
				assert.Equal(t, "stale-serial", deviceSerial)
				assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("stale-user")), username)
				assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("stale-password")), password)
			}
			var boundOwner int64
			require.NoError(t, h.db.QueryRow(`SELECT fleet_node_id FROM fleet_node_device WHERE device_id=$1`, deviceID).Scan(&boundOwner))
			assert.Equal(t, ownerID, boundOwner, "device ownership remains stable across physical member changes")
		})
	}
}
