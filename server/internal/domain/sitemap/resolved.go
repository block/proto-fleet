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
	siteLabel string
	// siteRef is the canonical site name the row references, filled by resolveReferences.
	siteRef       string
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
	buildingLabel string
	siteLabel     string
	// siteRef / buildingRef are the canonical site and building names the row
	// references, filled in by resolveReferences.
	siteRef     string
	buildingRef string
	// buildingID is the id of the existing building the rack references, when the
	// reference resolved to one; nil for a same-import create or an unlinked rack.
	// It disambiguates two buildings that share a (site, name) pair.
	buildingID      *int64
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
	// buildingID is the id of the existing building the miner's placement targets
	// (via its building or rack reference), when it resolved to one; nil
	// otherwise. It disambiguates two buildings that share a (site, name) pair.
	buildingID *int64
	rackRow    string
	rackCol    string
	renamed    bool
	moved      bool
	unassigned bool
	rowNum     int
}

// minerPopulation is the scoped mutable-miner set shared by omission counts,
// hidden-resource checks, validation, and apply.
type minerPopulation struct {
	miners            []minerSnapshot
	hiddenRackMembers []minerSnapshot
}

// topologyView is the desired end-state topology (CSV rows merged over the
// omission-target snapshot) that placement-target validators resolve references
// against. Built once so validators do not each re-derive it.
type topologyView struct {
	sites                  map[string]bool
	buildingKeys           map[string]bool
	buildingsByKey         map[string]buildingmodels.Building
	buildingsByLayoutID    map[int64]buildingmodels.Building
	buildingsByCapacityKey map[string]buildingmodels.Building
	racksByLabel           map[string]rackSnapshot
}

type resolvedPlan struct {
	mode       pb.OmissionMode
	sites      []*resolvedSite
	buildings  []*resolvedBuilding
	racks      []*resolvedRack
	miners     []*resolvedMiner
	population minerPopulation
	topology   *topologyView
	// target is the omission-scoped snapshot whose existing entities reference
	// resolution and existing-topology reconciliation run against.
	target *snapshot

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

	// Reference targets resolve against the desired end-state topology: CSV rows
	// merged over the omission-target snapshot (which drops omitted topology under
	// remove-omitted). Action classification and prevName below use the full snap.
	target := snapshotForOmissionMode(snap, mode)
	plan.target = target
	plan.topology = resolveTopologyView(parsed, target)

	plan.sites, plan.errors = resolveSites(parsed.sections["SITE"], sitesByID)
	sitesByNode := indexSitesByIdentity(plan.sites)

	var buildingErrs []*pb.ImportValidationError
	plan.buildings, buildingErrs = resolveBuildings(parsed.sections["BUILDING"], buildingsByID, sitesByName, sitesByNode)
	plan.errors = append(plan.errors, buildingErrs...)

	var rackErrs []*pb.ImportValidationError
	plan.racks, rackErrs = resolveRacks(parsed.sections["RACK"], racksByID)
	plan.errors = append(plan.errors, rackErrs...)

	plan.miners = resolveMiners(parsed.sections["MINER"], minerMap(snap.miners))
	classifyMinerMoves(plan.miners, plan.topology)

	classifyTopologyActions(plan, snap, mode)
	plan.omissions = computeOmissions(parsed, snap, mode)

	return plan
}

func resolveTopologyView(parsed *parsedCSV, target *snapshot) *topologyView {
	siteRows := parsed.sections["SITE"]
	buildingRows := parsed.sections["BUILDING"]
	rackRows := parsed.sections["RACK"]
	tv := &topologyView{
		sites:                  desiredSiteSet(siteRows, target.sites),
		buildingKeys:           rowSetFromDesiredBuildings(buildingRows, target.buildings),
		buildingsByKey:         desiredBuildingMap(buildingRows, target.buildings),
		buildingsByLayoutID:    desiredBuildingLayoutIDMap(buildingRows, target.buildings),
		buildingsByCapacityKey: desiredBuildingCapacityMap(buildingRows, target.buildings),
		racksByLabel:           desiredRackMap(rackRows, target.racks, buildingRows, target.buildings),
	}
	return tv
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
// The site reference cell has already been canonicalized to a site name by
// resolveReferences; a blank or unknown name keeps the raw label with no linked
// node.
func resolveBuildings(
	rows []map[string]string,
	existingByID map[int64]buildingmodels.Building,
	sitesByName map[string]sitemodels.Site,
	sitesByNode map[string]*resolvedSite,
) ([]*resolvedBuilding, []*pb.ImportValidationError) {
	out := make([]*resolvedBuilding, 0, len(rows))
	for i, row := range rows {
		rn := rowNumber(row, i+1)
		node := &resolvedBuilding{
			name:          buildingSectionName(row),
			siteRef:       row[fieldSite],
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
		node.siteLabel = row[fieldSite]
		node.site = linkSite(row[fieldSite], sitesByName, sitesByNode)
		out = append(out, node)
	}
	return out, nil
}

// resolveRacks resolves RACK rows. The building and site reference cells have
// already been canonicalized to names by resolveReferences (a building reference
// also fills the site), so this just records the resolved labels.
func resolveRacks(
	rows []map[string]string,
	existingByID map[int64]rackSnapshot,
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
			siteRef:         row[fieldSite],
			buildingRef:     row[fieldBuilding],
			buildingID:      refID(row, refBuildingIDCell),
			rowNum:          rn,
		}
		if id, ok := rowID(row); ok {
			node.id = int64Ptr(id)
			if existing, found := existingByID[id]; found {
				node.prevLabel = existing.Label
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
			buildingID:   refID(row, refBuildingIDCell),
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

// classifyMinerMoves flags existing miners whose desired placement (site,
// building, rack, slot, or placement identity) differs from their live state.
func classifyMinerMoves(miners []*resolvedMiner, tv *topologyView) {
	for _, m := range miners {
		if m.existing == nil {
			continue
		}
		desiredSite, desiredBuilding := minerDesiredSiteBuilding(m, tv)
		buildingChanged := desiredSite != m.existing.Site || desiredBuilding != m.existing.Building
		// Two buildings can share a (site, name) pair, so a move between them shows
		// no name change; the resolved building id disambiguates them.
		if m.buildingID != nil && m.existing.BuildingID != nil && *m.buildingID != *m.existing.BuildingID {
			buildingChanged = true
		}
		if buildingChanged ||
			m.rackLabel != m.existing.Rack ||
			m.rackRow != m.existing.RackRow ||
			m.rackCol != m.existing.RackCol {
			m.moved = true
		}
	}
}

// minerDesiredSiteBuilding resolves the site and building a miner row targets. A
// rack reference dictates both; otherwise the row's canonicalized site/building
// names are authoritative (resolveReferences already filled the site from a
// building reference).
func minerDesiredSiteBuilding(m *resolvedMiner, tv *topologyView) (string, string) {
	if m.rackLabel != "" {
		rack := tv.racksByLabel[m.rackLabel]
		return rack.Site, rack.Building
	}
	return m.siteLabel, m.buildLabel
}

// validateMinerRenamePermission rejects renaming an existing miner without the
// miner rename permission.
func validateMinerRenamePermission(miners []*resolvedMiner) []*pb.ImportValidationError {
	var errs []*pb.ImportValidationError
	for _, m := range miners {
		if m.renamed {
			errs = append(errs, csvErr(m.rowNum, "MINER", "miner:rename permission is required to change miner name"))
		}
	}
	return errs
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

// validateSlotCollisions rejects two miners claiming the same rack slot in one import.
func validateSlotCollisions(miners []*resolvedMiner) []*pb.ImportValidationError {
	seen := map[string]bool{}
	var errs []*pb.ImportValidationError
	for _, m := range miners {
		key, ok := normalizedRackSlotKey(m.rackLabel, m.rackRow, m.rackCol)
		if !ok {
			continue
		}
		if seen[key] {
			errs = append(errs, csvErr(m.rowNum, "MINER", "duplicate rack slot"))
		}
		seen[key] = true
	}
	return errs
}

// validateBuildingSiteTargets rejects building rows whose site reference is not
// part of the desired topology.
func validateBuildingSiteTargets(buildings []*resolvedBuilding, tv *topologyView) []*pb.ImportValidationError {
	var errs []*pb.ImportValidationError
	for _, b := range buildings {
		if b.siteRef != "" && !tv.sites[b.siteRef] {
			errs = append(errs, csvErr(b.rowNum, "BUILDING", fmt.Sprintf("unknown site %q", b.siteRef)))
		}
	}
	return errs
}

// validateRackPlacementTargets rejects rack rows whose site or building
// reference cannot be resolved in the desired topology.
func validateRackPlacementTargets(racks []*resolvedRack, tv *topologyView) []*pb.ImportValidationError {
	var errs []*pb.ImportValidationError
	for _, r := range racks {
		if r.siteRef != "" && !tv.sites[r.siteRef] {
			errs = append(errs, csvErr(r.rowNum, "RACK", fmt.Sprintf("unknown site %q", r.siteRef)))
		}
		if r.buildingRef == "" {
			continue
		}
		if !tv.buildingKeys[r.siteRef+"\x00"+r.buildingRef] {
			errs = append(errs, csvErr(r.rowNum, "RACK", fmt.Sprintf("unknown building %q for site %q", r.buildingRef, r.siteRef)))
		}
	}
	return errs
}

// validatePlacementConsistency rejects miner rows whose declared site, building,
// and rack references disagree with one another or with the desired topology.
func validatePlacementConsistency(miners []*resolvedMiner, tv *topologyView) []*pb.ImportValidationError {
	var errs []*pb.ImportValidationError
	for _, m := range miners {
		if m.rackLabel != "" {
			rack, ok := tv.racksByLabel[m.rackLabel]
			if !ok {
				errs = append(errs, csvErr(m.rowNum, "MINER", fmt.Sprintf("unknown rack %q", m.rackLabel)))
				continue
			}
			if m.siteLabel != "" && m.siteLabel != rack.Site {
				errs = append(errs, csvErr(m.rowNum, "MINER", fmt.Sprintf("miner site %q does not match rack site %q", m.siteLabel, rack.Site)))
			}
			if m.buildLabel != "" && m.buildLabel != rack.Building {
				errs = append(errs, csvErr(m.rowNum, "MINER", fmt.Sprintf("miner building %q does not match rack building %q", m.buildLabel, rack.Building)))
			}
			continue
		}
		if m.buildLabel != "" {
			if _, ok := tv.buildingsByKey[m.siteLabel+"\x00"+m.buildLabel]; !ok {
				errs = append(errs, csvErr(m.rowNum, "MINER", fmt.Sprintf("unknown building %q for site %q", m.buildLabel, m.siteLabel)))
			}
			continue
		}
		if m.siteLabel != "" && !tv.sites[m.siteLabel] {
			errs = append(errs, csvErr(m.rowNum, "MINER", fmt.Sprintf("unknown site %q", m.siteLabel)))
		}
	}
	return errs
}

// validateRackGridPositions rejects rack aisle/position coordinates that are
// malformed or out of bounds for their building's grid.
func validateRackGridPositions(racks []*resolvedRack, tv *topologyView) []*pb.ImportValidationError {
	var errs []*pb.ImportValidationError
	for _, r := range racks {
		if (r.aisleIndex == "") != (r.positionInAisle == "") {
			errs = append(errs, csvErr(r.rowNum, "RACK", "aisle_index and position_in_aisle must both be set or both be blank"))
			continue
		}
		if r.aisleIndex == "" {
			continue
		}
		if r.buildingRef == "" {
			errs = append(errs, csvErr(r.rowNum, "RACK", "rack grid position requires a building"))
			continue
		}
		aisle, err := parseInt32Value(r.aisleIndex, "aisle_index")
		if err != nil {
			errs = append(errs, csvErr(r.rowNum, "RACK", err.Error()))
			continue
		}
		position, err := parseInt32Value(r.positionInAisle, "position_in_aisle")
		if err != nil {
			errs = append(errs, csvErr(r.rowNum, "RACK", err.Error()))
			continue
		}
		if aisle < 0 {
			errs = append(errs, csvErr(r.rowNum, "RACK", fmt.Sprintf("aisle_index %d is out of bounds", aisle)))
			continue
		}
		if position < 0 {
			errs = append(errs, csvErr(r.rowNum, "RACK", fmt.Sprintf("position_in_aisle %d is out of bounds", position)))
			continue
		}
		building, ok := rackGridBuilding(r, tv)
		if !ok {
			continue
		}
		if building.Aisles <= 0 || aisle >= building.Aisles {
			errs = append(errs, csvErr(r.rowNum, "RACK", fmt.Sprintf("aisle_index %d is out of bounds for building %q with %d aisles", aisle, building.Name, building.Aisles)))
		}
		if building.RacksPerAisle <= 0 || position >= building.RacksPerAisle {
			errs = append(errs, csvErr(r.rowNum, "RACK", fmt.Sprintf("position_in_aisle %d is out of bounds for building %q with %d racks per aisle", position, building.Name, building.RacksPerAisle)))
		}
	}
	return errs
}

func rackGridBuilding(r *resolvedRack, tv *topologyView) (buildingmodels.Building, bool) {
	// Prefer the resolved building id so two buildings sharing a (site, name) pair
	// keep independent grids; fall back to the (site, name) key for creates.
	if r.buildingID != nil {
		if building, ok := tv.buildingsByLayoutID[*r.buildingID]; ok {
			return building, true
		}
	}
	building, ok := tv.buildingsByKey[r.siteRef+"\x00"+r.buildingRef]
	return building, ok
}

// validateRackSlotBounds rejects miner rack_row/rack_col coordinates that are
// malformed or out of bounds for their rack's dimensions.
func validateRackSlotBounds(miners []*resolvedMiner, tv *topologyView) []*pb.ImportValidationError {
	var errs []*pb.ImportValidationError
	for _, m := range miners {
		if m.rackLabel == "" {
			if m.rackRow != "" || m.rackCol != "" {
				errs = append(errs, csvErr(m.rowNum, "MINER", "rack_row and rack_col require rack"))
			}
			continue
		}
		if (m.rackRow == "") != (m.rackCol == "") {
			errs = append(errs, csvErr(m.rowNum, "MINER", "rack_row and rack_col must both be set or both be blank"))
			continue
		}
		if m.rackRow == "" {
			continue
		}
		rack, ok := tv.racksByLabel[m.rackLabel]
		if !ok {
			continue
		}
		rackRow, err := parseInt32Value(m.rackRow, "rack_row")
		if err != nil {
			errs = append(errs, csvErr(m.rowNum, "MINER", err.Error()))
			continue
		}
		rackCol, err := parseInt32Value(m.rackCol, "rack_col")
		if err != nil {
			errs = append(errs, csvErr(m.rowNum, "MINER", err.Error()))
			continue
		}
		if rackRow < 0 || rack.Rows <= 0 || rackRow >= rack.Rows {
			errs = append(errs, csvErr(m.rowNum, "MINER", fmt.Sprintf("rack_row %d is out of bounds for rack %q with %d rows", rackRow, m.rackLabel, rack.Rows)))
		}
		if rackCol < 0 || rack.Columns <= 0 || rackCol >= rack.Columns {
			errs = append(errs, csvErr(m.rowNum, "MINER", fmt.Sprintf("rack_col %d is out of bounds for rack %q with %d columns", rackCol, m.rackLabel, rack.Columns)))
		}
	}
	return errs
}

// rackLabelsByID maps a resolved rack's stable id to its desired label, so an
// existing miner referencing a renamed rack by id resolves to the new label.
func rackLabelsByID(racks []*resolvedRack) map[int64]string {
	out := map[int64]string{}
	for _, r := range racks {
		if r.id != nil {
			out[*r.id] = r.label
		}
	}
	return out
}

// validateExistingSlotsFitRackDimensions rejects an import that would shrink a
// rack below the slot coordinates its current or incoming miners occupy.
func validateExistingSlotsFitRackDimensions(r *resolvedPlan) []*pb.ImportValidationError {
	racks := r.topology.racksByLabel
	rackLabels := rackLabelsByID(r.racks)
	desired := map[string]*resolvedMiner{}
	for _, m := range r.miners {
		desired[m.deviceID] = m
	}
	var errs []*pb.ImportValidationError
	fits := func(deviceID, rackLabel, rackRow, rackCol string) {
		if rackLabel == "" || rackRow == "" || rackCol == "" {
			return
		}
		rack, ok := racks[rackLabel]
		if !ok {
			return
		}
		rowValue, err := parseInt32Value(rackRow, "rack_row")
		if err != nil {
			return
		}
		colValue, err := parseInt32Value(rackCol, "rack_col")
		if err != nil {
			return
		}
		if rowValue >= rack.Rows || colValue >= rack.Columns {
			errs = append(errs, csvErr(0, "MINER", fmt.Sprintf("miner %s slot %d,%d does not fit rack %q dimensions %dx%d", deviceID, rowValue, colValue, rackLabel, rack.Rows, rack.Columns)))
		}
	}
	for _, miner := range r.population.miners {
		m, ok := desired[miner.DeviceIdentifier]
		if !ok && r.mode == pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
			continue
		}
		if ok {
			fits(miner.DeviceIdentifier, m.rackLabel, m.rackRow, m.rackCol)
			continue
		}
		fits(miner.DeviceIdentifier, desiredRackLabel(miner, rackLabels), miner.RackRow, miner.RackCol)
	}
	for _, miner := range r.population.hiddenRackMembers {
		fits(miner.DeviceIdentifier, desiredRackLabel(miner, rackLabels), miner.RackRow, miner.RackCol)
	}
	return errs
}

// validateRackCapacity rejects racks whose desired assigned-miner count exceeds
// their slot capacity, counting both incoming and retained existing miners.
func validateRackCapacity(r *resolvedPlan) []*pb.ImportValidationError {
	racks := r.topology.racksByLabel
	rackLabels := rackLabelsByID(r.racks)
	counts := map[string]int32{}
	presentMiners := map[string]bool{}
	for _, m := range r.miners {
		if m.deviceID != "" {
			presentMiners[m.deviceID] = true
		}
		if m.rackLabel != "" {
			counts[m.rackLabel]++
		}
	}
	if r.mode != pb.OmissionMode_OMISSION_MODE_REMOVE_OMITTED {
		for _, miner := range r.population.miners {
			if miner.Rack != "" && !presentMiners[miner.DeviceIdentifier] {
				counts[desiredRackLabel(miner, rackLabels)]++
			}
		}
	}
	for _, miner := range r.population.hiddenRackMembers {
		if miner.Rack != "" && !presentMiners[miner.DeviceIdentifier] {
			counts[desiredRackLabel(miner, rackLabels)]++
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

// validateBuildingRackCapacity rejects buildings whose desired assigned-rack
// count exceeds their grid capacity.
func validateBuildingRackCapacity(r *resolvedPlan) []*pb.ImportValidationError {
	counts := map[string]int32{}
	for _, rack := range r.topology.racksByLabel {
		if key, ok := rackBuildingCapacityKey(rack); ok {
			counts[key]++
		}
	}
	var errs []*pb.ImportValidationError
	for key, count := range counts {
		building, ok := r.topology.buildingsByCapacityKey[key]
		if !ok {
			continue
		}
		if buildingmodels.RackCapacityExceeded(building.Aisles, building.RacksPerAisle, int64(count)) {
			errs = append(errs, csvErr(0, "RACK", fmt.Sprintf("building %q has %d assigned racks but capacity is %d", building.Name, count, buildingmodels.GridCapacity(building.Aisles, building.RacksPerAisle))))
		}
	}
	return errs
}

// validateBuildingExistingRacksFitLayout rejects an import that would shrink a
// building's grid below the aisle/position an existing rack occupies.
func validateBuildingExistingRacksFitLayout(r *resolvedPlan) []*pb.ImportValidationError {
	desiredRacks := r.topology.racksByLabel
	desiredRacksByID := map[int64]rackSnapshot{}
	for _, rack := range desiredRacks {
		if rack.ID > 0 {
			desiredRacksByID[rack.ID] = rack
		}
	}
	var errs []*pb.ImportValidationError
	for _, rack := range r.target.racks {
		desiredRack, ok := desiredRacksByID[rack.ID]
		if !ok {
			desiredRack, ok = desiredRacks[rack.Label]
		}
		if !ok {
			desiredRack = rack
		}
		if desiredRack.Building == "" || desiredRack.AisleIndex == "" || desiredRack.PositionInAisle == "" {
			continue
		}
		var building buildingmodels.Building
		if desiredRack.BuildingID != nil {
			building, ok = r.topology.buildingsByLayoutID[*desiredRack.BuildingID]
		} else {
			building, ok = r.topology.buildingsByKey[desiredRack.Site+"\x00"+desiredRack.Building]
		}
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

// validateSlotConflictsWithExisting rejects incoming miner placements that land
// on a rack slot occupied by an existing miner that is not itself moving away.
func validateSlotConflictsWithExisting(r *resolvedPlan) []*pb.ImportValidationError {
	rackLabels := rackLabelsByID(r.racks)
	desired := map[string]*resolvedMiner{}
	for _, m := range r.miners {
		desired[m.deviceID] = m
	}
	movingMiners := map[string]bool{}
	for _, miner := range r.population.miners {
		m, ok := desired[miner.DeviceIdentifier]
		if !ok {
			continue
		}
		currentRack := desiredRackLabel(miner, rackLabels)
		currentKey, _ := normalizedRackSlotKey(currentRack, miner.RackRow, miner.RackCol)
		desiredKey, _ := normalizedRackSlotKey(m.rackLabel, m.rackRow, m.rackCol)
		if m.rackLabel != currentRack || desiredKey != currentKey {
			movingMiners[miner.DeviceIdentifier] = true
		}
	}

	currentOccupants := map[string]minerSnapshot{}
	for _, miner := range r.population.miners {
		key, ok := normalizedRackSlotKey(desiredRackLabel(miner, rackLabels), miner.RackRow, miner.RackCol)
		if !ok {
			continue
		}
		currentOccupants[key] = miner
	}
	for _, miner := range r.population.hiddenRackMembers {
		key, ok := normalizedRackSlotKey(desiredRackLabel(miner, rackLabels), miner.RackRow, miner.RackCol)
		if !ok {
			continue
		}
		currentOccupants[key] = miner
	}

	var errs []*pb.ImportValidationError
	for _, m := range r.miners {
		key, ok := normalizedRackSlotKey(m.rackLabel, m.rackRow, m.rackCol)
		if !ok {
			continue
		}
		occupant, ok := currentOccupants[key]
		if !ok || occupant.DeviceIdentifier == m.deviceID || movingMiners[occupant.DeviceIdentifier] {
			continue
		}
		errs = append(errs, csvErr(m.rowNum, "MINER", fmt.Sprintf("rack slot already occupied by miner %s", occupant.DeviceIdentifier)))
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

// countMinerMoveNodes counts existing miners whose placement changed.
func countMinerMoveNodes(miners []*resolvedMiner) int32 {
	var n int32
	for _, m := range miners {
		if m.moved {
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
	add(pb.ImportOperation_IMPORT_OPERATION_UPDATE, "rack", countRackUpdates(parsed.sections["RACK"], snap.racks), "rack rows with changed details")
	add(pb.ImportOperation_IMPORT_OPERATION_RENAME, "miner", countMinerRenameNodes(resolved.miners), "miner rows with changed names")
	add(pb.ImportOperation_IMPORT_OPERATION_MOVE, "miner", countMinerMoveNodes(resolved.miners), "miner placement rows with changed site, building, rack, or slot")
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
