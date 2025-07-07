package interfaces

import (
	"context"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

type DeviceStore interface {
	UpsertDevice(ctx context.Context, device *pb.Device, orgID int64, deviceType string) error
	UpsertDeviceIPAssignment(ctx context.Context, device *pb.Device, orgID int64, ipAddress string, port string) error
	UpsertMinerCredentials(ctx context.Context, device *pb.Device, orgID int64, usernameEnc string, passwordEnc *secrets.Text) error
	UpsertDevicePairing(ctx context.Context, device *pb.Device, orgID int64, pairingToken string, pairingStatus string) error
	GetMinerCredentials(ctx context.Context, device *pb.Device, orgID int64) (*pb.Credentials, error)
	GetDeviceByDeviceIdentifier(ctx context.Context, identifier string, orgID int64) (*pb.Device, error)
}
