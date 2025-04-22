package pairing

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/Ullaakut/nmap"
	"github.com/grandcat/zeroconf"
)

// Service handles the core device discovery functionality
type Service struct {
}

// NewService creates a new instance of Service
func NewService() *Service {
	return &Service{}
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

type Device struct {
	Hostname        string
	MacAddress      string
	DiscoveryMethod string
	DiscoveredAt    int64
	IPAddress       string
}
type NetworkInfo struct {
	networking.NetworkInfo
}
type MDNSDiscoveryRequest struct {
	ServiceType    string
	Domain         string
	TimeoutSeconds int32
}
type NmapDiscoveryRequest struct {
	Target   string
	Ports    []string
	FastScan bool
}
type IPListDiscoveryRequest struct {
	IPAddresses    []string
	Ports          []string
	TimeoutSeconds int32
}
type IPRangeDiscoveryRequest struct {
	StartIP        string
	EndIP          string
	Ports          []string
	TimeoutSeconds int32
}
type DiscoveryResponse struct {
	Error   string
	Devices []*Device
}

func (s *Service) GetLocalNetworkInfo(_ context.Context) (*NetworkInfo, error) {
	info, err := networking.GetLocalNetworkInfo()
	if err != nil {
		return nil, err
	}
	return &NetworkInfo{info}, nil
}

// DiscoverWithMDNS discovers devices using mDNS
func (s *Service) DiscoverWithMDNS(ctx context.Context, r *MDNSDiscoveryRequest) (<-chan *DiscoveryResponse, error) {
	// Use buffered channels to prevent blocking
	resultChan := make(chan *DiscoveryResponse, 10)
	entries := make(chan *zeroconf.ServiceEntry, 10)

	// Initialize resolver
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize resolver: %v", err)
	}

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(r.TimeoutSeconds)*time.Second)

	// Start goroutine for processing entries
	go func() {
		// Ensure cleanup
		defer close(resultChan)
		defer cancel()
		slog.Debug("DiscoverWithMDNS: discovering mdns...")
		for {
			select {
			case entry, ok := <-entries:
				if !ok {
					slog.Debug("DiscoverWithMDNS: not ok")
					return
				}

				slog.Debug("DiscoverWithMDNS", "entry", entry)
				if entry == nil {
					slog.Debug("DiscoverWithMDNS: empty entry")
					continue
				}

				device := &Device{
					Hostname:     entry.HostName,
					DiscoveredAt: time.Now().Unix(),
				}

				if len(entry.AddrIPv4) > 0 {
					ipAddr := entry.AddrIPv4[0].String()
					device.IPAddress = ipAddr
					device.MacAddress = networking.GetMacAddress(ipAddr)
				}

				if device.MacAddress == "" || device.IPAddress == "" {
					continue
				}

				// Try to resolve hostname
				if names, err := net.LookupAddr(device.IPAddress); err == nil && len(names) > 0 {
					device.Hostname = names[0]
				}

				// Handle context cancellation during send
				select {
				case resultChan <- &DiscoveryResponse{
					Devices: []*Device{device},
				}:
				case <-timeoutCtx.Done():
					slog.Debug("DiscoverWithMDNS: timed out")
					return
				}

			case <-timeoutCtx.Done():
				slog.Debug("DiscoverWithMDNS: discovering done")
				return
			}
		}
	}()

	domain := "local."
	if r.Domain != "" {
		domain = r.Domain
	}
	// Start browsing in a separate goroutine
	go func() {
		slog.Debug("DiscoverWithMDNS: browsing")
		err := resolver.Browse(timeoutCtx, r.ServiceType, domain, entries)
		if err != nil {
			select {
			case resultChan <- &DiscoveryResponse{
				Error: fmt.Sprintf("failed to browse: %v", err),
			}:
			case <-timeoutCtx.Done():
				// Context is done, don't send error
			}
		}
	}()

	return resultChan, nil
}

// DiscoverWithNmap discovers devices using Nmap
func (s *Service) DiscoverWithNmap(ctx context.Context, req *NmapDiscoveryRequest) (<-chan *DiscoveryResponse, error) {
	resultChan := make(chan *DiscoveryResponse)

	go func() {
		defer close(resultChan)

		var scanner *nmap.Scanner
		var err error
		if len(req.Ports) == 0 && req.FastScan {
			scanner, err = nmap.NewScanner(
				nmap.WithTargets(req.Target),
				nmap.WithFastMode(),
			)
		} else {
			scanner, err = nmap.NewScanner(
				nmap.WithTargets(req.Target),
				nmap.WithPorts(strings.Join(req.Ports, ",")),
			)
		}

		if err != nil {
			resultChan <- &DiscoveryResponse{
				Error: fmt.Sprintf("failed to create scanner: %v", err),
			}
			return
		}

		result, _, err := scanner.Run()
		if err != nil {
			resultChan <- &DiscoveryResponse{
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

			device := &Device{
				DiscoveredAt: time.Now().Unix(),
			}

			for _, addr := range host.Addresses {
				if addr.AddrType == "ipv4" {
					device.IPAddress = addr.Addr
				}
			}

			if len(device.IPAddress) == 0 {
				continue
			}

			device.MacAddress = networking.GetMacAddress(device.IPAddress)

			if device.MacAddress == "" {
				continue
			}

			// Try to resolve hostname
			if names, err := net.LookupAddr(device.IPAddress); err == nil && len(names) > 0 {
				device.Hostname = names[0]
			}

			select {
			case resultChan <- &DiscoveryResponse{
				Devices: []*Device{device},
			}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return resultChan, nil
}

// DiscoverWithIPRange discovers devices using IP range
func (s *Service) DiscoverWithIPRange(ctx context.Context, req *IPRangeDiscoveryRequest) (<-chan *DiscoveryResponse, error) {
	resultChan := make(chan *DiscoveryResponse)
	startIP, err := ipToUint32(req.StartIP)
	if err != nil {
		return nil, fmt.Errorf("error parsing start ip: %v", err)
	}
	endIP, err := ipToUint32(req.EndIP)
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

					device := &Device{
						IPAddress:    ipAddr,
						DiscoveredAt: time.Now().Unix(),
						MacAddress:   networking.GetMacAddress(ipAddr),
					}

					if device.MacAddress == "" {
						return
					}

					// Try to resolve hostname
					if names, err := net.LookupAddr(ipAddr); err == nil && len(names) > 0 {
						device.Hostname = names[0]
					}
					for _, port := range req.Ports {
						// Check if host is reachable
						if conn, err := net.DialTimeout("tcp", net.JoinHostPort(ipAddr, port), time.Duration(req.TimeoutSeconds)*time.Second); err == nil {
							_ = conn.Close()
							select {
							case resultChan <- &DiscoveryResponse{
								Devices: []*Device{device},
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
func (s *Service) DiscoverWithIPList(ctx context.Context, req *IPListDiscoveryRequest) (<-chan *DiscoveryResponse, error) {
	resultChan := make(chan *DiscoveryResponse)

	go func() {
		defer close(resultChan)

		var wg sync.WaitGroup
		semaphore := make(chan struct{}, 10) // Limit concurrent goroutines

		for _, ip := range req.IPAddresses {
			select {
			case <-ctx.Done():
				return
			default:
				wg.Add(1)
				semaphore <- struct{}{} // Acquire semaphore

				go func(ipAddr string) {
					defer wg.Done()
					defer func() { <-semaphore }() // Release semaphore

					device := &Device{
						IPAddress:    ipAddr,
						DiscoveredAt: time.Now().Unix(),
						MacAddress:   networking.GetMacAddress(ipAddr),
					}

					if device.MacAddress == "" {
						return
					}
					// Try to resolve hostname
					if names, err := net.LookupAddr(ipAddr); err == nil && len(names) > 0 {
						device.Hostname = names[0]
					}
					for _, port := range req.Ports {
						// Check if host is reachable
						if conn, err := net.DialTimeout("tcp", net.JoinHostPort(ipAddr, port), time.Duration(req.TimeoutSeconds)*time.Second); err == nil {
							_ = conn.Close()

							select {
							case resultChan <- &DiscoveryResponse{
								Devices: []*Device{device},
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
