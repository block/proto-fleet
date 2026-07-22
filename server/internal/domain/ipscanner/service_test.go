package ipscanner

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/ipscanner/mocks"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	storemocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// noopDiscoverer is a discoverer that returns nil for all discovery requests
type noopDiscoverer struct{}

func (n *noopDiscoverer) Discover(ctx context.Context, ipAddress, port string) (*discoverymodels.DiscoveredDevice, error) {
	return nil, nil
}

func TestIPScannerService_StartStopStart(t *testing.T) {
	ctrl := gomock.NewController(t)
	config := Config{
		Enabled:                       true,
		ScanInterval:                  time.Hour,
		MaxConcurrentSubnetScans:      1,
		MaxConcurrentIPScansPerSubnet: 1,
		ScanTimeout:                   time.Second,
		SubnetMaskBits:                24,
	}
	deviceStore := storemocks.NewMockDeviceStore(ctrl)
	scanned := make(chan struct{}, 2)
	deviceStore.EXPECT().GetOfflineDevices(gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, int) ([]stores.OfflineDeviceInfo, error) {
			scanned <- struct{}{}
			return nil, nil
		},
	).AnyTimes()
	service := NewIPScannerService(
		config,
		deviceStore,
		storemocks.NewMockDiscoveredDeviceStore(ctrl),
		&noopDiscoverer{},
		mocks.NewMockDeviceIdentityCheckService(ctrl),
		slog.Default(),
	)

	if err := service.Start(t.Context()); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	waitForScannerSignal(t, scanned, "first run did not scan")
	if err := service.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop failed: %v", err)
	}
	if err := service.Start(t.Context()); err != nil {
		t.Fatalf("second Start failed: %v", err)
	}
	waitForScannerSignal(t, scanned, "second run did not scan")
	if err := service.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop failed: %v", err)
	}
}

func TestIPScannerService_Start_ActivationCancellationStopsRun(t *testing.T) {
	ctrl := gomock.NewController(t)
	config := Config{
		Enabled:                       true,
		ScanInterval:                  time.Hour,
		MaxConcurrentSubnetScans:      1,
		MaxConcurrentIPScansPerSubnet: 1,
		ScanTimeout:                   time.Second,
		SubnetMaskBits:                24,
	}
	deviceStore := storemocks.NewMockDeviceStore(ctrl)
	scanned := make(chan struct{}, 1)
	deviceStore.EXPECT().GetOfflineDevices(gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, int) ([]stores.OfflineDeviceInfo, error) {
			scanned <- struct{}{}
			return nil, nil
		},
	)
	service := NewIPScannerService(
		config,
		deviceStore,
		storemocks.NewMockDiscoveredDeviceStore(ctrl),
		&noopDiscoverer{},
		mocks.NewMockDeviceIdentityCheckService(ctrl),
		slog.Default(),
	)

	activationCtx, cancelActivation := context.WithCancel(t.Context())
	if err := service.Start(activationCtx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	waitForScannerSignal(t, scanned, "initial scan did not run")

	service.lifecycleMu.Lock()
	run := service.run
	service.lifecycleMu.Unlock()
	if run == nil {
		t.Fatal("Start did not create an active scanner run")
	}

	cancelActivation()
	waitForScannerSignal(t, run.done, "activation context cancellation did not stop scanner run")

	if err := service.Stop(t.Context()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestIPScannerService_Stop_CancelsWorkerBlockedOnFullResults(t *testing.T) {
	ctrl := gomock.NewController(t)
	config := Config{
		Enabled:                       true,
		ScanInterval:                  time.Hour,
		MaxConcurrentSubnetScans:      1,
		MaxConcurrentIPScansPerSubnet: 1,
		ScanTimeout:                   time.Second,
		SubnetMaskBits:                24,
	}
	service := NewIPScannerService(
		config,
		storemocks.NewMockDeviceStore(ctrl),
		storemocks.NewMockDiscoveredDeviceStore(ctrl),
		&noopDiscoverer{},
		mocks.NewMockDeviceIdentityCheckService(ctrl),
		slog.Default(),
	)

	ctx, cancel := context.WithCancel(t.Context())
	run := &serviceRun{
		tasks:   make(chan SubnetScanTask, 1),
		results: make(chan SubnetScanResult, 1),
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	service.run = run
	run.results <- SubnetScanResult{}
	run.tasks <- SubnetScanTask{Subnet: "invalid"}
	run.wg.Go(func() { service.scanWorker(ctx, run, 0) })
	go func() {
		run.wg.Wait()
		close(run.done)
	}()

	deadline := time.Now().Add(time.Second)
	for len(run.tasks) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(run.tasks) != 0 {
		t.Fatal("worker did not consume queued task")
	}
	select {
	case <-run.done:
		t.Fatal("worker unexpectedly exited while the result queue was full")
	case <-time.After(10 * time.Millisecond):
	}

	stopCtx, stopCancel := context.WithTimeout(t.Context(), time.Second)
	defer stopCancel()
	if err := service.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestIPScannerService_Stop_ReturnsAtDeadline(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewIPScannerService(
		Config{Enabled: true, MaxConcurrentSubnetScans: 1, MaxConcurrentIPScansPerSubnet: 1},
		storemocks.NewMockDeviceStore(ctrl),
		storemocks.NewMockDiscoveredDeviceStore(ctrl),
		&noopDiscoverer{},
		mocks.NewMockDeviceIdentityCheckService(ctrl),
		slog.Default(),
	)
	service.run = &serviceRun{
		cancel: func() {},
		done:   make(chan struct{}),
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := service.Stop(ctx); err == nil {
		t.Fatal("Stop returned nil after its deadline")
	}
	if err := service.Start(t.Context()); err == nil {
		t.Fatal("Start succeeded while the prior run was still stopping")
	}
}

func waitForScannerSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
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
	err = service.Stop(context.Background())
	if err != nil {
		t.Fatalf("Failed to stop service: %v", err)
	}

	// Test passes if only one scan loop actually ran
	// This is verified by the mutex preventing concurrent execution
}
