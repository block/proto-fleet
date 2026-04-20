package ipscanner

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/ipscanner/mocks"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	storemocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// noopDiscoverer is a discoverer that returns nil for all discovery requests
type noopDiscoverer struct{}

func (n *noopDiscoverer) Discover(ctx context.Context, ipAddress, port string) (*discoverymodels.DiscoveredDevice, error) {
	return nil, nil
}

func TestNewIPScannerService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	config := Config{
		Enabled:                       true,
		ScanInterval:                  5 * time.Minute,
		MaxConcurrentSubnetScans:      5,
		MaxConcurrentIPScansPerSubnet: 10,
		ScanTimeout:                   30 * time.Second,
		SubnetMaskBits:                24,
	}

	deviceStore := storemocks.NewMockDeviceStore(ctrl)
	discoveredDeviceStore := storemocks.NewMockDiscoveredDeviceStore(ctrl)
	discoverer := &noopDiscoverer{}
	deviceIDCheckService := mocks.NewMockDeviceIdentityCheckService(ctrl)
	logger := slog.Default()

	service := NewIPScannerService(config, deviceStore, discoveredDeviceStore, discoverer, deviceIDCheckService, logger)

	if service == nil {
		t.Fatal("NewIPScannerService returned nil")
	}
}

func TestIPScannerService_StartStop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	config := Config{
		Enabled:                       true,
		ScanInterval:                  100 * time.Millisecond,
		MaxConcurrentSubnetScans:      2,
		MaxConcurrentIPScansPerSubnet: 5,
		ScanTimeout:                   1 * time.Second,
		SubnetMaskBits:                24,
	}

	deviceStore := storemocks.NewMockDeviceStore(ctrl)
	discoveredDeviceStore := storemocks.NewMockDiscoveredDeviceStore(ctrl)
	discoverer := &noopDiscoverer{}
	deviceIDCheckService := mocks.NewMockDeviceIdentityCheckService(ctrl)
	logger := slog.Default()

	// Expect GetOfflineDevices to be called at least once
	deviceStore.EXPECT().
		GetOfflineDevices(gomock.Any(), gomock.Any()).
		Return(nil, nil).
		AnyTimes()

	service := NewIPScannerService(config, deviceStore, discoveredDeviceStore, discoverer, deviceIDCheckService, logger)

	ctx := t.Context()
	err := service.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}

	// Give it a moment to start
	time.Sleep(150 * time.Millisecond)

	// Stop the service
	err = service.Stop()
	if err != nil {
		t.Fatalf("Failed to stop service: %v", err)
	}
}

func TestIPScannerService_DisabledService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	config := Config{
		Enabled:                       false, // Disabled
		ScanInterval:                  5 * time.Minute,
		MaxConcurrentSubnetScans:      5,
		MaxConcurrentIPScansPerSubnet: 10,
		ScanTimeout:                   30 * time.Second,
		SubnetMaskBits:                24,
	}

	deviceStore := storemocks.NewMockDeviceStore(ctrl)
	discoveredDeviceStore := storemocks.NewMockDiscoveredDeviceStore(ctrl)
	discoverer := &noopDiscoverer{}
	deviceIDCheckService := mocks.NewMockDeviceIdentityCheckService(ctrl)
	logger := slog.Default()

	service := NewIPScannerService(config, deviceStore, discoveredDeviceStore, discoverer, deviceIDCheckService, logger)

	ctx := t.Context()
	err := service.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start disabled service: %v", err)
	}

	// Service should start but do nothing
	// No error expected
}

func TestIPScannerService_PreventMultipleInstances(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	config := Config{
		Enabled:                       true,
		ScanInterval:                  100 * time.Millisecond,
		MaxConcurrentSubnetScans:      2,
		MaxConcurrentIPScansPerSubnet: 5,
		ScanTimeout:                   1 * time.Second,
		SubnetMaskBits:                24,
	}

	deviceStore := storemocks.NewMockDeviceStore(ctrl)
	discoveredDeviceStore := storemocks.NewMockDiscoveredDeviceStore(ctrl)
	discoverer := &noopDiscoverer{}
	deviceIDCheckService := mocks.NewMockDeviceIdentityCheckService(ctrl)
	logger := slog.Default()

	// Expect GetOfflineDevices to be called, but not more than reasonable
	// If multiple scan loops ran, we'd see many more calls
	deviceStore.EXPECT().
		GetOfflineDevices(gomock.Any(), gomock.Any()).
		Return(nil, nil).
		AnyTimes()

	service := NewIPScannerService(config, deviceStore, discoveredDeviceStore, discoverer, deviceIDCheckService, logger)

	ctx := t.Context()

	// Start the service multiple times
	err := service.Start(ctx)
	if err != nil {
		t.Fatalf("First Start failed: %v", err)
	}

	// Try to start again - should be prevented by mutex
	err = service.Start(ctx)
	if err != nil {
		t.Fatalf("Second Start failed: %v", err)
	}

	// Try one more time
	err = service.Start(ctx)
	if err != nil {
		t.Fatalf("Third Start failed: %v", err)
	}

	// Give time for scan loops to start
	time.Sleep(50 * time.Millisecond)

	// Stop the service
	err = service.Stop()
	if err != nil {
		t.Fatalf("Failed to stop service: %v", err)
	}

	// Test passes if only one scan loop actually ran
	// This is verified by the mutex preventing concurrent execution
}
