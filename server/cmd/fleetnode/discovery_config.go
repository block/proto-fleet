package main

import (
	"fmt"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoveryranges"
)

func (r *RunCmd) effectiveDiscoveryConfig() (*pb.DiscoveryConfig, error) {
	r.stateMu.Lock()
	ranges := r.DiscoveryRanges
	if ranges == nil && r.LocalDiscoverySubnet != "" {
		ranges = []string{r.LocalDiscoverySubnet}
	}
	if ranges == nil && r.state != nil {
		ranges = r.state.DiscoveryRanges
	}
	r.stateMu.Unlock()
	scope, err := discoveryranges.Parse(ranges)
	if err != nil {
		return nil, err
	}
	normalized := scope.Strings()
	if len(normalized) > 256 {
		return nil, fmt.Errorf("at most 256 discovery ranges are supported")
	}
	return &pb.DiscoveryConfig{Ranges: normalized}, nil
}
