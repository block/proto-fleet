package sitemap

import (
	"fmt"
	"strconv"

	pb "github.com/block/proto-fleet/server/generated/grpc/sitemap/v1"
	buildingmodels "github.com/block/proto-fleet/server/internal/domain/buildings/models"
	sitemodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
)

// resolved.go builds the canonical resolved plan for a sitemap import: parsed
// rows and the live snapshot are collapsed into one graph of typed nodes keyed
// by stable ID where one exists, with parents linked as pointers, so preview,
// validation, the commit token, and apply all consume a single representation.

// nodeAction is the resolved verb for a topology node (site/building/rack).
// Miners track renamed/moved/unassigned as independent booleans instead, since
// a miner can be both renamed and moved in one import.
type nodeAction int

const (
	actionNone nodeAction = iota
	actionCreate
	actionUpdate
	actionDelete
)

type resolvedSite struct {
	id       *int64 // nil ⇒ create
	name     string
	prevName string // live name when id != nil
	action   nodeAction
	rowNum   int // 1-based CSV row for error provenance
}

type resolvedBuilding struct {
	id   *int64
	site *resolvedSite // nil when the site could not be linked
	// siteLabel is the desired site name even when no site node was linked.
	siteLabel     string
	name          string
	prevName      string
	prevSiteLabel string
	aisles        int32
	racksPerAisle int32
	action        nodeAction
	rowNum        int
}

type resolvedRack struct {
	id       *int64
	building *resolvedBuilding // nil ⇒ rack sits directly under a site
	site     *resolvedSite
	// buildingLabel / siteLabel are the desired names even when no parent was linked.
	buildingLabel   string
	siteLabel       string
	label           string
	prevLabel       string
	zone            string
	rows            int32
	columns         int32
	orderIndex      string
	aisleIndex      string
	positionInAisle string
	action          nodeAction
	rowNum          int
}

// resolvedMiner is keyed by device identifier; name and placement are its only
// mutable facets. existing is the matched live miner, or nil when the device is
// unknown.
type resolvedMiner struct {
	deviceID     string
	name         string
	serialNumber string
	ipAddress    string
	macAddress   string
	existing     *minerSnapshot
	rack         *resolvedRack
	building     *resolvedBuilding
	site         *resolvedSite
	rackLabel    string
	siteLabel    string
	buildLabel   string
	rackRow      string
	rackCol      string
	renamed      bool
	moved        bool
	unassigned   bool
	rowNum       int
}

// minerPopulation is the scoped mutable-miner set shared by omission counts,
// hidden-resource checks, validation, and apply.
type minerPopulation struct {
	miners            []minerSnapshot
	hiddenRackMembers []minerSnapshot
}

type resolvedPlan struct {
	mode       pb.OmissionMode
	sites      []*resolvedSite
	buildings  []*resolvedBuilding
	racks      []*resolvedRack
	miners     []*resolvedMiner
	population minerPopulation

	omissions *pb.OmissionCounts
	errors    []*pb.ImportValidationError
}

func scopePopulation(snap *snapshot, _ pb.OmissionMode) minerPopulation {
	return minerPopulation{
		miners:            snap.miners,
		hiddenRackMembers: snap.hiddenRackMembers,
	}
}

// resolvePlan builds the graph, records ID/name consistency errors, classifies
// node actions, and counts omissions. It does not mutate parsed.
func resolvePlan(parsed *parsedCSV, snap *snapshot, mode pb.OmissionMode) *resolvedPlan {
	plan := &resolvedPlan{
		mode:       mode,
		population: scopePopulation(snap, mode),
		omissions:  &pb.OmissionCounts{},
	}

	sitesByID := existingSitesByID(snap.sites)
	sitesByName := existingSitesByName(snap.sites)
	buildingsByID := existingBuildingsByID(snap.buildings)
	racksByID := existingRacksByID(snap.racks)

	plan.sites, plan.errors = resolveSites(parsed.sections["SITE"], sitesByID)
	sitesByNode := indexSitesByIdentity(plan.sites)

	var buildingErrs []*pb.ImportValidationError
	plan.buildings, buildingErrs = resolveBuildings(parsed.sections["BUILDING"], buildingsByID, sitesByID, sitesByName, sitesByNode)
	plan.errors = append(plan.errors, buildingErrs...)

	// A rack's site can be inferred from an unambiguous building name reference.
	// Under remove-omitted, existing buildings are deleted, so only CSV-declared
	// buildings can supply the inferred site.
	inferBuildings := snap.buildings
	if mode == pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
		inferBuildings = nil
	}
	inferSiteByBuilding, inferAmbiguous := desiredBuildingNameLookup(parsed.sections["BUILDING"], inferBuildings)

	var rackErrs []*pb.ImportValidationError
	plan.racks, rackErrs = resolveRacks(parsed.sections["RACK"], racksByID, buildingsByID, inferSiteByBuilding, inferAmbiguous)
	plan.errors = append(plan.errors, rackErrs...)

	plan.miners = resolveMiners(parsed.sections["MINER"], minerMap(snap.miners))

	classifyTopologyActions(plan, snap, mode)
	plan.omissions = computeOmissions(parsed, snap, mode)

	return plan
}

// resolveSites resolves SITE rows. A row with an id references an existing site;
// a row without an id is a create keyed by name.
func resolveSites(rows []map[string]string, existingByID map[int64]sitemodels.Site) ([]*resolvedSite, []*pb.ImportValidationError) {
	out := make([]*resolvedSite, 0, len(rows))
	for i, row := range rows {
		node := &resolvedSite{
			name:   siteSectionName(row),
			rowNum: rowNumber(row, i+1),
		}
		if id, ok := rowID(row); ok {
			node.id = int64Ptr(id)
			if existing, found := existingByID[id]; found {
				node.prevName = existing.Name
			}
		}
		out = append(out, node)
	}
	return out, nil
}

// resolveBuildings resolves BUILDING rows, linking each to its resolved site.
// Site precedence: site_id wins, then site name; a blank or unknown reference
// keeps the raw label with no linked node. A site_id/site-name mismatch is an error.
func resolveBuildings(
	rows []map[string]string,
	existingByID map[int64]buildingmodels.Building,
	sitesByID map[int64]sitemodels.Site,
	sitesByName map[string]sitemodels.Site,
	sitesByNode map[string]*resolvedSite,
) ([]*resolvedBuilding, []*pb.ImportValidationError) {
	out := make([]*resolvedBuilding, 0, len(rows))
	var errs []*pb.ImportValidationError
	for i, row := range rows {
		rn := rowNumber(row, i+1)
		node := &resolvedBuilding{
			name:          buildingSectionName(row),
			aisles:        optInt32(row, "aisles"),
			racksPerAisle: optInt32(row, "racks_per_aisle"),
			rowNum:        rn,
		}
		if id, ok := rowID(row); ok {
			node.id = int64Ptr(id)
			if existing, found := existingByID[id]; found {
				node.prevName = existing.Name
				node.prevSiteLabel = existing.SiteLabel
			}
		}

		siteName := row[fieldSite]
		if siteID, ok := idFromCell(row[fieldSiteID]); ok {
			if site, found := sitesByID[siteID]; found {
				if siteName != "" && siteName != site.Name {
					errs = append(errs, csvErr(rn, "BUILDING", "site_id "+quote(row[fieldSiteID])+" does not match site "+quote(siteName)))
					out = append(out, node)
					continue
				}
				siteName = site.Name
			}
		}
		node.siteLabel = siteName
		node.site = linkSite(siteName, sitesByName, sitesByNode)
		out = append(out, node)
	}
	return out, errs
}

// resolveRacks resolves RACK rows, linking each to its building and site.
// Precedence: building_id wins and dictates the site; otherwise a blank site is
// inferred from an unambiguous building name.
func resolveRacks(
	rows []map[string]string,
	existingByID map[int64]rackSnapshot,
	buildingsByID map[int64]buildingmodels.Building,
	inferSiteByBuilding map[string]buildingmodels.Building,
	inferAmbiguous map[string]bool,
) ([]*resolvedRack, []*pb.ImportValidationError) {
	out := make([]*resolvedRack, 0, len(rows))
	for i, row := range rows {
		rn := rowNumber(row, i+1)
		node := &resolvedRack{
			label:           rackSectionLabel(row),
			zone:            row["zone"],
			rows:            optInt32(row, "rows"),
			columns:         optInt32(row, "columns"),
			orderIndex:      row["order_index"],
			aisleIndex:      row["aisle_index"],
			positionInAisle: row["position_in_aisle"],
			buildingLabel:   row[fieldBuilding],
			siteLabel:       row[fieldSite],
			rowNum:          rn,
		}
		if id, ok := rowID(row); ok {
			node.id = int64Ptr(id)
			if existing, found := existingByID[id]; found {
				node.prevLabel = existing.Label
			}
		}
		if buildingID, ok := idFromCell(row[fieldBuildingID]); ok {
			if building, found := buildingsByID[buildingID]; found {
				node.buildingLabel = building.Name
				node.siteLabel = building.SiteLabel
			}
		} else if node.siteLabel == "" && node.buildingLabel != "" && !inferAmbiguous[node.buildingLabel] {
			if b, found := inferSiteByBuilding[node.buildingLabel]; found {
				node.siteLabel = b.SiteLabel
			}
		}
		out = append(out, node)
	}
	return out, nil
}

// resolveMiners resolves MINER rows, matching each to its live miner by device
// identifier and flagging renames.
func resolveMiners(rows []map[string]string, existingByID map[string]minerSnapshot) []*resolvedMiner {
	out := make([]*resolvedMiner, 0, len(rows))
	for i, row := range rows {
		node := &resolvedMiner{
			deviceID:     row["device_identifier"],
			name:         row[fieldName],
			serialNumber: row["serial_number"],
			ipAddress:    row["ip_address"],
			macAddress:   row["mac_address"],
			rackLabel:    row[fieldRack],
			siteLabel:    row[fieldSite],
			buildLabel:   row[fieldBuilding],
			rackRow:      row["rack_row"],
			rackCol:      row["rack_col"],
			rowNum:       rowNumber(row, i+1),
		}
		if existing, ok := existingByID[node.deviceID]; ok {
			e := existing
			node.existing = &e
			node.renamed = node.name != existing.Name
		}
		out = append(out, node)
	}
	return out
}

// validateKnownMiners rejects rows whose device identifier is not a live miner.
func validateKnownMiners(miners []*resolvedMiner) []*pb.ImportValidationError {
	var errs []*pb.ImportValidationError
	for _, m := range miners {
		if m.deviceID != "" && m.existing == nil {
			errs = append(errs, csvErr(m.rowNum, "MINER", "unknown miner device_identifier"))
		}
	}
	return errs
}

// validateReadOnlyMinerFields rejects edits to serial/ip/mac on existing miners.
func validateReadOnlyMinerFields(miners []*resolvedMiner) []*pb.ImportValidationError {
	var errs []*pb.ImportValidationError
	for _, m := range miners {
		if m.existing == nil {
			continue
		}
		for _, field := range []struct{ name, got, want string }{
			{"serial_number", m.serialNumber, m.existing.SerialNumber},
			{"ip_address", m.ipAddress, m.existing.IPAddress},
			{"mac_address", m.macAddress, m.existing.MACAddress},
		} {
			if field.got != field.want {
				errs = append(errs, csvErr(m.rowNum, "MINER", fmt.Sprintf("%s is read-only for existing miner %s", field.name, m.deviceID)))
			}
		}
	}
	return errs
}

// countMinerRenameNodes counts existing miners whose name changed.
func countMinerRenameNodes(miners []*resolvedMiner) int32 {
	var n int32
	for _, m := range miners {
		if m.renamed {
			n++
		}
	}
	return n
}

// classifyTopologyActions marks each topology node: a row whose identity is not
// already present in the live snapshot creates; a row matching an existing
// entity updates when a tracked field differs.
func classifyTopologyActions(plan *resolvedPlan, snap *snapshot, _ pb.OmissionMode) {
	siteNames := map[string]bool{}
	for _, s := range snap.sites {
		siteNames[s.Name] = true
	}
	buildingKeys := map[string]bool{}
	for _, b := range snap.buildings {
		buildingKeys[b.SiteLabel+"\x00"+b.Name] = true
	}
	rackLabels := map[string]bool{}
	for _, r := range snap.racks {
		rackLabels[r.Label] = true
	}

	for _, s := range plan.sites {
		if s.id == nil {
			if !siteNames[s.name] {
				s.action = actionCreate
			}
		} else if s.name != s.prevName {
			s.action = actionUpdate
		}
	}
	for _, b := range plan.buildings {
		if b.id == nil {
			if !buildingKeys[b.siteLabel+"\x00"+b.name] {
				b.action = actionCreate
			}
		} else if b.name != b.prevName || b.siteLabel != b.prevSiteLabel {
			b.action = actionUpdate
		}
	}
	for _, r := range plan.racks {
		if r.id == nil {
			if !rackLabels[r.label] {
				r.action = actionCreate
			}
		} else if r.label != r.prevLabel {
			r.action = actionUpdate
		}
	}
}

func countSiteCreateNodes(sites []*resolvedSite) int32 {
	var n int32
	for _, s := range sites {
		if s.action == actionCreate {
			n++
		}
	}
	return n
}

func countBuildingCreateNodes(buildings []*resolvedBuilding) int32 {
	var n int32
	for _, b := range buildings {
		if b.action == actionCreate {
			n++
		}
	}
	return n
}

func countRackCreateNodes(racks []*resolvedRack) int32 {
	var n int32
	for _, r := range racks {
		if r.action == actionCreate {
			n++
		}
	}
	return n
}

// computeChanges produces the preview change summaries from the resolved plan.
// Creates come from node classification and deletes from omission counts (the
// two are the same identity math); updates and miner rename/move still use the
// row-comparison helpers.
func computeChanges(resolved *resolvedPlan, parsed *parsedCSV, snap, targetSnap *snapshot, mode pb.OmissionMode) []*pb.ImportChangeSummary {
	var changes []*pb.ImportChangeSummary
	add := func(op pb.ImportOperation, entityType string, count int32, description string) {
		if count > 0 {
			changes = append(changes, &pb.ImportChangeSummary{Operation: op, EntityType: entityType, Count: count, Description: description})
		}
	}
	add(pb.ImportOperation_IMPORT_OPERATION_CREATE, "site", countSiteCreateNodes(resolved.sites), "new site rows")
	add(pb.ImportOperation_IMPORT_OPERATION_CREATE, fieldBuilding, countBuildingCreateNodes(resolved.buildings), "new building rows")
	add(pb.ImportOperation_IMPORT_OPERATION_CREATE, "rack", countRackCreateNodes(resolved.racks), "new rack rows")
	add(pb.ImportOperation_IMPORT_OPERATION_UPDATE, "site", countSiteUpdates(parsed.sections["SITE"], snap.sites), "site rows with changed details")
	add(pb.ImportOperation_IMPORT_OPERATION_UPDATE, fieldBuilding, countBuildingUpdates(parsed.sections["BUILDING"], snap.buildings), "building rows with changed details")
	add(pb.ImportOperation_IMPORT_OPERATION_UPDATE, "rack", countRackUpdates(parsed.sections["RACK"], snap.racks, targetSnap.buildings), "rack rows with changed details")
	add(pb.ImportOperation_IMPORT_OPERATION_RENAME, "miner", countMinerRenameNodes(resolved.miners), "miner rows with changed names")
	add(pb.ImportOperation_IMPORT_OPERATION_MOVE, "miner", countMinerPlacementUpdates(parsed.sections["MINER"], parsed.sections["RACK"], parsed.sections["BUILDING"], targetSnap), "miner placement rows with changed site, building, rack, or slot")
	if mode == pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
		add(pb.ImportOperation_IMPORT_OPERATION_UNASSIGN, "miner", resolved.omissions.GetMiners(), "omitted miner rows to unassign")
		add(pb.ImportOperation_IMPORT_OPERATION_DELETE, "rack", resolved.omissions.GetRacks(), "omitted rack rows to delete")
		add(pb.ImportOperation_IMPORT_OPERATION_DELETE, fieldBuilding, resolved.omissions.GetBuildings(), "omitted building rows to delete")
		add(pb.ImportOperation_IMPORT_OPERATION_DELETE, "site", resolved.omissions.GetSites(), "omitted site rows to delete")
	}
	return changes
}

// computeOmissions counts live entities absent from the CSV.
func computeOmissions(parsed *parsedCSV, snap *snapshot, _ pb.OmissionMode) *pb.OmissionCounts {
	out := &pb.OmissionCounts{}
	siteKeys := siteIdentitySet(parsed.sections["SITE"], snap.sites)
	buildingKeys := buildingIdentitySet(parsed.sections["BUILDING"], snap.buildings)
	rackKeys := rackIdentitySet(parsed.sections["RACK"], snap.racks)
	minerKeys := rowSet(parsed.sections["MINER"], "device_identifier")
	for _, site := range snap.sites {
		if !siteKeys[siteIdentity(site)] {
			out.Sites++
		}
	}
	for _, building := range snap.buildings {
		if !buildingKeys[buildingIdentity(building)] {
			out.Buildings++
		}
	}
	for _, rack := range snap.racks {
		if !rackKeys[rackIdentity(rack)] {
			out.Racks++
		}
	}
	for _, miner := range snap.miners {
		if !minerKeys[miner.DeviceIdentifier] {
			out.Miners++
		}
	}
	return out
}

func linkSite(name string, sitesByName map[string]sitemodels.Site, sitesByNode map[string]*resolvedSite) *resolvedSite {
	if name == "" {
		return nil
	}
	if node, ok := sitesByNode["name:"+name]; ok {
		return node
	}
	if _, ok := sitesByName[name]; ok {
		if node, ok := sitesByNode["name:"+name]; ok {
			return node
		}
	}
	return nil
}

func indexSitesByIdentity(sites []*resolvedSite) map[string]*resolvedSite {
	out := map[string]*resolvedSite{}
	for _, s := range sites {
		out["name:"+s.name] = s
		if s.id != nil {
			out["id:"+strconv.FormatInt(*s.id, 10)] = s
		}
	}
	return out
}

func existingSitesByID(sites []sitemodels.Site) map[int64]sitemodels.Site {
	out := map[int64]sitemodels.Site{}
	for _, s := range sites {
		out[s.ID] = s
	}
	return out
}

func existingSitesByName(sites []sitemodels.Site) map[string]sitemodels.Site {
	out := map[string]sitemodels.Site{}
	for _, s := range sites {
		out[s.Name] = s
	}
	return out
}

func existingBuildingsByID(buildings []buildingmodels.Building) map[int64]buildingmodels.Building {
	out := map[int64]buildingmodels.Building{}
	for _, b := range buildings {
		out[b.ID] = b
	}
	return out
}

func existingRacksByID(racks []rackSnapshot) map[int64]rackSnapshot {
	out := map[int64]rackSnapshot{}
	for _, r := range racks {
		out[r.ID] = r
	}
	return out
}

func optInt32(row map[string]string, field string) int32 {
	if v, err := parseInt32Value(row[field], field); err == nil {
		return v
	}
	return 0
}

func int64Ptr(v int64) *int64 { return &v }

func quote(v string) string { return strconv.Quote(v) }
