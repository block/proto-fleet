package ipscanner

import (
	"context"
	"log/slog"
	"sync"

	"github.com/block/proto-fleet/server/internal/domain/minerdiscovery"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=scanner.go -destination=mocks/mock_scanner.go -package=mocks DeviceIdentityCheckService

// DeviceIdentityCheckService defines the interface for device identity verification
type DeviceIdentityCheckService interface {
	IsSameDevice(ctx context.Context, newDiscoveredDevice *discoverymodels.DiscoveredDevice, pairedDeviceIdentifier string, orgID int64) bool
}

// NetworkScanner handles the actual network scanning operations
type NetworkScanner struct {
	discoverer           minerdiscovery.Discoverer
	deviceIDCheckService DeviceIdentityCheckService
	maxConcurrentIPScans int
	logger               *slog.Logger
}

// NewNetworkScanner creates a new network scanner
func NewNetworkScanner(discoverer minerdiscovery.Discoverer, deviceIDCheckService DeviceIdentityCheckService, maxConcurrentIPScans int, logger *slog.Logger) *NetworkScanner {
	return &NetworkScanner{
		discoverer:           discoverer,
		deviceIDCheckService: deviceIDCheckService,
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
	matches := n.scanIPsConcurrentlyForMultipleDevices(ctx, ips, targetDevices)

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
	targetDevices []TargetDevice,
) []DeviceMatch {
	var (
		wg              sync.WaitGroup
		mu              sync.Mutex
		matches         []DeviceMatch
		foundDevices    = make(map[string]bool) // Track found devices by identifier
		sem             = make(chan struct{}, n.maxConcurrentIPScans)
		scanCtx, cancel = context.WithCancel(ctx)
	)
	defer cancel()

	// We need to try different ports since different devices may use different ports
	portsToTry := make(map[string]bool)
	for _, target := range targetDevices {
		portsToTry[target.Port] = true
	}

	totalTargets := len(targetDevices)

scanLoop:
	for _, ip := range ips {
		// If we've found all target devices, stop scanning
		mu.Lock()
		allFound := len(foundDevices) == totalTargets
		mu.Unlock()
		if allFound {
			break
		}

		select {
		case <-scanCtx.Done():
			break scanLoop
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
				device, err := n.discoverer.Discover(scanCtx, ipAddr, port)
				if err != nil {
					// Discovery failed, which is expected for most IPs
					continue
				}

				// Check if we found a device
				if device != nil {
					var matchedTarget *TargetDevice
					for i := range targetDevices {
						if device.DriverName != targetDevices[i].DriverName {
							continue
						}

						// Driver name matches, now check if it's the same device via API
						if n.deviceIDCheckService.IsSameDevice(scanCtx, device, targetDevices[i].DeviceIdentifier, targetDevices[i].OrgID) {
							matchedTarget = &targetDevices[i]
							n.logger.Debug("Device matched by identity check",
								"ip", ipAddr,
								"port", port,
								"device_identifier", targetDevices[i].DeviceIdentifier,
							)
							break
						}
					}

					// If we found a match, record it
					if matchedTarget != nil {
						mu.Lock()
						if !foundDevices[matchedTarget.DeviceIdentifier] {
							foundDevices[matchedTarget.DeviceIdentifier] = true
							matches = append(matches, DeviceMatch{
								TargetDevice:   *matchedTarget,
								DiscoveredIP:   ipAddr,
								DiscoveredPort: device.Port,
								URLScheme:      device.UrlScheme,
							})
							n.logger.Info("Device found and verified",
								"ip", ipAddr,
								"port", port,
								"driver_name", device.DriverName,
								"mac_address", device.MacAddress,
								"device_identifier", matchedTarget.DeviceIdentifier,
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
