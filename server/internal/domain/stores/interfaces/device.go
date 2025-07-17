package interfaces

import (
	"context"

	mm "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"

	fm "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery"
	"github.com/btc-mining/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
)

//go:generate mockgen -source=device.go -destination=mocks/mock_device_store.go -package=mocks DeviceStore

type MinerFilter struct {
	StatusFilter []string
	MinerType    []mm.Type
}

//nolint:interfacebloat // DeviceStore defines the interface for device-related operations in the store layer. We are okay with bloat at this time.
type DeviceStore interface {
	UpsertDevice(ctx context.Context, device *pb.Device, orgID int64, deviceType string) error
	UpsertDeviceIPAssignment(ctx context.Context, device *pb.Device, orgID int64, ipAddress string, port string) error
	UpsertMinerCredentials(ctx context.Context, device *pb.Device, orgID int64, usernameEnc string, passwordEnc *secrets.Text) error
	UpsertDevicePairing(ctx context.Context, device *pb.Device, orgID int64, pairingToken string, pairingStatus string) error
	GetMinerCredentials(ctx context.Context, device *pb.Device, orgID int64) (*pb.Credentials, error)
	GetDeviceByDeviceIdentifier(ctx context.Context, identifier string, orgID int64) (*pb.Device, error)
	GetDeviceWithIPAssignment(ctx context.Context, deviceIdentifier string, orgID int64) (*minerdiscovery.DiscoveredDevice, error)
	GetTotalPairedDevices(ctx context.Context, orgID int64, filter *MinerFilter) (int64, error)
	ListPairedDevices(ctx context.Context, cursor string, pageSize int32) ([]*fm.PairedDevice, string, error)
	ListPairedMinersWithStatus(ctx context.Context, orgID int64, cursor string, pageSize int32, filter *MinerFilter) ([]*pb.Device, string, error)
	GetAllPairedDeviceIdentifiers(ctx context.Context) ([]models.DeviceIdentifier, error)
	GetMinerStateCounts(ctx context.Context, orgID int64, filter *MinerFilter) (*fm.MinerStateCounts, error)
}
