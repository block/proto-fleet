package command

import (
	"context"
	"log/slog"

	"github.com/block/proto-fleet/server/internal/domain/pools/rewriter"
	sdk "github.com/block/proto-fleet/server/sdk/v1"

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

// staticCapabilitiesProvider returns the static capability view for
// each device identifier in a single batched call. Implementations
// must do at most O(1) DB lookups for the input set so a wide preview
// or commit doesn't turn into an N+1 latency spike. Devices missing
// from the returned map are treated as "no static signal" — the
// resolver falls back to telemetry alone for those entries.
type staticCapabilitiesProvider interface {
	StaticCapabilitiesForDevices(ctx context.Context, orgID int64, deviceIdentifiers []string) map[string]sdk.Capabilities
}

// NewTelemetrySV2Resolver constructs an SV2CapabilityResolver that
// merges three layers, in order of increasing precedence:
//
//  1. Static driver capabilities (from the plugin's DescribeDriver),
//     plus any per-model overrides — this is the day-1 view available
//     before any telemetry scrape has landed and the only signal a
//     plugin without a live SV2 probe ever provides.
//  2. Telemetry-reported StratumV2Support — what the firmware actually
//     said in the most recent scrape; overrides the static layer when
//     the value is Supported or Unsupported.
//
// Telemetry-only sourcing was the v1 approach but misclassified
// SV2-native miners as SV1-only during the window before their first
// scrape and on transient telemetry-store failures. Static caps now
// preserve the driver/model view in those cases.
func NewTelemetrySV2Resolver(store telemetryMetricsBatchGetter, statics staticCapabilitiesProvider) SV2CapabilityResolver {
	return &telemetrySV2Resolver{store: store, statics: statics}
}

type telemetrySV2Resolver struct {
	store   telemetryMetricsBatchGetter
	statics staticCapabilitiesProvider
}

func (r *telemetrySV2Resolver) ResolveCapabilities(
	ctx context.Context,
	orgID int64,
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
		// Telemetry batch failure: don't strip the static-capability
		// layer. Build the result from static caps alone with telemetry
		// reported as Unknown so MergeCapabilities leaves the SV2 bit
		// driven by static. Without this, a transient telemetry outage
		// would silently demote every native-SV2 miner to SV1-only and
		// route them through the proxy.
		slog.Warn("sv2 capability resolver: telemetry lookup failed; falling back to static caps", "error", err)
		batch = nil
	}
	var staticByDevice map[string]sdk.Capabilities
	if r.statics != nil {
		staticByDevice = r.statics.StaticCapabilitiesForDevices(ctx, orgID, deviceIdentifiers)
	}
	out := make(map[string]rewriter.DeviceCapabilities, len(deviceIdentifiers))
	for _, id := range deviceIdentifiers {
		sv2 := modelsV2.StratumV2SupportUnknown
		if m, ok := batch[tmodels.DeviceIdentifier(id)]; ok {
			sv2 = m.StratumV2Support
		}
		out[id] = rewriter.MergeCapabilities(staticByDevice[id], nil, sv2)
	}
	return out
}
