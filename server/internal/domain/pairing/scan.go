package pairing

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/netip"
	"strconv"
	"sync"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
)

type portScanner interface {
	Scan(ctx context.Context, addrs iter.Seq[netip.Addr], ports []uint16, emit func(netscan.HostResult) error) error
}

// DiscoverWithNmap discovers miners on a network using TCP connect probes.
// The wire mode is renamed with the coordinated contract update.
func (s *Service) DiscoverWithNmap(ctx context.Context, r *pb.NmapModeRequest) (<-chan *pb.DiscoverResponse, error) {
	rawTargets, err := s.resolveNmapTargets(ctx, r.Target)
	if err != nil {
		return nil, err
	}
	targets := make([]netscan.Target, 0, len(rawTargets))
	for _, raw := range rawTargets {
		target, err := netscan.ParseTarget(raw)
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentError(err.Error())
		}
		targets = append(targets, target)
	}
	ports, err := s.scanPorts(ctx, r.Ports)
	if err != nil {
		return nil, err
	}
	return s.discoverTargets(ctx, targets, ports, true), nil
}

// DiscoverWithIPRange probes plugins over an inclusive IPv4 interval lazily,
// including .0 and .1 when requested. There is no aggregate server-local limit.
func (s *Service) DiscoverWithIPRange(ctx context.Context, r *pb.IPRangeModeRequest) (<-chan *pb.DiscoverResponse, error) {
	target, err := netscan.Range(r.StartIp, r.EndIp)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentError(err.Error())
	}
	ports, err := s.scanPorts(ctx, r.Ports)
	if err != nil {
		return nil, err
	}
	return s.discoverTargets(ctx, []netscan.Target{target}, ports, false), nil
}

// DiscoverWithIPList resolves each hostname once before plugin probing. Literal
// addresses retain the server's existing public/private address policy.
func (s *Service) DiscoverWithIPList(ctx context.Context, r *pb.IPListModeRequest) (<-chan *pb.DiscoverResponse, error) {
	targets := make([]netscan.Target, 0, len(r.IpAddresses))
	for _, raw := range r.IpAddresses {
		target, err := netscan.ParseAddrTarget(raw)
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentError(err.Error())
		}
		targets = append(targets, target)
	}
	ports, err := s.scanPorts(ctx, r.Ports)
	if err != nil {
		return nil, err
	}
	return s.discoverTargets(ctx, targets, ports, false), nil
}

func (s *Service) scanPorts(ctx context.Context, raw []string) ([]uint16, error) {
	ports, err := s.resolveDiscoveryPorts(ctx, raw)
	if err != nil {
		return nil, err
	}
	parsed, err := netscan.Ports(ports, nil)
	if err != nil {
		if len(raw) == 0 {
			return nil, fleeterror.NewInternalErrorf("invalid default discovery ports: %v", err)
		}
		return nil, fleeterror.NewInvalidArgumentError(err.Error())
	}
	return parsed, nil
}

// discoverTargets pipelines resolved targets into bounded plugin work. Network
// scans prefilter open TCP ports; explicit lists/ranges reach plugins directly,
// including virtual miners with no TCP listener. Only these host
// workers write responses; late plugin calls write to private buffered channels.
// Closing the response stream therefore never races a cancelled plugin.
func (s *Service) discoverTargets(ctx context.Context, targets []netscan.Target, ports []uint16, scanOpenPorts bool) <-chan *pb.DiscoverResponse {
	raw := make(chan *pb.DiscoverResponse)
	results := dedupeDiscoverResponses(ctx, raw)
	go func() {
		defer close(raw)
		scanCtx, cancel := context.WithTimeout(ctx, defaultIPDiscoveryTimeoutSecs*time.Second)
		defer cancel()
		var workerCount uint64
		for _, target := range targets {
			// Each unresolved hostname produces at most one address.
			workerCount = min(concurrentDiscoveryLimit, workerCount+max(1, target.Count()))
			if workerCount == concurrentDiscoveryLimit {
				break
			}
		}
		type hostTarget struct {
			addr  netip.Addr
			ports []uint16
		}
		hosts := make(chan hostTarget, concurrentDiscoveryLimit)
		var workers sync.WaitGroup
		var processingErr error
		var recordProcessingErr sync.Once
		for range workerCount {
			workers.Go(func() {
				for {
					select {
					case <-scanCtx.Done():
						return
					case host, ok := <-hosts:
						if !ok || scanCtx.Err() != nil {
							return
						}
						probePorts := make([]string, len(host.ports))
						for i, port := range host.ports {
							probePorts[i] = strconv.Itoa(int(port))
						}
						if err := s.discoverAllPortsForIP(scanCtx, host.addr.String(), probePorts, raw); err != nil {
							recordProcessingErr.Do(func() { processingErr = err })
						}
					}
				}
			})
		}

		var resolutionErr error
		addresses := func(yield func(netip.Addr) bool) {
			// Deduplicate resolved input targets, not every address in broad
			// subnets, so enumeration stays bounded by the request size.
			seenTargets := make(map[netscan.Target]struct{}, len(targets))
			for _, target := range targets {
				resolved, err := target.Resolve(scanCtx, s.resolver, false)
				if err != nil {
					if resolutionErr == nil {
						resolutionErr = err
					}
					if scanCtx.Err() != nil {
						return
					}
					continue
				}
				if _, seen := seenTargets[resolved]; seen {
					continue
				}
				seenTargets[resolved] = struct{}{}
				for addr := range resolved.Addresses() {
					if !yield(addr) {
						return
					}
				}
			}
		}
		enqueue := func(addr netip.Addr, ports []uint16) error {
			select {
			case hosts <- hostTarget{addr: addr, ports: ports}:
				return nil
			case <-scanCtx.Done():
				return scanCtx.Err()
			}
		}
		var err error
		if scanOpenPorts {
			err = s.scanner.Scan(scanCtx, addresses, ports, func(host netscan.HostResult) error {
				return enqueue(host.Addr, host.OpenPorts)
			})
		} else {
			for addr := range addresses {
				if err = enqueue(addr, ports); err != nil {
					break
				}
			}
		}
		close(hosts)
		workers.Wait()
		if err == nil {
			err = scanCtx.Err()
		}
		if err == nil {
			err = resolutionErr
		}
		if err == nil {
			err = processingErr
		}
		if err == nil || ctx.Err() != nil {
			return
		}
		message := fmt.Sprintf("Fleet Server network discovery incomplete: %v", err)
		if errors.Is(err, context.DeadlineExceeded) {
			message = "Fleet Server network discovery timed out; some devices may not have been discovered"
		}
		// The scan budget may be exhausted, but the client stream is still live.
		select {
		case raw <- &pb.DiscoverResponse{Warning: message}:
		case <-ctx.Done():
		}
	}()
	return results
}
