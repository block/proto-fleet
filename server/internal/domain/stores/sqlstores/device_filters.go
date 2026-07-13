package sqlstores

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// minerFilterParams holds the parsed filter parameters for miner queries.
type minerFilterParams struct {
	statusFilter              sql.NullString
	statusValues              []string
	modelFilter               sql.NullString
	modelValues               []string
	manufacturerFilter        sql.NullString
	manufacturerValues        []string
	pairingStatusFilter       sql.NullString
	pairingStatusValues       []string
	needsAttentionFilter      bool
	includeNullStatus         bool
	errorComponentTypesFilter sql.NullString
	errorComponentTypeValues  []int32
	deviceIdentifiersFilter   sql.NullString
	deviceIdentifierValues    []string
	groupIDsFilter            sql.NullString
	groupIDValues             []int64
	rackIDsFilter             sql.NullString
	rackIDValues              []int64
	firmwareVersionsFilter    sql.NullString
	firmwareVersionValues     []string
	// Site filter: site_ids OR (site_id IS NULL when includeUnassigned).
	siteIDsFilter     sql.NullString
	siteIDValues      []int64
	includeUnassigned bool
	// Building filter: building_ids match direct device.building_id and
	// rack-derived placement. includeNoBuilding matches rack rows with
	// no building. includeNoRack widens to devices with no rack
	// membership at all.
	buildingIDsFilter sql.NullString
	buildingIDValues  []int64
	includeNoBuilding bool
	includeNoRack     bool
	// Zone keys: scoped tuples emit an UNNEST + tuple-IN branch;
	// wildcards (building_id == 0) emit a zone = ANY branch. Either
	// branch can be empty.
	zoneKeysFilter    sql.NullString
	scopedBuildingIDs []int64
	scopedZones       []string
	wildcardZones     []string
	// numericRanges drives both the WHERE predicates emitted by
	// appendFilterSQL and the CTE/JOIN gating in buildListQuerySQL: when
	// non-empty, latest_metrics is INNER-joined and OFFLINE miners are
	// excluded so results match how the UI renders telemetry cells.
	numericRanges []stores.NumericRange
	ipCIDRsFilter sql.NullString
	// ipCIDRValues are pre-stringified prefixes (already normalized by
	// parseFilter) suitable for pq.Array on a $N::cidr[] parameter.
	ipCIDRValues []string
	// ipRangeStarts/ipRangeEnds are pre-stringified inclusive range bounds
	// (index-aligned), each pair emitting an `ip BETWEEN start AND end`
	// branch OR'd into the same subnet group as ipCIDRValues.
	ipRangeStarts []string
	ipRangeEnds   []string
	// limit, when > 0, becomes a SQL-level `LIMIT N` on the device-id
	// query. Threaded through from MinerFilter.Limit so the stats RPCs
	// can fail-fast on oversize fleets without first materializing every
	// matching identifier.
	limit int
}

// buildMinerFilterParams converts a MinerFilter to SQL-ready parameters.
func buildMinerFilterParams(filter *stores.MinerFilter) minerFilterParams {
	var fp minerFilterParams

	if filter == nil {
		return fp
	}

	// Status filter
	if len(filter.DeviceStatusFilter) > 0 {
		fp.statusFilter = sql.NullString{Valid: true}
		for _, status := range filter.DeviceStatusFilter {
			fp.statusValues = append(fp.statusValues, string(toDeviceStatus(status)))
			if status == minermodels.MinerStatusError {
				fp.needsAttentionFilter = true
			}
			if status == minermodels.MinerStatusOffline {
				fp.includeNullStatus = true
			}
		}
	}

	// Model filter
	if len(filter.ModelNames) > 0 {
		fp.modelFilter = sql.NullString{Valid: true}
		fp.modelValues = filter.ModelNames
	}

	// Manufacturer filter
	if len(filter.ManufacturerNames) > 0 {
		fp.manufacturerFilter = sql.NullString{Valid: true}
		fp.manufacturerValues = filter.ManufacturerNames
	}

	// Pairing status filter
	if len(filter.PairingStatuses) > 0 {
		fp.pairingStatusFilter = sql.NullString{Valid: true}
		for _, status := range filter.PairingStatuses {
			if status == fm.PairingStatus_PAIRING_STATUS_UNSPECIFIED {
				continue
			}
			fp.pairingStatusValues = append(fp.pairingStatusValues, string(ProtoPairingStatusToSQL(status)))
		}
		// If no valid statuses, clear the filter
		if len(fp.pairingStatusValues) == 0 {
			fp.pairingStatusFilter = sql.NullString{}
		}
	}

	// Component error type filter
	if len(filter.ErrorComponentTypes) > 0 {
		fp.errorComponentTypesFilter = sql.NullString{Valid: true}
		fp.errorComponentTypeValues = make([]int32, len(filter.ErrorComponentTypes))
		for i, ct := range filter.ErrorComponentTypes {
			// #nosec G115 -- ComponentType enum bounded (0-6), safe for int32
			fp.errorComponentTypeValues[i] = int32(ct)
		}
	}

	// Device identifiers filter
	if len(filter.DeviceIdentifiers) > 0 {
		fp.deviceIdentifiersFilter = sql.NullString{Valid: true}
		fp.deviceIdentifierValues = filter.DeviceIdentifiers
	}

	// Group ID filter
	if len(filter.GroupIDs) > 0 {
		fp.groupIDsFilter = sql.NullString{Valid: true}
		fp.groupIDValues = filter.GroupIDs
	}

	// Rack ID filter
	if len(filter.RackIDs) > 0 {
		fp.rackIDsFilter = sql.NullString{Valid: true}
		fp.rackIDValues = filter.RackIDs
	}

	// Firmware version filter
	if len(filter.FirmwareVersions) > 0 {
		fp.firmwareVersionsFilter = sql.NullString{Valid: true}
		fp.firmwareVersionValues = filter.FirmwareVersions
	}

	if len(filter.SiteIDs) > 0 {
		fp.siteIDsFilter = sql.NullString{Valid: true}
		fp.siteIDValues = filter.SiteIDs
	}
	fp.includeUnassigned = filter.IncludeUnassigned

	if len(filter.BuildingIDs) > 0 {
		fp.buildingIDsFilter = sql.NullString{Valid: true}
		fp.buildingIDValues = filter.BuildingIDs
	}
	fp.includeNoBuilding = filter.IncludeNoBuilding
	fp.includeNoRack = filter.IncludeNoRack

	if len(filter.ZoneKeys) > 0 {
		fp.zoneKeysFilter = sql.NullString{Valid: true}
		for _, zk := range filter.ZoneKeys {
			if zk.BuildingID == 0 {
				fp.wildcardZones = append(fp.wildcardZones, zk.Zone)
			} else {
				fp.scopedBuildingIDs = append(fp.scopedBuildingIDs, zk.BuildingID)
				fp.scopedZones = append(fp.scopedZones, zk.Zone)
			}
		}
	}

	// Numeric range filters (telemetry predicates).
	if len(filter.NumericRanges) > 0 {
		fp.numericRanges = filter.NumericRanges
	}

	// IP CIDR filter — pre-stringify so appendFilterSQL hands one pq.Array
	// into the cidr[] cast regardless of how many prefixes were supplied.
	if len(filter.IPCIDRs) > 0 {
		fp.ipCIDRsFilter = sql.NullString{Valid: true}
		fp.ipCIDRValues = make([]string, len(filter.IPCIDRs))
		for i, p := range filter.IPCIDRs {
			fp.ipCIDRValues[i] = p.String()
		}
	}

	// IP range filter — pre-stringify bounds so each range emits a
	// `ip BETWEEN $start::inet AND $end::inet` branch.
	if len(filter.IPRanges) > 0 {
		fp.ipRangeStarts = make([]string, len(filter.IPRanges))
		fp.ipRangeEnds = make([]string, len(filter.IPRanges))
		for i, r := range filter.IPRanges {
			fp.ipRangeStarts[i] = r.Start.String()
			fp.ipRangeEnds[i] = r.End.String()
		}
	}

	if filter.Limit > 0 {
		fp.limit = filter.Limit
	}

	return fp
}

// numericFieldColumn returns the SQL expression that yields a value in the
// same display units the corresponding Measurement is emitted in by other
// telemetry APIs. The column→display conversions mirror
// telemetry/models/units.go: hashrate H/s ÷ 1e12 → TH/s, power W ÷ 1e3 → kW,
// efficiency J/H × 1e12 → J/TH; temperature/voltage/current pass through.
func numericFieldColumn(f stores.NumericFilterField) string {
	//nolint:exhaustive // Unspecified is filtered out by parseFilter; treat as unknown.
	switch f {
	case stores.NumericFilterFieldHashrateTHs:
		return "latest_metrics.hash_rate_hs / 1e12"
	case stores.NumericFilterFieldEfficiencyJTH:
		return "latest_metrics.efficiency_jh * 1e12"
	case stores.NumericFilterFieldPowerKW:
		return "latest_metrics.power_w / 1e3"
	case stores.NumericFilterFieldTemperatureC:
		return "latest_metrics.temp_c"
	case stores.NumericFilterFieldVoltageV:
		return "latest_metrics.voltage_v"
	case stores.NumericFilterFieldCurrentA:
		return "latest_metrics.current_a"
	}
	return ""
}

// appendFilterSQL appends filter conditions to the query builder and returns updated args.
func appendFilterSQL(sb *strings.Builder, args []any, argNum int, orgID int64, fp minerFilterParams) ([]any, int) {
	if fp.pairingStatusFilter.Valid {
		fmt.Fprintf(sb, " AND (%s = ANY($%d::text[]))", pairingStatusExpr, argNum)
		args = append(args, pq.Array(fp.pairingStatusValues))
		argNum++
	}

	if fp.modelFilter.Valid {
		fmt.Fprintf(sb, " AND discovered_device.model = ANY($%d::text[])", argNum)
		args = append(args, pq.Array(fp.modelValues))
		argNum++
	}

	if fp.manufacturerFilter.Valid {
		fmt.Fprintf(sb, " AND discovered_device.manufacturer = ANY($%d::text[])", argNum)
		args = append(args, pq.Array(fp.manufacturerValues))
		argNum++
	}

	if fp.deviceIdentifiersFilter.Valid {
		fmt.Fprintf(sb, " AND device.device_identifier = ANY($%d::text[])", argNum)
		args = append(args, pq.Array(fp.deviceIdentifierValues))
		argNum++
	}

	if fp.statusFilter.Valid {
		// Start outer AND group for status filter with optional needs attention
		fmt.Fprintf(sb,
			" AND ((device_status.status::text = ANY($%d::text[])"+
				" AND (device_status.status IN %s"+
				" OR (device_status.status = 'ACTIVE' AND NOT EXISTS ("+
				"SELECT 1 FROM errors WHERE errors.device_id = device.id"+
				" AND errors.org_id = $%d AND errors.closed_at IS NULL AND %s))",
			argNum, nonActionableStatuses, argNum+1, actionableErrorSeverities)
		args = append(args, pq.Array(fp.statusValues), orgID)
		argNum += 2

		if fp.needsAttentionFilter {
			// OR TRUE makes status filter match all requested statuses (including ERROR)
			sb.WriteString(" OR TRUE")
		}
		// Close inner AND condition and status match group
		sb.WriteString("))")

		if fp.needsAttentionFilter {
			// Auth-needed (exclude OFFLINE only)
			sb.WriteString(
				" OR (device_pairing.pairing_status IN ('AUTHENTICATION_NEEDED')" +
					" AND (device_status.status IS NULL OR device_status.status != 'OFFLINE'))")
			// Devices with actionable errors. Excludes NULL-status paired-like miners
			// so they stay bucketed as offline (matches CountMinersByState).
			fmt.Fprintf(sb,
				" OR (EXISTS (SELECT 1 FROM errors WHERE errors.device_id = device.id"+
					" AND errors.org_id = $%d AND errors.closed_at IS NULL AND %s)"+
					" AND NOT (device_status.status IS NULL AND device_pairing.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD'))"+
					" AND (device_status.status IS NULL OR device_status.status NOT IN %s))",
				argNum, actionableErrorSeverities, nonActionableStatuses)
			args = append(args, orgID)
			argNum++
		}
		if fp.includeNullStatus {
			// NULL-status paired-like miners (counted as offline in dashboard).
			// Scoped to PAIRED/DEFAULT_PASSWORD to match CountMinersByState's WHERE clause.
			sb.WriteString(
				" OR (device_status.status IS NULL" +
					" AND device_pairing.pairing_status IN ('PAIRED', 'DEFAULT_PASSWORD'))")
		}
		// Close outer AND group
		sb.WriteString(")")
	}

	if fp.errorComponentTypesFilter.Valid {
		fmt.Fprintf(sb,
			" AND EXISTS (SELECT 1 FROM errors WHERE errors.device_id = device.id"+
				" AND errors.closed_at IS NULL AND errors.component_type = ANY($%d::int[]))",
			argNum)
		args = append(args, pq.Array(fp.errorComponentTypeValues))
		argNum++
	}

	if fp.groupIDsFilter.Valid {
		fmt.Fprintf(sb,
			" AND EXISTS (SELECT 1 FROM device_set_membership dcm"+
				" WHERE dcm.device_id = device.id"+
				" AND dcm.org_id = $%d"+
				" AND dcm.device_set_type = 'group'"+
				" AND dcm.device_set_id = ANY($%d::bigint[]))",
			argNum, argNum+1)
		args = append(args, orgID, pq.Array(fp.groupIDValues))
		argNum += 2
	}

	// Rack membership predicate. rack_ids match an explicit rack; include_no_rack
	// admits devices with no rack membership. Emitting this as a top-level AND
	// whenever include_no_rack is set (even with no rack_ids) is what keeps the
	// assignable-only filter for a NEW rack from leaking miners that sit in
	// ANOTHER rack in the same building — their direct/rack-derived building
	// still matches the building predicate below, so "no rack" has to be
	// enforced here rather than only OR'd inside the building group.
	if fp.rackIDsFilter.Valid || fp.includeNoRack {
		sb.WriteString(" AND (")
		rackFirst := true
		if fp.rackIDsFilter.Valid {
			fmt.Fprintf(sb,
				"EXISTS (SELECT 1 FROM device_set_membership dcm"+
					" WHERE dcm.device_id = device.id"+
					" AND dcm.org_id = $%d"+
					" AND dcm.device_set_type = 'rack'"+
					" AND dcm.device_set_id = ANY($%d::bigint[]))",
				argNum, argNum+1)
			args = append(args, orgID, pq.Array(fp.rackIDValues))
			argNum += 2
			rackFirst = false
		}
		if fp.includeNoRack {
			if !rackFirst {
				sb.WriteString(" OR ")
			}
			fmt.Fprintf(sb,
				"NOT EXISTS (SELECT 1 FROM device_set_membership dcm"+
					" JOIN device_set ds ON ds.id = dcm.device_set_id"+
					" WHERE dcm.device_id = device.id"+
					" AND dcm.org_id = $%d"+
					" AND dcm.device_set_type = 'rack'"+
					" AND ds.deleted_at IS NULL)",
				argNum)
			args = append(args, orgID)
			argNum++
		}
		sb.WriteString(")")
	}

	if fp.firmwareVersionsFilter.Valid {
		fmt.Fprintf(sb, " AND discovered_device.firmware_version = ANY($%d::text[])", argNum)
		args = append(args, pq.Array(fp.firmwareVersionValues))
		argNum++
	}

	if len(fp.numericRanges) > 0 {
		for _, r := range fp.numericRanges {
			col := numericFieldColumn(r.Field)
			if col == "" {
				continue
			}
			if r.Min != nil {
				op := ">"
				if r.MinInclusive {
					op = ">="
				}
				fmt.Fprintf(sb, " AND %s %s $%d", col, op, argNum)
				args = append(args, *r.Min)
				argNum++
			}
			if r.Max != nil {
				op := "<"
				if r.MaxInclusive {
					op = "<="
				}
				fmt.Fprintf(sb, " AND %s %s $%d", col, op, argNum)
				args = append(args, *r.Max)
				argNum++
			}
		}
		// Match the UI's em-dash semantics: OFFLINE miners never expose a
		// telemetry value, so a numeric predicate should not surface them
		// even if a fresh metric exists.
		sb.WriteString(" AND (device_status.status IS NULL OR device_status.status != 'OFFLINE')")
	}

	// Subnet facet: CIDR containment and inclusive ranges are OR'd together
	// into a single group (all subnet-box lines match as one OR set) which is
	// then AND'd with the other facets.
	if fp.ipCIDRsFilter.Valid || len(fp.ipRangeStarts) > 0 {
		sb.WriteString(" AND (")
		first := true
		if fp.ipCIDRsFilter.Valid {
			fmt.Fprintf(sb,
				"discovered_device.ip_address_inet <<= ANY($%d::cidr[])",
				argNum)
			args = append(args, pq.Array(fp.ipCIDRValues))
			argNum++
			first = false
		}
		for i := range fp.ipRangeStarts {
			if !first {
				sb.WriteString(" OR ")
			}
			fmt.Fprintf(sb,
				"discovered_device.ip_address_inet BETWEEN $%d::inet AND $%d::inet",
				argNum, argNum+1)
			args = append(args, fp.ipRangeStarts[i], fp.ipRangeEnds[i])
			argNum += 2
			first = false
		}
		sb.WriteString(")")
	}

	if fp.siteIDsFilter.Valid || fp.includeUnassigned {
		sb.WriteString(" AND (")
		first := true
		if fp.siteIDsFilter.Valid {
			fmt.Fprintf(sb, "device.site_id = ANY($%d::bigint[])", argNum)
			args = append(args, pq.Array(fp.siteIDValues))
			argNum++
			first = false
		}
		if fp.includeUnassigned {
			if !first {
				sb.WriteString(" OR ")
			}
			sb.WriteString("device.site_id IS NULL")
		}
		sb.WriteString(")")
	}

	// Building filter: building_ids, include_no_building, and
	// include_no_rack are OR'd together at the top level. Explicit
	// building IDs must match both the direct device FK written by
	// AssignDevicesToBuilding and rack-derived placement from
	// device_set_rack. Every emitted rack-membership predicate carries
	// the dcm.org_id = $orgID clause — see
	// device_filters_orgid_audit_test.go.
	// When explicit rack IDs are present, include_no_rack belongs to the
	// rack bucket above (rack IDs OR no rack). Re-emitting it here would
	// turn that into "rack ID AND no rack" unless the building filter also
	// explicitly selected its unassigned bucket.
	includeNoRackInBuildingFilter := fp.includeNoRack && (!fp.rackIDsFilter.Valid || fp.includeNoBuilding)
	if fp.buildingIDsFilter.Valid || fp.includeNoBuilding || includeNoRackInBuildingFilter {
		sb.WriteString(" AND (")
		first := true
		if fp.buildingIDsFilter.Valid {
			fmt.Fprintf(sb,
				"(device.building_id = ANY($%d::bigint[])"+
					" OR EXISTS (SELECT 1 FROM device_set_membership dcm"+
					" JOIN device_set ds ON ds.id = dcm.device_set_id"+
					" JOIN device_set_rack dsr ON dsr.device_set_id = dcm.device_set_id"+
					" WHERE dcm.device_id = device.id"+
					" AND dcm.org_id = $%d"+
					" AND dcm.device_set_type = 'rack'"+
					" AND ds.deleted_at IS NULL"+
					" AND dsr.building_id = ANY($%d::bigint[])))",
				argNum+1, argNum, argNum+1)
			args = append(args, orgID, pq.Array(fp.buildingIDValues))
			argNum += 2
			first = false
		}
		// include_no_building matches miners whose RACK has no building. That
		// keeps a target rack's own members visible in the assignable-only
		// filter (their building-less rack matches). But the assignable-only
		// filter for a NEW rack carries no rack_ids and sets include_no_rack —
		// there are no members to preserve, and this branch would instead pull
		// in the members of OTHER building-less racks. So drop it for that
		// shape (include_no_rack with no explicit rack filter); the no-rack
		// branch below still admits rackless miners with no direct building.
		emitNoBuildingRackBranch := fp.includeNoBuilding && !(fp.includeNoRack && !fp.rackIDsFilter.Valid)
		if emitNoBuildingRackBranch {
			if !first {
				sb.WriteString(" OR ")
			}
			fmt.Fprintf(sb,
				"EXISTS (SELECT 1 FROM device_set_membership dcm"+
					" JOIN device_set ds ON ds.id = dcm.device_set_id"+
					" JOIN device_set_rack dsr ON dsr.device_set_id = dcm.device_set_id"+
					" WHERE dcm.device_id = device.id"+
					" AND dcm.org_id = $%d"+
					" AND dcm.device_set_type = 'rack'"+
					" AND ds.deleted_at IS NULL"+
					" AND dsr.building_id IS NULL)",
				argNum)
			args = append(args, orgID)
			argNum++
			first = false
		}
		if includeNoRackInBuildingFilter {
			if !first {
				sb.WriteString(" OR ")
			}
			// The no-rack branch keeps genuinely-unplaced miners in the
			// building group (they own no device_set_rack row, so the
			// building_ids / include_no_building branches above can never
			// match them). When the caller also constrains building
			// (explicit IDs or include_no_building — e.g. the assignable-only
			// miner-selection filter), a rackless miner should only pass if
			// it is ALSO unplaced at the building level: a rackless miner
			// with a direct device.building_id in another building is
			// placed elsewhere, not assignable. Without this, that miner
			// leaks into the default assignable-only list (issue #702).
			// When no building predicate is present (e.g. the fleet
			// "Unassigned" rack bucket with no building facet), this branch
			// is the sole no-rack predicate and must stay unqualified so
			// rackless-but-building-placed miners still surface.
			buildingConstrained := fp.buildingIDsFilter.Valid || fp.includeNoBuilding
			if buildingConstrained {
				sb.WriteString("(device.building_id IS NULL AND ")
			}
			fmt.Fprintf(sb,
				"NOT EXISTS (SELECT 1 FROM device_set_membership dcm"+
					" JOIN device_set ds ON ds.id = dcm.device_set_id"+
					" WHERE dcm.device_id = device.id"+
					" AND dcm.org_id = $%d"+
					" AND dcm.device_set_type = 'rack'"+
					" AND ds.deleted_at IS NULL)",
				argNum)
			if buildingConstrained {
				sb.WriteString(")")
			}
			args = append(args, orgID)
			argNum++
		}
		sb.WriteString(")")
	}

	// Zone keys: scoped (building_id > 0) tuples join via UNNEST + tuple
	// IN; wildcards (building_id == 0) match zone label across any
	// building. Branches OR'd inside one EXISTS so org_id is enforced
	// once. See device_filters_orgid_audit_test.go for the
	// single-layer-defense audit on the wildcard path.
	//
	// The OR'd predicate body is shared with the rack-list filter
	// (collection_sort.go) via appendZoneKeyPredicate. Caller emits
	// the EXISTS framing + dcm/ds/dsr joins + org_id clause and the
	// helper fills the (dsr.building_id, dsr.zone) tuple / dsr.zone
	// = ANY branches.
	if fp.zoneKeysFilter.Valid {
		fmt.Fprintf(sb,
			" AND EXISTS (SELECT 1 FROM device_set_membership dcm"+
				" JOIN device_set ds ON ds.id = dcm.device_set_id"+
				" JOIN device_set_rack dsr ON dsr.device_set_id = dcm.device_set_id"+
				" WHERE dcm.device_id = device.id"+
				" AND dcm.org_id = $%d"+
				" AND dcm.device_set_type = 'rack'"+
				" AND ds.deleted_at IS NULL"+
				" AND (",
			argNum)
		args = append(args, orgID)
		argNum++

		keys := make([]stores.ZoneKey, 0, len(fp.scopedBuildingIDs)+len(fp.wildcardZones))
		for i, b := range fp.scopedBuildingIDs {
			keys = append(keys, stores.ZoneKey{BuildingID: b, Zone: fp.scopedZones[i]})
		}
		for _, z := range fp.wildcardZones {
			keys = append(keys, stores.ZoneKey{BuildingID: 0, Zone: z})
		}
		args, argNum = appendZoneKeyPredicate(sb, args, argNum, "dsr", keys)
		sb.WriteString("))")
	}

	return args, argNum
}
