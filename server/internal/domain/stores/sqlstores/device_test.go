package sqlstores

import (
	"testing"

	fm "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
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
