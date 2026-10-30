package ha_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/credentialkey"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/ha"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

const (
	v6 = pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V6
	v7 = pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V7
	v8 = pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8
)

type harness struct {
	database   *testutil.DatabaseService
	db         *sql.DB
	svc        *ha.Service
	registry   *control.Registry
	orgID      int64
	active     int64
	standby    int64
	standbyKey *ecdh.PrivateKey

	mu          sync.Mutex
	invalidated []models.DeviceIdentifier
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	h := &harness{database: database, db: database.DB, registry: control.NewRegistry()}
	h.orgID = database.CreateSuperAdminUser().OrganizationID
	h.svc = ha.NewService(h.db, sqlstores.NewSQLTransactor(h.db), h.registry, activity.NewService(sqlstores.NewSQLActivityStore(h.db)), func(deviceID models.DeviceIdentifier) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.invalidated = append(h.invalidated, deviceID)
	})
	h.svc.SetTimingForTest(0, 0, time.Hour)
	h.standbyKey, err = ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	h.active = h.createNode(t, "active", "CONFIRMED", make([]byte, 32))
	h.standby = h.createNode(t, "standby", "CONFIRMED", h.standbyKey.PublicKey().Bytes())
	return h
}

func (h *harness) createNode(t *testing.T, name, status string, encryptionPubkey []byte) int64 {
	t.Helper()
	var id int64
	require.NoError(t, h.db.QueryRowContext(t.Context(),
		`INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status) VALUES($1,$2,$3,$4,$5) RETURNING id`,
		h.orgID, name, []byte(uuid.NewString()), encryptionPubkey, status).Scan(&id))
	return id
}

func (h *harness) createPair(t *testing.T, ready bool) {
	t.Helper()
	require.NoError(t, sqlstores.NewSQLTransactor(h.db).RunInTx(t.Context(), func(ctx context.Context) error {
		return h.svc.CreatePair(ctx, h.orgID, h.active, h.standby)
	}))
	if ready {
		h.exec(t, `UPDATE fleet_node_ha_pair SET standby_ready=true, status_reason='' WHERE active_node_id=$1`, h.active)
	}
}

func (h *harness) createReadyPair(t *testing.T, orgID int64) *harness {
	t.Helper()
	pair := &harness{database: h.database, db: h.db, svc: h.svc, registry: h.registry, orgID: orgID}
	pair.active = pair.createNode(t, "active-"+uuid.NewString(), "CONFIRMED", make([]byte, 32))
	pair.standby = pair.createNode(t, "standby-"+uuid.NewString(), "CONFIRMED", h.standbyKey.PublicKey().Bytes())
	pair.createPair(t, true)
	return pair
}

func (h *harness) addDevice(t *testing.T, nodeID int64) int64 {
	t.Helper()
	device := h.database.CreateDevice(h.orgID, "proto")
	h.exec(t, `INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,$3)`, nodeID, device.DatabaseID, h.orgID)
	h.exec(t, `UPDATE discovered_device SET discovered_by_fleet_node_id=$1 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$2)`, nodeID, device.DatabaseID)
	return device.DatabaseID
}

func (h *harness) connect(t *testing.T, nodeID int64, version pb.CommandProtocolVersion) *control.Stream {
	t.Helper()
	stream, err := h.registry.RegisterAuthenticated(nodeID, "", version)
	require.NoError(t, err)
	t.Cleanup(stream.Unregister)
	return stream
}

func (h *harness) connectConfigured(t *testing.T, nodeID int64, version pb.CommandProtocolVersion) *control.Stream {
	t.Helper()
	stream := h.connect(t, nodeID, version)
	stream.SetDiscoveryConfig(&pb.DiscoveryConfig{Ranges: []string{"10.0.0.1-10.0.0.4"}})
	return stream
}

type pairRow struct {
	active, standby int64
	ready           bool
	reason          string
}

func (h *harness) pair(t *testing.T) (pairRow, bool) {
	t.Helper()
	var p pairRow
	err := h.db.QueryRowContext(t.Context(), `SELECT active_node_id, standby_node_id, standby_ready, status_reason FROM fleet_node_ha_pair WHERE org_id=$1`, h.orgID).
		Scan(&p.active, &p.standby, &p.ready, &p.reason)
	if errors.Is(err, sql.ErrNoRows) {
		return pairRow{}, false
	}
	require.NoError(t, err)
	return p, true
}

func (h *harness) deviceNode(t *testing.T, deviceID int64) int64 {
	t.Helper()
	var nodeID int64
	require.NoError(t, h.db.QueryRowContext(t.Context(), `SELECT fleet_node_id FROM fleet_node_device WHERE device_id=$1`, deviceID).Scan(&nodeID))
	return nodeID
}

func (h *harness) discoveredBy(t *testing.T, deviceID int64) int64 {
	t.Helper()
	var nodeID int64
	require.NoError(t, h.db.QueryRowContext(t.Context(),
		`SELECT dd.discovered_by_fleet_node_id FROM discovered_device dd JOIN device d ON d.discovered_device_id=dd.id WHERE d.id=$1`, deviceID).Scan(&nodeID))
	return nodeID
}

func (h *harness) pairID(t *testing.T) int64 {
	t.Helper()
	var id int64
	require.NoError(t, h.db.QueryRowContext(t.Context(), `SELECT id FROM fleet_node_ha_pair WHERE org_id=$1`, h.orgID).Scan(&id))
	return id
}

func (h *harness) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	_, err := h.db.ExecContext(t.Context(), query, args...)
	require.NoError(t, err)
}

type failoverActivity struct {
	description    string
	result         string
	actorType      string
	organizationID int64
	fromNodeID     int64
	toNodeID       int64
	deviceCount    int
}

func (h *harness) failoverActivities(t *testing.T) []failoverActivity {
	t.Helper()
	rows, err := h.db.QueryContext(t.Context(),
		`SELECT description, result, actor_type, organization_id, metadata FROM activity_log WHERE event_type='fleet_node_failover' AND organization_id=$1 ORDER BY id`, h.orgID)
	require.NoError(t, err)
	defer rows.Close()
	var events []failoverActivity
	for rows.Next() {
		var event failoverActivity
		var metadata []byte
		require.NoError(t, rows.Scan(&event.description, &event.result, &event.actorType, &event.organizationID, &metadata))
		var fields struct {
			FromNodeID  int64 `json:"from_node_id"`
			ToNodeID    int64 `json:"to_node_id"`
			DeviceCount int   `json:"device_count"`
		}
		require.NoError(t, json.Unmarshal(metadata, &fields))
		event.fromNodeID, event.toNodeID, event.deviceCount = fields.FromNodeID, fields.ToNodeID, fields.DeviceCount
		events = append(events, event)
	}
	require.NoError(t, rows.Err())
	return events
}

func respond(t *testing.T, stream *control.Stream, reply func(*pb.AgentCommand) *pb.ControlAck) *atomic.Int32 {
	t.Helper()
	var received atomic.Int32
	go func() {
		for {
			select {
			case <-t.Context().Done():
				return
			case <-stream.Done:
				return
			case cmd := <-stream.Outgoing:
				received.Add(1)
				command := &pb.AgentCommand{}
				ack := &pb.ControlAck{Code: pb.AckCode_ACK_CODE_BAD_REQUEST, ErrorMessage: "undecodable command"}
				if proto.Unmarshal(cmd.GetPayload(), command) == nil {
					ack = reply(command)
				}
				ack.CommandId = cmd.GetCommandId()
				stream.PublishAck(ack)
			}
		}
	}()
	return &received
}

func keyResultAck(result *pb.CredentialKeyResult) *pb.ControlAck {
	payload, err := proto.Marshal(result)
	if err != nil {
		return &pb.ControlAck{Code: pb.AckCode_ACK_CODE_INTERNAL, ErrorMessage: err.Error()}
	}
	return &pb.ControlAck{Succeeded: true, Code: pb.AckCode_ACK_CODE_OK, Payload: payload}
}

func discoveryConfigAck(command *pb.AgentCommand) *pb.ControlAck {
	payload, err := proto.Marshal(command.GetInstallDiscoveryConfig())
	if err != nil {
		return failedAck(err)
	}
	return &pb.ControlAck{Succeeded: true, Code: pb.AckCode_ACK_CODE_OK, Payload: payload}
}

// respondWithDiscoveryConfig models a node that persists ranges before acknowledging
// them, and counts key commands separately so key-transfer assertions stay precise.
func respondWithDiscoveryConfig(t *testing.T, stream *control.Stream, reply func(*pb.AgentCommand) *pb.ControlAck) (*atomic.Int32, *atomic.Int32) {
	t.Helper()
	var keys, ranges atomic.Int32
	respond(t, stream, func(command *pb.AgentCommand) *pb.ControlAck {
		if command.GetInstallDiscoveryConfig() != nil {
			ranges.Add(1)
			return discoveryConfigAck(command)
		}
		keys.Add(1)
		return reply(command)
	})
	return &keys, &ranges
}

func failedAck(err error) *pb.ControlAck {
	return &pb.ControlAck{Code: pb.AckCode_ACK_CODE_BAD_REQUEST, ErrorMessage: err.Error()}
}

func exportKey(key []byte) func(*pb.AgentCommand) *pb.ControlAck {
	return exportKeyWithFingerprint(key, credentialkey.Fingerprint(key))
}

func exportKeyWithFingerprint(key, fingerprint []byte) func(*pb.AgentCommand) *pb.ControlAck {
	return func(command *pb.AgentCommand) *pb.ControlAck {
		sealed, err := credentialkey.Seal(command.GetExportCredentialKey().GetRecipientPublicKey(), key)
		if err != nil {
			return failedAck(err)
		}
		return keyResultAck(&pb.CredentialKeyResult{SealedKey: sealed, Fingerprint: fingerprint})
	}
}

func installKey(priv *ecdh.PrivateKey) func(*pb.AgentCommand) *pb.ControlAck {
	return func(command *pb.AgentCommand) *pb.ControlAck {
		installed, err := credentialkey.Open(priv.Bytes(), command.GetInstallCredentialKey().GetSealedKey())
		if err != nil {
			return failedAck(err)
		}
		return keyResultAck(&pb.CredentialKeyResult{Fingerprint: credentialkey.Fingerprint(installed)})
	}
}

func requireCode(t *testing.T, err error, code connect.Code) {
	t.Helper()
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, code, fleetErr.GRPCCode)
}

func TestCheckStandbyTarget(t *testing.T) {
	h := newHarness(t)
	h.createPair(t, false)
	unpaired := h.createNode(t, "unpaired", "CONFIRMED", make([]byte, 32))
	pending := h.createNode(t, "pending", "PENDING", make([]byte, 32))

	tests := []struct {
		name     string
		targetID int64
		wantCode connect.Code
	}{
		{name: "confirmed unpaired node", targetID: unpaired},
		{name: "unconfirmed node", targetID: pending, wantCode: connect.CodeFailedPrecondition},
		{name: "active node of a pair", targetID: h.active, wantCode: connect.CodeFailedPrecondition},
		{name: "standby node of a pair", targetID: h.standby, wantCode: connect.CodeFailedPrecondition},
		{name: "missing node", targetID: unpaired + 1000, wantCode: connect.CodeNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := h.svc.CheckStandbyTarget(t.Context(), h.orgID, tc.targetID)

			if tc.wantCode == 0 {
				require.NoError(t, err)
				return
			}
			requireCode(t, err, tc.wantCode)
		})
	}
}

func TestCreatePair_RechecksTargetAtConfirmation(t *testing.T) {
	for _, change := range []string{"revoked", "soft deleted"} {
		t.Run(change, func(t *testing.T) {
			h := newHarness(t)
			require.NoError(t, h.svc.CheckStandbyTarget(t.Context(), h.orgID, h.active))
			if change == "revoked" {
				h.exec(t, `UPDATE fleet_node SET enrollment_status='REVOKED' WHERE id=$1`, h.active)
			} else {
				h.exec(t, `UPDATE fleet_node SET deleted_at=now() WHERE id=$1`, h.active)
			}

			err := sqlstores.NewSQLTransactor(h.db).RunInTx(t.Context(), func(ctx context.Context) error {
				return h.svc.CreatePair(ctx, h.orgID, h.active, h.standby)
			})

			requireCode(t, err, connect.CodeFailedPrecondition)
			_, paired := h.pair(t)
			require.False(t, paired, "confirmation must not create a pair with an ineligible target")
		})
	}
}

func TestCreatePair_RequiresTransaction(t *testing.T) {
	h := newHarness(t)

	err := h.svc.CreatePair(t.Context(), h.orgID, h.active, h.standby)

	requireCode(t, err, connect.CodeInternal)
}

func TestRequireNotStandby(t *testing.T) {
	h := newHarness(t)
	h.createPair(t, true)

	tests := []struct {
		name     string
		nodeID   int64
		wantCode connect.Code
	}{
		{name: "active node", nodeID: h.active},
		{name: "standby node", nodeID: h.standby, wantCode: connect.CodeFailedPrecondition},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := h.svc.RequireNotStandby(t.Context(), h.orgID, tc.nodeID)

			if tc.wantCode == 0 {
				require.NoError(t, err)
				return
			}
			requireCode(t, err, tc.wantCode)
		})
	}
}

func TestFailover_MovesOnlyTheActiveNodesDevicesAndLogsActivity(t *testing.T) {
	tests := []struct {
		name    string
		trigger func(t *testing.T, h *harness) error
		why     string
	}{
		{
			name: "tick after the active node disconnects",
			trigger: func(t *testing.T, h *harness) error {
				h.svc.TickForTest(t.Context(), time.Now())
				return nil
			},
			why: "after its control stream disconnected",
		},
		{
			name: "hand-off after revoking the active node",
			trigger: func(t *testing.T, h *harness) error {
				return h.svc.HandOff(t.Context(), h.orgID, h.active)
			},
			why: "after revoking the active node",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.createPair(t, true)
			moved := []int64{h.addDevice(t, h.active), h.addDevice(t, h.active)}
			bystander := h.createNode(t, "bystander", "CONFIRMED", make([]byte, 32))
			bystanderDevice := h.addDevice(t, bystander)
			h.connect(t, h.standby, v7)

			err := tc.trigger(t, h)

			require.NoError(t, err)
			pair, ok := h.pair(t)
			require.True(t, ok)
			assert.Equal(t, pairRow{active: h.standby, standby: h.active, ready: true}, pair, "the old active node becomes the ready standby")
			for _, deviceID := range moved {
				assert.Equal(t, h.standby, h.deviceNode(t, deviceID))
				assert.Equal(t, h.standby, h.discoveredBy(t, deviceID))
			}
			assert.Equal(t, bystander, h.deviceNode(t, bystanderDevice))
			assert.Equal(t, bystander, h.discoveredBy(t, bystanderDevice))
			var movedIdentifiers []models.DeviceIdentifier
			for _, deviceID := range moved {
				var identifier models.DeviceIdentifier
				require.NoError(t, h.db.QueryRowContext(t.Context(), `SELECT device_identifier FROM device WHERE id=$1`, deviceID).Scan(&identifier))
				movedIdentifiers = append(movedIdentifiers, identifier)
			}
			assert.ElementsMatch(t, movedIdentifiers, h.invalidated)
			assert.Equal(t, []failoverActivity{{
				description:    fmt.Sprintf("Moved 2 devices from fleet node 'active' (id=%d) to standby 'standby' (id=%d) %s", h.active, h.standby, tc.why),
				result:         "success",
				actorType:      "system",
				organizationID: h.orgID,
				fromNodeID:     h.active,
				toNodeID:       h.standby,
				deviceCount:    2,
			}}, h.failoverActivities(t))
		})
	}
}

func TestFailover_MovesLargeDeviceSetWithinTransactionDeadline(t *testing.T) {
	const deviceCount = 5000
	h := newHarness(t)
	h.createPair(t, true)
	h.connect(t, h.standby, v7)
	_, err := h.db.ExecContext(t.Context(), `
		WITH discovered AS (
			INSERT INTO discovered_device (
				org_id, device_identifier, ip_address, port, url_scheme,
				driver_name, is_active, discovered_by_fleet_node_id
			)
			SELECT $1::bigint, format('bulk:%s:%s', $2::bigint, n), '10.0.0.1', '4028', 'http',
			       'virtual', true, $2::bigint
			FROM generate_series(1, $3::integer) AS n
			RETURNING id, device_identifier
		), devices AS (
			INSERT INTO device (
				device_identifier, mac_address, serial_number, org_id, discovered_device_id
			)
			SELECT 'device-' || device_identifier, '', device_identifier, $1::bigint, id
			FROM discovered
			RETURNING id
		)
		INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id)
		SELECT $2::bigint, id, $1::bigint FROM devices`, h.orgID, h.active, deviceCount)
	require.NoError(t, err)

	h.svc.TickForTest(t.Context(), time.Now())

	var assigned, attributed int
	require.NoError(t, h.db.QueryRowContext(t.Context(), `SELECT count(*) FROM fleet_node_device WHERE org_id=$1 AND fleet_node_id=$2`, h.orgID, h.standby).Scan(&assigned))
	require.NoError(t, h.db.QueryRowContext(t.Context(), `SELECT count(*) FROM discovered_device WHERE org_id=$1 AND discovered_by_fleet_node_id=$2`, h.orgID, h.standby).Scan(&attributed))
	assert.Equal(t, deviceCount, assigned)
	assert.Equal(t, deviceCount, attributed)
	assert.Len(t, h.invalidated, deviceCount)
}

func TestFailover_RejectsStalePair(t *testing.T) {
	tests := []struct {
		name  string
		stale func(t *testing.T, h *harness) (activeID, standbyID int64)
	}{
		{name: "standby revoked", stale: func(t *testing.T, h *harness) (int64, int64) {
			h.exec(t, `UPDATE fleet_node SET enrollment_status='REVOKED' WHERE id=$1`, h.standby)
			return h.active, h.standby
		}},
		{name: "standby deleted", stale: func(t *testing.T, h *harness) (int64, int64) {
			h.exec(t, `UPDATE fleet_node SET deleted_at=now() WHERE id=$1`, h.standby)
			return h.active, h.standby
		}},
		{name: "active deleted", stale: func(t *testing.T, h *harness) (int64, int64) {
			h.exec(t, `UPDATE fleet_node SET deleted_at=now() WHERE id=$1`, h.active)
			return h.active, h.standby
		}},
		{name: "standby not ready", stale: func(t *testing.T, h *harness) (int64, int64) {
			h.exec(t, `UPDATE fleet_node_ha_pair SET standby_ready=false WHERE active_node_id=$1`, h.active)
			return h.active, h.standby
		}},
		{name: "different standby", stale: func(t *testing.T, h *harness) (int64, int64) {
			return h.active, h.createNode(t, "other-standby", "CONFIRMED", make([]byte, 32))
		}},
		{name: "pair deleted", stale: func(t *testing.T, h *harness) (int64, int64) {
			h.exec(t, `DELETE FROM fleet_node_ha_pair WHERE active_node_id=$1`, h.active)
			return h.active, h.standby
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.createPair(t, true)
			deviceID := h.addDevice(t, h.active)
			pairID := h.pairID(t)
			activeID, standbyID := tc.stale(t, h)

			err := h.svc.FailoverForTest(t.Context(), h.orgID, pairID, activeID, standbyID)

			require.ErrorIs(t, err, ha.ErrPairChangedForTest)
			assert.Equal(t, h.active, h.deviceNode(t, deviceID))
			assert.Equal(t, h.active, h.discoveredBy(t, deviceID))
			assert.Empty(t, h.invalidated)
			assert.Empty(t, h.failoverActivities(t))
		})
	}
}

func TestTick_KeepsDevicesWhileFailoverIsNotDue(t *testing.T) {
	tests := []struct {
		name                  string
		failoverDelay         time.Duration
		startupGrace          time.Duration
		ready                 bool
		connectActive         bool
		connectStandby        bool
		standbyAfter          time.Duration
		activeDisconnectAfter time.Duration
	}{
		{name: "within startup grace", startupGrace: time.Hour, ready: true, connectStandby: true},
		{name: "within failover delay", failoverDelay: time.Hour, ready: true, connectStandby: true},
		{name: "standby connected for less than the failover delay", failoverDelay: 300 * time.Millisecond, ready: true, connectStandby: true, standbyAfter: 400 * time.Millisecond},
		{name: "active just disconnected after long standby uptime", failoverDelay: 300 * time.Millisecond, ready: true, connectActive: true, connectStandby: true, activeDisconnectAfter: 400 * time.Millisecond},
		{name: "active connected", ready: true, connectActive: true, connectStandby: true},
		{name: "standby disconnected", ready: true},
		{name: "standby not ready", connectStandby: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.svc.SetTimingForTest(tc.failoverDelay, tc.startupGrace, time.Hour)
			h.createPair(t, tc.ready)
			deviceID := h.addDevice(t, h.active)
			var active *control.Stream
			if tc.connectActive {
				active = h.connect(t, h.active, v7)
			}
			time.Sleep(tc.standbyAfter)
			if tc.connectStandby {
				h.connect(t, h.standby, v7)
			}

			if tc.activeDisconnectAfter > 0 {
				time.Sleep(tc.activeDisconnectAfter)
				active.Unregister()
			}
			h.svc.TickForTest(t.Context(), time.Now())

			pair, ok := h.pair(t)
			require.True(t, ok)
			assert.Equal(t, h.active, pair.active)
			assert.Equal(t, h.active, h.deviceNode(t, deviceID))
			assert.Equal(t, h.active, h.discoveredBy(t, deviceID))
			assert.Empty(t, h.invalidated)
		})
	}
}

func TestTick_FingerprintMismatchKeepsStandbyNotReadyAndBacksOff(t *testing.T) {
	h := newHarness(t)
	h.createPair(t, false)
	activeKey := make([]byte, credentialkey.KeySize)
	otherKey := make([]byte, credentialkey.KeySize)
	otherKey[0] = 1
	respond(t, h.connectConfigured(t, h.active, v7), exportKey(activeKey))
	installs, _ := respondWithDiscoveryConfig(t, h.connect(t, h.standby, v8), func(*pb.AgentCommand) *pb.ControlAck {
		return keyResultAck(&pb.CredentialKeyResult{Fingerprint: credentialkey.Fingerprint(otherKey)})
	})

	h.svc.TickForTest(t.Context(), time.Now())
	h.svc.TickForTest(t.Context(), time.Now())

	pair, ok := h.pair(t)
	require.True(t, ok)
	assert.False(t, pair.ready)
	assert.Equal(t, ha.ReasonTransferFailed, pair.reason)
	assert.Equal(t, int32(1), installs.Load(), "the retry backoff suppresses the second attempt")
}

func TestTick_StandbyDiscoveryRangeConflictSurfacesDedicatedReason(t *testing.T) {
	// Arrange
	h := newHarness(t)
	h.createPair(t, false)
	respond(t, h.connectConfigured(t, h.active, v7), exportKey(make([]byte, credentialkey.KeySize)))
	respond(t, h.connect(t, h.standby, v8), func(*pb.AgentCommand) *pb.ControlAck {
		return &pb.ControlAck{Code: pb.AckCode_ACK_CODE_BAD_REQUEST, ErrorMessage: control.StandbyDiscoveryConflictMessage}
	})

	// Act
	h.svc.TickForTest(t.Context(), time.Now())

	// Assert
	pair, ok := h.pair(t)
	require.True(t, ok)
	assert.False(t, pair.ready)
	assert.Equal(t, ha.ReasonStandbyDiscoveryConflict, pair.reason)
}

func TestTick_KeyTransferFailuresKeepStandbyNotReady(t *testing.T) {
	key := make([]byte, credentialkey.KeySize)
	tests := []struct {
		name          string
		export        func(*pb.AgentCommand) *pb.ControlAck
		install       func(*pb.AgentCommand) *pb.ControlAck
		wantInstalled int32
	}{
		{
			name: "export fails",
			export: func(*pb.AgentCommand) *pb.ControlAck {
				return failedAck(errors.New("export refused"))
			},
		},
		{
			name: "export returns no sealed key",
			export: func(*pb.AgentCommand) *pb.ControlAck {
				return keyResultAck(&pb.CredentialKeyResult{Fingerprint: credentialkey.Fingerprint(key)})
			},
		},
		{
			name: "export payload is malformed",
			export: func(*pb.AgentCommand) *pb.ControlAck {
				return &pb.ControlAck{Succeeded: true, Code: pb.AckCode_ACK_CODE_OK, Payload: []byte{0xff, 0xff, 0xff}}
			},
		},
		{
			name:   "export result is invalid",
			export: exportKeyWithFingerprint(key, []byte{1, 2, 3}),
		},
		{
			name:   "install fails",
			export: exportKey(key),
			install: func(*pb.AgentCommand) *pb.ControlAck {
				return failedAck(errors.New("install refused"))
			},
			wantInstalled: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.createPair(t, false)
			exports := respond(t, h.connectConfigured(t, h.active, v7), tc.export)
			install := tc.install
			if install == nil {
				install = installKey(h.standbyKey)
			}
			installs, _ := respondWithDiscoveryConfig(t, h.connect(t, h.standby, v8), install)

			h.svc.TickForTest(t.Context(), time.Now())
			h.svc.TickForTest(t.Context(), time.Now())

			pair, ok := h.pair(t)
			require.True(t, ok)
			assert.False(t, pair.ready)
			assert.Equal(t, ha.ReasonTransferFailed, pair.reason)
			assert.Equal(t, int32(1), exports.Load(), "the retry backoff suppresses the second export")
			assert.Equal(t, tc.wantInstalled, installs.Load())
		})
	}
}

func TestTick_DoesNotStartSecondTransferWhileOneIsInFlight(t *testing.T) {
	h := newHarness(t)
	h.createPair(t, false)
	key := make([]byte, credentialkey.KeySize)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseExport := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseExport)
	active := h.connectConfigured(t, h.active, v7)
	exports := respond(t, active, func(command *pb.AgentCommand) *pb.ControlAck {
		select {
		case <-release:
		case <-t.Context().Done():
		}
		return exportKey(key)(command)
	})
	installs, _ := respondWithDiscoveryConfig(t, h.connect(t, h.standby, v8), installKey(h.standbyKey))
	firstTick := make(chan struct{})
	go func() {
		defer close(firstTick)
		h.svc.TickForTest(t.Context(), time.Now())
	}()
	require.Eventually(t, func() bool { return exports.Load() == 1 }, 10*time.Second, 5*time.Millisecond, "the first transfer should reach the active node")

	h.svc.TickForTest(t.Context(), time.Now())

	assert.Equal(t, int32(1), exports.Load())
	assert.Empty(t, active.Outgoing, "no second export is queued while the first is in flight")
	releaseExport()
	select {
	case <-firstTick:
	case <-time.After(10 * time.Second):
		require.Fail(t, "the first transfer did not finish after the export was released")
	}
	pair, ok := h.pair(t)
	require.True(t, ok)
	assert.True(t, pair.ready)
	assert.Equal(t, h.active, pair.active, "a transfer never moves devices")
	assert.Equal(t, int32(1), exports.Load())
	assert.Equal(t, int32(1), installs.Load())
}

func TestTick_RetriesTransferOnceBackoffExpires(t *testing.T) {
	h := newHarness(t)
	h.svc.SetTimingForTest(0, 0, 0)
	h.createPair(t, false)
	key := make([]byte, credentialkey.KeySize)
	exports := respond(t, h.connectConfigured(t, h.active, v7), exportKey(key))
	var attempts atomic.Int32
	install := installKey(h.standbyKey)
	_, rangeInstalls := respondWithDiscoveryConfig(t, h.connect(t, h.standby, v8), func(command *pb.AgentCommand) *pb.ControlAck {
		if attempts.Add(1) == 1 {
			return failedAck(errors.New("install refused"))
		}
		return install(command)
	})

	h.svc.TickForTest(t.Context(), time.Now())
	h.svc.TickForTest(t.Context(), time.Now())

	pair, ok := h.pair(t)
	require.True(t, ok)
	assert.True(t, pair.ready)
	assert.Empty(t, pair.reason)
	assert.Equal(t, int32(1), exports.Load(), "retry reuses the persisted export")
	assert.Equal(t, int32(2), attempts.Load(), "the failed installation is retried")
	assert.Equal(t, int32(1), rangeInstalls.Load(), "the pending key proves ranges were already acknowledged")
}

func TestTick_ReportsWhyStandbyIsNotReady(t *testing.T) {
	tests := []struct {
		name           string
		activeVersion  pb.CommandProtocolVersion
		standbyVersion pb.CommandProtocolVersion
		wantReason     string
	}{
		{name: "nodes disconnected", wantReason: ha.ReasonWaitingForNodes},
		{name: "standby predates HA", activeVersion: v7, standbyVersion: v6, wantReason: ha.ReasonUpgradeRequired},
		{name: "active predates HA", activeVersion: v6, standbyVersion: v7, wantReason: ha.ReasonUpgradeRequired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.createPair(t, false)
			if tc.activeVersion != 0 {
				h.connect(t, h.active, tc.activeVersion)
			}
			if tc.standbyVersion != 0 {
				h.connect(t, h.standby, tc.standbyVersion)
			}

			h.svc.TickForTest(t.Context(), time.Now())

			pair, ok := h.pair(t)
			require.True(t, ok)
			assert.False(t, pair.ready)
			assert.Equal(t, tc.wantReason, pair.reason)
		})
	}
}

func TestTick_NewSetupRequiresDiscoveryConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name           string
		standbyVersion pb.CommandProtocolVersion
		config         *pb.DiscoveryConfig
		wantReason     string
	}{
		{name: "legacy nodes without reported ranges", standbyVersion: v7, wantReason: "Upgrade standby Fleet Node to support discovery configuration installation"},
		{name: "active reports empty ranges", standbyVersion: v8, config: &pb.DiscoveryConfig{}, wantReason: "Active Fleet Node must report configured discovery ranges before standby setup"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			h := newHarness(t)
			h.createPair(t, false)
			active := h.connect(t, h.active, v7)
			active.SetDiscoveryConfig(tc.config)
			exports := respond(t, active, exportKey(make([]byte, credentialkey.KeySize)))
			installs := respond(t, h.connect(t, h.standby, tc.standbyVersion), installKey(h.standbyKey))

			// Act
			h.svc.TickForTest(t.Context(), time.Now())

			// Assert
			pair, ok := h.pair(t)
			require.True(t, ok)
			assert.False(t, pair.ready)
			assert.Equal(t, tc.wantReason, pair.reason)
			assert.Zero(t, exports.Load())
			assert.Zero(t, installs.Load())
		})
	}
}

func TestTick_LegacyPendingInstallDoesNotNeedActiveConfiguration(t *testing.T) {
	// Arrange
	h := newHarness(t)
	h.createPair(t, false)
	key := make([]byte, credentialkey.KeySize)
	sealed, err := credentialkey.Seal(h.standbyKey.PublicKey().Bytes(), key)
	require.NoError(t, err)
	pending, err := proto.Marshal(&pb.CredentialKeyResult{SealedKey: sealed, Fingerprint: credentialkey.Fingerprint(key)})
	require.NoError(t, err)
	// A key prepared by the prior server version must remain recoverable even
	// though it predates discovery configuration installation.
	h.exec(t, `UPDATE fleet_node_ha_pair SET pending_credential_key=$1 WHERE active_node_id=$2`, pending, h.active)
	h.exec(t, `UPDATE fleet_node SET enrollment_status='REVOKED' WHERE id=$1`, h.active)
	installs := respond(t, h.connect(t, h.standby, v7), installKey(h.standbyKey))

	// Act
	h.svc.TickForTest(t.Context(), time.Now())

	// Assert
	pair, ok := h.pair(t)
	require.True(t, ok)
	assert.True(t, pair.ready)
	assert.Equal(t, int32(1), installs.Load())
}

func TestHandOff(t *testing.T) {
	tests := []struct {
		name          string
		ready         bool
		revokeStandby bool
		wantMoved     bool
	}{
		{name: "ready standby takes the devices", ready: true, wantMoved: true},
		{name: "standby not ready", ready: false},
		{name: "revoking the standby", ready: true, revokeStandby: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.createPair(t, tc.ready)
			deviceID := h.addDevice(t, h.active)
			h.connect(t, h.standby, v7)
			revoked := h.active
			if tc.revokeStandby {
				revoked = h.standby
			}

			err := h.svc.HandOff(t.Context(), h.orgID, revoked)

			require.NoError(t, err)
			wantNode := h.active
			if tc.wantMoved {
				wantNode = h.standby
			}
			assert.Equal(t, wantNode, h.deviceNode(t, deviceID))
			pair, ok := h.pair(t)
			require.True(t, ok)
			assert.Equal(t, wantNode, pair.active)
		})
	}
}

func TestHandOff_IgnoresStandbyRevokedAfterPairWasRead(t *testing.T) {
	h := newHarness(t)
	h.createPair(t, true)
	deviceID := h.addDevice(t, h.active)
	h.exec(t, `UPDATE fleet_node SET enrollment_status='REVOKED' WHERE id=$1`, h.standby)

	err := h.svc.HandOff(t.Context(), h.orgID, h.active)

	require.NoError(t, err)
	assert.Equal(t, h.active, h.deviceNode(t, deviceID))
	assert.Equal(t, h.active, h.discoveredBy(t, deviceID))
	assert.Empty(t, h.invalidated)
	assert.Empty(t, h.failoverActivities(t))
}

func TestFailover_RechecksConnectivityAfterDeviceLockWait(t *testing.T) {
	for _, handoff := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic", true: "revoke hand-off"}[handoff], func(t *testing.T) {
			h := newHarness(t)
			h.createPair(t, true)
			deviceID := h.addDevice(t, h.active)
			standby := h.connect(t, h.standby, v7)
			call, err := h.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = call.Rollback() })
			_, err = call.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR SHARE`, deviceID)
			require.NoError(t, err)
			finished := make(chan error, 1)
			go func() {
				if handoff {
					finished <- h.svc.HandOff(t.Context(), h.orgID, h.active)
				} else {
					h.svc.TickForTest(t.Context(), time.Now())
					finished <- nil
				}
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := h.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%LockFleetNodeDevicesForRevocation%')`).Scan(&waiting)
				return err == nil && waiting
			}, 5*time.Second, 10*time.Millisecond)
			if handoff {
				standby.Unregister()
			} else {
				h.connect(t, h.active, v7)
			}
			require.NoError(t, call.Rollback())
			select {
			case err := <-finished:
				if handoff {
					require.Error(t, err, "revoke must leave the pair retryable when its standby disconnects")
				} else {
					require.NoError(t, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("failover did not finish after device lock was released")
			}
			assert.Equal(t, h.active, h.deviceNode(t, deviceID))
			assert.Equal(t, h.active, h.discoveredBy(t, deviceID))
			pair, ok := h.pair(t)
			require.True(t, ok)
			assert.Equal(t, h.active, pair.active)
			assert.Empty(t, h.failoverActivities(t))
		})
	}
}

func TestTick_BlockedFailoverDoesNotBlockAnotherOrganizationAndRetries(t *testing.T) {
	h := newHarness(t)
	h.svc.SetFailoverTimeoutForTest(5 * time.Second)
	h.svc.SetFailoverLockTimeoutForTest(500 * time.Millisecond)
	h.createPair(t, true)
	deviceID := h.addDevice(t, h.active)
	h.connect(t, h.standby, v7)
	other := h.createReadyPair(t, h.database.CreateSuperAdminUser2().OrganizationID)
	otherDevice := other.addDevice(t, other.active)
	h.connect(t, other.standby, v7)

	call, err := h.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = call.Rollback() })
	_, err = call.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR SHARE`, deviceID)
	require.NoError(t, err)
	waiting := func() int {
		var count int
		require.NoError(t, h.db.QueryRowContext(t.Context(), `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%LockFleetNodeDevicesForRevocation%'`).Scan(&count))
		return count
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		h.svc.TickForTest(t.Context(), time.Now())
	}()
	require.Eventually(t, func() bool {
		return waiting() == 1 && other.deviceNode(t, otherDevice) == other.standby
	}, 5*time.Second, 10*time.Millisecond, "another organization must fail over while the first device remains locked")

	secondTick := make(chan struct{})
	go func() {
		defer close(secondTick)
		h.svc.TickForTest(t.Context(), time.Now())
	}()
	select {
	case <-secondTick:
	case <-time.After(time.Second):
		t.Fatal("another tick started a duplicate failover for the locked pair")
	}
	assert.Equal(t, 1, waiting(), "only one transaction may wait for this pair")
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the automatic lock timeout did not cancel the lock wait")
	}
	require.Eventually(t, func() bool { return waiting() == 0 }, time.Second, 10*time.Millisecond)
	assert.Equal(t, h.active, h.deviceNode(t, deviceID), "the timed-out transaction must leave ownership unchanged")
	assert.Equal(t, h.active, h.discoveredBy(t, deviceID))
	assert.Empty(t, h.failoverActivities(t))

	require.NoError(t, call.Rollback())
	h.svc.TickForTest(t.Context(), time.Now())
	assert.Equal(t, h.standby, h.deviceNode(t, deviceID), "the next tick retries after the lock is released")
	assert.Equal(t, h.standby, h.discoveredBy(t, deviceID))
	assert.Len(t, h.failoverActivities(t), 1)
	assert.Len(t, other.failoverActivities(t), 1, "later ticks must not swap the completed pair again")
}

func TestTick_FailoverCapacityRotatesPastBlockedPairs(t *testing.T) {
	h := newHarness(t)
	h.svc.SetFailoverTimeoutForTest(2 * time.Second)
	h.svc.SetFailoverLockTimeoutForTest(200 * time.Millisecond)
	call, err := h.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = call.Rollback() })
	for range ha.MaxConcurrentFailoversForTest {
		pair := h.createReadyPair(t, h.orgID)
		deviceID := pair.addDevice(t, pair.active)
		h.connect(t, pair.standby, v7)
		_, err := call.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR SHARE`, deviceID)
		require.NoError(t, err)
	}
	other := h.createReadyPair(t, h.database.CreateSuperAdminUser2().OrganizationID)
	deviceID := other.addDevice(t, other.active)
	h.connect(t, other.standby, v7)

	h.svc.TickForTest(t.Context(), time.Now())
	assert.Equal(t, other.active, other.deviceNode(t, deviceID), "the first tick fills only the bounded worker slots")
	h.svc.TickForTest(t.Context(), time.Now())
	assert.Equal(t, other.standby, other.deviceNode(t, deviceID), "the next tick must reach the pair beyond the blocked workers")
}

func TestRun_CancelsAndWaitsForBlockedFailovers(t *testing.T) {
	h := newHarness(t)
	h.svc.SetFailoverTimeoutForTest(time.Hour)
	h.createPair(t, true)
	deviceID := h.addDevice(t, h.active)
	h.connect(t, h.standby, v7)
	call, err := h.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = call.Rollback() })
	_, err = call.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR SHARE`, deviceID)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		h.svc.Run(ctx)
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := h.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%LockFleetNodeDevicesForRevocation%')`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not cancel its blocked failover")
	}
	assert.Equal(t, h.active, h.deviceNode(t, deviceID))
	require.NoError(t, call.Rollback())
	h.svc.TickForTest(t.Context(), time.Now())
	assert.Equal(t, h.standby, h.deviceNode(t, deviceID), "Run must finish its workers and release the pair's in-flight claim before returning")
}

func TestTick_PendingInstallSurvivesTeardownAndRestart(t *testing.T) {
	h := newHarness(t)
	h.createPair(t, false)
	key := make([]byte, credentialkey.KeySize)
	active := h.connectConfigured(t, h.active, v7)
	exports := respond(t, active, exportKey(key))
	standby := h.connect(t, h.standby, v8)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		h.svc.TickForTest(ctx, time.Now())
	}()
	// The standby durably installs ranges before any credential key is exported.
	select {
	case cmd := <-standby.Outgoing:
		command := &pb.AgentCommand{}
		require.NoError(t, proto.Unmarshal(cmd.GetPayload(), command))
		require.Equal(t, []string{"10.0.0.1-10.0.0.4"}, command.GetInstallDiscoveryConfig().GetRanges())
		assert.Zero(t, exports.Load(), "ranges must be acknowledged before key export")
		ack := discoveryConfigAck(command)
		ack.CommandId = cmd.GetCommandId()
		standby.PublishAck(ack)
	case <-time.After(5 * time.Second):
		t.Fatal("standby did not receive discovery ranges")
	}
	var delayed *pb.ControlCommand
	select {
	case delayed = <-standby.Outgoing:
		command := &pb.AgentCommand{}
		require.NoError(t, proto.Unmarshal(delayed.GetPayload(), command))
		require.NotNil(t, command.GetInstallCredentialKey())
	case <-time.After(5 * time.Second):
		t.Fatal("standby did not receive installation")
	}
	h.exec(t, `UPDATE fleet_node SET enrollment_status='REVOKED' WHERE id=$1`, h.active)
	active.Unregister()
	requireCode(t, h.svc.DeletePair(t.Context(), h.orgID, h.active), connect.CodeFailedPrecondition)
	requireCode(t, h.svc.RequireNotStandby(t.Context(), h.orgID, h.standby), connect.CodeFailedPrecondition)
	requireCode(t, h.svc.CheckStandbyTarget(t.Context(), h.orgID, h.standby), connect.CodeFailedPrecondition)
	cancel() // A server timeout/cancellation cannot prove the node stopped installing.
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("canceled transfer did not return")
	}

	// Restart loses all connection metadata; only the standby reconnects.
	standby.Unregister()
	h.registry = control.NewRegistry()
	standby = h.connectConfigured(t, h.standby, v8)
	restarted := ha.NewService(h.db, sqlstores.NewSQLTransactor(h.db), h.registry, nil, nil)
	restarted.SetTimingForTest(0, 0, 0)
	retried := make(chan struct{})
	go func() {
		defer close(retried)
		restarted.TickForTest(t.Context(), time.Now())
	}()
	select {
	case cmd := <-standby.Outgoing:
		assert.Equal(t, delayed.GetPayload(), cmd.GetPayload(), "retry must install the same sealed key")
		command := &pb.AgentCommand{}
		require.NoError(t, proto.Unmarshal(cmd.GetPayload(), command))
		require.NotNil(t, command.GetInstallCredentialKey(), "pending recovery must not repeat discovery configuration installation")
		ack := installKey(h.standbyKey)(command)
		ack.CommandId = cmd.GetCommandId()
		standby.PublishAck(ack)
	case <-time.After(5 * time.Second):
		t.Fatal("restart did not retry pending installation without the active node")
	}
	select {
	case <-retried:
	case <-time.After(5 * time.Second):
		t.Fatal("retry did not finish")
	}
	pair, ok := h.pair(t)
	require.True(t, ok)
	require.True(t, pair.ready)
	assert.Equal(t, int32(1), exports.Load())
	requireCode(t, restarted.DeletePair(t.Context(), h.orgID, h.active), connect.CodeFailedPrecondition)
	require.NoError(t, restarted.HandOff(t.Context(), h.orgID, h.active))
	require.NoError(t, restarted.DeletePair(t.Context(), h.orgID, h.active))
	require.NoError(t, restarted.RequireNotStandby(t.Context(), h.orgID, h.standby))
	require.NoError(t, restarted.CheckStandbyTarget(t.Context(), h.orgID, h.standby))
}

func TestTick_DoesNotInstallAfterTeardownDuringExport(t *testing.T) {
	h := newHarness(t)
	h.createPair(t, false)
	active := h.connectConfigured(t, h.active, v7)
	standby := h.connect(t, h.standby, v8)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		h.svc.TickForTest(t.Context(), time.Now())
	}()
	select {
	case cmd := <-standby.Outgoing:
		command := &pb.AgentCommand{}
		require.NoError(t, proto.Unmarshal(cmd.GetPayload(), command))
		require.NotNil(t, command.GetInstallDiscoveryConfig())
		ack := discoveryConfigAck(command)
		ack.CommandId = cmd.GetCommandId()
		standby.PublishAck(ack)
	case <-time.After(5 * time.Second):
		t.Fatal("standby did not receive discovery ranges")
	}
	select {
	case cmd := <-active.Outgoing:
		require.NoError(t, h.svc.DeletePair(t.Context(), h.orgID, h.active))
		command := &pb.AgentCommand{}
		require.NoError(t, proto.Unmarshal(cmd.GetPayload(), command))
		ack := exportKey(make([]byte, credentialkey.KeySize))(command)
		ack.CommandId = cmd.GetCommandId()
		active.PublishAck(ack)
	case <-time.After(5 * time.Second):
		t.Fatal("active did not receive export")
	}
	select {
	case <-finished:
	case cmd := <-standby.Outgoing:
		t.Fatalf("installation sent after pair was dissolved: %s", cmd.GetCommandId())
	case <-time.After(5 * time.Second):
		t.Fatal("transfer did not stop after teardown")
	}
}
