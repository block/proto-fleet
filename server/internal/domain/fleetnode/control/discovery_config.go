package control

import (
	"time"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"google.golang.org/protobuf/proto"
)

// StandbyDiscoveryConflictMessage lets the server tell an operator-fixable
// standby range conflict from other install failures.
const StandbyDiscoveryConflictMessage = "standby discovery ranges conflict with the active node; correct local configuration before retrying"

// DiscoveryConfig returns only the live connection's metadata, never a stale snapshot.
func (r *Registry) DiscoveryConfig(nodeID int64) *gatewaypb.DiscoveryConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	if conn := r.conns[nodeID]; conn != nil {
		return proto.CloneOf(conn.discoveryConfig)
	}
	return nil
}

// UpdateDiscoveryConfig updates metadata only for the connection that acknowledged installation.
func (r *Registry) UpdateDiscoveryConfig(nodeID int64, connectedAt time.Time, config *gatewaypb.DiscoveryConfig) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	conn := r.conns[nodeID]
	if conn == nil || !conn.connectedAt.Equal(connectedAt) {
		return false
	}
	conn.discoveryConfig = proto.CloneOf(config)
	return true
}

func (s *Stream) SetDiscoveryConfig(config *gatewaypb.DiscoveryConfig) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if s.r.conns[s.fleetNodeID] == s.conn {
		s.conn.discoveryConfig = proto.CloneOf(config)
	}
}
