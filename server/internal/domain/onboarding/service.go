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

func (s *Service) GetFleetInitStatus(ctx context.Context) (*pb.FleetInitStatus, error) {
	hasUser, err := s.userStore.HasUser(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error checking if admin user exists: %v", err)
	}

	return &pb.FleetInitStatus{
		AdminCreated: hasUser,
	}, nil
}

func (s *Service) GetFleetOnboardingStatus(ctx context.Context) (*pb.FleetOnboardingStatus, error) {
	claims, err := tokenDomain.GetClientAuthJWTClaims(ctx)
	if err != nil {
		return nil, err
	}

	totalPairedDevices, err := s.deviceStore.GetTotalPairedDevices(ctx, claims.OrgID, nil)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting number of paired devices: %v", err)
	}

	totalPools, err := s.poolStore.GetTotalPools(ctx, claims.OrgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting number of configured pools: %v", err)
	}

	return &pb.FleetOnboardingStatus{
		PoolConfigured: totalPools > 0,
		DevicePaired:   totalPairedDevices > 0,
	}, nil
}
