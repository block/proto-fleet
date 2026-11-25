package control

import (
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"google.golang.org/protobuf/proto"
)

// DiscoveryConfig returns only the live connection's metadata, never a stale snapshot.
func (r *Registry) DiscoveryConfig(nodeID int64) *gatewaypb.DiscoveryConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	if conn := r.conns[nodeID]; conn != nil {
		return proto.CloneOf(conn.discoveryConfig)
	}
	return nil
}

func (s *Stream) SetDiscoveryConfig(config *gatewaypb.DiscoveryConfig) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	if s.r.conns[s.fleetNodeID] == s.conn {
		s.conn.discoveryConfig = proto.CloneOf(config)
	}
}
