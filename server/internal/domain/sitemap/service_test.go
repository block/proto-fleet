package sitemap

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"connectrpc.com/authn"
	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	fleetpb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/sitemap/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	buildingmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	sitemodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"go.uber.org/mock/gomock"
)

func TestBuildSiteMapExportZipIncludesCSVAndAgentGuide(t *testing.T) {
	csvData, err := buildSiteMapCSV(testSnapshotMatchingValidCSV())
	if err != nil {
		t.Fatalf("buildSiteMapCSV error = %v", err)
	}

	zipData, err := buildSiteMapExportZip(csvData)
	if err != nil {
		t.Fatalf("buildSiteMapExportZip error = %v", err)
	}

	files := readZipFiles(t, zipData)
	csvText := files[siteMapExportCSVPath]
	if !strings.Contains(csvText, "# SECTION: MINER") {
		t.Fatalf("%s missing MINER section: %q", siteMapExportCSVPath, csvText)
	}
	if !strings.Contains(csvText, "label,id (read only),building,site,zone,rows,columns,order_index,aisle_index,position_in_aisle") {
		t.Fatalf("%s missing expected RACK headers: %q", siteMapExportCSVPath, csvText)
	}

	guideText := files[siteMapExportGuideTXTPath]
	for _, want := range []string{
		"Edit proto-fleet-site-map/site-map.csv",
		"If rack is set, the rack determines the miner's building and site; leave building and site blank.",
		"Leave omitted rows in place keeps missing rows unchanged.",
		"Remove omitted rows soft-deletes omitted sites, buildings, and racks, and unassigns omitted miners.",
	} {
		if !strings.Contains(guideText, want) {
			t.Fatalf("%s missing %q: %q", siteMapExportGuideTXTPath, want, guideText)
		}
	}
}

func TestMaxImportBytesAllowsLargeFleetExports(t *testing.T) {
	if MaxImportBytes < 64*1024*1024 {
		t.Fatalf("MaxImportBytes = %d, want at least 64 MiB", MaxImportBytes)
	}
}

func TestSiteMapMinerPairingStatusesIncludeHiddenMutableStatuses(t *testing.T) {
	statuses := map[fleetpb.PairingStatus]bool{}
	for _, status := range siteMapMinerPairingStatuses {
		statuses[status] = true
	}
	for _, want := range []fleetpb.PairingStatus{
		fleetpb.PairingStatus_PAIRING_STATUS_PAIRED,
		fleetpb.PairingStatus_PAIRING_STATUS_UNPAIRED,
		fleetpb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
		fleetpb.PairingStatus_PAIRING_STATUS_PENDING,
		fleetpb.PairingStatus_PAIRING_STATUS_FAILED,
		fleetpb.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
	} {
		if !statuses[want] {
			t.Fatalf("siteMapMinerPairingStatuses missing %s", want)
		}
	}
	if statuses[fleetpb.PairingStatus_PAIRING_STATUS_UNSPECIFIED] {
		t.Fatal("siteMapMinerPairingStatuses should enumerate concrete statuses, not rely on UNSPECIFIED")
	}
}

func TestParseSiteMapCSVRejectsMissingRequiredSection(t *testing.T) {
	csv := `# SECTION: SITE
name,id (read only)
Site A,1

# SECTION: BUILDING
name,id (read only),site,aisles,racks_per_aisle
Building A,10,1,1,1

# SECTION: MINER
device_identifier (read only),serial_number (read only),name,ip_address (read only),mac_address (read only),site,building,rack,rack_row,rack_col
`
	_, errs := parseSiteMapCSV([]byte(csv))
	if !hasValidationError(errs, "RACK", "missing section") {
		t.Fatalf("parse errors = %+v, want missing RACK section", errs)
	}
}

func TestParseSiteMapCSVAndBuildPlanRequiresOmissionChoice(t *testing.T) {
	parsed, errs := parseSiteMapCSV([]byte(validCSV()))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}

	plan := buildPlan(parsed, testSnapshot(), pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %v", plan.errors)
	}
	if plan.omissions.GetSites() != 1 || plan.omissions.GetBuildings() != 1 || plan.omissions.GetRacks() != 1 || plan.omissions.GetMiners() != 1 {
		t.Fatalf("omissions = %+v", plan.omissions)
	}
	if len(plan.changes) != 0 {
		t.Fatalf("unspecified omission mode should not build changes, got %v", plan.changes)
	}
}

func TestBuildPlanWithRemoveOmittedSummarizesDeletes(t *testing.T) {
	parsed, errs := parseSiteMapCSV([]byte(validCSV()))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}

	plan := buildPlan(parsed, testSnapshot(), pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %v", plan.errors)
	}
	if !hasChange(plan.changes, pb.ImportOperation_IMPORT_OPERATION_UNASSIGN, "miner", 1) {
		t.Fatalf("changes = %+v, want omitted miner unassign", plan.changes)
	}
	if !hasChange(plan.changes, pb.ImportOperation_IMPORT_OPERATION_DELETE, "rack", 1) {
		t.Fatalf("changes = %+v, want omitted rack delete", plan.changes)
	}
	if !hasChange(plan.changes, pb.ImportOperation_IMPORT_OPERATION_DELETE, "building", 1) {
		t.Fatalf("changes = %+v, want omitted building delete", plan.changes)
	}
	if !hasChange(plan.changes, pb.ImportOperation_IMPORT_OPERATION_DELETE, "site", 1) {
		t.Fatalf("changes = %+v, want omitted site delete", plan.changes)
	}
}

func TestBuildPlanSummarizesNewTopologyRows(t *testing.T) {
	csv := strings.Replace(validCSV(), "Site A,\n", "Site A,\nNew Site,\n", 1)
	csv = strings.Replace(csv, "Building A,,NAME:Site A,2,2\n", "Building A,,NAME:Site A,2,2\nNew Building,,NAME:Site A,2,2\n", 1)
	csv = strings.Replace(csv, "Rack A,,NAME:Building A,,Z1,4,6,BOTTOM_LEFT,0,0\n", "Rack A,,NAME:Building A,,Z1,4,6,BOTTOM_LEFT,0,0\nNew Rack,,NAME:Building A,,Z1,4,6,BOTTOM_LEFT,0,1\n", 1)
	parsed, errs := parseSiteMapCSV([]byte(csv))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}

	plan := buildPlan(parsed, testSnapshotMatchingValidCSV(), pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %v, want new topology rows accepted", plan.errors)
	}
	for _, entityType := range []string{"site", "building", "rack"} {
		if !hasChange(plan.changes, pb.ImportOperation_IMPORT_OPERATION_CREATE, entityType, 1) {
			t.Fatalf("changes = %+v, want create summary for %s", plan.changes, entityType)
		}
	}
}

func TestBuildPlanAllowsMinerPlacementIntoNewTopologyRows(t *testing.T) {
	csv := strings.Replace(validCSV(), "Site A,\n", "Site A,\nNew Site,\n", 1)
	csv = strings.Replace(csv, "Building A,,NAME:Site A,2,2\n", "Building A,,NAME:Site A,2,2\nNew Building,,NAME:New Site,2,2\n", 1)
	csv = strings.Replace(csv, "Rack A,,NAME:Building A,,Z1,4,6,BOTTOM_LEFT,0,0\n", "Rack A,,NAME:Building A,,Z1,4,6,BOTTOM_LEFT,0,0\nNew Rack,,NAME:New Building,,Z2,4,6,BOTTOM_LEFT,0,1\n", 1)
	csv = strings.Replace(csv, "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,NAME:Rack A,0,0", "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,NAME:New Rack,0,0", 1)
	parsed, errs := parseSiteMapCSV([]byte(csv))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}

	plan := buildPlan(parsed, testSnapshotMatchingValidCSV(), pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %v, want miner placement into new topology accepted", plan.errors)
	}
	if !hasChange(plan.changes, pb.ImportOperation_IMPORT_OPERATION_MOVE, "miner", 1) {
		t.Fatalf("changes = %+v, want miner move into new rack", plan.changes)
	}
}

func TestBuildPlanAcceptsExportedUnassignedBuildings(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": nil,
		"BUILDING": {
			{"__row": "5", "site": "", "building": "Unassigned Building", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK":  nil,
		"MINER": nil,
	}}
	snap := &snapshot{buildings: []buildingmodels.Building{{SiteLabel: "", Name: "Unassigned Building", Aisles: 1, RacksPerAisle: 1}}}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %v, want unassigned building row accepted", plan.errors)
	}
	if plan.omissions.GetBuildings() != 0 {
		t.Fatalf("building omissions = %d, want 0", plan.omissions.GetBuildings())
	}
}

func TestBuildPlanAcceptsDuplicateUnassignedBuildingsWithIDs(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": nil,
		"BUILDING": {
			{"__row": "5", "id": "10", "name": "Unassigned Building", "aisles": "1", "racks_per_aisle": "1"},
			{"__row": "6", "id": "11", "name": "Unassigned Building", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK":  nil,
		"MINER": nil,
	}}
	snap := &snapshot{buildings: []buildingmodels.Building{
		{ID: 10, Name: "Unassigned Building", Aisles: 1, RacksPerAisle: 1},
		{ID: 11, Name: "Unassigned Building", Aisles: 1, RacksPerAisle: 1},
	}}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %v, want duplicate unassigned building rows accepted when IDs disambiguate", plan.errors)
	}
	if plan.omissions.GetBuildings() != 0 {
		t.Fatalf("building omissions = %d, want 0", plan.omissions.GetBuildings())
	}
}

func TestBuildPlanRejectsAmbiguousBlankIDUnassignedBuildings(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": nil,
		"BUILDING": {
			{"__row": "5", "name": "Unassigned Building", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK":  nil,
		"MINER": nil,
	}}
	snap := &snapshot{buildings: []buildingmodels.Building{
		{ID: 10, Name: "Unassigned Building", Aisles: 1, RacksPerAisle: 1},
		{ID: 11, Name: "Unassigned Building", Aisles: 1, RacksPerAisle: 1},
	}}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if !hasValidationError(plan.errors, "BUILDING", `building "Unassigned Building" is ambiguous; add id`) {
		t.Fatalf("plan errors = %+v, want ambiguous blank-id building error", plan.errors)
	}
}

func TestBuildPlanRejectsImportedNamesOverServerLimits(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": {
			{"__row": "5", fieldName: strings.Repeat("s", maxSiteNameLength+1)},
		},
		"BUILDING": {
			{"__row": "9", fieldName: strings.Repeat("b", maxBuildingNameLength+1), "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK": {
			{"__row": "13", fieldLabel: strings.Repeat("r", maxRackLabelLength+1), "rows": "4", "columns": "6"},
			{"__row": "14", fieldLabel: "Rack A", "zone": strings.Repeat("z", maxRackZoneLength+1), "rows": "4", "columns": "6"},
		},
		"MINER": {
			{"__row": "17", "device_identifier": "miner-1", fieldName: strings.Repeat("m", maxMinerNameLength+1)},
		},
	}}

	plan := buildPlan(parsed, &snapshot{miners: []minerSnapshot{{DeviceIdentifier: "miner-1"}}}, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 5 {
		t.Fatalf("plan errors = %+v, want 5 length errors", plan.errors)
	}
	for _, err := range plan.errors {
		if !strings.Contains(err.GetMessage(), "must be at most") {
			t.Fatalf("error = %q, want max length error", err.GetMessage())
		}
	}
}

func TestBuildPlanRejectsDuplicateIdentityIDs(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": {
			{"__row": "3", fieldID: "10", fieldName: "Site A"},
			{"__row": "4", fieldID: "10", fieldName: "Site B"},
		},
		"BUILDING": {
			{"__row": "7", fieldID: "20", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "1"},
			{"__row": "8", fieldID: "20", fieldName: "Building B", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK": {
			{"__row": "11", fieldID: "30", fieldLabel: "Rack A", "rows": "4", "columns": "6"},
			{"__row": "12", fieldID: "30", fieldLabel: "Rack B", "rows": "4", "columns": "6"},
		},
		"MINER": nil,
	}}
	snap := &snapshot{
		sites:     []sitemodels.Site{{ID: 10, Name: "Site A"}},
		buildings: []buildingmodels.Building{{ID: 20, Name: "Building A", Aisles: 1, RacksPerAisle: 1}},
		racks:     []rackSnapshot{{ID: 30, Label: "Rack A", Rows: 4, Columns: 6}},
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	for _, section := range []string{"SITE", "BUILDING", "RACK"} {
		if !hasValidationError(plan.errors, section, "duplicate id") {
			t.Fatalf("plan errors = %+v, want duplicate id error for %s", plan.errors, section)
		}
	}
}

func TestBuildPlanInfersRackSiteAfterBuildingSiteIDNormalization(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": {
			{"__row": "3", fieldID: "1", fieldName: "Site A"},
		},
		"BUILDING": {
			{"__row": "7", fieldName: "Building A", fieldSite: "1", "aisles": "1", "racks_per_aisle": "2"},
		},
		"RACK": {
			{"__row": "11", fieldLabel: "Rack A", fieldBuilding: "NAME:Building A", "rows": "4", "columns": "6", "order_index": "BOTTOM_LEFT"},
		},
		"MINER": nil,
	}}
	snap := &snapshot{sites: []sitemodels.Site{{ID: 1, Name: "Site A"}}}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %+v, want site-backed rack inference accepted", plan.errors)
	}
	if got := parsed.sections["RACK"][0][fieldSite]; got != "Site A" {
		t.Fatalf("rack site = %q, want inferred Site A", got)
	}
}

func TestResolveReferencesRefreshesRackSiteFromSiteID(t *testing.T) {
	siteAID := int64(1)
	siteBID := int64(2)
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE":     nil,
		"BUILDING": nil,
		"RACK": {
			{"__row": "11", fieldID: "20", fieldLabel: "Rack A", fieldSite: "2"},
		},
		"MINER": {
			{"__row": "15", "device_identifier": "miner-1", fieldRack: "20"},
		},
	}}
	snap := &snapshot{
		sites: []sitemodels.Site{
			{ID: siteAID, Name: "Site A"},
			{ID: siteBID, Name: "Site B"},
		},
		racks:  []rackSnapshot{{ID: 20, SiteID: &siteAID, Site: "Site A", Label: "Rack A"}},
		miners: []minerSnapshot{{DeviceIdentifier: "miner-1"}},
	}

	errs := resolveReferences(parsed, snap)
	if len(errs) != 0 {
		t.Fatalf("resolveReferences errors = %+v, want rack site id to canonicalize", errs)
	}
	if got := parsed.sections["RACK"][0][fieldSite]; got != "Site B" {
		t.Fatalf("rack site = %q, want canonical Site B", got)
	}
}

func TestResolveReferencesResolvesRackBuildingMoveFromID(t *testing.T) {
	siteAID := int64(1)
	siteBID := int64(2)
	buildingAID := int64(10)
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE":     nil,
		"BUILDING": nil,
		"RACK": {
			{"__row": "11", fieldID: "20", fieldLabel: "Rack A", fieldBuilding: "11"},
		},
		"MINER": {
			{"__row": "15", "device_identifier": "miner-1", fieldRack: "20"},
		},
	}}
	snap := &snapshot{
		sites: []sitemodels.Site{
			{ID: siteAID, Name: "Site A"},
			{ID: siteBID, Name: "Site B"},
		},
		buildings: []buildingmodels.Building{
			{ID: 10, SiteID: &siteAID, SiteLabel: "Site A", Name: "Building A"},
			{ID: 11, SiteID: &siteBID, SiteLabel: "Site B", Name: "Building B"},
		},
		racks:  []rackSnapshot{{ID: 20, SiteID: &siteAID, BuildingID: &buildingAID, Site: "Site A", Building: "Building A", Label: "Rack A"}},
		miners: []minerSnapshot{{DeviceIdentifier: "miner-1"}},
	}

	errs := resolveReferences(parsed, snap)
	if len(errs) != 0 {
		t.Fatalf("resolveReferences errors = %+v, want id-based rack building move to canonicalize", errs)
	}
	if got := parsed.sections["RACK"][0][fieldBuilding]; got != "Building B" {
		t.Fatalf("rack building = %q, want canonical Building B", got)
	}
	if got := parsed.sections["RACK"][0][fieldSite]; got != "Site B" {
		t.Fatalf("rack site = %q, want Site B inherited from building", got)
	}
}

func TestBuildPlanRejectsIDRenamesCollidingWithRetainedOmittedTopology(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": {
			{"__row": "3", fieldID: "1", fieldName: "Site B"},
		},
		"BUILDING": {
			{"__row": "7", fieldID: "10", fieldName: "Building B", fieldSite: "3", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK": {
			{"__row": "11", fieldID: "20", fieldLabel: "Rack B", "rows": "4", "columns": "6", "order_index": "BOTTOM_LEFT"},
		},
		"MINER": nil,
	}}
	siteCID := int64(3)
	snap := &snapshot{
		sites: []sitemodels.Site{
			{ID: 1, Name: "Site A"},
			{ID: 2, Name: "Site B"},
			{ID: 3, Name: "Site C"},
		},
		buildings: []buildingmodels.Building{
			{ID: 10, SiteID: &siteCID, SiteLabel: "Site C", Name: "Building A", Aisles: 1, RacksPerAisle: 1},
			{ID: 11, SiteID: &siteCID, SiteLabel: "Site C", Name: "Building B", Aisles: 1, RacksPerAisle: 1},
		},
		racks: []rackSnapshot{
			{ID: 20, Label: "Rack A", Rows: 4, Columns: 6},
			{ID: 21, Label: "Rack B", Rows: 4, Columns: 6},
		},
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	for _, want := range []struct {
		section string
		message string
	}{
		{"SITE", "duplicate retained site name"},
		{"BUILDING", "duplicate retained building name at site"},
		{"RACK", "duplicate retained rack label"},
	} {
		if !hasValidationError(plan.errors, want.section, want.message) {
			t.Fatalf("plan errors = %+v, want %s error containing %q", plan.errors, want.section, want.message)
		}
	}
}

func TestBuildPlanRejectsTransientSiteRenameCollisions(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": {
			{"__row": "3", fieldID: "1", fieldName: "Site B"},
			{"__row": "4", fieldID: "2", fieldName: "Site A"},
		},
		"BUILDING": nil,
		"RACK":     nil,
		"MINER":    nil,
	}}
	snap := &snapshot{sites: []sitemodels.Site{
		{ID: 1, Name: "Site A"},
		{ID: 2, Name: "Site B"},
	}}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if !hasValidationError(plan.errors, "SITE", `site rename target "Site B" is currently used by site_id 2`) {
		t.Fatalf("plan errors = %+v, want transient site rename collision", plan.errors)
	}
	if !hasValidationError(plan.errors, "SITE", `site rename target "Site A" is currently used by site_id 1`) {
		t.Fatalf("plan errors = %+v, want reciprocal transient site rename collision", plan.errors)
	}
}

func TestBuildPlanRejectsRemoveOmittedDuplicateFinalBuildingNames(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": {
			{"__row": "3", fieldID: "1", fieldName: "Site A"},
		},
		"BUILDING": {
			{"__row": "7", fieldID: "10", fieldName: "Building B", fieldSite: "1", "aisles": "1", "racks_per_aisle": "1"},
			{"__row": "8", fieldID: "11", fieldName: "Building B", fieldSite: "1", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK":  nil,
		"MINER": nil,
	}}
	siteID := int64(1)
	snap := &snapshot{
		sites: []sitemodels.Site{{ID: siteID, Name: "Site A"}},
		buildings: []buildingmodels.Building{
			{ID: 10, SiteID: &siteID, SiteLabel: "Site A", Name: "Building A", Aisles: 1, RacksPerAisle: 1},
			{ID: 11, SiteID: &siteID, SiteLabel: "Site A", Name: "Building C", Aisles: 1, RacksPerAisle: 1},
		},
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if !hasValidationError(plan.errors, "BUILDING", "duplicate building name at site") {
		t.Fatalf("plan errors = %+v, want duplicate final building name", plan.errors)
	}
}

func TestBuildPlanRejectsTransientBuildingMoveRenameCollisions(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": {
			{"__row": "3", fieldID: "1", fieldName: "Site A"},
			{"__row": "4", fieldID: "2", fieldName: "Site B"},
		},
		"BUILDING": {
			{"__row": "7", fieldID: "10", fieldName: "Building Y", fieldSite: "2", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK":  nil,
		"MINER": nil,
	}}
	siteAID := int64(1)
	siteBID := int64(2)
	snap := &snapshot{
		sites: []sitemodels.Site{
			{ID: siteAID, Name: "Site A"},
			{ID: siteBID, Name: "Site B"},
		},
		buildings: []buildingmodels.Building{
			{ID: 10, SiteID: &siteAID, SiteLabel: "Site A", Name: "Building X", Aisles: 1, RacksPerAisle: 1},
			{ID: 11, SiteID: &siteBID, SiteLabel: "Site B", Name: "Building X", Aisles: 1, RacksPerAisle: 1},
		},
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if !hasValidationError(plan.errors, "BUILDING", `building move target "Site B"/"Building X" is currently used by building_id 11`) {
		t.Fatalf("plan errors = %+v, want transient building move collision", plan.errors)
	}
}

func TestBuildPlanRejectsSameSiteBuildingRenameSwaps(t *testing.T) {
	siteID := int64(1)
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": {
			{"__row": "3", fieldID: "1", fieldName: "Site A"},
		},
		"BUILDING": {
			{"__row": "7", fieldID: "10", fieldName: "Building Y", fieldSite: "1", "aisles": "1", "racks_per_aisle": "1"},
			{"__row": "8", fieldID: "11", fieldName: "Building X", fieldSite: "1", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK":  nil,
		"MINER": nil,
	}}
	snap := &snapshot{
		sites: []sitemodels.Site{{ID: siteID, Name: "Site A"}},
		buildings: []buildingmodels.Building{
			{ID: 10, SiteID: &siteID, SiteLabel: "Site A", Name: "Building X", Aisles: 1, RacksPerAisle: 1},
			{ID: 11, SiteID: &siteID, SiteLabel: "Site A", Name: "Building Y", Aisles: 1, RacksPerAisle: 1},
		},
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if !hasValidationError(plan.errors, "BUILDING", `building rename target "Site A"/"Building Y" is currently used by building_id 11`) {
		t.Fatalf("plan errors = %+v, want first transient building rename collision", plan.errors)
	}
	if !hasValidationError(plan.errors, "BUILDING", `building rename target "Site A"/"Building X" is currently used by building_id 10`) {
		t.Fatalf("plan errors = %+v, want reciprocal transient building rename collision", plan.errors)
	}
}

func TestBuildPlanRejectsTransientRackLabelCollisions(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE":     nil,
		"BUILDING": nil,
		"RACK": {
			{"__row": "7", fieldID: "20", fieldLabel: "Rack B", "rows": "4", "columns": "6"},
			{"__row": "8", fieldID: "21", fieldLabel: "Rack A", "rows": "4", "columns": "6"},
		},
		"MINER": nil,
	}}
	snap := &snapshot{racks: []rackSnapshot{
		{ID: 20, Label: "Rack A", Rows: 4, Columns: 6},
		{ID: 21, Label: "Rack B", Rows: 4, Columns: 6},
	}}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if !hasValidationError(plan.errors, "RACK", `rack rename target "Rack B" is currently used by rack_id 21`) {
		t.Fatalf("plan errors = %+v, want transient rack label collision", plan.errors)
	}
	if !hasValidationError(plan.errors, "RACK", `rack rename target "Rack A" is currently used by rack_id 20`) {
		t.Fatalf("plan errors = %+v, want reciprocal transient rack label collision", plan.errors)
	}
}

func TestBuildPlanRejectsRackRenameToOmittedLabel(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE":     nil,
		"BUILDING": nil,
		"RACK": {
			{"__row": "7", fieldID: "20", fieldLabel: "Rack B", "rows": "4", "columns": "6"},
		},
		"MINER": nil,
	}}
	snap := &snapshot{racks: []rackSnapshot{
		{ID: 20, Label: "Rack A", Rows: 4, Columns: 6},
		{ID: 21, Label: "Rack B", Rows: 4, Columns: 6},
	}}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if !hasValidationError(plan.errors, "RACK", `rack rename target "Rack B" is currently used by rack_id 21`) {
		t.Fatalf("plan errors = %+v, want omitted rack label collision", plan.errors)
	}
}

func TestBuildPlanRejectsUnassignedBuildingIDMoveCollidingWithRetainedBuilding(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": {
			{"__row": "3", fieldName: "Site A"},
		},
		"BUILDING": {
			{"__row": "7", fieldID: "10", fieldName: "Building B", fieldSite: "NAME:Site A", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK":  nil,
		"MINER": nil,
	}}
	siteID := int64(1)
	snap := &snapshot{
		sites: []sitemodels.Site{{ID: 1, Name: "Site A"}},
		buildings: []buildingmodels.Building{
			{ID: 10, Name: "Building A", Aisles: 1, RacksPerAisle: 1},
			{ID: 11, SiteID: &siteID, SiteLabel: "Site A", Name: "Building B", Aisles: 1, RacksPerAisle: 1},
		},
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if !hasValidationError(plan.errors, "BUILDING", "duplicate retained building name at site") {
		t.Fatalf("plan errors = %+v, want retained building collision", plan.errors)
	}
}

func TestBuildPlanRejectsOldSiteReferenceAfterIDRename(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": {
			{"__row": "3", fieldID: "1", fieldName: "New Site"},
		},
		"BUILDING": {
			{"__row": "7", fieldName: "Building A", fieldSite: "NAME:Old Site", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK":  nil,
		"MINER": nil,
	}}
	snap := &snapshot{
		sites: []sitemodels.Site{{ID: 1, Name: "Old Site"}},
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if !hasValidationError(plan.errors, "BUILDING", "site reference NAME:Old Site matches no SITE row in this import") {
		t.Fatalf("plan errors = %+v, want old site reference rejected after ID rename", plan.errors)
	}
}

func TestBuildPlanRejectsOldBuildingReferenceAfterIDRename(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": {
			{"__row": "3", fieldName: "Site A"},
		},
		"BUILDING": {
			{"__row": "7", fieldID: "10", fieldName: "New Building", fieldSite: "NAME:Site A", "aisles": "2", "racks_per_aisle": "2"},
		},
		"RACK": {
			{"__row": "11", fieldLabel: "Rack A", fieldBuilding: "NAME:Old Building", "rows": "4", "columns": "6"},
		},
		"MINER": nil,
	}}
	snap := &snapshot{
		sites:     []sitemodels.Site{{Name: "Site A"}},
		buildings: []buildingmodels.Building{{ID: 10, SiteLabel: "Site A", Name: "Old Building", Aisles: 2, RacksPerAisle: 2}},
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) == 0 {
		t.Fatal("plan errors = nil, want old building reference rejected after ID rename")
	}
	for _, err := range plan.errors {
		if strings.Contains(err.GetMessage(), "building reference NAME:Old Building matches no BUILDING row in this import") {
			return
		}
	}
	t.Fatalf("plan errors = %+v, want unknown old building reference", plan.errors)
}

func TestBuildPlanResolvesSiteNameBuildingMoveForIDReferences(t *testing.T) {
	oldSiteID := int64(1)
	newSiteID := int64(2)
	buildingID := int64(10)
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": nil,
		"BUILDING": {
			{"__row": "7", fieldID: "10", fieldName: "Building A", fieldSite: "2", "aisles": "1", "racks_per_aisle": "2"},
		},
		"RACK": {
			{"__row": "11", fieldLabel: "Rack A", fieldBuilding: "10", "rows": "4", "columns": "6"},
		},
		"MINER": {
			{"__row": "13", "device_identifier": "miner-1", "serial_number": "SN1", fieldName: "Miner 1", "ip_address": "10.0.0.5", "mac_address": "aa:bb:cc:dd:ee:ff", fieldBuilding: "10"},
		},
	}}
	snap := &snapshot{
		sites: []sitemodels.Site{
			{ID: oldSiteID, Name: "Site A"},
			{ID: newSiteID, Name: "Site B"},
		},
		buildings: []buildingmodels.Building{
			{ID: buildingID, SiteID: &oldSiteID, SiteLabel: "Site A", Name: "Building A", Aisles: 1, RacksPerAisle: 2},
		},
		miners: []minerSnapshot{{DeviceIdentifier: "miner-1", SerialNumber: "SN1", Name: "Miner 1", IPAddress: "10.0.0.5", MACAddress: "aa:bb:cc:dd:ee:ff"}},
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %+v, want site-name building move accepted", plan.errors)
	}
	if got := parsed.sections["RACK"][0][fieldSite]; got != "Site B" {
		t.Fatalf("rack site = %q, want inherited Site B", got)
	}
	if got := parsed.sections["MINER"][0][fieldSite]; got != "Site B" {
		t.Fatalf("miner site = %q, want inherited Site B", got)
	}
}

func TestBuildPlanAllowsRackTargetDisambiguatedByBuildingID(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": nil,
		"BUILDING": {
			{"__row": "5", fieldID: "10", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "2"},
			{"__row": "6", fieldID: "11", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "2"},
		},
		"RACK": {
			{"__row": "9", fieldLabel: "Rack A", fieldBuilding: "10", "rows": "4", "columns": "6"},
		},
		"MINER": nil,
	}}
	snap := &snapshot{buildings: []buildingmodels.Building{
		{ID: 10, Name: "Building A", Aisles: 1, RacksPerAisle: 2},
		{ID: 11, Name: "Building A", Aisles: 1, RacksPerAisle: 2},
	}}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %+v, want building-id-disambiguated rack accepted", plan.errors)
	}
}

func TestBuildPlanRemoveOmittedAllowsIDQualifiedDuplicateUnassignedBuildingReferences(t *testing.T) {
	buildingID := int64(10)
	otherBuildingID := int64(11)
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": nil,
		"BUILDING": {
			{"__row": "5", fieldID: "10", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "2"},
			{"__row": "6", fieldID: "11", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "2"},
		},
		"RACK": {
			{"__row": "9", fieldID: "20", fieldLabel: "Rack A", fieldBuilding: "10", "rows": "4", "columns": "6"},
		},
		"MINER": {
			{"__row": "13", "device_identifier": "miner-1", "serial_number": "SN1", fieldName: "Miner 1", "ip_address": "10.0.0.5", "mac_address": "aa:bb:cc:dd:ee:ff", fieldBuilding: "11"},
		},
	}}
	snap := &snapshot{
		buildings: []buildingmodels.Building{
			{ID: buildingID, Name: "Building A", Aisles: 1, RacksPerAisle: 2},
			{ID: otherBuildingID, Name: "Building A", Aisles: 1, RacksPerAisle: 2},
		},
		racks: []rackSnapshot{{ID: 20, Label: "Rack A", BuildingID: &buildingID, Building: "Building A", Rows: 4, Columns: 6}},
		miners: []minerSnapshot{{
			DeviceIdentifier: "miner-1",
			SerialNumber:     "SN1",
			Name:             "Miner 1",
			IPAddress:        "10.0.0.5",
			MACAddress:       "aa:bb:cc:dd:ee:ff",
			BuildingID:       &otherBuildingID,
			Building:         "Building A",
		}},
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %+v, want ID-qualified building references accepted", plan.errors)
	}
}

func TestBuildPlanRemoveOmittedRejectsOmittedBuildingIDReferences(t *testing.T) {
	omittedBuildingID := int64(10)
	retainedBuildingID := int64(11)
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": nil,
		"BUILDING": {
			{"__row": "6", fieldID: "11", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "2"},
		},
		"RACK": {
			{"__row": "9", fieldID: "20", fieldLabel: "Rack A", fieldBuilding: "10", "rows": "4", "columns": "6"},
		},
		"MINER": {
			{"__row": "13", "device_identifier": "miner-1", "serial_number": "SN1", fieldName: "Miner 1", "ip_address": "10.0.0.5", "mac_address": "aa:bb:cc:dd:ee:ff", fieldBuilding: "10"},
		},
	}}
	snap := &snapshot{
		buildings: []buildingmodels.Building{
			{ID: omittedBuildingID, Name: "Building X", Aisles: 1, RacksPerAisle: 2},
			{ID: retainedBuildingID, Name: "Building A", Aisles: 1, RacksPerAisle: 2},
		},
		racks: []rackSnapshot{{ID: 20, Label: "Rack A", BuildingID: &omittedBuildingID, Building: "Building X", Rows: 4, Columns: 6}},
		miners: []minerSnapshot{{
			DeviceIdentifier: "miner-1",
			SerialNumber:     "SN1",
			Name:             "Miner 1",
			IPAddress:        "10.0.0.5",
			MACAddress:       "aa:bb:cc:dd:ee:ff",
			BuildingID:       &omittedBuildingID,
			Building:         "Building X",
		}},
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if !hasValidationError(plan.errors, "RACK", `rack building "Building X" for site "" is omitted`) {
		t.Fatalf("plan errors = %+v, want rack omitted building error", plan.errors)
	}
	if !hasValidationError(plan.errors, "MINER", `miner building "Building X" for site "" is omitted`) {
		t.Fatalf("plan errors = %+v, want miner omitted building error", plan.errors)
	}
}

func TestBuildPlanRejectsOldRackReferenceAfterIDRename(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE":     nil,
		"BUILDING": nil,
		"RACK": {
			{"__row": "9", fieldID: "20", fieldLabel: "New Rack", "rows": "4", "columns": "6"},
		},
		"MINER": {
			{"__row": "13", "device_identifier": "miner-1", "serial_number": "SN1", fieldName: "Miner 1", "ip_address": "10.0.0.5", "mac_address": "aa:bb:cc:dd:ee:ff", fieldRack: "NAME:Old Rack", "rack_row": "0", "rack_col": "0"},
		},
	}}
	snap := &snapshot{
		racks:  []rackSnapshot{{ID: 20, Label: "Old Rack", Rows: 4, Columns: 6}},
		miners: []minerSnapshot{{DeviceIdentifier: "miner-1", SerialNumber: "SN1", Name: "Miner 1", IPAddress: "10.0.0.5", MACAddress: "aa:bb:cc:dd:ee:ff"}},
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if !hasValidationError(plan.errors, "MINER", "rack reference NAME:Old Rack matches no RACK row in this import") {
		t.Fatalf("plan errors = %+v, want old rack reference rejected after ID rename", plan.errors)
	}
}

func TestBuildPlanRejectsBuildingUnknownSite(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": nil,
		"BUILDING": {
			{"__row": "5", "site": "NAME:Typo Site", "building": "New Building", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK":  nil,
		"MINER": nil,
	}}
	snap := &snapshot{sites: []sitemodels.Site{{Name: "Site A"}}}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 1 || plan.errors[0].GetSection() != "BUILDING" || plan.errors[0].GetMessage() != "site reference NAME:Typo Site matches no SITE row in this import" {
		t.Fatalf("plan errors = %+v, want building unknown site error", plan.errors)
	}
	if len(plan.changes) != 0 {
		t.Fatalf("changes = %+v, want no token-eligible changes when validation fails", plan.changes)
	}
}

func TestBuildPlanRemoveOmittedRejectsReferencesToOmittedParents(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": nil,
		"BUILDING": {
			{"__row": "5", fieldID: "10", "name": "Building A", "site": "1", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK": {
			{"__row": "9", fieldID: "20", "label": "Rack A", "building": "10", "rows": "4", "columns": "6"},
		},
		"MINER": {
			{"__row": "13", "device_identifier": "miner-1", "serial_number": "SN1", "name": "Miner 1", "ip_address": "10.0.0.5", "mac_address": "aa:bb:cc:dd:ee:ff", "site": "1"},
		},
	}}
	siteID := int64(1)
	buildingID := int64(10)
	snap := &snapshot{
		sites:     []sitemodels.Site{{ID: 1, Name: "Site A"}},
		buildings: []buildingmodels.Building{{ID: 10, SiteID: &siteID, SiteLabel: "Site A", Name: "Building A"}},
		racks:     []rackSnapshot{{ID: 20, BuildingID: &buildingID, Label: "Rack A", Site: "Site A", Building: "Building A", Rows: 4, Columns: 6}},
		miners:    []minerSnapshot{{DeviceIdentifier: "miner-1", SerialNumber: "SN1", Name: "Miner 1", IPAddress: "10.0.0.5", MACAddress: "aa:bb:cc:dd:ee:ff", Site: "Site A"}},
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if len(plan.errors) == 0 {
		t.Fatal("plan errors = nil, want remove-mode omitted parent reference errors")
	}
	for _, err := range plan.errors {
		if !strings.Contains(err.GetMessage(), "is omitted") {
			t.Fatalf("error = %q, want omitted reference error", err.GetMessage())
		}
	}
	if len(plan.changes) != 0 {
		t.Fatalf("changes = %+v, want no changes when validation fails", plan.changes)
	}
}

func TestCommitTokenChangesWithSnapshotDrift(t *testing.T) {
	parsed, errs := parseSiteMapCSV([]byte(validCSV()))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}
	before := testSnapshotMatchingValidCSV()
	plan := buildPlan(parsed, before, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %v", plan.errors)
	}
	after := testSnapshotMatchingValidCSV()
	after.miners[0].RackCol = "1"

	if commitToken(parsed, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED, plan, before) == commitToken(parsed, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED, plan, after) {
		t.Fatal("commit token must change when live site-map snapshot changes")
	}
}

func TestBuildPlanWithNoOmissionsSummarizesMinerPlacementChanges(t *testing.T) {
	csv := strings.Replace(validCSV(), "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,NAME:Rack A,0,0", "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,NAME:Rack A,0,1", 1)
	parsed, errs := parseSiteMapCSV([]byte(csv))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}

	plan := buildPlan(parsed, testSnapshotMatchingValidCSV(), pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %v", plan.errors)
	}
	if hasOmissions(plan.omissions) {
		t.Fatalf("omissions = %+v, want none", plan.omissions)
	}

	var sawMinerMove bool
	for _, change := range plan.changes {
		if change.Operation == pb.ImportOperation_IMPORT_OPERATION_MOVE && change.EntityType == "miner" && change.Count == 1 {
			sawMinerMove = true
		}
	}
	if !sawMinerMove {
		t.Fatalf("changes did not include expected miner move summary: %+v", plan.changes)
	}
}

func TestBuildPlanSummarizesMinerRenames(t *testing.T) {
	csv := strings.Replace(validCSV(), "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,NAME:Rack A,0,0", "miner-1,SN1,Renamed Miner,10.0.0.5,aa:bb:cc:dd:ee:ff,,,NAME:Rack A,0,0", 1)
	parsed, errs := parseSiteMapCSV([]byte(csv))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}

	plan := buildPlan(parsed, testSnapshotMatchingValidCSV(), pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %v, want miner rename accepted", plan.errors)
	}
	if !hasChange(plan.changes, pb.ImportOperation_IMPORT_OPERATION_RENAME, "miner", 1) {
		t.Fatalf("changes = %+v, want miner rename summary", plan.changes)
	}
}

func TestBuildPlanReportsRowCitedErrors(t *testing.T) {
	csv := strings.Replace(
		validCSV(),
		"miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,NAME:Rack A,0,0\n",
		"miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,NAME:Rack A,0,0\nminer-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,NAME:Rack A,0,0\n",
		1,
	)
	parsed, errs := parseSiteMapCSV([]byte(csv))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}

	plan := buildPlan(parsed, testSnapshot(), pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) == 0 {
		t.Fatal("expected validation errors")
	}
	var sawDuplicateSlot, sawDuplicateMiner bool
	for _, err := range plan.errors {
		if err.GetSection() == "MINER" && err.GetRow() == 13 && err.GetMessage() == "duplicate rack slot" {
			sawDuplicateSlot = true
		}
		if err.GetSection() == "MINER" && err.GetRow() == 13 && err.GetMessage() == "duplicate device_identifier" {
			sawDuplicateMiner = true
		}
	}
	if !sawDuplicateSlot || !sawDuplicateMiner {
		t.Fatalf("expected row-cited duplicate errors at row 13, got %+v", plan.errors)
	}
}

func TestParseSiteMapCSVUnescapesFormulaProtectedExports(t *testing.T) {
	// Only the identity cell (the rack's own label) is escaped; the miner's rack
	// reference points at that same-import rack by its (unescaped) label.
	csv := strings.Replace(validCSV(), "Rack A,,NAME:Building A", "'-Rack,,NAME:Building A", 1)
	csv = strings.Replace(csv, "NAME:Rack A", "NAME:-Rack", 1)
	parsed, errs := parseSiteMapCSV([]byte(csv))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}

	if got := parsed.sections["RACK"][0]["label"]; got != "-Rack" {
		t.Fatalf("rack = %q, want unescaped -Rack", got)
	}
	if got := parsed.sections["MINER"][0]["rack"]; got != "NAME:-Rack" {
		t.Fatalf("miner rack = %q, want reference to -Rack", got)
	}
}

func TestCleanRoundTripsLiteralApostropheFormulaLikeLabels(t *testing.T) {
	exported := clean("'-Rack")
	if exported != "''-Rack" {
		t.Fatalf("exported value = %q, want doubled apostrophe", exported)
	}
	if got := unescapeCleanedValue(exported); got != "'-Rack" {
		t.Fatalf("unescaped value = %q, want literal apostrophe preserved", got)
	}
	if got := unescapeCleanedValue(clean("-Rack")); got != "-Rack" {
		t.Fatalf("formula-guarded value = %q, want -Rack", got)
	}
}

func TestCleanEscapesSectionMarkerShapedValues(t *testing.T) {
	exported := clean("# SECTION: RACK")
	if exported != "'# SECTION: RACK" {
		t.Fatalf("exported value = %q, want section marker escaped", exported)
	}
	if got := unescapeCleanedValue(exported); got != "# SECTION: RACK" {
		t.Fatalf("unescaped value = %q, want section marker value preserved", got)
	}
}

func TestCleanPreservesIdentifierWhitespace(t *testing.T) {
	if got := clean(" Rack A "); got != " Rack A " {
		t.Fatalf("cleaned value = %q, want surrounding whitespace preserved", got)
	}
}

func TestParseSiteMapCSVPreservesDataCellWhitespace(t *testing.T) {
	csv := strings.Replace(validCSV(), "Site A,\n", "\" Site A \",\n", 1)
	parsed, errs := parseSiteMapCSV([]byte(csv))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}
	if got := parsed.sections["SITE"][0]["name"]; got != " Site A " {
		t.Fatalf("site = %q, want surrounding whitespace preserved", got)
	}
}

func TestNullableInt64EqualComparesPlacementIDs(t *testing.T) {
	id := func(value int64) *int64 { return &value }

	if nullableInt64Equal(id(1), id(2)) {
		t.Fatal("different placement IDs should not compare equal")
	}
	if nullableInt64Equal(id(1), nil) {
		t.Fatal("assigned and unassigned placement IDs should not compare equal")
	}
	if !nullableInt64Equal(id(1), id(1)) {
		t.Fatal("matching placement IDs should compare equal")
	}
	if !nullableInt64Equal(nil, nil) {
		t.Fatal("two unassigned placement IDs should compare equal")
	}
}

func TestExportedSectionMarkerShapedSiteRoundTrips(t *testing.T) {
	csvData, err := buildSiteMapCSV(&snapshot{
		sites: []sitemodels.Site{{Name: "# SECTION: RACK"}},
	})
	if err != nil {
		t.Fatalf("buildSiteMapCSV error = %v", err)
	}

	parsed, errs := parseSiteMapCSV(csvData)
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v\ncsv:\n%s", errs, string(csvData))
	}
	if got := parsed.sections["SITE"][0]["name"]; got != "# SECTION: RACK" {
		t.Fatalf("site = %q, want section-marker-shaped site name", got)
	}
}

func TestBuildPlanTreatsEscapedExportValuesAsNoOp(t *testing.T) {
	siteID := int64(1)
	buildingID := int64(2)
	snap := &snapshot{
		sites: []sitemodels.Site{{ID: siteID, Name: "-Site"}},
		buildings: []buildingmodels.Building{{
			ID:            buildingID,
			SiteID:        &siteID,
			SiteLabel:     "-Site",
			Name:          "+Building",
			Aisles:        2,
			RacksPerAisle: 2,
		}},
		racks: []rackSnapshot{{
			ID:              3,
			SiteID:          &siteID,
			BuildingID:      &buildingID,
			Site:            "-Site",
			Building:        "+Building",
			Label:           "-Rack",
			Zone:            "# SECTION: RACK",
			Rows:            4,
			Columns:         6,
			OrderIndex:      "BOTTOM_LEFT",
			AisleIndex:      "0",
			PositionInAisle: "0",
		}},
	}
	csvData, err := buildSiteMapCSV(snap)
	if err != nil {
		t.Fatalf("buildSiteMapCSV error = %v", err)
	}
	parsed, errs := parseSiteMapCSV(csvData)
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v\ncsv:\n%s", errs, string(csvData))
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %v", plan.errors)
	}
	if len(plan.changes) != 0 {
		t.Fatalf("changes = %+v, want escaped export values to reimport as no-op", plan.changes)
	}
}

func TestDesiredRackZoneClearsWhenRackLeavesBuildingScope(t *testing.T) {
	current := rackSnapshot{Site: "Site A", Building: "Building A", Zone: "Old Zone"}

	if got := desiredRackZone(map[string]string{"site": "Site A", "building": "Building B", "zone": "Old Zone"}, current); got != "" {
		t.Fatalf("zone crossing building = %q, want cleared", got)
	}
	if got := desiredRackZone(map[string]string{"site": "", "building": "", "zone": "Old Zone"}, current); got != "" {
		t.Fatalf("zone leaving building = %q, want cleared", got)
	}
	if got := desiredRackZone(map[string]string{"site": "Site A", "building": "Building A", "zone": "New Zone"}, current); got != "New Zone" {
		t.Fatalf("zone staying in building = %q, want New Zone", got)
	}
}

func TestLogSiteMapImportActivitySummarizesChanges(t *testing.T) {
	ctrl := gomock.NewController(t)
	activityStore := mocks.NewMockActivityStore(ctrl)
	activitySvc := activity.NewService(activityStore)
	svc := NewService(nil, nil, nil, nil, nil, nil, activitySvc)
	orgID := int64(42)
	ctx := authn.SetInfo(context.Background(), &session.Info{
		OrganizationID: orgID,
		ExternalUserID: "usr_1",
		Username:       "alice",
	})
	plan := importPlan{changes: []*pb.ImportChangeSummary{
		{
			Operation:   pb.ImportOperation_IMPORT_OPERATION_UPDATE,
			EntityType:  "rack",
			Count:       2,
			Description: "rack rows with changed details",
		},
		{
			Operation:   pb.ImportOperation_IMPORT_OPERATION_MOVE,
			EntityType:  "miner",
			Count:       3,
			Description: "miner placement rows with changed site, building, rack, or slot",
		},
	}}

	activityStore.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event *activitymodels.Event) error {
			if event.Type != "site_map_import" {
				t.Fatalf("event type = %q, want site_map_import", event.Type)
			}
			if event.Category != activitymodels.CategoryFleetManagement {
				t.Fatalf("event category = %q, want fleet_management", event.Category)
			}
			if event.OrganizationID == nil || *event.OrganizationID != orgID {
				t.Fatalf("event org = %v, want %d", event.OrganizationID, orgID)
			}
			if event.Username == nil || *event.Username != "alice" {
				t.Fatalf("event username = %v, want alice", event.Username)
			}
			if event.ScopeCount == nil || *event.ScopeCount != 5 {
				t.Fatalf("event scope count = %v, want 5", event.ScopeCount)
			}
			changes, ok := event.Metadata["changes"].([]map[string]any)
			if !ok || len(changes) != 2 {
				t.Fatalf("event changes metadata = %#v, want two changes", event.Metadata["changes"])
			}
			if changes[0]["operation"] != "update" || changes[0]["entity_type"] != "rack" || changes[0]["count"] != int32(2) {
				t.Fatalf("first change metadata = %#v", changes[0])
			}
			return nil
		},
	)

	svc.logSiteMapImportActivity(ctx, orgID, plan)
}

func TestApplyMinerRowsClearsDirectPlacementWhenAssigningUnassignedRack(t *testing.T) {
	ctrl := gomock.NewController(t)
	siteStore := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	svc := NewService(siteStore, buildingStore, collectionStore, nil, nil, nil, nil)
	ctx := context.Background()
	orgID := int64(42)
	deviceIDs := []string{"miner-1"}
	rack := rackSnapshot{ID: 7, Label: "Rack A"}
	existing := map[string]minerSnapshot{
		"miner-1": {
			DeviceIdentifier: "miner-1",
			Site:             "Site A",
			Building:         "Building A",
		},
	}

	collectionStore.EXPECT().LockRacksForReparent(ctx, orgID, deviceIDs, rack.ID).Return([]int64{rack.ID}, nil)
	collectionStore.EXPECT().LockRackPlacementForWrite(ctx, rack.ID, orgID).Return(interfaces.RackPlacement{}, nil)
	collectionStore.EXPECT().RemoveDevicesFromAnyRack(ctx, orgID, deviceIDs, rack.ID).Return(int64(1), nil)
	collectionStore.EXPECT().AddDevicesToCollection(ctx, orgID, rack.ID, deviceIDs).Return(int64(1), nil)
	collectionStore.EXPECT().CascadeAddedDeviceSites(ctx, orgID, rack.ID, deviceIDs).Return(int64(0), nil)
	collectionStore.EXPECT().CascadeAddedDeviceBuildings(ctx, orgID, rack.ID, deviceIDs).Return(int64(0), nil)
	siteStore.EXPECT().AssignDevicesToSite(ctx, orgID, nil, deviceIDs).Return(int64(1), nil)
	buildingStore.EXPECT().AssignDevicesToBuilding(ctx, orgID, nil, deviceIDs).Return(int64(1), nil)
	collectionStore.EXPECT().ClearRackSlotPosition(ctx, rack.ID, "miner-1", orgID).Return(nil)

	existingMiner := existing["miner-1"]
	node := &resolvedMiner{
		deviceID:  "miner-1",
		rackLabel: "Rack A",
		moved:     true,
		existing:  &existingMiner,
	}
	if err := svc.applyMiners(ctx, orgID, []*resolvedMiner{node}, nil, nil, nil, map[string]rackSnapshot{"Rack A": rack}); err != nil {
		t.Fatalf("applyMiners error = %v", err)
	}
}

func TestDeleteOmittedSitesRejectsInfrastructureDevicesReferencedByProfiles(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()
	orgID := int64(42)
	siteStore := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	svc := NewService(siteStore, buildingStore, nil, nil, nil, nil, nil)

	gomock.InOrder(
		siteStore.EXPECT().LockSiteForWrite(ctx, orgID, int64(11)).Return(nil),
		siteStore.EXPECT().LockBuildingsBySiteForWrite(ctx, orgID, int64(11)).Return(nil),
		siteStore.EXPECT().LockInfrastructureDevicesBySiteForWrite(ctx, orgID, int64(11)).Return([]int64{70}, nil),
		siteStore.EXPECT().UnassignRacksFromBuildingsBySite(ctx, orgID, int64(11)).Return(int64(0), nil),
		buildingStore.EXPECT().ClearDeviceBuildingsBySite(ctx, orgID, int64(11)).Return(int64(0), nil),
		siteStore.EXPECT().SoftDeleteBuildingsBySite(ctx, orgID, int64(11)).Return(int64(0), nil),
		siteStore.EXPECT().UnassignRacksFromSite(ctx, orgID, int64(11)).Return(int64(0), nil),
		siteStore.EXPECT().UnassignDevicesFromSite(ctx, orgID, int64(11)).Return(int64(0), nil),
		siteStore.EXPECT().DeleteCurtailmentResponseProfilesBySite(ctx, orgID, int64(11)).Return(int64(0), nil),
		siteStore.EXPECT().CountResponseProfilesByInfrastructureDevices(ctx, orgID, []int64{70}).Return(int64(1), nil),
	)

	err := svc.deleteOmittedSites(ctx, orgID, []sitemodels.Site{{ID: 11}})
	if !fleeterror.IsFailedPreconditionError(err) {
		t.Fatalf("deleteOmittedSites error = %v, want failed precondition", err)
	}
}

func TestValidateOmittedSiteDeleteImpactsRejectsHiddenResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()
	orgID := int64(42)
	siteStore := mocks.NewMockSiteStore(ctrl)
	svc := NewService(siteStore, nil, nil, nil, nil, nil, nil)

	siteStore.EXPECT().CountCurtailmentResponseProfilesBySite(ctx, orgID, int64(11)).Return(int64(2), nil)
	siteStore.EXPECT().CountInfrastructureDevicesBySite(ctx, orgID, int64(11)).Return(int64(3), nil)

	errs, err := svc.validateOmittedSiteDeleteImpacts(ctx, orgID, []sitemodels.Site{{ID: 11, Name: "Site A"}})
	if err != nil {
		t.Fatalf("validateOmittedSiteDeleteImpacts error = %v", err)
	}
	if !hasValidationError(errs, "SITE", `omitted site "Site A" has curtailment response profiles`) {
		t.Fatalf("errors = %+v, want curtailment profile impact", errs)
	}
	if !hasValidationError(errs, "SITE", `omitted site "Site A" has infrastructure devices`) {
		t.Fatalf("errors = %+v, want infrastructure impact", errs)
	}
}

func TestValidateOmittedSiteDeleteImpactsAllowsEmptySites(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()
	orgID := int64(42)
	siteStore := mocks.NewMockSiteStore(ctrl)
	svc := NewService(siteStore, nil, nil, nil, nil, nil, nil)

	siteStore.EXPECT().CountCurtailmentResponseProfilesBySite(ctx, orgID, int64(11)).Return(int64(0), nil)
	siteStore.EXPECT().CountInfrastructureDevicesBySite(ctx, orgID, int64(11)).Return(int64(0), nil)

	errs, err := svc.validateOmittedSiteDeleteImpacts(ctx, orgID, []sitemodels.Site{{ID: 11, Name: "Site A"}})
	if err != nil {
		t.Fatalf("validateOmittedSiteDeleteImpacts error = %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("errors = %+v, want none", errs)
	}
}

func TestApplyImportPlanMovesBuildingsBeforeDeletingOmittedSites(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()
	orgID := int64(42)
	siteStore := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	transactor := mocks.NewMockTransactor(ctrl)
	svc := NewService(siteStore, buildingStore, collectionStore, nil, nil, transactor, nil)
	siteAID := int64(1)
	siteBID := int64(2)
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": {
			{"__row": "3", fieldID: "2", fieldName: "Site B"},
		},
		"BUILDING": {
			{"__row": "7", fieldID: "10", fieldName: "Building A", fieldSite: "2", "aisles": "1", "racks_per_aisle": "2"},
		},
		"RACK":  nil,
		"MINER": nil,
	}}
	snap := &snapshot{
		sites: []sitemodels.Site{
			{ID: siteAID, Name: "Site A"},
			{ID: siteBID, Name: "Site B"},
		},
		buildings: []buildingmodels.Building{{ID: 10, SiteID: &siteAID, SiteLabel: "Site A", Name: "Building A", Aisles: 1, RacksPerAisle: 2}},
	}

	transactor.EXPECT().RunInTx(ctx, gomock.Any()).DoAndReturn(func(txCtx context.Context, fn func(context.Context) error) error {
		return fn(txCtx)
	})
	gomock.InOrder(
		siteStore.EXPECT().LockSiteForWrite(ctx, orgID, siteBID).Return(nil),
		siteStore.EXPECT().LockBuildingForWrite(ctx, orgID, int64(10)).Return(nil),
		siteStore.EXPECT().AssignBuildingsToSiteBulk(ctx, orgID, []int64{10}, &siteBID).Return(int64(1), nil),
		siteStore.EXPECT().ReassignRacksUnderBuildingsBulk(ctx, orgID, []int64{10}, &siteBID).Return(int64(0), nil),
		siteStore.EXPECT().ReassignDevicesUnderBuildingsBulk(ctx, orgID, []int64{10}, &siteBID).Return(int64(0), nil),
		buildingStore.EXPECT().CascadeDirectDeviceSitesByBuildings(ctx, orgID, []int64{10}, &siteBID).Return(int64(0), nil),
		siteStore.EXPECT().LockBuildingForWrite(ctx, orgID, int64(10)).Return(nil),
		buildingStore.EXPECT().GetBuilding(ctx, orgID, int64(10)).Return(&buildingmodels.Building{ID: 10, SiteID: &siteBID, SiteLabel: "Site B", Name: "Building A", Aisles: 1, RacksPerAisle: 2}, nil),
		buildingStore.EXPECT().CountRacksInBuilding(ctx, orgID, int64(10)).Return(int64(0), nil),
		buildingStore.EXPECT().UpdateBuilding(ctx, buildingmodels.UpdateParams{
			OrgID:         orgID,
			ID:            10,
			Name:          "Building A",
			Aisles:        1,
			RacksPerAisle: 2,
		}).Return(&buildingmodels.Building{ID: 10, SiteID: &siteBID, SiteLabel: "Site B", Name: "Building A", Aisles: 1, RacksPerAisle: 2}, nil),
		siteStore.EXPECT().LockSiteForWrite(ctx, orgID, siteAID).Return(nil),
		siteStore.EXPECT().LockBuildingsBySiteForWrite(ctx, orgID, siteAID).Return(nil),
		siteStore.EXPECT().LockInfrastructureDevicesBySiteForWrite(ctx, orgID, siteAID).Return(nil, nil),
		siteStore.EXPECT().UnassignRacksFromBuildingsBySite(ctx, orgID, siteAID).Return(int64(0), nil),
		buildingStore.EXPECT().ClearDeviceBuildingsBySite(ctx, orgID, siteAID).Return(int64(0), nil),
		siteStore.EXPECT().SoftDeleteBuildingsBySite(ctx, orgID, siteAID).Return(int64(0), nil),
		siteStore.EXPECT().UnassignRacksFromSite(ctx, orgID, siteAID).Return(int64(0), nil),
		siteStore.EXPECT().UnassignDevicesFromSite(ctx, orgID, siteAID).Return(int64(0), nil),
		siteStore.EXPECT().DeleteCurtailmentResponseProfilesBySite(ctx, orgID, siteAID).Return(int64(0), nil),
		siteStore.EXPECT().CountResponseProfilesByInfrastructureDevices(ctx, orgID, []int64(nil)).Return(int64(0), nil),
		siteStore.EXPECT().SoftDeleteInfrastructureDevicesBySite(ctx, orgID, siteAID).Return(int64(0), nil),
		siteStore.EXPECT().SoftDeleteSite(ctx, orgID, siteAID).Return(int64(1), nil),
	)

	resolveReferences(parsed, snap)
	resolved := resolvePlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if err := svc.applyImportPlan(ctx, orgID, resolved, parsed, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED); err != nil {
		t.Fatalf("applyImportPlan error = %v", err)
	}
}

func TestMoveBuildingsToSiteLocksTargetSiteBeforeBuildings(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()
	orgID := int64(42)
	siteStore := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	svc := NewService(siteStore, buildingStore, nil, nil, nil, nil, nil)
	targetSiteID := int64(99)
	buildingIDs := []int64{11, 12}

	gomock.InOrder(
		siteStore.EXPECT().LockSiteForWrite(ctx, orgID, targetSiteID).Return(nil),
		siteStore.EXPECT().LockBuildingForWrite(ctx, orgID, int64(11)).Return(nil),
		siteStore.EXPECT().LockBuildingForWrite(ctx, orgID, int64(12)).Return(nil),
		siteStore.EXPECT().AssignBuildingsToSiteBulk(ctx, orgID, buildingIDs, &targetSiteID).Return(int64(2), nil),
		siteStore.EXPECT().ReassignRacksUnderBuildingsBulk(ctx, orgID, buildingIDs, &targetSiteID).Return(int64(0), nil),
		siteStore.EXPECT().ReassignDevicesUnderBuildingsBulk(ctx, orgID, buildingIDs, &targetSiteID).Return(int64(0), nil),
		buildingStore.EXPECT().CascadeDirectDeviceSitesByBuildings(ctx, orgID, buildingIDs, &targetSiteID).Return(int64(0), nil),
	)

	if err := svc.moveBuildingsToSite(ctx, orgID, buildingIDs, &targetSiteID); err != nil {
		t.Fatalf("moveBuildingsToSite error = %v", err)
	}
}

func TestApplyBuildingRowsLocksParentSiteBeforeCreate(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()
	orgID := int64(42)
	siteID := int64(99)
	siteStore := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	svc := NewService(siteStore, buildingStore, nil, nil, nil, nil, nil)
	node := &resolvedBuilding{
		action:        actionCreate,
		siteRef:       "Site A",
		name:          "Building A",
		aisles:        2,
		racksPerAisle: 3,
	}

	gomock.InOrder(
		siteStore.EXPECT().LockSiteForWrite(ctx, orgID, siteID).Return(nil),
		buildingStore.EXPECT().CreateBuilding(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, params buildingmodels.CreateParams) (*buildingmodels.Building, error) {
			if !nullableInt64Equal(params.SiteID, &siteID) {
				t.Fatalf("site_id = %v, want %d", params.SiteID, siteID)
			}
			return &buildingmodels.Building{ID: 10, Name: params.Name, SiteID: params.SiteID, SiteLabel: "Site A", Aisles: params.Aisles, RacksPerAisle: params.RacksPerAisle}, nil
		}),
	)

	if err := svc.applyBuildings(ctx, orgID, []*resolvedBuilding{node}, map[string]sitemodels.Site{"Site A": {ID: siteID, Name: "Site A"}}, map[string]buildingmodels.Building{}, map[int64]buildingmodels.Building{}); err != nil {
		t.Fatalf("applyBuildings error = %v", err)
	}
}

func TestApplyBuildingRowsMatchesBlankIDBySiteAndName(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()
	orgID := int64(42)
	siteID := int64(99)
	buildingID := int64(10)
	siteStore := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	svc := NewService(siteStore, buildingStore, nil, nil, nil, nil, nil)
	building := buildingmodels.Building{ID: buildingID, Name: "Building A", SiteID: &siteID, SiteLabel: "Site A", Aisles: 2, RacksPerAisle: 3}
	node := &resolvedBuilding{
		action:        actionUpdate,
		siteRef:       "Site A",
		name:          "Building A",
		aisles:        2,
		racksPerAisle: 4,
	}

	gomock.InOrder(
		siteStore.EXPECT().LockBuildingForWrite(ctx, orgID, buildingID).Return(nil),
		buildingStore.EXPECT().GetBuilding(ctx, orgID, buildingID).Return(&building, nil),
		buildingStore.EXPECT().CountRacksInBuilding(ctx, orgID, buildingID).Return(int64(0), nil),
		buildingStore.EXPECT().UpdateBuilding(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, params buildingmodels.UpdateParams) (*buildingmodels.Building, error) {
			if params.ID != buildingID {
				t.Fatalf("building id = %d, want %d", params.ID, buildingID)
			}
			return &buildingmodels.Building{ID: params.ID, Name: params.Name, SiteID: &siteID, SiteLabel: "Site A", Aisles: params.Aisles, RacksPerAisle: params.RacksPerAisle}, nil
		}),
	)

	if err := svc.applyBuildings(
		ctx,
		orgID,
		[]*resolvedBuilding{node},
		map[string]sitemodels.Site{"Site A": {ID: siteID, Name: "Site A"}},
		map[string]buildingmodels.Building{"Site A\x00Building A": building},
		map[int64]buildingmodels.Building{buildingID: building},
	); err != nil {
		t.Fatalf("applyBuildings error = %v", err)
	}
}

func TestApplyBuildingRowsMovesBeforeRename(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()
	orgID := int64(42)
	oldSiteID := int64(1)
	newSiteID := int64(2)
	buildingID := int64(10)
	siteStore := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	svc := NewService(siteStore, buildingStore, nil, nil, nil, nil, nil)
	building := buildingmodels.Building{ID: buildingID, Name: "Old Name", SiteID: &oldSiteID, SiteLabel: "Site A", Aisles: 1, RacksPerAisle: 2}
	node := &resolvedBuilding{
		action:        actionUpdate,
		id:            &buildingID,
		siteRef:       "Site B",
		name:          "Target Name",
		aisles:        1,
		racksPerAisle: 2,
	}
	buildingIDs := []int64{buildingID}

	gomock.InOrder(
		siteStore.EXPECT().LockSiteForWrite(ctx, orgID, newSiteID).Return(nil),
		siteStore.EXPECT().LockBuildingForWrite(ctx, orgID, buildingID).Return(nil),
		siteStore.EXPECT().AssignBuildingsToSiteBulk(ctx, orgID, buildingIDs, &newSiteID).Return(int64(1), nil),
		siteStore.EXPECT().ReassignRacksUnderBuildingsBulk(ctx, orgID, buildingIDs, &newSiteID).Return(int64(0), nil),
		siteStore.EXPECT().ReassignDevicesUnderBuildingsBulk(ctx, orgID, buildingIDs, &newSiteID).Return(int64(0), nil),
		buildingStore.EXPECT().CascadeDirectDeviceSitesByBuildings(ctx, orgID, buildingIDs, &newSiteID).Return(int64(0), nil),
		siteStore.EXPECT().LockBuildingForWrite(ctx, orgID, buildingID).Return(nil),
		buildingStore.EXPECT().GetBuilding(ctx, orgID, buildingID).Return(&buildingmodels.Building{ID: buildingID, Name: "Old Name", Aisles: 1, RacksPerAisle: 2}, nil),
		buildingStore.EXPECT().CountRacksInBuilding(ctx, orgID, buildingID).Return(int64(0), nil),
		buildingStore.EXPECT().UpdateBuilding(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, params buildingmodels.UpdateParams) (*buildingmodels.Building, error) {
			if params.Name != "Target Name" {
				t.Fatalf("name = %q, want Target Name", params.Name)
			}
			return &buildingmodels.Building{ID: params.ID, Name: params.Name, SiteID: &newSiteID, SiteLabel: "Site B", Aisles: params.Aisles, RacksPerAisle: params.RacksPerAisle}, nil
		}),
	)

	if err := svc.applyBuildings(
		ctx,
		orgID,
		[]*resolvedBuilding{node},
		map[string]sitemodels.Site{"Site B": {ID: newSiteID, Name: "Site B"}},
		map[string]buildingmodels.Building{"Site A\x00Old Name": building},
		map[int64]buildingmodels.Building{buildingID: building},
	); err != nil {
		t.Fatalf("applyBuildings error = %v", err)
	}
}

func TestApplyBuildingRowsRechecksLayoutUnderLock(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()
	orgID := int64(42)
	buildingID := int64(10)
	aisle := int32(1)
	position := int32(0)
	siteStore := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	svc := NewService(siteStore, buildingStore, nil, nil, nil, nil, nil)
	building := buildingmodels.Building{ID: buildingID, Name: "Building A", Aisles: 2, RacksPerAisle: 1}
	node := &resolvedBuilding{
		action:        actionUpdate,
		id:            &buildingID,
		name:          "Building A",
		aisles:        1,
		racksPerAisle: 1,
	}

	gomock.InOrder(
		siteStore.EXPECT().LockBuildingForWrite(ctx, orgID, buildingID).Return(nil),
		buildingStore.EXPECT().GetBuilding(ctx, orgID, buildingID).Return(&building, nil),
		buildingStore.EXPECT().ListRacksOutsideBuildingBounds(ctx, orgID, buildingID, int32(1), int32(1)).Return([]buildingmodels.BuildingRack{{
			RackID:          20,
			RackLabel:       "Rack A",
			AisleIndex:      &aisle,
			PositionInAisle: &position,
		}}, nil),
	)

	err := svc.applyBuildings(ctx, orgID, []*resolvedBuilding{node}, nil, map[string]buildingmodels.Building{"\x00Building A": building}, map[int64]buildingmodels.Building{buildingID: building})
	if err == nil || !strings.Contains(err.Error(), "cannot shrink layout") {
		t.Fatalf("applyBuildings error = %v, want shrink-layout rejection", err)
	}
}

func TestApplyRackRowsUsesCurrentLockedBuildingSite(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()
	orgID := int64(42)
	staleSiteID := int64(5)
	currentSiteID := int64(6)
	buildingID := int64(11)
	siteStore := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	svc := NewService(siteStore, buildingStore, collectionStore, nil, nil, nil, nil)
	node := &resolvedRack{
		action:      actionCreate,
		label:       "Rack A",
		buildingRef: "Building A",
		siteRef:     "Old Site",
		rows:        4,
		columns:     6,
		orderIndex:  "BOTTOM_LEFT",
	}

	gomock.InOrder(
		siteStore.EXPECT().LockBuildingForWrite(ctx, orgID, buildingID).Return(nil),
		buildingStore.EXPECT().GetBuildingSiteID(ctx, orgID, buildingID).Return(&currentSiteID, nil),
		collectionStore.EXPECT().CreateCollection(ctx, orgID, collectionpb.CollectionType_COLLECTION_TYPE_RACK, "Rack A", "").Return(&collectionpb.DeviceCollection{Id: 20}, nil),
		collectionStore.EXPECT().CreateRackExtension(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, params interfaces.CreateRackExtensionParams) error {
			if !nullableInt64Equal(params.SiteID, &currentSiteID) {
				t.Fatalf("site_id = %v, want current site %d", params.SiteID, currentSiteID)
			}
			if !nullableInt64Equal(params.BuildingID, &buildingID) {
				t.Fatalf("building_id = %v, want %d", params.BuildingID, buildingID)
			}
			return nil
		}),
		buildingStore.EXPECT().SetRackBuildingPositionBulkClear(ctx, orgID, []int64{20}).Return(nil),
	)

	if err := svc.applyRacks(ctx, orgID, []*resolvedRack{node}, map[string]sitemodels.Site{"Old Site": {ID: staleSiteID, Name: "Old Site"}}, map[string]buildingmodels.Building{"Old Site\x00Building A": {ID: buildingID, Name: "Building A", SiteID: &staleSiteID, SiteLabel: "Old Site"}}, map[int64]buildingmodels.Building{}, map[string]rackSnapshot{}, map[int64]rackSnapshot{}); err != nil {
		t.Fatalf("applyRacks error = %v", err)
	}
}

func TestApplyRackRowsRechecksDimensionsUnderRackLock(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()
	orgID := int64(42)
	siteStore := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	svc := NewService(siteStore, buildingStore, collectionStore, nil, nil, nil, nil)
	rackID := int64(20)
	node := &resolvedRack{
		action:     actionUpdate,
		id:         &rackID,
		label:      "Rack A",
		prevLabel:  "Rack A",
		rows:       1,
		columns:    1,
		orderIndex: "BOTTOM_LEFT",
	}
	rack := rackSnapshot{ID: 20, Label: "Rack A", Rows: 4, Columns: 6, OrderIndex: "BOTTOM_LEFT"}

	gomock.InOrder(
		collectionStore.EXPECT().LockRackPlacementForWrite(ctx, int64(20), orgID).Return(interfaces.RackPlacement{}, nil),
		collectionStore.EXPECT().GetRackSlots(ctx, int64(20), orgID).Return([]*collectionpb.RackSlot{{
			DeviceIdentifier: "miner-1",
			Position:         &collectionpb.RackSlotPosition{Row: 1, Column: 0},
		}}, nil),
	)

	err := svc.applyRacks(ctx, orgID, []*resolvedRack{node}, nil, nil, map[int64]buildingmodels.Building{}, map[string]rackSnapshot{"Rack A": rack}, map[int64]rackSnapshot{20: rack})
	if err == nil || !strings.Contains(err.Error(), "cannot resize rack") {
		t.Fatalf("applyRacks error = %v, want resize rejection", err)
	}
}

func TestApplyMinerRowsUsesCurrentLockedBuildingSiteForDirectPlacement(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()
	orgID := int64(42)
	staleSiteID := int64(5)
	currentSiteID := int64(6)
	buildingID := int64(11)
	siteStore := mocks.NewMockSiteStore(ctrl)
	buildingStore := mocks.NewMockBuildingStore(ctrl)
	collectionStore := mocks.NewMockCollectionStore(ctrl)
	svc := NewService(siteStore, buildingStore, collectionStore, nil, nil, nil, nil)
	deviceIDs := []string{"miner-1"}
	existingMiner := minerSnapshot{DeviceIdentifier: "miner-1", Name: "Miner 1"}
	node := &resolvedMiner{
		deviceID:   "miner-1",
		name:       "Miner 1",
		buildLabel: "Building A",
		siteLabel:  "Old Site",
		moved:      true,
		existing:   &existingMiner,
	}

	gomock.InOrder(
		siteStore.EXPECT().LockBuildingForWrite(ctx, orgID, buildingID).Return(nil),
		buildingStore.EXPECT().GetBuildingSiteID(ctx, orgID, buildingID).Return(&currentSiteID, nil),
		collectionStore.EXPECT().LockRacksForReparent(ctx, orgID, deviceIDs, int64(0)).Return(nil, nil),
		collectionStore.EXPECT().RemoveDevicesFromAnyRack(ctx, orgID, deviceIDs, int64(0)).Return(int64(1), nil),
		siteStore.EXPECT().AssignDevicesToSite(ctx, orgID, &currentSiteID, deviceIDs).Return(int64(1), nil),
		buildingStore.EXPECT().AssignDevicesToBuilding(ctx, orgID, &buildingID, deviceIDs).Return(int64(1), nil),
	)

	if err := svc.applyMiners(ctx, orgID, []*resolvedMiner{node}, map[string]sitemodels.Site{"Old Site": {ID: staleSiteID, Name: "Old Site"}}, map[string]buildingmodels.Building{"Old Site\x00Building A": {ID: buildingID, Name: "Building A", SiteID: &staleSiteID, SiteLabel: "Old Site"}}, map[int64]buildingmodels.Building{}, nil); err != nil {
		t.Fatalf("applyMiners error = %v", err)
	}
}

func TestApplySiteRowsRegeneratesSlugWhenRenamingByID(t *testing.T) {
	ctrl := gomock.NewController(t)
	ctx := context.Background()
	orgID := int64(42)
	siteStore := mocks.NewMockSiteStore(ctrl)
	svc := NewService(siteStore, nil, nil, nil, nil, nil, nil)
	site := sitemodels.Site{ID: 11, Name: "Old Site", Slug: "old-site"}
	existingByName := map[string]sitemodels.Site{site.Name: site}
	existingByID := map[int64]sitemodels.Site{site.ID: site}
	siteID := int64(11)
	node := &resolvedSite{
		action:   actionUpdate,
		id:       &siteID,
		name:     "New Site",
		prevName: "Old Site",
	}

	siteStore.EXPECT().ListSiteSlugs(ctx, orgID).Return([]string{"old-site"}, nil)
	siteStore.EXPECT().UpdateSite(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, params sitemodels.UpdateSiteParams) (*sitemodels.Site, error) {
		if params.Slug != "new-site" {
			t.Fatalf("slug = %q, want regenerated new-site", params.Slug)
		}
		return &sitemodels.Site{ID: params.ID, Name: params.Name, Slug: params.Slug}, nil
	})

	if err := svc.applySites(ctx, orgID, []*resolvedSite{node}, existingByName, existingByID); err != nil {
		t.Fatalf("applySites error = %v", err)
	}
	if _, ok := existingByName["Old Site"]; ok {
		t.Fatal("old site name still present in lookup")
	}
	if got := existingByName["New Site"].Slug; got != "new-site" {
		t.Fatalf("updated lookup slug = %q, want new-site", got)
	}
}

func TestValidateSlotConflictsWithExistingAllowsSlotSwaps(t *testing.T) {
	rows := []map[string]string{
		{"__row": "21", "device_identifier": "miner-1", "rack": "Rack A", "rack_row": "0", "rack_col": "1"},
		{"__row": "22", "device_identifier": "miner-2", "rack": "Rack A", "rack_row": "0", "rack_col": "0"},
	}
	snap := &snapshot{miners: []minerSnapshot{
		{DeviceIdentifier: "miner-1", Rack: "Rack A", RackRow: "0", RackCol: "0"},
		{DeviceIdentifier: "miner-2", Rack: "Rack A", RackRow: "0", RackCol: "1"},
	}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"MINER": rows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if errs := validateSlotConflictsWithExisting(p); len(errs) != 0 {
		t.Fatalf("slot swap should not conflict, got %+v", errs)
	}
}

func TestValidateSlotConflictsWithExistingBlocksUnchangedOccupant(t *testing.T) {
	rows := []map[string]string{
		{"__row": "21", "device_identifier": "miner-1", "rack": "Rack A", "rack_row": "0", "rack_col": "1"},
	}
	snap := &snapshot{miners: []minerSnapshot{
		{DeviceIdentifier: "miner-1", Rack: "Rack A", RackRow: "0", RackCol: "0"},
		{DeviceIdentifier: "miner-2", Rack: "Rack A", RackRow: "0", RackCol: "1"},
	}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"MINER": rows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateSlotConflictsWithExisting(p)
	if len(errs) != 1 {
		t.Fatalf("errors = %+v, want one conflict", errs)
	}
	if errs[0].GetRow() != 21 || errs[0].GetSection() != "MINER" || errs[0].GetMessage() != "rack slot already occupied by miner miner-2" {
		t.Fatalf("unexpected error: %+v", errs[0])
	}
}

func TestValidateSlotConflictsWithExistingBlocksRemoveOmittedVacatedSlot(t *testing.T) {
	rows := []map[string]string{
		{"__row": "21", "device_identifier": "miner-1", "rack": "Rack A", "rack_row": "0", "rack_col": "1"},
	}
	snap := &snapshot{miners: []minerSnapshot{
		{DeviceIdentifier: "miner-1", Rack: "Rack A", RackRow: "0", RackCol: "0"},
		{DeviceIdentifier: "miner-2", Rack: "Rack A", RackRow: "0", RackCol: "1"},
	}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"MINER": rows}}, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	errs := validateSlotConflictsWithExisting(p)
	if len(errs) != 1 || errs[0].GetMessage() != "rack slot already occupied by miner miner-2" {
		t.Fatalf("errors = %+v, want omitted miner slot to remain occupied until apply clears it", errs)
	}
}

func TestValidateSlotConflictsWithExistingBlocksHiddenOccupant(t *testing.T) {
	rows := []map[string]string{
		{"__row": "21", "device_identifier": "miner-1", "rack": "Rack A", "rack_row": "0", "rack_col": "1"},
	}
	snap := &snapshot{
		miners:            []minerSnapshot{{DeviceIdentifier: "miner-1", Rack: "Rack A", RackRow: "0", RackCol: "0"}},
		hiddenRackMembers: []minerSnapshot{{DeviceIdentifier: "hidden-1", Rack: "Rack A", RackRow: "0", RackCol: "1"}},
	}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"MINER": rows}}, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	errs := validateSlotConflictsWithExisting(p)
	if len(errs) != 1 {
		t.Fatalf("errors = %+v, want one conflict", errs)
	}
	if errs[0].GetRow() != 21 || errs[0].GetSection() != "MINER" || errs[0].GetMessage() != "rack slot already occupied by miner hidden-1" {
		t.Fatalf("unexpected error: %+v", errs[0])
	}
}

func TestValidateSlotConflictsWithExistingUsesDesiredRackLabelForHiddenOccupants(t *testing.T) {
	rackID := int64(20)
	rows := []map[string]string{
		{"__row": "21", "device_identifier": "miner-1", "rack": "New Rack", "rack_row": "0", "rack_col": "1"},
	}
	rackRows := []map[string]string{
		{fieldID: "20", fieldLabel: "New Rack"},
	}
	snap := &snapshot{
		miners:            []minerSnapshot{{DeviceIdentifier: "miner-1", RackID: &rackID, Rack: "Old Rack", RackRow: "0", RackCol: "0"}},
		hiddenRackMembers: []minerSnapshot{{DeviceIdentifier: "hidden-1", RackID: &rackID, Rack: "Old Rack", RackRow: "0", RackCol: "1"}},
	}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"MINER": rows, "RACK": rackRows}}, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	errs := validateSlotConflictsWithExisting(p)
	if len(errs) != 1 {
		t.Fatalf("errors = %+v, want one conflict", errs)
	}
	if errs[0].GetRow() != 21 || errs[0].GetSection() != "MINER" || errs[0].GetMessage() != "rack slot already occupied by miner hidden-1" {
		t.Fatalf("unexpected error: %+v", errs[0])
	}
}

func TestValidateSlotCollisionsNormalizesCoordinates(t *testing.T) {
	rows := []map[string]string{
		{"__row": "21", "device_identifier": "miner-1", "rack": "Rack A", "rack_row": "1", "rack_col": "1"},
		{"__row": "22", "device_identifier": "miner-2", "rack": "Rack A", "rack_row": "01", "rack_col": "1"},
	}

	errs := validateSlotCollisions(resolveMiners(rows, nil))
	if len(errs) != 1 || errs[0].GetRow() != 22 || errs[0].GetMessage() != "duplicate rack slot" {
		t.Fatalf("errors = %+v, want normalized duplicate slot", errs)
	}
}

func TestValidateSlotConflictsWithExistingNormalizesCoordinates(t *testing.T) {
	rows := []map[string]string{
		{"__row": "21", "device_identifier": "miner-1", "rack": "Rack A", "rack_row": "01", "rack_col": "1"},
	}
	snap := &snapshot{miners: []minerSnapshot{
		{DeviceIdentifier: "miner-1", Rack: "Rack A", RackRow: "0", RackCol: "0"},
		{DeviceIdentifier: "miner-2", Rack: "Rack A", RackRow: "1", RackCol: "1"},
	}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"MINER": rows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateSlotConflictsWithExisting(p)
	if len(errs) != 1 || errs[0].GetRow() != 21 || errs[0].GetMessage() != "rack slot already occupied by miner miner-2" {
		t.Fatalf("errors = %+v, want normalized existing slot conflict", errs)
	}
}

func TestClassifyBuildingUpdateMatchesExistingRowsByID(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{"BUILDING": {{
		fieldName:         "Building A",
		fieldID:           "20",
		fieldSite:         "Site A",
		"aisles":          "2",
		"racks_per_aisle": "2",
	}}}}
	siteID := int64(10)
	snap := &snapshot{buildings: []buildingmodels.Building{{ID: 20, SiteID: &siteID, SiteLabel: "Site A", Name: "Building A", Aisles: 1, RacksPerAisle: 2}}}

	p := resolvePlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if got := countBuildingUpdateNodes(p.buildings); got != 1 {
		t.Fatalf("countBuildingUpdateNodes = %d, want existing row update counted by id", got)
	}
}

func TestClassifyBuildingUpdateIgnoresBlankIDCreateRows(t *testing.T) {
	// A blank-id row whose canonical (site, name) matches no existing building is a
	// create, not an update.
	parsed := &parsedCSV{sections: map[string][]map[string]string{"BUILDING": {{
		fieldName:         "Building A",
		fieldSite:         "Site B",
		"aisles":          "2",
		"racks_per_aisle": "2",
	}}}}
	siteID := int64(10)
	snap := &snapshot{buildings: []buildingmodels.Building{{ID: 20, SiteID: &siteID, SiteLabel: "Site A", Name: "Building A", Aisles: 1, RacksPerAisle: 2}}}

	p := resolvePlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if got := countBuildingUpdateNodes(p.buildings); got != 0 {
		t.Fatalf("countBuildingUpdateNodes = %d, want blank-id create row to not count as update", got)
	}
	if p.buildings[0].action != actionCreate {
		t.Fatalf("action = %v, want create", p.buildings[0].action)
	}
}

func TestClassifyRackUpdateMatchesExistingRowsByID(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{"RACK": {{
		fieldLabel:    "Rack A",
		fieldID:       "20",
		"rows":        "4",
		"columns":     "8",
		"order_index": "BOTTOM_LEFT",
	}}}}
	snap := &snapshot{racks: []rackSnapshot{{ID: 20, Label: "Rack A", Rows: 4, Columns: 6, OrderIndex: "BOTTOM_LEFT"}}}

	p := resolvePlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if got := countRackUpdateNodes(p.racks); got != 1 {
		t.Fatalf("countRackUpdateNodes = %d, want existing row update counted by id", got)
	}
}

func TestClassifyRackUpdateTreatsCanonicalRowAsNoOp(t *testing.T) {
	siteID := int64(1)
	buildingID := int64(10)
	racks := []rackSnapshot{{
		ID:              20,
		SiteID:          &siteID,
		BuildingID:      &buildingID,
		Site:            "Site A",
		Building:        "Building A",
		Label:           "Rack A",
		Zone:            "Zone 1",
		Rows:            4,
		Columns:         6,
		OrderIndex:      "BOTTOM_LEFT",
		AisleIndex:      "0",
		PositionInAisle: "1",
	}}
	// The canonical row is what an exported rack row normalizes to; it must be a
	// no-op against change detection.
	canonical := rackComparableRow(racks[0])

	parsed := &parsedCSV{sections: map[string][]map[string]string{"RACK": {canonical}}}
	p := resolvePlan(parsed, &snapshot{racks: racks}, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if got := countRackUpdateNodes(p.racks); got != 0 {
		t.Fatalf("countRackUpdateNodes = %d, want canonical rack row to be a no-op", got)
	}
}

func TestMinerRowsUseRackIDAndBlankNamesForRackedMiners(t *testing.T) {
	rackID := int64(30)
	siteID := int64(10)
	buildingID := int64(20)
	rows := minerRows([]minerSnapshot{{
		DeviceIdentifier: "miner-1",
		SerialNumber:     "SN1",
		Name:             "Miner 1",
		IPAddress:        "10.0.0.5",
		MACAddress:       "aa:bb:cc:dd:ee:ff",
		SiteID:           &siteID,
		Site:             "Site A",
		BuildingID:       &buildingID,
		Building:         "Building A",
		RackID:           &rackID,
		Rack:             "Rack A",
		RackRow:          "0",
		RackCol:          "0",
	}})

	if got := rows[0][7]; got != "30" {
		t.Fatalf("exported miner rack ref = %q, want 30", got)
	}
	for _, idx := range []int{5, 6} {
		if got := rows[0][idx]; got != "" {
			t.Fatalf("exported miner col %d = %q, want blank when rack is set", idx, got)
		}
	}
}

func TestMinerRowsUseBuildingIDForDirectBuildingAssignment(t *testing.T) {
	siteID := int64(10)
	buildingID := int64(20)
	rows := minerRows([]minerSnapshot{{
		DeviceIdentifier: "miner-1",
		SiteID:           &siteID,
		Site:             "Site A",
		BuildingID:       &buildingID,
		Building:         "Building A",
	}})

	if got := rows[0][6]; got != "20" {
		t.Fatalf("exported miner building ref = %q, want 20", got)
	}
	if got := rows[0][5]; got != "" {
		t.Fatalf("exported miner site ref = %q, want blank when building is set", got)
	}
}

func TestMinerRowsUseSiteIDForDirectSiteAssignment(t *testing.T) {
	siteID := int64(10)
	rows := minerRows([]minerSnapshot{{
		DeviceIdentifier: "miner-1",
		SiteID:           &siteID,
		Site:             "Site A",
	}})

	if got := rows[0][5]; got != "10" {
		t.Fatalf("exported miner site_id = %q, want 10", got)
	}
}

func TestDisplayHeadersMarkReadOnlyIdentityColumns(t *testing.T) {
	if got := strings.Join(displayHeaders("SITE", siteHeaders), ","); got != "name,id (read only)" {
		t.Fatalf("SITE headers = %q", got)
	}
	if got := strings.Join(displayHeaders("BUILDING", buildingHeaders), ","); got != "name,id (read only),site,aisles,racks_per_aisle" {
		t.Fatalf("BUILDING headers = %q", got)
	}
	if got := strings.Join(displayHeaders("RACK", rackHeaders), ","); got != "label,id (read only),building,site,zone,rows,columns,order_index,aisle_index,position_in_aisle" {
		t.Fatalf("RACK headers = %q", got)
	}
	if got := strings.Join(displayHeaders("MINER", minerHeaders), ","); got != "device_identifier (read only),serial_number (read only),name,ip_address (read only),mac_address (read only),site,building,rack,rack_row,rack_col" {
		t.Fatalf("MINER headers = %q", got)
	}
}

func TestRackExportRowsUseBuildingIDAndBlankNames(t *testing.T) {
	siteID := int64(1)
	buildingID := int64(10)
	rows := rackExportRows(
		[]rackSnapshot{{SiteID: &siteID, BuildingID: &buildingID, Site: "Site A", Building: "Building A", Label: "Rack A"}},
	)

	if got := rows[0][2]; got != "10" {
		t.Fatalf("exported rack building ref = %q, want 10", got)
	}
	if got := rows[0][3]; got != "" {
		t.Fatalf("exported rack site ref = %q, want blank when building is set", got)
	}
}

func TestRackExportRowsUseSiteIDForDirectSiteAssignment(t *testing.T) {
	siteID := int64(1)
	rows := rackExportRows(
		[]rackSnapshot{{SiteID: &siteID, Site: "Site A", Label: "Rack A"}},
	)

	if got := rows[0][3]; got != "1" {
		t.Fatalf("exported rack site ref = %q, want 1", got)
	}
	if got := rows[0][2]; got != "" {
		t.Fatalf("exported rack building ref = %q, want blank for direct site assignment", got)
	}
}

func TestDesiredMinerSiteBuildingResolvesDirectBuildingSite(t *testing.T) {
	minerRows := []map[string]string{{"__row": "21", "device_identifier": "miner-1", "building": "10"}}
	snap := &snapshot{
		sites:     []sitemodels.Site{{ID: 1, Name: "Site A"}},
		buildings: []buildingmodels.Building{{ID: 10, SiteLabel: "Site A", Name: "Building A"}},
		miners:    []minerSnapshot{{DeviceIdentifier: "miner-1"}},
	}

	parsed := &parsedCSV{sections: map[string][]map[string]string{"MINER": minerRows}}
	if errs := resolveReferences(parsed, snap); len(errs) != 0 {
		t.Fatalf("resolveReferences errors = %+v", errs)
	}
	p := resolvePlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	site, building := minerDesiredSiteBuilding(p.miners[0], p.topology)
	if site != "Site A" || building != "Building A" {
		t.Fatalf("placement = (%q, %q), want (Site A, Building A)", site, building)
	}
}

func TestValidatePlacementConsistencyHonorsSiteForDuplicateBuildingNames(t *testing.T) {
	rows := []map[string]string{{
		"__row":    "21",
		"site":     "Site A",
		"building": "Building A",
	}}
	snap := &snapshot{
		sites: []sitemodels.Site{{Name: "Site A"}, {Name: "Site B"}},
		buildings: []buildingmodels.Building{
			{SiteLabel: "Site A", Name: "Building A"},
			{SiteLabel: "Site B", Name: "Building A"},
		},
	}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"MINER": rows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if errs := validatePlacementConsistency(p.miners, p.topology); len(errs) != 0 {
		t.Fatalf("site-qualified duplicate building should validate, got %+v", errs)
	}
}

func TestValidateReadOnlyMinerFieldsIncludesIP(t *testing.T) {
	rows := []map[string]string{{
		"__row":             "21",
		"device_identifier": "miner-1",
		"serial_number":     "SN1",
		"name":              "Miner 1",
		"ip_address":        "10.0.0.99",
		"mac_address":       "aa:bb:cc:dd:ee:ff",
	}}
	snap := &snapshot{miners: []minerSnapshot{{
		DeviceIdentifier: "miner-1",
		SerialNumber:     "SN1",
		Name:             "Miner 1",
		IPAddress:        "10.0.0.5",
		MACAddress:       "aa:bb:cc:dd:ee:ff",
	}}}

	errs := validateReadOnlyMinerFields(resolveMiners(rows, minerMap(snap.miners)))
	if len(errs) != 1 || errs[0].GetMessage() != "ip_address is read-only for existing miner miner-1" {
		t.Fatalf("errors = %+v, want ip_address read-only error", errs)
	}
}

func TestValidateReadOnlyMinerFieldsAllowsName(t *testing.T) {
	rows := []map[string]string{{
		"__row":             "21",
		"device_identifier": "miner-1",
		"serial_number":     "SN1",
		"name":              "Renamed Miner",
		"ip_address":        "10.0.0.5",
		"mac_address":       "aa:bb:cc:dd:ee:ff",
	}}
	snap := &snapshot{miners: []minerSnapshot{{
		DeviceIdentifier: "miner-1",
		SerialNumber:     "SN1",
		Name:             "Miner 1",
		IPAddress:        "10.0.0.5",
		MACAddress:       "aa:bb:cc:dd:ee:ff",
	}}}

	errs := validateReadOnlyMinerFields(resolveMiners(rows, minerMap(snap.miners)))
	if len(errs) != 0 {
		t.Fatalf("errors = %+v, want name changes allowed", errs)
	}
}

func TestValidateRackPlacementTargetsRejectsUnknownSiteBuilding(t *testing.T) {
	rows := []map[string]string{{
		"__row":    "9",
		"site":     "Typo Site",
		"building": "Typo Building",
		"rack":     "Rack A",
	}}
	snap := &snapshot{
		sites:     []sitemodels.Site{{Name: "Site A"}},
		buildings: []buildingmodels.Building{{SiteLabel: "Site A", Name: "Building A"}},
	}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"RACK": rows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateRackPlacementTargets(p.racks, p.topology)
	if len(errs) != 2 {
		t.Fatalf("errors = %+v, want site and building target errors", errs)
	}
}

func TestValidateBuildingSiteTargetsUsesExistingAndCsvSites(t *testing.T) {
	rows := []map[string]string{
		{"__row": "5", "site": "Site A", "building": "Building A"},
		{"__row": "6", "site": "New Site", "building": "Building B"},
		{"__row": "7", "site": "", "building": "Unassigned Building"},
		{"__row": "8", "site": "Typo Site", "building": "Building C"},
	}
	siteRows := []map[string]string{{"site": "New Site"}}
	snap := &snapshot{sites: []sitemodels.Site{{Name: "Site A"}}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"SITE": siteRows, "BUILDING": rows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateBuildingSiteTargets(p.buildings, p.topology)
	if len(errs) != 1 || errs[0].GetRow() != 8 || errs[0].GetMessage() != `unknown site "Typo Site"` {
		t.Fatalf("errors = %+v, want only typo site rejected", errs)
	}
}

func TestValidatePlacementConsistencyRejectsUnknownDirectSite(t *testing.T) {
	rows := []map[string]string{{
		"__row": "21",
		"site":  "Typo Site",
	}}
	snap := &snapshot{sites: []sitemodels.Site{{Name: "Site A"}}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"MINER": rows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validatePlacementConsistency(p.miners, p.topology)
	if len(errs) != 1 || errs[0].GetMessage() != `unknown site "Typo Site"` {
		t.Fatalf("errors = %+v, want unknown site error", errs)
	}
}

func TestValidateRackCapacityBlocksOverfilledRack(t *testing.T) {
	minerRows := []map[string]string{
		{"device_identifier": "miner-1", "rack": "Rack A"},
		{"device_identifier": "miner-2", "rack": "Rack A"},
	}
	rackRows := []map[string]string{{"rack": "Rack A", "rows": "1", "columns": "1"}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"MINER": minerRows, "RACK": rackRows}}, &snapshot{}, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateRackCapacity(p)
	if len(errs) != 1 || errs[0].GetSection() != "MINER" {
		t.Fatalf("errors = %+v, want rack capacity error", errs)
	}
}

func TestValidateRackCapacityCountsRetainedOmittedMiners(t *testing.T) {
	minerRows := []map[string]string{{"device_identifier": "miner-2", "rack": "Rack A"}}
	rackRows := []map[string]string{{"rack": "Rack A", "rows": "1", "columns": "1"}}
	snap := &snapshot{miners: []minerSnapshot{{DeviceIdentifier: "miner-1", Rack: "Rack A"}}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"MINER": minerRows, "RACK": rackRows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateRackCapacity(p)
	if len(errs) != 1 || errs[0].GetSection() != "MINER" {
		t.Fatalf("errors = %+v, want rack capacity error", errs)
	}
}

func TestValidateRackCapacityCountsHiddenRackMembers(t *testing.T) {
	minerRows := []map[string]string{{"device_identifier": "miner-1", "rack": "Rack A"}}
	rackRows := []map[string]string{{"rack": "Rack A", "rows": "1", "columns": "1"}}
	snap := &snapshot{hiddenRackMembers: []minerSnapshot{{DeviceIdentifier: "hidden-1", Rack: "Rack A"}}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"MINER": minerRows, "RACK": rackRows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateRackCapacity(p)
	if len(errs) != 1 || errs[0].GetSection() != "MINER" {
		t.Fatalf("errors = %+v, want rack capacity error", errs)
	}
}

func TestValidateRackCapacityMapsHiddenMembersThroughRackRename(t *testing.T) {
	rackID := int64(20)
	minerRows := []map[string]string{{"device_identifier": "miner-1", "rack": "New Rack"}}
	rackRows := []map[string]string{{fieldID: "20", fieldLabel: "New Rack", "rows": "1", "columns": "1"}}
	snap := &snapshot{hiddenRackMembers: []minerSnapshot{{DeviceIdentifier: "hidden-1", RackID: &rackID, Rack: "Old Rack"}}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"MINER": minerRows, "RACK": rackRows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateRackCapacity(p)
	if len(errs) != 1 || errs[0].GetSection() != "MINER" || !strings.Contains(errs[0].GetMessage(), `rack "New Rack"`) {
		t.Fatalf("errors = %+v, want renamed-rack capacity error", errs)
	}
}

func TestValidateRackSlotBoundsRejectsPartialCoordinates(t *testing.T) {
	minerRows := []map[string]string{{"__row": "21", "device_identifier": "miner-1", "rack": "Rack A", "rack_row": "", "rack_col": "3"}}
	rackRows := []map[string]string{{"rack": "Rack A", "rows": "4", "columns": "6"}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"MINER": minerRows, "RACK": rackRows}}, &snapshot{}, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateRackSlotBounds(p.miners, p.topology)
	if len(errs) != 1 || errs[0].GetMessage() != "rack_row and rack_col must both be set or both be blank" {
		t.Fatalf("errors = %+v, want partial coordinate error", errs)
	}
}

func TestValidateRackDimensionsBlocksOutOfRange(t *testing.T) {
	rows := []map[string]string{{"__row": "7", "rack": "Rack A", "rows": "13", "columns": "0", "order_index": "BOTTOM_LEFT"}}

	errs := validateRackDimensions(rows)
	if len(errs) != 2 {
		t.Fatalf("errors = %+v, want row and column dimension errors", errs)
	}
}

func TestValidateRackGridPositionsBlocksOutOfBounds(t *testing.T) {
	rackRows := []map[string]string{{
		"__row":             "10",
		"site":              "Site A",
		"building":          "Building A",
		"rack":              "Rack A",
		"aisle_index":       "2",
		"position_in_aisle": "0",
	}}
	buildingRows := []map[string]string{{"site": "Site A", "building": "Building A", "aisles": "2", "racks_per_aisle": "6"}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"RACK": rackRows, "BUILDING": buildingRows}}, &snapshot{}, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateRackGridPositions(p.racks, p.topology)
	if len(errs) != 1 || !strings.Contains(errs[0].GetMessage(), "aisle_index 2 is out of bounds") {
		t.Fatalf("errors = %+v, want aisle bounds error", errs)
	}
}

func TestValidateRackGridCollisionsRejectsDuplicateCsvCells(t *testing.T) {
	rackRows := []map[string]string{
		{"__row": "10", "site": "Site A", "building": "Building A", "rack": "Rack A", "aisle_index": "0", "position_in_aisle": "0"},
		{"__row": "11", "site": "Site A", "building": "Building A", "rack": "Rack B", "aisle_index": "0", "position_in_aisle": "0"},
	}

	errs := validateRackGridCollisions(rackRows, &snapshot{}, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(errs) != 1 || errs[0].GetRow() != 11 || errs[0].GetMessage() != "rack grid cell already occupied by rack Rack A" {
		t.Fatalf("errors = %+v, want duplicate grid cell", errs)
	}
}

func TestValidateRackGridCollisionsCountsRetainedOmittedRacks(t *testing.T) {
	rackRows := []map[string]string{
		{"__row": "10", "site": "Site A", "building": "Building A", "rack": "Rack B", "aisle_index": "0", "position_in_aisle": "0"},
	}
	snap := &snapshot{racks: []rackSnapshot{{
		Site:            "Site A",
		Building:        "Building A",
		Label:           "Rack A",
		AisleIndex:      "0",
		PositionInAisle: "0",
	}}}

	errs := validateRackGridCollisions(rackRows, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(errs) != 1 || errs[0].GetMessage() != "rack grid cell already occupied by rack Rack A" {
		t.Fatalf("errors = %+v, want retained rack duplicate grid cell", errs)
	}
}

func TestValidateRackGridCollisionsResolvesNameOnlyBuildingID(t *testing.T) {
	buildingID := int64(10)
	rackRows := []map[string]string{
		{"__row": "10", fieldSite: "Site A", fieldBuilding: "Building A", fieldLabel: "Rack B", "aisle_index": "0", "position_in_aisle": "0"},
	}
	snap := &snapshot{
		buildings: []buildingmodels.Building{{ID: buildingID, SiteLabel: "Site A", Name: "Building A"}},
		racks: []rackSnapshot{{
			BuildingID:      &buildingID,
			Site:            "Site A",
			Building:        "Building A",
			Label:           "Rack A",
			AisleIndex:      "0",
			PositionInAisle: "0",
		}},
	}

	errs := validateRackGridCollisions(rackRows, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if len(errs) != 1 || errs[0].GetMessage() != "rack grid cell already occupied by rack Rack A" {
		t.Fatalf("errors = %+v, want name-only building row to collide with building_id occupant", errs)
	}
}

func TestValidateRackGridCollisionsBlocksRemoveOmittedRackCells(t *testing.T) {
	rackRows := []map[string]string{
		{"__row": "10", "site": "Site A", "building": "Building A", "rack": "Rack B", "aisle_index": "0", "position_in_aisle": "0"},
	}
	snap := &snapshot{racks: []rackSnapshot{{
		Site:            "Site A",
		Building:        "Building A",
		Label:           "Rack A",
		AisleIndex:      "0",
		PositionInAisle: "0",
	}}}

	errs := validateRackGridCollisions(rackRows, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if len(errs) != 1 || errs[0].GetMessage() != "rack grid cell already occupied by rack Rack A" {
		t.Fatalf("errors = %+v, want omitted rack grid cell to remain occupied until apply clears it", errs)
	}
}

func TestValidateRackGridCollisionsAllowsIDRenameSameCell(t *testing.T) {
	rackRows := []map[string]string{
		{"__row": "10", fieldID: "20", fieldSite: "Site A", fieldBuilding: "Building A", fieldLabel: "Renamed Rack", "aisle_index": "0", "position_in_aisle": "0"},
	}
	snap := &snapshot{racks: []rackSnapshot{{
		ID:              20,
		Site:            "Site A",
		Building:        "Building A",
		Label:           "Rack A",
		AisleIndex:      "0",
		PositionInAisle: "0",
	}}}

	errs := validateRackGridCollisions(rackRows, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(errs) != 0 {
		t.Fatalf("errors = %+v, want ID-based rack rename to keep its grid cell", errs)
	}
}

func TestValidateExistingSlotsFitRackDimensionsBlocksShrink(t *testing.T) {
	rackRows := []map[string]string{{"rack": "Rack A", "rows": "1", "columns": "1"}}
	snap := &snapshot{
		racks:  []rackSnapshot{{Label: "Rack A", Rows: 4, Columns: 6}},
		miners: []minerSnapshot{{DeviceIdentifier: "miner-1", Rack: "Rack A", RackRow: "1", RackCol: "0"}},
	}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"RACK": rackRows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateExistingSlotsFitRackDimensions(p)
	if len(errs) != 1 || !strings.Contains(errs[0].GetMessage(), "does not fit rack") {
		t.Fatalf("errors = %+v, want slot fit error", errs)
	}
}

func TestValidateExistingSlotsFitRackDimensionsCountsHiddenRackMembers(t *testing.T) {
	rackRows := []map[string]string{{"rack": "Rack A", "rows": "1", "columns": "1"}}
	snap := &snapshot{hiddenRackMembers: []minerSnapshot{{DeviceIdentifier: "hidden-1", Rack: "Rack A", RackRow: "1", RackCol: "0"}}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"RACK": rackRows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateExistingSlotsFitRackDimensions(p)
	if len(errs) != 1 || errs[0].GetSection() != "MINER" {
		t.Fatalf("errors = %+v, want hidden member slot dimension error", errs)
	}
}

func TestValidateExistingSlotsFitRackDimensionsMapsRetainedMembersThroughRackRename(t *testing.T) {
	rackID := int64(20)
	rackRows := []map[string]string{{fieldID: "20", fieldLabel: "New Rack", "rows": "1", "columns": "1"}}
	snap := &snapshot{
		racks:  []rackSnapshot{{ID: 20, Label: "Old Rack", Rows: 4, Columns: 6}},
		miners: []minerSnapshot{{DeviceIdentifier: "miner-1", RackID: &rackID, Rack: "Old Rack", RackRow: "1", RackCol: "0"}},
	}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"RACK": rackRows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateExistingSlotsFitRackDimensions(p)
	if len(errs) != 1 || errs[0].GetSection() != "MINER" || !strings.Contains(errs[0].GetMessage(), `rack "New Rack"`) {
		t.Fatalf("errors = %+v, want renamed-rack slot fit error", errs)
	}
}

func TestValidateExistingSlotsFitRackDimensionsMapsHiddenMembersThroughRackRename(t *testing.T) {
	rackID := int64(20)
	rackRows := []map[string]string{{fieldID: "20", fieldLabel: "New Rack", "rows": "1", "columns": "1"}}
	snap := &snapshot{
		racks:             []rackSnapshot{{ID: 20, Label: "Old Rack", Rows: 4, Columns: 6}},
		hiddenRackMembers: []minerSnapshot{{DeviceIdentifier: "hidden-1", RackID: &rackID, Rack: "Old Rack", RackRow: "1", RackCol: "0"}},
	}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"RACK": rackRows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateExistingSlotsFitRackDimensions(p)
	if len(errs) != 1 || errs[0].GetSection() != "MINER" || !strings.Contains(errs[0].GetMessage(), `rack "New Rack"`) {
		t.Fatalf("errors = %+v, want hidden renamed-rack slot fit error", errs)
	}
}

func TestValidateBuildingRackCapacityBlocksOverfilledBuilding(t *testing.T) {
	rackRows := []map[string]string{
		{"site": "Site A", "building": "Building A", "rack": "Rack A"},
		{"site": "Site A", "building": "Building A", "rack": "Rack B"},
	}
	buildingRows := []map[string]string{{"site": "Site A", "building": "Building A", "aisles": "1", "racks_per_aisle": "1"}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"RACK": rackRows, "BUILDING": buildingRows}}, &snapshot{}, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateBuildingRackCapacity(p)
	if len(errs) != 1 || errs[0].GetSection() != "RACK" {
		t.Fatalf("errors = %+v, want building rack capacity error", errs)
	}
}

func TestValidateBuildingRackCapacityCountsSiteLessBuildings(t *testing.T) {
	rackRows := []map[string]string{
		{"site": "", "building": "Building A", "rack": "Rack A"},
		{"site": "", "building": "Building A", "rack": "Rack B"},
	}
	buildingRows := []map[string]string{{"site": "", "building": "Building A", "aisles": "1", "racks_per_aisle": "1"}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"RACK": rackRows, "BUILDING": buildingRows}}, &snapshot{}, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateBuildingRackCapacity(p)
	if len(errs) != 1 || errs[0].GetSection() != "RACK" {
		t.Fatalf("errors = %+v, want site-less building rack capacity error", errs)
	}
}

func TestValidateBuildingRackCapacityResolvesNameOnlyRackBuildingIDs(t *testing.T) {
	buildingID := int64(10)
	rackRows := []map[string]string{
		{fieldSite: "Site A", fieldBuilding: "Building A", fieldLabel: "Rack A"},
		{fieldSite: "Site A", fieldBuilding: "Building A", fieldLabel: "Rack B"},
	}
	buildingRows := []map[string]string{
		{fieldID: "10", fieldSite: "Site A", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "1"},
	}
	snap := &snapshot{
		buildings: []buildingmodels.Building{
			{ID: buildingID, SiteLabel: "Site A", Name: "Building A", Aisles: 1, RacksPerAisle: 1},
		},
		racks: []rackSnapshot{
			{ID: 20, Label: "Rack A", Site: "Site A", BuildingID: &buildingID, Building: "Building A"},
		},
	}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"RACK": rackRows, "BUILDING": buildingRows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateBuildingRackCapacity(p)
	if len(errs) != 1 || errs[0].GetSection() != "RACK" {
		t.Fatalf("errors = %+v, want name-only rack rows counted against building_id capacity", errs)
	}
}

func TestValidateBuildingRackCapacityResolvesRenamedBuildingIDs(t *testing.T) {
	buildingID := int64(10)
	rackRows := []map[string]string{
		{fieldSite: "New Site", fieldBuilding: "New Building", fieldLabel: "Rack A"},
		{fieldSite: "New Site", fieldBuilding: "New Building", fieldLabel: "Rack B"},
	}
	buildingRows := []map[string]string{
		{fieldID: "10", fieldSite: "New Site", fieldName: "New Building", "aisles": "1", "racks_per_aisle": "1"},
	}
	snap := &snapshot{
		buildings: []buildingmodels.Building{
			{ID: buildingID, SiteLabel: "Old Site", Name: "Old Building", Aisles: 1, RacksPerAisle: 1},
		},
		racks: []rackSnapshot{
			{ID: 20, Label: "Rack A", Site: "Old Site", BuildingID: &buildingID, Building: "Old Building"},
		},
	}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"RACK": rackRows, "BUILDING": buildingRows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateBuildingRackCapacity(p)
	if len(errs) != 1 || errs[0].GetSection() != "RACK" {
		t.Fatalf("errors = %+v, want renamed building name references counted against building_id capacity", errs)
	}
}

func TestValidateBuildingExistingRacksFitLayoutCountsSiteLessBuildings(t *testing.T) {
	buildingRows := []map[string]string{{"site": "", "building": "Building A", "aisles": "1", "racks_per_aisle": "1"}}
	snap := &snapshot{racks: []rackSnapshot{{
		Site:            "",
		Building:        "Building A",
		Label:           "Rack A",
		AisleIndex:      "2",
		PositionInAisle: "0",
	}}}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"BUILDING": buildingRows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateBuildingExistingRacksFitLayout(p)
	if len(errs) != 1 || !strings.Contains(errs[0].GetMessage(), "does not fit building") {
		t.Fatalf("errors = %+v, want site-less building layout fit error", errs)
	}
}

func TestValidateBuildingExistingRacksFitLayoutUsesBuildingIDForDuplicateBuildings(t *testing.T) {
	buildingAID := int64(10)
	buildingRows := []map[string]string{
		{fieldID: "10", fieldName: "Building A", "aisles": "2", "racks_per_aisle": "1"},
		{fieldID: "11", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "1"},
	}
	snap := &snapshot{
		buildings: []buildingmodels.Building{
			{ID: 10, Name: "Building A", Aisles: 2, RacksPerAisle: 1},
			{ID: 11, Name: "Building A", Aisles: 1, RacksPerAisle: 1},
		},
		racks: []rackSnapshot{{
			ID:              20,
			BuildingID:      &buildingAID,
			Building:        "Building A",
			Label:           "Rack A",
			AisleIndex:      "1",
			PositionInAisle: "0",
		}},
	}

	p := resolvePlan(&parsedCSV{sections: map[string][]map[string]string{"BUILDING": buildingRows}}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateBuildingExistingRacksFitLayout(p)
	if len(errs) != 0 {
		t.Fatalf("errors = %+v, want building_id-specific layout accepted", errs)
	}
}

func TestBuildPlanCountsMinerMoveDisambiguatedByBuildingID(t *testing.T) {
	buildingAID := int64(10)
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": nil,
		"BUILDING": {
			{"__row": "5", fieldID: "10", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "2"},
			{"__row": "6", fieldID: "11", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "2"},
		},
		"RACK": nil,
		"MINER": {
			{"__row": "13", "device_identifier": "miner-1", "serial_number": "SN1", fieldName: "Miner 1", "ip_address": "10.0.0.5", "mac_address": "aa:bb:cc:dd:ee:ff", fieldBuilding: "11"},
		},
	}}
	snap := &snapshot{
		buildings: []buildingmodels.Building{
			{ID: 10, Name: "Building A", Aisles: 1, RacksPerAisle: 2},
			{ID: 11, Name: "Building A", Aisles: 1, RacksPerAisle: 2},
		},
		miners: []minerSnapshot{{DeviceIdentifier: "miner-1", SerialNumber: "SN1", Name: "Miner 1", IPAddress: "10.0.0.5", MACAddress: "aa:bb:cc:dd:ee:ff", BuildingID: &buildingAID, Building: "Building A"}},
	}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %+v, want building_id-disambiguated miner move accepted", plan.errors)
	}
	if !hasChange(plan.changes, pb.ImportOperation_IMPORT_OPERATION_MOVE, "miner", 1) {
		t.Fatalf("changes = %+v, want miner move summary", plan.changes)
	}
}

func TestValidateRackGridPositionsUsesBuildingIDForDuplicateBuildingNames(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"BUILDING": {
			{fieldID: "11", fieldName: "Building A", "aisles": "2", "racks_per_aisle": "1"},
			{fieldID: "10", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK": {
			{fieldBuilding: "11", fieldLabel: "Rack A", "aisle_index": "1", "position_in_aisle": "0"},
		},
	}}
	snap := &snapshot{buildings: []buildingmodels.Building{
		{ID: 10, Name: "Building A", Aisles: 1, RacksPerAisle: 1},
		{ID: 11, Name: "Building A", Aisles: 2, RacksPerAisle: 1},
	}}

	resolveReferences(parsed, snap)
	p := resolvePlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateRackGridPositions(p.racks, p.topology)
	if len(errs) != 0 {
		t.Fatalf("errors = %+v, want building_id-specific bounds", errs)
	}
}

func TestValidateRackGridCollisionsSeparatesIDQualifiedDuplicateBuildings(t *testing.T) {
	buildingAID := int64(10)
	buildingBID := int64(11)
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"BUILDING": {
			{fieldID: "10", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "1"},
			{fieldID: "11", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK": {
			{"__row": "10", fieldBuilding: "10", fieldLabel: "Rack A", "aisle_index": "0", "position_in_aisle": "0"},
			{"__row": "11", fieldBuilding: "11", fieldLabel: "Rack B", "aisle_index": "0", "position_in_aisle": "0"},
		},
	}}
	snap := &snapshot{
		buildings: []buildingmodels.Building{
			{ID: 10, Name: "Building A", Aisles: 1, RacksPerAisle: 1},
			{ID: 11, Name: "Building A", Aisles: 1, RacksPerAisle: 1},
		},
		racks: []rackSnapshot{
			{ID: 20, Label: "Rack A", BuildingID: &buildingAID, Building: "Building A", AisleIndex: "0", PositionInAisle: "0"},
			{ID: 21, Label: "Rack B", BuildingID: &buildingBID, Building: "Building A", AisleIndex: "0", PositionInAisle: "0"},
		},
	}

	resolveReferences(parsed, snap)
	errs := validateRackGridCollisions(parsed.sections["RACK"], snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(errs) != 0 {
		t.Fatalf("errors = %+v, want ID-qualified duplicate buildings to have independent grids", errs)
	}
}

func TestValidateBuildingRackCapacitySeparatesIDQualifiedDuplicateBuildings(t *testing.T) {
	siteID := int64(5)
	buildingAID := int64(10)
	buildingBID := int64(11)
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"BUILDING": {
			{fieldID: "10", fieldSite: "5", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "1"},
			{fieldID: "11", fieldSite: "5", fieldName: "Building A", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK": {
			{fieldBuilding: "10", fieldLabel: "Rack A"},
			{fieldBuilding: "11", fieldLabel: "Rack B"},
		},
	}}
	snap := &snapshot{
		sites: []sitemodels.Site{{ID: 5, Name: "Site A"}},
		buildings: []buildingmodels.Building{
			{ID: 10, SiteID: &siteID, SiteLabel: "Site A", Name: "Building A", Aisles: 1, RacksPerAisle: 1},
			{ID: 11, SiteID: &siteID, SiteLabel: "Site A", Name: "Building A", Aisles: 1, RacksPerAisle: 1},
		},
		racks: []rackSnapshot{
			{ID: 20, Label: "Rack A", SiteID: &siteID, Site: "Site A", BuildingID: &buildingAID, Building: "Building A"},
			{ID: 21, Label: "Rack B", SiteID: &siteID, Site: "Site A", BuildingID: &buildingBID, Building: "Building A"},
		},
	}

	resolveReferences(parsed, snap)
	p := resolvePlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	errs := validateBuildingRackCapacity(p)
	if len(errs) != 0 {
		t.Fatalf("errors = %+v, want ID-qualified duplicate buildings counted separately", errs)
	}
}

func TestParseSiteMapCSVAcceptsSpreadsheetPaddedSectionRows(t *testing.T) {
	csv := validCSV()
	csv = strings.Replace(csv, "# SECTION: SITE\n", "# SECTION: SITE,,,,,,,,,,\n", 1)
	csv = strings.Replace(csv, "\n\n# SECTION: BUILDING\n", "\n,,,,,,,,,,\n# SECTION: BUILDING,,,,,,,,,,\n", 1)
	csv = strings.Replace(csv, "\n\n# SECTION: RACK\n", "\n,,,,,,,,,,\n# SECTION: RACK,,,,,,,,,,\n", 1)
	csv = strings.Replace(csv, "\n\n# SECTION: MINER\n", "\n,,,,,,,,,,\n# SECTION: MINER,,,,,,,,,,\n", 1)
	csv = strings.Replace(csv, "name,id (read only)\n", "name,id (read only),\n", 1)
	csv = strings.Replace(csv, "Site A,\n", "Site A,,\n", 1)
	csv = strings.Replace(csv, "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,NAME:Rack A,0,0\n", "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,NAME:Rack A,0,,\n", 1)

	parsed, errs := parseSiteMapCSV([]byte(csv))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}
	if got := len(parsed.sections["SITE"]); got != 1 {
		t.Fatalf("SITE rows = %d, want 1", got)
	}
	if got := len(parsed.sections["MINER"]); got != 1 {
		t.Fatalf("MINER rows = %d, want 1", got)
	}
	if got := parsed.sections["MINER"][0]["rack_col"]; got != "" {
		t.Fatalf("MINER rack_col = %q, want blank", got)
	}
}

func readZipFiles(t *testing.T, data []byte) map[string]string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader error = %v", err)
	}
	files := make(map[string]string, len(reader.File))
	for _, file := range reader.File {
		body, err := readZipFile(file)
		if err != nil {
			t.Fatalf("read %s error = %v", file.Name, err)
		}
		files[file.Name] = string(body)
	}
	return files
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open zip file %s: %w", file.Name, err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read zip file %s: %w", file.Name, err)
	}
	return body, nil
}

func hasChange(changes []*pb.ImportChangeSummary, op pb.ImportOperation, entityType string, count int32) bool {
	for _, change := range changes {
		if change.GetOperation() == op && change.GetEntityType() == entityType && change.GetCount() == count {
			return true
		}
	}
	return false
}

func hasValidationError(errors []*pb.ImportValidationError, section, message string) bool {
	for _, err := range errors {
		if err.GetSection() == section && strings.Contains(err.GetMessage(), message) {
			return true
		}
	}
	return false
}

func validCSV() string {
	return `# SECTION: SITE
name,id (read only)
Site A,

# SECTION: BUILDING
name,id (read only),site,aisles,racks_per_aisle
Building A,,NAME:Site A,2,2

# SECTION: RACK
label,id (read only),building,site,zone,rows,columns,order_index,aisle_index,position_in_aisle
Rack A,,NAME:Building A,,Z1,4,6,BOTTOM_LEFT,0,0

# SECTION: MINER
device_identifier (read only),serial_number (read only),name,ip_address (read only),mac_address (read only),site,building,rack,rack_row,rack_col
miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,NAME:Rack A,0,0
`
}

func testSnapshot() *snapshot {
	return &snapshot{
		sites: []sitemodels.Site{
			{Name: "Site A"},
			{Name: "Site B"},
		},
		buildings: []buildingmodels.Building{
			{SiteLabel: "Site A", Name: "Building A"},
			{SiteLabel: "Site B", Name: "Building B"},
		},
		racks: []rackSnapshot{
			{Label: "Rack A"},
			{Label: "Rack B"},
		},
		miners: []minerSnapshot{
			{DeviceIdentifier: "miner-1", SerialNumber: "SN1", Name: "Miner 1", IPAddress: "10.0.0.5", MACAddress: "aa:bb:cc:dd:ee:ff"},
			{DeviceIdentifier: "miner-2"},
		},
	}
}

func testSnapshotMatchingValidCSV() *snapshot {
	return &snapshot{
		sites: []sitemodels.Site{
			{
				Name:            "Site A",
				LocationCity:    "Austin",
				LocationState:   "TX",
				Country:         "US",
				PowerCapacityMw: 1.5,
				NetworkConfig:   "10.0.0.0/24",
				Address:         "1 Main",
				PostalCode:      "78701",
				Timezone:        "America/Chicago",
				Notes:           "Primary",
			},
		},
		buildings: []buildingmodels.Building{
			{
				SiteLabel:             "Site A",
				Name:                  "Building A",
				Description:           "North",
				PowerKw:               100,
				OverheadKw:            10,
				Aisles:                2,
				PhysicalRackCount:     4,
				RacksPerAisle:         2,
				DefaultRackRows:       4,
				DefaultRackColumns:    6,
				DefaultRackOrderIndex: buildingmodels.RackOrderIndexBottomLeft,
			},
		},
		racks: []rackSnapshot{
			{
				Site:            "Site A",
				Building:        "Building A",
				Label:           "Rack A",
				Zone:            "Z1",
				Rows:            4,
				Columns:         6,
				CoolingType:     "AIR",
				OrderIndex:      "BOTTOM_LEFT",
				AisleIndex:      "0",
				PositionInAisle: "0",
			},
		},
		miners: []minerSnapshot{
			{
				DeviceIdentifier: "miner-1",
				SerialNumber:     "SN1",
				Name:             "Miner 1",
				IPAddress:        "10.0.0.5",
				MACAddress:       "aa:bb:cc:dd:ee:ff",
				Site:             "Site A",
				Building:         "Building A",
				Rack:             "Rack A",
				RackRow:          "0",
				RackCol:          "0",
			},
		},
	}
}
