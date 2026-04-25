package main

import (
	"context"
	"log/slog"

	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// staticSV2CapsProvider answers "what static capabilities does this
// device's plugin claim?" so the SV2 resolver can fall back to the
// driver's view when telemetry hasn't reported yet (or has reported
// Unknown). Without this fallback, freshly-paired native-SV2 miners
// would be misclassified as SV1-only for the entire window before their
// first telemetry scrape lands.
type staticSV2CapsProvider struct {
	devices interfaces.DeviceStore
	plugins *plugins.Service
}

func newStaticSV2CapsProvider(devices interfaces.DeviceStore, pluginsService *plugins.Service) *staticSV2CapsProvider {
	return &staticSV2CapsProvider{devices: devices, plugins: pluginsService}
}

// StaticCapabilitiesForDevice returns the plugin's declared capability
// set for the device's driver. Returns nil when the device row, the
// driver name, or the loaded plugin is unavailable — the resolver
// treats nil as "no static signal," which leaves the SV2 bit driven
// by telemetry. Per-model overrides are intentionally omitted in v1:
// the only capability the rewriter consults today is
// CapabilityStratumV2Native, which is a property of the firmware
// stratum client (not of a specific board model), so the base
// driver-level value is sufficient.
func (p *staticSV2CapsProvider) StaticCapabilitiesForDevice(ctx context.Context, orgID int64, deviceIdentifier string) sdk.Capabilities {
	if p == nil || p.devices == nil || p.plugins == nil {
		return nil
	}
	device, err := p.devices.GetDeviceByDeviceIdentifier(ctx, deviceIdentifier, orgID)
	if err != nil || device == nil {
		// Best-effort: a missing/unauthorised device just means we have
		// no static signal to contribute.
		slog.Debug("static sv2 caps: device lookup failed", "device_identifier", deviceIdentifier, "error", err)
		return nil
	}
	caps, err := p.plugins.GetPluginCapabilitiesByDriverName(device.GetDriverName())
	if err != nil {
		slog.Debug("static sv2 caps: plugin lookup failed", "driver_name", device.GetDriverName(), "error", err)
		return nil
	}
	return caps
}
