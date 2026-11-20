package control

import (
	"context"
	"time"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"google.golang.org/protobuf/proto"
)

// WithDispatchObserver must be called before sharing the registry. It measures
// registry admission through the server's stream write, not remote acceptance.
func (r *Registry) WithDispatchObserver(observe func(context.Context, string, string, string, time.Duration)) *Registry {
	r.dispatchObserver = observe
	return r
}

// ObserveCommandSend snapshots the command before a potentially blocking send.
// The returned callback belongs to this stream, so replacement or cancellation
// cannot accidentally attribute a late write to a newer command/connection.
func (s *Stream) ObserveCommandSend(ctx context.Context, cmd *gatewaypb.ControlCommand) func(error) {
	if s.r.dispatchObserver == nil {
		return func(error) {}
	}
	s.r.mu.Lock()
	c := s.conn.cmds[cmd.GetCommandId()]
	if s.r.conns[s.fleetNodeID] != s.conn || c == nil || c.dispatchObserved {
		s.r.mu.Unlock()
		return func(error) {}
	}
	c.dispatchObserved = true
	started := c.dispatchedAt
	s.r.mu.Unlock()
	env := &gatewaypb.AgentCommand{}
	if err := proto.Unmarshal(cmd.GetPayload(), env); err != nil {
		env = nil
	}
	class, kind := AdmissionForCommand(env)
	return func(err error) {
		result := "sent"
		if err != nil {
			result = "send_failed"
		}
		s.r.dispatchObserver(ctx, kind, string(class), result, time.Since(started))
	}
}
