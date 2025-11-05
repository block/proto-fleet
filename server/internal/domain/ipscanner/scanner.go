package ipscanner

import (
	"context"
	"log/slog"
	"sync"

	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
)

// NetworkScanner handles the actual network scanning operations
type NetworkScanner struct {
	discoveryService     *minerdiscovery.Service
	maxConcurrentIPScans int
	logger               *slog.Logger
}

// NewNetworkScanner creates a new network scanner
func NewNetworkScanner(discoveryService *minerdiscovery.Service, maxConcurrentIPScans int, logger *slog.Logger) *NetworkScanner {
	return &NetworkScanner{
		discoveryService:     discoveryService,
		maxConcurrentIPScans: maxConcurrentIPScans,
		logger:               logger.With("component", "network_scanner"),
	}
}

// ScanSubnetForDevices scans a subnet for multiple devices at once
func (n *NetworkScanner) ScanSubnetForDevices(
	ctx context.Context,
	subnet string,
	targetDevices []TargetDevice,
) ([]DeviceMatch, error) {
	n.logger.Info("Starting subnet scan for multiple devices",
		"subnet", subnet,
		"device_count", len(targetDevices),
	)

	// Build a map of MAC addresses to target devices for quick lookup
	targetsByMAC := make(map[string]TargetDevice)
	for _, target := range targetDevices {
		normalizedMAC := networking.NormalizeMAC(target.DeviceMAC)
		targetsByMAC[normalizedMAC] = target
	}

	// Generate all IPs in the subnet
	ips, err := generateIPsFromCIDR(subnet)
	if err != nil {
		n.logger.Error("Failed to generate IPs from CIDR",
			"subnet", subnet,
			"error", err,
		)
		return nil, err
	}

	n.logger.Debug("Generated IP list from subnet",
		"subnet", subnet,
		"ip_count", len(ips),
	)

	// Scan IPs concurrently with limited concurrency
	matches := n.scanIPsConcurrentlyForMultipleDevices(ctx, ips, targetsByMAC)

	n.logger.Info("Subnet scan completed",
		"subnet", subnet,
		"devices_sought", len(targetDevices),
		"devices_found", len(matches),
	)

	return matches, nil
}

// scanIPsConcurrentlyForMultipleDevices scans IPs and checks against multiple target devices
func (n *NetworkScanner) scanIPsConcurrentlyForMultipleDevices(
	ctx context.Context,
	ips []string,
	targetsByMAC map[string]TargetDevice,
) []DeviceMatch {
	var (
		wg              sync.WaitGroup
		mu              sync.Mutex
		matches         []DeviceMatch
		foundMACs       = make(map[string]bool)
		sem             = make(chan struct{}, n.maxConcurrentIPScans)
		scanCtx, cancel = context.WithCancel(ctx)
	)
	defer cancel()

	// We need to try different ports since different devices may use different ports
	portsToTry := make(map[string]bool)
	for _, target := range targetsByMAC {
		portsToTry[target.Port] = true
	}

	for _, ip := range ips {
		// If we've found all target devices, stop scanning
		mu.Lock()
		allFound := len(foundMACs) == len(targetsByMAC)
		mu.Unlock()
		if allFound {
			break
		}

		select {
		case <-scanCtx.Done():
			break
		default:
		}

		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(ipAddr string) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			// Skip if context cancelled
			select {
			case <-scanCtx.Done():
				return
			default:
			}

			// Try each port for each target device at this IP
			for port := range portsToTry {
				// Try to discover device at this IP and port
				device, err := n.discoveryService.Discover(scanCtx, ipAddr, port)
				if err != nil {
					// Discovery failed, which is expected for most IPs
					continue
				}

				// Check if we found a device
				if device != nil {
					deviceMAC := networking.NormalizeMAC(device.MacAddress)

					// Only accept device if MAC address is available
					// We cannot safely update IP assignments without MAC verification
					// TODO add support for miners that require auth the get MAC address
					if deviceMAC == "" {
						n.logger.Debug("Device found but MAC address not available - skipping",
							"ip", ipAddr,
							"port", port,
							"reason", "Cannot verify device identity without MAC address",
						)
						continue
					}

					// Check if this MAC matches any of our target devices
					if target, found := targetsByMAC[deviceMAC]; found {
						mu.Lock()
						// Check if we haven't already found this device
						if !foundMACs[deviceMAC] {
							foundMACs[deviceMAC] = true
							matches = append(matches, DeviceMatch{
								TargetDevice:   target,
								DiscoveredIP:   ipAddr,
								DiscoveredPort: device.Port,
								URLScheme:      device.UrlScheme,
							})
							n.logger.Info("Device found with verified MAC address",
								"ip", ipAddr,
								"port", port,
								"mac_address", device.MacAddress,
								"device_identifier", target.DeviceIdentifier,
							)
						}
						mu.Unlock()
						break // Found device at this IP, no need to try other ports
					}
				}
			}
		}(ip)
	}

	wg.Wait()
	return matches
}
