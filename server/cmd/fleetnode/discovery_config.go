package main

import (
	"fmt"
	"log/slog"
	"strings"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoveryranges"
)

const maxDiscoveryRanges = 256

func (r *RunCmd) effectiveDiscoveryConfig() (*pb.DiscoveryConfig, error) {
	r.stateMu.Lock()
	ranges := r.DiscoveryRanges
	if ranges == nil {
		ranges, _ = r.legacyDiscoveryRanges()
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
	if len(normalized) > maxDiscoveryRanges {
		return nil, fmt.Errorf("at most %d discovery ranges are supported", maxDiscoveryRanges)
	}
	return &pb.DiscoveryConfig{Ranges: normalized}, nil
}

// legacyDiscoveryRanges' error is only logged: an unusable legacy override must
// not stop the node, as it only ever affected local-subnet scans.
func (r *RunCmd) legacyDiscoveryRanges() ([]string, error) {
	subnet := strings.TrimSpace(r.LocalDiscoverySubnet)
	if subnet == "" {
		return nil, nil
	}
	if _, err := discoveryranges.Parse([]string{subnet}); err != nil {
		return nil, err
	}
	return []string{subnet}, nil
}

func (r *RunCmd) warnInvalidLegacyDiscoverySubnet(logger *slog.Logger) {
	if _, err := r.legacyDiscoveryRanges(); err != nil {
		logger.Warn("ignoring invalid local discovery subnet", "subnet", r.LocalDiscoverySubnet, "err", err)
	}
}
