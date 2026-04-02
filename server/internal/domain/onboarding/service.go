package onboarding

import (
	"context"

	pb "github.com/block/proto-fleet/server/generated/grpc/onboarding/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
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
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	totalPairedDevices, err := s.deviceStore.GetTotalPairedDevices(ctx, info.OrganizationID, nil)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting number of paired devices: %v", err)
	}

	totalDevicesPendingAuth, err := s.deviceStore.GetTotalDevicesPendingAuth(ctx, info.OrganizationID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting number of devices pending auth: %v", err)
	}

	totalPools, err := s.poolStore.GetTotalPools(ctx, info.OrganizationID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting number of configured pools: %v", err)
	}

	return &pb.FleetOnboardingStatus{
		PoolConfigured: totalPools > 0,
		DevicePaired:   totalPairedDevices > 0 || totalDevicesPendingAuth > 0,
	}, nil
}
