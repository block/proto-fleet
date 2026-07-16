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
	pb "github.com/block/proto-fleet/server/generated/grpc/sitemap/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	buildingmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
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
	if !strings.Contains(csvText, "label (read only),building,site,zone,rows,columns,order_index,aisle_index,position_in_aisle") {
		t.Fatalf("%s missing expected RACK headers: %q", siteMapExportCSVPath, csvText)
	}

	guideText := files[siteMapExportGuideTXTPath]
	for _, want := range []string{
		"Edit proto-fleet-site-map/site-map.csv",
		"If rack is set, the rack determines the miner's building and site.",
		"Headers ending in \"(read only)\" identify existing records or reference data.",
		"Leave omitted rows in place keeps missing rows unchanged.",
		"Remove omitted rows soft-deletes omitted sites, buildings, and racks, and unassigns omitted miners.",
		"Site, building, and rack names/labels are identities, not rename fields.",
	} {
		if !strings.Contains(guideText, want) {
			t.Fatalf("%s missing %q: %q", siteMapExportGuideTXTPath, want, guideText)
		}
	}
}

func TestMaxImportBytesAllowsLargeFleetExports(t *testing.T) {
	if maxImportBytes < 64*1024*1024 {
		t.Fatalf("maxImportBytes = %d, want at least 64 MiB", maxImportBytes)
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
	csv := strings.Replace(validCSV(), "Site A\n", "Site A\nNew Site\n", 1)
	csv = strings.Replace(csv, "Building A,Site A,2,2\n", "Building A,Site A,2,2\nNew Building,Site A,2,2\n", 1)
	csv = strings.Replace(csv, "Rack A,Building A,,Z1,4,6,BOTTOM_LEFT,0,0\n", "Rack A,Building A,,Z1,4,6,BOTTOM_LEFT,0,0\nNew Rack,Building A,,Z1,4,6,BOTTOM_LEFT,0,1\n", 1)
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
	csv := strings.Replace(validCSV(), "Site A\n", "Site A\nNew Site\n", 1)
	csv = strings.Replace(csv, "Building A,Site A,2,2\n", "Building A,Site A,2,2\nNew Building,New Site,2,2\n", 1)
	csv = strings.Replace(csv, "Rack A,Building A,,Z1,4,6,BOTTOM_LEFT,0,0\n", "Rack A,Building A,,Z1,4,6,BOTTOM_LEFT,0,0\nNew Rack,New Building,,Z2,4,6,BOTTOM_LEFT,0,1\n", 1)
	csv = strings.Replace(csv, "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,Rack A,0,0", "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,New Rack,0,0", 1)
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

func TestBuildPlanRejectsBuildingUnknownSite(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": nil,
		"BUILDING": {
			{"__row": "5", "site": "Typo Site", "building": "New Building", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK":  nil,
		"MINER": nil,
	}}
	snap := &snapshot{sites: []sitemodels.Site{{Name: "Site A"}}}

	plan := buildPlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 1 || plan.errors[0].GetSection() != "BUILDING" || plan.errors[0].GetMessage() != `unknown site "Typo Site"` {
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
			{"__row": "5", "site": "Site A", "building": "Building A", "aisles": "1", "racks_per_aisle": "1"},
		},
		"RACK": {
			{"__row": "9", "rack": "Rack A", "site": "Site A", "building": "Building A", "rows": "4", "columns": "6"},
		},
		"MINER": {
			{"__row": "13", "device_identifier": "miner-1", "serial_number": "SN1", "name": "Miner 1", "ip_address": "10.0.0.5", "mac_address": "aa:bb:cc:dd:ee:ff", "site": "Site A"},
		},
	}}
	snap := &snapshot{
		sites:     []sitemodels.Site{{Name: "Site A"}},
		buildings: []buildingmodels.Building{{SiteLabel: "Site A", Name: "Building A"}},
		racks:     []rackSnapshot{{Label: "Rack A", Site: "Site A", Building: "Building A", Rows: 4, Columns: 6}},
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
	csv := strings.Replace(validCSV(), "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,Rack A,0,0", "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,Rack A,0,1", 1)
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

func TestBuildPlanRejectsMinerRenames(t *testing.T) {
	csv := strings.Replace(validCSV(), "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,Rack A,0,0", "miner-1,SN1,Renamed Miner,10.0.0.5,aa:bb:cc:dd:ee:ff,,,Rack A,0,0", 1)
	parsed, errs := parseSiteMapCSV([]byte(csv))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}

	plan := buildPlan(parsed, testSnapshotMatchingValidCSV(), pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 1 || plan.errors[0].GetMessage() != "name is read-only for existing miner miner-1" {
		t.Fatalf("plan errors = %v, want name read-only error", plan.errors)
	}
}

func TestBuildPlanReportsRowCitedErrors(t *testing.T) {
	csv := strings.Replace(
		validCSV(),
		"miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,Rack A,0,0\n",
		"miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,Rack A,0,0\nminer-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,Rack A,0,0\n",
		1,
	)
	parsed, errs := parseSiteMapCSV([]byte(csv))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}

	plan := buildPlan(parsed, testSnapshot(), pb.OmissionMode_OMISSION_MODE_LEAVE_IN_PLACE)
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
	csv := strings.Replace(validCSV(), "Rack A", "'-Rack", 1)
	csv = strings.Replace(csv, "Rack A", "'-Rack", 1)
	parsed, errs := parseSiteMapCSV([]byte(csv))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}

	if got := parsed.sections["RACK"][0]["rack"]; got != "-Rack" {
		t.Fatalf("rack = %q, want unescaped -Rack", got)
	}
	if got := parsed.sections["MINER"][0]["rack"]; got != "-Rack" {
		t.Fatalf("miner rack = %q, want unescaped -Rack", got)
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
	csv := strings.Replace(validCSV(), "Site A\n", "\" Site A \"\n", 1)
	parsed, errs := parseSiteMapCSV([]byte(csv))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}
	if got := parsed.sections["SITE"][0]["site"]; got != " Site A " {
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
	if got := parsed.sections["SITE"][0]["site"]; got != "# SECTION: RACK" {
		t.Fatalf("site = %q, want section-marker-shaped site name", got)
	}
}

func TestBuildPlanTreatsEscapedExportValuesAsNoOp(t *testing.T) {
	snap := &snapshot{
		sites: []sitemodels.Site{{Name: "-Site"}},
		buildings: []buildingmodels.Building{{
			SiteLabel:     "-Site",
			Name:          "+Building",
			Aisles:        2,
			RacksPerAisle: 2,
		}},
		racks: []rackSnapshot{{
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
	rows := []map[string]string{{
		"device_identifier": "miner-1",
		"rack":              "Rack A",
	}}
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

	if err := svc.applyMinerRows(ctx, orgID, rows, nil, nil, nil, nil, map[string]rackSnapshot{"Rack A": rack}, existing); err != nil {
		t.Fatalf("applyMinerRows error = %v", err)
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

	if errs := validateSlotConflictsWithExisting(rows, snap, pb.OmissionMode_OMISSION_MODE_LEAVE_IN_PLACE); len(errs) != 0 {
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

	errs := validateSlotConflictsWithExisting(rows, snap, pb.OmissionMode_OMISSION_MODE_LEAVE_IN_PLACE)
	if len(errs) != 1 {
		t.Fatalf("errors = %+v, want one conflict", errs)
	}
	if errs[0].GetRow() != 21 || errs[0].GetSection() != "MINER" || errs[0].GetMessage() != "rack slot already occupied by miner miner-2" {
		t.Fatalf("unexpected error: %+v", errs[0])
	}
}

func TestValidateSlotConflictsWithExistingAllowsRemoveOmittedVacatedSlot(t *testing.T) {
	rows := []map[string]string{
		{"__row": "21", "device_identifier": "miner-1", "rack": "Rack A", "rack_row": "0", "rack_col": "1"},
	}
	snap := &snapshot{miners: []minerSnapshot{
		{DeviceIdentifier: "miner-1", Rack: "Rack A", RackRow: "0", RackCol: "0"},
		{DeviceIdentifier: "miner-2", Rack: "Rack A", RackRow: "0", RackCol: "1"},
	}}

	errs := validateSlotConflictsWithExisting(rows, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if len(errs) != 0 {
		t.Fatalf("errors = %+v, want omitted miner slot to be reusable", errs)
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

	errs := validateSlotConflictsWithExisting(rows, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
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

	errs := validateSlotCollisions(rows)
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

	errs := validateSlotConflictsWithExisting(rows, snap, pb.OmissionMode_OMISSION_MODE_LEAVE_IN_PLACE)
	if len(errs) != 1 || errs[0].GetRow() != 21 || errs[0].GetMessage() != "rack slot already occupied by miner miner-2" {
		t.Fatalf("errors = %+v, want normalized existing slot conflict", errs)
	}
}

func TestMinerRowsBlankSiteAndBuildingForRackedMiners(t *testing.T) {
	rows := minerRows([]minerSnapshot{{
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
	}}, nil)

	if got := rows[0][5]; got != "" {
		t.Fatalf("exported miner site = %q, want blank when rack is set", got)
	}
	if got := rows[0][6]; got != "" {
		t.Fatalf("exported miner building = %q, want blank when rack is set", got)
	}
}

func TestMinerRowsUseRackMembershipDerivedRackForExport(t *testing.T) {
	rows := minerRows([]minerSnapshot{{
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
	}}, nil)

	if got := rows[0][7]; got != "Rack A" {
		t.Fatalf("exported miner rack = %q, want Rack A", got)
	}
}

func TestMinerRowsBlankSiteForDirectBuildingAssignment(t *testing.T) {
	rows := minerRows([]minerSnapshot{{
		DeviceIdentifier: "miner-1",
		Site:             "Site A",
		Building:         "Building A",
	}}, []buildingmodels.Building{{SiteLabel: "Site A", Name: "Building A"}})

	if got := rows[0][5]; got != "" {
		t.Fatalf("exported miner site = %q, want blank when building is set", got)
	}
	if got := rows[0][6]; got != "Building A" {
		t.Fatalf("exported miner building = %q, want Building A", got)
	}
}

func TestMinerRowsPreserveSiteForAmbiguousDirectBuildingAssignment(t *testing.T) {
	rows := minerRows(
		[]minerSnapshot{{
			DeviceIdentifier: "miner-1",
			Site:             "Site A",
			Building:         "Building A",
		}},
		[]buildingmodels.Building{
			{SiteLabel: "Site A", Name: "Building A"},
			{SiteLabel: "Site B", Name: "Building A"},
		},
	)

	if got := rows[0][5]; got != "Site A" {
		t.Fatalf("exported miner site = %q, want Site A when building name is ambiguous", got)
	}
	if got := rows[0][6]; got != "Building A" {
		t.Fatalf("exported miner building = %q, want Building A", got)
	}
}

func TestDisplayHeadersMarkReadOnlyIdentityColumns(t *testing.T) {
	if got := strings.Join(displayHeaders("SITE", siteHeaders), ","); got != "name (read only)" {
		t.Fatalf("SITE headers = %q", got)
	}
	if got := strings.Join(displayHeaders("BUILDING", buildingHeaders), ","); got != "name (read only),site (read only),aisles,racks_per_aisle" {
		t.Fatalf("BUILDING headers = %q", got)
	}
	if got := strings.Join(displayHeaders("RACK", rackHeaders), ","); got != "label (read only),building,site,zone,rows,columns,order_index,aisle_index,position_in_aisle" {
		t.Fatalf("RACK headers = %q", got)
	}
	if got := strings.Join(displayHeaders("MINER", minerHeaders), ","); got != "device_identifier (read only),serial_number (read only),name (read only),ip_address (read only),mac_address (read only),site,building,rack,rack_row,rack_col" {
		t.Fatalf("MINER headers = %q", got)
	}
}

func TestRackExportRowsBlankSiteForUnambiguousBuildingAssignment(t *testing.T) {
	rows := rackExportRows(
		[]rackSnapshot{{Site: "Site A", Building: "Building A", Label: "Rack A"}},
		[]buildingmodels.Building{{SiteLabel: "Site A", Name: "Building A"}},
	)

	if got := rows[0][1]; got != "Building A" {
		t.Fatalf("exported rack building = %q, want Building A", got)
	}
	if got := rows[0][2]; got != "" {
		t.Fatalf("exported rack site = %q, want blank when building is unambiguous", got)
	}
}

func TestRackExportRowsPreserveSiteForAmbiguousBuildingAssignment(t *testing.T) {
	rows := rackExportRows(
		[]rackSnapshot{{Site: "Site A", Building: "Building A", Label: "Rack A"}},
		[]buildingmodels.Building{
			{SiteLabel: "Site A", Name: "Building A"},
			{SiteLabel: "Site B", Name: "Building A"},
		},
	)

	if got := rows[0][2]; got != "Site A" {
		t.Fatalf("exported rack site = %q, want Site A when building name is ambiguous", got)
	}
}

func TestDesiredMinerSiteBuildingResolvesDirectBuildingSite(t *testing.T) {
	buildingsByName, ambiguous := desiredBuildingNameLookup(
		[]map[string]string{{"site": "Site A", "building": "Building A"}},
		nil,
	)

	site, building := desiredMinerSiteBuilding(
		map[string]string{"site": "", "building": "Building A", "rack": ""},
		nil,
		buildingsByName,
		ambiguous,
	)
	if site != "Site A" || building != "Building A" {
		t.Fatalf("placement = (%q, %q), want (Site A, Building A)", site, building)
	}
}

func TestDesiredMinerSiteBuildingResolvesUnassignedDuplicateBuilding(t *testing.T) {
	buildingsByName, ambiguous := desiredBuildingNameLookup(nil, []buildingmodels.Building{
		{SiteLabel: "", Name: "Building A"},
		{SiteLabel: "Site A", Name: "Building A"},
	})

	site, building := desiredMinerSiteBuilding(
		map[string]string{"site": "", "building": "Building A", "rack": ""},
		nil,
		buildingsByName,
		ambiguous,
	)
	if site != "" || building != "Building A" {
		t.Fatalf("placement = (%q, %q), want unassigned Building A", site, building)
	}
	if ambiguous["Building A"] {
		t.Fatal("single unassigned building should disambiguate blank-site building rows")
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

	if errs := validatePlacementConsistency(rows, nil, nil, nil, snap); len(errs) != 0 {
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

	errs := validateReadOnlyMinerFields(rows, snap)
	if len(errs) != 1 || errs[0].GetMessage() != "ip_address is read-only for existing miner miner-1" {
		t.Fatalf("errors = %+v, want ip_address read-only error", errs)
	}
}

func TestValidateReadOnlyMinerFieldsIncludesName(t *testing.T) {
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

	errs := validateReadOnlyMinerFields(rows, snap)
	if len(errs) != 1 || errs[0].GetMessage() != "name is read-only for existing miner miner-1" {
		t.Fatalf("errors = %+v, want name read-only error", errs)
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

	errs := validateRackPlacementTargets(rows, nil, nil, snap)
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

	errs := validateBuildingSiteTargets(rows, siteRows, snap)
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

	errs := validatePlacementConsistency(rows, nil, nil, nil, snap)
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

	errs := validateRackCapacity(minerRows, rackRows, &snapshot{}, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(errs) != 1 || errs[0].GetSection() != "MINER" {
		t.Fatalf("errors = %+v, want rack capacity error", errs)
	}
}

func TestValidateRackCapacityCountsRetainedOmittedMiners(t *testing.T) {
	minerRows := []map[string]string{{"device_identifier": "miner-2", "rack": "Rack A"}}
	rackRows := []map[string]string{{"rack": "Rack A", "rows": "1", "columns": "1"}}
	snap := &snapshot{miners: []minerSnapshot{{DeviceIdentifier: "miner-1", Rack: "Rack A"}}}

	errs := validateRackCapacity(minerRows, rackRows, snap, pb.OmissionMode_OMISSION_MODE_LEAVE_IN_PLACE)
	if len(errs) != 1 || errs[0].GetSection() != "MINER" {
		t.Fatalf("errors = %+v, want rack capacity error", errs)
	}
}

func TestValidateRackCapacityCountsHiddenRackMembers(t *testing.T) {
	minerRows := []map[string]string{{"device_identifier": "miner-1", "rack": "Rack A"}}
	rackRows := []map[string]string{{"rack": "Rack A", "rows": "1", "columns": "1"}}
	snap := &snapshot{hiddenRackMembers: []minerSnapshot{{DeviceIdentifier: "hidden-1", Rack: "Rack A"}}}

	errs := validateRackCapacity(minerRows, rackRows, snap, pb.OmissionMode_OMISSION_MODE_LEAVE_IN_PLACE)
	if len(errs) != 1 || errs[0].GetSection() != "MINER" {
		t.Fatalf("errors = %+v, want rack capacity error", errs)
	}
}

func TestValidateRackSlotBoundsRejectsPartialCoordinates(t *testing.T) {
	minerRows := []map[string]string{{"__row": "21", "device_identifier": "miner-1", "rack": "Rack A", "rack_row": "", "rack_col": "3"}}
	rackRows := []map[string]string{{"rack": "Rack A", "rows": "4", "columns": "6"}}

	errs := validateRackSlotBounds(minerRows, rackRows, &snapshot{})
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

	errs := validateRackGridPositions(rackRows, buildingRows, &snapshot{})
	if len(errs) != 1 || !strings.Contains(errs[0].GetMessage(), "aisle_index 2 is out of bounds") {
		t.Fatalf("errors = %+v, want aisle bounds error", errs)
	}
}

func TestValidateRackGridCollisionsRejectsDuplicateCsvCells(t *testing.T) {
	rackRows := []map[string]string{
		{"__row": "10", "site": "Site A", "building": "Building A", "rack": "Rack A", "aisle_index": "0", "position_in_aisle": "0"},
		{"__row": "11", "site": "Site A", "building": "Building A", "rack": "Rack B", "aisle_index": "0", "position_in_aisle": "0"},
	}

	errs := validateRackGridCollisions(rackRows, &snapshot{}, pb.OmissionMode_OMISSION_MODE_LEAVE_IN_PLACE)
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

	errs := validateRackGridCollisions(rackRows, snap, pb.OmissionMode_OMISSION_MODE_LEAVE_IN_PLACE)
	if len(errs) != 1 || errs[0].GetMessage() != "rack grid cell already occupied by rack Rack A" {
		t.Fatalf("errors = %+v, want retained rack duplicate grid cell", errs)
	}
}

func TestValidateExistingSlotsFitRackDimensionsBlocksShrink(t *testing.T) {
	rackRows := []map[string]string{{"rack": "Rack A", "rows": "1", "columns": "1"}}
	snap := &snapshot{
		racks:  []rackSnapshot{{Label: "Rack A", Rows: 4, Columns: 6}},
		miners: []minerSnapshot{{DeviceIdentifier: "miner-1", Rack: "Rack A", RackRow: "1", RackCol: "0"}},
	}

	errs := validateExistingSlotsFitRackDimensions(nil, rackRows, snap, pb.OmissionMode_OMISSION_MODE_LEAVE_IN_PLACE)
	if len(errs) != 1 || !strings.Contains(errs[0].GetMessage(), "does not fit rack") {
		t.Fatalf("errors = %+v, want slot fit error", errs)
	}
}

func TestValidateExistingSlotsFitRackDimensionsCountsHiddenRackMembers(t *testing.T) {
	rackRows := []map[string]string{{"rack": "Rack A", "rows": "1", "columns": "1"}}
	snap := &snapshot{hiddenRackMembers: []minerSnapshot{{DeviceIdentifier: "hidden-1", Rack: "Rack A", RackRow: "1", RackCol: "0"}}}

	errs := validateExistingSlotsFitRackDimensions(nil, rackRows, snap, pb.OmissionMode_OMISSION_MODE_LEAVE_IN_PLACE)
	if len(errs) != 1 || errs[0].GetSection() != "MINER" {
		t.Fatalf("errors = %+v, want hidden member slot dimension error", errs)
	}
}

func TestValidateBuildingRackCapacityBlocksOverfilledBuilding(t *testing.T) {
	rackRows := []map[string]string{
		{"site": "Site A", "building": "Building A", "rack": "Rack A"},
		{"site": "Site A", "building": "Building A", "rack": "Rack B"},
	}
	buildingRows := []map[string]string{{"site": "Site A", "building": "Building A", "aisles": "1", "racks_per_aisle": "1"}}

	errs := validateBuildingRackCapacity(rackRows, buildingRows, &snapshot{})
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

	errs := validateBuildingRackCapacity(rackRows, buildingRows, &snapshot{})
	if len(errs) != 1 || errs[0].GetSection() != "RACK" {
		t.Fatalf("errors = %+v, want site-less building rack capacity error", errs)
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

	errs := validateBuildingExistingRacksFitLayout(nil, buildingRows, snap, pb.OmissionMode_OMISSION_MODE_LEAVE_IN_PLACE)
	if len(errs) != 1 || !strings.Contains(errs[0].GetMessage(), "does not fit building") {
		t.Fatalf("errors = %+v, want site-less building layout fit error", errs)
	}
}

func TestParseSiteMapCSVAcceptsSpreadsheetPaddedSectionRows(t *testing.T) {
	csv := validCSV()
	csv = strings.Replace(csv, "# SECTION: SITE\n", "# SECTION: SITE,,,,,,,,,,\n", 1)
	csv = strings.Replace(csv, "\n\n# SECTION: BUILDING\n", "\n,,,,,,,,,,\n# SECTION: BUILDING,,,,,,,,,,\n", 1)
	csv = strings.Replace(csv, "\n\n# SECTION: RACK\n", "\n,,,,,,,,,,\n# SECTION: RACK,,,,,,,,,,\n", 1)
	csv = strings.Replace(csv, "\n\n# SECTION: MINER\n", "\n,,,,,,,,,,\n# SECTION: MINER,,,,,,,,,,\n", 1)
	csv = strings.Replace(csv, "name (read only)\n", "name (read only),\n", 1)
	csv = strings.Replace(csv, "Site A\n", "Site A,\n", 1)
	csv = strings.Replace(csv, "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,Rack A,0,0\n", "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,Rack A,0,,\n", 1)

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

func validCSV() string {
	return `# SECTION: SITE
name (read only)
Site A

# SECTION: BUILDING
name (read only),site (read only),aisles,racks_per_aisle
Building A,Site A,2,2

# SECTION: RACK
label (read only),building,site,zone,rows,columns,order_index,aisle_index,position_in_aisle
Rack A,Building A,,Z1,4,6,BOTTOM_LEFT,0,0

# SECTION: MINER
device_identifier (read only),serial_number (read only),name (read only),ip_address (read only),mac_address (read only),site,building,rack,rack_row,rack_col
miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,,,Rack A,0,0
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
