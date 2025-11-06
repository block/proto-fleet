package pairing

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/capabilities"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	tmodels "github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"

	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	id "github.com/btc-mining/proto-fleet/server/internal/infrastructure/id"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"

	"github.com/Ullaakut/nmap/v3"
	"github.com/grandcat/zeroconf"
)

//go:generate mockgen -source=service.go -destination=mocks/mock_service.go -package=mocks Listener
type Listener interface {
	AddDevices(ctx context.Context, deviceID ...tmodels.DeviceIdentifier) error
}

// Service handles the core device discovery functionality
type Service struct {
	discoveredDeviceStore interfaces.DiscoveredDeviceStore
	deviceStore           interfaces.DeviceStore
	transactor            interfaces.Transactor
	tokenService          *tokenDomain.Service
	minerDiscoveryService *minerdiscovery.Service
	capabilitiesService   *capabilities.Service
	pairers               map[models.Type]Pairer
	listener              Listener
}

func NewService(
	discoveredDeviceStore interfaces.DiscoveredDeviceStore,
	deviceStore interfaces.DeviceStore,
	transactor interfaces.Transactor,
	tokenService *tokenDomain.Service,
	minerDiscoveryService *minerdiscovery.Service,
	capabilitiesService *capabilities.Service,
	listener Listener,
	pairers ...Pairer,
) *Service {
	pairersMap := make(map[models.Type]Pairer)
	for _, pairer := range pairers {
		pairersMap[pairer.GetMinerType()] = pairer
	}

	return &Service{
		discoveredDeviceStore: discoveredDeviceStore,
		deviceStore:           deviceStore,
		transactor:            transactor,
		tokenService:          tokenService,
		minerDiscoveryService: minerDiscoveryService,
		capabilitiesService:   capabilitiesService,
		pairers:               pairersMap,
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

// DiscoverWithMDNS discovers devices using mDNS
func (s *Service) DiscoverWithMDNS(ctx context.Context, r *pb.MDNSModeRequest) (<-chan *pb.DiscoverResponse, error) {
	resultChan := make(chan *pb.DiscoverResponse)

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to initialize resolver: %v", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(r.TimeoutSeconds)*time.Second)

	go func() {
		defer cancel()
		defer close(resultChan)

		err := resolver.Browse(timeoutCtx, r.ServiceType, "local.", entries)
		if err != nil {
			resultChan <- &pb.DiscoverResponse{
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

				err := s.discoverDevice(ctx, ipAddress, portStr, resultChan)
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
	resultChan := make(chan *pb.DiscoverResponse)

	go func() {
		defer close(resultChan)

		var scanner *nmap.Scanner
		var err error
		if len(r.Ports) == 0 && r.FastScan {
			scanner, err = nmap.NewScanner(
				ctx,
				nmap.WithTargets(r.Target),
				nmap.WithFastMode(),
				nmap.WithDisabledDNSResolution(),
			)
		} else {
			scanner, err = nmap.NewScanner(
				ctx,
				nmap.WithTargets(r.Target),
				nmap.WithPorts(strings.Join(r.Ports, ",")),
				nmap.WithDisabledDNSResolution(),
			)
		}

		if err != nil {
			resultChan <- &pb.DiscoverResponse{
				Error: fmt.Sprintf("failed to create scanner: %v", err),
			}
			return
		}

		result, _, err := scanner.Run()
		if err != nil {
			resultChan <- &pb.DiscoverResponse{
				Error: fmt.Sprintf("scan failed: %v", err),
			}
			return
		}

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

			for _, port := range host.Ports {
				portStr := fmt.Sprintf("%d", port.ID)
				err := s.discoverDevice(ctx, ipAddress, portStr, resultChan)
				if err != nil {
					slog.Debug("device discovery failed", "error", err)
				}
			}
		}
	}()

	return resultChan, nil
}

// DiscoverWithIPRange discovers devices using IP range
func (s *Service) DiscoverWithIPRange(ctx context.Context, r *pb.IPRangeModeRequest) (<-chan *pb.DiscoverResponse, error) {
	resultChan := make(chan *pb.DiscoverResponse)
	startIP, err := ipToUint32(r.StartIp)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("error parsing start ip: %v", err)
	}
	endIP, err := ipToUint32(r.EndIp)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("error parsing end ip: %v", err)
	}

	go func() {
		defer close(resultChan)

		var wg sync.WaitGroup
		semaphore := make(chan struct{}, 10) // Limit concurrent goroutines

		for ip := startIP; ip <= endIP; ip++ {
			select {
			case <-ctx.Done():
				return
			default:
				wg.Add(1)
				semaphore <- struct{}{} // Acquire semaphore

				go func(ipAddr string) {
					defer wg.Done()
					defer func() { <-semaphore }() // Release semaphore

					for _, port := range r.Ports {
						err := s.discoverDevice(ctx, ipAddr, port, resultChan)
						if err != nil {
							slog.Debug("device discovery failed", "error", err)
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
	resultChan := make(chan *pb.DiscoverResponse)

	go func() {
		defer close(resultChan)

		var wg sync.WaitGroup
		semaphore := make(chan struct{}, 10) // Limit concurrent goroutines

		for _, ip := range r.IpAddresses {
			select {
			case <-ctx.Done():
				return
			default:
				wg.Add(1)
				semaphore <- struct{}{} // Acquire semaphore

				go func(ipAddr string) {
					defer wg.Done()
					defer func() { <-semaphore }() // Release semaphore

					for _, port := range r.Ports {
						err := s.discoverDevice(ctx, ipAddr, port, resultChan)
						if err != nil {
							slog.Debug("device discovery failed", "error", err)
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
	discoveredDevice, err := s.minerDiscoveryService.Discover(ctx, ipAddress, port)
	if err != nil {
		slog.Debug("Discovery failed",
			"ipAddress", ipAddress,
			"port", port,
			"error", err)

		return err
	}

	return s.processDiscoveredDevice(ctx, discoveredDevice, resultChan)
}

func (s *Service) processDiscoveredDevice(ctx context.Context, discoveredDevice *discoverymodels.DiscoveredDevice, resultChan chan<- *pb.DiscoverResponse) error {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return err
	}

	// Use existing device identifier if available, otherwise generate new id
	deviceIdentifier := discoveredDevice.DeviceIdentifier
	if deviceIdentifier == "" {
		deviceIdentifier = id.GenerateID()
	}

	orgDeviceID := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            claims.OrgID,
	}

	result, err := s.discoveredDeviceStore.Save(ctx, orgDeviceID, discoveredDevice)
	if err != nil {
		return err
	}

	capabilities := s.capabilitiesService.GetCapabilitiesForDevice(ctx, &discoveredDevice.Device)
	result.Device.Capabilities = capabilities

	select {
	case resultChan <- &pb.DiscoverResponse{
		Devices: []*pb.Device{&result.Device},
	}:
	case <-ctx.Done():
	}

	return nil
}

func (s *Service) IsSameDevice(ctx context.Context, newDiscoveredDevice *discoverymodels.DiscoveredDevice, pairedDeviceIdentifier string, orgID int64) bool {
	pairedDevice, err := s.deviceStore.GetDeviceByDeviceIdentifier(ctx, pairedDeviceIdentifier, orgID)
	if err != nil {
		slog.Error("failed to get paired device", "error", err)
		return false
	}

	deviceType, err := models.TypeFromString(pairedDevice.Type)
	if err != nil {
		slog.Error("failed to get paired device type", "error", err)
		return false
	}

	pairer, ok := s.pairers[deviceType]
	if !ok {
		slog.Error("failed to get pairer", "device_type", deviceType)
		return false
	}

	pairedDeviceCredentials, err := s.deviceStore.GetMinerCredentials(ctx, pairedDevice, orgID)
	if err != nil {
		// log and continue without credentials
		slog.Debug("failed to get paired device credentials", "error", err)
	}

	newDiscoveredDeviceInfo, err := pairer.GetDeviceInfo(ctx, newDiscoveredDevice, pairedDeviceCredentials)
	if err != nil {
		slog.Debug("failed to get new discovered device info", "error", err)
		return false
	}

	return networking.NormalizeMAC(newDiscoveredDeviceInfo.MacAddress) == networking.NormalizeMAC(pairedDevice.MacAddress) &&
		newDiscoveredDeviceInfo.SerialNumber == pairedDevice.SerialNumber
}

func (s *Service) PairDevices(ctx context.Context, r *pb.PairRequest) (*pb.PairResponse, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	deviceIDs := make([]models.DeviceIdentifier, 0, len(r.DeviceIdentifiers))
	failedIDs := make([]string, 0, len(r.DeviceIdentifiers))

	// Create pairing records for each device
	for _, deviceID := range r.DeviceIdentifiers {
		err = s.pairDevice(ctx, deviceID, claims.OrgID, r.Credentials)
		if err == nil {
			deviceIDs = append(deviceIDs, models.DeviceIdentifier(deviceID))
		} else {
			slog.Error("failed to pair device", "error", err) // continue pairing other devices
			failedIDs = append(failedIDs, deviceID)
		}
	}

	// Partial success is valid
	if len(deviceIDs) == 0 {
		return nil, fleeterror.NewInternalError("Failed to pair any devices")
	}

	if err := s.listener.AddDevices(ctx, deviceIDs...); err != nil {
		slog.Error("failed to add devices to telemetry scheduler", "error", err)
		return nil, fleeterror.NewInternalErrorf("failed to add devices to telemetry scheduler: %v", err)
	}

	return &pb.PairResponse{
		FailedDeviceIds: failedIDs,
	}, nil
}

func (s *Service) pairDevice(ctx context.Context, deviceID string, orgID int64, credentials *pb.Credentials) error {
	orgDeviceID := discoverymodels.DeviceOrgIdentifier{
		DeviceIdentifier: deviceID,
		OrgID:            orgID,
	}

	discoveredDevice, err := s.discoveredDeviceStore.GetDevice(ctx, orgDeviceID)
	if err != nil {
		return fleeterror.NewInternalErrorf("error getting device from store: %v", err)
	}

	deviceType, err := models.TypeFromString(discoveredDevice.Type)
	if err != nil {
		return fleeterror.NewInternalErrorf("invalid device type for pairing: %v", err)
	}

	pairer, ok := s.pairers[deviceType]
	if !ok {
		return fleeterror.NewInvalidArgumentErrorf("device type '%s' is not supported for pairing yet", discoveredDevice.Type)
	}

	discoveredDevice.IsActive = true
	_, err = s.discoveredDeviceStore.Save(ctx, orgDeviceID, discoveredDevice)
	if err != nil {
		return fleeterror.NewInternalErrorf("error activating discovered device: %v", err)
	}

	if err := pairer.PairDevice(ctx, discoveredDevice, credentials); err != nil {
		return fleeterror.NewInternalErrorf("pairing device %s: %v", discoveredDevice.DeviceIdentifier, err)
	}

	return nil
}
