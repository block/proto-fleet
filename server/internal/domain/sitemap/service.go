package sitemap

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/sitemap/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	buildingmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	fleetmanagementdomain "github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
	sitemodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	maxPageSize        = 1000
	maxImportBytes     = 5 * 1024 * 1024
	maxImportRows      = 100000
	maxRackDimension   = 12
	maxLayoutDimension = 100
	exportChunkBytes   = 64 * 1024

	siteMapExportFolder       = "proto-fleet-site-map"
	siteMapExportCSVPath      = siteMapExportFolder + "/site-map.csv"
	siteMapExportGuideTXTPath = siteMapExportFolder + "/agent-editing-guide.txt"

	fieldBuilding       = "building"
	displayNameReadOnly = "name (read only)"
)

var (
	siteHeaders = []string{
		"site",
	}
	buildingHeaders = []string{
		fieldBuilding, "site", "aisles", "racks_per_aisle",
	}
	rackHeaders = []string{
		"rack", fieldBuilding, "site", "zone", "rows", "columns",
		"order_index", "aisle_index", "position_in_aisle",
	}
	minerHeaders = []string{
		"device_identifier", "serial_number", "name", "ip_address", "mac_address",
		"site", fieldBuilding, "rack", "rack_row", "rack_col",
	}
)

type Service struct {
	siteStore       interfaces.SiteStore
	buildingStore   interfaces.BuildingStore
	collectionStore interfaces.CollectionStore
	deviceStore     interfaces.DeviceStore
	fleetMgmtSvc    *fleetmanagementdomain.Service
	transactor      interfaces.Transactor
	activitySvc     *activity.Service
}

func NewService(
	siteStore interfaces.SiteStore,
	buildingStore interfaces.BuildingStore,
	collectionStore interfaces.CollectionStore,
	deviceStore interfaces.DeviceStore,
	fleetMgmtSvc *fleetmanagementdomain.Service,
	transactor interfaces.Transactor,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		siteStore:       siteStore,
		buildingStore:   buildingStore,
		collectionStore: collectionStore,
		deviceStore:     deviceStore,
		fleetMgmtSvc:    fleetMgmtSvc,
		transactor:      transactor,
		activitySvc:     activitySvc,
	}
}

func (s *Service) ExportSiteMapCsv(ctx context.Context, orgID int64, send func(*pb.ExportSiteMapCsvResponse) error) error {
	snapshot, err := s.loadSnapshot(ctx, orgID)
	if err != nil {
		return err
	}

	csvData, err := buildSiteMapCSV(snapshot)
	if err != nil {
		return err
	}
	zipData, err := buildSiteMapExportZip(csvData)
	if err != nil {
		return err
	}
	return streamSiteMapExport(zipData, send)
}

func buildSiteMapCSV(snapshot *snapshot) ([]byte, error) {
	buffer := &bytes.Buffer{}
	buffer.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(buffer)

	writeSection := func(name string, headers []string, rows [][]string) error {
		if err := writer.Write([]string{fmt.Sprintf("# SECTION: %s", name)}); err != nil {
			return fleeterror.NewInternalErrorf("failed to write %s section marker: %v", name, err)
		}
		if err := writer.Write(displayHeaders(name, headers)); err != nil {
			return fleeterror.NewInternalErrorf("failed to write %s header row: %v", name, err)
		}
		for _, row := range rows {
			if err := writer.Write(row); err != nil {
				return fleeterror.NewInternalErrorf("failed to write %s data row: %v", name, err)
			}
		}
		if err := writer.Write(nil); err != nil {
			return fleeterror.NewInternalErrorf("failed to write %s section spacer: %v", name, err)
		}
		return nil
	}

	if err := writeSection("SITE", siteHeaders, siteRows(snapshot.sites)); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to write SITE section: %v", err)
	}
	if err := writeSection("BUILDING", buildingHeaders, buildingRows(snapshot.buildings)); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to write BUILDING section: %v", err)
	}
	if err := writeSection("RACK", rackHeaders, rackExportRows(snapshot.racks, snapshot.buildings)); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to write RACK section: %v", err)
	}
	if err := writeSection("MINER", minerHeaders, minerRows(snapshot.miners, snapshot.buildings)); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to write MINER section: %v", err)
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to write site map CSV: %v", err)
	}
	return buffer.Bytes(), nil
}

func buildSiteMapExportZip(csvData []byte) ([]byte, error) {
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)

	addFile := func(path string, data []byte) error {
		header := &zip.FileHeader{Name: path, Method: zip.Deflate}
		file, err := writer.CreateHeader(header)
		if err != nil {
			return fleeterror.NewInternalErrorf("failed to create %s in site map export: %v", path, err)
		}
		if _, err := file.Write(data); err != nil {
			return fleeterror.NewInternalErrorf("failed to write %s in site map export: %v", path, err)
		}
		return nil
	}

	if err := addFile(siteMapExportCSVPath, csvData); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := addFile(siteMapExportGuideTXTPath, []byte(siteMapAgentEditingGuide())); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to finalize site map export zip: %v", err)
	}
	return buffer.Bytes(), nil
}

func streamSiteMapExport(data []byte, send func(*pb.ExportSiteMapCsvResponse) error) error {
	for start := 0; start < len(data); start += exportChunkBytes {
		end := start + exportChunkBytes
		if end > len(data) {
			end = len(data)
		}
		if err := send(&pb.ExportSiteMapCsvResponse{CsvData: data[start:end]}); err != nil {
			return err
		}
	}
	return nil
}

func siteMapAgentEditingGuide() string {
	return `Proto Fleet site map CSV editing guide for AI agents

Edit proto-fleet-site-map/site-map.csv, then import only the CSV file back into Proto Fleet. Do not import this text file or the zip archive.

File structure
- The CSV is UTF-8 and includes sections marked exactly as "# SECTION: SITE", "# SECTION: BUILDING", "# SECTION: RACK", and "# SECTION: MINER".
- Keep section markers, section order, header rows, and column count unchanged.
- Blank spacer rows between sections are allowed.
- Headers ending in "(read only)" identify existing records or reference data. Do not change those values for existing rows.
- Site, building, and rack rows are upserts keyed by their identity columns. Existing names/labels update the existing row, and new names/labels create rows. Miner rows must reference existing miners; unknown miner identifiers fail validation.

Omissions and renames
- Import preview reports omitted existing rows when a site, building, rack, or miner exists in Proto Fleet but is missing from the CSV.
- The user chooses omission handling during import. Leave omitted rows in place keeps missing rows unchanged. Remove omitted rows soft-deletes omitted sites, buildings, and racks, and unassigns omitted miners.
- Remove omitted rows can cascade placement cleanup: deleting an omitted site also deletes its buildings and unassigns racks/miners from that site; deleting an omitted building unassigns its racks and miners; deleting an omitted rack removes miners from that rack.
- Site, building, and rack names/labels are identities, not rename fields. If you change one, Proto Fleet treats the edited row as a new entity and treats the old entity as omitted.
- Therefore, do not "rename" a site, building, or rack by editing its name/label unless the intended result is create-new plus optional delete-old by omission.
- If you intentionally replace a site/building/rack name, update all dependent references in the CSV to the new name. For example, a building moved to a new site key must be referenced by that new site in rack and miner placement rows.
- Miner omission is different from miner deletion: miners cannot be created or deleted by site map import. With remove omitted rows, omitted miners are only unassigned from site/building/rack placement.

Formatting rules
- Keep the file as comma-separated CSV, not markdown or a table pasted into prose.
- Quote cells with commas, quotes, or newlines using normal CSV quoting rules.
- Use blank cells for empty values.
- Preserve apostrophe-prefixed values. They protect spreadsheet-sensitive text from formula/date conversion.
- Numeric indexes are zero-based integers unless noted otherwise.

SITE section
- Columns: name (read only).
- Add a new row with a new site name to create a site. Site details beyond the name are not editable through this import.

BUILDING section
- Columns: name (read only), site (read only), aisles, racks_per_aisle.
- Add a new row with a new name to create a building. The site value must reference an existing or newly-created site, or may be blank for an unassigned building.
- aisles and racks_per_aisle define rack layout capacity for the building.
- The site column is the building's parent context for creation and identity. Moving existing buildings between sites is not edited in this section.

RACK section
- Columns: label (read only), building, site, zone, rows, columns, order_index, aisle_index, position_in_aisle.
- Add a new row with a new rack label to create a rack. Rack labels must be unique across the organization.
- Set building to place a rack in a building. If the building name is unique, site may be blank and will be inferred.
- If the building name is ambiguous across sites, set site to disambiguate it.
- Set site and leave building blank to assign a rack directly to a site.
- Leave both building and site blank to unassign a rack.
- zone is scoped to a building. Moving a rack to another building, directly to a site, or unassigned clears incompatible zone assignment.
- rows and columns define rack slot capacity. Each must be between 1 and 12.
- order_index controls physical slot ordering. Allowed values are BOTTOM_LEFT, TOP_LEFT, BOTTOM_RIGHT, TOP_RIGHT, or blank.
- aisle_index and position_in_aisle place a rack in the building layout. They must both be blank or both be set. Aisle and position indexes are zero-based and must fit within aisles and racks_per_aisle.

MINER section
- Columns: device_identifier (read only), serial_number (read only), name (read only), ip_address (read only), mac_address (read only), site, building, rack, rack_row, rack_col.
- Edit only placement columns in this section.
- device_identifier identifies the miner. Unknown miner identifiers fail validation.
- If rack is set, the rack determines the miner's building and site. Leave miner site and building blank unless needed to disambiguate duplicate rack labels.
- If building is set and rack is blank, the building determines the miner's site when the building name is unique. Leave miner site blank unless needed to disambiguate duplicate building names.
- Set site and leave building and rack blank to assign a miner directly to a site.
- Leave site, building, and rack blank to unassign a miner.
- rack_row and rack_col must both be blank or both be set. They are zero-based and must fit within the rack's rows and columns.
- Multiple miners cannot end in the same rack slot. Slot swaps are valid when the final CSV has no duplicate slot positions.

Validation behavior
- Changing read-only identity fields fails validation.
- Assigning more racks to a building layout than aisles * racks_per_aisle fails validation.
- Assigning more miners to a rack than rows * columns fails validation.
- Assigning miners to rack slots outside the rack dimensions fails validation.
- Ambiguous names must be disambiguated with the parent placement column.
- The dry-run preview validates the entire CSV before commit. Fix all reported errors and run preview again before importing.
`
}

func (s *Service) ImportSiteMapCsv(ctx context.Context, orgID int64, req *pb.ImportSiteMapCsvRequest) (*pb.ImportSiteMapCsvResponse, error) {
	if len(req.GetCsvData()) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("csv_data is required")
	}
	if len(req.GetCsvData()) > maxImportBytes {
		return nil, fleeterror.NewInvalidArgumentErrorf("csv_data must be at most %d bytes", maxImportBytes)
	}
	parsed, parseErrs := parseSiteMapCSV(req.GetCsvData())
	if len(parseErrs) > 0 {
		return &pb.ImportSiteMapCsvResponse{Errors: parseErrs}, nil
	}
	snapshot, err := s.loadSnapshot(ctx, orgID)
	if err != nil {
		return nil, err
	}
	plan := buildPlan(parsed, snapshot, req.GetOmissionMode())
	if len(plan.errors) > 0 {
		return &pb.ImportSiteMapCsvResponse{
			OmissionCounts: plan.omissions,
			Errors:         plan.errors,
			Warnings:       plan.warnings,
		}, nil
	}
	if hasOmissions(plan.omissions) && req.GetOmissionMode() == pb.OmissionMode_OMISSION_MODE_UNSPECIFIED {
		return &pb.ImportSiteMapCsvResponse{
			OmissionChoiceRequired: true,
			OmissionCounts:         plan.omissions,
			Warnings:               plan.warnings,
		}, nil
	}

	token := commitToken(parsed, req.GetOmissionMode(), plan, snapshot)
	if !req.GetDryRun() {
		if req.GetCommitToken() == "" {
			return nil, fleeterror.NewInvalidArgumentError("commit_token is required when dry_run is false")
		}
		if req.GetCommitToken() != token {
			return nil, fleeterror.NewFailedPreconditionError("site map changed since dry-run; run dry-run again")
		}
		if err := ensureSupportedCommitPlan(plan); err != nil {
			return nil, err
		}
		if err := s.applyImportPlan(ctx, orgID, parsed, snapshot, req.GetOmissionMode()); err != nil {
			return nil, err
		}
		s.logSiteMapImportActivity(ctx, orgID, plan)
		return &pb.ImportSiteMapCsvResponse{
			OmissionCounts: plan.omissions,
			Warnings:       plan.warnings,
			Changes:        plan.changes,
			CommitToken:    token,
		}, nil
	}
	if err := ensureSupportedCommitPlan(plan); err != nil {
		return &pb.ImportSiteMapCsvResponse{
			OmissionCounts: plan.omissions,
			Errors:         []*pb.ImportValidationError{csvErr(0, "", err.Error())},
			Warnings:       plan.warnings,
			Changes:        plan.changes,
		}, nil
	}

	return &pb.ImportSiteMapCsvResponse{
		OmissionCounts: plan.omissions,
		Warnings:       plan.warnings,
		Changes:        plan.changes,
		CommitToken:    token,
	}, nil
}

type snapshot struct {
	sites             []sitemodels.Site
	buildings         []buildingmodels.Building
	racks             []rackSnapshot
	miners            []minerSnapshot
	hiddenRackMembers []minerSnapshot
}

type rackSnapshot struct {
	ID              int64
	SiteID          *int64
	BuildingID      *int64
	Site            string
	Building        string
	Label           string
	Zone            string
	Rows            int32
	Columns         int32
	CoolingType     string
	OrderIndex      string
	AisleIndex      string
	PositionInAisle string
}

type minerSnapshot struct {
	DeviceIdentifier string
	SerialNumber     string
	Name             string
	IPAddress        string
	MACAddress       string
	Site             string
	Building         string
	Rack             string
	RackRow          string
	RackCol          string
}

type slotPosition struct {
	rack     string
	site     string
	building string
	row      string
	col      string
}

type pendingMinerSlot struct {
	rackID           int64
	deviceIdentifier string
	row              string
	col              string
}

type pendingRackGridPosition struct {
	rackID          int64
	aisleIndex      *int32
	positionInAisle *int32
}

func (s *Service) loadSnapshot(ctx context.Context, orgID int64) (*snapshot, error) {
	siteRows, err := s.siteStore.ListSites(ctx, orgID)
	if err != nil {
		return nil, err
	}
	sites := make([]sitemodels.Site, 0, len(siteRows))
	for _, row := range siteRows {
		sites = append(sites, row.Site)
	}
	buildingRows, err := s.buildingStore.ListBuildings(ctx, buildingmodels.ListFilter{OrgID: orgID})
	if err != nil {
		return nil, err
	}
	buildings := make([]buildingmodels.Building, 0, len(buildingRows))
	for _, row := range buildingRows {
		buildings = append(buildings, row.Building)
	}
	racks, slots, err := s.listRacksAndSlots(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if err := s.fillRackGridPositions(ctx, orgID, buildings, racks); err != nil {
		return nil, err
	}
	miners, err := s.listMiners(ctx, slots)
	if err != nil {
		return nil, err
	}
	out := &snapshot{sites: sites, buildings: buildings, racks: racks, miners: miners, hiddenRackMembers: hiddenRackMembers(slots, miners)}
	sortSnapshot(out)
	return out, nil
}

func (s *Service) listRacksAndSlots(ctx context.Context, orgID int64) ([]rackSnapshot, map[string]slotPosition, error) {
	var racks []rackSnapshot
	slots := map[string]slotPosition{}
	cursor := ""
	for {
		collections, nextCursor, _, err := s.collectionStore.ListCollections(ctx, orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, maxPageSize, cursor, nil, nil)
		if err != nil {
			return nil, nil, err
		}
		for _, collection := range collections {
			info := collection.GetRackInfo()
			if info == nil {
				continue
			}
			siteLabel, buildingLabel := placementLabels(collection.GetPlacement())
			rack := rackSnapshot{
				ID:              collection.GetId(),
				SiteID:          placementID(collection.GetPlacement().GetSite()),
				BuildingID:      placementID(collection.GetPlacement().GetBuilding()),
				Site:            siteLabel,
				Building:        buildingLabel,
				Label:           collection.GetLabel(),
				Zone:            info.GetZone(),
				Rows:            info.GetRows(),
				Columns:         info.GetColumns(),
				CoolingType:     rackCoolingType(info.GetCoolingType()),
				OrderIndex:      rackOrderIndex(info.GetOrderIndex()),
				AisleIndex:      "",
				PositionInAisle: "",
			}
			racks = append(racks, rack)
			memberCursor := ""
			for {
				members, nextMemberCursor, err := s.collectionStore.ListCollectionMembers(ctx, orgID, collection.GetId(), maxPageSize, memberCursor, nil)
				if err != nil {
					return nil, nil, err
				}
				for _, member := range members {
					if member.GetRack() == nil {
						continue
					}
					slot := slotPosition{
						rack:     collection.GetLabel(),
						site:     rack.Site,
						building: rack.Building,
					}
					if pos := member.GetRack().GetSlotPosition(); pos != nil {
						slot.row = strconv.FormatInt(int64(pos.GetRow()), 10)
						slot.col = strconv.FormatInt(int64(pos.GetColumn()), 10)
					}
					slots[member.GetDeviceIdentifier()] = slot
				}
				if nextMemberCursor == "" {
					break
				}
				memberCursor = nextMemberCursor
			}
		}
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return racks, slots, nil
}

func (s *Service) fillRackGridPositions(ctx context.Context, orgID int64, buildings []buildingmodels.Building, racks []rackSnapshot) error {
	rackIndexes := map[int64]int{}
	for i, rack := range racks {
		rackIndexes[rack.ID] = i
	}
	for _, building := range buildings {
		cursor := ""
		for {
			buildingRacks, nextCursor, err := s.buildingStore.ListBuildingRacks(ctx, orgID, building.ID, maxPageSize, cursor)
			if err != nil {
				return err
			}
			for _, buildingRack := range buildingRacks {
				index, ok := rackIndexes[buildingRack.RackID]
				if !ok {
					continue
				}
				if buildingRack.AisleIndex != nil {
					racks[index].AisleIndex = strconv.FormatInt(int64(*buildingRack.AisleIndex), 10)
				}
				if buildingRack.PositionInAisle != nil {
					racks[index].PositionInAisle = strconv.FormatInt(int64(*buildingRack.PositionInAisle), 10)
				}
			}
			if nextCursor == "" {
				break
			}
			cursor = nextCursor
		}
	}
	return nil
}

func (s *Service) listMiners(ctx context.Context, slots map[string]slotPosition) ([]minerSnapshot, error) {
	var miners []minerSnapshot
	cursor := ""
	for {
		resp, err := s.fleetMgmtSvc.ListMinerStateSnapshots(ctx, &fleetpb.ListMinerStateSnapshotsRequest{
			PageSize: maxPageSize,
			Cursor:   cursor,
			Filter: &fleetpb.MinerListFilter{PairingStatuses: []fleetpb.PairingStatus{
				fleetpb.PairingStatus_PAIRING_STATUS_PAIRED,
				fleetpb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
				fleetpb.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
			}},
		})
		if err != nil {
			return nil, err
		}
		for _, miner := range resp.GetMiners() {
			site, building, rack := placementLabels3(miner.GetPlacement())
			slot := slots[miner.GetDeviceIdentifier()]
			if slot.rack != "" {
				site = slot.site
				building = slot.building
				rack = slot.rack
			}
			miners = append(miners, minerSnapshot{
				DeviceIdentifier: miner.GetDeviceIdentifier(),
				SerialNumber:     miner.GetSerialNumber(),
				Name:             miner.GetName(),
				IPAddress:        miner.GetIpAddress(),
				MACAddress:       miner.GetMacAddress(),
				Site:             site,
				Building:         building,
				Rack:             rack,
				RackRow:          slot.row,
				RackCol:          slot.col,
			})
		}
		if resp.GetCursor() == "" {
			break
		}
		cursor = resp.GetCursor()
	}
	return miners, nil
}

func hiddenRackMembers(slots map[string]slotPosition, miners []minerSnapshot) []minerSnapshot {
	exportedMiners := rowSetFromMiners(miners)
	hidden := make([]minerSnapshot, 0, len(slots))
	for deviceIdentifier, slot := range slots {
		if slot.rack == "" || exportedMiners[deviceIdentifier] {
			continue
		}
		hidden = append(hidden, minerSnapshot{
			DeviceIdentifier: deviceIdentifier,
			Site:             slot.site,
			Building:         slot.building,
			Rack:             slot.rack,
			RackRow:          slot.row,
			RackCol:          slot.col,
		})
	}
	return hidden
}

func sortSnapshot(s *snapshot) {
	sort.SliceStable(s.sites, func(i, j int) bool { return s.sites[i].Name < s.sites[j].Name })
	sort.SliceStable(s.buildings, func(i, j int) bool {
		if s.buildings[i].SiteLabel != s.buildings[j].SiteLabel {
			return s.buildings[i].SiteLabel < s.buildings[j].SiteLabel
		}
		return s.buildings[i].Name < s.buildings[j].Name
	})
	sort.SliceStable(s.racks, func(i, j int) bool {
		a, b := s.racks[i], s.racks[j]
		return compareStrings(a.Site, b.Site, a.Building, b.Building, a.Label, b.Label, strconv.FormatInt(a.ID, 10), strconv.FormatInt(b.ID, 10))
	})
	sort.SliceStable(s.miners, func(i, j int) bool {
		a, b := s.miners[i], s.miners[j]
		return compareStrings(a.Site, b.Site, a.Building, b.Building, a.Rack, b.Rack, a.RackRow, b.RackRow, a.RackCol, b.RackCol, a.DeviceIdentifier, b.DeviceIdentifier)
	})
	sort.SliceStable(s.hiddenRackMembers, func(i, j int) bool {
		a, b := s.hiddenRackMembers[i], s.hiddenRackMembers[j]
		return compareStrings(a.Site, b.Site, a.Building, b.Building, a.Rack, b.Rack, a.RackRow, b.RackRow, a.RackCol, b.RackCol, a.DeviceIdentifier, b.DeviceIdentifier)
	})
}

func compareStrings(values ...string) bool {
	for i := 0; i+1 < len(values); i += 2 {
		if values[i] == values[i+1] {
			continue
		}
		return values[i] < values[i+1]
	}
	return false
}

func displayHeaders(section string, headers []string) []string {
	out := make([]string, 0, len(headers))
	for _, header := range headers {
		out = append(out, displayHeader(section, header))
	}
	return out
}

func displayHeader(section, header string) string {
	switch section {
	case "SITE":
		if header == "site" {
			return displayNameReadOnly
		}
	case "BUILDING":
		switch header {
		case fieldBuilding:
			return displayNameReadOnly
		case "site":
			return "site (read only)"
		}
	case "RACK":
		if header == "rack" {
			return "label (read only)"
		}
	case "MINER":
		switch header {
		case "device_identifier", "serial_number", "name", "ip_address", "mac_address":
			return header + " (read only)"
		}
	}
	return header
}

func siteRows(sites []sitemodels.Site) [][]string {
	rows := make([][]string, 0, len(sites))
	for _, site := range sites {
		rows = append(rows, []string{
			clean(site.Name),
		})
	}
	return rows
}

func buildingRows(buildings []buildingmodels.Building) [][]string {
	rows := make([][]string, 0, len(buildings))
	for _, building := range buildings {
		rows = append(rows, []string{
			clean(building.Name),
			clean(building.SiteLabel),
			formatInt32(building.Aisles),
			formatInt32(building.RacksPerAisle),
		})
	}
	return rows
}

func rackRows(racks []rackSnapshot) [][]string {
	rows := make([][]string, 0, len(racks))
	for _, rack := range racks {
		rows = append(rows, []string{
			clean(rack.Label),
			clean(rack.Building),
			clean(rack.Site),
			clean(rack.Zone),
			formatInt32(rack.Rows),
			formatInt32(rack.Columns),
			rack.OrderIndex,
			rack.AisleIndex,
			rack.PositionInAisle,
		})
	}
	return rows
}

func rackExportRows(racks []rackSnapshot, buildings []buildingmodels.Building) [][]string {
	rows := make([][]string, 0, len(racks))
	ambiguousBuildingNames := ambiguousBuildingLabels(buildings)
	for _, rack := range racks {
		rows = append(rows, []string{
			clean(rack.Label),
			clean(rack.Building),
			clean(rackExportSite(rack, ambiguousBuildingNames)),
			clean(rack.Zone),
			formatInt32(rack.Rows),
			formatInt32(rack.Columns),
			rack.OrderIndex,
			rack.AisleIndex,
			rack.PositionInAisle,
		})
	}
	return rows
}

func rackExportSite(rack rackSnapshot, ambiguousBuildingNames map[string]bool) string {
	if rack.Building != "" && !ambiguousBuildingNames[rack.Building] {
		return ""
	}
	return rack.Site
}

func minerRows(miners []minerSnapshot, buildings []buildingmodels.Building) [][]string {
	rows := make([][]string, 0, len(miners))
	ambiguousBuildingNames := ambiguousBuildingLabels(buildings)
	for _, miner := range miners {
		rows = append(rows, []string{
			clean(miner.DeviceIdentifier),
			clean(miner.SerialNumber),
			clean(miner.Name),
			clean(miner.IPAddress),
			clean(miner.MACAddress),
			clean(minerExportSite(miner, ambiguousBuildingNames)),
			clean(minerExportBuilding(miner)),
			clean(miner.Rack),
			miner.RackRow,
			miner.RackCol,
		})
	}
	return rows
}

func minerExportSite(miner minerSnapshot, ambiguousBuildingNames map[string]bool) string {
	if miner.Rack != "" {
		return ""
	}
	if miner.Building != "" && !ambiguousBuildingNames[miner.Building] {
		return ""
	}
	return miner.Site
}

func minerExportBuilding(miner minerSnapshot) string {
	if miner.Rack != "" {
		return ""
	}
	return miner.Building
}

func placementLabels(refs *commonpb.PlacementRefs) (string, string) {
	site := ""
	building := ""
	if refs != nil && refs.GetSite() != nil {
		site = refs.GetSite().GetLabel()
	}
	if refs != nil && refs.GetBuilding() != nil {
		building = refs.GetBuilding().GetLabel()
	}
	return site, building
}

func placementID(ref *commonpb.ResourceRef) *int64 {
	if ref == nil {
		return nil
	}
	id := ref.GetId()
	return &id
}

func nullableInt64Equal(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func placementLabels3(refs *commonpb.PlacementRefs) (string, string, string) {
	site := ""
	building := ""
	rack := ""
	if refs != nil && refs.GetSite() != nil {
		site = refs.GetSite().GetLabel()
	}
	if refs != nil && refs.GetBuilding() != nil {
		building = refs.GetBuilding().GetLabel()
	}
	if refs != nil && refs.GetRack() != nil {
		rack = refs.GetRack().GetLabel()
	}
	return site, building, rack
}

func rackCoolingType(value collectionpb.RackCoolingType) string {
	switch value {
	case collectionpb.RackCoolingType_RACK_COOLING_TYPE_UNSPECIFIED:
		return ""
	case collectionpb.RackCoolingType_RACK_COOLING_TYPE_AIR:
		return "AIR"
	case collectionpb.RackCoolingType_RACK_COOLING_TYPE_IMMERSION:
		return "IMMERSION"
	default:
		return ""
	}
}

func rackOrderIndex(value collectionpb.RackOrderIndex) string {
	switch value {
	case collectionpb.RackOrderIndex_RACK_ORDER_INDEX_UNSPECIFIED:
		return ""
	case collectionpb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT:
		return "BOTTOM_LEFT"
	case collectionpb.RackOrderIndex_RACK_ORDER_INDEX_TOP_LEFT:
		return "TOP_LEFT"
	case collectionpb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_RIGHT:
		return "BOTTOM_RIGHT"
	case collectionpb.RackOrderIndex_RACK_ORDER_INDEX_TOP_RIGHT:
		return "TOP_RIGHT"
	default:
		return ""
	}
}

func formatInt32(value int32) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatInt(int64(value), 10)
}

func clean(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if strings.HasPrefix(value, "'") {
		return "'" + value
	}
	if isFormulaLike(value) || isSectionMarkerLike(value) {
		return "'" + value
	}
	return value
}

func unescapeCleanedValue(value string) string {
	if len(value) > 1 && strings.HasPrefix(value, "''") {
		return value[1:]
	}
	if len(value) > 1 && value[0] == '\'' && (isFormulaLike(value[1:]) || isSectionMarkerLike(value[1:])) {
		return value[1:]
	}
	return value
}

func isSectionMarkerLike(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "# SECTION: ")
}

func isFormulaLike(value string) bool {
	if value == "" {
		return false
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r', '\n':
		return true
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			continue
		}
		switch r {
		case '=', '+', '-', '@':
			return true
		}
		break
	}
	return false
}

type parsedCSV struct {
	sections map[string][]map[string]string
}

func parseSiteMapCSV(data []byte) (*parsedCSV, []*pb.ImportValidationError) {
	text := strings.TrimPrefix(string(data), "\ufeff")
	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, []*pb.ImportValidationError{{Message: fmt.Sprintf("invalid CSV: %v", err)}}
	}
	if len(records) > maxImportRows {
		return nil, []*pb.ImportValidationError{{Message: fmt.Sprintf("CSV has too many rows: %d exceeds limit %d", len(records), maxImportRows)}}
	}
	out := &parsedCSV{sections: map[string][]map[string]string{}}
	expected := map[string][]string{
		"SITE":     siteHeaders,
		"BUILDING": buildingHeaders,
		"RACK":     rackHeaders,
		"MINER":    minerHeaders,
	}
	var errs []*pb.ImportValidationError
	for i := 0; i < len(records); i++ {
		record := trimRecord(records[i])
		if isBlankRecord(record) {
			continue
		}
		if !isSectionMarker(record) {
			errs = append(errs, csvErr(i+1, "", "expected section marker"))
			continue
		}
		section := strings.TrimSpace(strings.TrimPrefix(record[0], "# SECTION: "))
		headers, ok := expected[section]
		if !ok {
			errs = append(errs, csvErr(i+1, section, "unknown section"))
			continue
		}
		i++
		for i < len(records) && isBlankRecord(trimRecord(records[i])) {
			i++
		}
		if i >= len(records) {
			errs = append(errs, csvErr(i, section, "missing header row"))
			break
		}
		gotHeaders := trimTrailingEmpty(trimRecord(records[i]))
		wantHeaders := displayHeaders(section, headers)
		if !sameStrings(gotHeaders, wantHeaders) {
			errs = append(errs, csvErr(i+1, section, fmt.Sprintf("unexpected header, want %s", strings.Join(wantHeaders, ","))))
			continue
		}
		for i+1 < len(records) {
			next := trimRecord(records[i+1])
			if isSectionMarker(next) {
				break
			}
			i++
			if isBlankRecord(next) {
				continue
			}
			next = trimTrailingEmptyToMax(next, len(headers))
			if len(next) != len(headers) {
				errs = append(errs, csvErr(i+1, section, "row has the wrong number of columns"))
				continue
			}
			row := map[string]string{}
			for j, header := range headers {
				row[header] = unescapeCleanedValue(strings.TrimSpace(next[j]))
			}
			row["__row"] = strconv.Itoa(i + 1)
			out.sections[section] = append(out.sections[section], row)
		}
	}
	for section := range expected {
		if _, ok := out.sections[section]; !ok {
			out.sections[section] = nil
		}
	}
	return out, errs
}

type importPlan struct {
	omissions *pb.OmissionCounts
	errors    []*pb.ImportValidationError
	warnings  []string
	changes   []*pb.ImportChangeSummary
}

func buildPlan(parsed *parsedCSV, snap *snapshot, mode pb.OmissionMode) importPlan {
	targetSnap := snapshotForOmissionMode(snap, mode)
	normalizeInferredPlacement(parsed, targetSnap)
	plan := importPlan{omissions: &pb.OmissionCounts{}}
	siteKeys := rowSet(parsed.sections["SITE"], "site")
	buildingKeys := compoundRowSet(parsed.sections["BUILDING"], "site", fieldBuilding)
	rackKeys := rowSet(parsed.sections["RACK"], "rack")
	minerKeys := rowSet(parsed.sections["MINER"], "device_identifier")

	for _, site := range snap.sites {
		if !siteKeys[site.Name] {
			plan.omissions.Sites++
		}
	}
	for _, building := range snap.buildings {
		if !buildingKeys[building.SiteLabel+"\x00"+building.Name] {
			plan.omissions.Buildings++
		}
	}
	for _, rack := range snap.racks {
		if !rackKeys[rack.Label] {
			plan.omissions.Racks++
		}
	}
	for _, miner := range snap.miners {
		if !minerKeys[miner.DeviceIdentifier] {
			plan.omissions.Miners++
		}
	}

	plan.errors = append(plan.errors, validateUnique(parsed.sections["SITE"], "SITE", "site")...)
	plan.errors = append(plan.errors, validateUniqueBuildingRows(parsed.sections["BUILDING"])...)
	plan.errors = append(plan.errors, validateUnique(parsed.sections["RACK"], "RACK", "rack")...)
	plan.errors = append(plan.errors, validateUnique(parsed.sections["MINER"], "MINER", "device_identifier")...)
	removeReferenceErrors := validateRemoveOmittedReferences(parsed, mode)
	plan.errors = append(plan.errors, removeReferenceErrors...)
	plan.errors = append(plan.errors, validateKnownMiners(parsed.sections["MINER"], snap)...)
	plan.errors = append(plan.errors, validateReadOnlyMinerFields(parsed.sections["MINER"], snap)...)
	if len(removeReferenceErrors) == 0 {
		plan.errors = append(plan.errors, validateBuildingSiteTargets(parsed.sections["BUILDING"], parsed.sections["SITE"], targetSnap)...)
		plan.errors = append(plan.errors, validateRackPlacementTargets(parsed.sections["RACK"], parsed.sections["BUILDING"], parsed.sections["SITE"], targetSnap)...)
		plan.errors = append(plan.errors, validatePlacementConsistency(parsed.sections["MINER"], parsed.sections["RACK"], parsed.sections["BUILDING"], parsed.sections["SITE"], targetSnap)...)
	}
	plan.errors = append(plan.errors, validateBuildingLayoutBounds(parsed.sections["BUILDING"])...)
	plan.errors = append(plan.errors, validateRackDimensions(parsed.sections["RACK"])...)
	plan.errors = append(plan.errors, validateRackGridPositions(parsed.sections["RACK"], parsed.sections["BUILDING"], targetSnap)...)
	plan.errors = append(plan.errors, validateRackGridCollisions(parsed.sections["RACK"], snap, mode)...)
	plan.errors = append(plan.errors, validateRackSlotBounds(parsed.sections["MINER"], parsed.sections["RACK"], targetSnap)...)
	plan.errors = append(plan.errors, validateExistingSlotsFitRackDimensions(parsed.sections["MINER"], parsed.sections["RACK"], targetSnap, mode)...)
	plan.errors = append(plan.errors, validateRackCapacity(parsed.sections["MINER"], parsed.sections["RACK"], targetSnap, mode)...)
	plan.errors = append(plan.errors, validateBuildingRackCapacity(parsed.sections["RACK"], parsed.sections["BUILDING"], targetSnap)...)
	plan.errors = append(plan.errors, validateBuildingExistingRacksFitLayout(parsed.sections["RACK"], parsed.sections["BUILDING"], targetSnap, mode)...)
	plan.errors = append(plan.errors, validateSlotCollisions(parsed.sections["MINER"])...)
	plan.errors = append(plan.errors, validateSlotConflictsWithExisting(parsed.sections["MINER"], snap, mode)...)
	if len(plan.errors) > 0 || (mode == pb.OmissionMode_OMISSION_MODE_UNSPECIFIED && hasOmissions(plan.omissions)) {
		return plan
	}

	addChange := func(op pb.ImportOperation, entityType string, count int32, description string) {
		if count > 0 {
			plan.changes = append(plan.changes, &pb.ImportChangeSummary{Operation: op, EntityType: entityType, Count: count, Description: description})
		}
	}
	addChange(pb.ImportOperation_IMPORT_OPERATION_CREATE, "site", countCreates(rowSetFromSites(snap.sites), siteKeys), "new site rows")
	addChange(pb.ImportOperation_IMPORT_OPERATION_CREATE, fieldBuilding, countCreates(rowSetFromBuildings(snap.buildings), buildingKeys), "new building rows")
	addChange(pb.ImportOperation_IMPORT_OPERATION_CREATE, "rack", countCreates(rowSetFromRacks(snap.racks), rackKeys), "new rack rows")
	addChange(pb.ImportOperation_IMPORT_OPERATION_UPDATE, "site", countSiteUpdates(parsed.sections["SITE"], snap.sites), "site rows with changed details")
	addChange(pb.ImportOperation_IMPORT_OPERATION_UPDATE, fieldBuilding, countBuildingUpdates(parsed.sections["BUILDING"], snap.buildings), "building rows with changed details")
	addChange(pb.ImportOperation_IMPORT_OPERATION_UPDATE, "rack", countRackUpdates(parsed.sections["RACK"], snap.racks), "rack rows with changed details")
	addChange(pb.ImportOperation_IMPORT_OPERATION_MOVE, "miner", countMinerPlacementUpdates(parsed.sections["MINER"], parsed.sections["RACK"], parsed.sections["BUILDING"], targetSnap), "miner placement rows with changed site, building, rack, or slot")
	if mode == pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
		addChange(pb.ImportOperation_IMPORT_OPERATION_UNASSIGN, "miner", countDeletes(rowSetFromMiners(snap.miners), minerKeys), "omitted miner rows to unassign")
		addChange(pb.ImportOperation_IMPORT_OPERATION_DELETE, "rack", countDeletes(rowSetFromRacks(snap.racks), rackKeys), "omitted rack rows to delete")
		addChange(pb.ImportOperation_IMPORT_OPERATION_DELETE, fieldBuilding, countDeletes(rowSetFromBuildings(snap.buildings), buildingKeys), "omitted building rows to delete")
		addChange(pb.ImportOperation_IMPORT_OPERATION_DELETE, "site", countDeletes(rowSetFromSites(snap.sites), siteKeys), "omitted site rows to delete")
	}
	return plan
}

func snapshotForOmissionMode(snap *snapshot, mode pb.OmissionMode) *snapshot {
	if mode != pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
		return snap
	}
	return &snapshot{
		miners:            snap.miners,
		hiddenRackMembers: snap.hiddenRackMembers,
	}
}

func normalizeInferredPlacement(parsed *parsedCSV, snap *snapshot) {
	buildingsByName, ambiguousBuildings := desiredBuildingNameLookup(parsed.sections["BUILDING"], snap.buildings)
	for _, row := range parsed.sections["RACK"] {
		if row["site"] != "" || row[fieldBuilding] == "" || ambiguousBuildings[row[fieldBuilding]] {
			continue
		}
		if building, ok := buildingsByName[row[fieldBuilding]]; ok {
			row["site"] = building.SiteLabel
		}
	}
}

func ensureSupportedCommitPlan(plan importPlan) error {
	for _, change := range plan.changes {
		switch change.GetOperation() {
		case pb.ImportOperation_IMPORT_OPERATION_UPDATE:
			switch change.GetEntityType() {
			case "site", fieldBuilding, "rack":
				continue
			}
		case pb.ImportOperation_IMPORT_OPERATION_CREATE:
			switch change.GetEntityType() {
			case "site", fieldBuilding, "rack":
				continue
			}
		case pb.ImportOperation_IMPORT_OPERATION_MOVE:
			if change.GetEntityType() == "miner" {
				continue
			}
		case pb.ImportOperation_IMPORT_OPERATION_RENAME:
			if change.GetEntityType() == "miner" {
				continue
			}
		case pb.ImportOperation_IMPORT_OPERATION_UNASSIGN:
			if change.GetEntityType() == "miner" {
				continue
			}
		case pb.ImportOperation_IMPORT_OPERATION_DELETE:
			switch change.GetEntityType() {
			case "site", fieldBuilding, "rack":
				continue
			}
		case pb.ImportOperation_IMPORT_OPERATION_UNSPECIFIED:
		}
		return fleeterror.NewFailedPreconditionErrorf(
			"site map commit does not yet support %s %s changes",
			strings.ToLower(change.GetOperation().String()),
			change.GetEntityType(),
		)
	}
	return nil
}

func (s *Service) applyImportPlan(ctx context.Context, orgID int64, parsed *parsedCSV, snap *snapshot, omissionMode pb.OmissionMode) error {
	if s.transactor == nil {
		return fleeterror.NewInternalError("site map import requires a transactor")
	}

	sitesByName := map[string]sitemodels.Site{}
	for _, site := range snap.sites {
		sitesByName[site.Name] = site
	}
	buildingsByKey := map[string]buildingmodels.Building{}
	for _, building := range snap.buildings {
		buildingsByKey[building.SiteLabel+"\x00"+building.Name] = building
	}
	racksByLabel := map[string]rackSnapshot{}
	for _, rack := range snap.racks {
		racksByLabel[rack.Label] = rack
	}
	minersByID := map[string]minerSnapshot{}
	for _, miner := range snap.miners {
		minersByID[miner.DeviceIdentifier] = miner
	}
	return s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		if omissionMode == pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
			if err := s.applyOmittedRows(txCtx, orgID, parsed, snap); err != nil {
				return err
			}
		}
		if err := s.applySiteRows(txCtx, orgID, parsed.sections["SITE"], sitesByName); err != nil {
			return err
		}
		if err := s.applyBuildingRows(txCtx, orgID, parsed.sections["BUILDING"], sitesByName, buildingsByKey); err != nil {
			return err
		}
		if err := s.applyRackRows(txCtx, orgID, parsed.sections["RACK"], sitesByName, buildingsByKey, racksByLabel); err != nil {
			return err
		}
		return s.applyMinerRows(txCtx, orgID, parsed.sections["MINER"], parsed.sections["BUILDING"], snap.buildings, sitesByName, buildingsByKey, racksByLabel, minersByID)
	})
}

func (s *Service) applyOmittedRows(ctx context.Context, orgID int64, parsed *parsedCSV, snap *snapshot) error {
	if err := s.unassignOmittedMiners(ctx, orgID, omittedMiners(parsed.sections["MINER"], snap.miners)); err != nil {
		return err
	}
	if err := s.deleteOmittedRacks(ctx, orgID, omittedRacks(parsed.sections["RACK"], snap.racks)); err != nil {
		return err
	}
	if err := s.deleteOmittedBuildings(ctx, orgID, omittedBuildings(parsed.sections["BUILDING"], snap.buildings)); err != nil {
		return err
	}
	return s.deleteOmittedSites(ctx, orgID, omittedSites(parsed.sections["SITE"], snap.sites))
}

func (s *Service) unassignOmittedMiners(ctx context.Context, orgID int64, miners []minerSnapshot) error {
	for _, miner := range miners {
		deviceIDs := []string{miner.DeviceIdentifier}
		if _, err := s.collectionStore.LockRacksForReparent(ctx, orgID, deviceIDs, 0); err != nil {
			return err
		}
		if _, err := s.collectionStore.RemoveDevicesFromAnyRack(ctx, orgID, deviceIDs, 0); err != nil {
			return err
		}
		if _, err := s.siteStore.AssignDevicesToSite(ctx, orgID, nil, deviceIDs); err != nil {
			return err
		}
		if _, err := s.buildingStore.AssignDevicesToBuilding(ctx, orgID, nil, deviceIDs); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) deleteOmittedRacks(ctx context.Context, orgID int64, racks []rackSnapshot) error {
	for _, rack := range racks {
		if _, err := s.collectionStore.LockRackPlacementForWrite(ctx, rack.ID, orgID); err != nil {
			return err
		}
		if _, err := s.collectionStore.UnassignDeviceSitesByRack(ctx, rack.ID, orgID); err != nil {
			return err
		}
		if _, err := s.collectionStore.UnassignDeviceBuildingsByRack(ctx, rack.ID, orgID); err != nil {
			return err
		}
		if err := s.collectionStore.ClearRackPlacementForSoftDelete(ctx, orgID, rack.ID); err != nil {
			return err
		}
		if _, err := s.collectionStore.RemoveAllDevicesFromCollection(ctx, orgID, rack.ID); err != nil {
			return err
		}
		rowsAffected, err := s.collectionStore.SoftDeleteCollection(ctx, orgID, rack.ID)
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return fleeterror.NewNotFoundErrorf("rack %d not found", rack.ID)
		}
	}
	return nil
}

func (s *Service) deleteOmittedBuildings(ctx context.Context, orgID int64, buildings []buildingmodels.Building) error {
	for _, building := range buildings {
		_, found, err := s.buildingStore.SoftDeleteBuilding(ctx, orgID, building.ID)
		if err != nil {
			return err
		}
		if !found {
			return fleeterror.NewNotFoundErrorf("building %d not found", building.ID)
		}
		if _, err := s.buildingStore.UnassignRacksFromBuilding(ctx, orgID, building.ID); err != nil {
			return err
		}
		if _, err := s.buildingStore.ClearDeviceBuildingsByBuilding(ctx, orgID, building.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) deleteOmittedSites(ctx context.Context, orgID int64, sites []sitemodels.Site) error {
	for _, site := range sites {
		if err := s.siteStore.LockSiteForWrite(ctx, orgID, site.ID); err != nil {
			return err
		}
		if err := s.siteStore.LockBuildingsBySiteForWrite(ctx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := s.siteStore.UnassignRacksFromBuildingsBySite(ctx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := s.buildingStore.ClearDeviceBuildingsBySite(ctx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := s.siteStore.SoftDeleteBuildingsBySite(ctx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := s.siteStore.UnassignRacksFromSite(ctx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := s.siteStore.UnassignDevicesFromSite(ctx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := s.siteStore.DeleteCurtailmentResponseProfilesBySite(ctx, orgID, site.ID); err != nil {
			return err
		}
		if _, err := s.siteStore.SoftDeleteInfrastructureDevicesBySite(ctx, orgID, site.ID); err != nil {
			return err
		}
		rowsAffected, err := s.siteStore.SoftDeleteSite(ctx, orgID, site.ID)
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return fleeterror.NewNotFoundErrorf("site %d not found", site.ID)
		}
	}
	return nil
}

func (s *Service) logSiteMapImportActivity(ctx context.Context, orgID int64, plan importPlan) {
	if s.activitySvc == nil || len(plan.changes) == 0 {
		return
	}
	scopeType := "site_map"
	changeCount := 0
	for _, change := range plan.changes {
		changeCount += int(change.GetCount())
	}
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           "site_map_import",
		Description:    "Import site map CSV",
		ScopeType:      &scopeType,
		ScopeCount:     &changeCount,
		OrganizationID: &orgID,
		Metadata: map[string]any{
			"changes": siteMapImportActivityChanges(plan.changes),
		},
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)
}

func siteMapImportActivityChanges(changes []*pb.ImportChangeSummary) []map[string]any {
	out := make([]map[string]any, 0, len(changes))
	for _, change := range changes {
		out = append(out, map[string]any{
			"operation":   strings.ToLower(strings.TrimPrefix(change.GetOperation().String(), "IMPORT_OPERATION_")),
			"entity_type": change.GetEntityType(),
			"count":       change.GetCount(),
			"description": change.GetDescription(),
		})
	}
	return out
}

func (s *Service) applySiteRows(ctx context.Context, orgID int64, rows []map[string]string, existing map[string]sitemodels.Site) error {
	// Site-map CSV carries only the site identity. Existing site metadata is
	// intentionally left to the site editor; unknown site names are created.
	for _, row := range rows {
		if _, ok := existing[row["site"]]; ok {
			continue
		}
		site, err := s.siteStore.CreateSite(ctx, sitemodels.CreateSiteParams{
			OrgID: orgID,
			Name:  row["site"],
		})
		if err != nil {
			return err
		}
		existing[site.Name] = *site
	}
	return nil
}

func (s *Service) applyBuildingRows(
	ctx context.Context,
	orgID int64,
	rows []map[string]string,
	sitesByName map[string]sitemodels.Site,
	existing map[string]buildingmodels.Building,
) error {
	for _, row := range rows {
		building, ok := existing[row["site"]+"\x00"+row[fieldBuilding]]
		aisles, err := parseInt32Field(row, "aisles")
		if err != nil {
			return err
		}
		racksPerAisle, err := parseInt32Field(row, "racks_per_aisle")
		if err != nil {
			return err
		}
		if !ok {
			siteID, _, err := desiredSiteBuildingIDs(row["site"], "", sitesByName, nil)
			if err != nil {
				return err
			}
			created, err := s.buildingStore.CreateBuilding(ctx, buildingmodels.CreateParams{
				OrgID:         orgID,
				SiteID:        siteID,
				Name:          row[fieldBuilding],
				Aisles:        aisles,
				RacksPerAisle: racksPerAisle,
			})
			if err != nil {
				return err
			}
			created.SiteLabel = row["site"]
			existing[row["site"]+"\x00"+row[fieldBuilding]] = *created
			continue
		}
		current := rowMap(buildingHeaders, buildingRows([]buildingmodels.Building{building})[0])
		if rowsEqual(row, current, buildingHeaders) {
			continue
		}
		if _, err := s.buildingStore.UpdateBuilding(ctx, buildingmodels.UpdateParams{
			OrgID:                 orgID,
			ID:                    building.ID,
			Name:                  building.Name,
			Description:           building.Description,
			PowerKw:               building.PowerKw,
			OverheadKw:            building.OverheadKw,
			Aisles:                aisles,
			PhysicalRackCount:     building.PhysicalRackCount,
			RacksPerAisle:         racksPerAisle,
			DefaultRackRows:       building.DefaultRackRows,
			DefaultRackColumns:    building.DefaultRackColumns,
			DefaultRackOrderIndex: building.DefaultRackOrderIndex,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) applyRackRows(
	ctx context.Context,
	orgID int64,
	rows []map[string]string,
	sitesByName map[string]sitemodels.Site,
	buildingsByKey map[string]buildingmodels.Building,
	existing map[string]rackSnapshot,
) error {
	var pendingGridPositions []pendingRackGridPosition
	for _, row := range rows {
		rack, ok := existing[row["rack"]]
		rowsValue, err := parseInt32Field(row, "rows")
		if err != nil {
			return err
		}
		columnsValue, err := parseInt32Field(row, "columns")
		if err != nil {
			return err
		}
		orderIndex, err := parseRackOrderIndex(row["order_index"])
		if err != nil {
			return err
		}
		siteID, buildingID, err := desiredSiteBuildingIDs(row["site"], row[fieldBuilding], sitesByName, buildingsByKey)
		if err != nil {
			return err
		}
		if !ok {
			collection, err := s.collectionStore.CreateCollection(ctx, orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, row["rack"], "")
			if err != nil {
				return err
			}
			if err := s.collectionStore.CreateRackExtension(ctx, interfaces.CreateRackExtensionParams{
				OrgID:        orgID,
				CollectionID: collection.Id,
				Rows:         rowsValue,
				Columns:      columnsValue,
				OrderIndex:   int32(orderIndex),
				CoolingType:  int32(collectionpb.RackCoolingType_RACK_COOLING_TYPE_UNSPECIFIED),
				Zone:         row["zone"],
				SiteID:       siteID,
				BuildingID:   buildingID,
			}); err != nil {
				return err
			}
			rack = rackSnapshot{
				ID:              collection.Id,
				SiteID:          siteID,
				BuildingID:      buildingID,
				Site:            row["site"],
				Building:        row[fieldBuilding],
				Label:           row["rack"],
				Zone:            row["zone"],
				Rows:            rowsValue,
				Columns:         columnsValue,
				OrderIndex:      row["order_index"],
				AisleIndex:      row["aisle_index"],
				PositionInAisle: row["position_in_aisle"],
			}
			existing[row["rack"]] = rack
			aisleIndex, positionInAisle, err := desiredRackGridPosition(row)
			if err != nil {
				return err
			}
			pendingGridPositions = append(pendingGridPositions, pendingRackGridPosition{
				rackID:          rack.ID,
				aisleIndex:      aisleIndex,
				positionInAisle: positionInAisle,
			})
			continue
		}
		current := rowMap(rackHeaders, rackRows([]rackSnapshot{rack})[0])
		if rowsEqual(row, current, rackHeaders) {
			continue
		}
		coolingType, err := parseRackCoolingType(rack.CoolingType)
		if err != nil {
			return err
		}
		finalZone := desiredRackZone(row, rack)
		if err := s.collectionStore.UpdateRackInfo(ctx, rack.ID, finalZone, rowsValue, columnsValue, int32(orderIndex), int32(coolingType), orgID); err != nil {
			return err
		}
		if err := s.collectionStore.UpdateRackPlacement(ctx, rack.ID, orgID, siteID, buildingID, finalZone); err != nil {
			return err
		}
		if !nullableInt64Equal(siteID, rack.SiteID) {
			if _, err := s.collectionStore.CascadeRackDeviceSites(ctx, rack.ID, orgID, siteID); err != nil {
				return err
			}
		}
		if !nullableInt64Equal(buildingID, rack.BuildingID) {
			if _, err := s.collectionStore.CascadeRackDeviceBuildings(ctx, rack.ID, orgID, buildingID); err != nil {
				return err
			}
		}
		aisleIndex, positionInAisle, err := desiredRackGridPosition(row)
		if err != nil {
			return err
		}
		pendingGridPositions = append(pendingGridPositions, pendingRackGridPosition{
			rackID:          rack.ID,
			aisleIndex:      aisleIndex,
			positionInAisle: positionInAisle,
		})
	}
	if len(pendingGridPositions) == 0 {
		return nil
	}
	rackIDs := make([]int64, 0, len(pendingGridPositions))
	for _, position := range pendingGridPositions {
		rackIDs = append(rackIDs, position.rackID)
	}
	if err := s.buildingStore.SetRackBuildingPositionBulkClear(ctx, orgID, rackIDs); err != nil {
		return err
	}
	placedRackIDs := make([]int64, 0, len(pendingGridPositions))
	aisleIndexes := make([]int32, 0, len(pendingGridPositions))
	positionsInAisle := make([]int32, 0, len(pendingGridPositions))
	for _, position := range pendingGridPositions {
		if position.aisleIndex == nil || position.positionInAisle == nil {
			continue
		}
		placedRackIDs = append(placedRackIDs, position.rackID)
		aisleIndexes = append(aisleIndexes, *position.aisleIndex)
		positionsInAisle = append(positionsInAisle, *position.positionInAisle)
	}
	if len(placedRackIDs) > 0 {
		if err := s.buildingStore.SetRackBuildingPositionBulkPlace(ctx, orgID, placedRackIDs, aisleIndexes, positionsInAisle); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) applyMinerRows(
	ctx context.Context,
	orgID int64,
	rows []map[string]string,
	buildingRows []map[string]string,
	buildings []buildingmodels.Building,
	sitesByName map[string]sitemodels.Site,
	buildingsByKey map[string]buildingmodels.Building,
	racksByLabel map[string]rackSnapshot,
	existing map[string]minerSnapshot,
) error {
	buildingsByName, ambiguousBuildings := desiredBuildingNameLookup(buildingRows, buildings)
	var pendingSlots []pendingMinerSlot
	for _, row := range rows {
		miner, ok := existing[row["device_identifier"]]
		if !ok {
			continue
		}
		desiredSite, desiredBuilding := desiredMinerSiteBuilding(row, racksByLabel, buildingsByName, ambiguousBuildings)
		if desiredSite == miner.Site && desiredBuilding == miner.Building && row["rack"] == miner.Rack && row["rack_row"] == miner.RackRow && row["rack_col"] == miner.RackCol {
			continue
		}
		deviceIDs := []string{row["device_identifier"]}
		if row["rack"] != "" {
			rack, ok := racksByLabel[row["rack"]]
			if !ok {
				return fleeterror.NewFailedPreconditionErrorf("unknown rack %q for miner %q", row["rack"], row["device_identifier"])
			}
			if _, err := s.collectionStore.LockRacksForReparent(ctx, orgID, deviceIDs, rack.ID); err != nil {
				return err
			}
			if _, err := s.collectionStore.LockRackPlacementForWrite(ctx, rack.ID, orgID); err != nil {
				return err
			}
			if _, err := s.collectionStore.RemoveDevicesFromAnyRack(ctx, orgID, deviceIDs, rack.ID); err != nil {
				return err
			}
			if _, err := s.collectionStore.AddDevicesToCollection(ctx, orgID, rack.ID, deviceIDs); err != nil {
				return err
			}
			if _, err := s.collectionStore.CascadeAddedDeviceSites(ctx, orgID, rack.ID, deviceIDs); err != nil {
				return err
			}
			if _, err := s.collectionStore.CascadeAddedDeviceBuildings(ctx, orgID, rack.ID, deviceIDs); err != nil {
				return err
			}
			pendingSlots = append(pendingSlots, pendingMinerSlot{
				rackID:           rack.ID,
				deviceIdentifier: row["device_identifier"],
				row:              row["rack_row"],
				col:              row["rack_col"],
			})
			continue
		}

		if _, err := s.collectionStore.LockRacksForReparent(ctx, orgID, deviceIDs, 0); err != nil {
			return err
		}
		if _, err := s.collectionStore.RemoveDevicesFromAnyRack(ctx, orgID, deviceIDs, 0); err != nil {
			return err
		}
		siteID, buildingID, err := desiredSiteBuildingIDs(desiredSite, desiredBuilding, sitesByName, buildingsByKey)
		if err != nil {
			return err
		}
		if _, err := s.siteStore.AssignDevicesToSite(ctx, orgID, siteID, deviceIDs); err != nil {
			return err
		}
		if _, err := s.buildingStore.AssignDevicesToBuilding(ctx, orgID, buildingID, deviceIDs); err != nil {
			return err
		}
	}
	for _, slot := range pendingSlots {
		if err := s.collectionStore.ClearRackSlotPosition(ctx, slot.rackID, slot.deviceIdentifier, orgID); err != nil {
			return err
		}
	}
	for _, slot := range pendingSlots {
		if slot.row == "" && slot.col == "" {
			continue
		}
		rackRow, err := parseInt32Value(slot.row, "rack_row")
		if err != nil {
			return err
		}
		rackCol, err := parseInt32Value(slot.col, "rack_col")
		if err != nil {
			return err
		}
		if err := s.collectionStore.SetRackSlotPosition(ctx, slot.rackID, slot.deviceIdentifier, rackRow, rackCol, orgID); err != nil {
			return err
		}
	}
	return nil
}

func commitToken(parsed *parsedCSV, mode pb.OmissionMode, plan importPlan, snap *snapshot) string {
	payload := mustMarshalJSON(struct {
		Parsed              *parsedCSV
		Mode                pb.OmissionMode
		Omissions           *pb.OmissionCounts
		Changes             []*pb.ImportChangeSummary
		SnapshotFingerprint string
	}{
		Parsed:              parsed,
		Mode:                mode,
		Omissions:           plan.omissions,
		Changes:             plan.changes,
		SnapshotFingerprint: snapshotFingerprint(snap),
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func snapshotFingerprint(snap *snapshot) string {
	payload := mustMarshalJSON(struct {
		Sites             [][]string
		Buildings         [][]string
		Racks             [][]string
		Miners            [][]string
		HiddenRackMembers [][]string
	}{
		Sites:             siteRows(snap.sites),
		Buildings:         buildingRows(snap.buildings),
		Racks:             rackRows(snap.racks),
		Miners:            minerRows(snap.miners, snap.buildings),
		HiddenRackMembers: hiddenRackMemberRows(snap.hiddenRackMembers),
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func hiddenRackMemberRows(miners []minerSnapshot) [][]string {
	rows := make([][]string, 0, len(miners))
	for _, miner := range miners {
		rows = append(rows, []string{
			miner.DeviceIdentifier,
			miner.Site,
			miner.Building,
			miner.Rack,
			miner.RackRow,
			miner.RackCol,
		})
	}
	return rows
}

func mustMarshalJSON(value any) []byte {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("site map token payload must be JSON-marshalable: %v", err))
	}
	return payload
}

func hasOmissions(c *pb.OmissionCounts) bool {
	return c != nil && (c.GetSites() > 0 || c.GetBuildings() > 0 || c.GetRacks() > 0 || c.GetMiners() > 0)
}

func trimRecord(record []string) []string {
	out := make([]string, len(record))
	for i, field := range record {
		out[i] = strings.TrimSpace(field)
	}
	return out
}

func isBlankRecord(record []string) bool {
	for _, field := range record {
		if strings.TrimSpace(field) != "" {
			return false
		}
	}
	return true
}

func isSectionMarker(record []string) bool {
	if len(record) == 0 || !strings.HasPrefix(record[0], "# SECTION: ") {
		return false
	}
	for _, field := range record[1:] {
		if strings.TrimSpace(field) != "" {
			return false
		}
	}
	return true
}

func trimTrailingEmpty(record []string) []string {
	end := len(record)
	for end > 0 && strings.TrimSpace(record[end-1]) == "" {
		end--
	}
	return record[:end]
}

func trimTrailingEmptyToMax(record []string, maxLen int) []string {
	end := len(record)
	for end > maxLen && strings.TrimSpace(record[end-1]) == "" {
		end--
	}
	return record[:end]
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func csvErr(row int, section, message string) *pb.ImportValidationError {
	return &pb.ImportValidationError{Row: safeInt32(row), Section: section, Message: message}
}

func rowSet(rows []map[string]string, key string) map[string]bool {
	out := map[string]bool{}
	for _, row := range rows {
		if row[key] != "" {
			out[row[key]] = true
		}
	}
	return out
}

func compoundRowSet(rows []map[string]string, a, b string) map[string]bool {
	out := map[string]bool{}
	for _, row := range rows {
		if row[b] != "" {
			out[row[a]+"\x00"+row[b]] = true
		}
	}
	return out
}

func validateUnique(rows []map[string]string, section, key string) []*pb.ImportValidationError {
	seen := map[string]bool{}
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		value := row[key]
		if value == "" {
			errs = append(errs, csvErr(rowNumber(row, i+1), section, key+" is required"))
			continue
		}
		if seen[value] {
			errs = append(errs, csvErr(rowNumber(row, i+1), section, "duplicate "+key))
		}
		seen[value] = true
	}
	return errs
}

func validateUniqueCompound(rows []map[string]string, section, a, b string) []*pb.ImportValidationError {
	seen := map[string]bool{}
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		if row[a] == "" || row[b] == "" {
			errs = append(errs, csvErr(rowNumber(row, i+1), section, a+" and "+b+" are required"))
			continue
		}
		key := row[a] + "\x00" + row[b]
		if seen[key] {
			errs = append(errs, csvErr(rowNumber(row, i+1), section, "duplicate "+a+"/"+b))
		}
		seen[key] = true
	}
	return errs
}

func validateUniqueBuildingRows(rows []map[string]string) []*pb.ImportValidationError {
	seen := map[string]bool{}
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		if row[fieldBuilding] == "" {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", "building is required"))
			continue
		}
		key := row["site"] + "\x00" + row[fieldBuilding]
		if seen[key] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", "duplicate site/building"))
		}
		seen[key] = true
	}
	return errs
}

func validateRemoveOmittedReferences(parsed *parsedCSV, mode pb.OmissionMode) []*pb.ImportValidationError {
	if mode != pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
		return nil
	}
	siteRows := parsed.sections["SITE"]
	buildingRows := parsed.sections["BUILDING"]
	rackRows := parsed.sections["RACK"]
	minerRows := parsed.sections["MINER"]

	presentSites := rowSet(siteRows, "site")
	presentBuildings := compoundRowSet(buildingRows, "site", fieldBuilding)
	presentRacks := rowSet(rackRows, "rack")
	buildingsByName, ambiguousBuildings := desiredBuildingNameLookup(buildingRows, nil)

	var errs []*pb.ImportValidationError
	for i, row := range buildingRows {
		if row["site"] != "" && !presentSites[row["site"]] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", fmt.Sprintf("building site %q is omitted; add the SITE row or choose leave omitted rows in place", row["site"])))
		}
	}
	for i, row := range rackRows {
		if row["site"] != "" && !presentSites[row["site"]] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("rack site %q is omitted; add the SITE row or choose leave omitted rows in place", row["site"])))
		}
		if row[fieldBuilding] == "" {
			continue
		}
		if row["site"] != "" {
			if !presentBuildings[row["site"]+"\x00"+row[fieldBuilding]] {
				errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("rack building %q for site %q is omitted; add the BUILDING row or choose leave omitted rows in place", row[fieldBuilding], row["site"])))
			}
			continue
		}
		if ambiguousBuildings[row[fieldBuilding]] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("rack building %q is ambiguous; add site", row[fieldBuilding])))
			continue
		}
		if _, ok := buildingsByName[row[fieldBuilding]]; !ok {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("rack building %q is omitted; add the BUILDING row or choose leave omitted rows in place", row[fieldBuilding])))
		}
	}
	for i, row := range minerRows {
		if row["rack"] != "" {
			if !presentRacks[row["rack"]] {
				errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("miner rack %q is omitted; add the RACK row or choose leave omitted rows in place", row["rack"])))
			}
			continue
		}
		if row[fieldBuilding] != "" {
			if row["site"] != "" {
				if !presentBuildings[row["site"]+"\x00"+row[fieldBuilding]] {
					errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("miner building %q for site %q is omitted; add the BUILDING row or choose leave omitted rows in place", row[fieldBuilding], row["site"])))
				}
				continue
			}
			if ambiguousBuildings[row[fieldBuilding]] {
				errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("miner building %q is ambiguous; add site", row[fieldBuilding])))
				continue
			}
			if _, ok := buildingsByName[row[fieldBuilding]]; !ok {
				errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("miner building %q is omitted; add the BUILDING row or choose leave omitted rows in place", row[fieldBuilding])))
			}
			continue
		}
		if row["site"] != "" && !presentSites[row["site"]] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("miner site %q is omitted; add the SITE row or choose leave omitted rows in place", row["site"])))
		}
	}
	return errs
}

func validateKnownMiners(rows []map[string]string, snap *snapshot) []*pb.ImportValidationError {
	known := rowSetFromMiners(snap.miners)
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		if row["device_identifier"] != "" && !known[row["device_identifier"]] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", "unknown miner device_identifier"))
		}
	}
	return errs
}

func validateReadOnlyMinerFields(rows []map[string]string, snap *snapshot) []*pb.ImportValidationError {
	known := minerMap(snap.miners)
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		miner, ok := known[row["device_identifier"]]
		if !ok {
			continue
		}
		for _, field := range []struct {
			name string
			want string
		}{
			{name: "serial_number", want: miner.SerialNumber},
			{name: "name", want: miner.Name},
			{name: "ip_address", want: miner.IPAddress},
			{name: "mac_address", want: miner.MACAddress},
		} {
			if row[field.name] != field.want {
				errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("%s is read-only for existing miner %s", field.name, row["device_identifier"])))
			}
		}
	}
	return errs
}

func validateBuildingSiteTargets(rows, siteRows []map[string]string, snap *snapshot) []*pb.ImportValidationError {
	existingSites := desiredSiteSet(siteRows, snap.sites)
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		if row["site"] != "" && !existingSites[row["site"]] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", fmt.Sprintf("unknown site %q", row["site"])))
		}
	}
	return errs
}

func validateRackPlacementTargets(rows, buildingRows, siteRows []map[string]string, snap *snapshot) []*pb.ImportValidationError {
	existingSites := desiredSiteSet(siteRows, snap.sites)
	existingBuildings := rowSetFromDesiredBuildings(buildingRows, snap.buildings)
	buildingsByName, ambiguousBuildings := desiredBuildingNameLookup(buildingRows, snap.buildings)
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		if row["site"] != "" && !existingSites[row["site"]] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("unknown site %q", row["site"])))
		}
		if row[fieldBuilding] != "" {
			if row["site"] == "" {
				if ambiguousBuildings[row[fieldBuilding]] {
					errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("rack building %q is ambiguous; add site", row[fieldBuilding])))
					continue
				}
				if _, ok := buildingsByName[row[fieldBuilding]]; !ok {
					errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("unknown building %q", row[fieldBuilding])))
				}
				continue
			}
			key := row["site"] + "\x00" + row[fieldBuilding]
			if !existingBuildings[key] {
				errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("unknown building %q for site %q", row[fieldBuilding], row["site"])))
			}
		}
	}
	return errs
}

func validatePlacementConsistency(minerRows, rackRows, buildingRows, siteRows []map[string]string, snap *snapshot) []*pb.ImportValidationError {
	racks := desiredRackMap(rackRows, snap.racks)
	buildingsByName, ambiguousBuildings := desiredBuildingNameLookup(buildingRows, snap.buildings)
	buildingsByKey := desiredBuildingMap(buildingRows, snap.buildings)
	existingSites := desiredSiteSet(siteRows, snap.sites)
	var errs []*pb.ImportValidationError
	for i, row := range minerRows {
		if row["rack"] != "" {
			rack, ok := racks[row["rack"]]
			if !ok {
				errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("unknown rack %q", row["rack"])))
				continue
			}
			if row["site"] != "" && row["site"] != rack.Site {
				errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("miner site %q does not match rack site %q", row["site"], rack.Site)))
			}
			if row[fieldBuilding] != "" && row[fieldBuilding] != rack.Building {
				errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("miner building %q does not match rack building %q", row[fieldBuilding], rack.Building)))
			}
			continue
		}
		if row[fieldBuilding] != "" {
			if row["site"] != "" {
				building, ok := buildingsByKey[row["site"]+"\x00"+row[fieldBuilding]]
				if !ok {
					errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("unknown building %q for site %q", row[fieldBuilding], row["site"])))
					continue
				}
				if row["site"] != building.SiteLabel {
					errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("miner site %q does not match building site %q", row["site"], building.SiteLabel)))
				}
				continue
			}
			if ambiguousBuildings[row[fieldBuilding]] {
				errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("miner building %q is ambiguous; add site", row[fieldBuilding])))
				continue
			}
			building, ok := buildingsByName[row[fieldBuilding]]
			if !ok {
				errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("unknown building %q", row[fieldBuilding])))
				continue
			}
			if row["site"] != "" && row["site"] != building.SiteLabel {
				errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("miner site %q does not match building site %q", row["site"], building.SiteLabel)))
			}
			continue
		}
		if row["site"] != "" && !existingSites[row["site"]] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("unknown site %q", row["site"])))
		}
	}
	return errs
}

func validateBuildingLayoutBounds(rows []map[string]string) []*pb.ImportValidationError {
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		aisles, err := parseInt32Value(row["aisles"], "aisles")
		if err != nil {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", err.Error()))
			continue
		}
		racksPerAisle, err := parseInt32Value(row["racks_per_aisle"], "racks_per_aisle")
		if err != nil {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", err.Error()))
			continue
		}
		if aisles < 0 {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", fmt.Sprintf("aisles must be non-negative (got %d)", aisles)))
		}
		if aisles > maxLayoutDimension {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", fmt.Sprintf("aisles must be at most %d (got %d)", maxLayoutDimension, aisles)))
		}
		if racksPerAisle < 0 {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", fmt.Sprintf("racks_per_aisle must be non-negative (got %d)", racksPerAisle)))
		}
		if racksPerAisle > maxLayoutDimension {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", fmt.Sprintf("racks_per_aisle must be at most %d (got %d)", maxLayoutDimension, racksPerAisle)))
		}
	}
	return errs
}

func validateRackDimensions(rows []map[string]string) []*pb.ImportValidationError {
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		rackRows, err := parseInt32Value(row["rows"], "rows")
		if err != nil {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", err.Error()))
			continue
		}
		rackCols, err := parseInt32Value(row["columns"], "columns")
		if err != nil {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", err.Error()))
			continue
		}
		if rackRows < 1 || rackRows > maxRackDimension {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("rows must be between 1 and %d", maxRackDimension)))
		}
		if rackCols < 1 || rackCols > maxRackDimension {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("columns must be between 1 and %d", maxRackDimension)))
		}
		if _, err := parseRackOrderIndex(row["order_index"]); err != nil {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", err.Error()))
		}
	}
	return errs
}

func validateRackGridPositions(rackRows, buildingRows []map[string]string, snap *snapshot) []*pb.ImportValidationError {
	buildings := desiredBuildingMap(buildingRows, snap.buildings)
	var errs []*pb.ImportValidationError
	for i, row := range rackRows {
		aisleRaw := row["aisle_index"]
		positionRaw := row["position_in_aisle"]
		if (aisleRaw == "") != (positionRaw == "") {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", "aisle_index and position_in_aisle must both be set or both be blank"))
			continue
		}
		if aisleRaw == "" {
			continue
		}
		if row[fieldBuilding] == "" {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", "rack grid position requires a building"))
			continue
		}
		aisle, err := parseInt32Value(aisleRaw, "aisle_index")
		if err != nil {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", err.Error()))
			continue
		}
		position, err := parseInt32Value(positionRaw, "position_in_aisle")
		if err != nil {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", err.Error()))
			continue
		}
		if aisle < 0 {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("aisle_index %d is out of bounds", aisle)))
			continue
		}
		if position < 0 {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("position_in_aisle %d is out of bounds", position)))
			continue
		}
		building, ok := buildings[row["site"]+"\x00"+row[fieldBuilding]]
		if !ok {
			continue
		}
		if building.Aisles <= 0 || aisle >= building.Aisles {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("aisle_index %d is out of bounds for building %q with %d aisles", aisle, building.Name, building.Aisles)))
		}
		if building.RacksPerAisle <= 0 || position >= building.RacksPerAisle {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("position_in_aisle %d is out of bounds for building %q with %d racks per aisle", position, building.Name, building.RacksPerAisle)))
		}
	}
	return errs
}

func validateRackGridCollisions(rackRows []map[string]string, snap *snapshot, mode pb.OmissionMode) []*pb.ImportValidationError {
	presentRacks := rowSet(rackRows, "rack")
	seen := map[string]string{}
	var errs []*pb.ImportValidationError
	for _, rack := range snap.racks {
		if presentRacks[rack.Label] || mode == pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
			continue
		}
		if rack.Building == "" || rack.AisleIndex == "" || rack.PositionInAisle == "" {
			continue
		}
		aisle, err := parseInt32Value(rack.AisleIndex, "aisle_index")
		if err != nil {
			continue
		}
		position, err := parseInt32Value(rack.PositionInAisle, "position_in_aisle")
		if err != nil {
			continue
		}
		key := fmt.Sprintf("%s\x00%s\x00%d\x00%d", rack.Site, rack.Building, aisle, position)
		seen[key] = rack.Label
	}
	for i, row := range rackRows {
		if row[fieldBuilding] == "" || row["aisle_index"] == "" || row["position_in_aisle"] == "" {
			continue
		}
		aisle, err := parseInt32Value(row["aisle_index"], "aisle_index")
		if err != nil {
			continue
		}
		position, err := parseInt32Value(row["position_in_aisle"], "position_in_aisle")
		if err != nil {
			continue
		}
		key := fmt.Sprintf("%s\x00%s\x00%d\x00%d", row["site"], row[fieldBuilding], aisle, position)
		if existingRack, ok := seen[key]; ok {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("rack grid cell already occupied by rack %s", existingRack)))
			continue
		}
		seen[key] = row["rack"]
	}
	return errs
}

func validateRackSlotBounds(minerRows, rackRows []map[string]string, snap *snapshot) []*pb.ImportValidationError {
	racks := desiredRackMap(rackRows, snap.racks)
	var errs []*pb.ImportValidationError
	for i, row := range minerRows {
		if row["rack"] == "" {
			if row["rack_row"] != "" || row["rack_col"] != "" {
				errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", "rack_row and rack_col require rack"))
			}
			continue
		}
		if (row["rack_row"] == "") != (row["rack_col"] == "") {
			errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", "rack_row and rack_col must both be set or both be blank"))
			continue
		}
		if row["rack_row"] == "" {
			continue
		}
		rack, ok := racks[row["rack"]]
		if !ok {
			continue
		}
		rackRow, err := parseInt32Value(row["rack_row"], "rack_row")
		if err != nil {
			errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", err.Error()))
			continue
		}
		rackCol, err := parseInt32Value(row["rack_col"], "rack_col")
		if err != nil {
			errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", err.Error()))
			continue
		}
		if rackRow < 0 || rack.Rows <= 0 || rackRow >= rack.Rows {
			errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("rack_row %d is out of bounds for rack %q with %d rows", rackRow, row["rack"], rack.Rows)))
		}
		if rackCol < 0 || rack.Columns <= 0 || rackCol >= rack.Columns {
			errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("rack_col %d is out of bounds for rack %q with %d columns", rackCol, row["rack"], rack.Columns)))
		}
	}
	return errs
}

func validateExistingSlotsFitRackDimensions(minerRows, rackRows []map[string]string, snap *snapshot, mode pb.OmissionMode) []*pb.ImportValidationError {
	racks := desiredRackMap(rackRows, snap.racks)
	desiredMiners := map[string]map[string]string{}
	for _, row := range minerRows {
		desiredMiners[row["device_identifier"]] = row
	}
	var errs []*pb.ImportValidationError
	for _, miner := range snap.miners {
		row, ok := desiredMiners[miner.DeviceIdentifier]
		if !ok && mode == pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
			continue
		}
		rackLabel := miner.Rack
		rackRow := miner.RackRow
		rackCol := miner.RackCol
		if ok {
			rackLabel = row["rack"]
			rackRow = row["rack_row"]
			rackCol = row["rack_col"]
		}
		if rackLabel == "" || rackRow == "" || rackCol == "" {
			continue
		}
		rack, ok := racks[rackLabel]
		if !ok {
			continue
		}
		rowValue, err := parseInt32Value(rackRow, "rack_row")
		if err != nil {
			continue
		}
		colValue, err := parseInt32Value(rackCol, "rack_col")
		if err != nil {
			continue
		}
		if rowValue >= rack.Rows || colValue >= rack.Columns {
			errs = append(errs, csvErr(0, "MINER", fmt.Sprintf("miner %s slot %d,%d does not fit rack %q dimensions %dx%d", miner.DeviceIdentifier, rowValue, colValue, rackLabel, rack.Rows, rack.Columns)))
		}
	}
	for _, miner := range snap.hiddenRackMembers {
		if miner.Rack == "" || miner.RackRow == "" || miner.RackCol == "" {
			continue
		}
		rack, ok := racks[miner.Rack]
		if !ok {
			continue
		}
		rowValue, err := parseInt32Value(miner.RackRow, "rack_row")
		if err != nil {
			continue
		}
		colValue, err := parseInt32Value(miner.RackCol, "rack_col")
		if err != nil {
			continue
		}
		if rowValue >= rack.Rows || colValue >= rack.Columns {
			errs = append(errs, csvErr(0, "MINER", fmt.Sprintf("miner %s slot %d,%d does not fit rack %q dimensions %dx%d", miner.DeviceIdentifier, rowValue, colValue, miner.Rack, rack.Rows, rack.Columns)))
		}
	}
	return errs
}

func validateRackCapacity(minerRows, rackRows []map[string]string, snap *snapshot, mode pb.OmissionMode) []*pb.ImportValidationError {
	racks := desiredRackMap(rackRows, snap.racks)
	counts := map[string]int32{}
	presentMiners := rowSet(minerRows, "device_identifier")
	for _, row := range minerRows {
		if row["rack"] != "" {
			counts[row["rack"]]++
		}
	}
	if mode != pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
		for _, miner := range snap.miners {
			if miner.Rack != "" && !presentMiners[miner.DeviceIdentifier] {
				counts[miner.Rack]++
			}
		}
	}
	for _, miner := range snap.hiddenRackMembers {
		if miner.Rack != "" && !presentMiners[miner.DeviceIdentifier] {
			counts[miner.Rack]++
		}
	}
	var errs []*pb.ImportValidationError
	for rackLabel, count := range counts {
		rack, ok := racks[rackLabel]
		if !ok || rack.Rows <= 0 || rack.Columns <= 0 {
			continue
		}
		capacity := rack.Rows * rack.Columns
		if count > capacity {
			errs = append(errs, csvErr(0, "MINER", fmt.Sprintf("rack %q has %d assigned miners but capacity is %d", rackLabel, count, capacity)))
		}
	}
	return errs
}

func validateBuildingRackCapacity(rackRows, buildingRows []map[string]string, snap *snapshot) []*pb.ImportValidationError {
	buildings := desiredBuildingMap(buildingRows, snap.buildings)
	counts := map[string]int32{}
	for _, rack := range desiredRackMap(rackRows, snap.racks) {
		if rack.Building != "" {
			counts[rack.Site+"\x00"+rack.Building]++
		}
	}
	var errs []*pb.ImportValidationError
	for key, count := range counts {
		building, ok := buildings[key]
		if !ok || building.Aisles <= 0 || building.RacksPerAisle <= 0 {
			continue
		}
		capacity := building.Aisles * building.RacksPerAisle
		if count > capacity {
			errs = append(errs, csvErr(0, "RACK", fmt.Sprintf("building %q has %d assigned racks but capacity is %d", building.Name, count, capacity)))
		}
	}
	return errs
}

func validateBuildingExistingRacksFitLayout(rackRows, buildingRows []map[string]string, snap *snapshot, mode pb.OmissionMode) []*pb.ImportValidationError {
	buildings := desiredBuildingMap(buildingRows, snap.buildings)
	desiredRacks := desiredRackMap(rackRows, snap.racks)
	presentRacks := rowSet(rackRows, "rack")
	var errs []*pb.ImportValidationError
	for _, rack := range snap.racks {
		if !presentRacks[rack.Label] && mode == pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
			continue
		}
		desiredRack, ok := desiredRacks[rack.Label]
		if !ok {
			desiredRack = rack
		}
		if desiredRack.Building == "" || desiredRack.AisleIndex == "" || desiredRack.PositionInAisle == "" {
			continue
		}
		building, ok := buildings[desiredRack.Site+"\x00"+desiredRack.Building]
		if !ok {
			continue
		}
		aisle, err := parseInt32Value(desiredRack.AisleIndex, "aisle_index")
		if err != nil {
			continue
		}
		position, err := parseInt32Value(desiredRack.PositionInAisle, "position_in_aisle")
		if err != nil {
			continue
		}
		if aisle >= building.Aisles || position >= building.RacksPerAisle {
			errs = append(errs, csvErr(0, "RACK", fmt.Sprintf("rack %q grid position %d,%d does not fit building %q layout %dx%d", desiredRack.Label, aisle, position, building.Name, building.Aisles, building.RacksPerAisle)))
		}
	}
	return errs
}

func validateSlotCollisions(rows []map[string]string) []*pb.ImportValidationError {
	seen := map[string]bool{}
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		key, ok := normalizedRackSlotKey(row["rack"], row["rack_row"], row["rack_col"])
		if !ok {
			continue
		}
		if seen[key] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", "duplicate rack slot"))
		}
		seen[key] = true
	}
	return errs
}

func validateSlotConflictsWithExisting(rows []map[string]string, snap *snapshot, mode pb.OmissionMode) []*pb.ImportValidationError {
	desiredRows := map[string]map[string]string{}
	movingMiners := map[string]bool{}
	presentMiners := rowSet(rows, "device_identifier")
	for _, row := range rows {
		desiredRows[row["device_identifier"]] = row
	}
	for _, miner := range snap.miners {
		row, ok := desiredRows[miner.DeviceIdentifier]
		if !ok {
			continue
		}
		currentKey, _ := normalizedRackSlotKey(miner.Rack, miner.RackRow, miner.RackCol)
		desiredKey, _ := normalizedRackSlotKey(row["rack"], row["rack_row"], row["rack_col"])
		if row["rack"] != miner.Rack || desiredKey != currentKey {
			movingMiners[miner.DeviceIdentifier] = true
		}
	}

	currentOccupants := map[string]minerSnapshot{}
	for _, miner := range snap.miners {
		if !presentMiners[miner.DeviceIdentifier] && mode == pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
			continue
		}
		key, ok := normalizedRackSlotKey(miner.Rack, miner.RackRow, miner.RackCol)
		if !ok {
			continue
		}
		currentOccupants[key] = miner
	}
	for _, miner := range snap.hiddenRackMembers {
		key, ok := normalizedRackSlotKey(miner.Rack, miner.RackRow, miner.RackCol)
		if !ok {
			continue
		}
		currentOccupants[key] = miner
	}

	var errs []*pb.ImportValidationError
	for i, row := range rows {
		key, ok := normalizedRackSlotKey(row["rack"], row["rack_row"], row["rack_col"])
		if !ok {
			continue
		}
		occupant, ok := currentOccupants[key]
		if !ok || occupant.DeviceIdentifier == row["device_identifier"] || movingMiners[occupant.DeviceIdentifier] {
			continue
		}
		errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("rack slot already occupied by miner %s", occupant.DeviceIdentifier)))
	}
	return errs
}

func normalizedRackSlotKey(rack, row, col string) (string, bool) {
	if rack == "" || row == "" || col == "" {
		return "", false
	}
	rowValue, err := parseInt32Value(row, "rack_row")
	if err != nil {
		return "", false
	}
	colValue, err := parseInt32Value(col, "rack_col")
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("%s\x00%d\x00%d", rack, rowValue, colValue), true
}

func rowNumber(row map[string]string, fallback int) int {
	if value, err := strconv.Atoi(row["__row"]); err == nil && value > 0 {
		return value
	}
	return fallback
}

func safeInt32(value int) int32 {
	const maxInt32 = int64(1<<31 - 1)
	if value < 0 {
		return 0
	}
	if int64(value) > maxInt32 {
		return int32(maxInt32)
	}
	return int32(value) // #nosec G115 -- value is bounded above to MaxInt32.
}

func countCreates(existing, desired map[string]bool) int32 {
	var count int32
	for key := range desired {
		if !existing[key] {
			count++
		}
	}
	return count
}

func countDeletes(existing, desired map[string]bool) int32 {
	var count int32
	for key := range existing {
		if !desired[key] {
			count++
		}
	}
	return count
}

func countSiteUpdates(rows []map[string]string, sites []sitemodels.Site) int32 {
	existing := map[string]map[string]string{}
	for _, site := range sites {
		existing[site.Name] = rowMap(siteHeaders, siteRows([]sitemodels.Site{site})[0])
	}
	return countExistingRowUpdates(rows, existing, "site", siteHeaders)
}

func countBuildingUpdates(rows []map[string]string, buildings []buildingmodels.Building) int32 {
	existing := map[string]map[string]string{}
	for _, building := range buildings {
		existing[building.SiteLabel+"\x00"+building.Name] = rowMap(buildingHeaders, buildingRows([]buildingmodels.Building{building})[0])
	}
	return countExistingRowUpdates(rows, existing, "site\x00building", buildingHeaders)
}

func countRackUpdates(rows []map[string]string, racks []rackSnapshot) int32 {
	existing := map[string]map[string]string{}
	for _, rack := range racks {
		existing[rack.Label] = rowMap(rackHeaders, rackRows([]rackSnapshot{rack})[0])
	}
	return countExistingRowUpdates(rows, existing, "rack", rackHeaders)
}

func countMinerPlacementUpdates(rows, rackRows, buildingRows []map[string]string, snap *snapshot) int32 {
	existing := minerMap(snap.miners)
	racks := desiredRackMap(rackRows, snap.racks)
	buildingsByName, ambiguousBuildings := desiredBuildingNameLookup(buildingRows, snap.buildings)
	var count int32
	for _, row := range rows {
		miner, ok := existing[row["device_identifier"]]
		if !ok {
			continue
		}
		desiredSite, desiredBuilding := desiredMinerSiteBuilding(row, racks, buildingsByName, ambiguousBuildings)
		if desiredSite != miner.Site ||
			desiredBuilding != miner.Building ||
			row["rack"] != miner.Rack ||
			row["rack_row"] != miner.RackRow ||
			row["rack_col"] != miner.RackCol {
			count++
		}
	}
	return count
}

func countExistingRowUpdates(rows []map[string]string, existing map[string]map[string]string, keySpec string, headers []string) int32 {
	var count int32
	for _, row := range rows {
		existingRow, ok := existing[rowKey(row, keySpec)]
		if !ok {
			continue
		}
		for _, header := range headers {
			if row[header] != existingRow[header] {
				count++
				break
			}
		}
	}
	return count
}

func rowMap(headers, values []string) map[string]string {
	out := map[string]string{}
	for i, header := range headers {
		if i < len(values) {
			out[header] = values[i]
		}
	}
	return out
}

func minerMap(miners []minerSnapshot) map[string]minerSnapshot {
	out := map[string]minerSnapshot{}
	for _, miner := range miners {
		out[miner.DeviceIdentifier] = miner
	}
	return out
}

func desiredBuildingMap(rows []map[string]string, buildings []buildingmodels.Building) map[string]buildingmodels.Building {
	out := map[string]buildingmodels.Building{}
	for _, building := range buildings {
		out[building.SiteLabel+"\x00"+building.Name] = building
	}
	for _, row := range rows {
		key := row["site"] + "\x00" + row[fieldBuilding]
		building := out[key]
		building.SiteLabel = row["site"]
		building.Name = row[fieldBuilding]
		if aisles, err := parseInt32Value(row["aisles"], "aisles"); err == nil {
			building.Aisles = aisles
		}
		if racksPerAisle, err := parseInt32Value(row["racks_per_aisle"], "racks_per_aisle"); err == nil {
			building.RacksPerAisle = racksPerAisle
		}
		out[key] = building
	}
	return out
}

func desiredBuildingNameLookup(rows []map[string]string, buildings []buildingmodels.Building) (map[string]buildingmodels.Building, map[string]bool) {
	byName := map[string]buildingmodels.Building{}
	ambiguous := map[string]bool{}
	byBuildingName := map[string][]buildingmodels.Building{}
	for _, building := range desiredBuildingMap(rows, buildings) {
		byBuildingName[building.Name] = append(byBuildingName[building.Name], building)
	}
	for name, candidates := range byBuildingName {
		if len(candidates) == 1 {
			byName[name] = candidates[0]
			continue
		}
		var unassigned []buildingmodels.Building
		for _, building := range candidates {
			if building.SiteLabel == "" {
				unassigned = append(unassigned, building)
			}
		}
		if len(unassigned) == 1 {
			byName[name] = unassigned[0]
			continue
		}
		ambiguous[name] = true
	}
	return byName, ambiguous
}

func ambiguousBuildingLabels(buildings []buildingmodels.Building) map[string]bool {
	firstSiteByName := map[string]string{}
	ambiguous := map[string]bool{}
	for _, building := range buildings {
		if site, ok := firstSiteByName[building.Name]; ok {
			if site != building.SiteLabel {
				ambiguous[building.Name] = true
			}
			continue
		}
		firstSiteByName[building.Name] = building.SiteLabel
	}
	return ambiguous
}

func desiredRackMap(rows []map[string]string, racks []rackSnapshot) map[string]rackSnapshot {
	out := map[string]rackSnapshot{}
	for _, rack := range racks {
		out[rack.Label] = rack
	}
	for _, row := range rows {
		rack := out[row["rack"]]
		rack.Label = row["rack"]
		rack.Site = row["site"]
		rack.Building = row[fieldBuilding]
		rack.Zone = row["zone"]
		if rows, err := parseInt32Value(row["rows"], "rows"); err == nil {
			rack.Rows = rows
		}
		if columns, err := parseInt32Value(row["columns"], "columns"); err == nil {
			rack.Columns = columns
		}
		rack.OrderIndex = row["order_index"]
		rack.AisleIndex = row["aisle_index"]
		rack.PositionInAisle = row["position_in_aisle"]
		out[rack.Label] = rack
	}
	return out
}

func rowsEqual(a, b map[string]string, headers []string) bool {
	for _, header := range headers {
		if a[header] != b[header] {
			return false
		}
	}
	return true
}

func rowKey(row map[string]string, keySpec string) string {
	parts := strings.Split(keySpec, "\x00")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		values = append(values, row[part])
	}
	return strings.Join(values, "\x00")
}

func parseInt32Field(row map[string]string, field string) (int32, error) {
	return parseInt32Value(row[field], field)
}

func parseInt32Value(raw string, field string) (int32, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, fleeterror.NewInvalidArgumentErrorf("invalid %s value %q", field, raw)
	}
	return int32(value), nil
}

func parseRackOrderIndex(value string) (collectionpb.RackOrderIndex, error) {
	switch value {
	case "":
		return collectionpb.RackOrderIndex_RACK_ORDER_INDEX_UNSPECIFIED, nil
	case "BOTTOM_LEFT":
		return collectionpb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_LEFT, nil
	case "TOP_LEFT":
		return collectionpb.RackOrderIndex_RACK_ORDER_INDEX_TOP_LEFT, nil
	case "BOTTOM_RIGHT":
		return collectionpb.RackOrderIndex_RACK_ORDER_INDEX_BOTTOM_RIGHT, nil
	case "TOP_RIGHT":
		return collectionpb.RackOrderIndex_RACK_ORDER_INDEX_TOP_RIGHT, nil
	default:
		return collectionpb.RackOrderIndex_RACK_ORDER_INDEX_UNSPECIFIED, fleeterror.NewInvalidArgumentErrorf("invalid rack order index %q", value)
	}
}

func parseRackCoolingType(value string) (collectionpb.RackCoolingType, error) {
	switch value {
	case "":
		return collectionpb.RackCoolingType_RACK_COOLING_TYPE_UNSPECIFIED, nil
	case "AIR":
		return collectionpb.RackCoolingType_RACK_COOLING_TYPE_AIR, nil
	case "IMMERSION":
		return collectionpb.RackCoolingType_RACK_COOLING_TYPE_IMMERSION, nil
	default:
		return collectionpb.RackCoolingType_RACK_COOLING_TYPE_UNSPECIFIED, fleeterror.NewInvalidArgumentErrorf("invalid rack cooling type %q", value)
	}
}

func desiredRackGridPosition(row map[string]string) (*int32, *int32, error) {
	if row["aisle_index"] == "" && row["position_in_aisle"] == "" {
		return nil, nil, nil
	}
	aisle, err := parseInt32Value(row["aisle_index"], "aisle_index")
	if err != nil {
		return nil, nil, err
	}
	position, err := parseInt32Value(row["position_in_aisle"], "position_in_aisle")
	if err != nil {
		return nil, nil, err
	}
	return &aisle, &position, nil
}

func desiredSiteBuildingIDs(
	siteName string,
	buildingName string,
	sitesByName map[string]sitemodels.Site,
	buildingsByKey map[string]buildingmodels.Building,
) (*int64, *int64, error) {
	var siteID *int64
	if siteName != "" {
		site, ok := sitesByName[siteName]
		if !ok {
			return nil, nil, fleeterror.NewFailedPreconditionErrorf("unknown site %q", siteName)
		}
		siteID = &site.ID
	}
	var buildingID *int64
	if buildingName != "" {
		building, ok := buildingsByKey[siteName+"\x00"+buildingName]
		if !ok {
			return nil, nil, fleeterror.NewFailedPreconditionErrorf("unknown building %q at site %q", buildingName, siteName)
		}
		buildingID = &building.ID
	}
	return siteID, buildingID, nil
}

func desiredRackZone(row map[string]string, current rackSnapshot) string {
	if current.Building != "" && (row[fieldBuilding] != current.Building || row["site"] != current.Site) {
		return ""
	}
	return row["zone"]
}

func desiredMinerSiteBuilding(
	row map[string]string,
	racksByLabel map[string]rackSnapshot,
	buildingsByName map[string]buildingmodels.Building,
	ambiguousBuildings map[string]bool,
) (string, string) {
	if row["rack"] != "" {
		rack := racksByLabel[row["rack"]]
		return rack.Site, rack.Building
	}
	if row["site"] == "" && row[fieldBuilding] != "" && !ambiguousBuildings[row[fieldBuilding]] {
		if building, ok := buildingsByName[row[fieldBuilding]]; ok {
			return building.SiteLabel, row[fieldBuilding]
		}
	}
	return row["site"], row[fieldBuilding]
}

func rowSetFromSites(rows []sitemodels.Site) map[string]bool {
	out := map[string]bool{}
	for _, row := range rows {
		out[row.Name] = true
	}
	return out
}

func desiredSiteSet(rows []map[string]string, sites []sitemodels.Site) map[string]bool {
	out := rowSetFromSites(sites)
	for _, row := range rows {
		if row["site"] != "" {
			out[row["site"]] = true
		}
	}
	return out
}

func rowSetFromBuildings(rows []buildingmodels.Building) map[string]bool {
	out := map[string]bool{}
	for _, row := range rows {
		out[row.SiteLabel+"\x00"+row.Name] = true
	}
	return out
}

func rowSetFromDesiredBuildings(rows []map[string]string, buildings []buildingmodels.Building) map[string]bool {
	out := rowSetFromBuildings(buildings)
	for _, row := range rows {
		if row[fieldBuilding] != "" {
			out[row["site"]+"\x00"+row[fieldBuilding]] = true
		}
	}
	return out
}

func rowSetFromRacks(rows []rackSnapshot) map[string]bool {
	out := map[string]bool{}
	for _, row := range rows {
		out[row.Label] = true
	}
	return out
}

func rowSetFromMiners(rows []minerSnapshot) map[string]bool {
	out := map[string]bool{}
	for _, row := range rows {
		out[row.DeviceIdentifier] = true
	}
	return out
}

func omittedSites(rows []map[string]string, sites []sitemodels.Site) []sitemodels.Site {
	present := rowSet(rows, "site")
	var out []sitemodels.Site
	for _, site := range sites {
		if !present[site.Name] {
			out = append(out, site)
		}
	}
	return out
}

func omittedBuildings(rows []map[string]string, buildings []buildingmodels.Building) []buildingmodels.Building {
	present := compoundRowSet(rows, "site", fieldBuilding)
	var out []buildingmodels.Building
	for _, building := range buildings {
		if !present[building.SiteLabel+"\x00"+building.Name] {
			out = append(out, building)
		}
	}
	return out
}

func omittedRacks(rows []map[string]string, racks []rackSnapshot) []rackSnapshot {
	present := rowSet(rows, "rack")
	var out []rackSnapshot
	for _, rack := range racks {
		if !present[rack.Label] {
			out = append(out, rack)
		}
	}
	return out
}

func omittedMiners(rows []map[string]string, miners []minerSnapshot) []minerSnapshot {
	present := rowSet(rows, "device_identifier")
	var out []minerSnapshot
	for _, miner := range miners {
		if !present[miner.DeviceIdentifier] {
			out = append(out, miner)
		}
	}
	return out
}
