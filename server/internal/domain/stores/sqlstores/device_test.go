package sqlstores

import (
	"testing"

	fm "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

// TestProtoDeviceStatusToSQL verifies enum conversion for all DeviceStatus values
func TestProtoDeviceStatusToSQL(t *testing.T) {
	tests := []struct {
		name     string
		input    fm.DeviceStatus
		expected sqlc.DeviceStatusStatus
	}{
		{
			name:     "UNSPECIFIED maps to UNKNOWN",
			input:    fm.DeviceStatus_DEVICE_STATUS_UNSPECIFIED,
			expected: sqlc.DeviceStatusStatusUNKNOWN,
		},
		{
			name:     "ONLINE maps to ACTIVE",
			input:    fm.DeviceStatus_DEVICE_STATUS_ONLINE,
			expected: sqlc.DeviceStatusStatusACTIVE,
		},
		{
			name:     "OFFLINE maps to OFFLINE",
			input:    fm.DeviceStatus_DEVICE_STATUS_OFFLINE,
			expected: sqlc.DeviceStatusStatusOFFLINE,
		},
		{
			name:     "MAINTENANCE maps to MAINTENANCE",
			input:    fm.DeviceStatus_DEVICE_STATUS_MAINTENANCE,
			expected: sqlc.DeviceStatusStatusMAINTENANCE,
		},
		{
			name:     "ERROR maps to ERROR",
			input:    fm.DeviceStatus_DEVICE_STATUS_ERROR,
			expected: sqlc.DeviceStatusStatusERROR,
		},
		{
			name:     "INACTIVE maps to INACTIVE",
			input:    fm.DeviceStatus_DEVICE_STATUS_INACTIVE,
			expected: sqlc.DeviceStatusStatusINACTIVE,
		},
		{
			name:     "NEEDS_MINING_POOL maps to NEEDSMININGPOOL",
			input:    fm.DeviceStatus_DEVICE_STATUS_NEEDS_MINING_POOL,
			expected: sqlc.DeviceStatusStatusNEEDSMININGPOOL,
		},
		{
			name:     "Unknown value (out of range) maps to UNKNOWN",
			input:    fm.DeviceStatus(999),
			expected: sqlc.DeviceStatusStatusUNKNOWN,
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
		expected sqlc.DevicePairingPairingStatus
	}{
		{
			name:     "UNSPECIFIED maps to UNPAIRED",
			input:    fm.PairingStatus_PAIRING_STATUS_UNSPECIFIED,
			expected: sqlc.DevicePairingPairingStatusUNPAIRED,
		},
		{
			name:     "PAIRED maps to PAIRED",
			input:    fm.PairingStatus_PAIRING_STATUS_PAIRED,
			expected: sqlc.DevicePairingPairingStatusPAIRED,
		},
		{
			name:     "UNPAIRED maps to UNPAIRED",
			input:    fm.PairingStatus_PAIRING_STATUS_UNPAIRED,
			expected: sqlc.DevicePairingPairingStatusUNPAIRED,
		},
		{
			name:     "AUTHENTICATION_NEEDED maps to AUTHENTICATIONNEEDED",
			input:    fm.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED,
			expected: sqlc.DevicePairingPairingStatusAUTHENTICATIONNEEDED,
		},
		{
			name:     "PENDING maps to PENDING",
			input:    fm.PairingStatus_PAIRING_STATUS_PENDING,
			expected: sqlc.DevicePairingPairingStatusPENDING,
		},
		{
			name:     "FAILED maps to FAILED",
			input:    fm.PairingStatus_PAIRING_STATUS_FAILED,
			expected: sqlc.DevicePairingPairingStatusFAILED,
		},
		{
			name:     "Unknown value (out of range) maps to UNPAIRED",
			input:    fm.PairingStatus(999),
			expected: sqlc.DevicePairingPairingStatusUNPAIRED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProtoPairingStatusToSQL(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}
