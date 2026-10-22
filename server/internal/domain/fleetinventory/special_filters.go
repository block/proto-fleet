package fleetinventory

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
)

type specialFilter struct {
	NoFacility  bool                `json:"no_facility"`
	NoGroup     bool                `json:"no_group"`
	MinerOnly   bool                `json:"miner_only"`
	Issues      []int32             `json:"issues"`
	BuildingIDs []int64             `json:"building_ids"`
	NoBuilding  bool                `json:"no_building"`
	NoRack      bool                `json:"no_rack"`
	Zones       []*commonpb.ZoneKey `json:"zones"`
	States      []string            `json:"states"`
	Models      []string            `json:"models"`
	Firmware    []string            `json:"firmware"`
	Pairing     []string            `json:"pairing"`
	Status      []string            `json:"status"`
	Groups      []int64             `json:"groups"`
	Racks       []int64             `json:"racks"`
	Sites       []int64             `json:"sites"`
	Unassigned  bool                `json:"unassigned"`
	Search      string              `json:"search"`
	CIDRs       []string            `json:"cidrs"`
	IPRanges    []ipRange           `json:"ip_ranges"`
	Ranges      []telemetryRange    `json:"ranges"`
}
type ipRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

var supportedStates = []string{"active", "inactive", "offline", "error", "maintenance", "updating", "needs_mining_pool", "reboot_required", "unknown"}

func (s *Service) specialFilters(ctx context.Context, orgID int64, req *pb.ListInventoryRequest) ([]byte, error) {
	f := specialFilter{NoFacility: req.IncludeNoFacility, NoGroup: req.IncludeNoGroup, BuildingIDs: req.BuildingIds, NoBuilding: req.IncludeNoBuilding, NoRack: req.IncludeNoRack, Zones: req.ZoneKeys, States: req.States}
	invalid := fleeterror.NewInvalidArgumentError("filters are invalid for this inventory view")
	if (f.NoFacility && req.View == pb.InventoryView_INVENTORY_VIEW_FACILITIES) || (f.NoGroup && req.View != pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS) {
		return nil, invalid
	}
	if req.Sort == pb.InventorySort_INVENTORY_SORT_ZONE && req.View != pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS {
		return nil, invalid
	}
	if len(f.BuildingIDs) > 1024 || len(f.Zones) > 1024 || len(req.ErrorComponentTypes) > 16 || len(f.States) > len(supportedStates) {
		return nil, invalid
	}
	if req.View == pb.InventoryView_INVENTORY_VIEW_FACILITIES && (len(f.BuildingIDs) > 0 || f.NoBuilding || len(f.Zones) > 0) {
		return nil, invalid
	}
	if req.View != pb.InventoryView_INVENTORY_VIEW_MINING_ASSETS && (req.MinerFilter != nil || f.NoRack || len(f.States) > 0 || (req.Sort >= pb.InventorySort_INVENTORY_SORT_MODEL && req.Sort <= pb.InventorySort_INVENTORY_SORT_WORKER_NAME)) {
		return nil, invalid
	}
	for _, id := range f.BuildingIDs {
		if id <= 0 {
			return nil, invalid
		}
	}
	for _, z := range f.Zones {
		if z == nil || z.BuildingId < 0 || strings.TrimSpace(z.Zone) == "" {
			return nil, invalid
		}
	}
	for _, c := range req.ErrorComponentTypes {
		if c < 1 || c > 6 {
			return nil, invalid
		}
		f.Issues = append(f.Issues, int32(c))
	}
	for _, state := range f.States {
		if !slices.Contains(supportedStates, state) {
			return nil, invalid
		}
	}
	if req.MinerFilter != nil {
		// Validate with the existing parser so subnet normalization, placement
		// validation, and numeric range behavior do not diverge between APIs.
		m, err := fleetmanagement.ParseListFilter(ctx, orgID, req.MinerFilter, s.buildings)
		if err != nil {
			return nil, err
		}
		// Outer placement/issue filters would require two independent OR groups.
		// Reject that ambiguous shape; callers put those fields in the miner filter.
		if len(f.BuildingIDs) > 0 || f.NoBuilding || f.NoRack || len(f.Zones) > 0 || len(f.Issues) > 0 {
			return nil, invalid
		}
		f.MinerOnly = true
		f.Models = m.ModelNames
		f.Firmware = m.FirmwareVersions
		f.Groups = m.GroupIDs
		f.Racks = m.RackIDs
		f.BuildingIDs = m.BuildingIDs
		f.NoBuilding = m.IncludeNoBuilding
		f.NoRack = m.IncludeNoRack
		f.Sites = m.SiteIDs
		f.Unassigned = m.IncludeUnassigned
		f.Search = m.SearchQuery
		for _, z := range m.ZoneKeys {
			f.Zones = append(f.Zones, &commonpb.ZoneKey{BuildingId: z.BuildingID, Zone: z.Zone})
		}
		for _, c := range m.ErrorComponentTypes {
			f.Issues = append(f.Issues, int32(c)) //nolint:gosec // ParseListFilter maps to the bounded component enum (0..4).
		}
		for _, status := range m.DeviceStatusFilter {
			f.Status = append(f.Status, strings.ToUpper(status.String()))
		}
		for _, p := range m.PairingStatuses {
			if p != 0 {
				f.Pairing = append(f.Pairing, strings.TrimPrefix(p.String(), "PAIRING_STATUS_"))
			}
		}
		for _, cidr := range m.IPCIDRs {
			f.CIDRs = append(f.CIDRs, cidr.String())
		}
		for _, r := range m.IPRanges {
			f.IPRanges = append(f.IPRanges, ipRange{r.Start.String(), r.End.String()})
		}
		for _, r := range m.NumericRanges {
			f.Ranges = append(f.Ranges, telemetryRange{int32(r.Field), r.Min, r.Max, r.MinInclusive, r.MaxInclusive}) //nolint:gosec // ParseListFilter validates fields 1..6.
		}
	}
	if f.NoGroup && len(f.Zones) > 0 {
		return nil, fleeterror.NewInvalidArgumentError("unassigned groups cannot be combined with zone filters")
	}
	// SQL coalesces omitted slices to empty arrays.
	b, err := json.Marshal(f)
	if err != nil {
		return nil, fmt.Errorf("encode inventory filters: %w", err)
	}
	return b, nil
}
