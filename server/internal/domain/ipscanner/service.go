package ipscanner

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/minerdiscovery"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Service orchestrates automatic IP address discovery for offline devices
type Service struct {
	config                Config
	deviceStore           stores.DeviceStore
	discoveredDeviceStore stores.DiscoveredDeviceStore
	discoverer            minerdiscovery.Discoverer
	deviceIDCheckService  DeviceIdentityCheckService
	scanner               *NetworkScanner
	logger                *slog.Logger

	// Worker pool
	tasks      chan SubnetScanTask
	results    chan SubnetScanResult
	cancelFunc context.CancelFunc
	wg         sync.WaitGroup
	mux        sync.Mutex // Prevents multiple instances of scanLoop from running
}

// NewIPScannerService creates a new IP scanner service
func NewIPScannerService(
	config Config,
	deviceStore stores.DeviceStore,
	discoveredDeviceStore stores.DiscoveredDeviceStore,
	discoverer minerdiscovery.Discoverer,
	deviceIDCheckService DeviceIdentityCheckService,
	logger *slog.Logger,
) *Service {
	return &Service{
		config:                config,
		deviceStore:           deviceStore,
		discoveredDeviceStore: discoveredDeviceStore,
		discoverer:            discoverer,
		deviceIDCheckService:  deviceIDCheckService,
		scanner:               NewNetworkScanner(discoverer, deviceIDCheckService, config.MaxConcurrentIPScansPerSubnet, logger),
		logger:                logger.With("component", "ipscanner"),
		tasks:                 make(chan SubnetScanTask, config.MaxConcurrentSubnetScans),
		results:               make(chan SubnetScanResult, config.MaxConcurrentSubnetScans),
	}
}

// Start begins the IP scanner service
func (s *Service) Start(ctx context.Context) error {
	if !s.config.Enabled {
		s.logger.Info("IP scanner service is disabled")
		return nil
	}

	// Prevent multiple Start() calls from creating duplicate goroutines
	if !s.mux.TryLock() {
		s.logger.Warn("IP scanner service already started")
		return nil
	}
	defer s.mux.Unlock()

	// Check if already started by examining cancelFunc
	if s.cancelFunc != nil {
		s.logger.Warn("IP scanner service already running")
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel

	s.logger.Info("Starting IP scanner service",
		"scan_interval", s.config.ScanInterval,
		"max_concurrent_subnet_scans", s.config.MaxConcurrentSubnetScans,
		"max_concurrent_ip_scans_per_subnet", s.config.MaxConcurrentIPScansPerSubnet,
	)

	// Start worker pool
	for i := range s.config.MaxConcurrentSubnetScans {
		s.wg.Add(1)
		go s.scanWorker(ctx, i)
	}

	// Start result processor
	s.wg.Add(1)
	go s.resultProcessor(ctx)

	// Start main scan loop
	s.wg.Add(1)
	go s.scanLoop(ctx)

	return nil
}

// Stop gracefully stops the IP scanner service
func (s *Service) Stop() error {
	if s.cancelFunc != nil {
		s.logger.Info("Stopping IP scanner service")
		s.cancelFunc()
		s.wg.Wait()
		s.cancelFunc = nil // Reset to allow restart
		s.logger.Info("IP scanner service stopped")
	}
	return nil
}

// scanLoop periodically scans for offline devices
func (s *Service) scanLoop(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.ScanInterval)
	defer ticker.Stop()

	// Run immediately on start
	s.scanOfflineDevices(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scanOfflineDevices(ctx)
		}
	}
}

// scanOfflineDevices fetches offline devices and dispatches scan tasks grouped by subnet
func (s *Service) scanOfflineDevices(ctx context.Context) {
	s.logger.Debug("Scanning for offline devices")

	// Fetch offline devices from the device store
	// Limit to MaxConcurrentSubnetScans to avoid overwhelming the system
	offlineDevices, err := s.deviceStore.GetOfflineDevices(ctx, s.config.MaxConcurrentSubnetScans)
	if err != nil {
		s.logger.Error("Failed to fetch offline devices",
			"error", err,
		)
		return
	}

	if len(offlineDevices) == 0 {
		s.logger.Debug("No offline devices found")
		return
	}

	s.logger.Info("Found offline devices to scan",
		"count", len(offlineDevices),
	)

	// Group devices by subnet for efficient scanning
	devicesBySubnet := make(map[string][]TargetDevice)
	skippedDevices := 0

	for _, device := range offlineDevices {
		// Skip devices without required information
		if device.MacAddress == "" {
			s.logger.Warn("Skipping device without MAC address",
				"device_identifier", device.DeviceIdentifier,
			)
			skippedDevices++
			continue
		}

		if device.LastKnownIP == "" {
			s.logger.Warn("Skipping device without last known IP",
				"device_identifier", device.DeviceIdentifier,
			)
			skippedDevices++
			continue
		}

		if device.LastKnownPort == "" {
			s.logger.Warn("Skipping device without last known port",
				"device_identifier", device.DeviceIdentifier,
			)
			skippedDevices++
			continue
		}

		// For IPv6 devices, probe only the last-known address (/128) instead of
		// sweeping a subnet. IPv6 subnets are too large to enumerate, but
		// re-probing the exact address handles the common case of a device
		// that rebooted and kept its address.
		maskBits := s.config.SubnetMaskBits
		if parsedIP := net.ParseIP(device.LastKnownIP); parsedIP != nil && parsedIP.To4() == nil {
			maskBits = 128
		}

		// Derive subnet from last known IP
		subnet, err := ipToSubnet(device.LastKnownIP, maskBits)
		if err != nil {
			s.logger.Warn("Failed to derive subnet from IP",
				"device_identifier", device.DeviceIdentifier,
				"last_known_ip", device.LastKnownIP,
				"error", err,
			)
			skippedDevices++
			continue
		}

		// Add device to subnet bucket
		targetDevice := TargetDevice{
			DeviceID:                   device.DeviceID,
			DeviceIdentifier:           device.DeviceIdentifier,
			DiscoveredDeviceIdentifier: device.DiscoveredDeviceIdentifier,
			DeviceMAC:                  device.MacAddress,
			DriverName:                 device.DriverName,
			Port:                       device.LastKnownPort,
			URLScheme:                  device.LastKnownURLScheme,
			OrgID:                      device.OrgID,
		}
		devicesBySubnet[subnet] = append(devicesBySubnet[subnet], targetDevice)
	}

	s.logger.Debug("Grouped devices by subnet",
		"total_devices", len(offlineDevices),
		"skipped_devices", skippedDevices,
		"unique_subnets", len(devicesBySubnet),
	)

	// Dispatch one scan task per subnet
	tasksDispatched := 0
	tasksSkipped := 0
	// Calculate dispatch timeout: 1.5x scan timeout, but never more than half the scan interval
	// This ensures we wait long enough for workers to free up, but don't block the scan loop
	dispatchTimeout := time.Duration(float64(s.config.ScanTimeout) * 1.5)
	if dispatchTimeout > s.config.ScanInterval/2 {
		dispatchTimeout = s.config.ScanInterval / 2
	}

	for subnet, targets := range devicesBySubnet {
		task := SubnetScanTask{
			Subnet:        subnet,
			TargetDevices: targets,
		}

		// Try to dispatch task with timeout (blocking)
		select {
		case s.tasks <- task:
			tasksDispatched++
			s.logger.Debug("Dispatched subnet scan task",
				"subnet", subnet,
				"device_count", len(targets),
			)
		case <-time.After(dispatchTimeout):
			// Timeout waiting for queue slot - workers are overloaded
			tasksSkipped++
			s.logger.Warn("Timeout waiting to dispatch subnet scan task",
				"subnet", subnet,
				"device_count", len(targets),
				"timeout", dispatchTimeout,
			)
		case <-ctx.Done():
			s.logger.Debug("Context cancelled during task dispatch",
				"tasks_dispatched", tasksDispatched,
				"tasks_skipped", tasksSkipped,
			)
			return
		}
	}

	s.logger.Debug("Offline device scan cycle completed",
		"devices_found", len(offlineDevices),
		"subnets_to_scan", len(devicesBySubnet),
		"tasks_dispatched", tasksDispatched,
		"tasks_skipped", tasksSkipped,
	)
}

// scanWorker processes scan tasks from the queue
func (s *Service) scanWorker(ctx context.Context, workerID int) {
	defer s.wg.Done()

	logger := s.logger.With("worker_id", workerID)
	logger.Debug("Starting scan worker")

	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-s.tasks:
			if !ok {
				return
			}
			result := s.processTask(ctx, task)
			s.results <- result
		}
	}
}

// processTask performs the actual IP scan for multiple devices in a subnet
func (s *Service) processTask(ctx context.Context, task SubnetScanTask) SubnetScanResult {
	ctx, cancel := context.WithTimeout(ctx, s.config.ScanTimeout)
	defer cancel()

	s.logger.Info("Processing subnet scan task",
		"subnet", task.Subnet,
		"device_count", len(task.TargetDevices),
	)

	// Scan subnet for all target devices
	matches, err := s.scanner.ScanSubnetForDevices(ctx, task.Subnet, task.TargetDevices)
	if err != nil {
		s.logger.Error("Subnet scan failed",
			"subnet", task.Subnet,
			"error", err,
		)
		return SubnetScanResult{
			Subnet: task.Subnet,
			Error:  err,
		}
	}

	s.logger.Debug("Subnet scan completed",
		"subnet", task.Subnet,
		"devices_sought", len(task.TargetDevices),
		"devices_found", len(matches),
	)

	return SubnetScanResult{
		Subnet:  task.Subnet,
		Matches: matches,
	}
}

// resultProcessor handles scan results
func (s *Service) resultProcessor(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case result, ok := <-s.results:
			if !ok {
				return
			}
			s.handleScanResult(ctx, result)
		}
	}
}

// handleScanResult processes a subnet scan result with multiple device matches
func (s *Service) handleScanResult(ctx context.Context, result SubnetScanResult) {
	if result.Error != nil {
		s.logger.Error("Subnet scan error",
			"subnet", result.Subnet,
			"error", result.Error,
		)
		return
	}

	if len(result.Matches) == 0 {
		s.logger.Debug("No devices found in subnet scan",
			"subnet", result.Subnet,
		)
		return
	}

	// Process each matched device
	for _, match := range result.Matches {
		s.logger.Info("Device found at new IP address",
			"device_identifier", match.TargetDevice.DeviceIdentifier,
			"subnet", result.Subnet,
			"new_ip", match.DiscoveredIP,
		)

		doi := discoverymodels.DeviceOrgIdentifier{
			DeviceIdentifier: match.TargetDevice.DiscoveredDeviceIdentifier,
			OrgID:            match.TargetDevice.OrgID,
		}

		discoveredDevice, err := s.discoveredDeviceStore.GetDevice(ctx, doi)
		if err != nil {
			s.logger.Error("Failed to get discovered device",
				"device_identifier", match.TargetDevice.DeviceIdentifier,
				"discovered_device_identifier", match.TargetDevice.DiscoveredDeviceIdentifier,
				"error", err,
			)
			continue
		}

		discoveredDevice.UpdateNetworkInfo(match.DiscoveredIP, match.DiscoveredPort, match.URLScheme)

		if _, err := s.discoveredDeviceStore.Save(ctx, doi, discoveredDevice); err != nil {
			s.logger.Error("Failed to save discovered device",
				"device_identifier", match.TargetDevice.DeviceIdentifier,
				"discovered_device_identifier", match.TargetDevice.DiscoveredDeviceIdentifier,
				"error", err,
			)
			continue
		}

		s.logger.Info("Successfully updated discovered device network info",
			"device_identifier", match.TargetDevice.DeviceIdentifier,
			"discovered_device_identifier", match.TargetDevice.DiscoveredDeviceIdentifier,
			"new_ip", match.DiscoveredIP,
		)
	}
}
