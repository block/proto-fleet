package sqlstores

import (
	"testing"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/collection/v1"
	stores "github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
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
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_GROUP, nil, "name", "ASC", 51)
	assert.Contains(t, query, "ORDER BY dc.label ASC, dc.id ASC")
	assert.Contains(t, query, "LIMIT $3")
	assert.Len(t, args, 3)
	assert.Equal(t, int64(1), args[0])
	assert.Equal(t, int32(51), args[2])
}

func TestBuildCollectionListQuery_DeviceCountDesc(t *testing.T) {
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_GROUP, nil, "device_count", "DESC", 51)
	assert.Contains(t, query, "ORDER BY device_count DESC, dc.id DESC")
	assert.Contains(t, query, "dc.type = $2")
	assert.Len(t, args, 3)
}

func TestBuildCollectionListQuery_NameCursorASC(t *testing.T) {
	cursor := &collectionCursor{Label: "Alpha", ID: 5, SortField: "name"}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, cursor, "name", "ASC", 51)
	assert.Contains(t, query, "AND (dc.label > $2 OR (dc.label = $2 AND dc.id > $3))")
	assert.Contains(t, query, "ORDER BY dc.label ASC, dc.id ASC")
	assert.Equal(t, []any{int64(1), "Alpha", int64(5), int32(51)}, args)
}

func TestBuildCollectionListQuery_DeviceCountCursorDESC(t *testing.T) {
	dc := int32(10)
	cursor := &collectionCursor{Label: "Test", ID: 3, SortField: "device_count", DeviceCount: &dc}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, cursor, "device_count", "DESC", 51)
	assert.Contains(t, query, "HAVING (COUNT(dcm.id)::int < $2 OR (COUNT(dcm.id)::int = $2 AND dc.id < $3))")
	assert.Contains(t, query, "ORDER BY device_count DESC, dc.id DESC")
	assert.Equal(t, []any{int64(1), int32(10), int64(3), int32(51)}, args)
}
