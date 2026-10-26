package enrollment_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	fleetnodeenrollment "github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/ha"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

type haEnrollment struct {
	db         *sql.DB
	orgID      int64
	enrollment *fleetnodeenrollment.Service
	registry   *control.Registry
}

func setupHAEnrollmentTest(t *testing.T) *haEnrollment {
	t.Helper()
	db, _, orgID, enrollment, _ := setupEnrollmentTest(t)
	registry := control.NewRegistry()
	enrollment.WithHA(ha.NewService(db, sqlstores.NewSQLTransactor(db), registry, nil, nil))
	return &haEnrollment{db: db, orgID: orgID, enrollment: enrollment, registry: registry}
}

// register mints a code (a standby code when standbyFor is non-zero) and
// registers a new node with it, leaving the node awaiting confirmation.
func (h *haEnrollment) register(t *testing.T, name string, standbyFor int64) (nodeID, pendingEnrollmentID int64) {
	t.Helper()
	ctx := t.Context()
	var code string
	var err error
	if standbyFor == 0 {
		code, pendingEnrollmentID, _, err = h.enrollment.CreateCodeWithEnrollmentID(ctx, 1, h.orgID, time.Hour)
	} else {
		code, pendingEnrollmentID, _, err = h.enrollment.CreateStandbyCode(ctx, 1, h.orgID, time.Hour, standbyFor)
	}
	require.NoError(t, err)
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	node, _, err := h.enrollment.RegisterFleetNode(ctx, code, name, pubKey, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	return node.ID, pendingEnrollmentID
}

func (h *haEnrollment) enroll(t *testing.T, name string, standbyFor int64) int64 {
	t.Helper()
	nodeID, pendingEnrollmentID := h.register(t, name, standbyFor)
	_, _, err := h.enrollment.ConfirmExpected(t.Context(), nodeID, h.orgID, pendingEnrollmentID, 1)
	require.NoError(t, err)
	return nodeID
}

// pairOf returns the pair's active and standby node IDs, or ok=false.
func (h *haEnrollment) pairOf(t *testing.T, nodeID int64) (active, standby int64, ok bool) {
	t.Helper()
	err := h.db.QueryRowContext(t.Context(),
		`SELECT active_node_id, standby_node_id FROM fleet_node_ha_pair WHERE active_node_id=$1 OR standby_node_id=$1`, nodeID).Scan(&active, &standby)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false
	}
	require.NoError(t, err)
	return active, standby, true
}

func (h *haEnrollment) deviceNodes(t *testing.T, deviceID int64) []int64 {
	t.Helper()
	rows, err := h.db.QueryContext(t.Context(), `SELECT fleet_node_id FROM fleet_node_device WHERE device_id=$1`, deviceID)
	require.NoError(t, err)
	defer rows.Close()
	var nodes []int64
	for rows.Next() {
		var nodeID int64
		require.NoError(t, rows.Scan(&nodeID))
		nodes = append(nodes, nodeID)
	}
	require.NoError(t, rows.Err())
	return nodes
}

func requireFleetErrorCode(t *testing.T, err error, code connect.Code) {
	t.Helper()
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, code, fleetErr.GRPCCode)
}

func TestConfirmStandbyEnrollmentCreatesPair(t *testing.T) {
	// Arrange
	h := setupHAEnrollmentTest(t)
	active := h.enroll(t, "active", 0)

	// Act
	standby := h.enroll(t, "standby", active)

	// Assert
	pairActive, pairStandby, ok := h.pairOf(t, active)
	require.True(t, ok)
	assert.Equal(t, active, pairActive)
	assert.Equal(t, standby, pairStandby)
}

func TestCreateStandbyCodeRejectsNodeAlreadyInPair(t *testing.T) {
	h := setupHAEnrollmentTest(t)
	active := h.enroll(t, "active", 0)
	standby := h.enroll(t, "standby", active)

	for name, target := range map[string]int64{"active": active, "standby": standby} {
		t.Run(name, func(t *testing.T) {
			// Act
			_, _, _, err := h.enrollment.CreateStandbyCode(t.Context(), 1, h.orgID, time.Hour, target)

			// Assert
			requireFleetErrorCode(t, err, connect.CodeFailedPrecondition)
		})
	}
}

func TestConfirmSecondStandbyForSameNodeFails(t *testing.T) {
	// Arrange
	h := setupHAEnrollmentTest(t)
	active := h.enroll(t, "active", 0)
	first, firstPending := h.register(t, "standby-1", active)
	second, secondPending := h.register(t, "standby-2", active)
	_, _, err := h.enrollment.ConfirmExpected(t.Context(), first, h.orgID, firstPending, 1)
	require.NoError(t, err)

	// Act
	_, _, err = h.enrollment.ConfirmExpected(t.Context(), second, h.orgID, secondPending, 1)

	// Assert
	requireFleetErrorCode(t, err, connect.CodeFailedPrecondition)
	node, err := h.enrollment.GetFleetNodeByID(t.Context(), second, h.orgID)
	require.NoError(t, err)
	assert.Equal(t, fleetnodeenrollment.FleetNodeStatusPending, node.EnrollmentStatus, "the failed confirm rolls back")
	_, pairStandby, ok := h.pairOf(t, active)
	require.True(t, ok)
	assert.Equal(t, first, pairStandby)
}

func TestRevokeActiveNodeHandsDevicesToReadyStandby(t *testing.T) {
	// Arrange
	h := setupHAEnrollmentTest(t)
	active := h.enroll(t, "active", 0)
	standby := h.enroll(t, "standby", active)
	deviceID := insertFleetNodeDevice(t, h.db, h.orgID, active)
	_, err := h.db.ExecContext(t.Context(), `UPDATE fleet_node_ha_pair SET standby_ready=true WHERE active_node_id=$1`, active)
	require.NoError(t, err)
	stream, err := h.registry.RegisterAuthenticated(standby, "", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V6)
	require.NoError(t, err)
	t.Cleanup(stream.Unregister)
	var invalidated []int64
	h.enrollment.WithMinerInvalidator(func(_ context.Context, deviceID int64) {
		invalidated = append(invalidated, deviceID)
	})

	// Act
	err = h.enrollment.RevokeFleetNode(t.Context(), active, h.orgID)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []int64{standby}, h.deviceNodes(t, deviceID))
	assert.Empty(t, invalidated, "the revoked node had no devices left to tear down")
	_, _, ok := h.pairOf(t, standby)
	assert.False(t, ok, "revoking either node dissolves the pair")
}

func TestRevokeDissolvesPairWithoutHandOff(t *testing.T) {
	tests := []struct {
		name          string
		revokeStandby bool
		wantNodes     func(active, standby int64) []int64
	}{
		{name: "active with an unready standby", wantNodes: func(int64, int64) []int64 { return nil }},
		{name: "standby", revokeStandby: true, wantNodes: func(active, _ int64) []int64 { return []int64{active} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			h := setupHAEnrollmentTest(t)
			active := h.enroll(t, "active", 0)
			standby := h.enroll(t, "standby", active)
			deviceID := insertFleetNodeDevice(t, h.db, h.orgID, active)
			revoked, kept := active, standby
			if tc.revokeStandby {
				revoked, kept = standby, active
			}

			// Act
			err := h.enrollment.RevokeFleetNode(t.Context(), revoked, h.orgID)

			// Assert
			require.NoError(t, err)
			assert.Equal(t, tc.wantNodes(active, standby), h.deviceNodes(t, deviceID))
			_, _, ok := h.pairOf(t, kept)
			assert.False(t, ok)
			node, err := h.enrollment.GetFleetNodeByID(t.Context(), kept, h.orgID)
			require.NoError(t, err)
			assert.Equal(t, fleetnodeenrollment.FleetNodeStatusConfirmed, node.EnrollmentStatus)
		})
	}
}
