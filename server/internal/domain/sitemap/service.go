package sitemap

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/sitemap/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	buildingmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	fleetmanagementdomain "github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
	sitesdomain "github.com/block/proto-fleet/server/internal/domain/sites"
	sitemodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	maxPageSize        = 1000
	MaxImportBytes     = 64 * 1024 * 1024
	maxImportRows      = 100000
	maxRackDimension   = 12
	maxLayoutDimension = 100
	exportChunkBytes   = 64 * 1024

	maxSiteNameLength     = 255
	maxBuildingNameLength = 255
	maxRackLabelLength    = 100
	maxRackZoneLength     = 100
	maxMinerNameLength    = 100

	siteMapExportFolder       = "proto-fleet-site-map"
	siteMapExportCSVPath      = siteMapExportFolder + "/site-map.csv"
	siteMapExportGuideTXTPath = siteMapExportFolder + "/agent-editing-guide.txt"

	fieldID       = "id"
	fieldName     = "name"
	fieldLabel    = "label"
	fieldSite     = "site"
	fieldBuilding = "building"
	fieldRack     = "rack"

	// refCreatePrefix marks a reference cell that points at an entity being
	// created in the same import, by its own name/label (e.g. "NAME:Building 2").
	// A bare integer references an existing entity by id; blank is unassigned.
	refCreatePrefix = "NAME:"

	// refBuildingIDCell is an internal companion cell that resolveReferences writes
	// next to a canonicalized building reference to preserve the resolved existing
	// building's id. Canonicalizing a reference to a name loses id precision when
	// two buildings share a (site, name) pair (a distinct-NULL-site_id case the DB
	// permits), so id-sensitive validators read the companion cell to key by
	// identity. The NUL prefix keeps it out of the header-driven CSV export and
	// every field-scoped reader.
	refBuildingIDCell = "\x00ref_building_id"
)

var (
	siteHeaders = []string{
		fieldName, fieldID,
	}
	buildingHeaders = []string{
		fieldName, fieldID, fieldSite, "aisles", "racks_per_aisle",
	}
	rackHeaders = []string{
		fieldLabel, fieldID, fieldBuilding, fieldSite, "zone", "rows", "columns",
		"order_index", "aisle_index", "position_in_aisle",
	}
	minerHeaders = []string{
		"device_identifier", "serial_number", "name", "ip_address", "mac_address",
		fieldSite, fieldBuilding, fieldRack, "rack_row", "rack_col",
	}
	siteMapMinerPairingStatuses = []fleetpb.PairingStatus{
		fleetpb.PairingStatus_PAIRING_STATUS_PAIRED,
		fleetpb.PairingStatus_PAIRING_STATUS_UNPAIRED,
		fleetpb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
		fleetpb.PairingStatus_PAIRING_STATUS_PENDING,
		fleetpb.PairingStatus_PAIRING_STATUS_FAILED,
		fleetpb.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
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

type ImportPermissions struct {
	CanRenameMiners bool
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
	if err := writeSection("RACK", rackHeaders, rackExportRows(snapshot.racks)); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to write RACK section: %v", err)
	}
	if err := writeSection("MINER", minerHeaders, minerRows(snapshot.miners)); err != nil {
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
- Each row has a read-only "id" column that identifies an existing record. Do not edit it. A blank id means the row creates a new entity.
- Every parent relationship (a building's site, a rack's building or site, a miner's rack/building/site) is a single reference cell that accepts one of: blank (unassigned); a bare integer (an existing entity, referenced by its id); or "NAME:x" (an entity being created in this same import, referenced by its own name or label x). Any other value fails validation.
- Reference an existing entity by its id, and a same-import new entity by NAME:. To point a rack at an existing building, put that building's id in the rack's building cell. To point a rack at a building this import creates, put "NAME:" followed by that new building's name.
- Site, building, and rack rows are upserts: an id updates that entity, a blank id creates one. Miner rows must reference existing miners; unknown miner identifiers fail validation.

Omissions and renames
- Import preview reports omitted existing rows when a site, building, rack, or miner exists in Proto Fleet but is missing from the CSV.
- The user chooses omission handling during import. Leave omitted rows in place keeps missing rows unchanged. Remove omitted rows soft-deletes omitted sites, buildings, and racks, and unassigns omitted miners.
- Remove omitted rows can cascade placement cleanup: deleting an omitted site also deletes its buildings and unassigns racks/miners from that site; deleting an omitted building unassigns its racks and miners; deleting an omitted rack removes miners from that rack.
- With a populated id, editing a site/building name or rack label renames the existing entity.
- With a blank id, editing a site/building name or rack label creates a new entity and the old entity may be counted as omitted.
- Renaming or moving an entity does not break references to it: existing entities are referenced by id, which is stable across renames.
- Miner omission is different from miner deletion: miners cannot be created or deleted by site map import. With remove omitted rows, omitted miners are only unassigned from site/building/rack placement.

Formatting rules
- Keep the file as comma-separated CSV, not markdown or a table pasted into prose.
- Quote cells with commas, quotes, or newlines using normal CSV quoting rules.
- Use blank cells for empty values.
- Preserve apostrophe-prefixed values. They protect spreadsheet-sensitive text from formula/date conversion.
- Numeric indexes are zero-based integers unless noted otherwise.

SITE section
- Columns: name, id (read only).
- Add a new row with a blank id and new site name to create a site. Site details beyond the name are not editable through this import.
- Existing rows are matched by id when present. Editing name on an existing id renames the site.

BUILDING section
- Columns: name, id (read only), site, aisles, racks_per_aisle.
- Add a new row with a blank id and new name to create a building.
- site is a reference cell: blank (unassigned building), an existing site's id, or "NAME:" plus a same-import site's name.
- aisles and racks_per_aisle define rack layout capacity for the building.
- Existing rows are matched by id when present. Editing name on an existing id renames the building; changing site moves it.

RACK section
- Columns: label, id (read only), building, site, zone, rows, columns, order_index, aisle_index, position_in_aisle.
- Add a new row with a blank id and new rack label to create a rack. Rack labels must be unique across the organization.
- building and site are reference cells: blank, an existing entity's id, or "NAME:" plus a same-import entity's name/label.
- Set building to place a rack in a building; the building determines the site, so site may be left blank.
- Set site and leave building blank to assign a rack directly to a site.
- Leave both building and site blank to unassign a rack.
- zone is scoped to a building. Moving a rack to another building, directly to a site, or unassigned clears incompatible zone assignment.
- rows and columns define rack slot capacity. Each must be between 1 and 12.
- order_index controls physical slot ordering. Allowed values are BOTTOM_LEFT, TOP_LEFT, BOTTOM_RIGHT, TOP_RIGHT, or blank.
- aisle_index and position_in_aisle place a rack in the building layout. They must both be blank or both be set. Aisle and position indexes are zero-based and must fit within aisles and racks_per_aisle.

MINER section
- Columns: device_identifier (read only), serial_number (read only), name, ip_address (read only), mac_address (read only), site, building, rack, rack_row, rack_col.
- Edit name and placement columns in this section.
- device_identifier identifies the miner. Unknown miner identifiers fail validation.
- site, building, and rack are reference cells: blank, an existing entity's id, or "NAME:" plus a same-import entity's name/label.
- If rack is set, the rack determines the miner's building and site; leave building and site blank.
- If building is set and rack is blank, the building determines the miner's site; leave site blank.
- Set site and leave building and rack blank to assign a miner directly to a site.
- Leave site, building, and rack blank to unassign a miner.
- rack_row and rack_col must both be blank or both be set. They are zero-based and must fit within the rack's rows and columns.
- Multiple miners cannot end in the same rack slot. Slot swaps are valid when the final CSV has no duplicate slot positions.

Validation behavior
- Changing read-only identity fields fails validation.
- Assigning more racks to a building layout than aisles * racks_per_aisle fails validation.
- Assigning more miners to a rack than rows * columns fails validation.
- Assigning miners to rack slots outside the rack dimensions fails validation.
- Ambiguous names must be disambiguated with the parent placement column or the relevant *_id reference.
- The dry-run preview validates the entire CSV before commit. Fix all reported errors and run preview again before importing.
`
}

func (s *Service) ImportSiteMapCsv(ctx context.Context, orgID int64, req *pb.ImportSiteMapCsvRequest, permissions ...ImportPermissions) (*pb.ImportSiteMapCsvResponse, error) {
	if len(req.GetCsvData()) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("csv_data is required")
	}
	if len(req.GetCsvData()) > MaxImportBytes {
		return nil, fleeterror.NewInvalidArgumentErrorf("csv_data must be at most %d bytes", MaxImportBytes)
	}
	parsed, parseErrs := parseSiteMapCSV(req.GetCsvData())
	if len(parseErrs) > 0 {
		return &pb.ImportSiteMapCsvResponse{Errors: parseErrs}, nil
	}
	snapshot, err := s.loadSnapshot(ctx, orgID)
	if err != nil {
		return nil, err
	}
	importPermissions := ImportPermissions{}
	if len(permissions) > 0 {
		importPermissions = permissions[0]
	}
	plan := buildPlan(parsed, snapshot, req.GetOmissionMode())
	if !importPermissions.CanRenameMiners {
		plan.errors = append(plan.errors, validateMinerRenamePermission(plan.resolved.miners)...)
	}
	if req.GetOmissionMode() == pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
		impactErrs, err := s.validateOmittedSiteDeleteImpacts(ctx, orgID, omittedSites(parsed.sections["SITE"], snapshot.sites))
		if err != nil {
			return nil, err
		}
		plan.errors = append(plan.errors, impactErrs...)
	}
	if len(plan.errors) > 0 {
		return &pb.ImportSiteMapCsvResponse{
			OmissionCounts: plan.omissions,
			Errors:         plan.errors,
			Warnings:       plan.warnings,
		}, nil
	}
	if hasOmissions(plan.omissions) && req.GetOmissionMode() != pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
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
		if err := s.applyImportPlan(ctx, orgID, plan.resolved, parsed, snapshot, req.GetOmissionMode()); err != nil {
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
	SiteID           *int64
	Site             string
	BuildingID       *int64
	Building         string
	RackID           *int64
	Rack             string
	RackRow          string
	RackCol          string
}

type slotPosition struct {
	rackID     int64
	rack       string
	siteID     *int64
	site       string
	buildingID *int64
	building   string
	row        string
	col        string
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
						rackID:     collection.GetId(),
						rack:       collection.GetLabel(),
						siteID:     rack.SiteID,
						site:       rack.Site,
						buildingID: rack.BuildingID,
						building:   rack.Building,
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
			Filter:   &fleetpb.MinerListFilter{PairingStatuses: siteMapMinerPairingStatuses},
		})
		if err != nil {
			return nil, err
		}
		for _, miner := range resp.GetMiners() {
			site, building, rack := placementLabels3(miner.GetPlacement())
			siteID, buildingID, rackID := placementIDs3(miner.GetPlacement())
			slot := slots[miner.GetDeviceIdentifier()]
			if slot.rack != "" {
				siteID = slot.siteID
				site = slot.site
				buildingID = slot.buildingID
				building = slot.building
				rackID = &slot.rackID
				rack = slot.rack
			}
			miners = append(miners, minerSnapshot{
				DeviceIdentifier: miner.GetDeviceIdentifier(),
				SerialNumber:     miner.GetSerialNumber(),
				Name:             miner.GetName(),
				IPAddress:        miner.GetIpAddress(),
				MACAddress:       miner.GetMacAddress(),
				SiteID:           siteID,
				Site:             site,
				BuildingID:       buildingID,
				Building:         building,
				RackID:           rackID,
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
			SiteID:           slot.siteID,
			Site:             slot.site,
			BuildingID:       slot.buildingID,
			Building:         slot.building,
			RackID:           &slot.rackID,
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
	if section == "MINER" {
		switch header {
		case "device_identifier", "serial_number", "ip_address", "mac_address":
			return header + " (read only)"
		}
	}
	if section == "SITE" || section == "BUILDING" || section == "RACK" {
		if header == fieldID {
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
			formatInt64(site.ID),
		})
	}
	return rows
}

func siteRawRows(sites []sitemodels.Site) [][]string {
	rows := make([][]string, 0, len(sites))
	for _, site := range sites {
		rows = append(rows, []string{
			site.Name,
			formatInt64(site.ID),
		})
	}
	return rows
}

// buildingRows emits id-authoritative BUILDING rows: the site reference cell
// holds the site id (blank for a site-less building).
func buildingRows(buildings []buildingmodels.Building) [][]string {
	rows := make([][]string, 0, len(buildings))
	for _, building := range buildings {
		rows = append(rows, []string{
			clean(building.Name),
			formatInt64(building.ID),
			formatNullableInt64(building.SiteID),
			formatInt32(building.Aisles),
			formatInt32(building.RacksPerAisle),
		})
	}
	return rows
}

// buildingRawRows is the uncleaned canonical building row used for change
// detection. Its site reference cell holds the resolved site name so it matches
// a parsed row after resolveReferences canonicalizes the id cell into a name.
func buildingRawRows(buildings []buildingmodels.Building) [][]string {
	rows := make([][]string, 0, len(buildings))
	for _, building := range buildings {
		rows = append(rows, []string{
			building.Name,
			formatInt64(building.ID),
			building.SiteLabel,
			formatInt32(building.Aisles),
			formatInt32(building.RacksPerAisle),
		})
	}
	return rows
}

// rackRawRows is the uncleaned canonical rack row used for change detection. Its
// building and site reference cells hold the resolved parent names so it matches
// a parsed row after resolveReferences canonicalizes the id cells into names.
func rackRawRows(racks []rackSnapshot) [][]string {
	rows := make([][]string, 0, len(racks))
	for _, rack := range racks {
		rows = append(rows, []string{
			rack.Label,
			formatInt64(rack.ID),
			rack.Building,
			rack.Site,
			rack.Zone,
			formatInt32(rack.Rows),
			formatInt32(rack.Columns),
			rack.OrderIndex,
			rack.AisleIndex,
			rack.PositionInAisle,
		})
	}
	return rows
}

// rackExportRows emits id-authoritative rack rows: the rack's most-specific
// parent is identified by id in a single reference cell — the building reference
// (which implies its site) or, when the rack sits directly under a site, the
// site reference.
func rackExportRows(racks []rackSnapshot) [][]string {
	rows := make([][]string, 0, len(racks))
	for _, rack := range racks {
		rows = append(rows, rackExportRow(rack))
	}
	return rows
}

func rackExportRow(rack rackSnapshot) []string {
	buildingRef, siteRef := rackParentIDCells(rack)
	return []string{
		clean(rack.Label),
		formatInt64(rack.ID),
		buildingRef,
		siteRef,
		clean(rack.Zone),
		formatInt32(rack.Rows),
		formatInt32(rack.Columns),
		rack.OrderIndex,
		rack.AisleIndex,
		rack.PositionInAisle,
	}
}

// rackParentIDCells returns the building and site reference cells for a rack,
// preferring the building (which implies its site) and falling back to a direct
// site assignment.
func rackParentIDCells(rack rackSnapshot) (buildingRef, siteRef string) {
	if rack.BuildingID != nil {
		return formatNullableInt64(rack.BuildingID), ""
	}
	if rack.SiteID != nil {
		return "", formatNullableInt64(rack.SiteID)
	}
	return "", ""
}

// minerRows emits id-authoritative miner rows: the miner's most-specific parent
// is identified by id in a single reference cell — rack, else building, else
// site.
func minerRows(miners []minerSnapshot) [][]string {
	rows := make([][]string, 0, len(miners))
	for _, miner := range miners {
		siteRef, buildingRef, rackRef := minerParentIDCells(miner)
		rows = append(rows, []string{
			clean(miner.DeviceIdentifier),
			clean(miner.SerialNumber),
			clean(miner.Name),
			clean(miner.IPAddress),
			clean(miner.MACAddress),
			siteRef,
			buildingRef,
			rackRef,
			miner.RackRow,
			miner.RackCol,
		})
	}
	return rows
}

// minerParentIDCells returns the site, building, and rack reference cells for a
// miner, emitting only the most-specific parent (rack implies building and site;
// building implies site).
func minerParentIDCells(miner minerSnapshot) (siteRef, buildingRef, rackRef string) {
	if miner.RackID != nil {
		return "", "", formatNullableInt64(miner.RackID)
	}
	if miner.BuildingID != nil {
		return "", formatNullableInt64(miner.BuildingID), ""
	}
	if miner.SiteID != nil {
		return formatNullableInt64(miner.SiteID), "", ""
	}
	return "", "", ""
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

func placementIDs3(refs *commonpb.PlacementRefs) (*int64, *int64, *int64) {
	if refs == nil {
		return nil, nil, nil
	}
	return placementID(refs.GetSite()), placementID(refs.GetBuilding()), placementID(refs.GetRack())
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

func formatInt64(value int64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}

func formatNullableInt64(value *int64) string {
	if value == nil {
		return ""
	}
	return formatInt64(*value)
}

func clean(value string) string {
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
	seenSections := map[string]bool{}
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
		seenSections[section] = true
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
			rawNext := records[i+1]
			trimmedNext := trimRecord(rawNext)
			if isSectionMarker(trimmedNext) {
				break
			}
			i++
			if isBlankRecord(trimmedNext) {
				continue
			}
			next := trimTrailingEmptyToMax(rawNext, len(headers))
			if len(next) != len(headers) {
				errs = append(errs, csvErr(i+1, section, "row has the wrong number of columns"))
				continue
			}
			row := map[string]string{}
			for j, header := range headers {
				row[header] = unescapeCleanedValue(next[j])
			}
			row["__row"] = strconv.Itoa(i + 1)
			out.sections[section] = append(out.sections[section], row)
		}
	}
	for section := range expected {
		if _, ok := out.sections[section]; !ok {
			if !seenSections[section] {
				errs = append(errs, csvErr(0, section, "missing section"))
			}
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
	resolved  *resolvedPlan
}

func buildPlan(parsed *parsedCSV, snap *snapshot, mode pb.OmissionMode) importPlan {
	referenceErrors := resolveReferences(parsed, snap)
	resolved := resolvePlan(parsed, snap, mode)
	plan := importPlan{omissions: resolved.omissions, resolved: resolved}

	plan.errors = append(plan.errors, validateUnique(parsed.sections["SITE"], "SITE", fieldName)...)
	plan.errors = append(plan.errors, validateUniqueBuildingRows(parsed.sections["BUILDING"])...)
	plan.errors = append(plan.errors, validateUnique(parsed.sections["RACK"], "RACK", fieldLabel)...)
	plan.errors = append(plan.errors, validateUnique(parsed.sections["MINER"], "MINER", "device_identifier")...)
	plan.errors = append(plan.errors, validateUniqueIDs(parsed.sections["SITE"], "SITE")...)
	plan.errors = append(plan.errors, validateUniqueIDs(parsed.sections["BUILDING"], "BUILDING")...)
	plan.errors = append(plan.errors, validateUniqueIDs(parsed.sections["RACK"], "RACK")...)
	plan.errors = append(plan.errors, validateSiteRenameTargets(parsed.sections["SITE"], snap.sites)...)
	plan.errors = append(plan.errors, validateUniqueAssignedBuildingRows(parsed.sections["BUILDING"])...)
	plan.errors = append(plan.errors, validateAmbiguousBlankIDBuildingRows(parsed.sections["BUILDING"], snap.buildings)...)
	plan.errors = append(plan.errors, validateBuildingMoveRenameTargets(parsed.sections["BUILDING"], snap.buildings)...)
	plan.errors = append(plan.errors, validateRackRenameTargets(parsed.sections["RACK"], snap.racks)...)
	plan.errors = append(plan.errors, validateImportedNameLengths(parsed)...)
	plan.errors = append(plan.errors, validateKnownEntityIDs(parsed, snap)...)
	plan.errors = append(plan.errors, referenceErrors...)
	plan.errors = append(plan.errors, validateRetainedTopologyUniqueness(parsed, snap, mode)...)
	removeReferenceErrors := validateRemoveOmittedReferences(parsed, mode)
	plan.errors = append(plan.errors, removeReferenceErrors...)
	plan.errors = append(plan.errors, validateKnownMiners(resolved.miners)...)
	plan.errors = append(plan.errors, validateReadOnlyMinerFields(resolved.miners)...)
	if len(removeReferenceErrors) == 0 {
		plan.errors = append(plan.errors, validateBuildingSiteTargets(resolved.buildings, resolved.topology)...)
		plan.errors = append(plan.errors, validateRackPlacementTargets(resolved.racks, resolved.topology)...)
		plan.errors = append(plan.errors, validatePlacementConsistency(resolved.miners, resolved.topology)...)
	}
	plan.errors = append(plan.errors, validateBuildingLayoutBounds(parsed.sections["BUILDING"])...)
	plan.errors = append(plan.errors, validateRackDimensions(parsed.sections["RACK"])...)
	plan.errors = append(plan.errors, validateRackGridPositions(resolved.racks, resolved.topology)...)
	plan.errors = append(plan.errors, validateRackGridCollisions(parsed.sections["RACK"], snap, mode)...)
	plan.errors = append(plan.errors, validateRackSlotBounds(resolved.miners, resolved.topology)...)
	plan.errors = append(plan.errors, validateExistingSlotsFitRackDimensions(resolved)...)
	plan.errors = append(plan.errors, validateRackCapacity(resolved)...)
	plan.errors = append(plan.errors, validateBuildingRackCapacity(resolved)...)
	plan.errors = append(plan.errors, validateBuildingExistingRacksFitLayout(resolved)...)
	plan.errors = append(plan.errors, validateSlotCollisions(resolved.miners)...)
	plan.errors = append(plan.errors, validateSlotConflictsWithExisting(resolved)...)
	if len(plan.errors) > 0 || (mode != pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED && hasOmissions(plan.omissions)) {
		return plan
	}

	plan.changes = computeChanges(resolved, mode)
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

func (s *Service) applyImportPlan(ctx context.Context, orgID int64, resolved *resolvedPlan, parsed *parsedCSV, snap *snapshot, omissionMode pb.OmissionMode) error {
	if s.transactor == nil {
		return fleeterror.NewInternalError("site map import requires a transactor")
	}

	sitesByName := map[string]sitemodels.Site{}
	sitesByID := map[int64]sitemodels.Site{}
	for _, site := range snap.sites {
		sitesByName[site.Name] = site
		sitesByID[site.ID] = site
	}
	buildingsByKey := map[string]buildingmodels.Building{}
	buildingsByID := map[int64]buildingmodels.Building{}
	for _, building := range snap.buildings {
		buildingsByKey[building.SiteLabel+"\x00"+building.Name] = building
		buildingsByID[building.ID] = building
	}
	racksByLabel := map[string]rackSnapshot{}
	racksByID := map[int64]rackSnapshot{}
	for _, rack := range snap.racks {
		racksByLabel[rack.Label] = rack
		racksByID[rack.ID] = rack
	}
	return s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		if err := s.applySites(txCtx, orgID, resolved.sites, sitesByName, sitesByID); err != nil {
			return err
		}
		if err := s.applyBuildings(txCtx, orgID, resolved.buildings, sitesByName, buildingsByKey, buildingsByID); err != nil {
			return err
		}
		if err := s.applyRacks(txCtx, orgID, resolved.racks, sitesByName, buildingsByKey, buildingsByID, racksByLabel, racksByID); err != nil {
			return err
		}
		if err := s.applyMiners(txCtx, orgID, resolved.miners, sitesByName, buildingsByKey, buildingsByID, racksByLabel); err != nil {
			return err
		}
		if omissionMode == pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
			return s.applyOmittedRows(txCtx, orgID, parsed, snap)
		}
		return nil
	})
}

func (s *Service) validateOmittedSiteDeleteImpacts(ctx context.Context, orgID int64, sites []sitemodels.Site) ([]*pb.ImportValidationError, error) {
	var errs []*pb.ImportValidationError
	for _, site := range sites {
		profileCount, err := s.siteStore.CountCurtailmentResponseProfilesBySite(ctx, orgID, site.ID)
		if err != nil {
			return nil, err
		}
		if profileCount > 0 {
			errs = append(errs, csvErr(0, "SITE", fmt.Sprintf("omitted site %q has curtailment response profiles; site map CSV v1 cannot remove hidden curtailment resources", site.Name)))
		}
		infrastructureCount, err := s.siteStore.CountInfrastructureDevicesBySite(ctx, orgID, site.ID)
		if err != nil {
			return nil, err
		}
		if infrastructureCount > 0 {
			errs = append(errs, csvErr(0, "SITE", fmt.Sprintf("omitted site %q has infrastructure devices; site map CSV v1 cannot remove hidden infrastructure resources", site.Name)))
		}
	}
	return errs, nil
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
		infrastructureDeviceIDs, err := s.siteStore.LockInfrastructureDevicesBySiteForWrite(ctx, orgID, site.ID)
		if err != nil {
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
		referencingProfileCount, err := s.siteStore.CountResponseProfilesByInfrastructureDevices(ctx, orgID, infrastructureDeviceIDs)
		if err != nil {
			return err
		}
		if referencingProfileCount > 0 {
			return fleeterror.NewFailedPreconditionError(
				"infrastructure devices at this site are referenced by curtailment response profiles; update those profiles first",
			)
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

// applySites walks the resolved site nodes and enacts each node's classified
// action. Site-map CSV carries only the site identity; existing site metadata is
// intentionally left to the site editor.
func (s *Service) applySites(ctx context.Context, orgID int64, sites []*resolvedSite, existingByName map[string]sitemodels.Site, existingByID map[int64]sitemodels.Site) error {
	for _, node := range sites {
		switch node.action {
		case actionUpdate:
			site := existingByID[*node.id]
			updated, err := s.updateSiteNameFromImport(ctx, orgID, site, node.name)
			if err != nil {
				return err
			}
			delete(existingByName, site.Name)
			existingByName[updated.Name] = *updated
			existingByID[updated.ID] = *updated
		case actionCreate:
			site, err := s.siteStore.CreateSite(ctx, sitemodels.CreateSiteParams{
				OrgID: orgID,
				Name:  node.name,
			})
			if err != nil {
				return err
			}
			existingByName[site.Name] = *site
			existingByID[site.ID] = *site
		}
	}
	return nil
}

func (s *Service) updateSiteNameFromImport(ctx context.Context, orgID int64, site sitemodels.Site, name string) (*sitemodels.Site, error) {
	usedSlugs, err := s.siteStore.ListSiteSlugs(ctx, orgID)
	if err != nil {
		return nil, err
	}
	usedSlugs = siteMapUsedSlugsExcluding(usedSlugs, site.Slug)

	for {
		slug := sitesdomain.GenerateSiteSlug(name, usedSlugs)
		updated, err := s.siteStore.UpdateSite(ctx, sitemodels.UpdateSiteParams{
			OrgID:           orgID,
			ID:              site.ID,
			Name:            name,
			Slug:            slug,
			LocationCity:    site.LocationCity,
			LocationState:   site.LocationState,
			Timezone:        site.Timezone,
			PowerCapacityMw: site.PowerCapacityMw,
			NetworkConfig:   site.NetworkConfig,
			Address:         site.Address,
			PostalCode:      site.PostalCode,
			Country:         site.Country,
			Notes:           site.Notes,
		})
		if errors.Is(err, sitemodels.ErrSiteSlugCollision) {
			usedSlugs = append(usedSlugs, slug)
			continue
		}
		if err != nil {
			return nil, err
		}
		return updated, nil
	}
}

func siteMapUsedSlugsExcluding(slugs []string, excluded string) []string {
	out := make([]string, 0, len(slugs))
	for _, slug := range slugs {
		if slug == excluded {
			continue
		}
		out = append(out, slug)
	}
	return out
}

func (s *Service) applyBuildings(
	ctx context.Context,
	orgID int64,
	buildings []*resolvedBuilding,
	sitesByName map[string]sitemodels.Site,
	existingByKey map[string]buildingmodels.Building,
	existingByID map[int64]buildingmodels.Building,
) error {
	for _, node := range buildings {
		if node.action == actionNone {
			continue
		}
		siteID, _, err := desiredSiteBuildingIDs(node.siteRef, "", sitesByName, nil)
		if err != nil {
			return err
		}
		if node.action == actionCreate {
			siteID, _, err = s.lockPlacementParents(ctx, orgID, siteID, nil)
			if err != nil {
				return err
			}
			created, err := s.buildingStore.CreateBuilding(ctx, buildingmodels.CreateParams{
				OrgID:         orgID,
				SiteID:        siteID,
				Name:          node.name,
				Aisles:        node.aisles,
				RacksPerAisle: node.racksPerAisle,
			})
			if err != nil {
				return err
			}
			created.SiteLabel = node.siteRef
			existingByKey[node.siteRef+"\x00"+node.name] = *created
			existingByID[created.ID] = *created
			continue
		}
		building, ok := applyTargetBuilding(node, existingByKey, existingByID)
		if !ok {
			return fleeterror.NewNotFoundErrorf("building %q not found", node.name)
		}
		if !nullableInt64Equal(siteID, building.SiteID) {
			if err := s.moveBuildingsToSite(ctx, orgID, []int64{building.ID}, siteID); err != nil {
				return err
			}
		}
		if err := s.enforceBuildingLayoutUnderLock(ctx, orgID, building.ID, node.aisles, node.racksPerAisle); err != nil {
			return err
		}
		if _, err := s.buildingStore.UpdateBuilding(ctx, buildingmodels.UpdateParams{
			OrgID:                 orgID,
			ID:                    building.ID,
			Name:                  node.name,
			Description:           building.Description,
			PowerKw:               building.PowerKw,
			OverheadKw:            building.OverheadKw,
			Aisles:                node.aisles,
			PhysicalRackCount:     building.PhysicalRackCount,
			RacksPerAisle:         node.racksPerAisle,
			DefaultRackRows:       building.DefaultRackRows,
			DefaultRackColumns:    building.DefaultRackColumns,
			DefaultRackOrderIndex: building.DefaultRackOrderIndex,
		}); err != nil {
			return err
		}
		delete(existingByKey, building.SiteLabel+"\x00"+building.Name)
		building.Name = node.name
		building.SiteID = siteID
		building.SiteLabel = node.siteRef
		building.Aisles = node.aisles
		building.RacksPerAisle = node.racksPerAisle
		existingByKey[building.SiteLabel+"\x00"+building.Name] = building
		existingByID[building.ID] = building
	}
	return nil
}

// applyTargetBuilding finds the live building an update node acts on: by id when
// the row carried one, else by its current (site, name) key.
func applyTargetBuilding(node *resolvedBuilding, existingByKey map[string]buildingmodels.Building, existingByID map[int64]buildingmodels.Building) (buildingmodels.Building, bool) {
	if node.id != nil {
		building, ok := existingByID[*node.id]
		return building, ok
	}
	building, ok := existingByKey[node.siteRef+"\x00"+node.name]
	return building, ok
}

func (s *Service) applyRacks(
	ctx context.Context,
	orgID int64,
	racks []*resolvedRack,
	sitesByName map[string]sitemodels.Site,
	buildingsByKey map[string]buildingmodels.Building,
	buildingsByID map[int64]buildingmodels.Building,
	existingByLabel map[string]rackSnapshot,
	existingByID map[int64]rackSnapshot,
) error {
	var pendingGridPositions []pendingRackGridPosition
	for _, node := range racks {
		if node.action == actionNone {
			continue
		}
		orderIndex, err := parseRackOrderIndex(node.orderIndex)
		if err != nil {
			return err
		}
		siteID, buildingID, err := desiredRackPlacementIDs(node, sitesByName, buildingsByKey, buildingsByID)
		if err != nil {
			return err
		}
		aisleIndex, positionInAisle, err := resolvedRackGridPosition(node)
		if err != nil {
			return err
		}
		if node.action == actionCreate {
			siteID, buildingID, err = s.lockPlacementParents(ctx, orgID, siteID, buildingID)
			if err != nil {
				return err
			}
			collection, err := s.collectionStore.CreateCollection(ctx, orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, node.label, "")
			if err != nil {
				return err
			}
			if err := s.collectionStore.CreateRackExtension(ctx, interfaces.CreateRackExtensionParams{
				OrgID:        orgID,
				CollectionID: collection.Id,
				Rows:         node.rows,
				Columns:      node.columns,
				OrderIndex:   int32(orderIndex),
				CoolingType:  int32(collectionpb.RackCoolingType_RACK_COOLING_TYPE_UNSPECIFIED),
				Zone:         node.zone,
				SiteID:       siteID,
				BuildingID:   buildingID,
			}); err != nil {
				return err
			}
			rack := rackSnapshot{
				ID:              collection.Id,
				SiteID:          siteID,
				BuildingID:      buildingID,
				Site:            node.siteRef,
				Building:        node.buildingRef,
				Label:           node.label,
				Zone:            node.zone,
				Rows:            node.rows,
				Columns:         node.columns,
				OrderIndex:      node.orderIndex,
				AisleIndex:      node.aisleIndex,
				PositionInAisle: node.positionInAisle,
			}
			existingByLabel[node.label] = rack
			existingByID[rack.ID] = rack
			pendingGridPositions = append(pendingGridPositions, pendingRackGridPosition{
				rackID:          rack.ID,
				aisleIndex:      aisleIndex,
				positionInAisle: positionInAisle,
			})
			continue
		}
		rack, ok := applyTargetRack(node, existingByLabel, existingByID)
		if !ok {
			return fleeterror.NewNotFoundErrorf("rack %q not found", node.label)
		}
		coolingType, err := parseRackCoolingType(rack.CoolingType)
		if err != nil {
			return err
		}
		finalZone := desiredRackZoneForNode(node, rack)
		if node.label != rack.Label {
			label := node.label
			if err := s.collectionStore.UpdateCollection(ctx, orgID, rack.ID, &label, nil); err != nil {
				return err
			}
		}
		siteID, buildingID, err = s.lockPlacementParents(ctx, orgID, siteID, buildingID)
		if err != nil {
			return err
		}
		currentPlacement, err := s.collectionStore.LockRackPlacementForWrite(ctx, rack.ID, orgID)
		if err != nil {
			return err
		}
		if err := s.enforceRackDimensionsFitCurrentMembers(ctx, orgID, rack.ID, node.rows, node.columns); err != nil {
			return err
		}
		if err := s.collectionStore.UpdateRackInfo(ctx, rack.ID, finalZone, node.rows, node.columns, int32(orderIndex), int32(coolingType), orgID); err != nil {
			return err
		}
		if err := s.collectionStore.UpdateRackPlacement(ctx, rack.ID, orgID, siteID, buildingID, finalZone); err != nil {
			return err
		}
		if !nullableInt64Equal(siteID, currentPlacement.SiteID) {
			if _, err := s.collectionStore.CascadeRackDeviceSites(ctx, rack.ID, orgID, siteID); err != nil {
				return err
			}
		}
		if !nullableInt64Equal(buildingID, currentPlacement.BuildingID) {
			if _, err := s.collectionStore.CascadeRackDeviceBuildings(ctx, rack.ID, orgID, buildingID); err != nil {
				return err
			}
		}
		pendingGridPositions = append(pendingGridPositions, pendingRackGridPosition{
			rackID:          rack.ID,
			aisleIndex:      aisleIndex,
			positionInAisle: positionInAisle,
		})
		delete(existingByLabel, rack.Label)
		rack.SiteID = siteID
		rack.BuildingID = buildingID
		rack.Site = node.siteRef
		rack.Building = node.buildingRef
		rack.Label = node.label
		rack.Zone = finalZone
		rack.Rows = node.rows
		rack.Columns = node.columns
		rack.OrderIndex = node.orderIndex
		rack.AisleIndex = node.aisleIndex
		rack.PositionInAisle = node.positionInAisle
		existingByLabel[rack.Label] = rack
		existingByID[rack.ID] = rack
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

func (s *Service) enforceBuildingLayoutUnderLock(ctx context.Context, orgID, buildingID int64, aisles, racksPerAisle int32) error {
	if err := s.siteStore.LockBuildingForWrite(ctx, orgID, buildingID); err != nil {
		return err
	}
	current, err := s.buildingStore.GetBuilding(ctx, orgID, buildingID)
	if err != nil {
		return err
	}
	if aisles < current.Aisles || racksPerAisle < current.RacksPerAisle {
		orphans, err := s.buildingStore.ListRacksOutsideBuildingBounds(ctx, orgID, buildingID, aisles, racksPerAisle)
		if err != nil {
			return err
		}
		if len(orphans) > 0 {
			rack := orphans[0]
			return fleeterror.NewInvalidArgumentErrorf(
				"cannot shrink layout: rack %q is at aisle %d, position %d which is outside the new %d aisles x %d racks-per-aisle bounds; unplace it first",
				rack.RackLabel, *rack.AisleIndex+1, *rack.PositionInAisle+1, aisles, racksPerAisle,
			)
		}
	}
	if capacity := int64(aisles) * int64(racksPerAisle); capacity > 0 {
		members, err := s.buildingStore.CountRacksInBuilding(ctx, orgID, buildingID)
		if err != nil {
			return err
		}
		if members > capacity {
			return fleeterror.NewInvalidArgumentErrorf(
				"cannot apply layout: building has %d racks but the new %d aisles x %d racks-per-aisle grid holds only %d; unassign some racks first",
				members, aisles, racksPerAisle, capacity,
			)
		}
	}
	return nil
}

func (s *Service) enforceRackDimensionsFitCurrentMembers(ctx context.Context, orgID, rackID int64, rows, columns int32) error {
	slots, err := s.collectionStore.GetRackSlots(ctx, rackID, orgID)
	if err != nil {
		return err
	}
	for _, slot := range slots {
		if slot.GetPosition() == nil {
			continue
		}
		if slot.GetPosition().GetRow() >= rows || slot.GetPosition().GetColumn() >= columns {
			return fleeterror.NewInvalidArgumentErrorf(
				"cannot resize rack to %dx%d: an assigned miner's slot falls outside the smaller grid; remove miners or choose a larger size",
				rows, columns,
			)
		}
	}
	collection, err := s.collectionStore.GetCollection(ctx, orgID, rackID)
	if err != nil {
		return err
	}
	if capacity := int64(rows) * int64(columns); int64(collection.GetDeviceCount()) > capacity {
		return fleeterror.NewInvalidArgumentErrorf(
			"cannot resize rack to %d slot(s): %d miner(s) are currently assigned; remove miners or choose a larger size",
			capacity, collection.GetDeviceCount(),
		)
	}
	return nil
}

func (s *Service) lockPlacementParents(ctx context.Context, orgID int64, siteID, buildingID *int64) (*int64, *int64, error) {
	if buildingID != nil {
		if err := s.siteStore.LockBuildingForWrite(ctx, orgID, *buildingID); err != nil {
			return nil, nil, err
		}
		currentSiteID, err := s.buildingStore.GetBuildingSiteID(ctx, orgID, *buildingID)
		if err != nil {
			return nil, nil, err
		}
		return currentSiteID, buildingID, nil
	}
	if siteID != nil {
		if err := s.siteStore.LockSiteForWrite(ctx, orgID, *siteID); err != nil {
			return nil, nil, err
		}
	}
	return siteID, buildingID, nil
}

func (s *Service) moveBuildingsToSite(ctx context.Context, orgID int64, buildingIDs []int64, targetSiteID *int64) error {
	if targetSiteID != nil {
		if err := s.siteStore.LockSiteForWrite(ctx, orgID, *targetSiteID); err != nil {
			return err
		}
	}
	for _, buildingID := range buildingIDs {
		if err := s.siteStore.LockBuildingForWrite(ctx, orgID, buildingID); err != nil {
			return err
		}
	}
	rowsAffected, err := s.siteStore.AssignBuildingsToSiteBulk(ctx, orgID, buildingIDs, targetSiteID)
	if err != nil {
		return err
	}
	if rowsAffected != int64(len(buildingIDs)) {
		return fleeterror.NewNotFoundErrorf("one or more buildings not found (expected %d, updated %d)", len(buildingIDs), rowsAffected)
	}
	if _, err := s.siteStore.ReassignRacksUnderBuildingsBulk(ctx, orgID, buildingIDs, targetSiteID); err != nil {
		return err
	}
	if _, err := s.siteStore.ReassignDevicesUnderBuildingsBulk(ctx, orgID, buildingIDs, targetSiteID); err != nil {
		return err
	}
	if _, err := s.buildingStore.CascadeDirectDeviceSitesByBuildings(ctx, orgID, buildingIDs, targetSiteID); err != nil {
		return err
	}
	return nil
}

func (s *Service) applyMiners(
	ctx context.Context,
	orgID int64,
	miners []*resolvedMiner,
	sitesByName map[string]sitemodels.Site,
	buildingsByKey map[string]buildingmodels.Building,
	buildingsByID map[int64]buildingmodels.Building,
	racksByLabel map[string]rackSnapshot,
) error {
	var pendingSlots []pendingMinerSlot
	renamedMiners := map[string]string{}
	for _, node := range miners {
		if node.existing == nil {
			continue
		}
		if node.renamed {
			renamedMiners[node.deviceID] = node.name
		}
		if !node.moved {
			continue
		}
		deviceIDs := []string{node.deviceID}
		if node.rackLabel != "" {
			rack, ok := racksByLabel[node.rackLabel]
			if !ok {
				return fleeterror.NewFailedPreconditionErrorf("unknown rack %q for miner %q", node.rackLabel, node.deviceID)
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
			if rack.SiteID == nil {
				if _, err := s.siteStore.AssignDevicesToSite(ctx, orgID, nil, deviceIDs); err != nil {
					return err
				}
			}
			if rack.BuildingID == nil {
				if _, err := s.buildingStore.AssignDevicesToBuilding(ctx, orgID, nil, deviceIDs); err != nil {
					return err
				}
			}
			pendingSlots = append(pendingSlots, pendingMinerSlot{
				rackID:           rack.ID,
				deviceIdentifier: node.deviceID,
				row:              node.rackRow,
				col:              node.rackCol,
			})
			continue
		}

		siteID, buildingID, err := placementIDsFromRef(node.buildingID, node.siteLabel, node.buildLabel, sitesByName, buildingsByKey, buildingsByID)
		if err != nil {
			return err
		}
		siteID, buildingID, err = s.lockPlacementParents(ctx, orgID, siteID, buildingID)
		if err != nil {
			return err
		}
		if _, err := s.collectionStore.LockRacksForReparent(ctx, orgID, deviceIDs, 0); err != nil {
			return err
		}
		if _, err := s.collectionStore.RemoveDevicesFromAnyRack(ctx, orgID, deviceIDs, 0); err != nil {
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
	if len(renamedMiners) > 0 {
		if err := s.deviceStore.UpdateDeviceCustomNames(ctx, orgID, renamedMiners); err != nil {
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
		Racks:             rackExportRows(snap.racks),
		Miners:            minerRows(snap.miners),
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

func rowIDSet(rows []map[string]string) map[int64]bool {
	out := map[int64]bool{}
	for _, row := range rows {
		id, ok := rowID(row)
		if ok {
			out[id] = true
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
		value := valueForUniqueKey(row, key)
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

func valueForUniqueKey(row map[string]string, key string) string {
	switch key {
	case fieldName:
		if row[fieldName] != "" {
			return row[fieldName]
		}
		return row[fieldSite]
	case fieldLabel:
		return rackSectionLabel(row)
	default:
		return row[key]
	}
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
		if buildingSectionName(row) == "" {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", "name is required"))
			continue
		}
		key := buildingRowIdentity(row)
		if seen[key] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", "duplicate building identity"))
		}
		seen[key] = true
	}
	return errs
}

func validateUniqueAssignedBuildingRows(rows []map[string]string) []*pb.ImportValidationError {
	seen := map[string]bool{}
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		name := buildingSectionName(row)
		if name == "" || row[fieldSite] == "" {
			continue
		}
		key := row[fieldSite] + "\x00" + name
		if seen[key] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", "duplicate building name at site"))
		}
		seen[key] = true
	}
	return errs
}

func validateAmbiguousBlankIDBuildingRows(rows []map[string]string, buildings []buildingmodels.Building) []*pb.ImportValidationError {
	countByKey := map[string]int{}
	for _, building := range buildings {
		countByKey[building.SiteLabel+"\x00"+building.Name]++
	}
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		if _, ok := rowID(row); ok {
			continue
		}
		name := buildingSectionName(row)
		if name == "" {
			continue
		}
		if countByKey[row[fieldSite]+"\x00"+name] > 1 {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", fmt.Sprintf("building %q is ambiguous; add id", name)))
		}
	}
	return errs
}

func validateUniqueIDs(rows []map[string]string, section string) []*pb.ImportValidationError {
	seen := map[string]bool{}
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		id := row[fieldID]
		if id == "" {
			continue
		}
		key := id
		if parsedID, err := parseInt64Value(id, fieldID); err == nil {
			key = strconv.FormatInt(parsedID, 10)
		}
		if seen[key] {
			errs = append(errs, csvErr(rowNumber(row, i+1), section, "duplicate id"))
		}
		seen[key] = true
	}
	return errs
}

func validateSiteRenameTargets(rows []map[string]string, sites []sitemodels.Site) []*pb.ImportValidationError {
	nameByID := map[int64]string{}
	idByName := map[string]int64{}
	for _, site := range sites {
		nameByID[site.ID] = site.Name
		idByName[site.Name] = site.ID
	}

	var errs []*pb.ImportValidationError
	for i, row := range rows {
		id, ok := rowID(row)
		if !ok {
			continue
		}
		currentName, ok := nameByID[id]
		if !ok {
			continue
		}
		name := siteSectionName(row)
		if name == "" || name == currentName {
			continue
		}
		ownerID, exists := idByName[name]
		if exists && ownerID != id {
			errs = append(errs, csvErr(rowNumber(row, i+1), "SITE", fmt.Sprintf("site rename target %q is currently used by site_id %d; split this rename into a separate import", name, ownerID)))
		}
	}
	return errs
}

func validateBuildingMoveRenameTargets(rows []map[string]string, buildings []buildingmodels.Building) []*pb.ImportValidationError {
	byID := map[int64]buildingmodels.Building{}
	idBySiteName := map[string]int64{}
	for _, building := range buildings {
		byID[building.ID] = building
		if building.SiteLabel != "" {
			idBySiteName[building.SiteLabel+"\x00"+building.Name] = building.ID
		}
	}

	var errs []*pb.ImportValidationError
	for i, row := range rows {
		id, ok := rowID(row)
		if !ok {
			continue
		}
		building, ok := byID[id]
		if !ok || row[fieldSite] == "" {
			continue
		}
		name := buildingSectionName(row)
		if name != "" && name != building.Name {
			ownerID, exists := idBySiteName[row[fieldSite]+"\x00"+name]
			if exists && ownerID != id {
				errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", fmt.Sprintf("building rename target %q/%q is currently used by building_id %d; split this rename into a separate import", row[fieldSite], name, ownerID)))
			}
		}
		ownerID, exists := idBySiteName[row[fieldSite]+"\x00"+building.Name]
		if row[fieldSite] != building.SiteLabel && exists && ownerID != id {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", fmt.Sprintf("building move target %q/%q is currently used by building_id %d; split this move and rename into separate imports", row[fieldSite], building.Name, ownerID)))
		}
	}
	return errs
}

func validateRackRenameTargets(rows []map[string]string, racks []rackSnapshot) []*pb.ImportValidationError {
	labelByID := map[int64]string{}
	idByLabel := map[string]int64{}
	for _, rack := range racks {
		labelByID[rack.ID] = rack.Label
		idByLabel[rack.Label] = rack.ID
	}

	var errs []*pb.ImportValidationError
	for i, row := range rows {
		id, ok := rowID(row)
		if !ok {
			continue
		}
		currentLabel, ok := labelByID[id]
		if !ok {
			continue
		}
		label := rackSectionLabel(row)
		if label == "" || label == currentLabel {
			continue
		}
		ownerID, exists := idByLabel[label]
		if exists && ownerID != id {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("rack rename target %q is currently used by rack_id %d; split this rename into a separate import", label, ownerID)))
		}
	}
	return errs
}

type topologyOwner struct {
	id  int64
	row int
}

func validateRetainedTopologyUniqueness(parsed *parsedCSV, snap *snapshot, mode pb.OmissionMode) []*pb.ImportValidationError {
	if mode == pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
		return nil
	}
	var errs []*pb.ImportValidationError
	errs = append(errs, validateRetainedSiteNames(parsed.sections["SITE"], snap.sites)...)
	errs = append(errs, validateRetainedBuildingNames(parsed.sections["BUILDING"], snap.buildings)...)
	errs = append(errs, validateRetainedRackLabels(parsed.sections["RACK"], snap.racks)...)
	return errs
}

func validateRetainedSiteNames(rows []map[string]string, sites []sitemodels.Site) []*pb.ImportValidationError {
	ownersByName := map[string][]topologyOwner{}
	namesByID := map[int64]string{}
	existingNames := map[string]bool{}
	for _, site := range sites {
		ownersByName[site.Name] = append(ownersByName[site.Name], topologyOwner{id: site.ID})
		namesByID[site.ID] = site.Name
		existingNames[site.Name] = true
	}
	nextID := int64(-1)
	for i, row := range rows {
		name := siteSectionName(row)
		if name == "" {
			continue
		}
		rowNum := rowNumber(row, i+1)
		if id, ok := rowID(row); ok {
			currentName, exists := namesByID[id]
			if !exists {
				continue
			}
			removeTopologyOwner(ownersByName, currentName, id)
			ownersByName[name] = append(ownersByName[name], topologyOwner{id: id, row: rowNum})
			namesByID[id] = name
			continue
		}
		if existingNames[name] {
			continue
		}
		ownersByName[name] = append(ownersByName[name], topologyOwner{id: nextID, row: rowNum})
		nextID--
	}
	return duplicateTopologyErrors(ownersByName, "SITE", "site name")
}

func validateRetainedBuildingNames(rows []map[string]string, buildings []buildingmodels.Building) []*pb.ImportValidationError {
	ownersByKey := map[string][]topologyOwner{}
	keysByID := map[int64]string{}
	existingKeys := map[string]bool{}
	for _, building := range buildings {
		key := ""
		if building.SiteLabel != "" {
			key = building.SiteLabel + "\x00" + building.Name
			ownersByKey[key] = append(ownersByKey[key], topologyOwner{id: building.ID})
			existingKeys[key] = true
		}
		keysByID[building.ID] = key
	}
	nextID := int64(-1)
	for i, row := range rows {
		name := buildingSectionName(row)
		site := row[fieldSite]
		key := ""
		if name != "" && site != "" {
			key = site + "\x00" + name
		}
		rowNum := rowNumber(row, i+1)
		if id, ok := rowID(row); ok {
			currentKey, exists := keysByID[id]
			if !exists {
				continue
			}
			if currentKey != "" {
				removeTopologyOwner(ownersByKey, currentKey, id)
			}
			if key != "" {
				ownersByKey[key] = append(ownersByKey[key], topologyOwner{id: id, row: rowNum})
			}
			keysByID[id] = key
			continue
		}
		if key == "" {
			continue
		}
		if existingKeys[key] {
			continue
		}
		ownersByKey[key] = append(ownersByKey[key], topologyOwner{id: nextID, row: rowNum})
		nextID--
	}
	return duplicateTopologyErrors(ownersByKey, "BUILDING", "building name at site")
}

func validateRetainedRackLabels(rows []map[string]string, racks []rackSnapshot) []*pb.ImportValidationError {
	ownersByLabel := map[string][]topologyOwner{}
	labelsByID := map[int64]string{}
	existingLabels := map[string]bool{}
	for _, rack := range racks {
		ownersByLabel[rack.Label] = append(ownersByLabel[rack.Label], topologyOwner{id: rack.ID})
		labelsByID[rack.ID] = rack.Label
		existingLabels[rack.Label] = true
	}
	nextID := int64(-1)
	for i, row := range rows {
		label := rackSectionLabel(row)
		if label == "" {
			continue
		}
		rowNum := rowNumber(row, i+1)
		if id, ok := rowID(row); ok {
			currentLabel, exists := labelsByID[id]
			if !exists {
				continue
			}
			removeTopologyOwner(ownersByLabel, currentLabel, id)
			ownersByLabel[label] = append(ownersByLabel[label], topologyOwner{id: id, row: rowNum})
			labelsByID[id] = label
			continue
		}
		if existingLabels[label] {
			continue
		}
		ownersByLabel[label] = append(ownersByLabel[label], topologyOwner{id: nextID, row: rowNum})
		nextID--
	}
	return duplicateTopologyErrors(ownersByLabel, "RACK", "rack label")
}

func removeTopologyOwner(ownersByKey map[string][]topologyOwner, key string, id int64) {
	owners := ownersByKey[key]
	for i, owner := range owners {
		if owner.id == id {
			ownersByKey[key] = append(owners[:i], owners[i+1:]...)
			if len(ownersByKey[key]) == 0 {
				delete(ownersByKey, key)
			}
			return
		}
	}
}

func duplicateTopologyErrors(ownersByKey map[string][]topologyOwner, section, description string) []*pb.ImportValidationError {
	var errs []*pb.ImportValidationError
	for _, owners := range ownersByKey {
		if len(owners) < 2 {
			continue
		}
		row := 0
		for _, owner := range owners {
			if owner.row != 0 {
				row = owner.row
				break
			}
		}
		errs = append(errs, csvErr(row, section, "duplicate retained "+description))
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

	presentSites := rowSet(siteRows, fieldName)
	presentBuildings := compoundRowSet(buildingRows, fieldSite, fieldName)
	presentRacks := rowSet(rackRows, fieldLabel)

	// Reference cells have been canonicalized to names with implied parents filled,
	// so a reference to an omitted entity shows up as a name not present in the CSV.
	var errs []*pb.ImportValidationError
	for i, row := range buildingRows {
		if row[fieldSite] != "" && !presentSites[row[fieldSite]] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "BUILDING", fmt.Sprintf("building site %q is omitted; add the SITE row or choose leave omitted rows in place", row[fieldSite])))
		}
	}
	for i, row := range rackRows {
		if row[fieldBuilding] != "" {
			if !presentBuildings[row[fieldSite]+"\x00"+row[fieldBuilding]] {
				errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("rack building %q for site %q is omitted; add the BUILDING row or choose leave omitted rows in place", row[fieldBuilding], row[fieldSite])))
			}
			continue
		}
		if row[fieldSite] != "" && !presentSites[row[fieldSite]] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("rack site %q is omitted; add the SITE row or choose leave omitted rows in place", row[fieldSite])))
		}
	}
	for i, row := range minerRows {
		if row[fieldRack] != "" {
			if !presentRacks[row[fieldRack]] {
				errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("miner rack %q is omitted; add the RACK row or choose leave omitted rows in place", row[fieldRack])))
			}
			continue
		}
		if row[fieldBuilding] != "" {
			if !presentBuildings[row[fieldSite]+"\x00"+row[fieldBuilding]] {
				errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("miner building %q for site %q is omitted; add the BUILDING row or choose leave omitted rows in place", row[fieldBuilding], row[fieldSite])))
			}
			continue
		}
		if row[fieldSite] != "" && !presentSites[row[fieldSite]] {
			errs = append(errs, csvErr(rowNumber(row, i+1), "MINER", fmt.Sprintf("miner site %q is omitted; add the SITE row or choose leave omitted rows in place", row[fieldSite])))
		}
	}
	return errs
}

func validateKnownEntityIDs(parsed *parsedCSV, snap *snapshot) []*pb.ImportValidationError {
	sitesByID := map[int64]sitemodels.Site{}
	for _, site := range snap.sites {
		sitesByID[site.ID] = site
	}
	buildingsByID := map[int64]buildingmodels.Building{}
	for _, building := range snap.buildings {
		buildingsByID[building.ID] = building
	}
	racksByID := map[int64]rackSnapshot{}
	for _, rack := range snap.racks {
		racksByID[rack.ID] = rack
	}
	// Only the row's own id column is validated here; parent references are checked
	// by resolveReferences.
	var errs []*pb.ImportValidationError
	for i, row := range parsed.sections["SITE"] {
		errs = append(errs, validateKnownIDCell(row, i, "SITE", fieldID, sitesByID)...)
	}
	for i, row := range parsed.sections["BUILDING"] {
		errs = append(errs, validateKnownIDCell(row, i, "BUILDING", fieldID, buildingsByID)...)
	}
	for i, row := range parsed.sections["RACK"] {
		errs = append(errs, validateKnownIDCell(row, i, "RACK", fieldID, racksByID)...)
	}
	return errs
}

func validateKnownIDCell[T any](row map[string]string, index int, section, field string, existing map[int64]T) []*pb.ImportValidationError {
	if row[field] == "" {
		return nil
	}
	id, err := parseInt64Value(row[field], field)
	if err != nil {
		return []*pb.ImportValidationError{csvErr(rowNumber(row, index+1), section, err.Error())}
	}
	if _, ok := existing[id]; !ok {
		return []*pb.ImportValidationError{csvErr(rowNumber(row, index+1), section, fmt.Sprintf("unknown %s %q", field, row[field]))}
	}
	return nil
}

// resolveReferences canonicalizes every parent reference cell in place. Each
// parent relationship is a single cell: blank (unassigned), a bare integer (an
// existing entity referenced by id), or "NAME:x" (a same-import create whose own
// name/label is x). After this pass every reference cell holds a canonical name
// and any implied ancestor is filled in too (a rack that references a building
// also gets that building's site), so resolve, validation, and apply read names
// exclusively and never re-interpret an id. Unresolvable references are errors.
func resolveReferences(parsed *parsedCSV, snap *snapshot) []*pb.ImportValidationError {
	var errs []*pb.ImportValidationError

	// SITE: existing sites by id (with same-import renames), creates by name.
	sitesByID := map[int64]sitemodels.Site{}
	for _, site := range snap.sites {
		sitesByID[site.ID] = site
	}
	createSites := map[string]bool{}
	for _, row := range parsed.sections["SITE"] {
		if id, ok := rowID(row); ok {
			site := sitesByID[id]
			site.Name = row[fieldName]
			sitesByID[id] = site
		} else if name := row[fieldName]; name != "" {
			createSites[name] = true
		}
	}
	resolveSite := func(cell, section string, rn int) string {
		switch ref := parseParentRef(cell); ref.kind {
		case refExisting:
			if site, ok := sitesByID[ref.id]; ok {
				return site.Name
			}
			errs = append(errs, csvErr(rn, section, fmt.Sprintf("site reference %d matches no existing site", ref.id)))
		case refCreate:
			if createSites[ref.name] {
				return ref.name
			}
			errs = append(errs, csvErr(rn, section, fmt.Sprintf("site reference %s%s matches no SITE row in this import", refCreatePrefix, ref.name)))
		case refInvalid:
			errs = append(errs, csvErr(rn, section, fmt.Sprintf("site reference %q must be a numeric id or %sNAME", cell, refCreatePrefix)))
		}
		return ""
	}

	// BUILDING: canonicalize each site reference, then index existing buildings by
	// id (with same-import moves/renames) and creates by name with their site.
	for i, row := range parsed.sections["BUILDING"] {
		row[fieldSite] = resolveSite(row[fieldSite], "BUILDING", rowNumber(row, i+1))
	}
	buildingsByID := map[int64]buildingmodels.Building{}
	for _, building := range snap.buildings {
		buildingsByID[building.ID] = building
	}
	createBuildings := map[string][]string{} // name -> resolved site names
	for _, row := range parsed.sections["BUILDING"] {
		if id, ok := rowID(row); ok {
			building := buildingsByID[id]
			building.Name = row[fieldName]
			building.SiteLabel = row[fieldSite]
			buildingsByID[id] = building
		} else if name := row[fieldName]; name != "" {
			createBuildings[name] = append(createBuildings[name], row[fieldSite])
		}
	}
	// resolveBuilding returns the canonical building name, its site, and the id of
	// the existing building it resolves to (0 for a same-import create). siteHint
	// is the row's own already-resolved site reference, used only to disambiguate a
	// create reference that names more than one same-import building.
	resolveBuilding := func(cell, siteHint, section string, rn int) (name, site string, id int64) {
		switch ref := parseParentRef(cell); ref.kind {
		case refExisting:
			if building, ok := buildingsByID[ref.id]; ok {
				return building.Name, building.SiteLabel, ref.id
			}
			errs = append(errs, csvErr(rn, section, fmt.Sprintf("building reference %d matches no existing building", ref.id)))
		case refCreate:
			sites := createBuildings[ref.name]
			switch len(sites) {
			case 0:
				errs = append(errs, csvErr(rn, section, fmt.Sprintf("building reference %s%s matches no BUILDING row in this import", refCreatePrefix, ref.name)))
			case 1:
				return ref.name, sites[0], 0
			default:
				if siteHint != "" {
					for _, s := range sites {
						if s == siteHint {
							return ref.name, s, 0
						}
					}
				}
				errs = append(errs, csvErr(rn, section, fmt.Sprintf("building reference %s%s is ambiguous; set site to choose one", refCreatePrefix, ref.name)))
			}
		case refInvalid:
			errs = append(errs, csvErr(rn, section, fmt.Sprintf("building reference %q must be a numeric id or %sNAME", cell, refCreatePrefix)))
		}
		return "", "", 0
	}

	// RACK: resolve the site reference, then the building reference (which wins and
	// dictates the site). Then index existing racks by id and creates by label.
	for i, row := range parsed.sections["RACK"] {
		rn := rowNumber(row, i+1)
		site := resolveSite(row[fieldSite], "RACK", rn)
		if row[fieldBuilding] != "" {
			building, bSite, bID := resolveBuilding(row[fieldBuilding], site, "RACK", rn)
			row[fieldBuilding] = building
			setRefID(row, refBuildingIDCell, bID)
			if bSite != "" {
				site = bSite
			}
		}
		row[fieldSite] = site
	}
	racksByID := map[int64]rackSnapshot{}
	for _, rack := range snap.racks {
		racksByID[rack.ID] = rack
	}
	type rackPlacement struct {
		building, site string
		buildingID     *int64
	}
	createRacks := map[string]rackPlacement{} // label -> placement
	for _, row := range parsed.sections["RACK"] {
		if id, ok := rowID(row); ok {
			rack := racksByID[id]
			rack.Label = row[fieldLabel]
			rack.Building = row[fieldBuilding]
			rack.Site = row[fieldSite]
			racksByID[id] = rack
		} else if label := row[fieldLabel]; label != "" {
			createRacks[label] = rackPlacement{building: row[fieldBuilding], site: row[fieldSite], buildingID: refID(row, refBuildingIDCell)}
		}
	}
	// resolveRack returns the canonical rack label, its building and site, and the
	// id of that rack's building when the rack sits under an existing building.
	resolveRack := func(cell, section string, rn int) (label, building, site string, buildingID *int64) {
		switch ref := parseParentRef(cell); ref.kind {
		case refExisting:
			if rack, ok := racksByID[ref.id]; ok {
				return rack.Label, rack.Building, rack.Site, rack.BuildingID
			}
			errs = append(errs, csvErr(rn, section, fmt.Sprintf("rack reference %d matches no existing rack", ref.id)))
		case refCreate:
			if placement, ok := createRacks[ref.name]; ok {
				return ref.name, placement.building, placement.site, placement.buildingID
			}
			errs = append(errs, csvErr(rn, section, fmt.Sprintf("rack reference %s%s matches no RACK row in this import", refCreatePrefix, ref.name)))
		case refInvalid:
			errs = append(errs, csvErr(rn, section, fmt.Sprintf("rack reference %q must be a numeric id or %sNAME", cell, refCreatePrefix)))
		}
		return "", "", "", nil
	}

	// MINER: resolve site, then building (wins over site), then rack (wins over
	// both, dictating building and site).
	for i, row := range parsed.sections["MINER"] {
		rn := rowNumber(row, i+1)
		site := resolveSite(row[fieldSite], "MINER", rn)
		building := ""
		var buildingID *int64
		if row[fieldBuilding] != "" {
			b, bSite, bID := resolveBuilding(row[fieldBuilding], site, "MINER", rn)
			building = b
			buildingID = nonZeroInt64Ptr(bID)
			if bSite != "" {
				site = bSite
			}
		}
		if row[fieldRack] != "" {
			label, rBuilding, rSite, rBuildingID := resolveRack(row[fieldRack], "MINER", rn)
			row[fieldRack] = label
			if rBuilding != "" {
				building = rBuilding
				buildingID = rBuildingID
			}
			if rSite != "" {
				site = rSite
			}
		}
		row[fieldBuilding] = building
		row[fieldSite] = site
		setRefIDPtr(row, refBuildingIDCell, buildingID)
	}
	return errs
}

// setRefID writes an existing-entity id companion cell, deleting it when id is 0
// so a re-resolution never leaves a stale id behind.
func setRefID(row map[string]string, cell string, id int64) {
	if id > 0 {
		row[cell] = strconv.FormatInt(id, 10)
		return
	}
	delete(row, cell)
}

// setRefIDPtr is setRefID for an optional id.
func setRefIDPtr(row map[string]string, cell string, id *int64) {
	if id != nil {
		setRefID(row, cell, *id)
		return
	}
	delete(row, cell)
}

// refID reads an existing-entity id companion cell, returning nil when unset.
func refID(row map[string]string, cell string) *int64 {
	raw := row[cell]
	if raw == "" {
		return nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return nil
	}
	return &id
}

func nonZeroInt64Ptr(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}

// refKind classifies a parent reference cell.
type refKind int

const (
	refUnassigned refKind = iota // blank cell: no parent
	refExisting                  // bare integer: existing entity by id
	refCreate                    // "NAME:x": same-import new entity named x
	refInvalid                   // a non-empty, non-integer cell without the NAME: prefix
)

// parentRef is a parsed parent reference cell.
type parentRef struct {
	kind refKind
	id   int64  // set when kind == refExisting
	name string // set when kind == refCreate
}

// parseParentRef interprets a single reference cell. A blank cell is
// unassigned; a value with the NAME: prefix references a same-import create by
// its own name/label (the prefix is stripped once, so a create literally named
// "NAME:x" is referenced as "NAME:NAME:x"); a bare integer references an
// existing entity by id; anything else is invalid.
func parseParentRef(cell string) parentRef {
	if cell == "" {
		return parentRef{kind: refUnassigned}
	}
	if name, ok := strings.CutPrefix(cell, refCreatePrefix); ok {
		return parentRef{kind: refCreate, name: name}
	}
	if id, err := parseInt64Value(cell, "reference"); err == nil {
		return parentRef{kind: refExisting, id: id}
	}
	return parentRef{kind: refInvalid}
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

func validateRackGridCollisions(rackRows []map[string]string, snap *snapshot, mode pb.OmissionMode) []*pb.ImportValidationError {
	presentRackIdentities := rackIdentitySet(rackRows, snap.racks)
	buildingIDByName := uniqueBuildingIDsByName(snap.buildings)
	seen := map[string]string{}
	var errs []*pb.ImportValidationError
	for _, rack := range snap.racks {
		if presentRackIdentities[rackIdentity(rack)] {
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
		bKey := buildingIdentityKey(rack.BuildingID, rack.Site, rack.Building, buildingIDByName)
		seen[rackGridCollisionKey(bKey, aisle, position)] = rack.Label
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
		bKey := buildingIdentityKey(refID(row, refBuildingIDCell), row[fieldSite], row[fieldBuilding], buildingIDByName)
		key := rackGridCollisionKey(bKey, aisle, position)
		if existingRack, ok := seen[key]; ok {
			errs = append(errs, csvErr(rowNumber(row, i+1), "RACK", fmt.Sprintf("rack grid cell already occupied by rack %s", existingRack)))
			continue
		}
		seen[key] = rackSectionLabel(row)
	}
	return errs
}

func rackGridCollisionKey(buildingKey string, aisle, position int32) string {
	return fmt.Sprintf("%s\x00%d\x00%d", buildingKey, aisle, position)
}

// buildingIdentityKey keys a building by its stable id so two buildings sharing
// a (site, name) pair stay distinct. It prefers an explicit id, then a unique
// snapshot building of that (site, name), and finally the (site, name) pair for
// same-import creates that have no id yet.
func buildingIdentityKey(id *int64, site, building string, idByName map[string]int64) string {
	if id != nil {
		return "id:" + strconv.FormatInt(*id, 10)
	}
	if resolved, ok := idByName[site+"\x00"+building]; ok {
		return "id:" + strconv.FormatInt(resolved, 10)
	}
	return "name:" + site + "\x00" + building
}

// uniqueBuildingIDsByName maps a (site, name) pair to its building id, omitting
// any pair shared by more than one building so ambiguous names fall back to
// name keying.
func uniqueBuildingIDsByName(buildings []buildingmodels.Building) map[string]int64 {
	out := map[string]int64{}
	ambiguous := map[string]bool{}
	for _, building := range buildings {
		if building.ID <= 0 {
			continue
		}
		key := building.SiteLabel + "\x00" + building.Name
		if _, ok := out[key]; ok {
			ambiguous[key] = true
			continue
		}
		out[key] = building.ID
	}
	for key := range ambiguous {
		delete(out, key)
	}
	return out
}

func validateImportedNameLengths(parsed *parsedCSV) []*pb.ImportValidationError {
	var errs []*pb.ImportValidationError
	errs = append(errs, validateFieldLength(parsed.sections["SITE"], "SITE", fieldName, maxSiteNameLength)...)
	errs = append(errs, validateFieldLength(parsed.sections["BUILDING"], "BUILDING", fieldName, maxBuildingNameLength)...)
	errs = append(errs, validateFieldLength(parsed.sections["RACK"], "RACK", fieldLabel, maxRackLabelLength)...)
	errs = append(errs, validateFieldLength(parsed.sections["RACK"], "RACK", "zone", maxRackZoneLength)...)
	errs = append(errs, validateFieldLength(parsed.sections["MINER"], "MINER", fieldName, maxMinerNameLength)...)
	return errs
}

func validateFieldLength(rows []map[string]string, section, field string, maxRunes int) []*pb.ImportValidationError {
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		value := row[field]
		if utf8.RuneCountInString(value) > maxRunes {
			errs = append(errs, csvErr(rowNumber(row, i+1), section, fmt.Sprintf("%s must be at most %d characters", field, maxRunes)))
		}
	}
	return errs
}

func desiredBuildingCapacityMap(rows []map[string]string, buildings []buildingmodels.Building) map[string]buildingmodels.Building {
	out := map[string]buildingmodels.Building{}
	buildingsByID := map[int64]buildingmodels.Building{}
	buildingsByKey := map[string]buildingmodels.Building{}
	for _, building := range buildings {
		buildingsByID[building.ID] = building
		buildingsByKey[building.SiteLabel+"\x00"+building.Name] = building
		out[buildingCapacityKey(building)] = building
	}
	for _, row := range rows {
		building := buildingsByKey[row[fieldSite]+"\x00"+buildingSectionName(row)]
		if id, ok := rowID(row); ok {
			if existing, ok := buildingsByID[id]; ok {
				building = existing
			} else {
				building.ID = id
			}
		}
		building.SiteLabel = row[fieldSite]
		building.Name = buildingSectionName(row)
		if aisles, err := parseInt32Value(row["aisles"], "aisles"); err == nil {
			building.Aisles = aisles
		}
		if racksPerAisle, err := parseInt32Value(row["racks_per_aisle"], "racks_per_aisle"); err == nil {
			building.RacksPerAisle = racksPerAisle
		}
		out[buildingCapacityKey(building)] = building
	}
	return out
}

func desiredBuildingLayoutIDMap(rows []map[string]string, buildings []buildingmodels.Building) map[int64]buildingmodels.Building {
	out := map[int64]buildingmodels.Building{}
	for _, building := range buildings {
		if building.ID > 0 {
			out[building.ID] = building
		}
	}
	for _, row := range rows {
		id, ok := rowID(row)
		if !ok {
			continue
		}
		building := out[id]
		building.ID = id
		building.SiteLabel = row[fieldSite]
		building.Name = buildingSectionName(row)
		if aisles, err := parseInt32Value(row["aisles"], "aisles"); err == nil {
			building.Aisles = aisles
		}
		if racksPerAisle, err := parseInt32Value(row["racks_per_aisle"], "racks_per_aisle"); err == nil {
			building.RacksPerAisle = racksPerAisle
		}
		out[id] = building
	}
	return out
}

func rackBuildingCapacityKey(rack rackSnapshot) (string, bool) {
	if rack.BuildingID != nil {
		return "id:" + strconv.FormatInt(*rack.BuildingID, 10), true
	}
	if rack.Building == "" {
		return "", false
	}
	return "name:" + rack.Site + "\x00" + rack.Building, true
}

func buildingCapacityKey(building buildingmodels.Building) string {
	if building.ID > 0 {
		return "id:" + strconv.FormatInt(building.ID, 10)
	}
	return "name:" + building.SiteLabel + "\x00" + building.Name
}

func desiredRackLabelsByID(rows []map[string]string) map[int64]string {
	out := map[int64]string{}
	for _, row := range rows {
		id, ok := rowID(row)
		if !ok {
			continue
		}
		out[id] = rackSectionLabel(row)
	}
	return out
}

func desiredRackLabel(miner minerSnapshot, desiredRackLabels map[int64]string) string {
	if miner.RackID == nil {
		return miner.Rack
	}
	if label, ok := desiredRackLabels[*miner.RackID]; ok {
		return label
	}
	return miner.Rack
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

func rowID(row map[string]string) (int64, bool) {
	id, err := parseOptionalInt64(row[fieldID], fieldID)
	if err != nil || id == nil {
		return 0, false
	}
	return *id, true
}

func parseOptionalInt64(raw string, field string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	id, err := parseInt64Value(raw, field)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func siteIdentity(site sitemodels.Site) string {
	if site.ID > 0 {
		return "id:" + strconv.FormatInt(site.ID, 10)
	}
	return "name:" + site.Name
}

func siteRowIdentity(row map[string]string) string {
	if id, ok := rowID(row); ok {
		return "id:" + strconv.FormatInt(id, 10)
	}
	return "name:" + siteSectionName(row)
}

func buildingIdentity(building buildingmodels.Building) string {
	if building.ID > 0 {
		return "id:" + strconv.FormatInt(building.ID, 10)
	}
	return "name:" + building.SiteLabel + "\x00" + building.Name
}

func buildingRowIdentity(row map[string]string) string {
	if id, ok := rowID(row); ok {
		return "id:" + strconv.FormatInt(id, 10)
	}
	return "name:" + row[fieldSite] + "\x00" + buildingSectionName(row)
}

func rackIdentity(rack rackSnapshot) string {
	if rack.ID > 0 {
		return "id:" + strconv.FormatInt(rack.ID, 10)
	}
	return "label:" + rack.Label
}

func rackRowIdentity(row map[string]string) string {
	if id, ok := rowID(row); ok {
		return "id:" + strconv.FormatInt(id, 10)
	}
	return "label:" + rackSectionLabel(row)
}

func siteSectionName(row map[string]string) string {
	if row[fieldName] != "" {
		return row[fieldName]
	}
	return row[fieldSite]
}

func buildingSectionName(row map[string]string) string {
	if row[fieldName] != "" {
		return row[fieldName]
	}
	return row[fieldBuilding]
}

func rackSectionLabel(row map[string]string) string {
	if row[fieldLabel] != "" {
		return row[fieldLabel]
	}
	return row[fieldRack]
}

func existingByIDRow[T any](row map[string]string, existing map[int64]T) (T, bool) {
	var zero T
	id, ok := rowID(row)
	if !ok {
		return zero, false
	}
	value, ok := existing[id]
	return value, ok
}

func existingRackByIDRow(row map[string]string, existing map[int64]rackSnapshot) (rackSnapshot, bool) {
	id, ok := rowID(row)
	if !ok {
		return rackSnapshot{}, false
	}
	value, ok := existing[id]
	return value, ok
}

func siteIdentitySet(rows []map[string]string, sites []sitemodels.Site) map[string]bool {
	byName := map[string]sitemodels.Site{}
	for _, site := range sites {
		byName[site.Name] = site
	}
	out := map[string]bool{}
	for _, row := range rows {
		if id, ok := rowID(row); ok {
			out["id:"+strconv.FormatInt(id, 10)] = true
			continue
		}
		if site, ok := byName[siteSectionName(row)]; ok {
			out[siteIdentity(site)] = true
			continue
		}
		out[siteRowIdentity(row)] = true
	}
	return out
}

func buildingIdentitySet(rows []map[string]string, buildings []buildingmodels.Building) map[string]bool {
	byKey := map[string]buildingmodels.Building{}
	for _, building := range buildings {
		byKey[building.SiteLabel+"\x00"+building.Name] = building
	}
	out := map[string]bool{}
	for _, row := range rows {
		if id, ok := rowID(row); ok {
			out["id:"+strconv.FormatInt(id, 10)] = true
			continue
		}
		key := row[fieldSite] + "\x00" + buildingSectionName(row)
		if building, ok := byKey[key]; ok {
			out[buildingIdentity(building)] = true
			continue
		}
		out[buildingRowIdentity(row)] = true
	}
	return out
}

func rackIdentitySet(rows []map[string]string, racks []rackSnapshot) map[string]bool {
	byLabel := map[string]rackSnapshot{}
	for _, rack := range racks {
		byLabel[rack.Label] = rack
	}
	out := map[string]bool{}
	for _, row := range rows {
		if id, ok := rowID(row); ok {
			out["id:"+strconv.FormatInt(id, 10)] = true
			continue
		}
		if rack, ok := byLabel[rackSectionLabel(row)]; ok {
			out[rackIdentity(rack)] = true
			continue
		}
		out[rackRowIdentity(row)] = true
	}
	return out
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

func rowMap(headers, values []string) map[string]string {
	out := map[string]string{}
	for i, header := range headers {
		if i < len(values) {
			out[header] = values[i]
		}
	}
	return out
}

// rackComparableRow is the canonical row an unedited exported rack row must
// equal, so change detection reads identically to export (id-authoritative,
// blank parent-name columns).
func rackComparableRow(rack rackSnapshot) map[string]string {
	return rowMap(rackHeaders, rackRawRows([]rackSnapshot{rack})[0])
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
		key := row[fieldSite] + "\x00" + buildingSectionName(row)
		building := out[key]
		if id, ok := rowID(row); ok {
			for _, existing := range buildings {
				if existing.ID == id {
					building = existing
					delete(out, existing.SiteLabel+"\x00"+existing.Name)
					break
				}
			}
		}
		building.SiteLabel = row[fieldSite]
		building.Name = buildingSectionName(row)
		if aisles, err := parseInt32Value(row["aisles"], "aisles"); err == nil {
			building.Aisles = aisles
		}
		if racksPerAisle, err := parseInt32Value(row["racks_per_aisle"], "racks_per_aisle"); err == nil {
			building.RacksPerAisle = racksPerAisle
		}
		out[building.SiteLabel+"\x00"+building.Name] = building
	}
	return out
}

func desiredBuildingList(rows []map[string]string, buildings []buildingmodels.Building) []buildingmodels.Building {
	out := append([]buildingmodels.Building(nil), buildings...)
	byID := map[int64]int{}
	byKey := map[string]int{}
	duplicateKeys := map[string]bool{}
	for i, building := range out {
		if building.ID != 0 {
			byID[building.ID] = i
		}
		key := building.SiteLabel + "\x00" + building.Name
		if _, ok := byKey[key]; ok {
			duplicateKeys[key] = true
			continue
		}
		byKey[key] = i
	}
	for _, row := range rows {
		if id, ok := rowID(row); ok {
			building := buildingmodels.Building{ID: id}
			if index, exists := byID[id]; exists {
				building = out[index]
				applyDesiredBuildingRow(row, &building)
				out[index] = building
				continue
			}
			applyDesiredBuildingRow(row, &building)
			out = append(out, building)
			continue
		}
		key := row[fieldSite] + "\x00" + buildingSectionName(row)
		if index, exists := byKey[key]; exists && !duplicateKeys[key] {
			building := out[index]
			applyDesiredBuildingRow(row, &building)
			out[index] = building
			continue
		}
		building := buildingmodels.Building{}
		applyDesiredBuildingRow(row, &building)
		out = append(out, building)
	}
	return out
}

func applyDesiredBuildingRow(row map[string]string, building *buildingmodels.Building) {
	building.SiteLabel = row[fieldSite]
	building.Name = buildingSectionName(row)
	if aisles, err := parseInt32Value(row["aisles"], "aisles"); err == nil {
		building.Aisles = aisles
	}
	if racksPerAisle, err := parseInt32Value(row["racks_per_aisle"], "racks_per_aisle"); err == nil {
		building.RacksPerAisle = racksPerAisle
	}
}

func desiredRackMap(rows []map[string]string, racks []rackSnapshot, buildingRows []map[string]string, buildings []buildingmodels.Building) map[string]rackSnapshot {
	out := map[string]rackSnapshot{}
	for _, rack := range racks {
		out[rack.Label] = rack
	}
	buildingBySiteName := desiredBuildingsBySiteName(buildingRows, buildings)
	buildingByID := desiredBuildingsByID(buildingRows, buildings)
	for _, row := range rows {
		rack := out[rackSectionLabel(row)]
		if id, ok := rowID(row); ok {
			for _, existing := range racks {
				if existing.ID == id {
					rack = existing
					delete(out, existing.Label)
					break
				}
			}
		}
		rack.Label = rackSectionLabel(row)
		rack.Site = row[fieldSite]
		rack.Building = row[fieldBuilding]
		// Prefer the resolved building id recorded by resolveReferences so two
		// buildings sharing a (site, name) pair stay distinct; fall back to the
		// (site, name) key for creates, whose uniqueness is enforced elsewhere.
		rack.BuildingID = nil
		if bID := refID(row, refBuildingIDCell); bID != nil {
			rack.BuildingID = bID
			if building, ok := buildingByID[*bID]; ok {
				rack.SiteID = building.SiteID
			}
		} else if building, ok := buildingBySiteName[row[fieldSite]+"\x00"+row[fieldBuilding]]; ok {
			if building.ID > 0 {
				rack.BuildingID = &building.ID
			}
			rack.SiteID = building.SiteID
		}
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

func desiredBuildingsBySiteName(rows []map[string]string, buildings []buildingmodels.Building) map[string]buildingmodels.Building {
	out := map[string]buildingmodels.Building{}
	for _, building := range desiredBuildingList(rows, buildings) {
		if building.SiteLabel != "" {
			out[building.SiteLabel+"\x00"+building.Name] = building
		}
	}
	return out
}

func desiredBuildingsByID(rows []map[string]string, buildings []buildingmodels.Building) map[int64]buildingmodels.Building {
	out := map[int64]buildingmodels.Building{}
	for _, building := range desiredBuildingList(rows, buildings) {
		if building.ID > 0 {
			out[building.ID] = building
		}
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

func parseInt64Value(raw string, field string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fleeterror.NewInvalidArgumentErrorf("invalid %s value %q", field, raw)
	}
	return value, nil
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

// desiredSiteBuildingIDs resolves the site and building ids a placement targets.
// Reference cells have already been canonicalized to names, so this is a pure
// name lookup against the running apply maps (which include entities created
// earlier in this same import).
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
		if buildingsByKey == nil {
			return siteID, nil, nil
		}
		building, ok := buildingsByKey[siteName+"\x00"+buildingName]
		if !ok {
			return nil, nil, fleeterror.NewFailedPreconditionErrorf("unknown building %q at site %q", buildingName, siteName)
		}
		buildingID = &building.ID
	}
	return siteID, buildingID, nil
}

func desiredRackZone(row map[string]string, current rackSnapshot) string {
	if current.Building != "" && (row[fieldBuilding] != current.Building || row[fieldSite] != current.Site) {
		return ""
	}
	return row["zone"]
}

// desiredRackZoneForNode is the node-based twin of desiredRackZone: a zone only
// survives when the rack stays in the same building/site it currently occupies.
func desiredRackZoneForNode(node *resolvedRack, current rackSnapshot) string {
	if current.Building != "" && (node.buildingRef != current.Building || node.siteRef != current.Site) {
		return ""
	}
	return node.zone
}

// resolvedRackGridPosition reads a rack node's desired aisle/position, parsing the
// canonicalized string cells into the nullable ints the placement store wants.
func resolvedRackGridPosition(node *resolvedRack) (*int32, *int32, error) {
	if node.aisleIndex == "" && node.positionInAisle == "" {
		return nil, nil, nil
	}
	aisle, err := parseInt32Value(node.aisleIndex, "aisle_index")
	if err != nil {
		return nil, nil, err
	}
	position, err := parseInt32Value(node.positionInAisle, "position_in_aisle")
	if err != nil {
		return nil, nil, err
	}
	return &aisle, &position, nil
}

// desiredRackPlacementIDs resolves the site/building ids a rack node targets,
// preferring the resolved existing building id (which disambiguates two buildings
// sharing a (site, name) pair) and falling back to the canonical name lookup.
func desiredRackPlacementIDs(
	node *resolvedRack,
	sitesByName map[string]sitemodels.Site,
	buildingsByKey map[string]buildingmodels.Building,
	buildingsByID map[int64]buildingmodels.Building,
) (*int64, *int64, error) {
	return placementIDsFromRef(node.buildingID, node.siteRef, node.buildingRef, sitesByName, buildingsByKey, buildingsByID)
}

// placementIDsFromRef resolves site/building ids from a resolved building id when
// present, else from canonical site/building names against the running apply maps.
func placementIDsFromRef(
	buildingID *int64,
	siteRef string,
	buildingRef string,
	sitesByName map[string]sitemodels.Site,
	buildingsByKey map[string]buildingmodels.Building,
	buildingsByID map[int64]buildingmodels.Building,
) (*int64, *int64, error) {
	if buildingID != nil {
		if b, ok := buildingsByID[*buildingID]; ok {
			id := b.ID
			return b.SiteID, &id, nil
		}
	}
	return desiredSiteBuildingIDs(siteRef, buildingRef, sitesByName, buildingsByKey)
}

// applyTargetRack finds the live rack a node updates: by resolved id when the row
// carried one, else by its current label.
func applyTargetRack(node *resolvedRack, existingByLabel map[string]rackSnapshot, existingByID map[int64]rackSnapshot) (rackSnapshot, bool) {
	if node.id != nil {
		if rack, ok := existingByID[*node.id]; ok {
			return rack, true
		}
	}
	label := node.prevLabel
	if label == "" {
		label = node.label
	}
	rack, ok := existingByLabel[label]
	return rack, ok
}

func rowSetFromSiteNames(rows []sitemodels.Site) map[string]bool {
	out := map[string]bool{}
	for _, row := range rows {
		out[row.Name] = true
	}
	return out
}

func desiredSiteSet(rows []map[string]string, sites []sitemodels.Site) map[string]bool {
	out := rowSetFromSiteNames(sites)
	sitesByID := map[int64]sitemodels.Site{}
	for _, site := range sites {
		sitesByID[site.ID] = site
	}
	for _, row := range rows {
		name := siteSectionName(row)
		if name == "" {
			continue
		}
		if id, ok := rowID(row); ok {
			site, exists := sitesByID[id]
			if !exists {
				continue
			}
			delete(out, site.Name)
		}
		out[name] = true
	}
	return out
}

func rowSetFromBuildings(rows []buildingmodels.Building) map[string]bool {
	out := map[string]bool{}
	for _, row := range rows {
		out[buildingIdentity(row)] = true
	}
	return out
}

func rowSetFromBuildingNames(rows []buildingmodels.Building) map[string]bool {
	out := map[string]bool{}
	for _, row := range rows {
		out[row.SiteLabel+"\x00"+row.Name] = true
	}
	return out
}

func rowSetFromDesiredBuildings(rows []map[string]string, buildings []buildingmodels.Building) map[string]bool {
	out := rowSetFromBuildingNames(buildings)
	for _, row := range rows {
		if id, ok := rowID(row); ok {
			for _, existing := range buildings {
				if existing.ID == id {
					delete(out, existing.SiteLabel+"\x00"+existing.Name)
					break
				}
			}
		}
		if buildingSectionName(row) != "" {
			out[row[fieldSite]+"\x00"+buildingSectionName(row)] = true
		}
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
	present := siteIdentitySet(rows, sites)
	var out []sitemodels.Site
	for _, site := range sites {
		if !present[siteIdentity(site)] {
			out = append(out, site)
		}
	}
	return out
}

func omittedBuildings(rows []map[string]string, buildings []buildingmodels.Building) []buildingmodels.Building {
	present := buildingIdentitySet(rows, buildings)
	var out []buildingmodels.Building
	for _, building := range buildings {
		if !present[buildingIdentity(building)] {
			out = append(out, building)
		}
	}
	return out
}

func omittedRacks(rows []map[string]string, racks []rackSnapshot) []rackSnapshot {
	present := rackIdentitySet(rows, racks)
	var out []rackSnapshot
	for _, rack := range racks {
		if !present[rackIdentity(rack)] {
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
