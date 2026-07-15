package sitemap

import (
	"strings"
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/sitemap/v1"
	buildingmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
	sitemodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
)

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

func TestBuildPlanWithRemoveOmittedSummarizesDestructiveChanges(t *testing.T) {
	parsed, errs := parseSiteMapCSV([]byte(validCSV()))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}

	plan := buildPlan(parsed, testSnapshot(), pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %v", plan.errors)
	}

	var sawDeleteSite, sawUnassignMiner bool
	for _, change := range plan.changes {
		if change.Operation == pb.ImportOperation_IMPORT_OPERATION_DELETE && change.EntityType == "site" && change.Count == 1 {
			sawDeleteSite = true
		}
		if change.Operation == pb.ImportOperation_IMPORT_OPERATION_UNASSIGN && change.EntityType == "miner" && change.Count == 1 {
			sawUnassignMiner = true
		}
	}
	if !sawDeleteSite || !sawUnassignMiner {
		t.Fatalf("changes did not include expected destructive summaries: %+v", plan.changes)
	}

	token := commitToken(parsed, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED, plan, testSnapshot())
	if token == "" {
		t.Fatal("commit token is empty")
	}
	leavePlan := buildPlan(parsed, testSnapshot(), pb.OmissionMode_OMISSION_MODE_LEAVE_IN_PLACE)
	if token == commitToken(parsed, pb.OmissionMode_OMISSION_MODE_LEAVE_IN_PLACE, leavePlan, testSnapshot()) {
		t.Fatal("commit token must include omission mode")
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
	csv := strings.Replace(validCSV(), "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,, ,Rack A,0,0", "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,, ,Rack A,0,1", 1)
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

func TestBuildPlanWithNoOmissionsSummarizesMinerRenames(t *testing.T) {
	csv := strings.Replace(validCSV(), "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,, ,Rack A,0,0", "miner-1,SN1,Renamed Miner,10.0.0.5,aa:bb:cc:dd:ee:ff,, ,Rack A,0,0", 1)
	parsed, errs := parseSiteMapCSV([]byte(csv))
	if len(errs) != 0 {
		t.Fatalf("parse errors = %v", errs)
	}

	plan := buildPlan(parsed, testSnapshotMatchingValidCSV(), pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.errors) != 0 {
		t.Fatalf("plan errors = %v", plan.errors)
	}

	var sawMinerRename bool
	for _, change := range plan.changes {
		if change.Operation == pb.ImportOperation_IMPORT_OPERATION_RENAME && change.EntityType == "miner" && change.Count == 1 {
			sawMinerRename = true
		}
	}
	if !sawMinerRename {
		t.Fatalf("changes did not include expected miner rename summary: %+v", plan.changes)
	}
}

func TestBuildPlanReportsRowCitedErrors(t *testing.T) {
	csv := strings.Replace(
		validCSV(),
		"miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,, ,Rack A,0,0\n",
		"miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,, ,Rack A,0,0\nminer-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,, ,Rack A,0,0\n",
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

func TestValidateSlotConflictsWithExistingAllowsSlotSwaps(t *testing.T) {
	rows := []map[string]string{
		{"__row": "21", "device_identifier": "miner-1", "rack": "Rack A", "rack_row": "0", "rack_col": "1"},
		{"__row": "22", "device_identifier": "miner-2", "rack": "Rack A", "rack_row": "0", "rack_col": "0"},
	}
	snap := &snapshot{miners: []minerSnapshot{
		{DeviceIdentifier: "miner-1", Rack: "Rack A", RackRow: "0", RackCol: "0"},
		{DeviceIdentifier: "miner-2", Rack: "Rack A", RackRow: "0", RackCol: "1"},
	}}

	if errs := validateSlotConflictsWithExisting(rows, snap); len(errs) != 0 {
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

	errs := validateSlotConflictsWithExisting(rows, snap)
	if len(errs) != 1 {
		t.Fatalf("errors = %+v, want one conflict", errs)
	}
	if errs[0].GetRow() != 21 || errs[0].GetSection() != "MINER" || errs[0].GetMessage() != "rack slot already occupied by miner miner-2" {
		t.Fatalf("unexpected error: %+v", errs[0])
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
	}})

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
	}})

	if got := rows[0][7]; got != "Rack A" {
		t.Fatalf("exported miner rack = %q, want Rack A", got)
	}
}

func TestMinerRowsBlankSiteForDirectBuildingAssignment(t *testing.T) {
	rows := minerRows([]minerSnapshot{{
		DeviceIdentifier: "miner-1",
		Site:             "Site A",
		Building:         "Building A",
	}})

	if got := rows[0][5]; got != "" {
		t.Fatalf("exported miner site = %q, want blank when building is set", got)
	}
	if got := rows[0][6]; got != "Building A" {
		t.Fatalf("exported miner building = %q, want Building A", got)
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

func TestValidateReadOnlyMinerFieldsAllowsNameChanges(t *testing.T) {
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

	if errs := validateReadOnlyMinerFields(rows, snap); len(errs) != 0 {
		t.Fatalf("name change should not be read-only, got %+v", errs)
	}
}

func TestMinerRenameUpdates(t *testing.T) {
	rows := []map[string]string{
		{"device_identifier": "miner-1", "name": "Renamed Miner"},
		{"device_identifier": "miner-2", "name": "Miner 2"},
		{"device_identifier": "unknown", "name": "Ignored"},
	}
	miners := []minerSnapshot{
		{DeviceIdentifier: "miner-1", Name: "Miner 1"},
		{DeviceIdentifier: "miner-2", Name: "Miner 2"},
	}

	names := minerRenameUpdates(rows, miners)
	if len(names) != 1 || names["miner-1"] != "Renamed Miner" {
		t.Fatalf("rename updates = %+v, want miner-1 only", names)
	}
}

func TestValidateRackCapacityBlocksOverfilledRack(t *testing.T) {
	minerRows := []map[string]string{
		{"device_identifier": "miner-1", "rack": "Rack A"},
		{"device_identifier": "miner-2", "rack": "Rack A"},
	}
	rackRows := []map[string]string{{"rack": "Rack A", "rows": "1", "columns": "1"}}

	errs := validateRackCapacity(minerRows, rackRows, &snapshot{})
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

func TestParseSiteMapCSVAcceptsSpreadsheetPaddedSectionRows(t *testing.T) {
	csv := validCSV()
	csv = strings.Replace(csv, "# SECTION: SITE\n", "# SECTION: SITE,,,,,,,,,,\n", 1)
	csv = strings.Replace(csv, "\n\n# SECTION: BUILDING\n", "\n,,,,,,,,,,\n# SECTION: BUILDING,,,,,,,,,,\n", 1)
	csv = strings.Replace(csv, "\n\n# SECTION: RACK\n", "\n,,,,,,,,,,\n# SECTION: RACK,,,,,,,,,,\n", 1)
	csv = strings.Replace(csv, "\n\n# SECTION: MINER\n", "\n,,,,,,,,,,\n# SECTION: MINER,,,,,,,,,,\n", 1)
	csv = strings.Replace(csv, "site\n", "site,\n", 1)
	csv = strings.Replace(csv, "Site A\n", "Site A,\n", 1)
	csv = strings.Replace(csv, "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,, ,Rack A,0,0\n", "miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,, ,Rack A,0,,\n", 1)

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

func validCSV() string {
	return `# SECTION: SITE
site
Site A

# SECTION: BUILDING
site,building,aisles,racks_per_aisle
Site A,Building A,2,2

# SECTION: RACK
site,building,rack,zone,rows,columns,order_index,aisle_index,position_in_aisle
Site A,Building A,Rack A,Z1,4,6,BOTTOM_LEFT,0,0

# SECTION: MINER
device_identifier,serial_number,name,ip_address,mac_address,site,building,rack,rack_row,rack_col
miner-1,SN1,Miner 1,10.0.0.5,aa:bb:cc:dd:ee:ff,, ,Rack A,0,0
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
