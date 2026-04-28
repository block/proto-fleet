package fleetmanagement

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFilter_NilFilter(t *testing.T) {
	filter, err := parseFilter(nil)

	require.NoError(t, err)
	require.NotNil(t, filter)
	assert.Empty(t, filter.FirmwareVersions)
	assert.Empty(t, filter.Zones)
}

func TestParseFilter_FirmwareVersions(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		FirmwareVersions: []string{"v3.5.1", "v3.5.2"},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Equal(t, []string{"v3.5.1", "v3.5.2"}, filter.FirmwareVersions)
}

func TestParseFilter_Zones(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		Zones: []string{"building-a", "building-b"},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Equal(t, []string{"building-a", "building-b"}, filter.Zones)
}

func TestParseFilter_FirmwareAndZonesEmpty(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		FirmwareVersions: []string{},
		Zones:            []string{},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Empty(t, filter.FirmwareVersions)
	assert.Empty(t, filter.Zones)
}

func TestParseFilter_NewFiltersCombineWithExisting(t *testing.T) {
	pbFilter := &pb.MinerListFilter{
		Models:           []string{"S21 XP"},
		FirmwareVersions: []string{"v3.5.1"},
		Zones:            []string{"building-a"},
		RackIds:          []int64{42},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Equal(t, []string{"S21 XP"}, filter.ModelNames)
	assert.Equal(t, []string{"v3.5.1"}, filter.FirmwareVersions)
	assert.Equal(t, []string{"building-a"}, filter.Zones)
	assert.Equal(t, []int64{42}, filter.RackIDs)
}

func TestParseFilter_FreeFormZoneWithSpecialChars(t *testing.T) {
	// Zone is free-form text. Server passes it through unchanged; URL/value
	// encoding is the client's responsibility.
	pbFilter := &pb.MinerListFilter{
		Zones: []string{"Austin, Building 1"},
	}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Equal(t, []string{"Austin, Building 1"}, filter.Zones)
}

func TestParseFilter_FirmwareVersions_RejectsOversizedArray(t *testing.T) {
	// Cap protects Postgres planner from `= ANY(huge_array)` blowup.
	values := make([]string, maxFreeFormFilterValues+1)
	for i := range values {
		values[i] = "v"
	}
	pbFilter := &pb.MinerListFilter{FirmwareVersions: values}

	_, err := parseFilter(pbFilter)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "firmware_versions")
}

func TestParseFilter_Zones_RejectsOversizedArray(t *testing.T) {
	values := make([]string, maxFreeFormFilterValues+1)
	for i := range values {
		values[i] = "z"
	}
	pbFilter := &pb.MinerListFilter{Zones: values}

	_, err := parseFilter(pbFilter)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "zones")
}

func TestParseFilter_FirmwareVersions_AcceptsMaxSizedArray(t *testing.T) {
	// Boundary: exactly maxFreeFormFilterValues is allowed.
	values := make([]string, maxFreeFormFilterValues)
	for i := range values {
		values[i] = "v"
	}
	pbFilter := &pb.MinerListFilter{FirmwareVersions: values}

	filter, err := parseFilter(pbFilter)

	require.NoError(t, err)
	assert.Len(t, filter.FirmwareVersions, maxFreeFormFilterValues)
}
