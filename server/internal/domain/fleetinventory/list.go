package fleetinventory

import (
	"context"
	"encoding/json"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Request validation and cursor handling are shared; each view owns its SQL.
type listQueryParams struct {
	OrgID             int64
	SpecialFilters    json.RawMessage
	Authority         json.RawMessage
	Kinds             []int32
	SortField         int32
	SiteIds           []int64
	IncludeUnassigned bool
	SearchQuery       string
	Ancestors         json.RawMessage
	Ranges            json.RawMessage
	Descending        bool
	HasCursor         bool
	CursorNull        bool
	CursorNumber      float64
	CursorText        string
	CursorKind        int32
	CursorID          int64
	PageLimit         int32
}

type listResult struct {
	Items      json.RawMessage
	TotalCount int64
	MinerCount int64
}

func (s *Service) list(ctx context.Context, view pb.InventoryView, p listQueryParams) (listResult, error) {
	switch view {
	case pb.InventoryView_INVENTORY_VIEW_FACILITIES:
		result, err := s.store.ListFleetFacilities(ctx, sqlc.ListFleetFacilitiesParams{
			OrgID:             p.OrgID,
			SpecialFilters:    p.SpecialFilters,
			Authority:         p.Authority,
			Kinds:             p.Kinds,
			SortField:         p.SortField,
			SiteIds:           p.SiteIds,
			IncludeUnassigned: p.IncludeUnassigned,
			SearchQuery:       p.SearchQuery,
			Ranges:            p.Ranges,
			Descending:        p.Descending,
			HasCursor:         p.HasCursor,
			CursorNull:        p.CursorNull,
			CursorNumber:      p.CursorNumber,
			CursorText:        p.CursorText,
			CursorKind:        p.CursorKind,
			CursorID:          p.CursorID,
			PageLimit:         p.PageLimit,
		})
		return listResult{Items: result.Items, TotalCount: result.TotalCount}, err
	case pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS:
		result, err := s.store.ListFleetEquipmentGroups(ctx, sqlc.ListFleetEquipmentGroupsParams{
			OrgID:             p.OrgID,
			SpecialFilters:    p.SpecialFilters,
			Authority:         p.Authority,
			Kinds:             p.Kinds,
			SortField:         p.SortField,
			SiteIds:           p.SiteIds,
			IncludeUnassigned: p.IncludeUnassigned,
			SearchQuery:       p.SearchQuery,
			Ancestors:         p.Ancestors,
			Ranges:            p.Ranges,
			Descending:        p.Descending,
			HasCursor:         p.HasCursor,
			CursorNull:        p.CursorNull,
			CursorNumber:      p.CursorNumber,
			CursorText:        p.CursorText,
			CursorKind:        p.CursorKind,
			CursorID:          p.CursorID,
			PageLimit:         p.PageLimit,
		})
		return listResult{Items: result.Items, TotalCount: result.TotalCount}, err
	case pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS:
		result, err := s.store.ListFleetMiningAssets(ctx, sqlc.ListFleetMiningAssetsParams{
			OrgID:             p.OrgID,
			SpecialFilters:    p.SpecialFilters,
			Authority:         p.Authority,
			Kinds:             p.Kinds,
			SortField:         p.SortField,
			SiteIds:           p.SiteIds,
			IncludeUnassigned: p.IncludeUnassigned,
			SearchQuery:       p.SearchQuery,
			Ancestors:         p.Ancestors,
			Ranges:            p.Ranges,
			Descending:        p.Descending,
			HasCursor:         p.HasCursor,
			CursorNull:        p.CursorNull,
			CursorNumber:      p.CursorNumber,
			CursorText:        p.CursorText,
			CursorKind:        p.CursorKind,
			CursorID:          p.CursorID,
			PageLimit:         p.PageLimit,
		})
		return listResult{Items: result.Items, TotalCount: result.TotalCount, MinerCount: result.MinerCount}, err
	case pb.InventoryView_INVENTORY_VIEW_UNSPECIFIED:
		// Unspecified and unrecognized views cannot select a query.
	}
	return listResult{}, fleeterror.NewInvalidArgumentError("invalid inventory view")
}
