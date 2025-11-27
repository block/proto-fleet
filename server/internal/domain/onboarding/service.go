package onboarding

import (
	"context"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
)

type Service struct {
	deviceStore interfaces.DeviceStore
	poolStore   interfaces.PoolStore
	userStore   interfaces.UserStore
}

func NewService(deviceStore interfaces.DeviceStore, poolStore interfaces.PoolStore, userStore interfaces.UserStore) *Service {
	return &Service{
		deviceStore: deviceStore,
		poolStore:   poolStore,
		userStore:   userStore,
	}
}

func (s *Service) GetFleetOnboardingStatus(ctx context.Context) (*pb.FleetOnboardingStatus, error) {
	// Check if admin is created (doesn't require authentication)
	hasUser, err := s.userStore.HasUser(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error checking if admin user exists: %v", err)
	}

	status := &pb.FleetOnboardingStatus{
		AdminCreated: hasUser,
	}

	// If authenticated, also check pool and device status
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err == nil {
		totalPairedDevices, err := s.deviceStore.GetTotalPairedDevices(ctx, claims.OrgID, nil)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error getting number of paired devices: %v", err)
		}

		totalPools, err := s.poolStore.GetTotalPools(ctx, claims.OrgID)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error getting number of configured pools: %v", err)
		}

		status.PoolConfigured = totalPools > 0
		status.DevicePaired = totalPairedDevices > 0
	}

	return status, nil
}
