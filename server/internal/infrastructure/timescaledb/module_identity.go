package timescaledb

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// The organization lock serializes identity resolution across controllers.
// Unresolved observations retain their independently identified boards, but
// cannot supply a hardware identity, module measurements, or command targets.
func resolveContainerModuleIdentities(ctx context.Context, q *sqlc.Queries, orgID int64, identifier string, snapshot *sdk.ContainerSnapshot) error {
	counts := map[string]int{}
	ids := []string{}
	for _, tank := range snapshot.Tanks {
		for _, module := range tank.Modules {
			id := strings.TrimSpace(module.ReportedID)
			if id != "" && sdk.SafeHashboardIdentity(module.ReportedID) {
				counts[id]++
				ids = append(ids, id)
			}
		}
	}
	known, err := q.GetContainerModuleIdentities(ctx, sqlc.GetContainerModuleIdentitiesParams{OrgID: orgID, ReportedModuleIds: ids})
	if err != nil {
		return err
	}
	byID := map[string]sqlc.GetContainerModuleIdentitiesRow{}
	for _, identity := range known {
		byID[identity.ReportedModuleID] = identity
	}
	withdraw := []int64{}
	for ti := range snapshot.Tanks {
		tank := &snapshot.Tanks[ti]
		for mi := range tank.Modules {
			module := &tank.Modules[mi]
			id := strings.TrimSpace(module.ReportedID)
			old, exists := byID[id]
			reason := ""
			switch {
			// A rejected claim can withdraw evidence only if it is not older than
			// the durable module, even when this controller's inventory is newer.
			case exists && (old.LastSeenAt.Time.After(snapshot.ObservedAt) || old.RetiredAt.Time.After(snapshot.ObservedAt) || old.PlacementStartedAt.Time.After(snapshot.ObservedAt)):
				reason = "older_psu_observation"
			case id == "":
				reason = "missing_or_conflicting_psu"
			case !sdk.SafeHashboardIdentity(module.ReportedID) || id != module.ReportedID:
				reason = "invalid_psu"
			case counts[id] > 1:
				reason = "duplicate_psu"
			case exists && !old.ModuleID.Valid:
				reason = "conflicting_legacy_psu"
			case exists && old.DeviceIdentifier.String != identifier && !old.RetiredAt.Valid && snapshot.ObservedAt.Sub(old.LastSeenAt.Time) < sdk.ContainerReadingMaxAge:
				reason = "conflicting_controller_psu"
			}
			if reason == "" {
				if exists {
					var boundary time.Time
					if old.HasPriorPlacement {
						boundary = old.PlacementStartedAt.Time
					}
					if old.RetiredAt.Valid || old.DeviceIdentifier.String != identifier || old.TankPosition.Int32 != tank.Position || old.Position.Int32 != module.Position || old.PlacementSiteID != old.SiteID {
						boundary = snapshot.ObservedAt
					}
					if !boundary.IsZero() {
						applyModuleAttributionBoundary(module, boundary)
					}
				}
				continue
			}
			if old.ModuleID.Valid && reason != "older_psu_observation" {
				withdraw = append(withdraw, old.ModuleID.Int64)
			}
			slog.WarnContext(ctx, "module identity unresolved", "controller", identifier, "tank", tank.Position, "module", module.Position, "reported_module_id", module.ReportedID, "reason", reason)
			module.ReportedID = ""
			module.IdentityConfirmed = false
			module.State = sdk.MinerStatusUnknown
			module.Metrics = nil
		}
	}
	return q.WithdrawContainerModuleIdentities(ctx, withdraw)
}

func applyModuleAttributionBoundary(module *sdk.ContainerModule, boundary time.Time) {
	if module.ObservedAt.Before(boundary) {
		module.State, module.ObservedAt = sdk.MinerStatusUnknown, time.Time{}
	}
	// Diagnostic persistence filters each issue source against attribution_since,
	// retaining fresh evidence when a report also contains pre-placement sources.
	metrics := module.Metrics
	if metrics == nil {
		return
	}
	if r := metrics.Rollup; r != nil {
		if r.SourceTimes[r.PowerSource].Before(boundary) {
			r.PowerW, r.EfficiencyJH = nil, nil
			r.Power.Reporting = 0
			r.Power.OldestAt, r.Power.NewestAt = time.Time{}, time.Time{}
			metrics.PowerW, metrics.EfficiencyJH = nil, nil
		}
	} else if metrics.ObservedAt.Before(boundary) {
		module.Metrics = nil
	}
}
