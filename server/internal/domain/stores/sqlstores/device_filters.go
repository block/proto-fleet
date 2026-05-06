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
	zonesFilter               sql.NullString
	zoneValues                []string
	// numericRanges drives both the WHERE predicates emitted by
	// appendFilterSQL and the CTE/JOIN gating in buildListQuerySQL: when
	// non-empty, latest_metrics is INNER-joined and OFFLINE miners are
	// excluded so results match how the UI renders telemetry cells.
	numericRanges []stores.NumericRange
	ipCIDRsFilter sql.NullString
	// ipCIDRValues are pre-stringified prefixes (already normalized by
	// parseFilter) suitable for pq.Array on a $N::cidr[] parameter.
	ipCIDRValues []string
}

// filterRequiresTelemetry reports whether any predicate in filter targets a
// device_metrics column. When true, callers must build the latest_metrics CTE
// and INNER-join it so missing telemetry naturally excludes the row.
func filterRequiresTelemetry(filter *stores.MinerFilter) bool {
	if filter == nil {
		return false
	}
	return len(filter.NumericRanges) > 0
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

	// Zone filter
	if len(filter.Zones) > 0 {
		fp.zonesFilter = sql.NullString{Valid: true}
		fp.zoneValues = filter.Zones
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
				" OR (device_pairing.pairing_status = 'AUTHENTICATION_NEEDED'" +
					" AND (device_status.status IS NULL OR device_status.status != 'OFFLINE'))")
			// Devices with actionable errors. Excludes NULL-status paired miners
			// so they stay bucketed as offline (matches CountMinersByState).
			fmt.Fprintf(sb,
				" OR (EXISTS (SELECT 1 FROM errors WHERE errors.device_id = device.id"+
					" AND errors.org_id = $%d AND errors.closed_at IS NULL AND %s)"+
					" AND NOT (device_status.status IS NULL AND device_pairing.pairing_status = 'PAIRED')"+
					" AND (device_status.status IS NULL OR device_status.status NOT IN %s))",
				argNum, actionableErrorSeverities, nonActionableStatuses)
			args = append(args, orgID)
			argNum++
		}
		if fp.includeNullStatus {
			// NULL-status paired miners (counted as offline in dashboard).
			// Scoped to PAIRED only to match CountMinersByState's WHERE clause.
			sb.WriteString(
				" OR (device_status.status IS NULL" +
					" AND device_pairing.pairing_status = 'PAIRED')")
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

	if fp.rackIDsFilter.Valid {
		fmt.Fprintf(sb,
			" AND EXISTS (SELECT 1 FROM device_set_membership dcm"+
				" WHERE dcm.device_id = device.id"+
				" AND dcm.org_id = $%d"+
				" AND dcm.device_set_type = 'rack'"+
				" AND dcm.device_set_id = ANY($%d::bigint[]))",
			argNum, argNum+1)
		args = append(args, orgID, pq.Array(fp.rackIDValues))
		argNum += 2
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

	if fp.ipCIDRsFilter.Valid {
		fmt.Fprintf(sb,
			" AND EXISTS (SELECT 1 FROM unnest($%d::cidr[]) c"+
				" WHERE NULLIF(discovered_device.ip_address, '')::inet <<= c)",
			argNum)
		args = append(args, pq.Array(fp.ipCIDRValues))
		argNum++
	}

	if fp.zonesFilter.Valid {
		// Match miners assigned to any non-deleted rack whose zone is in the value
		// list. Org scoping is enforced via device_set_membership.org_id; the join
		// to device_set carries the soft-delete check (rack delete sets
		// device_set.deleted_at, but membership/rack-extension rows persist), and
		// the join to device_set_rack pulls the zone for value comparison.
		fmt.Fprintf(sb,
			" AND EXISTS (SELECT 1 FROM device_set_membership dcm"+
				" JOIN device_set ds ON ds.id = dcm.device_set_id"+
				" JOIN device_set_rack dsr ON dsr.device_set_id = dcm.device_set_id"+
				" WHERE dcm.device_id = device.id"+
				" AND dcm.org_id = $%d"+
				" AND dcm.device_set_type = 'rack'"+
				" AND ds.deleted_at IS NULL"+
				" AND dsr.zone = ANY($%d::text[]))",
			argNum, argNum+1)
		args = append(args, orgID, pq.Array(fp.zoneValues))
		argNum += 2
	}

	return args, argNum
}
