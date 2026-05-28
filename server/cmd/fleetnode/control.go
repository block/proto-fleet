package main

import (
	"context"
	"fmt"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
)

type discoverer interface {
	Probe(ctx context.Context, ipAddress, port string) (*pb.DiscoveredDeviceReport, error)
	DefaultDiscoveryPorts(ctx context.Context) []string
}

type pluginDiscoverer struct {
	multi *plugins.MultiTypeDiscoverer
	svc   *plugins.Service
}

func newPluginDiscoverer(parent context.Context, pluginsDir string) (*pluginDiscoverer, func(), error) {
	// Manager.Shutdown waits the full grace period even when a plugin already
	// exited, so keep it tight; a stuck plugin still gets killed.
	manager := plugins.NewManager(&plugins.Config{
		Enabled:                    true,
		PluginsDir:                 pluginsDir,
		MaxStartupTimeSeconds:      30,
		ShutdownTimeoutSeconds:     10,
		ShutdownGracePeriodSeconds: 2,
		LogLevel:                   "info",
	})
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	if err := manager.LoadPlugins(ctx); err != nil {
		// LoadPlugins can leave partial subprocesses on error; reap them so
		// the agent doesn't exit with orphans behind it.
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = manager.Shutdown(shutdownCtx)
		shutdownCancel()
		return nil, func() {}, fmt.Errorf("load plugins: %w", err)
	}
	// Parent ctx is typically already cancelled by a signal when cleanup
	// runs; use a fresh background ctx bounded by the same 10s budget.
	cleanup := func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = manager.Shutdown(shutdownCtx)
	}
	return &pluginDiscoverer{
		multi: plugins.NewMultiTypeDiscoverer(manager),
		svc:   plugins.NewService(manager),
	}, cleanup, nil
}

func (p *pluginDiscoverer) Probe(ctx context.Context, ipAddress, port string) (*pb.DiscoveredDeviceReport, error) {
	dev, err := p.multi.Discover(ctx, ipAddress, port)
	if err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, nil
	}
	return reportFromDiscovered(dev), nil
}

func (p *pluginDiscoverer) DefaultDiscoveryPorts(ctx context.Context) []string {
	return p.svc.GetDefaultDiscoveryPorts(ctx)
}

// SDK drivers often leave DeviceIdentifier empty; the agent has no DB so it
// synthesizes auto:* and lets the server reconcile by (fleet_node, ip, port).
func reportFromDiscovered(dev *discoverymodels.DiscoveredDevice) *pb.DiscoveredDeviceReport {
	deviceID := dev.GetDeviceIdentifier()
	if deviceID == "" {
		deviceID = synthesizeIdentifier(dev.GetMacAddress(), dev.GetSerialNumber())
	}
	return &pb.DiscoveredDeviceReport{
		DeviceIdentifier: deviceID,
		IpAddress:        dev.GetIpAddress(),
		Port:             dev.GetPort(),
		UrlScheme:        dev.GetUrlScheme(),
		DriverName:       dev.GetDriverName(),
		Model:            dev.GetModel(),
		Manufacturer:     dev.GetManufacturer(),
		FirmwareVersion:  dev.GetFirmwareVersion(),
	}
}

func synthesizeIdentifier(mac, serial string) string {
	if mac != "" {
		return "mac:" + mac
	}
	if serial != "" {
		return "serial:" + serial
	}
	return "auto:" + id.GenerateID()
}
