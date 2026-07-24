package sitemap

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/sitemap/v1"
	buildingmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
	sitemodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
)

func TestResolvePlanClassifiesSiteIdentity(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": {
			{"__row": "3", "name": "Site A", "id": "1"},   // existing, unchanged
			{"__row": "4", "name": "Renamed", "id": "2"},  // existing, renamed
			{"__row": "5", "name": "Brand New", "id": ""}, // create
		},
	}}
	snap := &snapshot{sites: []sitemodels.Site{
		{ID: 1, Name: "Site A"},
		{ID: 2, Name: "Site B"},
	}}

	plan := resolvePlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if len(plan.sites) != 3 {
		t.Fatalf("resolved sites = %d, want 3", len(plan.sites))
	}
	if plan.sites[0].action != actionNone {
		t.Errorf("Site A action = %v, want none", plan.sites[0].action)
	}
	if plan.sites[1].action != actionUpdate || plan.sites[1].prevName != "Site B" {
		t.Errorf("Renamed site = action %v prevName %q, want update/\"Site B\"", plan.sites[1].action, plan.sites[1].prevName)
	}
	if plan.sites[2].action != actionCreate || plan.sites[2].id != nil {
		t.Errorf("Brand New site = action %v id %v, want create/nil", plan.sites[2].action, plan.sites[2].id)
	}
}

func TestResolvePlanCreateClassificationHonorsExistingNames(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE": {
			{"__row": "3", "name": "Site A", "id": ""},    // name-only, matches existing ⇒ not a create
			{"__row": "4", "name": "Brand New", "id": ""}, // new name ⇒ create
		},
	}}
	snap := &snapshot{sites: []sitemodels.Site{{ID: 1, Name: "Site A"}}}

	plan := resolvePlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if plan.sites[0].action != actionNone {
		t.Errorf("name-only existing site action = %v, want none", plan.sites[0].action)
	}
	if plan.sites[1].action != actionCreate {
		t.Errorf("new site action = %v, want create", plan.sites[1].action)
	}
	if got := countSiteCreateNodes(plan.sites); got != 1 {
		t.Errorf("site create count = %d, want 1", got)
	}
}

func TestResolvePlanCarriesBuildingSiteRef(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE":     {{"__row": "3", "name": "Site A", "id": "1"}},
		"BUILDING": {{"__row": "6", "name": "Bldg", "id": "10", "site": "Site A", "aisles": "2", "racks_per_aisle": "2"}},
	}}
	snap := &snapshot{
		sites:     []sitemodels.Site{{ID: 1, Name: "Site A"}},
		buildings: []buildingmodels.Building{{ID: 10, Name: "Bldg", SiteLabel: "Site A", Aisles: 2, RacksPerAisle: 2}},
	}

	plan := resolvePlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	b := plan.buildings[0]
	if b.siteRef != "Site A" || b.siteLabel != "Site A" {
		t.Fatalf("building site = (%q, %q), want Site A", b.siteRef, b.siteLabel)
	}
	if b.action != actionNone {
		t.Errorf("building action = %v, want none (unchanged)", b.action)
	}
}

func TestResolvePlanInfersRackSiteFromBuildingID(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE":     {{"__row": "3", "name": "Site A", "id": "1"}},
		"BUILDING": {{"__row": "6", "name": "Bldg", "id": "10", "site": "1", "aisles": "2", "racks_per_aisle": "2"}},
		"RACK":     {{"__row": "9", "label": "R1", "id": "20", "building": "10", "site": ""}},
	}}
	snap := &snapshot{
		sites:     []sitemodels.Site{{ID: 1, Name: "Site A"}},
		buildings: []buildingmodels.Building{{ID: 10, Name: "Bldg", SiteLabel: "Site A"}},
		racks:     []rackSnapshot{{ID: 20, Label: "R1", Building: "Bldg", Site: "Site A"}},
	}

	// resolveReferences canonicalizes the rack's building id into a building name
	// and fills the implied site; resolvePlan then reads those canonical names.
	if errs := resolveReferences(parsed, snap); len(errs) != 0 {
		t.Fatalf("resolveReferences errors = %+v", errs)
	}
	plan := resolvePlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	r := plan.racks[0]
	if r.buildingLabel != "Bldg" || r.siteLabel != "Site A" {
		t.Errorf("rack building/site = %q/%q, want Bldg/Site A inferred from building id", r.buildingLabel, r.siteLabel)
	}
}

func TestResolvePlanOmissionsMatchLegacyCounts(t *testing.T) {
	parsed := &parsedCSV{sections: map[string][]map[string]string{
		"SITE":     {{"__row": "3", "name": "Site A", "id": "1"}},
		"BUILDING": nil,
		"RACK":     nil,
		"MINER":    nil,
	}}
	snap := &snapshot{
		sites:  []sitemodels.Site{{ID: 1, Name: "Site A"}, {ID: 2, Name: "Site B"}},
		miners: []minerSnapshot{{DeviceIdentifier: "m1"}},
	}

	plan := resolvePlan(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	want := computeOmissions(parsed, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if plan.omissions.GetSites() != want.GetSites() || plan.omissions.GetSites() != 1 {
		t.Errorf("site omissions = %d, want 1", plan.omissions.GetSites())
	}
	if plan.omissions.GetMiners() != 1 {
		t.Errorf("miner omissions = %d, want 1", plan.omissions.GetMiners())
	}
}

func TestResolveMinersFlagsIdentityRenameAndReadOnly(t *testing.T) {
	rows := []map[string]string{
		{"__row": "20", "device_identifier": "m1", "name": "Renamed", "serial_number": "SN1", "ip_address": "10.0.0.9", "mac_address": "aa"},
		{"__row": "21", "device_identifier": "unknown", "name": "X"},
	}
	existing := map[string]minerSnapshot{
		"m1": {DeviceIdentifier: "m1", Name: "Orig", SerialNumber: "SN1", IPAddress: "10.0.0.5", MACAddress: "aa"},
	}

	miners := resolveMiners(rows, existing)
	if !miners[0].renamed {
		t.Errorf("m1 renamed = false, want true")
	}
	if miners[1].existing != nil {
		t.Errorf("unknown miner existing = %+v, want nil", miners[1].existing)
	}

	if got := countMinerRenameNodes(miners); got != 1 {
		t.Errorf("rename count = %d, want 1", got)
	}
	if errs := validateKnownMiners(miners); len(errs) != 1 || errs[0].GetRow() != 21 {
		t.Errorf("validateKnownMiners = %+v, want one error on row 21", errs)
	}
	roErrs := validateReadOnlyMinerFields(miners)
	if len(roErrs) != 1 || roErrs[0].GetMessage() != "ip_address is read-only for existing miner m1" {
		t.Errorf("read-only errs = %+v, want ip_address error", roErrs)
	}
}

func TestTopologyViewIsOmissionAware(t *testing.T) {
	// A building references site "Drop", which exists live but is omitted from the
	// CSV. Under unspecified mode the reference is known; under remove-omitted the
	// omitted site is dropped from the desired topology, so the reference is unknown.
	sections := map[string][]map[string]string{
		"SITE":     {{"__row": "3", "name": "Keep"}},
		"BUILDING": {{"__row": "6", "name": "Bldg", "site": "Drop"}},
	}
	snap := &snapshot{sites: []sitemodels.Site{{Name: "Keep"}, {Name: "Drop"}}}

	keep := resolvePlan(&parsedCSV{sections: sections}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if errs := validateBuildingSiteTargets(keep.buildings, keep.topology); len(errs) != 0 {
		t.Fatalf("unspecified mode = %+v, want no errors (Drop still live)", errs)
	}

	remove := resolvePlan(&parsedCSV{sections: sections}, snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	errs := validateBuildingSiteTargets(remove.buildings, remove.topology)
	if len(errs) != 1 || errs[0].GetRow() != 6 || errs[0].GetMessage() != `unknown site "Drop"` {
		t.Fatalf("remove-omitted mode = %+v, want unknown site \"Drop\" on row 6", errs)
	}
}

func TestResolveMinersFlagsPlacementMoves(t *testing.T) {
	sections := map[string][]map[string]string{
		"MINER": {
			{"__row": "20", "device_identifier": "m1", "name": "Orig", "rack": "Rack A", "rack_row": "0", "rack_col": "1"},
			{"__row": "21", "device_identifier": "m2", "name": "Renamed", "rack": "Rack A", "rack_row": "0", "rack_col": "0"},
		},
	}
	snap := &snapshot{miners: []minerSnapshot{
		{DeviceIdentifier: "m1", Name: "Orig", Rack: "Rack A", RackRow: "0", RackCol: "0"}, // slot changed ⇒ moved
		{DeviceIdentifier: "m2", Name: "Orig", Rack: "Rack A", RackRow: "0", RackCol: "0"}, // placement same ⇒ only renamed
	}}

	plan := resolvePlan(&parsedCSV{sections: sections}, snap, pb.OmissionMode_OMISSION_MODE_UNSPECIFIED)
	if !plan.miners[0].moved || plan.miners[0].renamed {
		t.Errorf("m1 = moved %v renamed %v, want moved-only", plan.miners[0].moved, plan.miners[0].renamed)
	}
	if plan.miners[1].moved || !plan.miners[1].renamed {
		t.Errorf("m2 = moved %v renamed %v, want renamed-only", plan.miners[1].moved, plan.miners[1].renamed)
	}
	if got := countMinerMoveNodes(plan.miners); got != 1 {
		t.Errorf("move count = %d, want 1", got)
	}
	if errs := validateMinerRenamePermission(plan.miners); len(errs) != 1 || errs[0].GetRow() != 21 {
		t.Errorf("rename permission errs = %+v, want one on row 21", errs)
	}
}

func TestScopePopulationIncludesHiddenRackMembers(t *testing.T) {
	snap := &snapshot{
		miners:            []minerSnapshot{{DeviceIdentifier: "m1"}},
		hiddenRackMembers: []minerSnapshot{{DeviceIdentifier: "hidden1"}},
	}
	pop := scopePopulation(snap, pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED)
	if len(pop.miners) != 1 || len(pop.hiddenRackMembers) != 1 {
		t.Fatalf("population = %d miners / %d hidden, want 1/1", len(pop.miners), len(pop.hiddenRackMembers))
	}
}
