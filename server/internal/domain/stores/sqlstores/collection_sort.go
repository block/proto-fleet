package sqlstores

import (
	"fmt"
	"strings"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/collection/v1"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	collectionSortFieldName        = "name"
	collectionSortFieldDeviceCount = "device_count"
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

	switch sort.Field { //nolint:exhaustive // only name and device_count are valid for collections
	case stores.SortFieldDeviceCount:
		field = collectionSortFieldDeviceCount
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
func buildCollectionCountQuery(orgID int64, collectionType pb.CollectionType) (string, []any) {
	if collectionType == pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED {
		return "SELECT COUNT(*)::int FROM device_collection WHERE org_id = $1 AND deleted_at IS NULL", []any{orgID}
	}
	sqlType := protoCollectionTypeToSQL(collectionType)
	return "SELECT COUNT(*)::int FROM device_collection WHERE org_id = $1 AND type = $2 AND deleted_at IS NULL", []any{orgID, sqlType}
}

// buildCollectionListQuery generates a dynamic SQL query for listing collections
// with sort and cursor-based keyset pagination.
func buildCollectionListQuery(orgID int64, collectionType pb.CollectionType, cursor *collectionCursor, sortField, sortDir string, limit int32) (string, []any) {
	var sb strings.Builder
	args := []any{orgID}
	argNum := 2

	// Base query
	sb.WriteString(`SELECT dc.id, dc.type, dc.label, dc.description, dc.created_at, dc.updated_at,
       COUNT(dcm.id)::int AS device_count
FROM device_collection dc
LEFT JOIN device_collection_membership dcm ON dc.id = dcm.collection_id
WHERE dc.org_id = $1 AND dc.deleted_at IS NULL`)

	// Type filter
	if collectionType != pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED {
		sqlType := protoCollectionTypeToSQL(collectionType)
		sb.WriteString(fmt.Sprintf(" AND dc.type = $%d", argNum))
		args = append(args, sqlType)
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

	sb.WriteString(" GROUP BY dc.id")

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
	default:
		sb.WriteString(fmt.Sprintf(" ORDER BY dc.label %s, dc.id %s", sortDir, sortDir))
	}

	// LIMIT
	sb.WriteString(fmt.Sprintf(" LIMIT $%d", argNum))
	args = append(args, limit)

	return sb.String(), args
}
