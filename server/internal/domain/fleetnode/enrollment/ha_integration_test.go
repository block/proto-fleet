package enrollment_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/ha"
	"github.com/stretchr/testify/require"
)

func TestStandbyIntentStaysWithCanonicalOwnerAcrossRevocationAndPromotion(t *testing.T) {
	conn, userID, orgID, enroll, auth := setupEnrollmentTest(t)
	registry := control.NewRegistry()
	coordinator := ha.NewService(conn, registry)
	enroll.WithHA(coordinator)
	enroll.WithControlStreamInvalidator(registry.RevokeSession)
	register := func(code, name string) (*enrollment.FleetNode, ed25519.PublicKey) {
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		node, _, err := enroll.RegisterFleetNode(t.Context(), code, name, pub, []byte("01234567890123456789012345678901"))
		require.NoError(t, err)
		return node, pub
	}
	code, id, _, err := enroll.CreateCodeWithEnrollmentID(t.Context(), userID, orgID, time.Hour)
	require.NoError(t, err)
	owner, pub := register(code, "owner")
	ownerKey, _, err := enroll.ConfirmExpected(t.Context(), owner.ID, orgID, id, userID)
	require.NoError(t, err)
	fp := bytes.Repeat([]byte{7}, 32)
	ownerStream := registry.Register(owner.ID)
	defer ownerStream.Unregister()
	require.NoError(t, coordinator.ConnectStream(t.Context(), owner.ID, orgID, &pb.ControlHello{HaCapable: true, InstanceId: "owner-process", CredentialKeyFingerprint: fp}, "owner-session", ownerStream))
	// Both intents are minted before a group exists. Neither may later retarget
	// the physical standby that becomes active.
	codeA, idA, _, err := enroll.CreateCodeForStandby(t.Context(), userID, orgID, time.Hour, &owner.ID)
	require.NoError(t, err)
	codeB, idB, _, err := enroll.CreateCodeForStandby(t.Context(), userID, orgID, time.Hour, &owner.ID)
	require.NoError(t, err)
	standby, _ := register(codeA, "standby")
	_, _, err = enroll.ConfirmExpected(t.Context(), standby.ID, orgID, idA, userID)
	require.NoError(t, err)
	// The installed key is proved by this new Hello. Activation is exercised
	// independently; enable the group here to isolate enrollment and revocation.
	standbyStream := registry.Register(standby.ID)
	defer standbyStream.Unregister()
	require.NoError(t, coordinator.ConnectStream(t.Context(), standby.ID, orgID, &pb.ControlHello{HaCapable: true, InstanceId: "standby-process", CredentialKeyFingerprint: fp}, "standby-session", standbyStream))
	_, err = conn.Exec(`UPDATE fleet_node_ha_group SET enabled=true WHERE owner_node_id=$1`, owner.ID)
	require.NoError(t, err)
	first, err := coordinator.Lease(t.Context(), owner.ID, orgID, "owner-process", "owner-session", &pb.HALeaseRequest{RequestId: "grant"})
	require.NoError(t, err)
	_, err = coordinator.Lease(t.Context(), owner.ID, orgID, "owner-process", "owner-session", &pb.HALeaseRequest{RequestId: "ack", AcknowledgedEpoch: first.GetLeaseEpoch()})
	require.NoError(t, err)
	deviceID := insertFleetNodeDevice(t, conn, orgID, owner.ID)
	_, err = conn.Exec(`INSERT INTO miner_credentials(device_id,username_enc,password_enc) VALUES($1,$2,$3)`, deviceID, "sealed-user", "sealed-password")
	require.NoError(t, err)
	require.NoError(t, enroll.RevokeFleetNode(t.Context(), owner.ID, orgID))
	_, _, err = auth.BeginHandshake(t.Context(), ownerKey, pub)
	require.Error(t, err, "revoked original physical node loses authentication")
	var deleted sql.NullTime
	require.NoError(t, conn.QueryRow(`SELECT deleted_at FROM fleet_node WHERE id=$1`, owner.ID).Scan(&deleted))
	require.False(t, deleted.Valid, "canonical owner metadata remains while a member survives")
	var count int
	require.NoError(t, conn.QueryRow(`SELECT count(*) FROM fleet_node_device WHERE fleet_node_id=$1 AND device_id=$2`, owner.ID, deviceID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, conn.QueryRow(`SELECT count(*) FROM miner_credentials WHERE device_id=$1`, deviceID).Scan(&count))
	require.Equal(t, 1, count)
	_, err = conn.Exec(`UPDATE fleet_node_ha_group SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE owner_node_id=$1`, owner.ID)
	require.NoError(t, err)
	grant, err := coordinator.Lease(t.Context(), standby.ID, orgID, "standby-process", "standby-session", &pb.HALeaseRequest{RequestId: "promote"})
	require.NoError(t, err)
	require.True(t, grant.GetGranted())
	_, err = coordinator.Lease(t.Context(), standby.ID, orgID, "standby-process", "standby-session", &pb.HALeaseRequest{RequestId: "promote-ack", AcknowledgedEpoch: grant.GetLeaseEpoch()})
	require.NoError(t, err)
	ownerID, reason, err := coordinator.Eligible(t.Context(), standby.ID, orgID)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.Equal(t, owner.ID, ownerID)
	second, _ := register(codeB, "second-standby")
	_, _, err = enroll.ConfirmExpected(t.Context(), second.ID, orgID, idB, userID)
	require.NoError(t, err)
	var actualOwner int64
	require.NoError(t, conn.QueryRow(`SELECT owner_node_id FROM fleet_node_ha_member WHERE node_id=$1`, second.ID).Scan(&actualOwner))
	require.Equal(t, owner.ID, actualOwner)
	// Nonfinal revocation preserves the shared set; the final member performs
	// the existing teardown against the canonical owner, not its physical ID.
	require.NoError(t, enroll.RevokeFleetNode(t.Context(), second.ID, orgID))
	require.NoError(t, enroll.RevokeFleetNode(t.Context(), standby.ID, orgID))
	require.NoError(t, conn.QueryRow(`SELECT count(*) FROM fleet_node_device WHERE fleet_node_id=$1`, owner.ID).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, conn.QueryRow(`SELECT count(*) FROM miner_credentials WHERE device_id=$1`, deviceID).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, conn.QueryRow(`SELECT deleted_at FROM fleet_node WHERE id=$1`, owner.ID).Scan(&deleted))
	require.True(t, deleted.Valid)
}

func TestStandbyEnrollmentRejectsCrossOrganizationAndLegacySource(t *testing.T) {
	conn, userID, orgID, enroll, _ := setupEnrollmentTest(t)
	registry := control.NewRegistry()
	coordinator := ha.NewService(conn, registry)
	enroll.WithHA(coordinator)
	code, id, _, err := enroll.CreateCodeWithEnrollmentID(t.Context(), userID, orgID, time.Hour)
	require.NoError(t, err)
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	node, _, err := enroll.RegisterFleetNode(t.Context(), code, "legacy", pub, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	_, _, err = enroll.ConfirmExpected(t.Context(), node.ID, orgID, id, userID)
	require.NoError(t, err)
	stream := registry.Register(node.ID)
	defer stream.Unregister()
	require.NoError(t, coordinator.ConnectStream(t.Context(), node.ID, orgID, &pb.ControlHello{}, "legacy-session", stream))
	_, _, _, err = enroll.CreateCodeForStandby(t.Context(), userID, orgID, time.Hour, &node.ID)
	require.ErrorContains(t, err, "upgrade required")
	_, _, _, err = enroll.CreateCodeForStandby(t.Context(), userID, orgID+1, time.Hour, &node.ID)
	require.Error(t, err)
	var pending int
	require.NoError(t, conn.QueryRow(`SELECT count(*) FROM pending_enrollment WHERE status='PENDING'`).Scan(&pending))
	require.Zero(t, pending, "rejected intents never mint an orphan code")
}
