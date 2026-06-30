package sqlstores_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestCohortStore_CreateGetListAndRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	siteStore := sqlstores.NewSQLSiteStore(tc.DatabaseService.DB)
	ctx := t.Context()

	site, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: user.OrganizationID, Name: "Cohort Site"})
	require.NoError(t, err)
	deviceA := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	deviceB := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	_, err = siteStore.AssignDevicesToSite(ctx, user.OrganizationID, &site.ID, []string{deviceA.ID, deviceB.ID})
	require.NoError(t, err)
	setDeviceDisplayFields(t, tc, user.OrganizationID, deviceA.ID, "Rig A", "worker-a", "SN-A")

	firmwareFileID := "firmware-file-1"
	ownerUsername := user.Username
	expiresAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Microsecond)
	idempotencyKey := "reservation-create-get-list"

	created, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:                 user.OrganizationID,
		Label:                 "PR 1247 test",
		OwnerUserID:           &user.DatabaseID,
		OwnerUsername:         &ownerUsername,
		ExpiresAt:             &expiresAt,
		DesiredFirmwareFileID: &firmwareFileID,
		Purpose:               "agent test",
		SourceActorType:       models.SourceActorUser,
		SourceActorID:         &ownerUsername,
		IdempotencyKey:        &idempotencyKey,
		DeviceIdentifiers:     []string{deviceA.ID, deviceB.ID},
	})
	require.NoError(t, err)
	assert.False(t, created.IsDefault)
	assert.Equal(t, models.CohortStateActive, created.State)
	assert.Equal(t, int64(2), created.ExplicitMemberCount)
	require.Len(t, created.Members, 2)
	for _, member := range created.Members {
		require.NotNil(t, member.SiteID)
		assert.Equal(t, site.ID, *member.SiteID)
	}
	memberA := requireCohortMember(t, created.Members, deviceA.ID)
	assert.Equal(t, "Rig A", memberA.Display.Name)
	assert.Equal(t, "worker-a", memberA.Display.WorkerName)
	assert.Equal(t, "TestCorp", memberA.Display.Manufacturer)
	assert.Equal(t, "TestMiner", memberA.Display.Model)
	assert.NotEmpty(t, memberA.Display.IPAddress)
	assert.Equal(t, "SN-A", memberA.Display.SerialNumber)
	assert.Equal(t, "Cohort Site", memberA.Display.SiteLabel)

	fetched, err := store.GetCohort(ctx, user.OrganizationID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, fetched.ID)
	assert.Equal(t, "PR 1247 test", fetched.Label)
	assert.Equal(t, "agent test", fetched.Purpose)
	require.NotNil(t, fetched.DesiredFirmwareFileID)
	assert.Equal(t, firmwareFileID, *fetched.DesiredFirmwareFileID)
	require.NotNil(t, fetched.OwnerUserID)
	assert.Equal(t, user.DatabaseID, *fetched.OwnerUserID)
	require.NotNil(t, fetched.OwnerUsername)
	assert.Equal(t, ownerUsername, *fetched.OwnerUsername)
	assert.Equal(t, int64(2), fetched.ExplicitMemberCount)
	require.Len(t, fetched.Members, 2)
	fetchedMemberA := requireCohortMember(t, fetched.Members, deviceA.ID)
	assert.Equal(t, "Rig A", fetchedMemberA.Display.Name)
	assert.Equal(t, "Cohort Site", fetchedMemberA.Display.SiteLabel)

	listed, err := store.ListCohorts(ctx, models.ListCohortsParams{OrgID: user.OrganizationID})
	require.NoError(t, err)
	require.Len(t, listed.Cohorts, 2) // the org default cohort plus the created cohort
	userCohorts := nonDefaultCohorts(listed.Cohorts)
	require.Len(t, userCohorts, 1)
	assert.Equal(t, created.ID, userCohorts[0].ID)
	assert.Equal(t, int64(2), userCohorts[0].ExplicitMemberCount)

	owned, err := store.ListCohortsByOwner(ctx, models.ListCohortsByOwnerParams{
		OrgID:       user.OrganizationID,
		OwnerUserID: user.DatabaseID,
	})
	require.NoError(t, err)
	require.Len(t, owned.Cohorts, 1)
	assert.Equal(t, created.ID, owned.Cohorts[0].ID)

	released, err := store.ReleaseCohort(ctx, user.OrganizationID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, models.CohortStateReleased, released.State)
	assert.Equal(t, int64(0), released.ExplicitMemberCount)
	assert.Empty(t, released.Members)

	active, err := store.ListCohorts(ctx, models.ListCohortsParams{OrgID: user.OrganizationID})
	require.NoError(t, err)
	assert.Empty(t, nonDefaultCohorts(active.Cohorts)) // only the org default cohort remains active

	withReleased, err := store.ListCohorts(ctx, models.ListCohortsParams{
		OrgID:           user.OrganizationID,
		IncludeReleased: true,
	})
	require.NoError(t, err)
	releasedUserCohorts := nonDefaultCohorts(withReleased.Cohorts)
	require.Len(t, releasedUserCohorts, 1)
	assert.Equal(t, created.ID, releasedUserCohorts[0].ID)
	assert.Equal(t, models.CohortStateReleased, releasedUserCohorts[0].State)
}

func TestCohortStore_UpdateDefaultCohortFirmware(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()

	listed, err := store.ListCohorts(ctx, models.ListCohortsParams{OrgID: user.OrganizationID})
	require.NoError(t, err)
	var defaultCohort *models.Cohort
	for _, cohort := range listed.Cohorts {
		if cohort.IsDefault {
			defaultCohort = cohort
			break
		}
	}
	require.NotNil(t, defaultCohort)

	firmwareFileID := "default-fw"
	updated, err := store.UpdateDefaultCohortFirmware(ctx, models.UpdateCohortParams{
		OrgID:                 user.OrganizationID,
		CohortID:              defaultCohort.ID,
		DesiredFirmwareFileID: &firmwareFileID,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.DesiredFirmwareFileID)
	assert.Equal(t, firmwareFileID, *updated.DesiredFirmwareFileID)

	cleared, err := store.UpdateDefaultCohortFirmware(ctx, models.UpdateCohortParams{
		OrgID:    user.OrganizationID,
		CohortID: defaultCohort.ID,
	})
	require.NoError(t, err)
	assert.Nil(t, cleared.DesiredFirmwareFileID)
}

func TestCohortStore_SetCohortFirmwareTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()

	listed, err := store.ListCohorts(ctx, models.ListCohortsParams{OrgID: user.OrganizationID})
	require.NoError(t, err)
	var defaultCohort *models.Cohort
	for _, cohort := range listed.Cohorts {
		if cohort.IsDefault {
			defaultCohort = cohort
			break
		}
	}
	require.NotNil(t, defaultCohort)

	protoFirmwareFileID := "proto-fw"
	antminerFirmwareFileID := "antminer-fw"
	updatedDefault, err := store.SetCohortFirmwareTarget(ctx, models.SetCohortFirmwareTargetParams{
		OrgID:          user.OrganizationID,
		CohortID:       defaultCohort.ID,
		Manufacturer:   "TestCorp",
		Model:          "TestMiner",
		FirmwareFileID: &protoFirmwareFileID,
	})
	require.NoError(t, err)
	require.Len(t, updatedDefault.FirmwareTargets, 1)
	assert.Equal(t, "TestCorp", updatedDefault.FirmwareTargets[0].Manufacturer)
	require.NotNil(t, updatedDefault.FirmwareTargets[0].FirmwareFileID)
	assert.Equal(t, protoFirmwareFileID, *updatedDefault.FirmwareTargets[0].FirmwareFileID)

	updatedDefault, err = store.SetCohortFirmwareTarget(ctx, models.SetCohortFirmwareTargetParams{
		OrgID:          user.OrganizationID,
		CohortID:       defaultCohort.ID,
		Manufacturer:   "Bitmain",
		Model:          "S21",
		FirmwareFileID: &antminerFirmwareFileID,
	})
	require.NoError(t, err)
	require.Len(t, updatedDefault.FirmwareTargets, 2)

	updatedDefault, err = store.SetCohortFirmwareTarget(ctx, models.SetCohortFirmwareTargetParams{
		OrgID:        user.OrganizationID,
		CohortID:     defaultCohort.ID,
		Manufacturer: "TestCorp",
		Model:        "TestMiner",
	})
	require.NoError(t, err)
	require.Len(t, updatedDefault.FirmwareTargets, 1)
	assert.Equal(t, "Bitmain", updatedDefault.FirmwareTargets[0].Manufacturer)
	require.NotNil(t, updatedDefault.FirmwareTargets[0].FirmwareFileID)
	assert.Equal(t, antminerFirmwareFileID, *updatedDefault.FirmwareTargets[0].FirmwareFileID)

	device := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	cohortFirmwareFileID := "cohort-fw"
	created, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "targeted firmware",
		Purpose:           "single cohort target",
		SourceActorType:   models.SourceActorUser,
		DeviceIdentifiers: []string{device.ID},
	})
	require.NoError(t, err)

	updatedCohort, err := store.SetCohortFirmwareTarget(ctx, models.SetCohortFirmwareTargetParams{
		OrgID:          user.OrganizationID,
		CohortID:       created.ID,
		Manufacturer:   "TestCorp",
		Model:          "TestMiner",
		FirmwareFileID: &cohortFirmwareFileID,
	})
	require.NoError(t, err)
	require.NotNil(t, updatedCohort.DesiredFirmwareFileID)
	assert.Equal(t, cohortFirmwareFileID, *updatedCohort.DesiredFirmwareFileID)
	require.Len(t, updatedCohort.FirmwareTargets, 1)
	require.NotNil(t, updatedCohort.FirmwareTargets[0].FirmwareFileID)
	assert.Equal(t, cohortFirmwareFileID, *updatedCohort.FirmwareTargets[0].FirmwareFileID)
}

func TestCohortStore_RejectsDuplicateDeviceMembership(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()
	device := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")

	_, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "first",
		Purpose:           "first reservation",
		SourceActorType:   models.SourceActorUser,
		DeviceIdentifiers: []string{device.ID},
	})
	require.NoError(t, err)

	_, err = store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "second",
		Purpose:           "second reservation",
		SourceActorType:   models.SourceActorUser,
		DeviceIdentifiers: []string{device.ID},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "expected AlreadyExists, got %v", err)
}

func TestCohortStore_CreateRejectsMismatchedDesiredFirmwareTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()
	device := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")

	firmwareFileID := "firmware-file-1"
	_, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:                             user.OrganizationID,
		Label:                             "wrong firmware",
		Purpose:                           "target mismatch",
		DesiredFirmwareFileID:             &firmwareFileID,
		DesiredFirmwareTargetManufacturer: "OtherCorp",
		DesiredFirmwareTargetModel:        "OtherMiner",
		SourceActorType:                   models.SourceActorUser,
		DeviceIdentifiers:                 []string{device.ID},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "expected InvalidArgument, got %v", err)

	listed, err := store.ListCohorts(ctx, models.ListCohortsParams{OrgID: user.OrganizationID, IncludeReleased: true})
	require.NoError(t, err)
	assert.Empty(t, nonDefaultCohorts(listed.Cohorts), "failed create should roll back cohort row")
}

func TestCohortStore_CreateRejectsMixedMinerTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()
	deviceA := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	deviceB := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	setDiscoveredDeviceShape(t, tc, user.OrganizationID, deviceB.ID, "OtherCorp", "OtherMiner")

	_, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "mixed cohort",
		Purpose:           "mixed hardware",
		SourceActorType:   models.SourceActorUser,
		DeviceIdentifiers: []string{deviceA.ID, deviceB.ID},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "expected InvalidArgument, got %v", err)

	listed, err := store.ListCohorts(ctx, models.ListCohortsParams{OrgID: user.OrganizationID, IncludeReleased: true})
	require.NoError(t, err)
	assert.Empty(t, nonDefaultCohorts(listed.Cohorts), "failed create should roll back cohort row")
}

func TestCohortStore_MoveRejectsMismatchedDesiredFirmwareTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()
	deviceA := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	deviceB := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	setDiscoveredDeviceShape(t, tc, user.OrganizationID, deviceB.ID, "OtherCorp", "OtherMiner")

	firmwareFileID := "firmware-file-1"
	created, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:                             user.OrganizationID,
		Label:                             "firmware cohort",
		Purpose:                           "targeted firmware",
		DesiredFirmwareFileID:             &firmwareFileID,
		DesiredFirmwareTargetManufacturer: "TestCorp",
		DesiredFirmwareTargetModel:        "TestMiner",
		SourceActorType:                   models.SourceActorUser,
		DeviceIdentifiers:                 []string{deviceA.ID},
	})
	require.NoError(t, err)

	_, err = store.MoveDevicesToCohort(ctx, models.MembershipMutationParams{
		OrgID:                             user.OrganizationID,
		CohortID:                          created.ID,
		DesiredFirmwareTargetManufacturer: "TestCorp",
		DesiredFirmwareTargetModel:        "TestMiner",
		DeviceIdentifiers:                 []string{deviceB.ID},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "expected InvalidArgument, got %v", err)

	fetched, err := store.GetCohort(ctx, user.OrganizationID, created.ID)
	require.NoError(t, err)
	require.Len(t, fetched.Members, 1, "failed move should roll back cohort membership")
	assert.Equal(t, deviceA.ID, fetched.Members[0].DeviceIdentifier)
}

func TestCohortStore_MoveRejectsMixedMinerTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()
	deviceA := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	deviceB := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	setDiscoveredDeviceShape(t, tc, user.OrganizationID, deviceB.ID, "OtherCorp", "OtherMiner")

	created, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "single type",
		Purpose:           "initial hardware",
		SourceActorType:   models.SourceActorUser,
		DeviceIdentifiers: []string{deviceA.ID},
	})
	require.NoError(t, err)

	_, err = store.MoveDevicesToCohort(ctx, models.MembershipMutationParams{
		OrgID:             user.OrganizationID,
		CohortID:          created.ID,
		DeviceIdentifiers: []string{deviceB.ID},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "expected InvalidArgument, got %v", err)

	fetched, err := store.GetCohort(ctx, user.OrganizationID, created.ID)
	require.NoError(t, err)
	require.Len(t, fetched.Members, 1, "failed move should roll back cohort membership")
	assert.Equal(t, deviceA.ID, fetched.Members[0].DeviceIdentifier)
}

func TestCohortStore_CreateWithSelectorAllocatesDefaultDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	siteStore := sqlstores.NewSQLSiteStore(tc.DatabaseService.DB)
	ctx := t.Context()

	siteA, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: user.OrganizationID, Name: "Site A"})
	require.NoError(t, err)
	siteB, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: user.OrganizationID, Name: "Site B"})
	require.NoError(t, err)

	match := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	otherProduct := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	otherSite := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	_, err = siteStore.AssignDevicesToSite(ctx, user.OrganizationID, &siteA.ID, []string{match.ID, otherProduct.ID})
	require.NoError(t, err)
	_, err = siteStore.AssignDevicesToSite(ctx, user.OrganizationID, &siteB.ID, []string{otherSite.ID})
	require.NoError(t, err)
	setDiscoveredDeviceShape(t, tc, user.OrganizationID, otherProduct.ID, "OtherCorp", "TestMiner")

	product := "TestCorp"
	model := "TestMiner"
	created, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:           user.OrganizationID,
		Label:           "selected",
		Purpose:         "selector allocation",
		SourceActorType: models.SourceActorUser,
		DeviceSelector: &models.CohortDeviceSelector{
			Count:   1,
			Product: &product,
			Model:   &model,
			SiteID:  &siteA.ID,
		},
	})
	require.NoError(t, err)
	require.Len(t, created.Members, 1)
	assert.Equal(t, match.ID, created.Members[0].DeviceIdentifier)
	require.NotNil(t, created.Members[0].SiteID)
	assert.Equal(t, siteA.ID, *created.Members[0].SiteID)

	_, err = store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:           user.OrganizationID,
		Label:           "selected again",
		Purpose:         "selector allocation",
		SourceActorType: models.SourceActorUser,
		DeviceSelector: &models.CohortDeviceSelector{
			Count:   1,
			Product: &product,
			Model:   &model,
			SiteID:  &siteA.ID,
		},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "expected AlreadyExists, got %v", err)

	listed, err := store.ListCohorts(ctx, models.ListCohortsParams{OrgID: user.OrganizationID})
	require.NoError(t, err)
	assert.Len(t, nonDefaultCohorts(listed.Cohorts), 1, "failed selector allocation should roll back the cohort row")
}

func TestCohortStore_RemoveDevicesAndGetCohortIsAtomic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()

	deviceA := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	deviceB := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	nonMember := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")

	created, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "remove test",
		Purpose:           "remove atomically",
		SourceActorType:   models.SourceActorUser,
		DeviceIdentifiers: []string{deviceA.ID, deviceB.ID},
	})
	require.NoError(t, err)

	_, err = store.RemoveDevicesAndGetCohort(ctx, models.MembershipMutationParams{
		OrgID:             user.OrganizationID,
		CohortID:          created.ID,
		DeviceIdentifiers: []string{deviceA.ID, nonMember.ID},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err), "expected NotFound, got %v", err)

	fetched, err := store.GetCohort(ctx, user.OrganizationID, created.ID)
	require.NoError(t, err)
	assert.Len(t, fetched.Members, 2, "partial remove must roll back")

	updated, err := store.RemoveDevicesAndGetCohort(ctx, models.MembershipMutationParams{
		OrgID:             user.OrganizationID,
		CohortID:          created.ID,
		DeviceIdentifiers: []string{deviceA.ID},
	})
	require.NoError(t, err)
	require.Len(t, updated.Members, 1)
	assert.Equal(t, deviceB.ID, updated.Members[0].DeviceIdentifier)
}

func TestCohortStore_IdempotencyKeyIsOrgScoped(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	otherUser := tc.DatabaseService.CreateSuperAdminUser2()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()

	key := "same-key"
	_, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:           user.OrganizationID,
		Label:           "first",
		Purpose:         "first reservation",
		SourceActorType: models.SourceActorUser,
		IdempotencyKey:  &key,
	})
	require.NoError(t, err)

	_, err = store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:           user.OrganizationID,
		Label:           "duplicate",
		Purpose:         "duplicate reservation",
		SourceActorType: models.SourceActorUser,
		IdempotencyKey:  &key,
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "expected AlreadyExists, got %v", err)

	_, err = store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:           otherUser.OrganizationID,
		Label:           "other org",
		Purpose:         "same idempotency key in another org",
		SourceActorType: models.SourceActorUser,
		IdempotencyKey:  &key,
	})
	require.NoError(t, err)
}

func TestCohortStore_ActiveLabelIsUniquePerOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	otherUser := tc.DatabaseService.CreateSuperAdminUser2()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()

	created, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:           user.OrganizationID,
		Label:           "Firmware Test",
		Purpose:         "first reservation",
		SourceActorType: models.SourceActorUser,
	})
	require.NoError(t, err)

	_, err = store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:           user.OrganizationID,
		Label:           " firmware test ",
		Purpose:         "duplicate reservation",
		SourceActorType: models.SourceActorUser,
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "expected AlreadyExists, got %v", err)

	_, err = store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:           otherUser.OrganizationID,
		Label:           "Firmware Test",
		Purpose:         "same label in another org",
		SourceActorType: models.SourceActorUser,
	})
	require.NoError(t, err)

	_, err = store.ReleaseCohort(ctx, user.OrganizationID, created.ID)
	require.NoError(t, err)

	_, err = store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:           user.OrganizationID,
		Label:           "Firmware Test",
		Purpose:         "name reused after release",
		SourceActorType: models.SourceActorUser,
	})
	require.NoError(t, err)
}

// nonDefaultCohorts filters out the always-present is_default cohort (seeded on
// org creation) so assertions can target user-created cohorts.
func nonDefaultCohorts(cohorts []*models.Cohort) []*models.Cohort {
	out := make([]*models.Cohort, 0, len(cohorts))
	for _, c := range cohorts {
		if !c.IsDefault {
			out = append(out, c)
		}
	}
	return out
}

func requireCohortMember(t *testing.T, members []models.CohortMember, deviceIdentifier string) models.CohortMember {
	t.Helper()

	for _, member := range members {
		if member.DeviceIdentifier == deviceIdentifier {
			return member
		}
	}
	require.Failf(t, "missing cohort member", "device identifier %q not found", deviceIdentifier)
	return models.CohortMember{}
}

func setDiscoveredDeviceShape(t *testing.T, tc *testutil.TestContext, orgID int64, deviceIdentifier string, manufacturer string, model string) {
	t.Helper()

	result, err := tc.DatabaseService.DB.ExecContext(t.Context(), `
		UPDATE discovered_device dd
		SET manufacturer = $3,
		    model = $4
		FROM device d
		WHERE d.discovered_device_id = dd.id
		  AND d.org_id = $1
		  AND d.device_identifier = $2
		  AND dd.org_id = $1
	`, orgID, deviceIdentifier, manufacturer, model)
	require.NoError(t, err)
	affected, err := result.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)
}

func setDeviceDisplayFields(t *testing.T, tc *testutil.TestContext, orgID int64, deviceIdentifier string, customName string, workerName string, serialNumber string) {
	t.Helper()

	result, err := tc.DatabaseService.DB.ExecContext(t.Context(), `
		UPDATE device
		SET custom_name = $3,
		    worker_name = $4,
		    serial_number = $5
		WHERE org_id = $1
		  AND device_identifier = $2
		  AND deleted_at IS NULL
	`, orgID, deviceIdentifier, customName, workerName, serialNumber)
	require.NoError(t, err)
	affected, err := result.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)
}
