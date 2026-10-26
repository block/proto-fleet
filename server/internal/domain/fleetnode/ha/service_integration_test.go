package ha_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"errors"
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
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/credentialkey"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/ha"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

const (
	v5 = pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V5
	v6 = pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V6
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
	invalidated []int64
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
	h.svc = ha.NewService(h.db, sqlstores.NewSQLTransactor(h.db), h.registry, nil, func(_ context.Context, deviceID int64) {
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
		_, err := h.db.ExecContext(t.Context(), `UPDATE fleet_node_ha_pair SET standby_ready=true, status_reason='' WHERE active_node_id=$1`, h.active)
		require.NoError(t, err)
	}
}

// addDevice pairs a new device to nodeID and attributes its discovery to it.
func (h *harness) addDevice(t *testing.T, nodeID int64) int64 {
	t.Helper()
	device := h.database.CreateDevice(h.orgID, "proto")
	_, err := h.db.ExecContext(t.Context(), `INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,$3)`, nodeID, device.DatabaseID, h.orgID)
	require.NoError(t, err)
	_, err = h.db.ExecContext(t.Context(), `UPDATE discovered_device SET discovered_by_fleet_node_id=$1 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$2)`, nodeID, device.DatabaseID)
	require.NoError(t, err)
	return device.DatabaseID
}

func (h *harness) connect(t *testing.T, nodeID int64, version pb.CommandProtocolVersion) *control.Stream {
	t.Helper()
	stream, err := h.registry.RegisterAuthenticated(nodeID, "", version)
	require.NoError(t, err)
	t.Cleanup(stream.Unregister)
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

// respond plays a fleet node: it answers each command sent to stream and
// returns how many it received.
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

func failedAck(err error) *pb.ControlAck {
	return &pb.ControlAck{Code: pb.AckCode_ACK_CODE_BAD_REQUEST, ErrorMessage: err.Error()}
}

// exportKey answers ExportCredentialKey the way a node holding key does.
func exportKey(key []byte) func(*pb.AgentCommand) *pb.ControlAck {
	return func(command *pb.AgentCommand) *pb.ControlAck {
		sealed, err := credentialkey.Seal(command.GetExportCredentialKey().GetRecipientPublicKey(), key)
		if err != nil {
			return failedAck(err)
		}
		return keyResultAck(&pb.CredentialKeyResult{SealedKey: sealed, Fingerprint: credentialkey.Fingerprint(key)})
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
			// Act
			err := h.svc.CheckStandbyTarget(t.Context(), h.orgID, tc.targetID)

			// Assert
			if tc.wantCode == 0 {
				require.NoError(t, err)
				return
			}
			requireCode(t, err, tc.wantCode)
		})
	}
}

func TestCreatePair_RejectsSecondStandbyForTheSameNode(t *testing.T) {
	// Arrange
	h := newHarness(t)
	h.createPair(t, false)
	second := h.createNode(t, "second-standby", "CONFIRMED", make([]byte, 32))

	// Act
	err := sqlstores.NewSQLTransactor(h.db).RunInTx(t.Context(), func(ctx context.Context) error {
		return h.svc.CreatePair(ctx, h.orgID, h.active, second)
	})

	// Assert
	requireCode(t, err, connect.CodeFailedPrecondition)
	pair, ok := h.pair(t)
	require.True(t, ok)
	assert.Equal(t, h.standby, pair.standby)
}

func TestCreatePair_RequiresTransaction(t *testing.T) {
	// Arrange
	h := newHarness(t)

	// Act
	err := h.svc.CreatePair(t.Context(), h.orgID, h.active, h.standby)

	// Assert
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
			// Act
			err := h.svc.RequireNotStandby(t.Context(), h.orgID, tc.nodeID)

			// Assert
			if tc.wantCode == 0 {
				require.NoError(t, err)
				return
			}
			requireCode(t, err, tc.wantCode)
		})
	}
}

func TestTick_FailsOverReadyPairWhenActiveStaysDisconnected(t *testing.T) {
	// Arrange
	h := newHarness(t)
	h.createPair(t, true)
	deviceID := h.addDevice(t, h.active)
	h.connect(t, h.standby, v6)

	// Act
	h.svc.TickForTest(t.Context(), time.Now())

	// Assert
	pair, ok := h.pair(t)
	require.True(t, ok)
	assert.Equal(t, pairRow{active: h.standby, standby: h.active, ready: true}, pair, "the old active node becomes the ready standby")
	assert.Equal(t, h.standby, h.deviceNode(t, deviceID))
	assert.Equal(t, h.standby, h.discoveredBy(t, deviceID))
	assert.Equal(t, []int64{deviceID}, h.invalidated)
}

func TestTick_KeepsDevicesWhileFailoverIsNotDue(t *testing.T) {
	tests := []struct {
		name           string
		failoverDelay  time.Duration
		startupGrace   time.Duration
		ready          bool
		connectActive  bool
		connectStandby bool
	}{
		{name: "within startup grace", startupGrace: time.Hour, ready: true, connectStandby: true},
		{name: "within failover delay", failoverDelay: time.Hour, ready: true, connectStandby: true},
		{name: "active connected", ready: true, connectActive: true, connectStandby: true},
		{name: "standby disconnected", ready: true},
		{name: "standby not ready", connectStandby: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			h := newHarness(t)
			h.svc.SetTimingForTest(tc.failoverDelay, tc.startupGrace, time.Hour)
			h.createPair(t, tc.ready)
			deviceID := h.addDevice(t, h.active)
			if tc.connectActive {
				h.connect(t, h.active, v6)
			}
			if tc.connectStandby {
				h.connect(t, h.standby, v6)
			}

			// Act
			h.svc.TickForTest(t.Context(), time.Now())

			// Assert
			pair, ok := h.pair(t)
			require.True(t, ok)
			assert.Equal(t, h.active, pair.active)
			assert.Equal(t, h.active, h.deviceNode(t, deviceID))
			assert.Equal(t, h.active, h.discoveredBy(t, deviceID))
			assert.Empty(t, h.invalidated)
		})
	}
}

func TestTick_TransfersCredentialKeyToNewStandby(t *testing.T) {
	// Arrange
	h := newHarness(t)
	h.createPair(t, false)
	key := make([]byte, credentialkey.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	respond(t, h.connect(t, h.active, v6), exportKey(key))
	respond(t, h.connect(t, h.standby, v6), func(command *pb.AgentCommand) *pb.ControlAck {
		installed, err := credentialkey.Open(h.standbyKey.Bytes(), command.GetInstallCredentialKey().GetSealedKey())
		if err != nil {
			return failedAck(err)
		}
		return keyResultAck(&pb.CredentialKeyResult{Fingerprint: credentialkey.Fingerprint(installed)})
	})

	// Act
	h.svc.TickForTest(t.Context(), time.Now())

	// Assert
	pair, ok := h.pair(t)
	require.True(t, ok)
	assert.True(t, pair.ready)
	assert.Equal(t, h.active, pair.active, "a transfer never moves devices")
}

func TestTick_FingerprintMismatchKeepsStandbyNotReadyAndBacksOff(t *testing.T) {
	// Arrange
	h := newHarness(t)
	h.createPair(t, false)
	activeKey := make([]byte, credentialkey.KeySize)
	otherKey := make([]byte, credentialkey.KeySize)
	otherKey[0] = 1
	respond(t, h.connect(t, h.active, v6), exportKey(activeKey))
	installs := respond(t, h.connect(t, h.standby, v6), func(*pb.AgentCommand) *pb.ControlAck {
		return keyResultAck(&pb.CredentialKeyResult{Fingerprint: credentialkey.Fingerprint(otherKey)})
	})

	// Act
	h.svc.TickForTest(t.Context(), time.Now())
	h.svc.TickForTest(t.Context(), time.Now())

	// Assert
	pair, ok := h.pair(t)
	require.True(t, ok)
	assert.False(t, pair.ready)
	assert.Equal(t, ha.ReasonTransferFailed, pair.reason)
	assert.Equal(t, int32(1), installs.Load(), "the retry backoff suppresses the second attempt")
}

func TestTick_ReportsWhyStandbyIsNotReady(t *testing.T) {
	tests := []struct {
		name           string
		activeVersion  pb.CommandProtocolVersion
		standbyVersion pb.CommandProtocolVersion
		wantReason     string
	}{
		{name: "nodes disconnected", wantReason: ha.ReasonWaitingForNodes},
		{name: "standby predates HA", activeVersion: v6, standbyVersion: v5, wantReason: ha.ReasonUpgradeRequired},
		{name: "active predates HA", activeVersion: v5, standbyVersion: v6, wantReason: ha.ReasonUpgradeRequired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			h := newHarness(t)
			h.createPair(t, false)
			if tc.activeVersion != 0 {
				h.connect(t, h.active, tc.activeVersion)
			}
			if tc.standbyVersion != 0 {
				h.connect(t, h.standby, tc.standbyVersion)
			}

			// Act
			h.svc.TickForTest(t.Context(), time.Now())

			// Assert
			pair, ok := h.pair(t)
			require.True(t, ok)
			assert.False(t, pair.ready)
			assert.Equal(t, tc.wantReason, pair.reason)
		})
	}
}

func TestHandOffBeforeRevoke(t *testing.T) {
	tests := []struct {
		name           string
		ready          bool
		connectStandby bool
		revokeStandby  bool
		wantMoved      bool
	}{
		{name: "ready connected standby takes the devices", ready: true, connectStandby: true, wantMoved: true},
		{name: "standby not ready", connectStandby: true},
		{name: "standby disconnected", ready: true},
		{name: "revoking the standby", ready: true, connectStandby: true, revokeStandby: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			h := newHarness(t)
			h.createPair(t, tc.ready)
			deviceID := h.addDevice(t, h.active)
			h.connect(t, h.active, v6)
			if tc.connectStandby {
				h.connect(t, h.standby, v6)
			}
			revoked := h.active
			if tc.revokeStandby {
				revoked = h.standby
			}

			// Act
			err := h.svc.HandOffBeforeRevoke(t.Context(), h.orgID, revoked)

			// Assert
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

func TestDeletePair_RemovesThePairForEitherNode(t *testing.T) {
	for _, revokeStandby := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "standby"}[revokeStandby], func(t *testing.T) {
			// Arrange
			h := newHarness(t)
			h.createPair(t, true)
			nodeID := h.active
			if revokeStandby {
				nodeID = h.standby
			}

			// Act
			err := h.svc.DeletePair(t.Context(), h.orgID, nodeID)

			// Assert
			require.NoError(t, err)
			_, ok := h.pair(t)
			assert.False(t, ok)
		})
	}
}
