package main

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
)

type portScanner interface {
	Scan(ctx context.Context, addrs iter.Seq[netip.Addr], ports []uint16, emitHost func(netscan.HostResult) error) error
}

// Explicit targets go straight to plugin identification. Plugins such as the
// virtual driver can identify devices without a listening TCP endpoint.
func (r *RunCmd) probeTargets(ctx context.Context, addrs iter.Seq[netip.Addr], ports []uint16, logger *slog.Logger) ([]*pb.DiscoveredDeviceReport, bool, error) {
	endpoints := func(yield func(endpoint) bool) {
		for addr := range addrs {
			for _, port := range ports {
				if ctx.Err() != nil || !yield(endpoint{ip: addr.String(), port: strconv.Itoa(int(port))}) {
					return
				}
			}
		}
	}
	reports, truncated := fanOutProbes(ctx, endpoints, probeConcurrency, r.discoverer.Probe, logger)
	if err := ctx.Err(); err != nil {
		return reports, truncated, fmt.Errorf("probe targets: %w", err)
	}
	return reports, truncated, nil
}

// scanAndProbe streams open ports into plugin identification through one bounded
// bridge. A scanner failure closes that bridge without canceling identification,
// so completed and already queued hosts can still produce reports.
func (r *RunCmd) scanAndProbe(ctx context.Context, addrs iter.Seq[netip.Addr], ports []uint16, logger *slog.Logger) ([]*pb.DiscoveredDeviceReport, bool, error) {
	r.scannerOnce.Do(func() {
		if r.scanner == nil {
			r.scanner = netscan.NewScanner()
		}
	})
	scanCtx, cancelScan := context.WithCancel(ctx)
	defer cancelScan()
	endpoints := make(chan endpoint, probeConcurrency)
	scanDone := make(chan error, 1)
	go func() {
		err := r.scanner.Scan(scanCtx, addrs, ports, func(host netscan.HostResult) error {
			for _, port := range host.OpenPorts {
				select {
				case endpoints <- endpoint{ip: host.Addr.String(), port: strconv.Itoa(int(port))}:
				case <-scanCtx.Done():
					return scanCtx.Err()
				}
			}
			return nil
		})
		close(endpoints)
		scanDone <- err
	}()
	pending := func(yield func(endpoint) bool) {
		for {
			select {
			case e, ok := <-endpoints:
				if !ok || !yield(e) {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
	reports, truncated := fanOutProbes(ctx, pending, probeConcurrency, r.discoverer.Probe, logger)
	// If probing stopped early, release scanner callbacks blocked on the bridge
	// and drain scanner workers before acknowledging the command.
	cancelScan()
	return reports, truncated, <-scanDone
}

func (r *RunCmd) networkScanTargets(ctx context.Context, req *pairingpb.NetworkScanModeRequest) (iter.Seq[netip.Addr], error) {
	raw := strings.TrimSpace(req.GetTarget())
	if raw != netscan.LocalSubnetTarget {
		target, err := netscan.ParseBoundedTarget(raw)
		if err != nil {
			return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "%s", err)
		}
		target, err = target.Resolve(ctx, r.resolver, true)
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("resolve scan target: %w", ctx.Err())
			}
			if isDNSResolutionError(err) {
				return nil, cmdErr(pb.AckCode_ACK_CODE_SCAN_FAILED, "%s", err)
			}
			return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "%s", err)
		}
		return netscan.InterleavedAddresses([]netscan.Target{target}), nil
	}

	scope, err := r.effectiveDiscoveryScope()
	if err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "%s", err)
	}
	if scope.Empty() {
		return nil, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "configure discovery-ranges before discovering new miners")
	}
	count := 0
	for range scope.Addresses() {
		count++
		if count > maxIPsPerCommand {
			return nil, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "automatic scan exceeds the limit of %d addresses; narrow discovery-ranges or use a smaller manual target", maxIPsPerCommand)
		}
	}
	return scope.Addresses(), nil
}

func isDNSResolutionError(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}
