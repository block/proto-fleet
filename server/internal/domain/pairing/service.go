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

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/Ullaakut/nmap"
	"github.com/grandcat/zeroconf"
)

// Service handles the core device discovery functionality
type Service struct {
	conn *sql.DB
	cfg  Config
}

func NewService(conn *sql.DB, cfg Config) *Service {
	return &Service{
		conn: conn,
		cfg:  cfg,
	}
}

// Helper function to convert IP string to uint32 for range comparison
func ipToUint32(ip string) (uint32, error) {
	addr := net.ParseIP(ip)
	ipv4 := addr.To4()
	if ipv4 == nil {
		return 0, fmt.Errorf("not a valid IPv4 address: '%v'", ip)
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
		return nil, fmt.Errorf("failed to initialize resolver: %v", err)
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

				device := &pb.Device{
					DiscoveredAt: time.Now().Unix(),
				}

				if len(entry.AddrIPv4) > 0 {
					device.IpAddress = entry.AddrIPv4[0].String()
					device.MacAddress = networking.GetMacAddress(device.IpAddress)
				}

				if device.MacAddress == "" {
					continue
				}
				portStr := fmt.Sprintf("%d", entry.Port)
				// Check if host is reachable
				if err := checkDeviceReachability(device.IpAddress, portStr, time.Second); err == nil {
					device.Port = portStr
					err := s.saveDevice(ctx, device)
					if err != nil {
						slog.Warn("error saving", "error", err)
						continue
					}

					select {
					case resultChan <- &pb.DiscoverResponse{
						Devices: []*pb.Device{device},
					}:
					case <-ctx.Done():
						return
					}
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
				nmap.WithTargets(r.Target),
				nmap.WithFastMode(),
			)
		} else {
			scanner, err = nmap.NewScanner(
				nmap.WithTargets(r.Target),
				nmap.WithPorts(strings.Join(r.Ports, ",")),
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

			device := &pb.Device{
				DiscoveredAt: time.Now().Unix(),
			}

			for _, addr := range host.Addresses {
				if addr.AddrType == "ipv4" {
					device.IpAddress = addr.Addr
					device.MacAddress = networking.GetMacAddress(device.IpAddress)
				}
			}

			if device.MacAddress == "" {
				continue
			}

			for _, port := range host.Ports {
				portStr := fmt.Sprintf("%d", port.ID)
				// Check if host is reachable
				if err := checkDeviceReachability(device.IpAddress, portStr, time.Second); err == nil {
					device.Port = portStr
					err := s.saveDevice(ctx, device)
					if err != nil {
						slog.Warn("error saving", "error", err)
						continue
					}

					select {
					case resultChan <- &pb.DiscoverResponse{
						Devices: []*pb.Device{device},
					}:
					case <-ctx.Done():
						return
					}
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
		return nil, fmt.Errorf("error parsing start ip: %v", err)
	}
	endIP, err := ipToUint32(r.EndIp)
	if err != nil {
		return nil, fmt.Errorf("error parsing end ip: %v", err)
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

					device := &pb.Device{
						IpAddress:    ipAddr,
						DiscoveredAt: time.Now().Unix(),
						MacAddress:   networking.GetMacAddress(ipAddr),
					}

					if device.MacAddress == "" {
						return
					}

					for _, port := range r.Ports {
						// Check if host is reachable
						if err := checkDeviceReachability(ipAddr, port, time.Duration(r.TimeoutSeconds)*time.Second); err == nil {
							device.Port = port
							err := s.saveDevice(ctx, device)
							if err != nil {
								slog.Warn("error saving", "error", err)
								continue
							}

							select {
							case resultChan <- &pb.DiscoverResponse{
								Devices: []*pb.Device{device},
							}:
							case <-ctx.Done():
							}
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

					device := &pb.Device{
						IpAddress:    ipAddr,
						DiscoveredAt: time.Now().Unix(),
						MacAddress:   networking.GetMacAddress(ipAddr),
					}

					if device.MacAddress == "" {
						return
					}

					for _, port := range r.Ports {
						// Check if host is reachable
						if err := checkDeviceReachability(ipAddr, port, time.Duration(r.TimeoutSeconds)*time.Second); err == nil {
							device.Port = port
							err := s.saveDevice(ctx, device)
							if err != nil {
								slog.Warn("error saving", "error", err)
								continue
							}

							select {
							case resultChan <- &pb.DiscoverResponse{
								Devices: []*pb.Device{device},
							}:
							case <-ctx.Done():
							}
						}
					}
				}(ip)
			}
		}

		wg.Wait()
	}()

	return resultChan, nil
}

// checkDeviceReachability attempts to connect to a host on specified ports with timeout
func checkDeviceReachability(host string, port string, timeout time.Duration) error {
	address := fmt.Sprintf("%s:%s", host, port)
	// TODO the check should call api on the miner
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return fmt.Errorf("failed to check host: address=%s %w", address, err)
	}
	defer func(conn net.Conn) {
		_ = conn.Close()
	}(conn)
	return nil
}

func (s *Service) saveDevice(ctx context.Context, device *pb.Device) error {
	return db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
		// Upsert device
		result, err := q.UpsertDevice(ctx, sqlc.UpsertDeviceParams{
			DeviceIdentifier: uuid.NewString(),
			MacAddress:       device.MacAddress,
			SerialNumber:     sql.NullString{String: device.SerialNumber, Valid: len(device.SerialNumber) > 0},
			IsActive:         sql.NullBool{Bool: true, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("failed to upsert device: %w", err)
		}

		// Get device ID (either from insert or existing record)
		deviceID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("failed to upsert device: %w", err)
		}

		dbDevice, err := q.GetDeviceByID(ctx, deviceID)
		if err != nil {
			return fmt.Errorf("failed to fetch device: id=%d %w", deviceID, err)
		}
		device.DeviceIdentifier = dbDevice.DeviceIdentifier

		// Deactivate old IP assignments
		err = q.DeactivateOldIPAssignments(ctx, sqlc.DeactivateOldIPAssignmentsParams{
			DeviceID:  deviceID,
			IpAddress: device.IpAddress,
		})
		if err != nil {
			return fmt.Errorf("failed to deactivate old IP assignments: %w", err)
		}

		// Upsert new IP assignment
		_, err = q.UpsertDeviceIPAssignment(ctx, sqlc.UpsertDeviceIPAssignmentParams{
			DeviceID:  deviceID,
			IpAddress: device.IpAddress,
			Port:      device.Port,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert IP assignment: %w", err)
		}

		return nil
	})
}

func (s *Service) PairDevices(ctx context.Context, r *pb.PairRequest) (*pb.PairResponse, error) {
	err := db.WithTransactionNoResult(ctx, s.conn, func(q *sqlc.Queries) error {
		// Create pairing records for each device
		for _, dID := range r.DeviceIdentifiers {
			device, err := q.GetDeviceByDeviceIdentifier(ctx, dID)
			if err != nil {
				return fmt.Errorf("failed get device device_identifier=%s: %w", dID, err)
			}
			pairingToken, err := s.generatePairingToken(&device)
			if err != nil {
				return fmt.Errorf("failed generate pairing token for device device_identifier=%s: %w", dID, err)
			}
			_, err = q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{
				DeviceID:      device.ID,
				PairingToken:  sql.NullString{Valid: true, String: pairingToken},
				PairingStatus: "PAIRED",
			})
			if err != nil {
				return fmt.Errorf("failed to create pairing for device device_identifier=%s: %w", dID, err)
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
	// TODO remove defaulting to mac when we start getting serial no from miner
	deviceKey := device.SerialNumber.String
	if deviceKey == "" {
		deviceKey = device.MacAddress
	}
	bytes, err := bcrypt.GenerateFromPassword(fmt.Appendf(nil, "%s:%s", s.cfg.SecretKey, deviceKey), 14)
	return string(bytes), err
}
