package minerdiscovery

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	interfaces "github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	miner "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
)

var MinerNotFoundFleetError = fleeterror.NewPlainError("miner not found at the specified address and port", connect.CodeNotFound).WithCallerStackTrace()

type DeviceOrgIdentifier struct {
	DeviceIdentifier string
	OrgID            int64
}

type DiscoveredDevice struct {
	pb.Device
	OrgID           int64
	FirstDiscovered time.Time
	LastSeen        time.Time
	Type            string
}

func (d *DiscoveredDevice) GetDeviceOrgIdentifier() DeviceOrgIdentifier {
	return DeviceOrgIdentifier{
		DeviceIdentifier: d.Device.DeviceIdentifier,
		OrgID:            d.OrgID,
	}
}

type Discoverer interface {
	Discover(ctx context.Context, ipAddress string, port string) (*DiscoveredDevice, error)
	GetMinerType() miner.Type
}

// Service maintains a collection of discoverers for different miner types
// and provides functionality to discover miners
type Service struct {
	discoverers map[miner.Type]Discoverer
}

func NewService(discoverers ...Discoverer) (*Service, error) {
	if len(discoverers) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("no discoverers provided")
	}

	discoverersMap := make(map[miner.Type]Discoverer)
	for _, d := range discoverers {
		discoverersMap[d.GetMinerType()] = d
	}

	return &Service{
		discoverers: discoverersMap,
	}, nil
}

// Discover attempts to discover a device at the given IP address and port
// using all available discoverers
func (s *Service) Discover(ctx context.Context, ipAddress string, port string) (*DiscoveredDevice, error) {
	var lastErr error

	for minerType := range s.discoverers {
		device, err := s.DiscoverMinerWithType(ctx, ipAddress, port, minerType)
		if err != nil {
			slog.Debug("Discovery failed",
				"minerType", minerType,
				"ipAddress", ipAddress,
				"port", port,
				"error", err)
			lastErr = err
			continue
		}

		if device != nil {
			return device, nil
		}
	}

	// If we reach here, no discoverer was successful
	return nil, lastErr
}

func (s *Service) DiscoverMinerWithType(ctx context.Context, ipAddress string, port string, minerType miner.Type) (*DiscoveredDevice, error) {
	discoverer, ok := s.discoverers[minerType]
	if !ok {
		return nil, fleeterror.NewInternalErrorf("no discoverer found for miner type: %s", minerType)
	}
	return discoverer.Discover(ctx, ipAddress, port)
}

//nolint:revive // GetMinerFromDeviceID will be implemented in the future
func (s *Service) GetMinerFromDeviceID(ctx context.Context, deviceID string) (interfaces.Miner, error) {
	// This method is a placeholder for future implementation
	// It should return a miner instance based on the device ID
	return nil, fmt.Errorf("unimplemented method: GetMinerFromDeviceID")
}
