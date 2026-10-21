package control

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"google.golang.org/protobuf/proto"
)

// RouteBinding separates the stable device owner from the authenticated process
// that may execute its commands. The database resolver never runs under mu.
type RouteBinding struct {
	OwnerNodeID, PhysicalNodeID, Epoch int64
	InstanceID, ControlSessionID       string
	EncryptionPublicKey                []byte
	Preparing                          bool
	ExpiresAt                          time.Time
}

func (r *Registry) SetHARouteResolver(resolve func(context.Context, int64) (RouteBinding, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.haResolve = resolve
}

func (r *Registry) SetHAConnection(nodeID, orgID int64, hello *pb.ControlHello, sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if conn := r.conns[nodeID]; conn != nil {
		conn.haHello = proto.CloneOf(hello)
		conn.haSessionID = sessionID
		conn.orgID = orgID
	}
}

func (r *Registry) HAConnection(nodeID int64) (*pb.ControlHello, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	conn := r.conns[nodeID]
	if conn == nil || conn.haHello == nil {
		return nil, "", false
	}
	return proto.CloneOf(conn.haHello), conn.haSessionID, true
}

type provisioningContextKey struct{}

// SendProvisioningCommand addresses a physical connection, never an alias. Only
// the credential-transfer command has this narrowly scoped admission exception.
func (r *Registry) SendProvisioningCommand(ctx context.Context, nodeID int64, env *pb.AgentCommand) (*pb.ControlAck, error) {
	if env.GetHaKeyTransfer() == nil {
		return nil, fmt.Errorf("provisioning requires a credential transfer")
	}
	payload, err := proto.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal credential transfer command: %w", err)
	}
	return r.SendCommand(context.WithValue(ctx, provisioningContextKey{}, true), nodeID, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, &pb.ControlCommand{CommandId: id.GenerateID(), Payload: payload})
}

func (r *Registry) resolveCommand(ctx context.Context, ownerID int64, command *pb.ControlCommand) (RouteBinding, *pb.ControlCommand, error) {
	r.mu.Lock()
	resolve := r.haResolve
	r.mu.Unlock()
	route := RouteBinding{OwnerNodeID: ownerID, PhysicalNodeID: ownerID}
	provisioning, _ := ctx.Value(provisioningContextKey{}).(bool)
	if provisioning {
		hello, session, connected := r.HAConnection(ownerID)
		if !connected {
			return route, nil, ErrNoActiveStream
		}
		route.InstanceID = hello.GetInstanceId()
		route.ControlSessionID = session
	} else if resolve != nil {
		var err error
		route, err = resolve(ctx, ownerID)
		if err != nil {
			return route, nil, err
		}
		if route.Preparing || route.PhysicalNodeID == 0 {
			return route, nil, fleeterror.NewUnavailableErrorf("fleet node has no ready active member")
		}
	}
	if !route.ExpiresAt.IsZero() && !time.Now().Before(route.ExpiresAt) {
		return route, nil, fleeterror.NewUnavailableErrorf("fleet node HA grant expired")
	}
	cmd := proto.CloneOf(command)
	cmd.IntendedInstanceId = route.InstanceID
	cmd.ControlSessionId = route.ControlSessionID
	if route.Epoch > 0 {
		cmd.HaOwnerNodeId = route.OwnerNodeID
		cmd.HaLeaseEpoch = route.Epoch
	}
	if provisioning {
		env := new(pb.AgentCommand)
		if err := proto.Unmarshal(cmd.Payload, env); err != nil {
			return route, nil, fmt.Errorf("decode credential transfer command: %w", err)
		}
		cmd.HaOwnerNodeId = env.GetHaKeyTransfer().GetOwnerNodeId()
	} else if route.Epoch > 0 {
		env := new(pb.AgentCommand)
		if err := proto.Unmarshal(cmd.Payload, env); err != nil {
			return route, nil, fmt.Errorf("decode HA command: %w", err)
		}
		if m := env.GetMinerCommand(); m != nil {
			var encrypted *pb.NodeEncryptedPayload
			switch {
			case m.GetUpdateMiningPools() != nil:
				encrypted = m.GetUpdateMiningPools().GetEncryptedConfig()
			case m.GetUpdateMinerPassword() != nil:
				encrypted = m.GetUpdateMinerPassword().GetEncryptedPasswordUpdate()
			case m.GetApplyCurtailmentConfig() != nil:
				encrypted = m.GetApplyCurtailmentConfig().GetEncryptedConfig()
			}
			if encrypted != nil && !bytes.Equal(encrypted.GetRecipientPublicKey(), route.EncryptionPublicKey) {
				return route, nil, fleeterror.NewFailedPreconditionError("encrypted command recipient changed; submit the command again")
			}
		}
	}
	return route, cmd, nil
}

// IsCommandInterrupted is explicit evidence that a dispatched command's result
// became unknown. Queue consumers must not replay it.
func IsCommandInterrupted(err error) bool {
	var interrupted *commandInterruptedError
	return errors.As(err, &interrupted)
}

type commandInterruptedError struct{ err error }

func (e *commandInterruptedError) Error() string {
	return "fleet node command interrupted; physical outcome unknown: " + e.err.Error()
}
func (e *commandInterruptedError) Unwrap() error { return e.err }
func CommandInterrupted(err error) error {
	return &commandInterruptedError{err: err}
}

// AdoptHAOwner also covers commands enqueued while this owner was standalone.
// Retain this fact after disconnect so those commands cannot become retryable
// merely because their original route snapshot predates standby confirmation.
func (r *Registry) AdoptHAOwner(ownerID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.haOwners == nil {
		r.haOwners = make(map[int64]bool)
	}
	r.haOwners[ownerID] = true
}

func (r *Registry) interrupted(route RouteBinding, err error) error {
	r.mu.Lock()
	adopted := r.haOwners[route.OwnerNodeID]
	r.mu.Unlock()
	if route.Epoch > 0 || adopted {
		return CommandInterrupted(err)
	}
	return err
}

// A long command may outlive many renewals. Observe the same incarnation and
// epoch at expiry rather than treating its initial grant as a command timeout.
func (r *Registry) leaseContext(parent context.Context, route RouteBinding) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		for {
			timer := time.NewTimer(max(time.Until(route.ExpiresAt), time.Millisecond))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			r.mu.Lock()
			resolve := r.haResolve
			r.mu.Unlock()
			if resolve == nil {
				cancel()
				return
			}
			next, err := resolve(ctx, route.OwnerNodeID)
			if err != nil || next.PhysicalNodeID != route.PhysicalNodeID || next.Epoch != route.Epoch || next.InstanceID != route.InstanceID || next.ControlSessionID != route.ControlSessionID || !time.Now().Before(next.ExpiresAt) {
				cancel()
				return
			}
			route = next
		}
	}()
	return ctx, cancel
}

// HARegistration captures the current connection identity before asynchronous
// setup. Production handlers already have this handle from RegisterAuthenticated.
func (r *Registry) HARegistration(nodeID int64) *Stream {
	r.mu.Lock()
	defer r.mu.Unlock()
	conn := r.conns[nodeID]
	if conn == nil {
		return nil
	}
	return &Stream{r: r, fleetNodeID: nodeID, conn: conn, Outgoing: conn.outgoing, Done: conn.done}
}

func (s *Stream) IsCurrent() bool {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	return s.r.conns[s.fleetNodeID] == s.conn
}

// SetHAConnection cannot attach an evicted stream's Hello to its replacement.
func (s *Stream) SetHAConnection(orgID int64, hello *pb.ControlHello, sessionID string) bool {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if s.r.conns[s.fleetNodeID] != s.conn {
		return false
	}
	s.conn.haHello = proto.CloneOf(hello)
	s.conn.haSessionID = sessionID
	s.conn.orgID = orgID
	return true
}

// UpdateHAFingerprint preserves the registration and Hello if an import's
// asynchronous acknowledgement races a reconnect.
func (r *Registry) UpdateHAFingerprint(nodeID int64, sessionID string, fingerprint []byte) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	conn := r.conns[nodeID]
	if conn == nil || conn.haSessionID != sessionID || conn.haHello == nil {
		return false
	}
	conn.haHello.CredentialKeyFingerprint = bytes.Clone(fingerprint)
	return true
}
