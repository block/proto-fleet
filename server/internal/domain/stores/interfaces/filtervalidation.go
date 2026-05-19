package interfaces

import (
	"context"
	"log/slog"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// ValidateFilterBuildings is the canonical cross-org check shared by
// the miner-list (fleetmanagement.parseFilter) and rack-list
// (collection.Service.ListCollectionsDomain) filter paths. It rejects
// requests that reference buildings outside the caller's org via the
// bulk BuildingsByIDs lookup and emits a structured audit log on
// rejection so security monitoring sees probes from either surface.
//
// Wildcard ZoneKey entries (BuildingID == 0) are intentionally
// skipped — they carry no specific building to validate, and the SQL
// builder's `org_id` predicate is the single-layer defense for that
// path.
//
// Error message is generic so the rejected building IDs cannot be
// enumerated by probing.
func ValidateFilterBuildings(
	ctx context.Context,
	orgID int64,
	buildingIDs []int64,
	zoneKeys []ZoneKey,
	store BuildingStore,
) error {
	requested := make(map[int64]struct{})
	for _, id := range buildingIDs {
		if id <= 0 {
			return fleeterror.NewInvalidArgumentErrorf("building_ids must contain only positive IDs")
		}
		requested[id] = struct{}{}
	}
	for _, zk := range zoneKeys {
		if zk.BuildingID < 0 {
			return fleeterror.NewInvalidArgumentErrorf("zone_keys.building_id must be non-negative")
		}
		if zk.Zone == "" {
			return fleeterror.NewInvalidArgumentErrorf("zone_keys.zone must be non-empty")
		}
		if zk.BuildingID > 0 {
			requested[zk.BuildingID] = struct{}{}
		}
	}
	if len(requested) == 0 {
		return nil
	}
	if store == nil {
		// Defensive: a nil store at runtime would let cross-org IDs
		// through silently. Treat as a server misconfiguration.
		return fleeterror.NewInternalErrorf("ValidateFilterBuildings: buildingStore is required for building_ids/zone_keys validation")
	}

	ids := make([]int64, 0, len(requested))
	for id := range requested {
		ids = append(ids, id)
	}
	found, err := store.BuildingsByIDs(ctx, orgID, ids)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to validate building ownership: %v", err)
	}
	if len(found) < len(requested) {
		// Audit signal for security monitoring. Do not include the
		// rejected IDs themselves — they may reference another org's
		// internal identifiers.
		slog.WarnContext(ctx, "cross_org_filter_probe",
			"org_id", orgID,
			"rejected_count", len(requested)-len(found),
		)
		return fleeterror.NewInvalidArgumentError(
			"one or more building_ids reference buildings outside the caller's org")
	}
	return nil
}
