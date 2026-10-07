package fleetinventory

import (
	"context"
	"database/sql"
	"fmt"

	buildingpb "github.com/block/proto-fleet/server/generated/grpc/buildings/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	rackpb "github.com/block/proto-fleet/server/generated/grpc/device_set/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/containers"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *Service) enrich(ctx context.Context, orgID int64, rows []listRow, items []*pb.InventoryItem, scopes []Scope) error {
	miners := []string{}
	controllers := []string{}
	byDevice := map[string]*pb.InventoryItem{}
	for i, r := range rows {
		item := items[i]
		item.SiteName = r.SiteName
		item.FacilityName = r.FacilityName
		item.GroupName = r.GroupName
		if r.TelemetryAllowed {
			item.ListStats = r.stats()
		}
		placement := &commonpb.PlacementRefs{}
		if r.SiteID != nil {
			placement.Site = &commonpb.ResourceRef{Id: *r.SiteID, Label: r.SiteName}
		}
		if r.FacilityKind == 1 && r.FacilityID != nil {
			placement.Building = &commonpb.ResourceRef{Id: *r.FacilityID, Label: r.FacilityName}
		}
		switch item.Ref.Kind {
		case pb.EntityKind_ENTITY_KIND_BUILDING:
			b := &buildingpb.Building{}
			if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(r.BuildingDetails, b); err != nil {
				return fmt.Errorf("decode inventory building: %w", err)
			}
			b.Placement = placement
			item.Details = &pb.InventoryItem_Building{Building: &buildingpb.BuildingWithCounts{Building: b, RackCount: r.RackCount, DeviceCount: r.AssetCount, ListStats: item.ListStats}}
		case pb.EntityKind_ENTITY_KIND_RACK:
			rack := &rackpb.DeviceSet{}
			if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(r.RackDetails, rack); err != nil {
				return fmt.Errorf("decode inventory rack: %w", err)
			}
			rack.Placement = placement
			item.Details = &pb.InventoryItem_Rack{Rack: rack}
		case pb.EntityKind_ENTITY_KIND_MINER:
			miners = append(miners, r.DeviceIdentifier)
			byDevice[r.DeviceIdentifier] = item
		case pb.EntityKind_ENTITY_KIND_CONTAINER:
			controllers = append(controllers, r.DeviceIdentifier)
			byDevice[r.DeviceIdentifier] = item
		case pb.EntityKind_ENTITY_KIND_TANK, pb.EntityKind_ENTITY_KIND_MODULE, pb.EntityKind_ENTITY_KIND_UNSPECIFIED:
			// Observed children use the inventory fields directly.
		}
	}
	if len(miners) > 0 && s.miners != nil {
		snapshots, err := s.miners.InventorySnapshots(ctx, orgID, miners, func(site *int64) bool {
			v := sql.NullInt64{}
			if site != nil {
				v = sql.NullInt64{Int64: *site, Valid: true}
			}
			return allows(scopes, v)
		})
		if err != nil {
			return err
		}
		for _, snapshot := range snapshots {
			if item := byDevice[snapshot.DeviceIdentifier]; item != nil {
				item.Details = &pb.InventoryItem_Miner{Miner: snapshot}
			}
		}
	}
	if len(controllers) > 0 {
		rows, err := s.store.ListContainers(ctx, sqlc.ListContainersParams{OrgID: orgID, DeviceIdentifiers: controllers, SiteIds: []int64{}})
		if err != nil {
			return err
		}
		cards, err := containers.Project(rows, func(site *int64) (bool, error) {
			v := sql.NullInt64{}
			if site != nil {
				v = sql.NullInt64{Int64: *site, Valid: true}
			}
			return allows(scopes, v), nil
		})
		if err != nil {
			return err
		}
		for _, card := range cards {
			if item := byDevice[card.DeviceIdentifier]; item != nil {
				item.Details = &pb.InventoryItem_Container{Container: card}
			}
		}
	}
	return nil
}

func (r listRow) stats() *commonpb.FleetListStats {
	// Fleet list counts already use int32. Saturate rather than wrapping at the
	// wire boundary if an unusually large inventory exceeds that representation.
	count := func(n int64) int32 { return int32(max(0, min(n, int64(2147483647)))) } //nolint:gosec // Explicitly clamped to the nonnegative int32 range.
	stats := &commonpb.FleetListStats{RackCount: count(r.RackCount), DeviceCount: count(r.AssetCount),
		HashingCount: r.HashingCount, SleepingCount: r.SleepingCount, OfflineCount: r.OfflineCount, BrokenCount: r.BrokenCount,
		PsuIssueCount: r.PSUIssues, FanIssueCount: r.FanIssues, HashBoardIssueCount: r.HashboardIssues, ControlBoardIssueCount: r.ControlIssues}
	if r.HashrateTHS != nil {
		stats.TotalHashrateThs = *r.HashrateTHS
		stats.HashrateReportingCount = count(r.HashrateReportingCount)
	}
	if r.PowerKW != nil {
		stats.TotalPowerKw = *r.PowerKW
		stats.PowerReportingCount = count(r.PowerReportingCount)
	}
	if r.EfficiencyJTH != nil {
		stats.AvgEfficiencyJth = *r.EfficiencyJTH
		stats.EfficiencyReportingCount = count(r.AssetCount)
		if pb.EntityKind(r.Kind) == pb.EntityKind_ENTITY_KIND_TANK {
			stats.EfficiencyReportingCount = count(r.EfficiencyReportingCount)
		}
	}
	if r.MinTemperatureC != nil && r.MaxTemperatureC != nil {
		stats.MinTemperatureC = *r.MinTemperatureC
		stats.MaxTemperatureC = *r.MaxTemperatureC
		stats.TemperatureReportingCount = count(r.AssetCount)
		if pb.EntityKind(r.Kind) == pb.EntityKind_ENTITY_KIND_TANK {
			stats.TemperatureReportingCount = count(r.TemperatureReportingCount)
		}
	}
	return stats
}
