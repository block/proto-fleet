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
	if route.Epoch > 0 {
		acknowledged, ok := r.HARoute(route.OwnerNodeID)
		if !ok || !sameAuthority(route, acknowledged) || !bytes.Equal(route.EncryptionPublicKey, acknowledged.EncryptionPublicKey) {
			return route, nil, fleeterror.NewUnavailableErrorf("fleet node HA authority changed")
		}
		route = acknowledged
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
			// Persisted commands issued before recipient markers were introduced
			// were encrypted for their original logical owner. They may execute only
			// on that physical node, never on a promoted standby.
			originalRecipient := encrypted != nil && len(encrypted.GetRecipientPublicKey()) == 0 && route.PhysicalNodeID == route.OwnerNodeID
			if encrypted != nil && !originalRecipient && !bytes.Equal(encrypted.GetRecipientPublicKey(), route.EncryptionPublicKey) {
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

func (r *Registry) interrupted(cmd *inflightCommand, err error) error {
	r.mu.Lock()
	terminal := cmd.route.Epoch > 0 || cmd.terminalOnInterruption
	r.mu.Unlock()
	if terminal {
		return CommandInterrupted(err)
	}
	return err
}

// haAuthority is the acknowledged database grant projected into this process.
// One context and expiry timer serve every command in its logical owner epoch.
type haAuthority struct {
	route       RouteBinding
	ctx         context.Context //nolint:containedctx // Shared lifetime for commands in one acknowledged lease epoch.
	cancel      context.CancelFunc
	timer       *time.Timer
	invalidated bool
}

func sameAuthority(a, b RouteBinding) bool {
	return a.OwnerNodeID == b.OwnerNodeID && a.PhysicalNodeID == b.PhysicalNodeID && a.Epoch == b.Epoch && a.InstanceID == b.InstanceID && a.ControlSessionID == b.ControlSessionID
}

// SetHARoute publishes an acknowledged, committed grant. A later renewal can
// replace a locally expired projection; its old commands remain terminal.
// Explicitly invalidated authority requires a newer database epoch.
func (r *Registry) SetHARoute(route RouteBinding) {
	r.mu.Lock()
	defer r.mu.Unlock()
	conn := r.conns[route.PhysicalNodeID]
	if route.Epoch <= 0 || !time.Now().Before(route.ExpiresAt) || conn == nil || conn.haSessionID != route.ControlSessionID || conn.haHello.GetInstanceId() != route.InstanceID {
		return
	}
	old := r.haRoutes[route.OwnerNodeID]
	if old != nil {
		if route.Epoch < old.route.Epoch {
			return
		}
		if sameAuthority(old.route, route) {
			if old.invalidated || !bytes.Equal(old.route.EncryptionPublicKey, route.EncryptionPublicKey) {
				return
			}
			if old.ctx.Err() == nil && time.Now().Before(old.route.ExpiresAt) {
				if route.ExpiresAt.After(old.route.ExpiresAt) {
					old.route.ExpiresAt = route.ExpiresAt
					old.timer.Reset(time.Until(route.ExpiresAt))
				}
				return
			}
			r.cancelAuthorityLocked(old)
			if !route.ExpiresAt.After(old.route.ExpiresAt) {
				return
			}
		} else {
			if route.Epoch == old.route.Epoch {
				return
			}
			r.invalidateAuthorityLocked(old)
		}
	}
	route.EncryptionPublicKey = bytes.Clone(route.EncryptionPublicKey)
	ctx, cancel := context.WithCancel(context.Background())
	authority := &haAuthority{route: route, ctx: ctx, cancel: cancel}
	r.haRoutes[route.OwnerNodeID] = authority
	authority.timer = time.AfterFunc(time.Until(route.ExpiresAt), func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		// A timer callback already waiting for mu may race a committed renewal.
		if r.haRoutes[route.OwnerNodeID] == authority && !time.Now().Before(authority.route.ExpiresAt) {
			r.cancelAuthorityLocked(authority)
		}
	})
	delete(r.haActivations, route.OwnerNodeID)
}

// HARoute returns the current acknowledged authority, never a database snapshot
// that has expired or belongs to a disconnected control session.
func (r *Registry) HARoute(ownerID int64) (RouteBinding, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	authority := r.haRoutes[ownerID]
	if authority == nil || authority.ctx.Err() != nil {
		return RouteBinding{}, false
	}
	if !time.Now().Before(authority.route.ExpiresAt) {
		r.cancelAuthorityLocked(authority)
		return RouteBinding{}, false
	}
	route := authority.route
	route.EncryptionPublicKey = bytes.Clone(route.EncryptionPublicKey)
	return route, true
}

func (r *Registry) InvalidateHARoute(ownerID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if authority := r.haRoutes[ownerID]; authority != nil {
		r.invalidateAuthorityLocked(authority)
	}
}

func (r *Registry) invalidateAuthorityLocked(authority *haAuthority) {
	authority.invalidated = true
	r.cancelAuthorityLocked(authority)
}

func (r *Registry) cancelAuthorityLocked(authority *haAuthority) {
	authority.cancel()
	if authority.timer != nil {
		authority.timer.Stop()
	}
	for _, conn := range r.conns {
		for id, cmd := range conn.cmds {
			if cmd.authority == authority {
				delete(conn.cmds, id)
				close(cmd.done)
			}
		}
	}
}

func (r *Registry) invalidateConnectionRoutesLocked(nodeID int64) {
	for _, authority := range r.haRoutes {
		if authority.route.PhysicalNodeID == nodeID {
			r.invalidateAuthorityLocked(authority)
		}
	}
}

// BeginHAActivation prevents new standalone work before the database enables HA.
// Already admitted ordinary commands become terminal if the stream disconnects.
func (r *Registry) BeginHAActivation(ownerID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.haActivations[ownerID] = true
	source := r.conns[ownerID]
	if source != nil {
		for _, cmd := range source.cmds {
			if !cmd.provisioning {
				cmd.terminalOnInterruption = true
			}
		}
	}
}

func (r *Registry) AbortHAActivation(ownerID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.haActivations, ownerID)
}

// FinishHAActivation interrupts the source and forces a reconnect with HA enabled.
// Dispatch stays gated until the first acknowledged grant is published.
func (r *Registry) FinishHAActivation(ownerID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, activating := r.haActivations[ownerID]; activating {
		// A replacement may have received its standalone configuration while the
		// enabling transaction was pending. The gate kept it idle too.
		r.disconnectLocked(ownerID)
	}
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
	if s.conn.haHello != nil && (s.conn.haSessionID != sessionID || s.conn.haHello.GetInstanceId() != hello.GetInstanceId()) {
		s.r.invalidateConnectionRoutesLocked(s.fleetNodeID)
	}
	s.conn.haHello = proto.CloneOf(hello)
	s.conn.haSessionID = sessionID
	s.conn.orgID = orgID
	return true
}
