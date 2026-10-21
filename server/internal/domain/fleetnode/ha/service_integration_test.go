package ha_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/ha"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type harness struct {
	db          *sql.DB
	svc         *ha.Service
	registry    *control.Registry
	nodes       []int64
	fingerprint []byte
}

func setup(t *testing.T, count int) *harness {
	t.Helper()
	if testing.Short() {
		t.Skip("requires Postgres/Timescale")
	}
	conn := dbtest.GetTestDB(t)
	_, err := conn.Exec(`INSERT INTO organization(id,org_id,name) VALUES(1,'ha-test','HA Test')`)
	require.NoError(t, err)
	h := &harness{db: conn, registry: control.NewRegistry(), fingerprint: bytes.Repeat([]byte{7}, 32)}
	h.svc = ha.NewService(conn, h.registry)
	q := sqlc.New(conn)
	for i := range count {
		n, err := q.CreateFleetNode(t.Context(), sqlc.CreateFleetNodeParams{OrgID: 1, Name: fmt.Sprintf("node-%d", i), IdentityPubkey: bytes.Repeat([]byte{byte(i + 1)}, 32), EncryptionPubkey: bytes.Repeat([]byte{byte(i + 10)}, 32)})
		require.NoError(t, err)
		_, err = q.SetFleetNodeEnrollmentStatus(t.Context(), sqlc.SetFleetNodeEnrollmentStatusParams{ID: n.ID, OrgID: 1, EnrollmentStatus: "CONFIRMED"})
		require.NoError(t, err)
		h.nodes = append(h.nodes, n.ID)
	}
	return h
}
func (h *harness) connect(t *testing.T, index int, instance, session string) {
	t.Helper()
	handle := h.registry.Register(h.nodes[index])
	t.Cleanup(handle.Unregister)
	require.NoError(t, h.svc.Connect(t.Context(), h.nodes[index], 1, &pb.ControlHello{HaCapable: true, InstanceId: instance, CredentialKeyFingerprint: h.fingerprint}, session))
}
func (h *harness) group(t *testing.T) {
	t.Helper()
	tx := sqlstores.NewSQLTransactor(h.db)
	require.NoError(t, tx.RunInTx(t.Context(), func(ctx context.Context) error { return h.svc.Confirm(ctx, h.nodes[1], 1, h.nodes[0]) }))
	for _, nodeID := range h.nodes[2:] {
		require.NoError(t, sqlc.New(h.db).CreateFleetNodeHAMember(t.Context(), sqlc.CreateFleetNodeHAMemberParams{NodeID: nodeID, OrgID: 1, OwnerNodeID: h.nodes[0], Ready: true, StatusReason: ""}))
	}
	_, err := h.db.Exec(`UPDATE fleet_node_ha_member SET ready=true,status_reason=''`)
	require.NoError(t, err)
}
func (h *harness) lease(t *testing.T, index int, instance, session string, ack int64) *pb.HALeaseResponse {
	t.Helper()
	resp, err := h.svc.Lease(t.Context(), h.nodes[index], 1, instance, session, &pb.HALeaseRequest{RequestId: "lease", AcknowledgedEpoch: ack})
	require.NoError(t, err)
	return resp
}

func TestLeaseAcknowledgementExclusiveElectionAndNoFailback(t *testing.T) {
	h := setup(t, 3)
	for i := range h.nodes {
		h.connect(t, i, fmt.Sprintf("p%d", i), fmt.Sprintf("s%d", i))
	}
	h.group(t)
	initialOwner, err := h.svc.EffectiveOwner(t.Context(), h.nodes[0], 1)
	require.NoError(t, err, "admitted source reports must survive group creation")
	require.Equal(t, h.nodes[0], initialOwner)
	_, err = h.svc.EffectiveOwner(t.Context(), h.nodes[1], 1)
	require.Error(t, err, "standbys cannot borrow the source's adoption allowance")
	first := h.lease(t, 0, "p0", "s0", 0)
	require.True(t, first.GetGranted())
	require.Equal(t, int64(1), first.GetLeaseEpoch())
	_, err = h.svc.Resolve(t.Context(), h.nodes[0], 1)
	require.Error(t, err, "grant is not routable before acknowledgement")
	_, err = h.svc.EffectiveOwner(t.Context(), h.nodes[0], 1)
	require.NoError(t, err, "already-admitted reports may finish before the first acknowledgement")
	h.lease(t, 0, "p0", "s0", first.GetLeaseEpoch())
	route, err := h.svc.Resolve(t.Context(), h.nodes[0], 1)
	require.NoError(t, err)
	require.Equal(t, h.nodes[0], route.PhysicalNodeID)
	require.False(t, h.lease(t, 1, "p1", "s1", 0).GetGranted(), "standby cannot preempt a current holder")
	require.NoError(t, h.svc.Disconnect(t.Context(), h.nodes[0], 1, "s0"))
	_, err = h.db.Exec(`UPDATE fleet_node_ha_group SET lease_expires_at=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	var wg sync.WaitGroup
	grants := make(chan int64, 8)
	for i := range 8 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			r, e := h.svc.Lease(t.Context(), h.nodes[index], 1, fmt.Sprintf("p%d", index), fmt.Sprintf("s%d", index), &pb.HALeaseRequest{RequestId: "race"})
			if e == nil && r.GetGranted() {
				grants <- h.nodes[index]
			}
		}(1 + i%2)
	}
	wg.Wait()
	close(grants)
	granted := 0
	for node := range grants {
		granted++
		require.Equal(t, h.nodes[1], node, "oldest ready member wins every race")
	}
	require.Positive(t, granted)
	next := h.lease(t, 1, "p1", "s1", 0)
	require.Equal(t, int64(2), next.GetLeaseEpoch())
	h.lease(t, 1, "p1", "s1", next.GetLeaseEpoch())
	h.connect(t, 0, "returned", "s0-new")
	require.False(t, h.lease(t, 0, "returned", "s0-new", 0).GetGranted(), "returning owner cannot trigger failback")
	route, err = h.svc.Resolve(t.Context(), h.nodes[0], 1)
	require.NoError(t, err)
	require.Equal(t, h.nodes[1], route.PhysicalNodeID)
	owner, err := h.svc.EffectiveOwner(t.Context(), h.nodes[1], 1)
	require.NoError(t, err)
	require.Equal(t, h.nodes[0], owner)
	_, err = h.svc.EffectiveOwner(t.Context(), h.nodes[0], 1)
	require.Error(t, err, "former active cannot attribute reports")
}

func TestRestartCannotInheritLeaseAndHeartbeatCannotRenew(t *testing.T) {
	h := setup(t, 2)
	h.connect(t, 0, "old", "old-session")
	h.group(t)
	first := h.lease(t, 0, "old", "old-session", 0)
	h.lease(t, 0, "old", "old-session", first.GetLeaseEpoch())
	h.connect(t, 0, "new", "new-session")
	require.False(t, h.lease(t, 0, "new", "new-session", first.GetLeaseEpoch()).GetGranted())
	_, err := h.svc.Lease(t.Context(), h.nodes[0], 1, "old", "old-session", &pb.HALeaseRequest{RequestId: "old"})
	require.Error(t, err)
	_, err = h.db.Exec(`UPDATE fleet_node_ha_group SET lease_expires_at=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	_, err = h.db.Exec(`INSERT INTO fleet_node_heartbeat(fleet_node_id,org_id,last_seen_at) VALUES($1,1,clock_timestamp()) ON CONFLICT(fleet_node_id) DO UPDATE SET last_seen_at=EXCLUDED.last_seen_at`, h.nodes[0])
	require.NoError(t, err)
	_, err = h.svc.Resolve(t.Context(), h.nodes[0], 1)
	require.Error(t, err, "heartbeat never renews control authority")
	next := h.lease(t, 0, "new", "new-session", first.GetLeaseEpoch())
	require.True(t, next.GetGranted())
	require.Greater(t, next.GetLeaseEpoch(), first.GetLeaseEpoch())
	_, err = h.svc.Resolve(t.Context(), h.nodes[0], 1)
	require.Error(t, err, "new process must acknowledge new epoch")
}

func TestCredentialPreparationOnlyReadyAfterInstalledFingerprintMatches(t *testing.T) {
	h := setup(t, 2)
	source := h.registry.Register(h.nodes[0])
	defer source.Unregister()
	require.NoError(t, h.svc.Connect(t.Context(), h.nodes[0], 1, &pb.ControlHello{HaCapable: true, InstanceId: "source", CredentialKeyFingerprint: h.fingerprint}, "source-session"))
	require.NoError(t, sqlstores.NewSQLTransactor(h.db).RunInTx(t.Context(), func(ctx context.Context) error { return h.svc.Confirm(ctx, h.nodes[1], 1, h.nodes[0]) }))
	recipient := h.registry.Register(h.nodes[1])
	defer recipient.Unregister()
	done := make(chan struct{})
	defer close(done)
	// Export and import are separate physical commands. The server stores only
	// the envelope bytes; recipient acknowledgement proves installed-key identity.
	go func() {
		select {
		case cmd := <-source.Outgoing:
			payload, _ := proto.Marshal(&pb.HAKeyTransferResult{EncryptedKey: &pb.NodeEncryptedPayload{Algorithm: "x25519-hkdf-sha256-aes-256-gcm-v1", EphemeralPubkey: bytes.Repeat([]byte{1}, 32), Nonce: bytes.Repeat([]byte{2}, 12), Ciphertext: bytes.Repeat([]byte{3}, 48)}, CredentialKeyFingerprint: h.fingerprint})
			source.PublishAck(&pb.ControlAck{CommandId: cmd.GetCommandId(), Code: pb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: payload})
		case <-done:
		}
	}()
	go func() {
		select {
		case cmd := <-recipient.Outgoing:
			payload, _ := proto.Marshal(&pb.HAKeyTransferResult{CredentialKeyFingerprint: bytes.Repeat([]byte{9}, 32)})
			recipient.PublishAck(&pb.ControlAck{CommandId: cmd.GetCommandId(), Code: pb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: payload})
		case <-done:
		}
	}()
	require.NoError(t, h.svc.Connect(t.Context(), h.nodes[1], 1, &pb.ControlHello{HaCapable: true, InstanceId: "recipient", CredentialKeyFingerprint: bytes.Repeat([]byte{4}, 32)}, "recipient-session"))
	h.lease(t, 1, "recipient", "recipient-session", 0)
	require.Eventually(t, func() bool {
		var attempts int
		_ = h.db.QueryRow(`SELECT attempts FROM fleet_node_ha_transfer WHERE recipient_node_id=$1`, h.nodes[1]).Scan(&attempts)
		return attempts > 0
	}, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		var encrypted []byte
		_ = h.db.QueryRow(`SELECT encrypted_key FROM fleet_node_ha_transfer WHERE recipient_node_id=$1`, h.nodes[1]).Scan(&encrypted)
		return len(encrypted) > 0
	}, time.Second, 10*time.Millisecond)
	var ready bool
	require.NoError(t, h.db.QueryRow(`SELECT ready FROM fleet_node_ha_member WHERE node_id=$1`, h.nodes[1]).Scan(&ready))
	require.False(t, ready)
	require.Eventually(t, func() bool {
		summary, err := h.svc.Summary(t.Context(), h.nodes[1], 1)
		return err == nil && summary.Role == "failed"
	}, time.Second, 10*time.Millisecond)
}

func TestStandaloneLeaseUsesNodeAuthorityStandaloneScope(t *testing.T) {
	h := setup(t, 1)
	h.connect(t, 0, "standalone", "session")
	response := h.lease(t, 0, "standalone", "session", 0)
	require.Zero(t, response.GetOwnerNodeId())
	require.Zero(t, response.GetLeaseEpoch())
	require.False(t, response.GetGranted())
	require.False(t, response.GetPreparing())
	require.Equal(t, h.nodes[0], response.GetActiveNodeId())
	require.Equal(t, int64(1), response.GetOrgId())
	require.Equal(t, "standalone", response.GetInstanceId())
	require.Equal(t, "session", response.GetControlSessionId())
	require.Equal(t, int64(30000), response.GetLeaseDurationMs())
}

func TestRevocationRetainsOriginalLeaseExpiryBeforePromotion(t *testing.T) {
	h := setup(t, 2)
	h.connect(t, 0, "active", "active-session")
	h.connect(t, 1, "standby", "standby-session")
	h.group(t)
	first := h.lease(t, 0, "active", "active-session", 0)
	h.lease(t, 0, "active", "active-session", first.GetLeaseEpoch())
	var before time.Time
	require.NoError(t, h.db.QueryRow(`SELECT lease_expires_at FROM fleet_node_ha_group WHERE owner_node_id=$1`, h.nodes[0]).Scan(&before))
	tx := sqlstores.NewSQLTransactor(h.db)
	require.NoError(t, tx.RunInTx(t.Context(), func(ctx context.Context) error {
		_, err := sqlc.New(h.db).SetFleetNodeEnrollmentStatus(ctx, sqlc.SetFleetNodeEnrollmentStatusParams{ID: h.nodes[0], OrgID: 1, EnrollmentStatus: "REVOKED"})
		if err != nil {
			return err
		}
		owner, retain, err := h.svc.Revoke(ctx, h.nodes[0], 1)
		require.Equal(t, h.nodes[0], owner)
		require.True(t, retain)
		return err
	}))
	var after time.Time
	require.NoError(t, h.db.QueryRow(`SELECT lease_expires_at FROM fleet_node_ha_group WHERE owner_node_id=$1`, h.nodes[0]).Scan(&after))
	require.Equal(t, before, after)
	_, err := h.svc.Resolve(t.Context(), h.nodes[0], 1)
	require.Error(t, err, "revocation denies routing immediately")
	require.False(t, h.lease(t, 1, "standby", "standby-session", 0).GetGranted(), "old process grant must expire before promotion")
	_, err = h.db.Exec(`UPDATE fleet_node_ha_group SET lease_expires_at=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	require.True(t, h.lease(t, 1, "standby", "standby-session", 0).GetGranted())
}

func TestTransferRetryBackoffAndOperationRotation(t *testing.T) {
	h := setup(t, 3)
	h.connect(t, 0, "source", "source-session")
	h.group(t)
	q := sqlc.New(h.db)
	create := func(operation string, source int64) {
		require.NoError(t, q.CreateFleetNodeHATransfer(t.Context(), sqlc.CreateFleetNodeHATransferParams{RecipientNodeID: h.nodes[1], OwnerNodeID: h.nodes[0], OperationID: operation, SourceNodeID: source}))
	}
	create("original-operation", h.nodes[0])
	first, err := q.ClaimFleetNodeHATransfer(t.Context(), h.nodes[1])
	require.NoError(t, err)
	require.Equal(t, int32(1), first.Attempts)
	_, err = q.ClaimFleetNodeHATransfer(t.Context(), h.nodes[1])
	require.ErrorIs(t, err, sql.ErrNoRows, "immediate retries are throttled persistently")
	_, err = h.db.Exec(`UPDATE fleet_node_ha_transfer SET attempts=6,next_attempt_at=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	retried, err := q.ClaimFleetNodeHATransfer(t.Context(), h.nodes[1])
	require.NoError(t, err)
	require.Equal(t, int32(7), retried.Attempts)
	require.WithinDuration(t, time.Now().Add(30*time.Second), retried.NextAttemptAt, time.Second)
	create("new-source-operation", h.nodes[2])
	changed, err := q.GetFleetNodeHATransfer(t.Context(), h.nodes[1])
	require.NoError(t, err)
	require.Equal(t, "new-source-operation", changed.OperationID)
	_, err = q.SetFleetNodeHATransferCiphertext(t.Context(), sqlc.SetFleetNodeHATransferCiphertextParams{RecipientNodeID: h.nodes[1], OperationID: changed.OperationID, EncryptedKey: []byte("sealed-envelope")})
	require.NoError(t, err)
	create("must-not-replace-ciphertext", h.nodes[0])
	retained, err := q.GetFleetNodeHATransfer(t.Context(), h.nodes[1])
	require.NoError(t, err)
	require.Equal(t, changed.OperationID, retained.OperationID)
	require.Equal(t, []byte("sealed-envelope"), retained.EncryptedKey)
	_, err = h.db.Exec(`UPDATE fleet_node_ha_transfer SET expires_at=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	create("renewed-operation", h.nodes[0])
	renewed, err := q.GetFleetNodeHATransfer(t.Context(), h.nodes[1])
	require.NoError(t, err)
	require.Equal(t, "renewed-operation", renewed.OperationID)
	require.Empty(t, renewed.EncryptedKey)
	require.Zero(t, renewed.Attempts)
	require.WithinDuration(t, time.Now().Add(5*time.Minute), renewed.ExpiresAt, time.Second)
}

func TestOlderBlockedConnectCannotOverwriteNewerRegistration(t *testing.T) {
	h := setup(t, 1)
	h.connect(t, 0, "seed", "seed-session")
	lock, err := h.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = lock.Rollback() }()
	_, err = lock.Exec(`SELECT node_id FROM fleet_node_ha_connection WHERE node_id=$1 FOR UPDATE`, h.nodes[0])
	require.NoError(t, err)
	old := h.registry.Register(h.nodes[0])
	defer old.Unregister()
	oldResult := make(chan error, 1)
	go func() {
		oldResult <- h.svc.ConnectStream(t.Context(), h.nodes[0], 1, &pb.ControlHello{HaCapable: true, InstanceId: "old", CredentialKeyFingerprint: h.fingerprint}, "old-session", old)
	}()
	require.Eventually(t, func() bool {
		var blocked int
		err := h.db.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%fleet_node_ha_connection%'`).Scan(&blocked)
		return err == nil && blocked > 0
	}, 2*time.Second, 10*time.Millisecond, "old connection SQL must be held in flight")
	// Register remains available while SQL is blocked; no registry lock spans I/O.
	current := h.registry.Register(h.nodes[0])
	defer current.Unregister()
	currentResult := make(chan error, 1)
	go func() {
		currentResult <- h.svc.ConnectStream(t.Context(), h.nodes[0], 1, &pb.ControlHello{HaCapable: true, InstanceId: "current", CredentialKeyFingerprint: h.fingerprint}, "current-session", current)
	}()
	require.NoError(t, lock.Rollback())
	require.Error(t, <-oldResult)
	require.NoError(t, <-currentResult)
	hello, session, connected := h.registry.HAConnection(h.nodes[0])
	require.True(t, connected)
	require.Equal(t, "current", hello.GetInstanceId())
	require.Equal(t, "current-session", session)
	var storedSession, storedInstance string
	require.NoError(t, h.db.QueryRow(`SELECT control_session_id,instance_id FROM fleet_node_ha_connection WHERE node_id=$1`, h.nodes[0]).Scan(&storedSession, &storedInstance))
	require.Equal(t, "current-session", storedSession)
	require.Equal(t, "current", storedInstance)
}

func TestLeadershipAuditsAndCacheInvalidationOncePerAcknowledgedEpoch(t *testing.T) {
	h := setup(t, 2)
	h.connect(t, 0, "owner", "owner-session")
	h.connect(t, 1, "standby", "standby-session")
	h.group(t)
	h.svc.WithAudit(activity.NewService(sqlstores.NewSQLActivityStore(h.db)), sqlstores.NewSQLTransactor(h.db))
	invalidations := 0
	h.svc.WithLeadershipInvalidator(func(ctx context.Context, ownerID, orgID int64) {
		require.Nil(t, db.GetTxQueries(ctx), "cache invalidation must run after transaction commits")
		require.Equal(t, h.nodes[0], ownerID)
		require.Equal(t, int64(1), orgID)
		var recorded int
		require.NoError(t, h.db.QueryRow(`SELECT count(*) FROM activity_log WHERE event_type='fleet_node.ha.leadership'`).Scan(&recorded))
		require.Equal(t, invalidations+1, recorded)
		invalidations++
	})
	grant := h.lease(t, 0, "owner", "owner-session", 0)
	require.Zero(t, invalidations, "unacknowledged grant is not leadership")
	h.lease(t, 0, "owner", "owner-session", grant.GetLeaseEpoch())
	h.lease(t, 0, "owner", "owner-session", grant.GetLeaseEpoch())
	h.lease(t, 0, "owner", "owner-session", 0)
	h.lease(t, 0, "owner", "owner-session", grant.GetLeaseEpoch())
	require.Equal(t, 1, invalidations, "renewals and stale acknowledgements must not duplicate leadership audit")
	require.NoError(t, h.svc.Disconnect(t.Context(), h.nodes[0], 1, "owner-session"))
	_, err := h.db.Exec(`UPDATE fleet_node_ha_group SET lease_expires_at=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	next := h.lease(t, 1, "standby", "standby-session", 0)
	require.True(t, next.GetGranted())
	h.lease(t, 1, "standby", "standby-session", next.GetLeaseEpoch())
	require.Equal(t, 2, invalidations)
}
