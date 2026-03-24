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
	capabilitiespb "github.com/proto-at-block/proto-fleet/server/generated/grpc/capabilities/v1"
	commandpb "github.com/proto-at-block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/miner/models"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/minerdiscovery"
	discoverymodels "github.com/proto-at-block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/session"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces"
	tmodels "github.com/proto-at-block/proto-fleet/server/internal/domain/telemetry/models"
	tokenDomain "github.com/proto-at-block/proto-fleet/server/internal/domain/token"

	pb "github.com/proto-at-block/proto-fleet/server/generated/grpc/pairing/v1"
	id "github.com/proto-at-block/proto-fleet/server/internal/infrastructure/id"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/networking"

	"github.com/Ullaakut/nmap/v3"
	"github.com/grandcat/zeroconf"
)

const (
	// concurrentDiscoveryLimit limits the number of concurrent device discovery operations.
	// 100 provides good parallelism for large fleet sizes while avoiding overwhelming the network.
	concurrentDiscoveryLimit = 100

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
// Network and gateway addresses should be skipped during discovery to avoid false positives
// where all devices appear to respond at the gateway IP.
func shouldSkipNetworkOrGatewayAddress(ipv4 net.IP) bool {
	if ipv4 == nil || len(ipv4) != 4 {
		return false
	}

	// Check if this is localhost (127.x.x.x)
	isLocalhost := ipv4[0] == localhostFirstOctet
	if isLocalhost {
		return false // Don't skip localhost addresses
	}

	// Check last octet for network (.0) or gateway (.1) addresses
	lastOctet := ipv4[3]
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
	}
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

func (s *Service) GetLocalNetworkInfo(_ context.Context) (*NetworkInfo, error) {
	info, err := networking.GetLocalNetworkInfo()
	if err != nil {
		return nil, err
	}
	return &NetworkInfo{info}, nil
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
	rawResultChan := make(chan *pb.DiscoverResponse)
	resultChan := dedupeDiscoverResponses(rawResultChan)

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to initialize resolver: %v", err)
	}

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

				if len(entry.AddrIPv4) == 0 {
					continue
				}

				ipAddress := entry.AddrIPv4[0].String()
				portStr := fmt.Sprintf("%d", entry.Port)

				err := s.discoverDevice(ctx, ipAddress, portStr, rawResultChan)
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
	rawResultChan := make(chan *pb.DiscoverResponse)
	resultChan := dedupeDiscoverResponses(rawResultChan)
	ports, err := s.resolveDiscoveryPorts(ctx, r.Ports)
	if err != nil {
		return nil, err
	}

	// Apply server-controlled timeout for the entire discovery operation
	timeoutCtx, cancel := context.WithTimeout(ctx, defaultNmapTimeoutSeconds*time.Second)

	go func() {
		defer cancel()
		defer close(rawResultChan)

		var scanner *nmap.Scanner
		var err error

		// Common nmap options for faster scanning
		nmapOpts := []nmap.Option{
			nmap.WithTargets(r.Target),
			nmap.WithDisabledDNSResolution(),
			nmap.WithTimingTemplate(nmap.TimingAggressive), // -T4 for faster scanning
			nmap.WithMaxRetries(nmapMaxRetriesPerHost),
			nmap.WithHostTimeout(time.Duration(nmapHostTimeoutMilliseconds) * time.Millisecond),
			nmap.WithMinRTTTimeout(time.Duration(nmapMinRTTTimeoutMilliseconds) * time.Millisecond),
		}

		nmapOpts = append(nmapOpts, nmap.WithPorts(strings.Join(ports, ",")))

		scanner, err = nmap.NewScanner(timeoutCtx, nmapOpts...)
		if err != nil {
			rawResultChan <- &pb.DiscoverResponse{
				Error: fmt.Sprintf("failed to create scanner: %v", err),
			}
			return
		}

		slog.Debug("Starting nmap scan",
			"target", r.Target,
			"ports", ports,
			"timeout_seconds", defaultNmapTimeoutSeconds)

		result, _, err := scanner.Run()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				slog.Info("Nmap scan timed out",
					"target", r.Target,
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
				if addr.AddrType == "ipv4" {
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

					err := s.discoverDevice(timeoutCtx, ip, port, rawResultChan)
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

// DiscoverWithIPRange discovers devices using IP range
func (s *Service) DiscoverWithIPRange(ctx context.Context, r *pb.IPRangeModeRequest) (<-chan *pb.DiscoverResponse, error) {
	rawResultChan := make(chan *pb.DiscoverResponse)
	resultChan := dedupeDiscoverResponses(rawResultChan)
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

					for _, port := range ports {
						if timeoutCtx.Err() != nil {
							return
						}
						err := s.discoverDevice(timeoutCtx, ipAddr, port, rawResultChan)
						if err != nil {
							slog.Debug("device discovery failed", "ip", ipAddr, "port", port, "error", err)
						}
					}
				}(uint32ToIP(ip))
			}
		}

		wg.Wait()
	}()

	return resultChan, nil
}

// DiscoverWithIPList discovers devices from a list of IPs
func (s *Service) DiscoverWithIPList(ctx context.Context, r *pb.IPListModeRequest) (<-chan *pb.DiscoverResponse, error) {
	rawResultChan := make(chan *pb.DiscoverResponse)
	resultChan := dedupeDiscoverResponses(rawResultChan)
	ports, err := s.resolveDiscoveryPorts(ctx, r.Ports)
	if err != nil {
		return nil, err
	}

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

					for _, port := range ports {
						if timeoutCtx.Err() != nil {
							return
						}
						err := s.discoverDevice(timeoutCtx, ipAddr, port, rawResultChan)
						if err != nil {
							slog.Debug("device discovery failed", "ip", ipAddr, "port", port, "error", err)
						}
					}
				}(ip)
			}
		}

		wg.Wait()
	}()

	return resultChan, nil
}

func (s *Service) discoverDevice(ctx context.Context, ipAddress string, port string, resultChan chan<- *pb.DiscoverResponse) error {
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

		return err
	}

	return s.processDiscoveredDevice(ctx, discoveredDevice, ipAddress, port, resultChan)
}

func (s *Service) processDiscoveredDevice(ctx context.Context, discoveredDevice *discoverymodels.DiscoveredDevice, scannedIP string, scannedPort string, resultChan chan<- *pb.DiscoverResponse) error {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return err
	}

	// Use existing device identifier if available
	deviceIdentifier := discoveredDevice.DeviceIdentifier
	preserveMissingFirmware := deviceIdentifier != ""
	if deviceIdentifier == "" {
		reconciledIdentifier, err := s.reconcileByMAC(ctx, discoveredDevice, info.OrganizationID, scannedIP, scannedPort)
		if err != nil {
			return err
		}
		reconciledByMAC := reconciledIdentifier != ""

		// Check if we've already discovered a device at this scanned IP:port
		// This prevents duplicate entries during network rescans
		// Note: We use scannedIP/scannedPort (what we scanned) not discoveredDevice IP/port
		// (which may have changed if device moved), to maintain stable identifiers per network endpoint
		existingDevice, err := s.discoveredDeviceStore.GetByIPAndPort(ctx, info.OrganizationID, scannedIP, scannedPort)
		if err != nil && !fleeterror.IsNotFoundError(err) {
			// Database error - propagate instead of silently generating new identifier
			return fleeterror.NewInternalErrorf("failed to check for existing device: %v", err)
		}
		if reconciledIdentifier == "" && existingDevice == nil {
			reconciledIdentifier, err = s.reconcileByIPAcrossDiscoveryPorts(ctx, discoveredDevice, info.OrganizationID, scannedIP, scannedPort)
			if err != nil {
				return err
			}
		}
		switch {
		case reconciledIdentifier != "":
			deviceIdentifier = reconciledIdentifier
			preserveMissingFirmware = reconciledByMAC
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
				return nil
			case !fleeterror.IsNotFoundError(linkedErr):
				return fleeterror.NewInternalErrorf("failed to check existing device linkage during reconciliation: %v", linkedErr)
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
		return err
	}

	result, err := s.discoveredDeviceStore.Save(ctx, orgDeviceID, discoveredDevice)
	if err != nil {
		return err
	}

	minerCapabilities := s.capabilitiesProvider.GetMinerCapabilitiesForDevice(ctx, &discoveredDevice.Device)
	result.Device.Capabilities = minerCapabilities

	select {
	case resultChan <- &pb.DiscoverResponse{
		Devices: []*pb.Device{&result.Device},
	}:
	case <-ctx.Done():
	}

	return nil
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
			if updateErr := s.deviceStore.UpdateDevicePairingStatusByIdentifier(ctx, pairedDevice.DeviceIdentifier, StatusAuthenticationNeeded); updateErr != nil {
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

	// Create pairing records for each device
	for _, deviceID := range deviceIdentifiers {
		persistedDeviceID, err := s.pairDevice(ctx, deviceID, info.OrganizationID, credentials)
		if err == nil {
			successfulIDs = append(successfulIDs, persistedDeviceID)
			shouldSchedule, scheduleErr := s.shouldScheduleTelemetryForDevice(ctx, persistedDeviceID, info.OrganizationID)
			if scheduleErr != nil {
				slog.Warn("failed to determine whether paired device should be scheduled for telemetry",
					"device_identifier", persistedDeviceID,
					"error", scheduleErr)
				continue
			}
			if shouldSchedule {
				telemetryDeviceIDs = append(telemetryDeviceIDs, persistedDeviceID)
			}
		} else {
			slog.Error("failed to pair device", "error", err) // continue pairing other devices
			failedIDs = append(failedIDs, deviceID)
		}
	}

	// Partial success is valid
	if len(successfulIDs) == 0 {
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
	return s.transactor.RunInTx(ctx, func(ctx context.Context) error {
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

		// Create pairing record with AUTHENTICATION_NEEDED status
		if err := s.deviceStore.UpsertDevicePairing(ctx, &discoveredDevice.Device, discoveredDevice.OrgID, StatusAuthenticationNeeded); err != nil {
			return fleeterror.NewInternalErrorf("failed to upsert device pairing: %v", err)
		}

		return nil
	})
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
		if fleeterror.IsAuthenticationError(err) {
			return "", err
		}
		return "", fleeterror.NewInternalErrorf("pairing device %s: %v", discoveredDevice.DeviceIdentifier, err)
	}

	// Get device info with authentication to fetch firmware version and other details
	updatedDeviceInfo, err := pairer.GetDeviceInfo(ctx, discoveredDevice, credentials)
	if err != nil {
		slog.Warn("failed to get device info after pairing, continuing without firmware version",
			"device_identifier", discoveredDevice.DeviceIdentifier,
			"error", err)
	} else {
		// Update firmware version from authenticated device info
		discoveredDevice.FirmwareVersion = updatedDeviceInfo.FirmwareVersion
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

	return models.DeviceIdentifier(discoveredDevice.DeviceIdentifier), nil
}
