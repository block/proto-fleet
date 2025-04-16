package pairing

import (
	"context"
	"encoding/binary"
	"fmt"
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
	Metadata        map[string]string
	IPAddress       string
}
type MDNSDiscoveryRequest struct {
	ServiceType    string
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

// DiscoverWithMDNS discovers devices using mDNS
func (s *Service) DiscoverWithMDNS(ctx context.Context, r *MDNSDiscoveryRequest) (<-chan *DiscoveryResponse, error) {
	resultChan := make(chan *DiscoveryResponse)

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
			resultChan <- &DiscoveryResponse{
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

				device := &Device{
					Hostname:     entry.HostName,
					DiscoveredAt: time.Now().Unix(),
					Metadata:     make(map[string]string),
				}

				if len(entry.AddrIPv4) > 0 {
					device.IPAddress = entry.AddrIPv4[0].String()
				}

				// Add metadata from TXT records
				for key, value := range entry.Text {
					device.Metadata[string(rune(key))] = value
				}

				resultChan <- &DiscoveryResponse{
					Devices: []*Device{device},
				}

			case <-timeoutCtx.Done():
				return
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
				Metadata:     make(map[string]string),
			}

			for _, addr := range host.Addresses {
				if addr.AddrType == "ipv4" {
					device.IPAddress = addr.Addr
				} else if addr.AddrType == "mac" {
					device.MacAddress = addr.Addr
				}
			}

			if len(host.Hostnames) > 0 {
				device.Hostname = host.Hostnames[0].Name
			}

			if host.Status.State != "" {
				device.Metadata["state"] = host.Status.State
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
						Metadata:     make(map[string]string),
					}

					// Try to resolve hostname
					if names, err := net.LookupAddr(ipAddr); err == nil && len(names) > 0 {
						device.Hostname = names[0]
					}
					for _, port := range req.Ports {
						// Check if host is reachable
						if conn, err := net.DialTimeout("tcp", net.JoinHostPort(ipAddr, port), time.Duration(req.TimeoutSeconds)*time.Second); err == nil {
							_ = conn.Close()
							device.Metadata["status"] = "online"

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
						Metadata:     make(map[string]string),
					}

					// Try to resolve hostname
					if names, err := net.LookupAddr(ipAddr); err == nil && len(names) > 0 {
						device.Hostname = names[0]
					}
					for _, port := range req.Ports {
						// Check if host is reachable
						if conn, err := net.DialTimeout("tcp", net.JoinHostPort(ipAddr, port), time.Duration(req.TimeoutSeconds)*time.Second); err == nil {
							_ = conn.Close()
							device.Metadata["status"] = "online"

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
