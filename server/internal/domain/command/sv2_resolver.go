package command

import (
	"context"
	"log/slog"

	"github.com/block/proto-fleet/server/internal/domain/pools/rewriter"
	tmodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

// telemetryMetricsBatchGetter is the subset of telemetry.TelemetryDataStore the
// resolver needs. Defining it here lets main.go reuse the timescaledb store
// implementation without command depending on the full telemetry interface.
type telemetryMetricsBatchGetter interface {
	GetLatestDeviceMetricsBatch(
		ctx context.Context,
		deviceIDs []tmodels.DeviceIdentifier,
	) (map[tmodels.DeviceIdentifier]modelsV2.DeviceMetrics, error)
}

// NewTelemetrySV2Resolver constructs an SV2CapabilityResolver backed by the
// latest telemetry scrape's StratumV2Support per device. Plugins probe the
// firmware on every cycle and set the field; the resolver pulls it for each
// requested device and merges via rewriter.MergeCapabilities so an
// Unknown/Unspecified result correctly falls back to the SV1 path.
//
// Telemetry-only is the intended v1 sourcing because every plugin in the
// rollout writes the field explicitly. Static-driver-capability merging is a
// later concern that arrives only when a plugin can't probe (none today).
func NewTelemetrySV2Resolver(store telemetryMetricsBatchGetter) SV2CapabilityResolver {
	return &telemetrySV2Resolver{store: store}
}

type telemetrySV2Resolver struct {
	store telemetryMetricsBatchGetter
}

func (r *telemetrySV2Resolver) ResolveCapabilities(
	ctx context.Context,
	deviceIdentifiers []string,
) map[string]rewriter.DeviceCapabilities {
	if len(deviceIdentifiers) == 0 {
		return nil
	}
	ids := make([]tmodels.DeviceIdentifier, len(deviceIdentifiers))
	for i, id := range deviceIdentifiers {
		ids[i] = tmodels.DeviceIdentifier(id)
	}
	batch, err := r.store.GetLatestDeviceMetricsBatch(ctx, ids)
	if err != nil {
		// Failing closed (return empty caps → SV1-only) is safe: it
		// degrades to the proxy path for SV2 pools, never to direct
		// dispatch of an SV2 URL at an SV1-only firmware.
		slog.Warn("sv2 capability resolver: telemetry lookup failed; treating fleet as SV1-only", "error", err)
		return nil
	}
	out := make(map[string]rewriter.DeviceCapabilities, len(deviceIdentifiers))
	for _, id := range deviceIdentifiers {
		sv2 := modelsV2.StratumV2SupportUnknown
		if m, ok := batch[tmodels.DeviceIdentifier(id)]; ok {
			sv2 = m.StratumV2Support
		}
		out[id] = rewriter.MergeCapabilities(nil, nil, sv2)
	}
	return out
}
