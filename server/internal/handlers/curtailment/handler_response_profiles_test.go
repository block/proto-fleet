package curtailment

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domainCurtailment "github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestHandler_CreateCurtailmentResponseProfile(t *testing.T) {
	t.Parallel()

	store := newHandlerResponseProfileStore()
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	resp, err := h.CreateCurtailmentResponseProfile(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.CreateCurtailmentResponseProfileRequest{
			ProfileName: "Standard shed",
			Site:        &pb.ScopeSite{SiteId: 7},
			Mode:        pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			Strategy:    pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST,
			Level:       pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL,
			Priority:    pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL,
			ModeParams: &pb.CreateCurtailmentResponseProfileRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 2500, ToleranceKw: ptrFloat64(25)},
			},
			CurtailBatchSize:        ptrUint32(100),
			CurtailBatchIntervalSec: ptrUint32(15),
			RestoreBatchSize:        ptrUint32(20),
			RestoreBatchIntervalSec: ptrUint32(30),
		}),
	)

	require.NoError(t, err)
	profile := resp.Msg.GetProfile()
	require.NotNil(t, profile)
	assert.Equal(t, int64(201), profile.GetProfileId())
	assert.Equal(t, "Standard shed", profile.GetProfileName())
	assert.Equal(t, int64(7), profile.GetSite().GetSiteId())
	assert.Equal(t, float64(2500), profile.GetFixedKw().GetTargetKw())
	require.NotNil(t, profile.CurtailBatchSize)
	assert.Equal(t, uint32(100), profile.GetCurtailBatchSize())
	assert.Equal(t, uint32(15), profile.GetCurtailBatchIntervalSec())
	assert.Equal(t, uint32(20), profile.GetRestoreBatchSize())
	assert.Equal(t, uint32(30), profile.GetRestoreBatchIntervalSec())
	require.NotNil(t, store.created)
	assert.Equal(t, int64(42), store.created.OrgID)
	require.NotNil(t, store.created.SiteID)
	assert.Equal(t, int64(7), *store.created.SiteID)
}

func TestHandler_CreateCurtailmentResponseProfileWithoutSite(t *testing.T) {
	t.Parallel()

	store := newHandlerResponseProfileStore()
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	resp, err := h.CreateCurtailmentResponseProfile(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.CreateCurtailmentResponseProfileRequest{
			ProfileName: "Whole org shed",
			Mode:        pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			Strategy:    pb.CurtailmentStrategy_CURTAILMENT_STRATEGY_LEAST_EFFICIENT_FIRST,
			Level:       pb.CurtailmentLevel_CURTAILMENT_LEVEL_FULL,
			Priority:    pb.CurtailmentPriority_CURTAILMENT_PRIORITY_NORMAL,
			ModeParams: &pb.CreateCurtailmentResponseProfileRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 2500},
			},
		}),
	)

	require.NoError(t, err)
	profile := resp.Msg.GetProfile()
	require.NotNil(t, profile)
	assert.Nil(t, profile.GetSite())
	require.NotNil(t, store.created)
	assert.Nil(t, store.created.SiteID)
	assert.Equal(t, 0, store.siteCheckCount)
}

func TestHandler_ResponseProfilesRequireManage(t *testing.T) {
	t.Parallel()

	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(newHandlerResponseProfileStore()))

	_, err := h.ListCurtailmentResponseProfiles(
		sessionCtxWithPerms(42, authz.PermCurtailmentRead),
		connect.NewRequest(&pb.ListCurtailmentResponseProfilesRequest{}),
	)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_ResponseProfileNonAdminCannotUseAdminControls(t *testing.T) {
	t.Parallel()

	store := newHandlerResponseProfileStore()
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	_, err := h.CreateCurtailmentResponseProfile(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.CreateCurtailmentResponseProfileRequest{
			ProfileName: "Maintenance shed",
			Site:        &pb.ScopeSite{SiteId: 7},
			Mode:        pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.CreateCurtailmentResponseProfileRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 2500},
			},
			IncludeMaintenance:      true,
			ForceIncludeMaintenance: true,
		}),
	)

	require.Error(t, err)
	assert.Nil(t, store.created)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_ResponseProfileAdminCanUseAdminControls(t *testing.T) {
	t.Parallel()

	store := newHandlerResponseProfileStore()
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	resp, err := h.CreateCurtailmentResponseProfile(
		startSessionCtxWithPerms(t, 42, domainAuth.AdminRoleName, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.CreateCurtailmentResponseProfileRequest{
			ProfileName: "Maintenance shed",
			Site:        &pb.ScopeSite{SiteId: 7},
			Mode:        pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.CreateCurtailmentResponseProfileRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 2500},
			},
			IncludeMaintenance:      true,
			ForceIncludeMaintenance: true,
		}),
	)

	require.NoError(t, err)
	require.NotNil(t, resp.Msg.GetProfile())
	require.NotNil(t, store.created)
	assert.True(t, store.created.ForceIncludeMaintenance)
}

type handlerResponseProfileStore struct {
	siteBelongs    bool
	siteCheckCount int
	created        *models.ResponseProfile
	profiles       []*models.ResponseProfile
}

func newHandlerResponseProfileStore() *handlerResponseProfileStore {
	return &handlerResponseProfileStore{
		siteBelongs: true,
	}
}

func (s *handlerResponseProfileStore) ListResponseProfiles(context.Context, int64) ([]*models.ResponseProfile, error) {
	return s.profiles, nil
}

func (s *handlerResponseProfileStore) GetResponseProfile(_ context.Context, _ int64, profileID int64) (*models.ResponseProfile, error) {
	for _, profile := range s.profiles {
		if profile.ID == profileID {
			return profile, nil
		}
	}
	return nil, fleeterror.NewNotFoundErrorf("curtailment response profile not found: %d", profileID)
}

func (s *handlerResponseProfileStore) CreateResponseProfile(_ context.Context, profile models.ResponseProfile) (*models.ResponseProfile, error) {
	profile.ID = 201
	s.created = &profile
	return &profile, nil
}

func (s *handlerResponseProfileStore) UpdateResponseProfile(_ context.Context, profile models.ResponseProfile) (*models.ResponseProfile, error) {
	return &profile, nil
}

func (s *handlerResponseProfileStore) DeleteResponseProfile(context.Context, int64, int64) error {
	return nil
}

func (s *handlerResponseProfileStore) SiteBelongsToOrg(context.Context, int64, int64) (bool, error) {
	s.siteCheckCount++
	return s.siteBelongs, nil
}

func ptrFloat64(v float64) *float64 {
	return &v
}

func ptrUint32(v uint32) *uint32 {
	return &v
}
