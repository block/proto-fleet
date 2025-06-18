package pairing

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"

	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/Ullaakut/nmap/v3"
	"github.com/grandcat/zeroconf"
)

// Service handles the core device discovery functionality
type Service struct {
	conn                  *sql.DB
	cfg                   Config
	tokenService          *tokenDomain.Service
	minerDiscoveryService *minerdiscovery.Service
}

func NewService(
	conn *sql.DB,
	cfg Config,
	tokenService *tokenDomain.Service,
	minerDiscoveryService *minerdiscovery.Service,
) *Service {
	return &Service{
		conn:                  conn,
		cfg:                   cfg,
		tokenService:          tokenService,
		minerDiscoveryService: minerDiscoveryService,
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
	device, err := s.minerDiscoveryService.Discover(ctx, ipAddress, port)
	if err != nil {
		slog.Debug("Discovery failed",
			"ipAddress", ipAddress,
			"port", port,
			"error", err)
		return err
	}

	// Process the discovered device
	err = s.processDiscoveredDevice(ctx, device, resultChan)
	if err != nil {
		return err
	}

	return nil
}

func (s *Service) processDiscoveredDevice(ctx context.Context, device *pb.Device, resultChan chan<- *pb.DiscoverResponse) error {
	err := s.saveDevice(ctx, device)
	if err != nil {
		return err
	}

	select {
	case resultChan <- &pb.DiscoverResponse{
		Devices: []*pb.Device{device},
	}:
	case <-ctx.Done():
	}

	return nil
}

func (s *Service) saveDevice(ctx context.Context, device *pb.Device) error {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return err
	}

	return db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
		// Use existing device identifier if available, otherwise generate new UUID
		deviceIdentifier := device.DeviceIdentifier
		if deviceIdentifier == "" {
			deviceIdentifier = uuid.NewString()
		}

		result, err := q.UpsertDevice(ctx, sqlc.UpsertDeviceParams{
			OrgID:            claims.OrgID,
			DeviceIdentifier: deviceIdentifier,
			MacAddress:       device.MacAddress,
			SerialNumber:     sql.NullString{String: device.SerialNumber, Valid: len(device.SerialNumber) > 0},
			Model:            sql.NullString{String: device.Model, Valid: len(device.Model) > 0},
			Manufacturer:     sql.NullString{String: device.Manufacturer, Valid: len(device.Manufacturer) > 0},
			Type:             miner.TypeProto.String(),
			IsActive:         sql.NullBool{Bool: true, Valid: true},
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to upsert device: %v", err)
		}

		deviceID, err := result.LastInsertId()
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to get device ID: %v", err)
		}

		dbDevice, err := q.GetDeviceByID(ctx, sqlc.GetDeviceByIDParams{ID: deviceID, OrgID: claims.OrgID})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to fetch device: id=%d %v", deviceID, err)
		}

		device.DeviceIdentifier = dbDevice.DeviceIdentifier

		currentIPAssignment, err := q.GetActiveDeviceIPAssignmentByDeviceID(ctx, deviceID)
		if err != nil && err != sql.ErrNoRows {
			return fleeterror.NewInternalErrorf("failed to query active device IP assignment: %v", err)
		} else if err != sql.ErrNoRows && currentIPAssignment.IpAddress == device.IpAddress && currentIPAssignment.Port == device.Port {
			return nil // Device IP assignment already exists
		}

		err = q.CreateInactiveDeviceIPAssignment(ctx, sqlc.CreateInactiveDeviceIPAssignmentParams{
			DeviceID:  deviceID,
			IpAddress: device.IpAddress,
			Port:      device.Port,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to create IP assignment: %v", err)
		}

		err = q.ActivateNewIPAssignment(ctx, sqlc.ActivateNewIPAssignmentParams{
			DeviceID:  deviceID,
			IpAddress: device.IpAddress,
			Port:      device.Port,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to activate new IP assignment: %v", err)
		}

		return nil
	})
}

func (s *Service) PairDevices(ctx context.Context, r *pb.PairRequest) (*pb.PairResponse, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	err = db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
		// Create pairing records for each device
		for _, dID := range r.DeviceIdentifiers {
			device, err := q.GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
				DeviceIdentifier: dID,
				OrgID:            claims.OrgID,
			})
			if err != nil {
				return fleeterror.NewInternalErrorf("failed get device device_identifier=%s: %v", dID, err)
			}
			pairingToken, err := s.generatePairingToken(&device)
			if err != nil {
				return fleeterror.NewInternalErrorf("failed generate pairing token for device device_identifier=%s: %v", dID, err)
			}
			_, err = q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{
				DeviceID:      device.ID,
				PairingToken:  sql.NullString{Valid: true, String: pairingToken},
				PairingStatus: "PAIRED",
			})
			if err != nil {
				return fleeterror.NewInternalErrorf("failed to create pairing for device device_identifier=%s: %v", dID, err)
			}

			minerInfo, err := q.GetMinerApiNetworkInfoByDeviceID(ctx, sqlc.GetMinerApiNetworkInfoByDeviceIDParams{OrgID: claims.OrgID, DeviceIdentifier: dID})
			if err != nil {
				return fleeterror.NewInternalErrorf("failed to query miner info: %v", minerInfo)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return &pb.PairResponse{}, nil
}

func (s *Service) generatePairingToken(device *sqlc.Device) (string, error) {
	deviceKey := device.SerialNumber.String
	bytes, err := bcrypt.GenerateFromPassword(fmt.Appendf(nil, "%s:%s", s.cfg.SecretKey, deviceKey), 14)
	if err != nil {
		return "", fleeterror.NewInternalError(err.Error())
	}

	return string(bytes), nil
}
