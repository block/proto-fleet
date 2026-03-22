package sqlstores

import (
	"fmt"
	"strings"

	"github.com/lib/pq"

	pb "github.com/proto-at-block/proto-fleet/server/generated/grpc/collection/v1"
	stores "github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	collectionSortFieldName        = "name"
	collectionSortFieldDeviceCount = "device_count"
	collectionSortFieldLocation    = "location"
	collectionSortDirASC           = "ASC"
	collectionSortDirDESC          = "DESC"
)

// resolveCollectionSort converts a SortConfig into a canonical field name and SQL direction.
// Defaults to name ASC when unspecified.
func resolveCollectionSort(sort *stores.SortConfig) (field, dir string) {
	field = collectionSortFieldName
	dir = collectionSortDirASC

	if sort == nil || sort.Field == stores.SortFieldUnspecified {
		return field, dir
	}

	switch sort.Field { //nolint:exhaustive // only name, device_count, and location are valid for collections
	case stores.SortFieldDeviceCount:
		field = collectionSortFieldDeviceCount
	case stores.SortFieldLocation:
		field = collectionSortFieldLocation
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
func buildCollectionCountQuery(orgID int64, collectionType pb.CollectionType, errorComponentTypes []int32, locations []string) (string, []any) {
	var sb strings.Builder
	args := []any{orgID}
	argNum := 2

	sb.WriteString("SELECT COUNT(*)::int FROM device_collection dc")

	if len(locations) > 0 {
		sb.WriteString(" LEFT JOIN device_collection_rack dcr ON dcr.collection_id = dc.id")
	}

	sb.WriteString(" WHERE dc.org_id = $1 AND dc.deleted_at IS NULL")

	if collectionType != pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED {
		sqlType := protoCollectionTypeToSQL(collectionType)
		sb.WriteString(fmt.Sprintf(" AND dc.type = $%d", argNum))
		args = append(args, sqlType)
		argNum++
	}

	if len(locations) > 0 {
		sb.WriteString(fmt.Sprintf(" AND dcr.location = ANY($%d::text[])", argNum))
		args = append(args, pq.Array(locations))
		argNum++
	}

	if len(errorComponentTypes) > 0 {
		sb.WriteString(fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM device_collection_membership dcm_err
			JOIN device d_err ON dcm_err.device_id = d_err.id AND d_err.deleted_at IS NULL
			JOIN discovered_device dd_err ON d_err.discovered_device_id = dd_err.id AND dd_err.is_active = TRUE
			JOIN device_pairing dp_err ON d_err.id = dp_err.device_id
				AND dp_err.pairing_status IN ('PAIRED', 'AUTHENTICATION_NEEDED')
			JOIN errors e ON d_err.id = e.device_id
				AND e.org_id = dcm_err.org_id
				AND e.closed_at IS NULL
				AND e.severity IN (1, 2, 3)
				AND e.component_type = ANY($%d::int[])
			WHERE dcm_err.collection_id = dc.id AND dcm_err.org_id = $1
		)`, argNum))
		args = append(args, pq.Array(errorComponentTypes))
	}

	return sb.String(), args
}

// buildCollectionListQuery generates a dynamic SQL query for listing collections
// with sort and cursor-based keyset pagination.
func buildCollectionListQuery(orgID int64, collectionType pb.CollectionType, cursor *collectionCursor, sortField, sortDir string, limit int32, errorComponentTypes []int32, locations []string) (string, []any) {
	var sb strings.Builder
	args := []any{orgID}
	argNum := 2

	// Base query — always LEFT JOIN rack table so we can always scan dcr.location
	// without conditional branching. LEFT JOIN ensures racks without a
	// device_collection_rack row are not silently excluded.
	sb.WriteString(`SELECT dc.id, dc.type, dc.label, dc.description, dc.created_at, dc.updated_at,
       COUNT(dcm.id)::int AS device_count, dcr.location
FROM device_collection dc
LEFT JOIN device_collection_membership dcm ON dc.id = dcm.collection_id
LEFT JOIN device_collection_rack dcr ON dcr.collection_id = dc.id
WHERE dc.org_id = $1 AND dc.deleted_at IS NULL`)

	// Type filter
	if collectionType != pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED {
		sqlType := protoCollectionTypeToSQL(collectionType)
		sb.WriteString(fmt.Sprintf(" AND dc.type = $%d", argNum))
		args = append(args, sqlType)
		argNum++
	}

	// Location filter
	if len(locations) > 0 {
		sb.WriteString(fmt.Sprintf(" AND dcr.location = ANY($%d::text[])", argNum))
		args = append(args, pq.Array(locations))
		argNum++
	}

	// Error component types filter — matches the device/error criteria used by stats
	// (active, non-deleted, paired devices with actionable severity errors)
	if len(errorComponentTypes) > 0 {
		sb.WriteString(fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM device_collection_membership dcm_err
			JOIN device d_err ON dcm_err.device_id = d_err.id AND d_err.deleted_at IS NULL
			JOIN discovered_device dd_err ON d_err.discovered_device_id = dd_err.id AND dd_err.is_active = TRUE
			JOIN device_pairing dp_err ON d_err.id = dp_err.device_id
				AND dp_err.pairing_status IN ('PAIRED', 'AUTHENTICATION_NEEDED')
			JOIN errors e ON d_err.id = e.device_id
				AND e.org_id = dcm_err.org_id
				AND e.closed_at IS NULL
				AND e.severity IN (1, 2, 3)
				AND e.component_type = ANY($%d::int[])
			WHERE dcm_err.collection_id = dc.id AND dcm_err.org_id = $1
		)`, argNum))
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

	// Keyset cursor for location sort (WHERE before GROUP BY, similar to name).
	// Location is nullable (LEFT JOIN), so use the same NULL-aware pattern as
	// device_sort.go: NULL locations sort last (NULLS LAST in ORDER BY) and cursor
	// predicates branch on whether the cursor row itself had a NULL value.
	if cursor != nil && sortField == collectionSortFieldLocation {
		cmp := ">"
		if sortDir == collectionSortDirDESC {
			cmp = "<"
		}
		if cursor.Location == nil {
			// Cursor row had NULL location — only compare IDs among NULLs
			sb.WriteString(fmt.Sprintf(
				" AND (dcr.location IS NULL AND dc.id %s $%d)",
				cmp, argNum,
			))
			args = append(args, cursor.ID)
			argNum++
		} else {
			// Cursor row had non-NULL location — include NULLs (they sort last)
			sb.WriteString(fmt.Sprintf(
				" AND ((dcr.location, dc.id) %s ($%d, $%d) OR dcr.location IS NULL)",
				cmp, argNum, argNum+1,
			))
			args = append(args, *cursor.Location, cursor.ID)
			argNum += 2
		}
	}

	sb.WriteString(" GROUP BY dc.id, dcr.location")

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

	// ORDER BY
	switch sortField {
	case collectionSortFieldDeviceCount:
		sb.WriteString(fmt.Sprintf(" ORDER BY device_count %s, dc.id %s", sortDir, sortDir))
	case collectionSortFieldLocation:
		sb.WriteString(fmt.Sprintf(" ORDER BY dcr.location %s NULLS LAST, dc.id %s", sortDir, sortDir))
	default:
		sb.WriteString(fmt.Sprintf(" ORDER BY dc.label %s, dc.id %s", sortDir, sortDir))
	}

	// LIMIT
	sb.WriteString(fmt.Sprintf(" LIMIT $%d", argNum))
	args = append(args, limit)

	return sb.String(), args
}
