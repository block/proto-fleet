package admin_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/discovery"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/admin"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/testutil"
)

type pairingHarness struct {
	handler    *admin.Handler
	db         *sql.DB
	orgID      int64
	enrollment *enrollment.Service
	registry   *control.Registry
	pairing    *pairing.Service
}

func newPairingHarness(t *testing.T) *pairingHarness {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := testutil.GetTestDB(t)
	_, err := db.Exec(`INSERT INTO organization (id, org_id, name) VALUES (1, 'test-org', 'Test Org') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO "user" (id, user_id, username, password_hash) VALUES (1, 'test-user', 'op', 'dummy') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)

	apiKeyStore := sqlstores.NewSQLApiKeyStore(db)
	apiKeySvc := apikey.NewService(apiKeyStore, nil)
	transactor := sqlstores.NewSQLTransactor(db)
	enrollmentStore := sqlstores.NewSQLFleetNodeEnrollmentStore(db)
	enrollmentSvc := enrollment.NewService(enrollmentStore, apiKeySvc, transactor, nil)
	pairingStore := sqlstores.NewSQLFleetNodePairingStore(db)
	registry := control.NewRegistry()
	pairingSvc := pairing.NewService(pairingStore, enrollmentStore, transactor).
		WithProvisioning(sqlstores.NewSQLDeviceStore(db), sqlstores.NewSQLDiscoveredDeviceStore(db), registry)

	discoverySvc := discovery.NewService(registry, enrollmentSvc)
	return &pairingHarness{
		handler:    admin.NewHandler(enrollmentSvc, pairingSvc, discoverySvc),
		db:         db,
		orgID:      1,
		enrollment: enrollmentSvc,
		registry:   registry,
		pairing:    pairingSvc,
	}
}

func (h *pairingHarness) ctxWithPerms(perms ...string) context.Context {
	ctx := authn.SetInfo(context.Background(), &session.Info{
		AuthMethod:     session.AuthMethodSession,
		OrganizationID: h.orgID,
		UserID:         1,
	})
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  perms,
	}}))
}

func (h *pairingHarness) adminCtx() context.Context {
	return h.ctxWithPerms(authz.PermFleetnodeManage, authz.PermFleetnodeRead)
}

func (h *pairingHarness) viewerCtx() context.Context {
	return h.ctxWithPerms()
}

func (h *pairingHarness) createFleetNode(t *testing.T, name string) int64 {
	t.Helper()
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	code, _, err := h.enrollment.CreateCode(context.Background(), 1, h.orgID, time.Hour)
	require.NoError(t, err)
	node, _, err := h.enrollment.RegisterFleetNode(context.Background(), code, name, pubKey)
	require.NoError(t, err)
	_, _, err = h.enrollment.Confirm(context.Background(), node.ID, h.orgID)
	require.NoError(t, err)
	return node.ID
}

func (h *pairingHarness) insertDevice(t *testing.T) int64 {
	t.Helper()
	var ddID int64
	err := h.db.QueryRow(`INSERT INTO discovered_device (org_id, device_identifier, ip_address, port, url_scheme, driver_name, is_active)
		VALUES ($1, gen_random_uuid()::text, '10.0.0.1', '80', 'http', 'virtual', TRUE) RETURNING id`, h.orgID).Scan(&ddID)
	require.NoError(t, err)
	var devID int64
	err = h.db.QueryRow(`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		fmt.Sprintf("dev-%d", ddID),
		fmt.Sprintf("aa:bb:cc:00:01:%02x", ddID%256),
		fmt.Sprintf("sn-%d", ddID),
		h.orgID,
		ddID,
	).Scan(&devID)
	require.NoError(t, err)
	return devID
}

func TestPairDeviceToFleetNode_HappyPath(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-pair-1")
	deviceID := h.insertDevice(t)

	// Act
	_, err := h.handler.PairDeviceToFleetNode(h.adminCtx(), connect.NewRequest(&pb.PairDeviceToFleetNodeRequest{
		FleetNodeId: fleetNodeID,
		DeviceId:    deviceID,
	}))

	// Assert
	require.NoError(t, err)
	var paired int
	require.NoError(t, h.db.QueryRow(`SELECT COUNT(*) FROM fleet_node_device WHERE fleet_node_id = $1 AND device_id = $2`, fleetNodeID, deviceID).Scan(&paired))
	assert.Equal(t, 1, paired)
}

func TestPairDeviceToFleetNode_RequiresAdminSession(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)

	// Act
	_, err := h.handler.PairDeviceToFleetNode(h.viewerCtx(), connect.NewRequest(&pb.PairDeviceToFleetNodeRequest{FleetNodeId: 1, DeviceId: 1}))

	// Assert
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestPairDeviceToFleetNode_RejectsUnknownFleetNode(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)

	// Act
	_, err := h.handler.PairDeviceToFleetNode(h.adminCtx(), connect.NewRequest(&pb.PairDeviceToFleetNodeRequest{
		FleetNodeId: 99999,
		DeviceId:    99999,
	}))

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

func TestUnpairDevice_HappyPath(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-unpair-1")
	deviceID := h.insertDevice(t)
	_, err := h.handler.PairDeviceToFleetNode(h.adminCtx(), connect.NewRequest(&pb.PairDeviceToFleetNodeRequest{FleetNodeId: fleetNodeID, DeviceId: deviceID}))
	require.NoError(t, err)

	// Act
	_, err = h.handler.UnpairDevice(h.adminCtx(), connect.NewRequest(&pb.UnpairDeviceRequest{DeviceId: deviceID}))

	// Assert
	require.NoError(t, err)
	var remaining int
	require.NoError(t, h.db.QueryRow(`SELECT COUNT(*) FROM fleet_node_device WHERE device_id = $1`, deviceID).Scan(&remaining))
	assert.Equal(t, 0, remaining)
}

func TestUnpairDevice_RequiresAdminSession(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)

	// Act
	_, err := h.handler.UnpairDevice(h.viewerCtx(), connect.NewRequest(&pb.UnpairDeviceRequest{DeviceId: 1}))

	// Assert
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestListFleetNodeDevices_HappyPath(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)
	fleetNodeID := h.createFleetNode(t, "admin-list-1")
	deviceID := h.insertDevice(t)
	_, err := h.handler.PairDeviceToFleetNode(h.adminCtx(), connect.NewRequest(&pb.PairDeviceToFleetNodeRequest{FleetNodeId: fleetNodeID, DeviceId: deviceID}))
	require.NoError(t, err)

	// Act
	resp, err := h.handler.ListFleetNodeDevices(h.adminCtx(), connect.NewRequest(&pb.ListFleetNodeDevicesRequest{FleetNodeId: fleetNodeID}))

	// Assert
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetPairs(), 1)
	assert.Equal(t, fleetNodeID, resp.Msg.GetPairs()[0].GetFleetNodeId())
	assert.Equal(t, deviceID, resp.Msg.GetPairs()[0].GetDeviceId())
}

func TestListFleetNodeDevices_RequiresAdminSession(t *testing.T) {
	// Arrange
	h := newPairingHarness(t)

	// Act
	_, err := h.handler.ListFleetNodeDevices(h.viewerCtx(), connect.NewRequest(&pb.ListFleetNodeDevicesRequest{}))

	// Assert
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}
