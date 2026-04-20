package pairing

import (
	"context"

	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
)

// pairing statuses
const (
	StatusPaired               = "PAIRED"
	StatusUnpaired             = "UNPAIRED"
	StatusAuthenticationNeeded = "AUTHENTICATION_NEEDED"
	StatusPending              = "PENDING"
	StatusFailed               = "FAILED"
)

type Pairer interface {
	// PairDevice handles the entire pairing process including saving the device to the database
	PairDevice(ctx context.Context, device *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) error
	// GetDeviceInfo returns the device information for a discovered device without pairing
	GetDeviceInfo(ctx context.Context, device *discoverymodels.DiscoveredDevice, credentials *pb.Credentials) (*pb.Device, error)
}
