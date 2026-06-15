package fleetmanagement

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
)

// TestCollectPairedDeviceIdentifiers verifies that DEFAULT_PASSWORD devices are
// enriched with telemetry like PAIRED devices, while non-paired statuses are not.
func TestCollectPairedDeviceIdentifiers(t *testing.T) {
	// Arrange
	snapshots := []*pb.MinerStateSnapshot{
		{DeviceIdentifier: "paired", PairingStatus: pb.PairingStatus_PAIRING_STATUS_PAIRED},
		{DeviceIdentifier: "default-pw", PairingStatus: pb.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD},
		{DeviceIdentifier: "auth-needed", PairingStatus: pb.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED},
		{DeviceIdentifier: "unpaired", PairingStatus: pb.PairingStatus_PAIRING_STATUS_UNPAIRED},
	}

	// Act
	ids := collectPairedDeviceIdentifiers(snapshots)

	// Assert
	assert.ElementsMatch(t, []string{"paired", "default-pw"}, ids)
}
