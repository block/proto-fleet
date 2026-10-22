package ha_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
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
func (h *harness) connect(t *testing.T, index int, instance, session string) *control.Stream {
	t.Helper()
	handle := h.registry.Register(h.nodes[index])
	t.Cleanup(handle.Unregister)
	require.NoError(t, h.svc.ConnectStream(t.Context(), h.nodes[index], 1, &pb.ControlHello{HaCapable: true, InstanceId: instance, CredentialKeyFingerprint: h.fingerprint}, session, handle))
	return handle
}

// Lease tests start from a prepared, enabled group; activation is exercised separately.
func (h *harness) group(t *testing.T) {
	t.Helper()
	tx := sqlstores.NewSQLTransactor(h.db)
	require.NoError(t, tx.RunInTx(t.Context(), func(ctx context.Context) error { return h.svc.Confirm(ctx, h.nodes[1], 1, h.nodes[0]) }))
	for _, nodeID := range h.nodes[2:] {
		require.NoError(t, sqlc.New(h.db).CreateFleetNodeHAMember(t.Context(), sqlc.CreateFleetNodeHAMemberParams{NodeID: nodeID, OrgID: 1, OwnerNodeID: h.nodes[0], Ready: true, StatusReason: ""}))
	}
	_, err := h.db.Exec(`UPDATE fleet_node_ha_member SET ready=true,status_reason=''`)
	require.NoError(t, err)
	_, err = h.db.Exec(`UPDATE fleet_node_ha_group SET enabled=true`)
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
	require.Error(t, err, "enabled HA requires an acknowledged lease for reports")
	require.Zero(t, initialOwner)
	_, err = h.svc.EffectiveOwner(t.Context(), h.nodes[1], 1)
	require.Error(t, err, "a passive standby cannot attribute reports")
	first := h.lease(t, 0, "p0", "s0", 0)
	require.True(t, first.GetGranted())
	require.Equal(t, int64(1), first.GetLeaseEpoch())
	_, err = h.svc.Resolve(t.Context(), h.nodes[0], 1)
	require.Error(t, err, "grant is not routable before acknowledgement")
	_, err = h.svc.EffectiveOwner(t.Context(), h.nodes[0], 1)
	require.Error(t, err, "there is no pre-adoption report exception")
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

func TestCredentialInstallWaitsForNewHelloBeforeActivatingHA(t *testing.T) {
	h := setup(t, 2)
	source := h.registry.Register(h.nodes[0])
	defer source.Unregister()
	require.NoError(t, h.svc.ConnectStream(t.Context(), h.nodes[0], 1, &pb.ControlHello{HaCapable: true, InstanceId: "source", CredentialKeyFingerprint: h.fingerprint}, "source-session", source))
	require.NoError(t, sqlstores.NewSQLTransactor(h.db).RunInTx(t.Context(), func(ctx context.Context) error { return h.svc.Confirm(ctx, h.nodes[1], 1, h.nodes[0]) }))
	accepted := &pb.ControlAccepted{}
	require.NoError(t, h.svc.Configure(t.Context(), h.nodes[0], 1, accepted))
	require.Zero(t, accepted.GetHaOwnerNodeId(), "the original keeps standalone authority while preparation is pending")
	recipient := h.registry.Register(h.nodes[1])
	defer recipient.Unregister()
	require.NoError(t, h.svc.ConnectStream(t.Context(), h.nodes[1], 1, &pb.ControlHello{HaCapable: true, InstanceId: "recipient", CredentialKeyFingerprint: bytes.Repeat([]byte{4}, 32)}, "recipient-session", recipient))
	accepted = &pb.ControlAccepted{}
	require.NoError(t, h.svc.Configure(t.Context(), h.nodes[1], 1, accepted))
	require.True(t, accepted.GetPreparing())
	require.Equal(t, h.nodes[0], accepted.GetHaOwnerNodeId())
	require.Equal(t, h.fingerprint, accepted.GetCredentialKeyFingerprint())
	exported := make(chan *pb.HAKeyTransfer, 1)
	imported := make(chan *pb.HAKeyTransfer, 1)
	go func() {
		select {
		case cmd := <-source.Outgoing:
			env := new(pb.AgentCommand)
			_ = proto.Unmarshal(cmd.GetPayload(), env)
			exported <- env.GetHaKeyTransfer()
			payload, _ := proto.Marshal(&pb.HAKeyTransferResult{EncryptedKey: &pb.NodeEncryptedPayload{Algorithm: "x25519-hkdf-sha256-aes-256-gcm-v1", EphemeralPubkey: bytes.Repeat([]byte{1}, 32), Nonce: bytes.Repeat([]byte{2}, 12), Ciphertext: bytes.Repeat([]byte{3}, 48)}, CredentialKeyFingerprint: h.fingerprint})
			source.PublishAck(&pb.ControlAck{CommandId: cmd.GetCommandId(), Code: pb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: payload})
		case <-t.Context().Done():
		}
	}()
	go func() {
		select {
		case cmd := <-recipient.Outgoing:
			env := new(pb.AgentCommand)
			_ = proto.Unmarshal(cmd.GetPayload(), env)
			imported <- env.GetHaKeyTransfer()
			payload, _ := proto.Marshal(&pb.HAKeyTransferResult{CredentialKeyFingerprint: h.fingerprint})
			recipient.PublishAck(&pb.ControlAck{CommandId: cmd.GetCommandId(), Code: pb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: payload})
		case <-t.Context().Done():
		}
	}()
	h.svc.StartPreparation(t.Context(), h.nodes[1], 1)
	var export, install *pb.HAKeyTransfer
	select {
	case export = <-exported:
	case <-time.After(2 * time.Second):
		t.Fatal("source did not export")
	}
	select {
	case install = <-imported:
	case <-time.After(2 * time.Second):
		t.Fatal("recipient did not receive sealed key")
	}
	require.Equal(t, export.GetOperationId(), install.GetOperationId())
	require.Equal(t, h.nodes[0], install.GetSourceNodeId())
	require.Equal(t, h.nodes[1], install.GetRecipientNodeId())
	require.NotNil(t, install.GetEncryptedKey())
	var ready, enabled bool
	require.NoError(t, h.db.QueryRow(`SELECT m.ready,g.enabled FROM fleet_node_ha_member m JOIN fleet_node_ha_group g ON g.owner_node_id=m.owner_node_id WHERE m.node_id=$1`, h.nodes[1]).Scan(&ready, &enabled))
	require.False(t, ready, "an install acknowledgement cannot update the live process fingerprint")
	require.False(t, enabled)
	route, err := h.svc.Resolve(t.Context(), h.nodes[0], 1)
	require.NoError(t, err)
	require.Zero(t, route.Epoch)
	// Hold an admitted standalone operation while the ready standby tries to
	// activate. Canceling that stream's preparation must resume ordinary dispatch.
	longResult := make(chan error, 1)
	go func() {
		_, err := h.registry.SendCommand(t.Context(), h.nodes[0], pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "held-operation"})
		longResult <- err
	}()
	var held *pb.ControlCommand
	select {
	case held = <-source.Outgoing:
	case <-time.After(time.Second):
		t.Fatal("standalone operation was not admitted")
	}
	go func() {
		for {
			select {
			case cmd := <-source.Outgoing:
				source.PublishAck(&pb.ControlAck{CommandId: cmd.GetCommandId(), Code: pb.AckCode_ACK_CODE_OK, Succeeded: true})
			case <-source.Done:
				return
			case <-t.Context().Done():
				return
			}
		}
	}()
	probe := func() error {
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		_, err := h.registry.SendCommand(ctx, h.nodes[0], pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: "dispatch-probe"})
		return err
	}
	h.connect(t, 1, "recipient-restarted", "recipient-new-session")
	preparing, cancelPreparation := context.WithCancel(t.Context())
	h.svc.StartPreparation(preparing, h.nodes[1], 1)
	require.Eventually(t, func() bool {
		err := probe()
		return err != nil && strings.Contains(err.Error(), "activation is draining")
	}, time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return probe() == nil }, 21*time.Second, 50*time.Millisecond, "a long drain must outlive the bounded credential-transfer attempt")
	cancelPreparation()
	require.Eventually(t, func() bool { return probe() == nil }, time.Second, 10*time.Millisecond, "canceling preparation must reopen standalone dispatch")
	require.NoError(t, h.db.QueryRow(`SELECT enabled FROM fleet_node_ha_group WHERE owner_node_id=$1`, h.nodes[0]).Scan(&enabled))
	require.False(t, enabled)
	source.PublishAck(&pb.ControlAck{CommandId: held.GetCommandId(), Code: pb.AckCode_ACK_CODE_OK, Succeeded: true})
	require.NoError(t, <-longResult)
	h.svc.StartPreparation(t.Context(), h.nodes[1], 1)
	require.Eventually(t, func() bool {
		var enabled bool
		err := h.db.QueryRow(`SELECT enabled FROM fleet_node_ha_group WHERE owner_node_id=$1`, h.nodes[0]).Scan(&enabled)
		return err == nil && enabled
	}, 2*time.Second, 10*time.Millisecond)
	select {
	case <-source.Done:
	case <-time.After(time.Second):
		t.Fatal("original was not reconnected into HA scope")
	}
	require.NoError(t, h.svc.Disconnect(t.Context(), h.nodes[0], 1, "source-session"))
	standbyFirst := h.lease(t, 1, "recipient-restarted", "recipient-new-session", 0)
	require.False(t, standbyFirst.GetGranted(), "first activation reserves the initial grant for the reconnecting source")
	require.Zero(t, standbyFirst.GetLeaseEpoch(), "the reconnect reservation grants no authority")
	h.connect(t, 0, "source", "source-new-session")
	accepted = &pb.ControlAccepted{}
	require.NoError(t, h.svc.Configure(t.Context(), h.nodes[0], 1, accepted))
	require.Equal(t, h.nodes[0], accepted.GetHaOwnerNodeId())
	first := h.lease(t, 0, "source", "source-new-session", 0)
	require.True(t, first.GetGranted())
	h.lease(t, 0, "source", "source-new-session", first.GetLeaseEpoch())
	_, err = h.svc.Resolve(t.Context(), h.nodes[0], 1)
	require.NoError(t, err)
}

func TestReadyStandbyActivatesWhenSourceDisconnectsBeforeFirstGrant(t *testing.T) {
	h := setup(t, 2)
	source := h.connect(t, 0, "source", "source-session")
	require.NoError(t, sqlstores.NewSQLTransactor(h.db).RunInTx(t.Context(), func(ctx context.Context) error { return h.svc.Confirm(ctx, h.nodes[1], 1, h.nodes[0]) }))
	source.Unregister()
	require.NoError(t, h.svc.Disconnect(t.Context(), h.nodes[0], 1, "source-session"))
	h.connect(t, 1, "recipient", "recipient-session")
	h.svc.StartPreparation(t.Context(), h.nodes[1], 1)
	require.Eventually(t, func() bool {
		var enabled bool
		err := h.db.QueryRow(`SELECT enabled FROM fleet_node_ha_group WHERE owner_node_id=$1`, h.nodes[0]).Scan(&enabled)
		return err == nil && enabled
	}, 2*time.Second, 10*time.Millisecond, "disconnected source work is already torn down")
	reservation := h.lease(t, 1, "recipient", "recipient-session", 0)
	require.False(t, reservation.GetGranted())
	_, err := h.db.Exec(`UPDATE fleet_node_ha_group SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE owner_node_id=$1`, h.nodes[0])
	require.NoError(t, err)
	grant := h.lease(t, 1, "recipient", "recipient-session", 0)
	require.True(t, grant.GetGranted())
	h.lease(t, 1, "recipient", "recipient-session", grant.GetLeaseEpoch())
	route, err := h.svc.Resolve(t.Context(), h.nodes[0], 1)
	require.NoError(t, err)
	require.Equal(t, h.nodes[1], route.PhysicalNodeID)
}

func TestInstalledFingerprintConflictCannotBecomeAReadyConnection(t *testing.T) {
	h := setup(t, 2)
	h.connect(t, 0, "source", "source-session")
	h.group(t)
	stream := h.registry.Register(h.nodes[1])
	defer stream.Unregister()
	err := h.svc.ConnectStream(t.Context(), h.nodes[1], 1, &pb.ControlHello{HaCapable: true, InstanceId: "wrong-key", CredentialKeyFingerprint: bytes.Repeat([]byte{9}, 32)}, "wrong-key-session", stream)
	require.ErrorContains(t, err, "credential key conflicts")
	_, _, connected := h.registry.HAConnection(h.nodes[1])
	require.False(t, connected)
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

func TestRetryStartsFreshTransferWithAnotherReadySource(t *testing.T) {
	h := setup(t, 3)
	source := h.connect(t, 0, "source", "source-session")
	require.NoError(t, sqlstores.NewSQLTransactor(h.db).RunInTx(t.Context(), func(ctx context.Context) error { return h.svc.Confirm(ctx, h.nodes[1], 1, h.nodes[0]) }))
	replacement := h.connect(t, 2, "replacement", "replacement-session")
	require.NoError(t, sqlc.New(h.db).CreateFleetNodeHAMember(t.Context(), sqlc.CreateFleetNodeHAMemberParams{NodeID: h.nodes[2], OrgID: 1, OwnerNodeID: h.nodes[0], Ready: true, StatusReason: ""}))
	recipient := h.registry.Register(h.nodes[1])
	defer recipient.Unregister()
	require.NoError(t, h.svc.ConnectStream(t.Context(), h.nodes[1], 1, &pb.ControlHello{HaCapable: true, InstanceId: "recipient", CredentialKeyFingerprint: bytes.Repeat([]byte{4}, 32)}, "recipient-session", recipient))
	h.svc.StartPreparation(t.Context(), h.nodes[1], 1)
	var first *pb.ControlCommand
	select {
	case first = <-source.Outgoing:
	case <-time.After(2 * time.Second):
		t.Fatal("initial source was not selected")
	}
	firstEnv := new(pb.AgentCommand)
	require.NoError(t, proto.Unmarshal(first.GetPayload(), firstEnv))
	source.PublishAck(&pb.ControlAck{CommandId: first.GetCommandId(), Code: pb.AckCode_ACK_CODE_INTERNAL, ErrorMessage: "try another source"})
	source.Unregister()
	require.NoError(t, h.svc.Disconnect(t.Context(), h.nodes[0], 1, "source-session"))
	select {
	case <-replacement.Outgoing:
		t.Fatal("failed transfer retried without backoff")
	case <-time.After(200 * time.Millisecond):
	}
	var next *pb.ControlCommand
	select {
	case next = <-replacement.Outgoing:
	case <-time.After(3 * time.Second):
		t.Fatal("retry did not select another ready source")
	}
	nextEnv := new(pb.AgentCommand)
	require.NoError(t, proto.Unmarshal(next.GetPayload(), nextEnv))
	require.NotEqual(t, firstEnv.GetHaKeyTransfer().GetOperationId(), nextEnv.GetHaKeyTransfer().GetOperationId())
	require.Equal(t, h.nodes[2], nextEnv.GetHaKeyTransfer().GetSourceNodeId())
	require.Equal(t, h.nodes[1], nextEnv.GetHaKeyTransfer().GetRecipientNodeId())
	require.Nil(t, nextEnv.GetHaKeyTransfer().GetEncryptedKey(), "a retry always asks the source for a new sealed envelope")
	payload, err := proto.Marshal(&pb.HAKeyTransferResult{EncryptedKey: &pb.NodeEncryptedPayload{Algorithm: "x25519-hkdf-sha256-aes-256-gcm-v1", EphemeralPubkey: bytes.Repeat([]byte{1}, 32), Nonce: bytes.Repeat([]byte{2}, 12), Ciphertext: bytes.Repeat([]byte{3}, 48)}, CredentialKeyFingerprint: h.fingerprint})
	require.NoError(t, err)
	replacement.PublishAck(&pb.ControlAck{CommandId: next.GetCommandId(), Code: pb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: payload})
	select {
	case cmd := <-recipient.Outgoing:
		env := new(pb.AgentCommand)
		require.NoError(t, proto.Unmarshal(cmd.GetPayload(), env))
		require.Equal(t, nextEnv.GetHaKeyTransfer().GetOperationId(), env.GetHaKeyTransfer().GetOperationId())
		require.Equal(t, h.nodes[2], env.GetHaKeyTransfer().GetSourceNodeId())
		payload, err := proto.Marshal(&pb.HAKeyTransferResult{CredentialKeyFingerprint: h.fingerprint})
		require.NoError(t, err)
		recipient.PublishAck(&pb.ControlAck{CommandId: cmd.GetCommandId(), Code: pb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: payload})
	case <-time.After(time.Second):
		t.Fatal("fresh sealed envelope did not reach recipient")
	}
}

func TestBulkSummaryUsesTheAcknowledgedConnectionTuple(t *testing.T) {
	h := setup(t, 2)
	h.connect(t, 0, "owner", "owner-session")
	h.connect(t, 1, "standby", "standby-session")
	h.group(t)
	first := h.lease(t, 0, "owner", "owner-session", 0)
	summaries, err := h.svc.ListSummaries(t.Context(), 1)
	require.NoError(t, err)
	require.NotEqual(t, "active", summaries[h.nodes[0]].Role)
	require.False(t, summaries[h.nodes[0]].Eligible)
	h.lease(t, 0, "owner", "owner-session", first.GetLeaseEpoch())
	summaries, err = h.svc.ListSummaries(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "active", summaries[h.nodes[0]].Role)
	require.True(t, summaries[h.nodes[0]].Eligible)
	require.Equal(t, "standby", summaries[h.nodes[1]].Role)
	require.False(t, summaries[h.nodes[1]].Eligible)
	h.connect(t, 0, "restarted", "new-session")
	summaries, err = h.svc.ListSummaries(t.Context(), 1)
	require.NoError(t, err)
	require.NotEqual(t, "active", summaries[h.nodes[0]].Role)
	require.False(t, summaries[h.nodes[0]].Eligible)
	summaries, err = h.svc.ListSummaries(t.Context(), 2)
	require.NoError(t, err)
	require.Empty(t, summaries)
}

func TestConfirmationWaitsForSourceRevocationLock(t *testing.T) {
	h := setup(t, 2)
	h.connect(t, 0, "source", "source-session")
	revoke, err := h.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = revoke.Rollback() }()
	_, err = sqlc.New(revoke).LockFleetNodeByID(t.Context(), sqlc.LockFleetNodeByIDParams{ID: h.nodes[0], OrgID: 1})
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() {
		result <- sqlstores.NewSQLTransactor(h.db).RunInTx(t.Context(), func(ctx context.Context) error { return h.svc.Confirm(ctx, h.nodes[1], 1, h.nodes[0]) })
	}()
	require.Eventually(t, func() bool {
		var blocked int
		err := h.db.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FOR NO KEY UPDATE OF fn%'`).Scan(&blocked)
		return err == nil && blocked > 0
	}, 2*time.Second, 10*time.Millisecond)
	_, err = sqlc.New(revoke).SetFleetNodeEnrollmentStatus(t.Context(), sqlc.SetFleetNodeEnrollmentStatusParams{ID: h.nodes[0], OrgID: 1, EnrollmentStatus: "REVOKED"})
	require.NoError(t, err)
	require.NoError(t, revoke.Commit())
	require.ErrorContains(t, <-result, "standby source is revoked")
	var count int
	require.NoError(t, h.db.QueryRow(`SELECT count(*) FROM fleet_node_ha_group`).Scan(&count))
	require.Zero(t, count)
}

func TestSourceFingerprintMismatchPersistsFailedPreparation(t *testing.T) {
	h := setup(t, 2)
	source := h.connect(t, 0, "source", "source-session")
	require.NoError(t, sqlstores.NewSQLTransactor(h.db).RunInTx(t.Context(), func(ctx context.Context) error { return h.svc.Confirm(ctx, h.nodes[1], 1, h.nodes[0]) }))
	recipient := h.registry.Register(h.nodes[1])
	defer recipient.Unregister()
	require.NoError(t, h.svc.ConnectStream(t.Context(), h.nodes[1], 1, &pb.ControlHello{HaCapable: true, InstanceId: "recipient", CredentialKeyFingerprint: bytes.Repeat([]byte{4}, 32)}, "recipient-session", recipient))
	h.svc.StartPreparation(t.Context(), h.nodes[1], 1)
	select {
	case cmd := <-source.Outgoing:
		payload, err := proto.Marshal(&pb.HAKeyTransferResult{CredentialKeyFingerprint: bytes.Repeat([]byte{9}, 32), EncryptedKey: &pb.NodeEncryptedPayload{Algorithm: "x25519-hkdf-sha256-aes-256-gcm-v1", EphemeralPubkey: bytes.Repeat([]byte{1}, 32), Nonce: bytes.Repeat([]byte{2}, 12), Ciphertext: bytes.Repeat([]byte{3}, 48)}})
		require.NoError(t, err)
		source.PublishAck(&pb.ControlAck{CommandId: cmd.GetCommandId(), Code: pb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: payload})
	case <-time.After(2 * time.Second):
		t.Fatal("source was not asked to prepare")
	}
	require.Eventually(t, func() bool {
		summaries, err := h.svc.ListSummaries(t.Context(), 1)
		return err == nil && summaries[h.nodes[1]].Role == "failed"
	}, time.Second, 10*time.Millisecond)
	select {
	case <-recipient.Outgoing:
		t.Fatal("mismatched source result was sent to recipient")
	default:
	}
	route, err := h.svc.Resolve(t.Context(), h.nodes[0], 1)
	require.NoError(t, err)
	require.Zero(t, route.Epoch)
}

// Model one-way response loss: upstream lease requests still arrive, but the
// process cannot acknowledge an initial grant (or has expired its last grant).
func TestLeaseMissingAcknowledgementsCannotPreventStandbyPromotion(t *testing.T) {
	for _, adopted := range []bool{false, true} {
		t.Run(fmt.Sprintf("previously-acknowledged-%t", adopted), func(t *testing.T) {
			h := setup(t, 2)
			h.connect(t, 0, "owner", "owner-session")
			h.connect(t, 1, "standby", "standby-session")
			h.group(t)
			first := h.lease(t, 0, "owner", "owner-session", 0)
			require.True(t, first.GetGranted())
			if adopted {
				h.lease(t, 0, "owner", "owner-session", first.GetLeaseEpoch())
			}
			// Advance close to the actual persisted expiry without a 30-second test.
			_, err := h.db.Exec(`UPDATE fleet_node_ha_group SET lease_expires_at=clock_timestamp()+interval '250 milliseconds'`)
			require.NoError(t, err)
			var before, after time.Time
			require.NoError(t, h.db.QueryRow(`SELECT lease_expires_at FROM fleet_node_ha_group`).Scan(&before))
			for range 3 {
				retry := h.lease(t, 0, "owner", "owner-session", 0)
				require.False(t, retry.GetGranted(), "a missing acknowledgement cannot advertise a fresh grant")
			}
			require.NoError(t, h.db.QueryRow(`SELECT lease_expires_at FROM fleet_node_ha_group`).Scan(&after))
			require.Equal(t, before, after, "forwarded requests cannot renew unacknowledged authority")
			require.False(t, h.lease(t, 1, "standby", "standby-session", 0).GetGranted(), "retain the outstanding old expiry")
			time.Sleep(time.Until(before) + 20*time.Millisecond)
			require.False(t, h.lease(t, 0, "owner", "owner-session", 0).GetGranted(), "an expired tuple cannot reacquire itself")
			next := h.lease(t, 1, "standby", "standby-session", 0)
			require.True(t, next.GetGranted(), "the oldest healthy standby wins after actual expiry")
			require.Greater(t, next.GetLeaseEpoch(), first.GetLeaseEpoch())
			h.lease(t, 1, "standby", "standby-session", next.GetLeaseEpoch())
			route, err := h.svc.Resolve(t.Context(), h.nodes[0], 1)
			require.NoError(t, err)
			require.Equal(t, h.nodes[1], route.PhysicalNodeID)
			_, err = h.svc.Lease(t.Context(), h.nodes[0], 1, "owner", "owner-session", &pb.HALeaseRequest{RequestId: "retired"})
			require.Error(t, err, "a promoted-away expired tuple must reconnect before further requests")
		})
	}
}

func TestLeaseLiveAcknowledgementExtendsAuthority(t *testing.T) {
	h := setup(t, 2)
	h.connect(t, 0, "owner", "owner-session")
	h.group(t)
	first := h.lease(t, 0, "owner", "owner-session", 0)
	_, err := h.db.Exec(`UPDATE fleet_node_ha_group SET lease_expires_at=clock_timestamp()+interval '10 seconds'`)
	require.NoError(t, err)
	var before, after time.Time
	require.NoError(t, h.db.QueryRow(`SELECT lease_expires_at FROM fleet_node_ha_group`).Scan(&before))
	renewed := h.lease(t, 0, "owner", "owner-session", first.GetLeaseEpoch())
	require.True(t, renewed.GetGranted())
	require.NoError(t, h.db.QueryRow(`SELECT lease_expires_at FROM fleet_node_ha_group`).Scan(&after))
	require.Greater(t, after.Sub(before), 15*time.Second)
	require.Equal(t, first.GetLeaseEpoch(), renewed.GetLeaseEpoch())
}
