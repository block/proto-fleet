package sqlstores

import (
	"fmt"
	"strings"

	"github.com/lib/pq"

	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	collectionSortFieldName        = "name"
	collectionSortFieldDeviceCount = "device_count"
	collectionSortFieldIssueCount  = "issue_count"
	collectionSortFieldZone        = "zone"
	collectionSortDirASC           = "ASC"
	collectionSortDirDESC          = "DESC"
	collectionIssueCountExpr       = "MAX(COALESCE(issue_counts.issue_count, 0))::int"
)

var collectionIssueCountJoin = fmt.Sprintf(`LEFT JOIN (
	SELECT component_issue_counts.device_set_id, SUM(component_issue_counts.device_count)::int AS issue_count
	FROM (
		SELECT dcm_issue.device_set_id, e.component_type, COUNT(DISTINCT e.device_id)::int AS device_count
		FROM device_set_membership dcm_issue
		JOIN device_set dc_issue ON dcm_issue.device_set_id = dc_issue.id AND dc_issue.deleted_at IS NULL
		JOIN device d_issue ON dcm_issue.device_id = d_issue.id AND d_issue.deleted_at IS NULL
		JOIN discovered_device dd_issue ON d_issue.discovered_device_id = dd_issue.id AND dd_issue.is_active = TRUE
		JOIN device_pairing dp_issue ON d_issue.id = dp_issue.device_id
			AND %s
		JOIN errors e ON d_issue.id = e.device_id
			AND e.org_id = dcm_issue.org_id
			AND e.closed_at IS NULL
			AND %s
			AND %s
		WHERE dcm_issue.org_id = $1
		GROUP BY dcm_issue.device_set_id, e.component_type
	) component_issue_counts
	GROUP BY component_issue_counts.device_set_id
) issue_counts ON issue_counts.device_set_id = dc.id
`, actionablePairingStatusesExpr("dp_issue"), actionableErrorSeveritiesExpr("e"), actionableErrorComponentTypesExpr("e"))

// resolveCollectionSort converts a SortConfig into a canonical field name and SQL direction.
// Defaults to name ASC when unspecified.
func resolveCollectionSort(sort *stores.SortConfig) (field, dir string) {
	field = collectionSortFieldName
	dir = collectionSortDirASC

	if sort == nil || sort.Field == stores.SortFieldUnspecified {
		return field, dir
	}

	switch sort.Field { //nolint:exhaustive // only name, device_count, issue_count, and zone are valid for collections
	case stores.SortFieldDeviceCount:
		field = collectionSortFieldDeviceCount
	case stores.SortFieldIssueCount:
		field = collectionSortFieldIssueCount
	case stores.SortFieldLocation:
		field = collectionSortFieldZone
	default:
		field = collectionSortFieldName
	}

	switch sort.Direction { //nolint:exhaustive // unspecified and asc both map to ASC
	case stores.SortDirectionDesc:
		dir = collectionSortDirDESC
	default:
		dir = collectionSortDirASC
	}

	return field, dir
}

// buildCollectionCountQuery returns the SQL and args for counting collections.
func buildCollectionCountQuery(orgID int64, collectionType pb.CollectionType, errorComponentTypes []int32, zones []string) (string, []any) {
	var sb strings.Builder
	args := []any{orgID}
	argNum := 2

	sb.WriteString("SELECT COUNT(*)::int FROM device_set dc")

	if len(zones) > 0 {
		sb.WriteString(" LEFT JOIN device_set_rack dcr ON dcr.device_set_id = dc.id")
	}

	sb.WriteString(" WHERE dc.org_id = $1 AND dc.deleted_at IS NULL")

	if collectionType != pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED {
		sqlType := protoDeviceSetTypeToSQL(collectionType)
		sb.WriteString(fmt.Sprintf(" AND dc.type = $%d", argNum))
		args = append(args, sqlType)
		argNum++
	}

	if len(zones) > 0 {
		sb.WriteString(fmt.Sprintf(" AND dcr.zone = ANY($%d::text[])", argNum))
		args = append(args, pq.Array(zones))
		argNum++
	}

	if len(errorComponentTypes) > 0 {
		sb.WriteString(fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM device_set_membership dcm_err
			JOIN device d_err ON dcm_err.device_id = d_err.id AND d_err.deleted_at IS NULL
			JOIN discovered_device dd_err ON d_err.discovered_device_id = dd_err.id AND dd_err.is_active = TRUE
			JOIN device_pairing dp_err ON d_err.id = dp_err.device_id
				AND %s
			JOIN errors e ON d_err.id = e.device_id
				AND e.org_id = dcm_err.org_id
				AND e.closed_at IS NULL
				AND %s
				AND e.component_type = ANY($%d::int[])
			WHERE dcm_err.device_set_id = dc.id AND dcm_err.org_id = $1
		)`, actionablePairingStatusesExpr("dp_err"), actionableErrorSeveritiesExpr("e"), argNum))
		args = append(args, pq.Array(errorComponentTypes))
	}

	return sb.String(), args
}

// buildCollectionListQuery generates a dynamic SQL query for listing collections
// with sort and cursor-based keyset pagination.
func buildCollectionListQuery(orgID int64, collectionType pb.CollectionType, cursor *collectionCursor, sortField, sortDir string, limit int32, errorComponentTypes []int32, zones []string) (string, []any) {
	var sb strings.Builder
	args := []any{orgID}
	argNum := 2
	issueCountSelect := "0::int"

	// Base query — always LEFT JOIN rack table so we can always scan dcr.zone
	// without conditional branching. LEFT JOIN ensures racks without a
	// device_set_rack row are not silently excluded.
	if sortField == collectionSortFieldIssueCount {
		issueCountSelect = collectionIssueCountExpr
	}
	sb.WriteString(fmt.Sprintf(`SELECT dc.id, dc.type, dc.label, dc.description, dc.created_at, dc.updated_at,
       COUNT(dcm.id)::int AS device_count, %s AS issue_count, dcr.zone
FROM device_set dc
LEFT JOIN device_set_membership dcm ON dc.id = dcm.device_set_id
LEFT JOIN device_set_rack dcr ON dcr.device_set_id = dc.id
`, issueCountSelect))
	if sortField == collectionSortFieldIssueCount {
		sb.WriteString(collectionIssueCountJoin)
	}
	sb.WriteString(`
WHERE dc.org_id = $1 AND dc.deleted_at IS NULL`)

	// Type filter
	if collectionType != pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED {
		sqlType := protoDeviceSetTypeToSQL(collectionType)
		sb.WriteString(fmt.Sprintf(" AND dc.type = $%d", argNum))
		args = append(args, sqlType)
		argNum++
	}

	// Zone filter
	if len(zones) > 0 {
		sb.WriteString(fmt.Sprintf(" AND dcr.zone = ANY($%d::text[])", argNum))
		args = append(args, pq.Array(zones))
		argNum++
	}

	// Error component types filter — matches the device/error criteria used by stats
	// (active, non-deleted, paired devices with actionable severity errors)
	if len(errorComponentTypes) > 0 {
		sb.WriteString(fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM device_set_membership dcm_err
			JOIN device d_err ON dcm_err.device_id = d_err.id AND d_err.deleted_at IS NULL
			JOIN discovered_device dd_err ON d_err.discovered_device_id = dd_err.id AND dd_err.is_active = TRUE
			JOIN device_pairing dp_err ON d_err.id = dp_err.device_id
				AND %s
			JOIN errors e ON d_err.id = e.device_id
				AND e.org_id = dcm_err.org_id
				AND e.closed_at IS NULL
				AND %s
				AND e.component_type = ANY($%d::int[])
			WHERE dcm_err.device_set_id = dc.id AND dcm_err.org_id = $1
		)`, actionablePairingStatusesExpr("dp_err"), actionableErrorSeveritiesExpr("e"), argNum))
		args = append(args, pq.Array(errorComponentTypes))
		argNum++
	}

	// Keyset cursor for non-aggregate fields (WHERE before GROUP BY)
	if cursor != nil && sortField == collectionSortFieldName {
		cmp := ">"
		if sortDir == collectionSortDirDESC {
			cmp = "<"
		}
		sb.WriteString(fmt.Sprintf(
			" AND (dc.label %s $%d OR (dc.label = $%d AND dc.id %s $%d))",
			cmp, argNum, argNum, cmp, argNum+1,
		))
		args = append(args, cursor.Label, cursor.ID)
		argNum += 2
	}

	// Keyset cursor for zone sort (WHERE before GROUP BY, similar to name).
	// Zone is nullable (LEFT JOIN), so use the same NULL-aware pattern as
	// device_sort.go: NULL zones sort last (NULLS LAST in ORDER BY) and cursor
	// predicates branch on whether the cursor row itself had a NULL value.
	if cursor != nil && sortField == collectionSortFieldZone {
		cmp := ">"
		if sortDir == collectionSortDirDESC {
			cmp = "<"
		}
		if cursor.Zone == nil {
			// Cursor row had NULL zone — only compare IDs among NULLs
			sb.WriteString(fmt.Sprintf(
				" AND (dcr.zone IS NULL AND dc.id %s $%d)",
				cmp, argNum,
			))
			args = append(args, cursor.ID)
			argNum++
		} else {
			// Cursor row had non-NULL zone — include NULLs (they sort last)
			sb.WriteString(fmt.Sprintf(
				" AND ((dcr.zone, dc.id) %s ($%d, $%d) OR dcr.zone IS NULL)",
				cmp, argNum, argNum+1,
			))
			args = append(args, *cursor.Zone, cursor.ID)
			argNum += 2
		}
	}

	sb.WriteString(" GROUP BY dc.id, dcr.zone")

	// Keyset cursor for aggregate fields (HAVING after GROUP BY)
	if cursor != nil && sortField == collectionSortFieldDeviceCount {
		cmp := ">"
		if sortDir == collectionSortDirDESC {
			cmp = "<"
		}
		cursorCount := int32(0)
		if cursor.DeviceCount != nil {
			cursorCount = *cursor.DeviceCount
		}
		sb.WriteString(fmt.Sprintf(
			" HAVING (COUNT(dcm.id)::int %s $%d OR (COUNT(dcm.id)::int = $%d AND dc.id %s $%d))",
			cmp, argNum, argNum, cmp, argNum+1,
		))
		args = append(args, cursorCount, cursor.ID)
		argNum += 2
	}

	if cursor != nil && sortField == collectionSortFieldIssueCount {
		cmp := ">"
		if sortDir == collectionSortDirDESC {
			cmp = "<"
		}
		cursorCount := int32(0)
		if cursor.IssueCount != nil {
			cursorCount = *cursor.IssueCount
		}
		sb.WriteString(fmt.Sprintf(
			" HAVING (%s %s $%d OR (%s = $%d AND dc.id %s $%d))",
			collectionIssueCountExpr, cmp, argNum, collectionIssueCountExpr, argNum, cmp, argNum+1,
		))
		args = append(args, cursorCount, cursor.ID)
		argNum += 2
	}

	// ORDER BY
	switch sortField {
	case collectionSortFieldDeviceCount:
		sb.WriteString(fmt.Sprintf(" ORDER BY device_count %s, dc.id %s", sortDir, sortDir))
	case collectionSortFieldIssueCount:
		sb.WriteString(fmt.Sprintf(" ORDER BY issue_count %s, dc.id %s", sortDir, sortDir))
	case collectionSortFieldZone:
		sb.WriteString(fmt.Sprintf(" ORDER BY dcr.zone %s NULLS LAST, dc.id %s", sortDir, sortDir))
	default:
		sb.WriteString(fmt.Sprintf(" ORDER BY dc.label %s, dc.id %s", sortDir, sortDir))
	}

	// LIMIT
	sb.WriteString(fmt.Sprintf(" LIMIT $%d", argNum))
	args = append(args, limit)

	return sb.String(), args
}
