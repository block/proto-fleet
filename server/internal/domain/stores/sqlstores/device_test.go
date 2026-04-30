package sqlstores

import (
	"testing"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProtoDeviceStatusToSQL verifies enum conversion for all DeviceStatus values
func TestProtoDeviceStatusToSQL(t *testing.T) {
	tests := []struct {
		name     string
		input    fm.DeviceStatus
		expected sqlc.DeviceStatusEnum
	}{
		{
			name:     "UNSPECIFIED maps to UNKNOWN",
			input:    fm.DeviceStatus_DEVICE_STATUS_UNSPECIFIED,
			expected: sqlc.DeviceStatusEnumUNKNOWN,
		},
		{
			name:     "ONLINE maps to ACTIVE",
			input:    fm.DeviceStatus_DEVICE_STATUS_ONLINE,
			expected: sqlc.DeviceStatusEnumACTIVE,
		},
		{
			name:     "OFFLINE maps to OFFLINE",
			input:    fm.DeviceStatus_DEVICE_STATUS_OFFLINE,
			expected: sqlc.DeviceStatusEnumOFFLINE,
		},
		{
			name:     "MAINTENANCE maps to MAINTENANCE",
			input:    fm.DeviceStatus_DEVICE_STATUS_MAINTENANCE,
			expected: sqlc.DeviceStatusEnumMAINTENANCE,
		},
		{
			name:     "ERROR maps to ERROR",
			input:    fm.DeviceStatus_DEVICE_STATUS_ERROR,
			expected: sqlc.DeviceStatusEnumERROR,
		},
		{
			name:     "INACTIVE maps to INACTIVE",
			input:    fm.DeviceStatus_DEVICE_STATUS_INACTIVE,
			expected: sqlc.DeviceStatusEnumINACTIVE,
		},
		{
			name:     "NEEDS_MINING_POOL maps to NEEDSMININGPOOL",
			input:    fm.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL,
			expected: sqlc.DeviceStatusEnumNEEDSMININGPOOL,
		},
		{
			name:     "Unknown value (out of range) maps to UNKNOWN",
			input:    fm.DeviceStatus(999),
			expected: sqlc.DeviceStatusEnumUNKNOWN,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProtoDeviceStatusToSQL(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestProtoPairingStatusToSQL verifies enum conversion for all PairingStatus values
func TestProtoPairingStatusToSQL(t *testing.T) {
	tests := []struct {
		name     string
		input    fm.PairingStatus
		expected sqlc.PairingStatusEnum
	}{
		{
			name:     "UNSPECIFIED maps to UNPAIRED",
			input:    fm.PairingStatus_PAIRING_STATUS_UNSPECIFIED,
			expected: sqlc.PairingStatusEnumUNPAIRED,
		},
		{
			name:     "PAIRED maps to PAIRED",
			input:    fm.PairingStatus_PAIRING_STATUS_PAIRED,
			expected: sqlc.PairingStatusEnumPAIRED,
		},
		{
			name:     "UNPAIRED maps to UNPAIRED",
			input:    fm.PairingStatus_PAIRING_STATUS_UNPAIRED,
			expected: sqlc.PairingStatusEnumUNPAIRED,
		},
		{
			name:     "AUTHENTICATION_NEEDED maps to AUTHENTICATIONNEEDED",
			input:    fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
			expected: sqlc.PairingStatusEnumAUTHENTICATIONNEEDED,
		},
		{
			name:     "PENDING maps to PENDING",
			input:    fm.PairingStatus_PAIRING_STATUS_PENDING,
			expected: sqlc.PairingStatusEnumPENDING,
		},
		{
			name:     "FAILED maps to FAILED",
			input:    fm.PairingStatus_PAIRING_STATUS_FAILED,
			expected: sqlc.PairingStatusEnumFAILED,
		},
		{
			name:     "Unknown value (out of range) maps to UNPAIRED",
			input:    fm.PairingStatus(999),
			expected: sqlc.PairingStatusEnumUNPAIRED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProtoPairingStatusToSQL(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildFilterParams_NilFilter(t *testing.T) {
	fp := buildFilterParams(nil)

	assert.False(t, fp.statusFilter.Valid)
	assert.False(t, fp.modelFilter.Valid)
	assert.False(t, fp.firmwareFilter.Valid)
	assert.False(t, fp.zoneFilter.Valid)
}

func TestBuildFilterParams_FirmwareOnly(t *testing.T) {
	fp := buildFilterParams(&stores.MinerFilter{
		FirmwareVersions: []string{"v3.5.1", "v3.5.2"},
	})

	assert.True(t, fp.firmwareFilter.Valid)
	assert.Equal(t, []string{"v3.5.1", "v3.5.2"}, fp.firmwareValues)
	assert.False(t, fp.zoneFilter.Valid)
	assert.Nil(t, fp.zoneValues)
}

func TestBuildFilterParams_ZonesOnly(t *testing.T) {
	fp := buildFilterParams(&stores.MinerFilter{
		Zones: []string{"building-a", "building-b"},
	})

	assert.True(t, fp.zoneFilter.Valid)
	assert.Equal(t, []string{"building-a", "building-b"}, fp.zoneValues)
	assert.False(t, fp.firmwareFilter.Valid)
	assert.Nil(t, fp.firmwareValues)
}

// Zones may contain commas (e.g. "Austin, Building 1"). The values must pass
// through verbatim — no CSV splitting — so the SQL ANY($N::text[]) match works.
func TestBuildFilterParams_ZoneWithCommaPassesThrough(t *testing.T) {
	fp := buildFilterParams(&stores.MinerFilter{
		Zones: []string{"Austin, Building 1", "Dallas"},
	})

	assert.True(t, fp.zoneFilter.Valid)
	assert.Equal(t, []string{"Austin, Building 1", "Dallas"}, fp.zoneValues)
}

func TestBuildFilterParams_FirmwareAndZones(t *testing.T) {
	fp := buildFilterParams(&stores.MinerFilter{
		FirmwareVersions: []string{"v3.5.1"},
		Zones:            []string{"building-a"},
	})

	assert.True(t, fp.firmwareFilter.Valid)
	assert.Equal(t, []string{"v3.5.1"}, fp.firmwareValues)
	assert.True(t, fp.zoneFilter.Valid)
	assert.Equal(t, []string{"building-a"}, fp.zoneValues)
}

// Empty slices must leave the filter unset so the query treats them as
// "no filter applied" rather than "match nothing".
func TestBuildFilterParams_EmptySlicesUnset(t *testing.T) {
	fp := buildFilterParams(&stores.MinerFilter{
		FirmwareVersions: []string{},
		Zones:            []string{},
	})

	assert.False(t, fp.firmwareFilter.Valid)
	assert.Nil(t, fp.firmwareValues)
	assert.False(t, fp.zoneFilter.Valid)
	assert.Nil(t, fp.zoneValues)
}

func TestBuildFilterParams_AllFilters(t *testing.T) {
	fp := buildFilterParams(&stores.MinerFilter{
		DeviceStatusFilter: []minermodels.MinerStatus{minermodels.MinerStatusActive},
		ModelNames:         []string{"S21 XP"},
		FirmwareVersions:   []string{"v3.5.1"},
		Zones:              []string{"zone-a"},
	})

	assert.True(t, fp.statusFilter.Valid)
	assert.True(t, fp.modelFilter.Valid)
	assert.True(t, fp.firmwareFilter.Valid)
	assert.True(t, fp.zoneFilter.Valid)
}
