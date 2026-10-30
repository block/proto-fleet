package main

import (
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net/netip"
	"slices"
	"strings"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
	"github.com/block/proto-fleet/server/internal/domain/discoveryranges"
	fleetnodecontrol "github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
)

func (r *RunCmd) effectiveDiscoveryConfig() (*pb.DiscoveryConfig, error) {
	scope, err := r.effectiveDiscoveryScope()
	if err != nil {
		return nil, err
	}
	return &pb.DiscoveryConfig{Ranges: scope.Strings()}, nil
}

func (r *RunCmd) effectiveDiscoveryScope() (discoveryranges.Scope, error) {
	r.stateMu.Lock()
	ranges := r.DiscoveryRanges
	if ranges == nil {
		ranges, _ = r.legacyDiscoveryRanges()
	}
	scope, err := discoveryranges.Parse(ranges)
	if err != nil {
		return discoveryranges.Scope{}, err
	}
	if len(scope.Ranges()) > discoverylimits.MaxDiscoveryRanges {
		return discoveryranges.Scope{}, fmt.Errorf("at most %d discovery ranges are supported", discoverylimits.MaxDiscoveryRanges)
	}
	return scope, nil
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

func (r *RunCmd) installDiscoveryConfig(ranges []string) error {
	scope, err := discoveryranges.Parse(ranges)
	if err != nil {
		return err
	}
	if scope.Empty() {
		return fmt.Errorf("active node discovery ranges are not configured")
	}
	r.saveMu.Lock()
	defer r.saveMu.Unlock()
	current, err := r.effectiveDiscoveryScope()
	if err != nil {
		return err
	}
	if (r.DiscoveryRanges != nil || !current.Empty()) && !slices.Equal(current.Ranges(), scope.Ranges()) {
		return errors.New(fleetnodecontrol.StandbyDiscoveryConflictMessage)
	}
	r.stateMu.Lock()
	if r.state == nil {
		r.stateMu.Unlock()
		return fmt.Errorf("node state is unavailable")
	}
	snapshot := *r.state
	snapshot.DiscoveryRanges = scope.Strings()
	r.stateMu.Unlock()
	if err := bootstrap.SaveState(r.statePath, &snapshot); err != nil {
		return fmt.Errorf("persist discovery configuration: %w", err)
	}
	r.stateMu.Lock()
	r.state.DiscoveryRanges = snapshot.DiscoveryRanges
	r.stateMu.Unlock()
	return nil
}

func (r *RunCmd) scopedDiscoveryTargets(targets iter.Seq[netip.Addr]) (iter.Seq[netip.Addr], error) {
	scope, err := r.effectiveDiscoveryScope()
	if err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "%s", err)
	}
	if scope.Empty() {
		return nil, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "configure discovery-ranges before discovering new miners")
	}
	return func(yield func(netip.Addr) bool) {
		for addr := range targets {
			if scope.Contains(addr) && !yield(addr) {
				return
			}
		}
	}, nil
}
