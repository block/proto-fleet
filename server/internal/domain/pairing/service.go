package pairing

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	capabilitiespb "github.com/block/proto-fleet/server/generated/grpc/capabilities/v1"
	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetoptions"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/minerdiscovery"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	tmodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	tokenDomain "github.com/block/proto-fleet/server/internal/domain/token"
	"github.com/block/proto-fleet/server/internal/domain/workername"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	id "github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"

	"github.com/Ullaakut/nmap/v3"
	"github.com/grandcat/zeroconf"
)

const (
	// concurrentDiscoveryLimit caps concurrent IP probes. 254 covers a full /24 without queuing
	// (.0 network and .1 gateway are skipped, leaving .2–.255 = 254 usable addresses);
	// probes are I/O-bound so goroutine count is not a CPU concern.
	concurrentDiscoveryLimit = 254

	// MaxPortsPerIP caps per-IP parallel port fan-out to prevent resource exhaustion from
	// caller-supplied port lists.
	MaxPortsPerIP = 10

	// globalProbeLimit caps total concurrent TCP dials across all IPs and ports. Each dial
	// holds an OS file descriptor for its duration; 512 leaves headroom for DB connections
	// and other process FDs while staying well below typical OS limits (1024–65536).
	globalProbeLimit = 512

	// IP address constants for network address filtering
	networkAddressLastOctet = 0   // Network address last octet (.0)
	gatewayAddressLastOctet = 1   // Gateway address last octet (.1)
	firstHostAddressOffset  = 2   // First usable host address offset
	localhostFirstOctet     = 127 // Localhost IP range first octet (127.x.x.x)

	// IP address bit masks
	ipv4LocalhostMask   = 0xFF000000 // Mask for checking IPv4 address class
	ipv4LocalhostPrefix = 0x7F000000 // IPv4 localhost prefix (127.0.0.0/8)
	ipv4LastOctetMask   = 0xFF       // Mask for extracting last octet

	// Discovery timeout constants
	defaultNmapTimeoutSeconds     = 600              // Overall timeout for nmap discovery operation (10 minutes)
	defaultIPDiscoveryTimeoutSecs = 600              // Overall timeout for IP-based discovery (10 minutes)
	perDeviceDiscoveryTimeout     = 10 * time.Second // Timeout for probing a single device
	perDevicePairingTimeout       = 30 * time.Second // Timeout for pairing a single device (plugin RPC + DB writes)

	// Nmap tuning parameters for faster scanning
	nmapMaxRetriesPerHost = 1 // Reduce retries to speed up scanning of unresponsive hosts

	// nmapHostTimeoutMilliseconds is the max time nmap waits for a single host to respond.
	// 10s allows slow devices to respond while keeping scans reasonably fast.
	nmapHostTimeoutMilliseconds = 10000

	// nmapMinRTTTimeoutMilliseconds sets the minimum round-trip time (RTT) for probe packets.
	// This sets a floor on how long nmap waits before retransmitting probes. 100ms is a reasonable
	// baseline for local networks - lower values may cause unnecessary retransmissions.
	nmapMinRTTTimeoutMilliseconds = 100

	discoveryPortsUnavailableError = "no discovery ports were provided and no loaded plugins reported canonical discovery ports"
)

// shouldSkipNetworkOrGatewayAddress returns true if the IPv4 address is a network address (.0)
// or gateway address (.1), except for localhost addresses (127.x.x.x).
// For IPv6 addresses (16 bytes), this always returns false since IPv6 has no .0/.1 convention.
// Network and gateway addresses should be skipped during discovery to avoid false positives
// where all devices appear to respond at the gateway IP.
func shouldSkipNetworkOrGatewayAddress(ip net.IP) bool {
	if ip == nil || len(ip) != 4 {
		return false
	}

	// Check if this is localhost (127.x.x.x)
	isLocalhost := ip[0] == localhostFirstOctet
	if isLocalhost {
		return false // Don't skip localhost addresses
	}

	// Check last octet for network (.0) or gateway (.1) addresses
	lastOctet := ip[3]
	return lastOctet == networkAddressLastOctet || lastOctet == gatewayAddressLastOctet
}

// adjustIPRangeStartForNetworkAddresses adjusts an IPv4 address (as uint32) to skip
// network (.0) and gateway (.1) addresses, except for localhost (127.x.x.x).
// Returns the adjusted IP address as uint32.
func adjustIPRangeStartForNetworkAddresses(ipAddr uint32) uint32 {
	// Check if this is localhost (127.x.x.x)
	isLocalhost := (ipAddr & ipv4LocalhostMask) == ipv4LocalhostPrefix
	if isLocalhost {
		return ipAddr // Don't adjust localhost addresses
	}

	lastOctet := ipAddr & ipv4LastOctetMask
	switch lastOctet {
	case networkAddressLastOctet:
		// Network address (.0), skip to .2
		return ipAddr + firstHostAddressOffset
	case gatewayAddressLastOctet:
		// Gateway address (.1), skip to .2
		return ipAddr + 1
	}
	return ipAddr
}

func dedupeDiscoverResponses(source <-chan *pb.DiscoverResponse) <-chan *pb.DiscoverResponse {
	resultChan := make(chan *pb.DiscoverResponse)

	go func() {
		defer close(resultChan)

		seenDevices := make(map[string]struct{})

		for result := range source {
			if result == nil {
				continue
			}

			dedupedDevices := make([]*pb.Device, 0, len(result.Devices))
			for _, device := range result.Devices {
				if device == nil {
					continue
				}

				identity := device.DeviceIdentifier
				if identity == "" {
					identity = fmt.Sprintf("%s:%s", device.IpAddress, device.Port)
				}

				if _, alreadySeen := seenDevices[identity]; alreadySeen {
					continue
				}

				seenDevices[identity] = struct{}{}
				dedupedDevices = append(dedupedDevices, device)
			}

			if len(dedupedDevices) == 0 && result.Error == "" {
				continue
			}

			resultChan <- &pb.DiscoverResponse{
				Devices: dedupedDevices,
				Error:   result.Error,
			}
		}
	}()

	return resultChan
}

//go:generate go run go.uber.org/mock/mockgen -source=service.go -destination=mocks/mock_service.go -package=mocks Listener,CapabilitiesProvider
type Listener interface {
	AddDevices(ctx context.Context, deviceID ...tmodels.DeviceIdentifier) error
}

type devicePairingStatusProvider interface {
	GetDevicePairingStatusByIdentifier(ctx context.Context, deviceIdentifier string, orgID int64) (string, error)
}

// CapabilitiesProvider provides miner capabilities from plugins
type CapabilitiesProvider interface {
	GetMinerCapabilitiesForDevice(ctx context.Context, device *pb.Device) *capabilitiespb.MinerCapabilities
	// GetDefaultDiscoveryPorts returns the stock discovery scan set used when a
	// discovery request omits explicit ports.
	GetDefaultDiscoveryPorts(ctx context.Context) []string
	GetDiscoveryPorts(ctx context.Context) []string
}

// Service handles the core device discovery functionality
type Service struct {
	discoveredDeviceStore interfaces.DiscoveredDeviceStore
	deviceStore           interfaces.DeviceStore
	transactor            interfaces.Transactor
	tokenService          *tokenDomain.Service
	discoverer            minerdiscovery.Discoverer
	capabilitiesProvider  CapabilitiesProvider
	pairer                Pairer
	listener              Listener
	localNetworkInfo      func(context.Context) (*NetworkInfo, error)
	probeSemaphore        chan struct{}
	invalidateMiner       func(models.DeviceIdentifier)
	optionsCache          *fleetoptions.Cache
}

func NewService(
	discoveredDeviceStore interfaces.DiscoveredDeviceStore,
	deviceStore interfaces.DeviceStore,
	transactor interfaces.Transactor,
	tokenService *tokenDomain.Service,
	discoverer minerdiscovery.Discoverer,
	capabilitiesProvider CapabilitiesProvider,
	listener Listener,
	pairer Pairer,
) *Service {
	return &Service{
		discoveredDeviceStore: discoveredDeviceStore,
		deviceStore:           deviceStore,
		transactor:            transactor,
		tokenService:          tokenService,
		discoverer:            discoverer,
		capabilitiesProvider:  capabilitiesProvider,
		pairer:                pairer,
		listener:              listener,
		localNetworkInfo:      defaultLocalNetworkInfo,
		probeSemaphore:        make(chan struct{}, globalProbeLimit),
	}
}

// WithMinerInvalidator sets a callback to evict cached miner handles on rediscovery
// (IP/port changes) and successful pairing (credential updates).
func (s *Service) WithMinerInvalidator(invalidate func(models.DeviceIdentifier)) {
	s.invalidateMiner = invalidate
}

// WithOptionsCache wires the per-org fleet options cache so pairing writes
// can invalidate stale model/firmware lists. Pass nil to disable.
func (s *Service) WithOptionsCache(cache *fleetoptions.Cache) {
	s.optionsCache = cache
}

// Helper function to convert IP string to uint32 for range comparison
func ipToUint32(ip string) (uint32, error) {
	addr := net.ParseIP(ip)
	ipv4 := addr.To4()
	if ipv4 == nil {
		return 0, fleeterror.NewInternalErrorf("not a valid IPv4 address: '%v'", ip)
	}
	return binary.BigEndian.Uint32(ipv4), nil
}

// Helper function to convert uint32 to IP string
func uint32ToIP(n uint32) string {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, n)
	return ip.String()
}

type NetworkInfo struct {
	networking.NetworkInfo
}

func defaultLocalNetworkInfo(_ context.Context) (*NetworkInfo, error) {
	info, err := networking.GetLocalNetworkInfo()
	if err != nil {
		return nil, err
	}
	return &NetworkInfo{info}, nil
}

func (s *Service) GetLocalNetworkInfo(ctx context.Context) (*NetworkInfo, error) {
	if s.localNetworkInfo != nil {
		return s.localNetworkInfo(ctx)
	}
	return defaultLocalNetworkInfo(ctx)
}

func canonicalCIDR(cidr string) (canonical string, maskBits int, isIPv4 bool, ok bool) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", 0, false, false
	}

	ones, _ := ipNet.Mask.Size()
	canonical = (&net.IPNet{
		IP:   ipNet.IP.Mask(ipNet.Mask),
		Mask: ipNet.Mask,
	}).String()

	return canonical, ones, ipNet.IP.To4() != nil, true
}

func maskBitsForLocalSubnetTarget(target string, localSubnet string) (int, bool) {
	canonicalTarget, targetMaskBits, _, ok := canonicalCIDR(target)
	if !ok {
		return 0, false
	}

	canonicalLocalSubnet, _, _, ok := canonicalCIDR(localSubnet)
	if !ok || canonicalTarget != canonicalLocalSubnet {
		return 0, false
	}

	return targetMaskBits, true
}

func mergeAutoDiscoveryTargets(baseTarget string, knownSubnets []string) []string {
	canonicalBaseTarget, _, baseIsIPv4, ok := canonicalCIDR(baseTarget)
	if !ok {
		return []string{baseTarget}
	}

	targets := []string{canonicalBaseTarget}
	seen := map[string]struct{}{
		canonicalBaseTarget: {},
	}

	for _, subnet := range knownSubnets {
		canonicalSubnet, _, subnetIsIPv4, ok := canonicalCIDR(subnet)
		if !ok || subnetIsIPv4 != baseIsIPv4 {
			continue
		}
		if _, exists := seen[canonicalSubnet]; exists {
			continue
		}

		seen[canonicalSubnet] = struct{}{}
		targets = append(targets, canonicalSubnet)
	}

	return targets
}

func (s *Service) resolveNmapTargets(ctx context.Context, target string) ([]string, error) {
	targets := []string{target}

	localNetworkInfo, err := s.GetLocalNetworkInfo(ctx)
	if err != nil {
		slog.Debug("Skipping known-subnet expansion for nmap discovery because local network info is unavailable",
			"target", target,
			"error", err)
		return targets, nil
	}

	maskBits, shouldExpand := maskBitsForLocalSubnetTarget(target, localNetworkInfo.Subnet)
	if !shouldExpand {
		slog.Debug("Skipping known-subnet expansion because target does not match local subnet",
			"target", target,
			"local_subnet", localNetworkInfo.Subnet)
		return targets, nil
	}

	// Subnet expansion only runs for IPv4 targets matching the local subnet
	// (the guard above ensures this). Pass isIPv4=true directly.
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	knownSubnets, err := s.deviceStore.GetKnownSubnets(ctx, info.OrganizationID, maskBits, true)
	if err != nil {
		slog.Debug("Skipping known-subnet expansion because subnet query failed",
			"target", target,
			"error", err)
		return targets, nil
	}

	expandedTargets := mergeAutoDiscoveryTargets(target, knownSubnets)
	if len(expandedTargets) > 1 {
		slog.Info("Expanded auto-discovery scan targets from paired miners",
			"base_target", target,
			"targets", expandedTargets,
			"organization_id", info.OrganizationID)
	}

	return expandedTargets, nil
}

// validateNmapTargets validates targets and resolves hostnames to IP literals
// so nmap receives concrete addresses. Hostnames are replaced with their
// resolved IP, preferring IPv4 to avoid flipping a dual-stack host into
// IPv6-only mode. The returned flag indicates whether -6 is needed.
func validateNmapTargets(ctx context.Context, targets []string, lookupIPAddr func(context.Context, string) ([]net.IPAddr, error)) ([]string, bool, error) {
	resolved := make([]string, 0, len(targets))
	var useIPv6 bool
	for _, t := range targets {
		if _, ipNet, err := net.ParseCIDR(t); err == nil {
			if _, bits := ipNet.Mask.Size(); bits == 128 {
				return nil, false, fleeterror.NewInvalidArgumentError(
					"IPv6 CIDR subnet scanning is not supported; use mDNS or IP list discovery for IPv6 devices")
			}
			resolved = append(resolved, t)
		} else if ip := net.ParseIP(t); ip != nil {
			if ip.To4() == nil {
				useIPv6 = true
			}
			resolved = append(resolved, t)
		} else {
			// Hostname — resolve and substitute the IP, preferring IPv4 so
			// dual-stack hosts don't lose their v4 scan.
			addrs, err := lookupIPAddr(ctx, t)
			if err != nil || len(addrs) == 0 {
				// Keep the original hostname; let nmap resolve it.
				resolved = append(resolved, t)
				continue
			}
			var ipv4, ipv6 string
			for _, addr := range addrs {
				if addr.IP.To4() != nil && ipv4 == "" {
					ipv4 = addr.IP.String()
				} else if addr.IP.To4() == nil && ipv6 == "" {
					ipv6 = addr.IP.String()
				}
			}
			if ipv4 != "" {
				resolved = append(resolved, ipv4)
			} else {
				resolved = append(resolved, ipv6)
				useIPv6 = true
			}
		}
	}
	return resolved, useIPv6, nil
}

func (s *Service) resolveDiscoveryPorts(ctx context.Context, requestPorts []string) ([]string, error) {
	if len(requestPorts) > 0 {
		slog.Debug("Resolved discovery ports from request override", "ports", requestPorts)
		return requestPorts, nil
	}

	ports := s.capabilitiesProvider.GetDefaultDiscoveryPorts(ctx)
	if len(ports) == 0 {
		return nil, fleeterror.NewInvalidArgumentError(discoveryPortsUnavailableError)
	}

	slog.Debug("Resolved discovery ports from plugin default scan set", "ports", ports)
	return ports, nil
}

// DiscoverWithMDNS discovers devices using mDNS
func (s *Service) DiscoverWithMDNS(ctx context.Context, r *pb.MDNSModeRequest) (<-chan *pb.DiscoverResponse, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to initialize resolver: %v", err)
	}

	// Create channels after validation to avoid leaking the dedupe goroutine on early returns.
	rawResultChan := make(chan *pb.DiscoverResponse)
	resultChan := dedupeDiscoverResponses(rawResultChan)

	entries := make(chan *zeroconf.ServiceEntry)
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(r.TimeoutSeconds)*time.Second)

	go func() {
		defer cancel()
		defer close(rawResultChan)

		err := resolver.Browse(timeoutCtx, r.ServiceType, "local.", entries)
		if err != nil {
			rawResultChan <- &pb.DiscoverResponse{
				Error: fmt.Sprintf("failed to browse: %v", err),
			}
			return
		}

		for {
			select {
			case entry := <-entries:
				if entry == nil {
					return
				}

				// NOTE: mDNS records are unauthenticated — a malicious host on the
				// LAN can advertise arbitrary addresses. This is the same trust model
				// as the pre-existing IPv4 mDNS path. On-link verification would
				// require plumbing interface scope through the entire discovery stack.
				var ipAddress string
				if len(entry.AddrIPv4) > 0 {
					ipAddress = entry.AddrIPv4[0].String()
				} else {
					// Fall back to a non-link-local IPv6 address. Link-local (fe80::/10)
					// addresses require an interface scope (%eth0) that net.IP.String()
					// does not preserve, so they cannot be used for TCP connections.
					for _, addr := range entry.AddrIPv6 {
						if !addr.IsLinkLocalUnicast() {
							ipAddress = addr.String()
							break
						}
					}
					if ipAddress == "" {
						if len(entry.AddrIPv6) > 0 {
							slog.Warn("mDNS device has only link-local IPv6 addresses which require interface scope; skipping",
								"service", entry.ServiceInstanceName(),
								"ipv6_addrs", entry.AddrIPv6)
						}
						continue
					}
				}
				portStr := fmt.Sprintf("%d", entry.Port)

				_, err := s.discoverDevice(ctx, ipAddress, portStr, rawResultChan)
				if err != nil {
					slog.Debug("device discovery failed", "error", err)
				}

			case <-timeoutCtx.Done():
				return
			}
		}
	}()

	return resultChan, nil
}

// DiscoverWithNmap discovers devices using Nmap
func (s *Service) DiscoverWithNmap(ctx context.Context, r *pb.NmapModeRequest) (<-chan *pb.DiscoverResponse, error) {
	if r.Target == "" {
		return nil, fleeterror.NewInvalidArgumentError("nmap discovery target is required")
	}
	ports, err := s.resolveDiscoveryPorts(ctx, r.Ports)
	if err != nil {
		return nil, err
	}
	targets, err := s.resolveNmapTargets(ctx, r.Target)
	if err != nil {
		return nil, err
	}

	// Apply server-controlled timeout before any DNS work so hostname
	// resolution cannot outlive the scan budget.
	timeoutCtx, cancel := context.WithTimeout(ctx, defaultNmapTimeoutSeconds*time.Second)

	targets, useIPv6Scanning, err := validateNmapTargets(timeoutCtx, targets, net.DefaultResolver.LookupIPAddr)
	if err != nil {
		cancel()
		return nil, err
	}

	// Create channels after validation to avoid leaking the dedupe goroutine on early returns.
	rawResultChan := make(chan *pb.DiscoverResponse)
	resultChan := dedupeDiscoverResponses(rawResultChan)

	go func() {
		defer cancel()
		defer close(rawResultChan)

		var scanner *nmap.Scanner
		var err error

		// Common nmap options for faster scanning
		nmapOpts := []nmap.Option{
			nmap.WithTargets(targets...),
			nmap.WithUnique(),
			nmap.WithDisabledDNSResolution(),
			nmap.WithTimingTemplate(nmap.TimingAggressive), // -T4 for faster scanning
			nmap.WithMaxRetries(nmapMaxRetriesPerHost),
			nmap.WithHostTimeout(time.Duration(nmapHostTimeoutMilliseconds) * time.Millisecond),
			nmap.WithMinRTTTimeout(time.Duration(nmapMinRTTTimeoutMilliseconds) * time.Millisecond),
		}

		nmapOpts = append(nmapOpts, nmap.WithPorts(strings.Join(ports, ",")))

		if useIPv6Scanning {
			nmapOpts = append(nmapOpts, nmap.WithIPv6Scanning())
		}

		scanner, err = nmap.NewScanner(timeoutCtx, nmapOpts...)
		if err != nil {
			rawResultChan <- &pb.DiscoverResponse{
				Error: fmt.Sprintf("failed to create scanner: %v", err),
			}
			return
		}

		slog.Debug("Starting nmap scan",
			"targets", targets,
			"ports", ports,
			"timeout_seconds", defaultNmapTimeoutSeconds)

		result, _, err := scanner.Run()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				slog.Info("Nmap scan timed out",
					"targets", targets,
					"timeout_seconds", defaultNmapTimeoutSeconds)
				// After timeout, we cannot probe hosts because the context is expired.
				// Send timeout error and return.
				select {
				case rawResultChan <- &pb.DiscoverResponse{
					Error: fmt.Sprintf("scan timed out after %d seconds; some devices may not have been discovered", defaultNmapTimeoutSeconds),
				}:
				case <-timeoutCtx.Done():
				}
				return
			}
			// Non-timeout error
			select {
			case rawResultChan <- &pb.DiscoverResponse{
				Error: fmt.Sprintf("scan failed: %v", err),
			}:
			case <-timeoutCtx.Done():
			}
			return
		}

		if result == nil {
			// Scan timed out with no results - notify caller explicitly
			select {
			case rawResultChan <- &pb.DiscoverResponse{
				Error: fmt.Sprintf("scan timed out after %d seconds without finding any hosts; verify the target network range is correct and devices are powered on", defaultNmapTimeoutSeconds),
			}:
			case <-timeoutCtx.Done():
			}
			return
		}

		// Collect all host:port combinations to probe
		type hostPort struct {
			ip   string
			port string
		}
		var hostsToProbe []hostPort

		for _, host := range result.Hosts {
			if len(host.Addresses) == 0 {
				continue
			}

			var openPortCount int32
			for _, p := range host.Ports {
				if p.Status() == "open" {
					openPortCount++
				}
			}
			if openPortCount == 0 {
				continue
			}

			var ipAddress string
			for _, addr := range host.Addresses {
				if addr.AddrType == "ipv4" || addr.AddrType == "ipv6" {
					ipAddress = addr.Addr
					break
				}
			}

			if ipAddress == "" {
				continue
			}

			// Skip network address (.0) and gateway (.1) to avoid discovery issues
			parsedIP := net.ParseIP(ipAddress)
			if parsedIP != nil {
				ipv4 := parsedIP.To4()
				if shouldSkipNetworkOrGatewayAddress(ipv4) {
					slog.Debug("Skipping network/gateway address", "ip", ipAddress)
					continue
				}
			}

			for _, port := range host.Ports {
				if port.Status() == "open" {
					hostsToProbe = append(hostsToProbe, hostPort{
						ip:   ipAddress,
						port: fmt.Sprintf("%d", port.ID),
					})
				}
			}
		}

		slog.Debug("Probing discovered hosts",
			"hosts_to_probe", len(hostsToProbe))

		// Probe discovered hosts in parallel with concurrency limit
		var wg sync.WaitGroup
		semaphore := make(chan struct{}, concurrentDiscoveryLimit)

		for _, hp := range hostsToProbe {
			// Acquire semaphore with timeout support to prevent goroutine leak
			select {
			case <-timeoutCtx.Done():
				slog.Debug("Discovery timeout reached, stopping device probing")
				wg.Wait()
				return
			case semaphore <- struct{}{}:
				wg.Add(1)
				go func(ip, port string) {
					defer wg.Done()
					defer func() { <-semaphore }()

					_, err := s.discoverDevice(timeoutCtx, ip, port, rawResultChan)
					if err != nil {
						slog.Debug("device discovery failed", "ip", ip, "port", port, "error", err)
					}
				}(hp.ip, hp.port)
			}
		}

		wg.Wait()
	}()

	return resultChan, nil
}

// DiscoverWithIPRange discovers devices using an IPv4 IP range.
// IPv6 is not supported for range-based discovery; use mDNS or IP list for IPv6 devices.
func (s *Service) DiscoverWithIPRange(ctx context.Context, r *pb.IPRangeModeRequest) (<-chan *pb.DiscoverResponse, error) {
	startIP, err := ipToUint32(r.StartIp)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("error parsing start ip: %v", err)
	}
	endIP, err := ipToUint32(r.EndIp)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("error parsing end ip: %v", err)
	}

	// Skip network address (.0) and gateway (.1) to avoid discovery issues
	startIP = adjustIPRangeStartForNetworkAddresses(startIP)

	ports, err := s.resolveDiscoveryPorts(ctx, r.Ports)
	if err != nil {
		return nil, err
	}
	if len(ports) > MaxPortsPerIP {
		return nil, fleeterror.NewInvalidArgumentErrorf("too many ports: %d exceeds the limit of %d", len(ports), MaxPortsPerIP)
	}

	// Create channels after validation to avoid leaking the dedupe goroutine on early returns.
	rawResultChan := make(chan *pb.DiscoverResponse)
	resultChan := dedupeDiscoverResponses(rawResultChan)

	// Apply server-controlled timeout for the entire discovery operation
	timeoutCtx, cancel := context.WithTimeout(ctx, defaultIPDiscoveryTimeoutSecs*time.Second)

	go func() {
		defer cancel()
		defer close(rawResultChan)

		var wg sync.WaitGroup
		semaphore := make(chan struct{}, concurrentDiscoveryLimit)

		for ip := startIP; ip <= endIP; ip++ {
			// Acquire semaphore with timeout support to prevent goroutine leak
			select {
			case <-timeoutCtx.Done():
				slog.Debug("Discovery timeout reached, stopping IP range scan")
				wg.Wait()
				return
			case semaphore <- struct{}{}:
				wg.Add(1)
				go func(ipAddr string) {
					defer wg.Done()
					defer func() { <-semaphore }()

					s.discoverAllPortsForIP(timeoutCtx, ipAddr, ports, rawResultChan)
				}(uint32ToIP(ip))
			}
		}

		wg.Wait()
	}()

	return resultChan, nil
}

// DiscoverWithIPList discovers devices from a list of IPs
func (s *Service) DiscoverWithIPList(ctx context.Context, r *pb.IPListModeRequest) (<-chan *pb.DiscoverResponse, error) {
	ports, err := s.resolveDiscoveryPorts(ctx, r.Ports)
	if err != nil {
		return nil, err
	}
	if len(ports) > MaxPortsPerIP {
		return nil, fleeterror.NewInvalidArgumentErrorf("too many ports: %d exceeds the limit of %d", len(ports), MaxPortsPerIP)
	}

	// Create channels after validation to avoid leaking the dedupe goroutine on early returns.
	rawResultChan := make(chan *pb.DiscoverResponse)
	resultChan := dedupeDiscoverResponses(rawResultChan)

	// Apply server-controlled timeout for the entire discovery operation
	timeoutCtx, cancel := context.WithTimeout(ctx, defaultIPDiscoveryTimeoutSecs*time.Second)

	go func() {
		defer cancel()
		defer close(rawResultChan)

		var wg sync.WaitGroup
		semaphore := make(chan struct{}, concurrentDiscoveryLimit)

		for _, ip := range r.IpAddresses {
			// Acquire semaphore with timeout support to prevent goroutine leak
			select {
			case <-timeoutCtx.Done():
				slog.Debug("Discovery timeout reached, stopping IP list scan")
				wg.Wait()
				return
			case semaphore <- struct{}{}:
				wg.Add(1)
				go func(ipAddr string) {
					defer wg.Done()
					defer func() { <-semaphore }()

					// Reject scoped IPv6 literals (%zone) — the connection
					// stack does not propagate interface scope.
					if strings.Contains(ipAddr, "%") {
						slog.Debug("rejecting scoped IPv6 address", "input", ipAddr)
						return
					}
					if parsedIP := net.ParseIP(ipAddr); parsedIP != nil {
						// Reject link-local IPv6 — requires interface scope for
						// TCP connections, consistent with mDNS and hostname paths.
						if parsedIP.To4() == nil && parsedIP.IsLinkLocalUnicast() {
							slog.Debug("rejecting link-local IPv6 address", "input", ipAddr)
							return
						}
						// Normalize to canonical form so dedupe and storage
						// treat equivalent spellings (e.g. 2001:0DB8::1 vs
						// 2001:db8::1) as the same address.
						ipAddr = parsedIP.String()
					} else {
						var resolver net.Resolver
						addrs, err := resolver.LookupIPAddr(timeoutCtx, ipAddr)
						if err != nil {
							slog.Debug("hostname resolution failed, skipping entry", "input", ipAddr, "error", err)
							return
						}
						var resolvedIPv4, resolvedIPv6 string
						for _, addr := range addrs {
							if addr.IP.To4() != nil {
								resolvedIPv4 = addr.IP.String()
								break // IPv4 is preferred; no need to look further
							} else if resolvedIPv6 == "" && !addr.IP.IsLinkLocalUnicast() {
								// Skip link-local IPv6 (fe80::) because net.IP.String()
								// does not preserve the interface scope (%eth0) required
								// for TCP connections.
								resolvedIPv6 = addr.IP.String()
							}
						}
						resolved := resolvedIPv4
						if resolved == "" {
							resolved = resolvedIPv6
						}
						if resolved == "" {
							slog.Debug("hostname resolved but no address found, skipping entry", "input", ipAddr)
							return
						}
						ipAddr = resolved
					}

					s.discoverAllPortsForIP(timeoutCtx, ipAddr, ports, rawResultChan)
				}(ip)
			}
		}

		wg.Wait()
	}()

	return resultChan, nil
}

// discoverAllPortsForIP probes all ports for a single IP concurrently. Raw probe results are
// fed into a buffered channel so processDiscoveredDevice is called sequentially — preventing
// duplicate discovered_device rows from concurrent port wins. Siblings are cancelled only after
// processDiscoveredDevice returns found==true, preserving fallback to other ports when the first
// raw winner is collision-skipped or otherwise non-emitting. All probe goroutines are drained
// before returning so the outer semaphore slot is not released prematurely.
func (s *Service) discoverAllPortsForIP(ctx context.Context, ipAddr string, ports []string, resultChan chan<- *pb.DiscoverResponse) {
	portCtx, portCancel := context.WithCancel(ctx)
	defer portCancel()

	type rawResult struct {
		device *discoverymodels.DiscoveredDevice
		port   string
	}
	rawCh := make(chan rawResult, len(ports))

	var wg sync.WaitGroup
	for _, port := range ports {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			// Acquire the global probe semaphore before dialing to bound total FD usage
			// across all concurrently probed IPs. Release it as soon as Discover returns.
			select {
			case s.probeSemaphore <- struct{}{}:
			case <-portCtx.Done():
				return
			}
			probeCtx, cancel := context.WithTimeout(portCtx, perDeviceDiscoveryTimeout)
			defer cancel()
			device, err := s.discoverer.Discover(probeCtx, ipAddr, p)
			<-s.probeSemaphore
			if err != nil {
				if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
					slog.Debug("discovery failed", "ip", ipAddr, "port", p, "error", err)
				}
				return
			}
			select {
			case rawCh <- rawResult{device, p}:
			case <-portCtx.Done():
			}
		}(port)
	}

	// Close rawCh once all probes finish. Use a done channel so we can wait for closure
	// after draining rawCh below — ensuring no goroutines outlive this function.
	allDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(rawCh)
		close(allDone)
	}()

	for w := range rawCh {
		found, err := s.processDiscoveredDevice(ctx, w.device, ipAddr, w.port, resultChan)
		if err != nil {
			slog.Debug("failed to process discovered device", "ip", ipAddr, "port", w.port, "error", err)
		}
		if found {
			portCancel()
			break
		}
	}

	<-allDone
}

// discoverDevice attempts to discover a device at the given IP and port. It returns (true, nil)
// only when a device was found and successfully emitted to resultChan. It returns (false, nil)
// for handled-but-suppressed paths (e.g. paired-endpoint collision skip) so callers can
// distinguish "nothing found yet, keep scanning" from "device emitted, stop scanning".
func (s *Service) discoverDevice(ctx context.Context, ipAddress string, port string, resultChan chan<- *pb.DiscoverResponse) (bool, error) {
	// Apply per-device discovery timeout to prevent individual slow devices from blocking others
	discoveryCtx, cancel := context.WithTimeout(ctx, perDeviceDiscoveryTimeout)
	defer cancel()

	discoveredDevice, err := s.discoverer.Discover(discoveryCtx, ipAddress, port)
	if err != nil {
		// Only log non-timeout errors at debug level; timeouts are expected for non-miner hosts
		if !errors.Is(err, context.DeadlineExceeded) {
			slog.Debug("Discovery failed",
				"ipAddress", ipAddress,
				"port", port,
				"error", err)
		}

		return false, err
	}

	return s.processDiscoveredDevice(ctx, discoveredDevice, ipAddress, port, resultChan)
}

func (s *Service) processDiscoveredDevice(ctx context.Context, discoveredDevice *discoverymodels.DiscoveredDevice, scannedIP string, scannedPort string, resultChan chan<- *pb.DiscoverResponse) (bool, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return false, err
	}

	// Use existing device identifier if available
	deviceIdentifier := discoveredDevice.DeviceIdentifier
	preserveMissingFirmware := deviceIdentifier != ""
	if deviceIdentifier == "" {
		reconciledIdentifier, err := s.reconcileByMAC(ctx, discoveredDevice, info.OrganizationID, scannedIP, scannedPort)
		if err != nil {
			return false, err
		}
		reconciledByMAC := reconciledIdentifier != ""

		// Check if we've already discovered a device at this scanned IP:port
		// This prevents duplicate entries during network rescans
		// Note: We use scannedIP/scannedPort (what we scanned) not discoveredDevice IP/port
		// (which may have changed if device moved), to maintain stable identifiers per network endpoint
		existingDevice, err := s.discoveredDeviceStore.GetByIPAndPort(ctx, info.OrganizationID, scannedIP, scannedPort)
		if err != nil && !fleeterror.IsNotFoundError(err) {
			// Database error - propagate instead of silently generating new identifier
			return false, fleeterror.NewInternalErrorf("failed to check for existing device: %v", err)
		}
		if reconciledIdentifier == "" && existingDevice == nil {
			reconciledIdentifier, err = s.reconcileByIPAcrossDiscoveryPorts(ctx, discoveredDevice, info.OrganizationID, scannedIP, scannedPort)
			if err != nil {
				return false, err
			}
		}
		switch {
		case reconciledIdentifier != "":
			deviceIdentifier = reconciledIdentifier
			preserveMissingFirmware = reconciledByMAC
			// Device moved to a new IP/port; evict stale cached handle.
			if reconciledByMAC && s.invalidateMiner != nil {
				s.invalidateMiner(models.DeviceIdentifier(reconciledIdentifier))
			}
		case existingDevice != nil:
			// Reuse the existing device_identifier to update the same row
			deviceIdentifier = existingDevice.DeviceIdentifier
			preserveMissingFirmware = false
			slog.Debug("reusing existing device identifier for rescan",
				"scanned_ip", scannedIP,
				"scanned_port", scannedPort,
				"device_identifier", deviceIdentifier)
		default:
			// Truly first time seeing this device, generate new identifier.
			deviceIdentifier = id.GenerateID()
			slog.Debug("generated new device identifier for first discovery",
				"scanned_ip", scannedIP,
				"scanned_port", scannedPort,
				"device_identifier", deviceIdentifier)
		}

		// If reconciliation found a match and there was already a discovered_device at the
		// scanned endpoint, resolve the collision before moving the reconciled row there.
		// Unpaired stale rows can be deleted, but paired occupants must win to avoid leaving
		// multiple active discovered_device rows on the same IP:port.
		if reconciledIdentifier != "" && existingDevice != nil && existingDevice.DeviceIdentifier != reconciledIdentifier {
			_, linkedErr := s.deviceStore.GetDeviceByDeviceIdentifier(ctx, existingDevice.DeviceIdentifier, info.OrganizationID)
			switch {
			case linkedErr == nil:
				slog.Warn("skipping reconciliation because scanned endpoint is occupied by a different paired device",
					"scanned_ip", scannedIP,
					"scanned_port", scannedPort,
					"occupying_device_identifier", existingDevice.DeviceIdentifier,
					"reconciled_identifier", reconciledIdentifier)
				return false, nil
			case !fleeterror.IsNotFoundError(linkedErr):
				return false, fleeterror.NewInternalErrorf("failed to check existing device linkage during reconciliation: %v", linkedErr)
			default:
				staleID := discoverymodels.DeviceOrgIdentifier{
					DeviceIdentifier: existingDevice.DeviceIdentifier,
					OrgID:            info.OrganizationID,
				}
				if err := s.discoveredDeviceStore.SoftDelete(ctx, staleID); err != nil {
					slog.Warn("failed to soft-delete stale discovered device after reconciliation",
						"stale_device_identifier", existingDevice.DeviceIdentifier,
						"reconciled_identifier", reconciledIdentifier,
						"error", err)
				}
			}
		}
	}

	orgDeviceID := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            info.OrganizationID,
	}

	// Override the IP/port with the scanned values to ensure consistency
	// The discovered device may report a different IP if it has multiple interfaces
	// or if its configuration has changed, but we want to store it at the IP:port we scanned
	discoveredDevice.IpAddress = scannedIP
	discoveredDevice.Port = scannedPort
	if err := s.hydrateMissingFirmwareVersion(ctx, discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            info.OrganizationID,
	}, discoveredDevice, preserveMissingFirmware); err != nil {
		return false, err
	}

	result, err := s.discoveredDeviceStore.Save(ctx, orgDeviceID, discoveredDevice)
	if err != nil {
		return false, err
	}

	minerCapabilities := s.capabilitiesProvider.GetMinerCapabilitiesForDevice(ctx, &discoveredDevice.Device)
	result.Device.Capabilities = minerCapabilities

	select {
	case resultChan <- &pb.DiscoverResponse{
		Devices: []*pb.Device{&result.Device},
	}:
	case <-ctx.Done():
	}

	return true, nil
}

func (s *Service) hydrateMissingFirmwareVersion(
	ctx context.Context,
	doi discoverymodels.DeviceOrgIdentifier,
	discoveredDevice *discoverymodels.DiscoveredDevice,
	preserveMissingFirmware bool,
) error {
	if !preserveMissingFirmware || discoveredDevice.FirmwareVersion != "" {
		return nil
	}

	existingDevice, err := s.discoveredDeviceStore.GetDevice(ctx, doi)
	switch {
	case fleeterror.IsNotFoundError(err):
		return nil
	case err != nil:
		return fleeterror.NewInternalErrorf("failed to load existing discovered device firmware: %v", err)
	}

	discoveredDevice.FirmwareVersion = existingDevice.FirmwareVersion
	return nil
}

// reconcileByMAC checks if a newly discovered device matches an existing paired device
// by MAC address. This handles the case where a device moved to a new IP/subnet.
// If a match is found, it returns the existing discovered_device_identifier so the
// upsert updates the old record's IP instead of creating a duplicate.
func (s *Service) reconcileByMAC(ctx context.Context, discoveredDevice *discoverymodels.DiscoveredDevice, orgID int64, newIP string, newPort string) (string, error) {
	mac := networking.NormalizeMAC(discoveredDevice.MacAddress)

	pairedDevice, err := s.deviceStore.GetPairedDeviceByMACAddress(ctx, mac, orgID)
	if err != nil {
		// Not found is expected for genuinely new devices
		if !fleeterror.IsNotFoundError(err) {
			return "", fleeterror.NewInternalErrorf("failed to look up paired device by MAC during reconciliation: %v", err)
		}
		return "", nil
	}

	// Cross-check serial number when available to avoid mismatches
	if discoveredDevice.SerialNumber != "" && pairedDevice.SerialNumber != "" &&
		discoveredDevice.SerialNumber != pairedDevice.SerialNumber {
		slog.Warn("MAC address matches but serial number differs, skipping reconciliation",
			"mac_address", mac,
			"discovered_serial", discoveredDevice.SerialNumber,
			"paired_serial", pairedDevice.SerialNumber,
		)
		return "", nil
	}

	slog.Info("reconciled discovered device with existing paired device by MAC address",
		"mac_address", mac,
		"paired_device_identifier", pairedDevice.DeviceIdentifier,
		"discovered_device_identifier", pairedDevice.DiscoveredDeviceIdentifier,
		"new_ip", newIP,
		"new_port", newPort,
	)

	return pairedDevice.DiscoveredDeviceIdentifier, nil
}

// reconcileByIPAcrossDiscoveryPorts reuses an existing discovered device
// identifier when the same driver/model/manufacturer is rediscovered on the
// same IP via a different canonical discovery port. This covers discovery
// drivers that probe primarily by IP and therefore cannot distinguish the same
// physical device across multiple scanned ports.
func (s *Service) reconcileByIPAcrossDiscoveryPorts(ctx context.Context, discoveredDevice *discoverymodels.DiscoveredDevice, orgID int64, scannedIP string, scannedPort string) (string, error) {
	if scannedIP == "" || scannedPort == "" || discoveredDevice.DriverName == "" {
		return "", nil
	}
	if discoveredDevice.MacAddress != "" || discoveredDevice.SerialNumber != "" {
		return "", nil
	}

	for _, port := range s.capabilitiesProvider.GetDiscoveryPorts(ctx) {
		if port == "" || port == scannedPort {
			continue
		}

		existingDevice, err := s.discoveredDeviceStore.GetByIPAndPort(ctx, orgID, scannedIP, port)
		switch {
		case fleeterror.IsNotFoundError(err):
			continue
		case err != nil:
			return "", fleeterror.NewInternalErrorf("failed to query discovered device during same-IP reconciliation: %v", err)
		}

		if existingDevice.DriverName != discoveredDevice.DriverName {
			continue
		}
		if !strings.EqualFold(existingDevice.Model, discoveredDevice.Model) {
			continue
		}
		if !strings.EqualFold(existingDevice.Manufacturer, discoveredDevice.Manufacturer) {
			continue
		}

		slog.Debug("reused discovered device for same-IP cross-port rediscovery",
			"ip_address", scannedIP,
			"driver_name", discoveredDevice.DriverName,
			"existing_port", existingDevice.Port,
			"rediscovered_port", scannedPort,
			"device_identifier", existingDevice.DeviceIdentifier)
		return existingDevice.DeviceIdentifier, nil
	}

	return "", nil
}

func (s *Service) IsSameDevice(ctx context.Context, newDiscoveredDevice *discoverymodels.DiscoveredDevice, pairedDeviceIdentifier string, orgID int64) bool {
	pairedDevice, err := s.deviceStore.GetDeviceByDeviceIdentifier(ctx, pairedDeviceIdentifier, orgID)
	if err != nil {
		slog.Error("failed to get paired device", "error", err)
		return false
	}

	pairer := s.pairer

	pairedDeviceCredentials, err := s.deviceStore.GetMinerCredentials(ctx, pairedDevice, orgID)
	if err != nil {
		// log and continue without credentials
		slog.Debug("failed to get paired device credentials", "error", err)
	}

	newDiscoveredDeviceInfo, err := pairer.GetDeviceInfo(ctx, newDiscoveredDevice, pairedDeviceCredentials)
	if err != nil {
		// Check if this is an authentication error and update pairing status
		if fleeterror.IsAuthenticationError(err) {
			slog.Info("authentication failed for paired device, updating pairing status",
				"device_identifier", pairedDevice.DeviceIdentifier)
			// No options-cache invalidation needed: PAIRED → AUTH_NEEDED
			// keeps the device in the dropdown source set
			// (GetAvailableModels / GetAvailableFirmwareVersions scan
			// PAIRED ∪ AUTH_NEEDED).
			if _, updateErr := s.deviceStore.UpdateDevicePairingStatusByIdentifier(ctx, pairedDevice.DeviceIdentifier, StatusAuthenticationNeeded); updateErr != nil {
				slog.Error("failed to update pairing status to AUTHENTICATION_NEEDED",
					"device_identifier", pairedDevice.DeviceIdentifier, "error", updateErr)
			}
		}
		slog.Debug("failed to get new discovered device info", "error", err)
		return false
	}

	return networking.NormalizeMAC(newDiscoveredDeviceInfo.MacAddress) == networking.NormalizeMAC(pairedDevice.MacAddress) &&
		newDiscoveredDeviceInfo.SerialNumber == pairedDevice.SerialNumber
}

// resolveDeviceIdentifiers resolves a DeviceSelector to a list of device identifiers.
// This follows the same pattern as the command service's getDeviceIDs method.
func (s *Service) resolveDeviceIdentifiers(ctx context.Context, selector *commandpb.DeviceSelector, orgID int64) ([]string, error) {
	if selector == nil {
		return nil, fleeterror.NewInvalidArgumentError("device_selector is required")
	}

	switch x := selector.SelectionType.(type) {
	case *commandpb.DeviceSelector_AllDevices:
		filter := x.AllDevices
		minerFilter := &interfaces.MinerFilter{}

		if filter != nil && len(filter.PairingStatus) > 0 {
			minerFilter.PairingStatuses = filter.PairingStatus
		}

		return s.deviceStore.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID, minerFilter)

	case *commandpb.DeviceSelector_IncludeDevices:
		if x.IncludeDevices == nil || len(x.IncludeDevices.DeviceIdentifiers) == 0 {
			return nil, fleeterror.NewInvalidArgumentError("include_devices selector requires at least one device identifier")
		}
		return x.IncludeDevices.DeviceIdentifiers, nil

	default:
		return nil, fleeterror.NewInvalidArgumentErrorf("unknown device selector type: %T", x)
	}
}

func (s *Service) PairDevices(ctx context.Context, r *pb.PairRequest) (*pb.PairResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	// Resolve device selector to identifiers
	deviceIdentifiers, err := s.resolveDeviceIdentifiers(ctx, r.DeviceSelector, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	if len(deviceIdentifiers) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("no devices match the selector")
	}

	successfulIDs := make([]models.DeviceIdentifier, 0, len(deviceIdentifiers))
	telemetryDeviceIDs := make([]models.DeviceIdentifier, 0, len(deviceIdentifiers))
	failedIDs := make([]string, 0, len(deviceIdentifiers))

	credentials := r.Credentials

	// Deduplicate to prevent concurrent pairDevice calls against the same physical device.
	// We check both exact identifier strings and IP+port because different identifiers can
	// alias to the same physical device (e.g., duplicate discovered_device rows for the same
	// network endpoint). Concurrent pairing of aliases races key rotation, credential upserts,
	// and pairing-state updates.
	seenIDs := make(map[string]struct{}, len(deviceIdentifiers))
	seenEndpoints := make(map[string]struct{}, len(deviceIdentifiers))
	uniqueIDs := deviceIdentifiers[:0:0]
	for _, id := range deviceIdentifiers {
		if _, ok := seenIDs[id]; ok {
			continue
		}
		seenIDs[id] = struct{}{}

		dd, ddErr := s.discoveredDeviceStore.GetDevice(ctx, discoverymodels.DeviceOrgIdentifier{
			DeviceIdentifier: id,
			OrgID:            info.OrganizationID,
		})
		if ddErr == nil {
			endpoint := dd.IpAddress + ":" + dd.Port
			if _, ok := seenEndpoints[endpoint]; ok {
				slog.Warn("skipping duplicate physical device (same IP:port) in PairDevices request",
					"device_identifier", id,
					"endpoint", endpoint,
				)
				failedIDs = append(failedIDs, id)
				continue
			}
			seenEndpoints[endpoint] = struct{}{}
		}
		uniqueIDs = append(uniqueIDs, id)
	}
	deviceIdentifiers = uniqueIDs

	// Pair devices concurrently — each device involves a plugin RPC + DB writes.
	// Semaphore mirrors the discovery pattern to cap simultaneous RPCs/DB connections.
	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, concurrentDiscoveryLimit)

	for _, deviceID := range deviceIdentifiers {
		select {
		case <-ctx.Done():
			slog.Debug("Pairing context canceled, stopping device pairing")
		case semaphore <- struct{}{}:
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				defer func() { <-semaphore }()
				defer func() {
					if r := recover(); r != nil {
						slog.Error("panic in pairing worker", "device_identifier", id, "panic", r)
						mu.Lock()
						failedIDs = append(failedIDs, id)
						mu.Unlock()
					}
				}()

				deviceCtx, cancel := context.WithTimeout(ctx, perDevicePairingTimeout)
				defer cancel()

				persistedDeviceID, err := s.pairDevice(deviceCtx, id, info.OrganizationID, credentials)
				if err != nil {
					slog.Error("failed to pair device", "error", err)
					mu.Lock()
					failedIDs = append(failedIDs, id)
					mu.Unlock()
					return
				}

				// Evict stale cached handle; pairing wrote new credentials/IP to DB.
				if s.invalidateMiner != nil {
					s.invalidateMiner(persistedDeviceID)
				}

				// shouldScheduleTelemetryForDevice may hit the DB — call outside the lock.
				// Use parent ctx: deviceCtx budget is spent on pairDevice and may be expired.
				shouldSchedule, scheduleErr := s.shouldScheduleTelemetryForDevice(ctx, persistedDeviceID, info.OrganizationID)

				mu.Lock()
				defer mu.Unlock()
				successfulIDs = append(successfulIDs, persistedDeviceID)
				if scheduleErr != nil {
					slog.Warn("failed to determine whether paired device should be scheduled for telemetry",
						"device_identifier", persistedDeviceID,
						"error", scheduleErr)
					return
				}
				if shouldSchedule {
					telemetryDeviceIDs = append(telemetryDeviceIDs, persistedDeviceID)
				}
			}(deviceID)
		}
		if ctx.Err() != nil {
			break
		}
	}
	wg.Wait()

	// Partial success is valid
	if len(successfulIDs) == 0 {
		if ctxErr := ctx.Err(); ctxErr != nil {
			if errors.Is(ctxErr, context.DeadlineExceeded) {
				return nil, fleeterror.NewPlainError("pairing deadline exceeded", connect.CodeDeadlineExceeded).WithCallerStackTrace()
			}
			return nil, fleeterror.NewCanceledError()
		}
		return nil, fleeterror.NewInternalError("Failed to pair any devices")
	}

	if len(telemetryDeviceIDs) > 0 {
		if err := s.listener.AddDevices(ctx, telemetryDeviceIDs...); err != nil {
			slog.Error("failed to add devices to telemetry scheduler", "error", err)
			return nil, fleeterror.NewInternalErrorf("failed to add devices to telemetry scheduler: %v", err)
		}
	}

	return &pb.PairResponse{
		FailedDeviceIds: failedIDs,
	}, nil
}

func (s *Service) shouldScheduleTelemetryForDevice(ctx context.Context, deviceID models.DeviceIdentifier, orgID int64) (bool, error) {
	statusProvider, ok := s.deviceStore.(devicePairingStatusProvider)
	if !ok {
		return true, nil
	}

	pairingStatus, err := statusProvider.GetDevicePairingStatusByIdentifier(ctx, string(deviceID), orgID)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			// Some tests use mocked pairers that don't persist the device row. In that case
			// preserve the pre-existing behavior and allow telemetry scheduling.
			return true, nil
		}
		return false, err
	}

	if pairingStatus != StatusPaired {
		slog.Info("skipping telemetry scheduling for device that is not fully paired",
			"device_identifier", deviceID,
			"pairing_status", pairingStatus)
		return false, nil
	}

	return true, nil
}

// isCredentialsRequiredError checks if an error indicates that credentials are required but not provided
func isCredentialsRequiredError(err error) bool {
	if err == nil {
		return false
	}

	var fleetErr fleeterror.FleetError
	if errors.As(err, &fleetErr) {
		// Check for invalid_argument error with "credentials are required" message
		return fleetErr.GRPCCode == connect.CodeInvalidArgument &&
			strings.Contains(strings.ToLower(fleetErr.DebugMessage), "credentials are required")
	}

	// Also check the error message string directly for wrapped errors
	return strings.Contains(strings.ToLower(err.Error()), "credentials are required") &&
		strings.Contains(strings.ToLower(err.Error()), "invalid_argument")
}

// isAlreadyPairedKeyRotationError checks for plugin errors emitted when a device is
// already paired and the caller did not provide credentials needed to rotate keys.
// Treating this as an idempotent no-op prevents rediscovered paired devices from
// causing the entire batch Pair request to fail.
func isAlreadyPairedKeyRotationError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "already paired") &&
		strings.Contains(message, "key rotation") &&
		strings.Contains(message, "valid credentials")
}

// handleAuthenticationRequiredPairing inserts a device and creates a pairing record with AUTHENTICATION_NEEDED status
func (s *Service) handleAuthenticationRequiredPairing(ctx context.Context, discoveredDevice *discoverymodels.DiscoveredDevice) error {
	originalIdentifier := discoveredDevice.DeviceIdentifier
	if err := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		// Restore original identifier so retries after serialization failures start with clean state.
		discoveredDevice.DeviceIdentifier = originalIdentifier

		reconciledIdentifier, err := s.reconcileByMAC(ctx, discoveredDevice, discoveredDevice.OrgID, discoveredDevice.IpAddress, discoveredDevice.Port)
		if err != nil {
			return err
		}
		if reconciledIdentifier != "" {
			discoveredDevice.DeviceIdentifier = reconciledIdentifier
		}

		// Check if device already exists (e.g., from a previous discovery attempt)
		existingDevice, err := s.deviceStore.GetDeviceByDeviceIdentifier(ctx, discoveredDevice.DeviceIdentifier, discoveredDevice.OrgID)
		if err != nil && !fleeterror.IsNotFoundError(err) {
			return fleeterror.NewInternalErrorf("failed to check if device exists: %v", err)
		}

		if existingDevice == nil {
			// Device doesn't exist yet, insert it
			if err := s.deviceStore.InsertDevice(ctx, &discoveredDevice.Device, discoveredDevice.OrgID, discoveredDevice.DeviceIdentifier); err != nil {
				return fleeterror.NewInternalErrorf("failed to insert device: %v", err)
			}
		} else {
			// Device already exists, update MAC address and serial number
			if err := s.deviceStore.UpdateDeviceInfo(ctx, &discoveredDevice.Device, discoveredDevice.OrgID); err != nil {
				return fleeterror.NewInternalErrorf("failed to update device info: %v", err)
			}
		}

		workerName := networking.NormalizeMAC(discoveredDevice.MacAddress)
		shouldUpdateWorkerName := workerName != ""
		if shouldUpdateWorkerName && existingDevice != nil {
			keepExistingWorkerName, err := workername.HasStored(ctx, s.deviceStore, discoveredDevice.OrgID, discoveredDevice.DeviceIdentifier)
			if err != nil {
				return err
			}
			shouldUpdateWorkerName = !keepExistingWorkerName
		}
		if shouldUpdateWorkerName {
			if err := s.deviceStore.UpdateWorkerName(ctx, models.DeviceIdentifier(discoveredDevice.DeviceIdentifier), workerName); err != nil {
				return fleeterror.NewInternalErrorf("failed to update worker name: %v", err)
			}
		}

		// Create pairing record with AUTHENTICATION_NEEDED status
		if err := s.deviceStore.UpsertDevicePairing(ctx, &discoveredDevice.Device, discoveredDevice.OrgID, StatusAuthenticationNeeded); err != nil {
			return fleeterror.NewInternalErrorf("failed to upsert device pairing: %v", err)
		}

		return nil
	}); err != nil {
		return err
	}

	// The device is now AUTHENTICATION_NEEDED, which is part of the
	// dropdown source set (GetAvailableModels / GetAvailableFirmwareVersions
	// scan PAIRED ∪ AUTH_NEEDED). Evict so the new device's model and
	// firmware show up immediately rather than after TTL expiry.
	s.optionsCache.Invalidate(discoveredDevice.OrgID)
	return nil
}

func (s *Service) pairDevice(ctx context.Context, deviceID string, orgID int64, credentials *pb.Credentials) (models.DeviceIdentifier, error) {
	orgDeviceID := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: deviceID,
		OrgID:            orgID,
	}

	existingDevice, err := s.deviceStore.GetDeviceByDeviceIdentifier(ctx, deviceID, orgID)
	if err != nil && !fleeterror.IsNotFoundError(err) {
		return "", fleeterror.NewInternalErrorf("error getting existing device from store: %v", err)
	}
	knownPairedDevice := false
	if existingDevice != nil {
		statusProvider, ok := s.deviceStore.(devicePairingStatusProvider)
		if ok {
			pairingStatus, statusErr := statusProvider.GetDevicePairingStatusByIdentifier(ctx, deviceID, orgID)
			if statusErr != nil && !fleeterror.IsNotFoundError(statusErr) {
				return "", fleeterror.NewInternalErrorf("error getting existing device pairing status: %v", statusErr)
			}
			knownPairedDevice = pairingStatus == StatusPaired || pairingStatus == StatusAuthenticationNeeded
		} else {
			knownPairedDevice = true
		}
	}

	discoveredDevice, err := s.discoveredDeviceStore.GetDevice(ctx, orgDeviceID)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error getting device from store: %v", err)
	}

	pairer := s.pairer

	discoveredDevice.IsActive = true
	_, err = s.discoveredDeviceStore.Save(ctx, orgDeviceID, discoveredDevice)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error activating discovered device: %v", err)
	}

	if err := pairer.PairDevice(ctx, discoveredDevice, credentials); err != nil {
		// Check if this is a "credentials required" error (not wrong credentials, but missing credentials)
		if isCredentialsRequiredError(err) {
			// Device needs authentication but no credentials were provided
			// Insert device and mark as AUTHENTICATION_NEEDED so it shows up in the UI
			slog.Info("device requires authentication, marking as AUTHENTICATION_NEEDED",
				"device_identifier", discoveredDevice.DeviceIdentifier,
				"driver_name", discoveredDevice.DriverName)

			if txErr := s.handleAuthenticationRequiredPairing(ctx, discoveredDevice); txErr != nil {
				slog.Error("failed to create AUTHENTICATION_NEEDED pairing record",
					"device_identifier", discoveredDevice.DeviceIdentifier,
					"error", txErr)
				return "", txErr
			}

			return models.DeviceIdentifier(discoveredDevice.DeviceIdentifier), nil
		}

		// Discovery can legitimately return devices that are already paired.
		// If the caller did not provide credentials, treat explicit key-rotation
		// auth failures on known devices as a no-op so Pair remains idempotent.
		if credentials == nil && knownPairedDevice && isAlreadyPairedKeyRotationError(err) {
			slog.Info("pair request targeted an already paired device without rotation credentials; treating as already paired",
				"device_identifier", discoveredDevice.DeviceIdentifier,
				"driver_name", discoveredDevice.DriverName)
			return models.DeviceIdentifier(discoveredDevice.DeviceIdentifier), nil
		}
		if credentials == nil && isAlreadyPairedKeyRotationError(err) {
			slog.Info("device is already paired externally, marking as AUTHENTICATION_NEEDED",
				"device_identifier", discoveredDevice.DeviceIdentifier,
				"driver_name", discoveredDevice.DriverName)

			if txErr := s.handleAuthenticationRequiredPairing(ctx, discoveredDevice); txErr != nil {
				slog.Error("failed to create AUTHENTICATION_NEEDED pairing record for externally paired device",
					"device_identifier", discoveredDevice.DeviceIdentifier,
					"error", txErr)
				return "", txErr
			}

			return models.DeviceIdentifier(discoveredDevice.DeviceIdentifier), nil
		}

		// Preserve authentication errors - don't wrap them
		if fleeterror.IsAuthenticationError(err) || fleeterror.IsForbiddenError(err) {
			return "", err
		}
		return "", fleeterror.NewInternalErrorf("pairing device %s: %v", discoveredDevice.DeviceIdentifier, err)
	}

	// Only fetch device info when PairDevice didn't return a firmware version.
	if discoveredDevice.FirmwareVersion == "" {
		updatedDeviceInfo, infoErr := pairer.GetDeviceInfo(ctx, discoveredDevice, credentials)
		if infoErr != nil {
			slog.Warn("failed to get device info after pairing, continuing without firmware version",
				"device_identifier", discoveredDevice.DeviceIdentifier,
				"error", infoErr)
		}
		if updatedDeviceInfo != nil {
			discoveredDevice.FirmwareVersion = updatedDeviceInfo.FirmwareVersion
		}
	}

	// Save updated device info (firmware version, serial number, MAC address) back to discovered_device table
	finalDeviceID := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: discoveredDevice.DeviceIdentifier,
		OrgID:            orgID,
	}
	_, err = s.discoveredDeviceStore.Save(ctx, finalDeviceID, discoveredDevice)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error saving updated device info after pairing: %v", err)
	}

	s.optionsCache.Invalidate(orgID)

	return models.DeviceIdentifier(discoveredDevice.DeviceIdentifier), nil
}
