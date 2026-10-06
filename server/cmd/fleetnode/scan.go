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
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
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

var errNoLocalSubnet = errors.New("no local IPv4 subnet found")

// Automatic subnet detection retains the existing primary-interface policy.
// Operators can override it on multi-NIC hosts or platforms without detection.
func (r *RunCmd) detectLocalSubnets() ([]string, error) {
	if r.localSubnets != nil {
		return r.localSubnets()
	}
	if configured := strings.TrimSpace(r.LocalDiscoverySubnet); configured != "" {
		return []string{configured}, nil
	}
	info, err := networking.GetLocalNetworkInfo()
	if err != nil {
		return nil, fmt.Errorf("get local network info: %w", err)
	}
	if info.Subnet == "" {
		return nil, errNoLocalSubnet
	}
	return []string{info.Subnet}, nil
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

	subnets, err := r.detectLocalSubnets()
	if err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "no connected private IPv4 subnet for local-subnet scan: %s", err)
	}
	if len(subnets) == 0 {
		return nil, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "%s", errNoLocalSubnet)
	}
	targets := make([]netscan.Target, 0, len(subnets))
	var count uint64
	for _, raw := range subnets {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || !prefix.Addr().Is4() {
			return nil, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "local-subnet scan target %q must be an IPv4 CIDR", raw)
		}
		target, err := netscan.ParseBoundedTarget(raw)
		if err != nil {
			return nil, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "local-subnet scan target %q is not scannable: %s", raw, err)
		}
		if !target.IsPrivate() {
			return nil, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "local-subnet scan target %q must be private", raw)
		}
		count += target.Count()
		if count > maxIPsPerCommand {
			return nil, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "local subnets contain %d addresses, exceeds the limit of %d", count, maxIPsPerCommand)
		}
		targets = append(targets, target)
	}
	return func(yield func(netip.Addr) bool) {
		seen := make(map[netip.Addr]struct{})
		for addr := range netscan.InterleavedAddresses(targets) {
			if _, duplicate := seen[addr]; duplicate {
				continue
			}
			seen[addr] = struct{}{}
			if !yield(addr) {
				return
			}
		}
	}, nil
}

func isDNSResolutionError(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}
