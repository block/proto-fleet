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
	"github.com/block/proto-fleet/server/internal/domain/session"
)

func TestHandler_CreateCurtailmentResponseProfile(t *testing.T) {
	t.Parallel()

	store := newHandlerResponseProfileStore()
	store.infrastructureDevices[31] = models.ResponseProfileInfrastructureDevice{ID: 31, SiteID: 7, Enabled: true}
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
			PostEventCooldownSec:    600,
			FacilityFanDeviceIds:    []int64{31},
			FanOffDelaySec:          45,
			FanRestoreDelaySec:      90,
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
	assert.Equal(t, uint32(600), profile.GetPostEventCooldownSec())
	assert.Equal(t, []int64{31}, profile.GetFacilityFanDeviceIds())
	assert.Equal(t, uint32(45), profile.GetFanOffDelaySec())
	assert.Equal(t, uint32(90), profile.GetFanRestoreDelaySec())
	require.NotNil(t, store.created)
	assert.Equal(t, int64(42), store.created.OrgID)
	require.NotNil(t, store.created.SiteID)
	assert.Equal(t, int64(7), *store.created.SiteID)
	assert.Equal(t, int32(600), store.created.PostEventCooldownSec)
	assert.Equal(t, []int64{31}, store.created.FacilityFanDeviceIDs)
	assert.Equal(t, int32(45), store.created.FanOffDelaySec)
	assert.Equal(t, int32(90), store.created.FanRestoreDelaySec)
}

func TestHandler_CreateCurtailmentResponseProfilePreservesExplicitZeroRestoreInterval(t *testing.T) {
	t.Parallel()

	store := newHandlerResponseProfileStore()
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	resp, err := h.CreateCurtailmentResponseProfile(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.CreateCurtailmentResponseProfileRequest{
			ProfileName: "Immediate restore",
			Site:        &pb.ScopeSite{SiteId: 7},
			Mode:        pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.CreateCurtailmentResponseProfileRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 2500},
			},
			RestoreBatchIntervalSec: ptrUint32(0),
		}),
	)

	require.NoError(t, err)
	assert.Equal(t, uint32(0), resp.Msg.GetProfile().GetRestoreBatchIntervalSec())
	assert.Equal(t, uint32(0), resp.Msg.GetProfile().GetRestoreBatchSize())
	require.NotNil(t, store.created)
	assert.Equal(t, int32(0), store.created.RestoreBatchIntervalSec)
	assert.Equal(t, int32(0), store.created.RestoreBatchSize)
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

func TestHandler_ListCurtailmentResponseProfilesFiltersSiteNarrowing(t *testing.T) {
	t.Parallel()

	shadowedSiteID := int64(7)
	fallbackSiteID := int64(8)
	store := newHandlerResponseProfileStore()
	store.deviceSites = map[string]*int64{"hidden-miner": &shadowedSiteID, "unassigned-miner": nil}
	store.profiles = []*models.ResponseProfile{
		{ID: 201, OrgID: 42, ProfileName: "Whole org", Mode: models.ModeFixedKw, TargetKW: ptrFloat64(2500), RestoreBatchSize: 50},
		{ID: 202, OrgID: 42, ProfileName: "Hidden site", SiteID: &shadowedSiteID, Mode: models.ModeFixedKw, TargetKW: ptrFloat64(2500), RestoreBatchSize: 50},
		{ID: 203, OrgID: 42, ProfileName: "Visible site", SiteID: &fallbackSiteID, Mode: models.ModeFixedKw, TargetKW: ptrFloat64(2500), RestoreBatchSize: 50},
		{ID: 204, OrgID: 42, ProfileName: "Hidden multi-site", ScopeJSON: []byte(`{"site_ids":[7,8]}`), Mode: models.ModeFixedKw, TargetKW: ptrFloat64(2500), RestoreBatchSize: 50},
		{ID: 205, OrgID: 42, ProfileName: "Hidden device", ScopeJSON: []byte(`{"device_identifiers":["hidden-miner"]}`), Mode: models.ModeFixedKw, TargetKW: ptrFloat64(2500), RestoreBatchSize: 50},
		{ID: 206, OrgID: 42, ProfileName: "Unassigned device", ScopeJSON: []byte(`{"device_identifiers":["unassigned-miner"]}`), Mode: models.ModeFixedKw, TargetKW: ptrFloat64(2500), RestoreBatchSize: 50},
	}
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	resp, err := h.ListCurtailmentResponseProfiles(
		testSessionCtxWithAssignments(t, &session.Info{
			AuthMethod:     session.AuthMethodSession,
			OrganizationID: 42,
			Role:           "OPERATOR",
			SessionID:      "sess-response-profile-list",
		}, testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(shadowedSiteID)),
		connect.NewRequest(&pb.ListCurtailmentResponseProfilesRequest{}),
	)

	require.NoError(t, err)
	profiles := resp.Msg.GetProfiles()
	require.Len(t, profiles, 1)
	assert.Equal(t, int64(203), profiles[0].GetProfileId())
}

func TestHandler_CreateCurtailmentResponseProfileChecksExplicitDeviceSites(t *testing.T) {
	t.Parallel()

	const (
		allowedSite = int64(7)
		deniedSite  = int64(8)
	)
	store := newHandlerResponseProfileStore()
	store.deviceSites = map[string]*int64{"hidden-miner": ptrHandlerInt64(deniedSite)}
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	_, err := h.CreateCurtailmentResponseProfile(
		testSessionCtxWithAssignments(t, &session.Info{
			AuthMethod:     session.AuthMethodSession,
			OrganizationID: 42,
			Role:           "OPERATOR",
			SessionID:      "sess-response-profile-create-device-scope",
		},
			testOrgAssignment(authz.PermCurtailmentManage),
			testSiteAssignment(allowedSite, authz.PermCurtailmentManage),
			testSiteAssignment(deniedSite),
		),
		connect.NewRequest(&pb.CreateCurtailmentResponseProfileRequest{
			ProfileName: "Composite shed",
			Scopes: []*pb.CurtailmentScope{
				{Scope: &pb.CurtailmentScope_Site{Site: &pb.ScopeSite{SiteId: allowedSite}}},
				{Scope: &pb.CurtailmentScope_DeviceIdentifiers{
					DeviceIdentifiers: &pb.ScopeDeviceList{DeviceIdentifiers: []string{"hidden-miner"}},
				}},
			},
			Mode: pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.CreateCurtailmentResponseProfileRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 2500},
			},
		}),
	)

	require.Error(t, err)
	assert.Nil(t, store.created)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_CreateCurtailmentResponseProfileRequiresOrgWideForUnassignedDevices(t *testing.T) {
	t.Parallel()

	const narrowedSite = int64(7)
	store := newHandlerResponseProfileStore()
	store.deviceSites = map[string]*int64{"unassigned-miner": nil}
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	_, err := h.CreateCurtailmentResponseProfile(
		testSessionCtxWithAssignments(t, &session.Info{
			AuthMethod:     session.AuthMethodSession,
			OrganizationID: 42,
			Role:           "OPERATOR",
			SessionID:      "sess-response-profile-create-unassigned-device",
		},
			testOrgAssignment(authz.PermCurtailmentManage),
			testSiteAssignment(narrowedSite),
		),
		connect.NewRequest(&pb.CreateCurtailmentResponseProfileRequest{
			ProfileName: "Unassigned miner shed",
			Scopes: []*pb.CurtailmentScope{
				{Scope: &pb.CurtailmentScope_DeviceIdentifiers{
					DeviceIdentifiers: &pb.ScopeDeviceList{DeviceIdentifiers: []string{"unassigned-miner"}},
				}},
			},
			Mode: pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.CreateCurtailmentResponseProfileRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 2500},
			},
		}),
	)

	require.Error(t, err)
	assert.Nil(t, store.created)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_CreateCurtailmentResponseProfileChecksCompositeSites(t *testing.T) {
	t.Parallel()

	const (
		allowedSite = int64(7)
		deniedSite  = int64(8)
	)
	store := newHandlerResponseProfileStore()
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	_, err := h.CreateCurtailmentResponseProfile(
		testSessionCtxWithAssignments(t, &session.Info{
			AuthMethod:     session.AuthMethodSession,
			OrganizationID: 42,
			Role:           "OPERATOR",
			SessionID:      "sess-response-profile-create-composite",
		},
			testOrgAssignment(authz.PermCurtailmentManage),
			testSiteAssignment(allowedSite, authz.PermCurtailmentManage),
			testSiteAssignment(deniedSite),
		),
		connect.NewRequest(&pb.CreateCurtailmentResponseProfileRequest{
			ProfileName: "Composite shed",
			Scopes: []*pb.CurtailmentScope{
				{Scope: &pb.CurtailmentScope_Site{Site: &pb.ScopeSite{SiteId: allowedSite}}},
				{Scope: &pb.CurtailmentScope_Site{Site: &pb.ScopeSite{SiteId: deniedSite}}},
			},
			Mode: pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.CreateCurtailmentResponseProfileRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 2500},
			},
		}),
	)

	require.Error(t, err)
	assert.Nil(t, store.created)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_GetCurtailmentResponseProfileChecksStoredSite(t *testing.T) {
	t.Parallel()

	siteID := int64(7)
	store := newHandlerResponseProfileStore()
	store.profiles = []*models.ResponseProfile{
		{ID: 201, OrgID: 42, ProfileName: "Hidden site", SiteID: &siteID, Mode: models.ModeFixedKw, TargetKW: ptrFloat64(2500), RestoreBatchSize: 50},
	}
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	_, err := h.GetCurtailmentResponseProfile(
		testSessionCtxWithAssignments(t, &session.Info{
			AuthMethod:     session.AuthMethodSession,
			OrganizationID: 42,
			Role:           "OPERATOR",
			SessionID:      "sess-response-profile-get",
		}, testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(siteID)),
		connect.NewRequest(&pb.GetCurtailmentResponseProfileRequest{ProfileId: 201}),
	)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_GetCurtailmentResponseProfileChecksStoredCompositeSites(t *testing.T) {
	t.Parallel()

	const (
		allowedSite = int64(7)
		deniedSite  = int64(8)
	)
	store := newHandlerResponseProfileStore()
	store.profiles = []*models.ResponseProfile{
		{ID: 201, OrgID: 42, ProfileName: "Hidden multi-site", ScopeJSON: []byte(`{"site_ids":[7,8]}`), Mode: models.ModeFixedKw, TargetKW: ptrFloat64(2500), RestoreBatchSize: 50},
	}
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	_, err := h.GetCurtailmentResponseProfile(
		testSessionCtxWithAssignments(t, &session.Info{
			AuthMethod:     session.AuthMethodSession,
			OrganizationID: 42,
			Role:           "OPERATOR",
			SessionID:      "sess-response-profile-get-composite",
		},
			testOrgAssignment(authz.PermCurtailmentManage),
			testSiteAssignment(allowedSite, authz.PermCurtailmentManage),
			testSiteAssignment(deniedSite),
		),
		connect.NewRequest(&pb.GetCurtailmentResponseProfileRequest{ProfileId: 201}),
	)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_UpdateCurtailmentResponseProfile(t *testing.T) {
	t.Parallel()

	siteID := int64(7)
	store := newHandlerResponseProfileStore()
	store.profiles = []*models.ResponseProfile{
		{ID: 201, OrgID: 42, ProfileName: "Old", SiteID: &siteID, ScopeJSON: siteScopeJSON(t, siteID), Mode: models.ModeFixedKw, TargetKW: ptrFloat64(1000), RestoreBatchSize: 50, FacilityFanDeviceIDs: []int64{31}, FanOffDelaySec: 45, FanRestoreDelaySec: 90},
	}
	store.infrastructureDevices[31] = models.ResponseProfileInfrastructureDevice{ID: 31, SiteID: siteID, Enabled: true}
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	resp, err := h.UpdateCurtailmentResponseProfile(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.UpdateCurtailmentResponseProfileRequest{
			ProfileId:   201,
			ProfileName: "Updated",
			Site:        &pb.ScopeSite{SiteId: siteID},
			Mode:        pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.UpdateCurtailmentResponseProfileRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 3000, ToleranceKw: ptrFloat64(10)},
			},
			RestoreBatchSize:        ptrUint32(40),
			RestoreBatchIntervalSec: ptrUint32(0),
			PostEventCooldownSec:    900,
		}),
	)

	require.NoError(t, err)
	profile := resp.Msg.GetProfile()
	require.NotNil(t, profile)
	assert.Equal(t, int64(201), profile.GetProfileId())
	assert.Equal(t, "Updated", profile.GetProfileName())
	assert.Equal(t, float64(3000), profile.GetFixedKw().GetTargetKw())
	assert.Equal(t, uint32(40), profile.GetRestoreBatchSize())
	assert.Equal(t, uint32(0), profile.GetRestoreBatchIntervalSec())
	assert.Equal(t, uint32(900), profile.GetPostEventCooldownSec())
	require.NotNil(t, store.updated)
	assert.Equal(t, int32(0), store.updated.RestoreBatchIntervalSec)
	assert.Equal(t, int32(900), store.updated.PostEventCooldownSec)
	assert.Equal(t, []int64{31}, store.updated.FacilityFanDeviceIDs)
	assert.Equal(t, int32(45), store.updated.FanOffDelaySec)
	assert.Equal(t, int32(90), store.updated.FanRestoreDelaySec)
	require.NotNil(t, store.updateExpectedSiteID)
	assert.Equal(t, siteID, *store.updateExpectedSiteID)
	assert.JSONEq(t, `{"site_id":7}`, string(store.updateExpectedScopeJSON))
}

func TestHandler_UpdateCurtailmentResponseProfileClearsFacilityFanSettingsWhenRequested(t *testing.T) {
	t.Parallel()

	siteID := int64(7)
	store := newHandlerResponseProfileStore()
	store.profiles = []*models.ResponseProfile{
		{ID: 201, OrgID: 42, ProfileName: "Old", SiteID: &siteID, ScopeJSON: siteScopeJSON(t, siteID), Mode: models.ModeFixedKw, TargetKW: ptrFloat64(1000), RestoreBatchSize: 50, FacilityFanDeviceIDs: []int64{31}, FanOffDelaySec: 45, FanRestoreDelaySec: 90},
	}
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	_, err := h.UpdateCurtailmentResponseProfile(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.UpdateCurtailmentResponseProfileRequest{
			ProfileId:                  201,
			ProfileName:                "Updated",
			Site:                       &pb.ScopeSite{SiteId: siteID},
			Mode:                       pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams:                 &pb.UpdateCurtailmentResponseProfileRequest_FixedKw{FixedKw: &pb.FixedKwParams{TargetKw: 3000}},
			ReplaceFacilityFanSettings: true,
			RestoreBatchSize:           ptrUint32(40),
			RestoreBatchIntervalSec:    ptrUint32(0),
		}),
	)

	require.NoError(t, err)
	require.NotNil(t, store.updated)
	assert.Empty(t, store.updated.FacilityFanDeviceIDs)
	assert.Zero(t, store.updated.FanOffDelaySec)
	assert.Zero(t, store.updated.FanRestoreDelaySec)
}

func TestHandler_UpdateCurtailmentResponseProfileGuardsStoredCompositeScope(t *testing.T) {
	t.Parallel()

	const (
		siteA = int64(7)
		siteB = int64(8)
	)
	store := newHandlerResponseProfileStore()
	store.profiles = []*models.ResponseProfile{
		{ID: 201, OrgID: 42, ProfileName: "Old composite", ScopeJSON: []byte(`{"site_ids":[7,8]}`), Mode: models.ModeFixedKw, TargetKW: ptrFloat64(1000), RestoreBatchSize: 50},
	}
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	_, err := h.UpdateCurtailmentResponseProfile(
		testSessionCtxWithAssignments(t, &session.Info{
			AuthMethod:     session.AuthMethodSession,
			OrganizationID: 42,
			Role:           "OPERATOR",
			SessionID:      "sess-response-profile-update-composite",
		},
			testOrgAssignment(authz.PermCurtailmentManage),
			testSiteAssignment(siteA, authz.PermCurtailmentManage),
			testSiteAssignment(siteB, authz.PermCurtailmentManage),
		),
		connect.NewRequest(&pb.UpdateCurtailmentResponseProfileRequest{
			ProfileId:   201,
			ProfileName: "Updated composite",
			Scopes: []*pb.CurtailmentScope{
				{Scope: &pb.CurtailmentScope_Site{Site: &pb.ScopeSite{SiteId: siteA}}},
				{Scope: &pb.CurtailmentScope_Site{Site: &pb.ScopeSite{SiteId: siteB}}},
			},
			Mode: pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.UpdateCurtailmentResponseProfileRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 3000},
			},
		}),
	)

	require.NoError(t, err)
	require.NotNil(t, store.updated)
	assert.Nil(t, store.updateExpectedSiteID)
	assert.JSONEq(t, `{"site_ids":[7,8]}`, string(store.updateExpectedScopeJSON))
}

func TestHandler_UpdateCurtailmentResponseProfilePreservesOmittedScope(t *testing.T) {
	t.Parallel()

	const (
		siteA = int64(7)
		siteB = int64(8)
	)
	store := newHandlerResponseProfileStore()
	store.profiles = []*models.ResponseProfile{
		{ID: 201, OrgID: 42, ProfileName: "Old composite", ScopeJSON: []byte(`{"site_ids":[7,8]}`), Mode: models.ModeFixedKw, TargetKW: ptrFloat64(1000), RestoreBatchSize: 50},
	}
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	_, err := h.UpdateCurtailmentResponseProfile(
		testSessionCtxWithAssignments(t, &session.Info{
			AuthMethod:     session.AuthMethodSession,
			OrganizationID: 42,
			Role:           "OPERATOR",
			SessionID:      "sess-response-profile-update-omitted-scope",
		},
			testOrgAssignment(authz.PermCurtailmentManage),
			testSiteAssignment(siteA, authz.PermCurtailmentManage),
			testSiteAssignment(siteB, authz.PermCurtailmentManage),
		),
		connect.NewRequest(&pb.UpdateCurtailmentResponseProfileRequest{
			ProfileId:   201,
			ProfileName: "Updated composite",
			Mode:        pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.UpdateCurtailmentResponseProfileRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 3000},
			},
		}),
	)

	require.NoError(t, err)
	require.NotNil(t, store.updated)
	assert.Nil(t, store.updated.SiteID)
	assert.JSONEq(t, `{"site_ids":[7,8],"device_identifiers":null}`, string(store.updated.ScopeJSON))
	assert.Nil(t, store.updateExpectedSiteID)
	assert.JSONEq(t, `{"site_ids":[7,8]}`, string(store.updateExpectedScopeJSON))
}

func TestHandler_UpdateCurtailmentResponseProfileAllowsLegacySiteClear(t *testing.T) {
	t.Parallel()

	siteID := int64(7)
	store := newHandlerResponseProfileStore()
	store.profiles = []*models.ResponseProfile{
		{ID: 201, OrgID: 42, ProfileName: "Old site", SiteID: &siteID, ScopeJSON: siteScopeJSON(t, siteID), Mode: models.ModeFixedKw, TargetKW: ptrFloat64(1000), RestoreBatchSize: 50},
	}
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	_, err := h.UpdateCurtailmentResponseProfile(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.UpdateCurtailmentResponseProfileRequest{
			ProfileId:   201,
			ProfileName: "Updated whole org",
			Mode:        pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.UpdateCurtailmentResponseProfileRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 3000},
			},
		}),
	)

	require.NoError(t, err)
	require.NotNil(t, store.updated)
	assert.Nil(t, store.updated.SiteID)
	assert.JSONEq(t, `{}`, string(store.updated.ScopeJSON))
	require.NotNil(t, store.updateExpectedSiteID)
	assert.Equal(t, siteID, *store.updateExpectedSiteID)
	assert.JSONEq(t, `{"site_id":7}`, string(store.updateExpectedScopeJSON))
}

func TestHandler_UpdateCurtailmentResponseProfileRequiresOrgWideToClearSite(t *testing.T) {
	t.Parallel()

	const narrowedSite = int64(8)
	siteID := int64(7)
	store := newHandlerResponseProfileStore()
	store.profiles = []*models.ResponseProfile{
		{ID: 201, OrgID: 42, ProfileName: "Old site", SiteID: &siteID, ScopeJSON: siteScopeJSON(t, siteID), Mode: models.ModeFixedKw, TargetKW: ptrFloat64(1000), RestoreBatchSize: 50},
	}
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	_, err := h.UpdateCurtailmentResponseProfile(
		testSessionCtxWithAssignments(t, &session.Info{
			AuthMethod:     session.AuthMethodSession,
			OrganizationID: 42,
			Role:           "OPERATOR",
			SessionID:      "sess-response-profile-update-clear-site",
		},
			testOrgAssignment(authz.PermCurtailmentManage),
			testSiteAssignment(narrowedSite),
		),
		connect.NewRequest(&pb.UpdateCurtailmentResponseProfileRequest{
			ProfileId:   201,
			ProfileName: "Updated whole org",
			Mode:        pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.UpdateCurtailmentResponseProfileRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 3000},
			},
		}),
	)

	require.Error(t, err)
	assert.Nil(t, store.updated)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_UpdateCurtailmentResponseProfileChecksExistingSite(t *testing.T) {
	t.Parallel()

	siteID := int64(7)
	store := newHandlerResponseProfileStore()
	store.profiles = []*models.ResponseProfile{
		{ID: 201, OrgID: 42, ProfileName: "Hidden site", SiteID: &siteID, Mode: models.ModeFixedKw, TargetKW: ptrFloat64(2500), RestoreBatchSize: 50},
	}
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	_, err := h.UpdateCurtailmentResponseProfile(
		testSessionCtxWithAssignments(t, &session.Info{
			AuthMethod:     session.AuthMethodSession,
			OrganizationID: 42,
			Role:           "OPERATOR",
			SessionID:      "sess-response-profile-update",
		}, testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(siteID)),
		connect.NewRequest(&pb.UpdateCurtailmentResponseProfileRequest{
			ProfileId:   201,
			ProfileName: "Still hidden",
			Site:        &pb.ScopeSite{SiteId: siteID},
			Mode:        pb.CurtailmentMode_CURTAILMENT_MODE_FIXED_KW,
			ModeParams: &pb.UpdateCurtailmentResponseProfileRequest_FixedKw{
				FixedKw: &pb.FixedKwParams{TargetKw: 2500},
			},
		}),
	)

	require.Error(t, err)
	assert.Nil(t, store.updated)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestHandler_DeleteCurtailmentResponseProfile(t *testing.T) {
	t.Parallel()

	siteID := int64(7)
	store := newHandlerResponseProfileStore()
	store.profiles = []*models.ResponseProfile{
		{ID: 201, OrgID: 42, ProfileName: "Standard shed", SiteID: &siteID, ScopeJSON: siteScopeJSON(t, siteID), Mode: models.ModeFixedKw, TargetKW: ptrFloat64(2500), RestoreBatchSize: 50},
	}
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	_, err := h.DeleteCurtailmentResponseProfile(
		sessionCtxWithPerms(42, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.DeleteCurtailmentResponseProfileRequest{ProfileId: 201}),
	)

	require.NoError(t, err)
	assert.Equal(t, int64(201), store.deletedProfileID)
	require.NotNil(t, store.deleteExpectedSiteID)
	assert.Equal(t, siteID, *store.deleteExpectedSiteID)
	assert.JSONEq(t, `{"site_id":7}`, string(store.deleteExpectedScopeJSON))
}

func TestHandler_DeleteCurtailmentResponseProfileChecksStoredSite(t *testing.T) {
	t.Parallel()

	siteID := int64(7)
	store := newHandlerResponseProfileStore()
	store.profiles = []*models.ResponseProfile{
		{ID: 201, OrgID: 42, ProfileName: "Hidden site", SiteID: &siteID, Mode: models.ModeFixedKw, TargetKW: ptrFloat64(2500), RestoreBatchSize: 50},
	}
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	_, err := h.DeleteCurtailmentResponseProfile(
		testSessionCtxWithAssignments(t, &session.Info{
			AuthMethod:     session.AuthMethodSession,
			OrganizationID: 42,
			Role:           "OPERATOR",
			SessionID:      "sess-response-profile-delete",
		}, testOrgAssignment(authz.PermCurtailmentManage), testSiteAssignment(siteID)),
		connect.NewRequest(&pb.DeleteCurtailmentResponseProfileRequest{ProfileId: 201}),
	)

	require.Error(t, err)
	assert.Zero(t, store.deletedProfileID)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
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

func TestHandler_ResponseProfileAdminCanCreateAllPairedPolicy(t *testing.T) {
	t.Parallel()

	store := newHandlerResponseProfileStore()
	h := NewHandlerWithResponseProfiles(nil, domainCurtailment.NewResponseProfileService(store))

	resp, err := h.CreateCurtailmentResponseProfile(
		startSessionCtxWithPerms(t, 42, domainAuth.AdminRoleName, authz.PermCurtailmentManage),
		connect.NewRequest(&pb.CreateCurtailmentResponseProfileRequest{
			ProfileName:                 "All paired shed",
			Mode:                        pb.CurtailmentMode_CURTAILMENT_MODE_FULL_FLEET,
			ForceIncludeAllPairedMiners: true,
		}),
	)

	require.NoError(t, err)
	require.NotNil(t, resp.Msg.GetProfile())
	require.NotNil(t, store.created)
	assert.True(t, store.created.ForceIncludeAllPairedMiners)
	assert.True(t, resp.Msg.GetProfile().GetForceIncludeAllPairedMiners())
}

type handlerResponseProfileStore struct {
	siteBelongs             bool
	siteCheckCount          int
	created                 *models.ResponseProfile
	updated                 *models.ResponseProfile
	updateExpectedSiteID    *int64
	updateExpectedScopeJSON []byte
	deletedProfileID        int64
	deleteExpectedSiteID    *int64
	deleteExpectedScopeJSON []byte
	profiles                []*models.ResponseProfile
	deviceSites             map[string]*int64
	infrastructureDevices   map[int64]models.ResponseProfileInfrastructureDevice
}

func newHandlerResponseProfileStore() *handlerResponseProfileStore {
	return &handlerResponseProfileStore{
		siteBelongs:           true,
		infrastructureDevices: map[int64]models.ResponseProfileInfrastructureDevice{},
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

func (s *handlerResponseProfileStore) ListResponseProfileDeviceSites(_ context.Context, _ int64, deviceIdentifiers []string) (map[string]*int64, error) {
	out := make(map[string]*int64, len(deviceIdentifiers))
	for _, deviceIdentifier := range deviceIdentifiers {
		siteID, ok := s.deviceSites[deviceIdentifier]
		if !ok {
			continue
		}
		out[deviceIdentifier] = cloneInt64Ptr(siteID)
	}
	return out, nil
}

func (s *handlerResponseProfileStore) ListResponseProfileInfrastructureDevices(_ context.Context, _ int64, deviceIDs []int64) (map[int64]models.ResponseProfileInfrastructureDevice, error) {
	out := make(map[int64]models.ResponseProfileInfrastructureDevice, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		if device, ok := s.infrastructureDevices[deviceID]; ok {
			out[deviceID] = device
		}
	}
	return out, nil
}

func (s *handlerResponseProfileStore) CreateResponseProfile(_ context.Context, profile models.ResponseProfile, _ map[int64]models.ResponseProfileInfrastructureDevice) (*models.ResponseProfile, error) {
	profile.ID = 201
	s.created = &profile
	return &profile, nil
}

func (s *handlerResponseProfileStore) UpdateResponseProfile(_ context.Context, profile models.ResponseProfile, _ map[int64]models.ResponseProfileInfrastructureDevice, expectedSiteID *int64, expectedScopeJSON []byte) (*models.ResponseProfile, error) {
	s.updated = &profile
	s.updateExpectedSiteID = cloneInt64Ptr(expectedSiteID)
	s.updateExpectedScopeJSON = cloneBytes(expectedScopeJSON)
	return &profile, nil
}

func (s *handlerResponseProfileStore) DeleteResponseProfile(_ context.Context, _ int64, profileID int64, expectedSiteID *int64, expectedScopeJSON []byte) error {
	s.deletedProfileID = profileID
	s.deleteExpectedSiteID = cloneInt64Ptr(expectedSiteID)
	s.deleteExpectedScopeJSON = cloneBytes(expectedScopeJSON)
	return nil
}

func (*handlerResponseProfileStore) CountAutomationRulesByResponseProfile(context.Context, int64, int64) (int64, error) {
	return 0, nil
}

func (s *handlerResponseProfileStore) SiteBelongsToOrg(context.Context, int64, int64) (bool, error) {
	s.siteCheckCount++
	return s.siteBelongs, nil
}

func ptrHandlerInt64(v int64) *int64 {
	return &v
}

func ptrFloat64(v float64) *float64 {
	return &v
}

func ptrUint32(v uint32) *uint32 {
	return &v
}
