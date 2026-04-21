package sqlstores

import (
	"testing"

	"github.com/lib/pq"

	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
)

func TestResolveCollectionSort(t *testing.T) {
	tests := []struct {
		name      string
		sort      *stores.SortConfig
		wantField string
		wantDir   string
	}{
		{"nil defaults to name ASC", nil, "name", "ASC"},
		{"unspecified defaults to name ASC", &stores.SortConfig{}, "name", "ASC"},
		{"name ASC", &stores.SortConfig{
			Field:     stores.SortFieldName,
			Direction: stores.SortDirectionAsc,
		}, "name", "ASC"},
		{"name DESC", &stores.SortConfig{
			Field:     stores.SortFieldName,
			Direction: stores.SortDirectionDesc,
		}, "name", "DESC"},
		{"device_count ASC", &stores.SortConfig{
			Field:     stores.SortFieldDeviceCount,
			Direction: stores.SortDirectionAsc,
		}, "device_count", "ASC"},
		{"device_count DESC", &stores.SortConfig{
			Field:     stores.SortFieldDeviceCount,
			Direction: stores.SortDirectionDesc,
		}, "device_count", "DESC"},
		{"issue_count ASC", &stores.SortConfig{
			Field:     stores.SortFieldIssueCount,
			Direction: stores.SortDirectionAsc,
		}, "issue_count", "ASC"},
		{"issue_count DESC", &stores.SortConfig{
			Field:     stores.SortFieldIssueCount,
			Direction: stores.SortDirectionDesc,
		}, "issue_count", "DESC"},
		{"zone ASC", &stores.SortConfig{
			Field:     stores.SortFieldLocation,
			Direction: stores.SortDirectionAsc,
		}, "zone", "ASC"},
		{"zone DESC", &stores.SortConfig{
			Field:     stores.SortFieldLocation,
			Direction: stores.SortDirectionDesc,
		}, "zone", "DESC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, dir := resolveCollectionSort(tt.sort)
			assert.Equal(t, tt.wantField, field)
			assert.Equal(t, tt.wantDir, dir)
		})
	}
}

func TestBuildCollectionListQuery_DefaultSort(t *testing.T) {
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_GROUP, nil, "name", "ASC", 51, nil, nil)
	assert.Contains(t, query, "0::int AS issue_count")
	assert.Contains(t, query, "ORDER BY dc.label ASC, dc.id ASC")
	assert.NotContains(t, query, "SUM(component_issue_counts.device_count)::int AS issue_count")
	assert.Contains(t, query, "LIMIT $3")
	assert.Len(t, args, 3)
	assert.Equal(t, int64(1), args[0])
	assert.Equal(t, int32(51), args[2])
}

func TestBuildCollectionListQuery_DeviceCountDesc(t *testing.T) {
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_GROUP, nil, "device_count", "DESC", 51, nil, nil)
	assert.Contains(t, query, "ORDER BY device_count DESC, dc.id DESC")
	assert.Contains(t, query, "dc.type = $2")
	assert.Len(t, args, 3)
}

func TestBuildCollectionListQuery_IssueCountDesc(t *testing.T) {
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_GROUP, nil, "issue_count", "DESC", 51, nil, nil)
	assert.Contains(t, query, "MAX(COALESCE(issue_counts.issue_count, 0))::int AS issue_count")
	assert.Contains(t, query, "ORDER BY issue_count DESC, dc.id DESC")
	assert.Contains(t, query, "SUM(component_issue_counts.device_count)::int AS issue_count")
	assert.Contains(t, query, "dc.type = $2")
	assert.Len(t, args, 3)
}

func TestBuildCollectionListQuery_NameCursorASC(t *testing.T) {
	cursor := &collectionCursor{Label: "Alpha", ID: 5, SortField: "name"}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, cursor, "name", "ASC", 51, nil, nil)
	assert.Contains(t, query, "AND (dc.label > $2 OR (dc.label = $2 AND dc.id > $3))")
	assert.Contains(t, query, "ORDER BY dc.label ASC, dc.id ASC")
	assert.Equal(t, []any{int64(1), "Alpha", int64(5), int32(51)}, args)
}

func TestBuildCollectionListQuery_DeviceCountCursorDESC(t *testing.T) {
	dc := int32(10)
	cursor := &collectionCursor{Label: "Test", ID: 3, SortField: "device_count", DeviceCount: &dc}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, cursor, "device_count", "DESC", 51, nil, nil)
	assert.Contains(t, query, "HAVING (COUNT(dcm.id)::int < $2 OR (COUNT(dcm.id)::int = $2 AND dc.id < $3))")
	assert.Contains(t, query, "ORDER BY device_count DESC, dc.id DESC")
	assert.Equal(t, []any{int64(1), int32(10), int64(3), int32(51)}, args)
}

func TestBuildCollectionListQuery_IssueCountCursorDESC(t *testing.T) {
	ic := int32(7)
	cursor := &collectionCursor{Label: "Test", ID: 3, SortField: "issue_count", IssueCount: &ic}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, cursor, "issue_count", "DESC", 51, nil, nil)
	assert.Contains(t, query, "HAVING (MAX(COALESCE(issue_counts.issue_count, 0))::int < $2 OR (MAX(COALESCE(issue_counts.issue_count, 0))::int = $2 AND dc.id < $3))")
	assert.Contains(t, query, "ORDER BY issue_count DESC, dc.id DESC")
	assert.Equal(t, []any{int64(1), int32(7), int64(3), int32(51)}, args)
}

func TestBuildCollectionListQuery_ErrorComponentTypes(t *testing.T) {
	errorTypes := []int32{1, 3}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "name", "ASC", 51, errorTypes, nil)
	assert.Contains(t, query, "AND EXISTS")
	assert.Contains(t, query, "e.component_type = ANY($3::int[])")
	assert.Len(t, args, 4)
	assert.Equal(t, pq.Array(errorTypes), args[2])
}

func TestBuildCollectionListQuery_ZoneSortASC(t *testing.T) {
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "zone", "ASC", 51, nil, nil)
	assert.Contains(t, query, "ORDER BY dcr.zone ASC NULLS LAST, dc.id ASC")
	assert.Contains(t, query, "dc.type = $2")
	assert.Len(t, args, 3)
}

func TestBuildCollectionListQuery_ZoneSortDESC(t *testing.T) {
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "zone", "DESC", 51, nil, nil)
	assert.Contains(t, query, "ORDER BY dcr.zone DESC NULLS LAST, dc.id DESC")
	assert.Len(t, args, 3)
}

func TestBuildCollectionListQuery_ZoneCursorASC(t *testing.T) {
	z := "Building A"
	cursor := &collectionCursor{Label: "Rack1", ID: 7, SortField: "zone", Zone: &z}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, cursor, "zone", "ASC", 51, nil, nil)
	assert.Contains(t, query, "AND ((dcr.zone, dc.id) > ($3, $4) OR dcr.zone IS NULL)")
	assert.Contains(t, query, "ORDER BY dcr.zone ASC NULLS LAST, dc.id ASC")
	assert.Equal(t, "Building A", args[2])
	assert.Equal(t, int64(7), args[3])
}

func TestBuildCollectionListQuery_ZoneCursorNullASC(t *testing.T) {
	cursor := &collectionCursor{Label: "Rack1", ID: 7, SortField: "zone", Zone: nil}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, cursor, "zone", "ASC", 51, nil, nil)
	assert.Contains(t, query, "AND (dcr.zone IS NULL AND dc.id > $3)")
	assert.Equal(t, int64(7), args[2])
}

func TestBuildCollectionListQuery_ZoneFilter(t *testing.T) {
	zones := []string{"Building A", "Building B"}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "name", "ASC", 51, nil, zones)
	assert.Contains(t, query, "AND dcr.zone = ANY($3::text[])")
	assert.Equal(t, pq.Array(zones), args[2])
	assert.Len(t, args, 4)
}

func TestBuildCollectionCountQuery_ZoneFilter(t *testing.T) {
	zones := []string{"Building A"}
	query, args := buildCollectionCountQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, zones)
	assert.Contains(t, query, "LEFT JOIN device_set_rack dcr")
	assert.Contains(t, query, "AND dcr.zone = ANY($3::text[])")
	assert.Equal(t, pq.Array(zones), args[2])
}

func TestBuildCollectionCountQuery_ErrorComponentTypes(t *testing.T) {
	errorTypes := []int32{2, 4}
	query, args := buildCollectionCountQuery(1, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, errorTypes, nil)
	assert.Contains(t, query, "AND EXISTS")
	assert.Contains(t, query, "e.component_type = ANY($2::int[])")
	assert.Len(t, args, 2)
	assert.Equal(t, pq.Array(errorTypes), args[1])
}
