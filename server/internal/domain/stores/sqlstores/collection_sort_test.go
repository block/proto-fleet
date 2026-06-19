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
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_GROUP, nil, "name", "ASC", 51, nil)
	assert.Contains(t, query, "0::int AS issue_count")
	assert.Contains(t, query, "ORDER BY dc.label ASC, dc.id ASC")
	assert.NotContains(t, query, "SUM(component_issue_counts.device_count)::int AS issue_count")
	assert.Contains(t, query, "LIMIT $3")
	assert.Len(t, args, 3)
	assert.Equal(t, int64(1), args[0])
	assert.Equal(t, int32(51), args[2])
}

func TestBuildCollectionListQuery_DeviceCountDesc(t *testing.T) {
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_GROUP, nil, "device_count", "DESC", 51, nil)
	assert.Contains(t, query, "ORDER BY device_count DESC, dc.id DESC")
	assert.Contains(t, query, "dc.type = $2")
	assert.Len(t, args, 3)
}

func TestBuildCollectionListQuery_IssueCountDesc(t *testing.T) {
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_GROUP, nil, "issue_count", "DESC", 51, nil)
	assert.Contains(t, query, "MAX(COALESCE(issue_counts.issue_count, 0))::int AS issue_count")
	assert.Contains(t, query, "ORDER BY issue_count DESC, dc.id DESC")
	assert.Contains(t, query, "SUM(component_issue_counts.device_count)::int AS issue_count")
	assert.Contains(t, query, "dc.type = $2")
	assert.Len(t, args, 3)
}

func TestBuildCollectionListQuery_NameCursorASC(t *testing.T) {
	cursor := &collectionCursor{Label: "Alpha", ID: 5, SortField: "name"}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, cursor, "name", "ASC", 51, nil)
	assert.Contains(t, query, "AND (dc.label > $2 OR (dc.label = $2 AND dc.id > $3))")
	assert.Contains(t, query, "ORDER BY dc.label ASC, dc.id ASC")
	assert.Equal(t, []any{int64(1), "Alpha", int64(5), int32(51)}, args)
}

func TestBuildCollectionListQuery_DeviceCountCursorDESC(t *testing.T) {
	dc := int32(10)
	cursor := &collectionCursor{Label: "Test", ID: 3, SortField: "device_count", DeviceCount: &dc}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, cursor, "device_count", "DESC", 51, nil)
	assert.Contains(t, query, "HAVING (COUNT(dcm.id)::int < $2 OR (COUNT(dcm.id)::int = $2 AND dc.id < $3))")
	assert.Contains(t, query, "ORDER BY device_count DESC, dc.id DESC")
	assert.Equal(t, []any{int64(1), int32(10), int64(3), int32(51)}, args)
}

func TestBuildCollectionListQuery_IssueCountCursorDESC(t *testing.T) {
	ic := int32(7)
	cursor := &collectionCursor{Label: "Test", ID: 3, SortField: "issue_count", IssueCount: &ic}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, cursor, "issue_count", "DESC", 51, nil)
	assert.Contains(t, query, "HAVING (MAX(COALESCE(issue_counts.issue_count, 0))::int < $2 OR (MAX(COALESCE(issue_counts.issue_count, 0))::int = $2 AND dc.id < $3))")
	assert.Contains(t, query, "ORDER BY issue_count DESC, dc.id DESC")
	assert.Equal(t, []any{int64(1), int32(7), int64(3), int32(51)}, args)
}

func TestBuildCollectionListQuery_ErrorComponentTypes(t *testing.T) {
	errorTypes := []int32{1, 3}
	filter := &stores.DeviceSetFilter{ErrorComponentTypes: errorTypes}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "name", "ASC", 51, filter)
	assert.Contains(t, query, "AND EXISTS")
	assert.Contains(t, query, "e.component_type = ANY($3::int[])")
	assert.Len(t, args, 4)
	assert.Equal(t, pq.Array(errorTypes), args[2])
}

func TestBuildCollectionListQuery_ZoneSortASC(t *testing.T) {
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "zone", "ASC", 51, nil)
	assert.Contains(t, query, "ORDER BY dcr.zone ASC NULLS LAST, dc.id ASC")
	assert.Contains(t, query, "dc.type = $2")
	assert.Len(t, args, 3)
}

func TestBuildCollectionListQuery_ZoneSortDESC(t *testing.T) {
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "zone", "DESC", 51, nil)
	assert.Contains(t, query, "ORDER BY dcr.zone DESC NULLS LAST, dc.id DESC")
	assert.Len(t, args, 3)
}

func TestBuildCollectionListQuery_ZoneCursorASC(t *testing.T) {
	z := "Building A"
	cursor := &collectionCursor{Label: "Rack1", ID: 7, SortField: "zone", Zone: &z}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, cursor, "zone", "ASC", 51, nil)
	assert.Contains(t, query, "AND ((dcr.zone, dc.id) > ($3, $4) OR dcr.zone IS NULL)")
	assert.Contains(t, query, "ORDER BY dcr.zone ASC NULLS LAST, dc.id ASC")
	assert.Equal(t, "Building A", args[2])
	assert.Equal(t, int64(7), args[3])
}

func TestBuildCollectionListQuery_ZoneCursorNullASC(t *testing.T) {
	cursor := &collectionCursor{Label: "Rack1", ID: 7, SortField: "zone", Zone: nil}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, cursor, "zone", "ASC", 51, nil)
	assert.Contains(t, query, "AND (dcr.zone IS NULL AND dc.id > $3)")
	assert.Equal(t, int64(7), args[2])
}

func TestBuildCollectionListQuery_ZoneKeys_Wildcard(t *testing.T) {
	// Wildcard zone_keys preserve the legacy "match zone label across all
	// buildings" behavior the deprecated Zones field used to give callers.
	filter := &stores.DeviceSetFilter{
		ZoneKeys: []stores.ZoneKey{
			{BuildingID: 0, Zone: "Building A"},
			{BuildingID: 0, Zone: "Building B"},
		},
	}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "name", "ASC", 51, filter)
	assert.Contains(t, query, "AND (dcr.zone = ANY($3::text[]))")
	assert.Equal(t, pq.Array([]string{"Building A", "Building B"}), args[2])
	assert.Len(t, args, 4)
}

func TestBuildCollectionListQuery_ZoneKeys_Scoped(t *testing.T) {
	filter := &stores.DeviceSetFilter{
		ZoneKeys: []stores.ZoneKey{
			{BuildingID: 7, Zone: "Room 2"},
		},
	}
	query, _ := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "name", "ASC", 51, filter)
	assert.Contains(t, query, "(dcr.building_id, dcr.zone) IN (")
	assert.Contains(t, query, "UNNEST($3::bigint[], $4::text[])")
}

func TestBuildCollectionListQuery_SiteIDs(t *testing.T) {
	filter := &stores.DeviceSetFilter{
		SiteIDs: []int64{3, 5},
	}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "name", "ASC", 51, filter)
	assert.Contains(t, query, "AND (dcr.site_id = ANY($3::bigint[]))")
	assert.Equal(t, pq.Array([]int64{3, 5}), args[2])
}

func TestBuildCollectionListQuery_IncludeUnassigned(t *testing.T) {
	filter := &stores.DeviceSetFilter{IncludeUnassigned: true}
	query, _ := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "name", "ASC", 51, filter)
	assert.Contains(t, query, "AND (dcr.site_id IS NULL)")
}

func TestBuildCollectionListQuery_SiteIDsPlusIncludeUnassigned_OR(t *testing.T) {
	filter := &stores.DeviceSetFilter{
		SiteIDs:           []int64{7},
		IncludeUnassigned: true,
	}
	query, _ := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "name", "ASC", 51, filter)
	assert.Contains(t, query, "AND (dcr.site_id = ANY($3::bigint[]) OR dcr.site_id IS NULL)")
}

func TestBuildCollectionCountQuery_SiteIDs_JoinsRack(t *testing.T) {
	filter := &stores.DeviceSetFilter{SiteIDs: []int64{2}}
	query, _ := buildCollectionCountQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, filter)
	assert.Contains(t, query, "LEFT JOIN device_set_rack dcr")
	assert.Contains(t, query, "AND (dcr.site_id = ANY($3::bigint[]))")
}

func TestBuildCollectionListQuery_BuildingIDs(t *testing.T) {
	filter := &stores.DeviceSetFilter{
		BuildingIDs: []int64{7, 9},
	}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "name", "ASC", 51, filter)
	assert.Contains(t, query, "AND (dcr.building_id = ANY($3::bigint[]))")
	assert.Equal(t, pq.Array([]int64{7, 9}), args[2])
}

func TestBuildCollectionListQuery_SiteIDsAndIncludeUnassigned(t *testing.T) {
	filter := &stores.DeviceSetFilter{
		SiteIDs:           []int64{3, 5},
		IncludeUnassigned: true,
	}
	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "name", "ASC", 51, filter)
	assert.Contains(t, query, "LEFT JOIN device_set_rack dcr ON dcr.device_set_id = dc.id")
	assert.Contains(t, query, "AND (dcr.site_id = ANY($3::bigint[]) OR dcr.site_id IS NULL)")
	assert.Equal(t, pq.Array([]int64{3, 5}), args[2])
}

func TestBuildCollectionListQuery_IncludeNoBuilding(t *testing.T) {
	filter := &stores.DeviceSetFilter{IncludeNoBuilding: true}
	query, _ := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, nil, "name", "ASC", 51, filter)
	assert.Contains(t, query, "AND (dcr.building_id IS NULL)")
}

func TestBuildCollectionCountQuery_ZoneKeys_Wildcard(t *testing.T) {
	filter := &stores.DeviceSetFilter{
		ZoneKeys: []stores.ZoneKey{{BuildingID: 0, Zone: "Building A"}},
	}
	query, args := buildCollectionCountQuery(1, pb.CollectionType_COLLECTION_TYPE_RACK, filter)
	assert.Contains(t, query, "LEFT JOIN device_set_rack dcr")
	assert.Contains(t, query, "AND (dcr.zone = ANY($3::text[]))")
	assert.Equal(t, pq.Array([]string{"Building A"}), args[2])
}

func TestBuildCollectionCountQuery_ErrorComponentTypes(t *testing.T) {
	errorTypes := []int32{2, 4}
	filter := &stores.DeviceSetFilter{ErrorComponentTypes: errorTypes}
	query, args := buildCollectionCountQuery(1, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, filter)
	assert.Contains(t, query, "AND EXISTS")
	assert.Contains(t, query, "e.component_type = ANY($2::int[])")
	assert.Len(t, args, 2)
	assert.Equal(t, pq.Array(errorTypes), args[1])
}

func TestBuildCollectionListQuery_TelemetryRanges(t *testing.T) {
	minTemp := 40.0
	maxTemp := 80.0
	filter := &stores.DeviceSetFilter{TelemetryRanges: []stores.NumericRange{{
		Field:        stores.NumericFilterFieldTemperatureC,
		Min:          &minTemp,
		Max:          &maxTemp,
		MinInclusive: true,
	}}}

	query, args := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_GROUP, nil, "name", "ASC", 51, filter)

	assert.Contains(t, query, "telemetry_stats ON telemetry_stats.device_set_id = dc.id")
	assert.Contains(t, query, "COALESCE(telemetry_stats.temperature_reporting_count, 0) > 0")
	assert.Contains(t, query, "telemetry_stats.min_temperature_c >= $3")
	assert.Contains(t, query, "telemetry_stats.max_temperature_c < $4")
	assert.Len(t, args, 5)
	assert.Equal(t, int64(1), args[0])
	assert.Equal(t, minTemp, args[2])
	assert.Equal(t, maxTemp, args[3])
	assert.Equal(t, int32(51), args[4])
}

func TestBuildCollectionListQuery_TelemetryStatsFilterInvalidValues(t *testing.T) {
	minHashrate := 10.0
	filter := &stores.DeviceSetFilter{TelemetryRanges: []stores.NumericRange{{
		Field: stores.NumericFilterFieldHashrateTHs,
		Min:   &minHashrate,
	}}}

	query, _ := buildCollectionListQuery(1, pb.CollectionType_COLLECTION_TYPE_GROUP, nil, "name", "ASC", 51, filter)

	assert.Contains(t, query, "COUNT(lm.hash_rate_hs) FILTER (WHERE isfinite(lm.hash_rate_hs) AND lm.hash_rate_hs >= 0)::int AS hashrate_reporting_count")
	assert.Contains(t, query, "SUM(lm.hash_rate_hs) FILTER (WHERE isfinite(lm.hash_rate_hs) AND lm.hash_rate_hs >= 0)")
	assert.Contains(t, query, "COUNT(lm.efficiency_jh) FILTER (WHERE isfinite(lm.efficiency_jh) AND lm.efficiency_jh >= 0)::int AS efficiency_reporting_count")
	assert.Contains(t, query, "AVG(lm.efficiency_jh * 1e12) FILTER (WHERE isfinite(lm.efficiency_jh) AND lm.efficiency_jh >= 0)")
	assert.Contains(t, query, "COUNT(lm.power_w) FILTER (WHERE isfinite(lm.power_w) AND lm.power_w >= 0)::int AS power_reporting_count")
	assert.Contains(t, query, "SUM(lm.power_w) FILTER (WHERE isfinite(lm.power_w) AND lm.power_w >= 0)")
	assert.Contains(t, query, "COUNT(lm.temp_c) FILTER (WHERE isfinite(lm.temp_c))::int AS temperature_reporting_count")
	assert.Contains(t, query, "MIN(lm.temp_c) FILTER (WHERE isfinite(lm.temp_c))")
	assert.Contains(t, query, "MAX(lm.temp_c) FILTER (WHERE isfinite(lm.temp_c))")
	assert.NotContains(t, query, "lm.temp_c >= 0")
}

func TestBuildCollectionCountQuery_IssueAndTelemetryFiltersKeepArgumentOrder(t *testing.T) {
	minPower := 30.0
	errorTypes := []int32{2, 4}
	filter := &stores.DeviceSetFilter{
		ErrorComponentTypes: errorTypes,
		TelemetryRanges: []stores.NumericRange{{
			Field: stores.NumericFilterFieldPowerKW,
			Min:   &minPower,
		}},
	}

	query, args := buildCollectionCountQuery(1, pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, filter)

	assert.Contains(t, query, "e.component_type = ANY($2::int[])")
	assert.Contains(t, query, "COALESCE(telemetry_stats.power_reporting_count, 0) > 0")
	assert.Contains(t, query, "telemetry_stats.total_power_kw > $3")
	assert.Equal(t, pq.Array(errorTypes), args[1])
	assert.Equal(t, minPower, args[2])
}
