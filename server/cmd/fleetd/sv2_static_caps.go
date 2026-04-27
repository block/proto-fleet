package main

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// staticSV2CapsProvider answers "what static capabilities does this
// device's plugin claim?" so the SV2 resolver can fall back to the
// driver's view when telemetry hasn't reported yet (or has reported
// Unknown). Without this fallback, freshly-paired native-SV2 miners
// would be misclassified as SV1-only for the entire window before their
// first telemetry scrape lands.
//
// Implementation note: the resolver calls this once per
// preview/commit, so the lookup is batched into a single SQL query
// over (identifier → driver_name) and a per-driver capability map
// derived from plugins.Service. That keeps wide selections from
// turning into an N+1 storm of GetDeviceByDeviceIdentifier calls.
type staticSV2CapsProvider struct {
	conn    *sql.DB
	plugins *plugins.Service
}

func newStaticSV2CapsProvider(conn *sql.DB, pluginsService *plugins.Service) *staticSV2CapsProvider {
	return &staticSV2CapsProvider{conn: conn, plugins: pluginsService}
}

// StaticCapabilitiesForDevices returns the plugin's declared
// capability set for each device identifier, keyed by identifier.
// Devices missing from the result map have no static signal (unknown
// driver, plugin not loaded, or no row in the org); the resolver
// falls back to telemetry alone for those entries.
//
// Per-model overrides are intentionally omitted in v1: the only
// capability the rewriter consults today is CapabilityStratumV2Native,
// which is a property of the firmware stratum client (not of a
// specific board model), so the base driver-level value is sufficient.
func (p *staticSV2CapsProvider) StaticCapabilitiesForDevices(ctx context.Context, orgID int64, deviceIdentifiers []string) map[string]sdk.Capabilities {
	if p == nil || p.conn == nil || p.plugins == nil || len(deviceIdentifiers) == 0 {
		return nil
	}
	rows, err := db.WithTransaction(ctx, p.conn, func(q *sqlc.Queries) ([]sqlc.GetDriverNamesByDeviceIdentifiersForOrgRow, error) {
		return q.GetDriverNamesByDeviceIdentifiersForOrg(ctx, sqlc.GetDriverNamesByDeviceIdentifiersForOrgParams{
			DeviceIdentifiers: deviceIdentifiers,
			OrgID:             orgID,
		})
	})
	if err != nil {
		slog.Debug("static sv2 caps: batched driver lookup failed", "error", err)
		return nil
	}
	// Cache plugin caps per driver name across the batch — most fleets
	// run a small number of distinct drivers, so this collapses a
	// O(devices) plugin lookup into O(drivers).
	capsByDriver := make(map[string]sdk.Capabilities)
	out := make(map[string]sdk.Capabilities, len(rows))
	for _, row := range rows {
		caps, ok := capsByDriver[row.DriverName]
		if !ok {
			c, err := p.plugins.GetPluginCapabilitiesByDriverName(row.DriverName)
			if err != nil {
				// No plugin for this driver — not necessarily fatal
				// (e.g. plugin disabled at runtime), but means we
				// can't contribute static caps for those devices.
				slog.Debug("static sv2 caps: plugin not loaded for driver", "driver_name", row.DriverName)
				capsByDriver[row.DriverName] = nil
				continue
			}
			caps = c
			capsByDriver[row.DriverName] = c
		}
		if caps != nil {
			out[row.DeviceIdentifier] = caps
		}
	}
	return out
}
