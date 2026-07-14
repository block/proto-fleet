package sqlstores_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
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
	ctx := t.Context()

	deviceA := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	deviceB := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	setDeviceDisplayFields(t, tc, user.OrganizationID, deviceA.ID, "Rig A", "worker-a", "SN-A")

	initialCohorts, err := store.ListCohorts(ctx, models.ListCohortsParams{OrgID: user.OrganizationID})
	require.NoError(t, err)
	assert.Equal(t, int64(2), requireDefaultCohort(t, initialCohorts.Cohorts).ExplicitMemberCount)

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
	memberA := requireCohortMember(t, created.Members, deviceA.ID)
	assert.Equal(t, "Rig A", memberA.Display.Name)
	assert.Equal(t, "worker-a", memberA.Display.WorkerName)
	assert.Equal(t, "TestCorp", memberA.Display.Manufacturer)
	assert.Equal(t, "TestMiner", memberA.Display.Model)
	assert.NotEmpty(t, memberA.Display.IPAddress)
	assert.Equal(t, "SN-A", memberA.Display.SerialNumber)

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

	listed, err := store.ListCohorts(ctx, models.ListCohortsParams{OrgID: user.OrganizationID})
	require.NoError(t, err)
	require.Len(t, listed.Cohorts, 2) // the org default cohort plus the created cohort
	assert.Equal(t, int64(0), requireDefaultCohort(t, listed.Cohorts).ExplicitMemberCount)
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
	assert.Equal(t, int64(2), requireDefaultCohort(t, active.Cohorts).ExplicitMemberCount)

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
	testCorp := "TestCorp"
	testMiner := "TestMiner"
	bitmain := "Bitmain"
	s21 := "S21"
	antminerFirmwareFileID := "antminer-fw"
	updatedDefault, err := store.SetCohortFirmwareTarget(ctx, models.SetCohortFirmwareTargetParams{
		OrgID:          user.OrganizationID,
		CohortID:       defaultCohort.ID,
		Manufacturer:   &testCorp,
		Model:          &testMiner,
		FirmwareFileID: &protoFirmwareFileID,
	})
	require.NoError(t, err)
	require.Len(t, updatedDefault.FirmwareTargets, 1)
	assert.Equal(t, "TestCorp", updatedDefault.FirmwareTargets[0].Manufacturer)
	require.NotNil(t, updatedDefault.FirmwareTargets[0].FirmwareFileID)
	assert.Equal(t, protoFirmwareFileID, *updatedDefault.FirmwareTargets[0].FirmwareFileID)

	replacementProtoFirmwareFileID := "proto-fw-replacement"
	lowerTestCorp := "testcorp"
	lowerTestMiner := "testminer"
	updatedDefault, err = store.SetCohortFirmwareTarget(ctx, models.SetCohortFirmwareTargetParams{
		OrgID:          user.OrganizationID,
		CohortID:       defaultCohort.ID,
		Manufacturer:   &lowerTestCorp,
		Model:          &lowerTestMiner,
		FirmwareFileID: &replacementProtoFirmwareFileID,
	})
	require.NoError(t, err)
	require.Len(t, updatedDefault.FirmwareTargets, 1)
	assert.Equal(t, lowerTestCorp, updatedDefault.FirmwareTargets[0].Manufacturer)
	require.NotNil(t, updatedDefault.FirmwareTargets[0].FirmwareFileID)
	assert.Equal(t, replacementProtoFirmwareFileID, *updatedDefault.FirmwareTargets[0].FirmwareFileID)

	updatedDefault, err = store.SetCohortFirmwareTarget(ctx, models.SetCohortFirmwareTargetParams{
		OrgID:          user.OrganizationID,
		CohortID:       defaultCohort.ID,
		Manufacturer:   &bitmain,
		Model:          &s21,
		FirmwareFileID: &antminerFirmwareFileID,
	})
	require.NoError(t, err)
	require.Len(t, updatedDefault.FirmwareTargets, 2)

	updatedDefault, err = store.SetCohortFirmwareTarget(ctx, models.SetCohortFirmwareTargetParams{
		OrgID:        user.OrganizationID,
		CohortID:     defaultCohort.ID,
		Manufacturer: &testCorp,
		Model:        &testMiner,
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
		Manufacturer:   &testCorp,
		Model:          &testMiner,
		FirmwareFileID: &cohortFirmwareFileID,
	})
	require.NoError(t, err)
	require.NotNil(t, updatedCohort.DesiredFirmwareFileID)
	assert.Equal(t, cohortFirmwareFileID, *updatedCohort.DesiredFirmwareFileID)
	require.Len(t, updatedCohort.FirmwareTargets, 1)
	require.NotNil(t, updatedCohort.FirmwareTargets[0].FirmwareFileID)
	assert.Equal(t, cohortFirmwareFileID, *updatedCohort.FirmwareTargets[0].FirmwareFileID)

	cleared, err := store.ClearMissingFirmwareTarget(ctx, user.OrganizationID, cohortFirmwareFileID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), cleared)

	clearedCohort, err := store.GetCohort(ctx, user.OrganizationID, created.ID)
	require.NoError(t, err)
	assert.Nil(t, clearedCohort.DesiredFirmwareFileID)
	assert.Empty(t, clearedCohort.FirmwareTargets)
}

func TestCohortStore_ReadsFirmwareStatusesFromExistingState(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()

	deviceA := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	deviceB := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	setDiscoveredDeviceShape(t, tc, user.OrganizationID, deviceA.ID, "Proto", "Rig")
	setDiscoveredDeviceShape(t, tc, user.OrganizationID, deviceB.ID, "Proto", "Rig")
	upsertObservedFirmware(t, tc, user.OrganizationID, deviceA.ID, "1.0.0")
	upsertObservedFirmware(t, tc, user.OrganizationID, deviceB.ID, "1.2.0")
	upsertDeviceStatus(t, tc, user.OrganizationID, deviceA.ID, "UPDATING")

	created, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "firmware visibility",
		Purpose:           "status read path",
		SourceActorType:   models.SourceActorUser,
		DeviceIdentifiers: []string{deviceA.ID, deviceB.ID},
	})
	require.NoError(t, err)

	manufacturer := "proto"
	model := "rig"
	firmwareFileID := "fw-1.2.0"
	_, err = store.SetCohortFirmwareTarget(ctx, models.SetCohortFirmwareTargetParams{
		OrgID:          user.OrganizationID,
		CohortID:       created.ID,
		Manufacturer:   &manufacturer,
		Model:          &model,
		FirmwareFileID: &firmwareFileID,
	})
	require.NoError(t, err)

	dispatchAt := time.Date(2026, 7, 9, 15, 30, 0, 0, time.UTC)
	claimed, err := store.ClaimFirmwareDispatch(ctx, models.ClaimFirmwareDispatchParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceA.ID,
		DesiredFirmwareFileID:  firmwareFileID,
		DesiredFirmwareVersion: "1.2.0",
		DispatchingBefore:      dispatchAt.Add(-time.Hour),
	})
	require.NoError(t, err)
	require.True(t, claimed)
	dispatched, err := store.MarkFirmwareDispatched(ctx, models.MarkFirmwareDispatchedParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceA.ID,
		DesiredFirmwareFileID:  firmwareFileID,
		DesiredFirmwareVersion: "1.2.0",
		LastBatchUUID:          "batch-1",
		LastDispatchedAt:       dispatchAt,
	})
	require.NoError(t, err)
	require.True(t, dispatched)
	claimed, err = store.ClaimFirmwareDispatch(ctx, models.ClaimFirmwareDispatchParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceB.ID,
		DesiredFirmwareFileID:  firmwareFileID,
		DesiredFirmwareVersion: "1.2.0",
		DispatchingBefore:      dispatchAt.Add(-time.Hour),
	})
	require.NoError(t, err)
	require.True(t, claimed)
	confirmed, err := store.MarkFirmwareConfirmed(ctx, models.MarkFirmwareConfirmedParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceB.ID,
		DesiredFirmwareFileID:  firmwareFileID,
		DesiredFirmwareVersion: "1.2.0",
		ConfirmedAt:            dispatchAt.Add(time.Minute),
		ObservedAt:             dispatchAt.Add(time.Minute),
	})
	require.NoError(t, err)
	require.True(t, confirmed)

	fetched, err := store.GetCohort(ctx, user.OrganizationID, created.ID)
	require.NoError(t, err)
	memberA := requireCohortMember(t, fetched.Members, deviceA.ID)
	require.NotNil(t, memberA.FirmwareStatus)
	assert.Equal(t, firmwareFileID, memberA.FirmwareStatus.TargetFirmwareFileID)
	assert.Equal(t, "1.2.0", memberA.FirmwareStatus.TargetFirmwareVersion)
	assert.Equal(t, "1.0.0", memberA.FirmwareStatus.CurrentFirmwareVersion)
	require.NotNil(t, memberA.FirmwareStatus.EnforcementState)
	assert.Equal(t, models.EnforcementStateDispatched, *memberA.FirmwareStatus.EnforcementState)
	assert.Equal(t, "UPDATING", memberA.FirmwareStatus.DeviceStatus)
	require.NotNil(t, memberA.FirmwareStatus.LastDispatchedAt)
	assert.True(t, dispatchAt.Equal(*memberA.FirmwareStatus.LastDispatchedAt))

	memberB := requireCohortMember(t, fetched.Members, deviceB.ID)
	require.NotNil(t, memberB.FirmwareStatus)
	assert.Equal(t, "1.2.0", memberB.FirmwareStatus.CurrentFirmwareVersion)
	require.NotNil(t, memberB.FirmwareStatus.ConfirmedAt)

	listed, err := store.ListCohorts(ctx, models.ListCohortsParams{OrgID: user.OrganizationID})
	require.NoError(t, err)
	listedCohort := nonDefaultCohorts(listed.Cohorts)[0]
	require.Len(t, listedCohort.FirmwareStatuses, 2)

	devices, err := store.ListDevices(ctx, models.ListDevicesParams{
		OrgID:    user.OrganizationID,
		PageSize: 20,
	})
	require.NoError(t, err)
	listedDeviceA := requireCohortDevice(t, devices.Devices, deviceA.ID)
	require.NotNil(t, listedDeviceA.FirmwareStatus)
	assert.Equal(t, firmwareFileID, listedDeviceA.FirmwareStatus.TargetFirmwareFileID)
	assert.Equal(t, "1.2.0", listedDeviceA.FirmwareStatus.TargetFirmwareVersion)
}

func TestCohortStore_FirmwareVersionEventsRecordTransitionsOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()
	device := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	created, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "firmware history",
		Purpose:           "version event query",
		SourceActorType:   models.SourceActorUser,
		DeviceIdentifiers: []string{device.ID},
	})
	require.NoError(t, err)

	start := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	upsertObservedFirmwareAt(t, tc, user.OrganizationID, device.ID, "1.0.0", start.Add(-30*time.Minute))
	upsertObservedFirmwareAt(t, tc, user.OrganizationID, device.ID, "1.0.0", start.Add(-20*time.Minute))
	upsertObservedFirmwareAt(t, tc, user.OrganizationID, device.ID, "2.0.0", start.Add(10*time.Minute))

	events, err := store.ListCohortFirmwareVersionEvents(ctx, user.OrganizationID, created.ID, start, start.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, "1.0.0", events[0].FirmwareVersion)
	assert.True(t, start.Add(-30*time.Minute).Equal(events[0].ObservedAt))
	assert.Equal(t, "2.0.0", events[1].FirmwareVersion)
	assert.True(t, start.Add(10*time.Minute).Equal(events[1].ObservedAt))

	var eventCount int
	err = tc.DatabaseService.DB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM device_firmware_version_event
		WHERE org_id = $1 AND device_identifier = $2
	`, user.OrganizationID, device.ID).Scan(&eventCount)
	require.NoError(t, err)
	assert.Equal(t, 2, eventCount)
}

func TestCohortStore_MarkFirmwareConfirmedClearsStaleDispatchForNewTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()

	deviceIdentifier := "firmware-confirm-target-change"
	oldFileID := "firmware-1.3.5"
	oldVersion := "1.3.5"
	newFileID := "firmware-1.3.6"
	newVersion := "1.3.6"
	dispatchAt := time.Date(2026, 7, 2, 18, 6, 35, 0, time.UTC)

	claimed, err := store.ClaimFirmwareDispatch(ctx, models.ClaimFirmwareDispatchParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  oldFileID,
		DesiredFirmwareVersion: oldVersion,
		DispatchingBefore:      dispatchAt.Add(-time.Minute),
	})
	require.NoError(t, err)
	require.True(t, claimed)

	dispatched, err := store.MarkFirmwareDispatched(ctx, models.MarkFirmwareDispatchedParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  oldFileID,
		DesiredFirmwareVersion: oldVersion,
		LastBatchUUID:          "old-batch",
		LastDispatchedAt:       dispatchAt,
	})
	require.NoError(t, err)
	require.True(t, dispatched)

	confirmed, err := store.MarkFirmwareConfirmed(ctx, models.MarkFirmwareConfirmedParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  oldFileID,
		DesiredFirmwareVersion: oldVersion,
		ConfirmedAt:            dispatchAt.Add(time.Minute),
		ObservedAt:             dispatchAt.Add(time.Minute),
	})
	require.NoError(t, err)
	require.True(t, confirmed)

	row := readFirmwareEnforcementState(t, tc.DatabaseService.DB, user.OrganizationID, deviceIdentifier)
	assert.Equal(t, "confirmed", row.state)
	assert.Equal(t, oldFileID, row.desiredFileID)
	assert.Equal(t, oldVersion, row.desiredVersion)
	assert.True(t, row.lastBatchUUID.Valid)
	assert.True(t, row.lastDispatchedAt.Valid)

	confirmed, err = store.MarkFirmwareConfirmed(ctx, models.MarkFirmwareConfirmedParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  newFileID,
		DesiredFirmwareVersion: newVersion,
		ConfirmedAt:            dispatchAt.Add(2 * time.Minute),
		ObservedAt:             dispatchAt.Add(2 * time.Minute),
	})
	require.NoError(t, err)
	require.True(t, confirmed)

	row = readFirmwareEnforcementState(t, tc.DatabaseService.DB, user.OrganizationID, deviceIdentifier)
	assert.Equal(t, "confirmed", row.state)
	assert.Equal(t, newFileID, row.desiredFileID)
	assert.Equal(t, newVersion, row.desiredVersion)
	assert.False(t, row.lastBatchUUID.Valid)
	assert.False(t, row.lastDispatchedAt.Valid)
}

func TestCohortStore_ClaimFirmwareDispatchResetsRetryAndTargetScopedFieldsOnTargetChange(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()

	deviceIdentifier := "firmware-claim-target-change"
	oldFileID := "firmware-1.0.0"
	oldVersion := "1.0.0"
	newFileID := "firmware-2.0.0"
	newVersion := "2.0.0"
	dispatchAt := time.Date(2026, 7, 2, 19, 0, 0, 0, time.UTC)

	claimed, err := store.ClaimFirmwareDispatch(ctx, models.ClaimFirmwareDispatchParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  oldFileID,
		DesiredFirmwareVersion: oldVersion,
		DispatchingBefore:      dispatchAt.Add(-time.Hour),
	})
	require.NoError(t, err)
	require.True(t, claimed)

	_, err = tc.DatabaseService.DB.ExecContext(ctx, `
		UPDATE device_enforcement_state
		SET state = 'failed',
		    retry_count = 4,
		    last_batch_uuid = 'stale-batch',
		    last_dispatched_at = $3,
		    confirmed_at = $3,
		    observed_at = $3,
		    last_error = 'old failure'
		WHERE org_id = $1
		  AND device_identifier = $2
		  AND dimension = 'firmware'
	`, user.OrganizationID, deviceIdentifier, dispatchAt)
	require.NoError(t, err)

	claimed, err = store.ClaimFirmwareDispatch(ctx, models.ClaimFirmwareDispatchParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  newFileID,
		DesiredFirmwareVersion: newVersion,
		DispatchingBefore:      dispatchAt.Add(-time.Hour),
	})
	require.NoError(t, err)
	require.True(t, claimed)

	row := readFirmwareEnforcementState(t, tc.DatabaseService.DB, user.OrganizationID, deviceIdentifier)
	assert.Equal(t, "dispatching", row.state)
	assert.Equal(t, newFileID, row.desiredFileID)
	assert.Equal(t, newVersion, row.desiredVersion)
	assert.Equal(t, int32(0), row.retryCount)
	assert.False(t, row.lastBatchUUID.Valid)
	assert.False(t, row.lastDispatchedAt.Valid)
	assert.False(t, row.confirmedAt.Valid)
	assert.False(t, row.observedAt.Valid)
	assert.False(t, row.lastError.Valid)
}

func TestCohortStore_ClaimFirmwareDispatchDoesNotReclaimFreshDispatchingState(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()

	deviceIdentifier := "firmware-dispatch-timeout"
	fileID := "firmware-1.0.0"
	version := "1.0.0"

	claimed, err := store.ClaimFirmwareDispatch(ctx, models.ClaimFirmwareDispatchParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  fileID,
		DesiredFirmwareVersion: version,
		DispatchingBefore:      time.Now().Add(-time.Hour),
	})
	require.NoError(t, err)
	require.True(t, claimed)

	claimed, err = store.ClaimFirmwareDispatch(ctx, models.ClaimFirmwareDispatchParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  fileID,
		DesiredFirmwareVersion: version,
		DispatchingBefore:      time.Now().Add(-time.Minute),
	})
	require.NoError(t, err)
	assert.False(t, claimed)

	claimed, err = store.ClaimFirmwareDispatch(ctx, models.ClaimFirmwareDispatchParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  fileID,
		DesiredFirmwareVersion: version,
		DispatchingBefore:      time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	assert.True(t, claimed)
}

func TestCohortStore_DispatchCompletionAndFailureAreTargetGuarded(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()

	deviceIdentifier := "firmware-target-guard"
	oldFileID := "firmware-old"
	oldVersion := "1.0.0"
	newFileID := "firmware-new"
	newVersion := "2.0.0"
	now := time.Date(2026, 7, 2, 20, 0, 0, 0, time.UTC)

	claimed, err := store.ClaimFirmwareDispatch(ctx, models.ClaimFirmwareDispatchParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  newFileID,
		DesiredFirmwareVersion: newVersion,
		DispatchingBefore:      now.Add(-time.Hour),
	})
	require.NoError(t, err)
	require.True(t, claimed)

	dispatched, err := store.MarkFirmwareDispatched(ctx, models.MarkFirmwareDispatchedParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  oldFileID,
		DesiredFirmwareVersion: oldVersion,
		LastBatchUUID:          "old-batch",
		LastDispatchedAt:       now,
	})
	require.NoError(t, err)
	assert.False(t, dispatched)

	failed, err := store.MarkFirmwareDispatchFailure(ctx, models.MarkFirmwareDispatchFailureParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  oldFileID,
		DesiredFirmwareVersion: oldVersion,
		RetryState:             models.EnforcementStateDrifted,
		LastError:              "old target failed",
		MaxRetries:             3,
	})
	require.NoError(t, err)
	assert.False(t, failed)

	row := readFirmwareEnforcementState(t, tc.DatabaseService.DB, user.OrganizationID, deviceIdentifier)
	assert.Equal(t, "dispatching", row.state)
	assert.Equal(t, newFileID, row.desiredFileID)
	assert.Equal(t, newVersion, row.desiredVersion)
	assert.Equal(t, int32(0), row.retryCount)
	assert.False(t, row.lastBatchUUID.Valid)
	assert.False(t, row.lastError.Valid)

	failed, err = store.MarkFirmwareDispatchFailure(ctx, models.MarkFirmwareDispatchFailureParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  newFileID,
		DesiredFirmwareVersion: newVersion,
		RetryState:             models.EnforcementStateDrifted,
		LastError:              "new target failed",
		MaxRetries:             3,
	})
	require.NoError(t, err)
	assert.True(t, failed)

	row = readFirmwareEnforcementState(t, tc.DatabaseService.DB, user.OrganizationID, deviceIdentifier)
	assert.Equal(t, "drifted", row.state)
	assert.Equal(t, int32(1), row.retryCount)
	require.True(t, row.lastError.Valid)
	assert.Equal(t, "new target failed", row.lastError.String)
}

func TestCohortStore_MarkFirmwareDispatchHeldDoesNotIncrementRetry(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()

	deviceIdentifier := "firmware-held"
	fileID := "firmware-held-target"
	version := "1.0.0"

	claimed, err := store.ClaimFirmwareDispatch(ctx, models.ClaimFirmwareDispatchParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  fileID,
		DesiredFirmwareVersion: version,
		DispatchingBefore:      time.Now().Add(-time.Hour),
	})
	require.NoError(t, err)
	require.True(t, claimed)

	held, err := store.MarkFirmwareDispatchHeld(ctx, models.MarkFirmwareDispatchHeldParams{
		OrgID:                  user.OrganizationID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  fileID,
		DesiredFirmwareVersion: version,
		RetryState:             models.EnforcementStateDrifted,
		LastError:              "policy skip: curtailment",
	})
	require.NoError(t, err)
	require.True(t, held)

	row := readFirmwareEnforcementState(t, tc.DatabaseService.DB, user.OrganizationID, deviceIdentifier)
	assert.Equal(t, "drifted", row.state)
	assert.Equal(t, int32(0), row.retryCount)
	require.True(t, row.lastError.Valid)
	assert.Equal(t, "policy skip: curtailment", row.lastError.String)
}

func TestCohortStore_FirmwareTargetAndMembershipChangesResetEnforcementState(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()

	manufacturer := "TestCorp"
	model := "TestMiner"
	firmwareFileID := "firmware-reset-target"

	targetDevice := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	targetCohort, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "target reset",
		Purpose:           "target reset",
		SourceActorType:   models.SourceActorUser,
		DeviceIdentifiers: []string{targetDevice.ID},
	})
	require.NoError(t, err)
	requireFirmwareDispatchClaim(t, store, user.OrganizationID, targetDevice.ID, firmwareFileID, "1.0.0")
	_, err = store.SetCohortFirmwareTarget(ctx, models.SetCohortFirmwareTargetParams{
		OrgID:          user.OrganizationID,
		CohortID:       targetCohort.ID,
		Manufacturer:   &manufacturer,
		Model:          &model,
		FirmwareFileID: &firmwareFileID,
	})
	require.NoError(t, err)
	requireNoFirmwareEnforcementState(t, tc.DatabaseService.DB, user.OrganizationID, targetDevice.ID)

	requireFirmwareDispatchClaim(t, store, user.OrganizationID, targetDevice.ID, firmwareFileID, "1.0.0")
	_, err = store.SetCohortFirmwareTarget(ctx, models.SetCohortFirmwareTargetParams{
		OrgID:        user.OrganizationID,
		CohortID:     targetCohort.ID,
		Manufacturer: &manufacturer,
		Model:        &model,
	})
	require.NoError(t, err)
	requireNoFirmwareEnforcementState(t, tc.DatabaseService.DB, user.OrganizationID, targetDevice.ID)

	moveDevice := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	moveSource, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "move source",
		Purpose:           "move source",
		SourceActorType:   models.SourceActorUser,
		DeviceIdentifiers: []string{moveDevice.ID},
	})
	require.NoError(t, err)
	require.NotZero(t, moveSource.ID)
	moveTarget, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:           user.OrganizationID,
		Label:           "move target",
		Purpose:         "move target",
		SourceActorType: models.SourceActorUser,
	})
	require.NoError(t, err)
	requireFirmwareDispatchClaim(t, store, user.OrganizationID, moveDevice.ID, firmwareFileID, "1.0.0")
	_, err = store.MoveDevicesToCohort(ctx, models.MembershipMutationParams{
		OrgID:             user.OrganizationID,
		CohortID:          moveTarget.ID,
		DeviceIdentifiers: []string{moveDevice.ID},
	})
	require.NoError(t, err)
	requireNoFirmwareEnforcementState(t, tc.DatabaseService.DB, user.OrganizationID, moveDevice.ID)

	removeDevice := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	removeCohort, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "remove reset",
		Purpose:           "remove reset",
		SourceActorType:   models.SourceActorUser,
		DeviceIdentifiers: []string{removeDevice.ID},
	})
	require.NoError(t, err)
	requireFirmwareDispatchClaim(t, store, user.OrganizationID, removeDevice.ID, firmwareFileID, "1.0.0")
	_, err = store.RemoveDevicesAndGetCohort(ctx, models.MembershipMutationParams{
		OrgID:             user.OrganizationID,
		CohortID:          removeCohort.ID,
		DeviceIdentifiers: []string{removeDevice.ID},
	})
	require.NoError(t, err)
	requireNoFirmwareEnforcementState(t, tc.DatabaseService.DB, user.OrganizationID, removeDevice.ID)

	releaseDevice := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	releaseCohort, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "release reset",
		Purpose:           "release reset",
		SourceActorType:   models.SourceActorUser,
		DeviceIdentifiers: []string{releaseDevice.ID},
	})
	require.NoError(t, err)
	requireFirmwareDispatchClaim(t, store, user.OrganizationID, releaseDevice.ID, firmwareFileID, "1.0.0")
	_, err = store.ReleaseCohort(ctx, user.OrganizationID, releaseCohort.ID)
	require.NoError(t, err)
	requireNoFirmwareEnforcementState(t, tc.DatabaseService.DB, user.OrganizationID, releaseDevice.ID)

	expiredDevice := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	expiresAt := time.Now().Add(-time.Minute)
	expiredCohort, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "expiry reset",
		Purpose:           "expiry reset",
		SourceActorType:   models.SourceActorUser,
		ExpiresAt:         &expiresAt,
		DeviceIdentifiers: []string{expiredDevice.ID},
	})
	require.NoError(t, err)
	requireFirmwareDispatchClaim(t, store, user.OrganizationID, expiredDevice.ID, firmwareFileID, "1.0.0")
	released, err := store.SweepExpiredCohorts(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, released)
	assert.Contains(t, cohortIDs(released), expiredCohort.ID)
	requireNoFirmwareEnforcementState(t, tc.DatabaseService.DB, user.OrganizationID, expiredDevice.ID)
}

func TestCohortStore_ListFirmwareEnforcementCandidatesMatchesTargetCaseInsensitively(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()

	device := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	setDiscoveredDeviceShape(t, tc, user.OrganizationID, device.ID, "Proto", "Rig")
	pairCohortTestDevice(t, tc, user.OrganizationID, device.ID)
	upsertObservedFirmware(t, tc, user.OrganizationID, device.ID, "0.9.0")

	listed, err := store.ListCohorts(ctx, models.ListCohortsParams{OrgID: user.OrganizationID})
	require.NoError(t, err)
	defaultCohort := requireDefaultCohort(t, listed.Cohorts)

	manufacturer := "proto"
	model := "rig"
	firmwareFileID := "proto-rig-fw"
	_, err = store.SetCohortFirmwareTarget(ctx, models.SetCohortFirmwareTargetParams{
		OrgID:          user.OrganizationID,
		CohortID:       defaultCohort.ID,
		Manufacturer:   &manufacturer,
		Model:          &model,
		FirmwareFileID: &firmwareFileID,
	})
	require.NoError(t, err)

	candidates, err := store.ListFirmwareEnforcementCandidates(ctx, user.OrganizationID)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, device.ID, candidates[0].DeviceIdentifier)
	assert.Equal(t, "Proto", candidates[0].Manufacturer)
	assert.Equal(t, "Rig", candidates[0].Model)
	assert.Equal(t, firmwareFileID, candidates[0].FirmwareFileID)
	require.NotNil(t, candidates[0].ObservedFirmwareVersion)
	assert.Equal(t, "0.9.0", *candidates[0].ObservedFirmwareVersion)
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

type firmwareEnforcementStateRow struct {
	state            string
	desiredFileID    string
	desiredVersion   string
	retryCount       int32
	lastBatchUUID    sql.NullString
	lastDispatchedAt sql.NullTime
	confirmedAt      sql.NullTime
	observedAt       sql.NullTime
	lastError        sql.NullString
}

func readFirmwareEnforcementState(t *testing.T, db *sql.DB, orgID int64, deviceIdentifier string) firmwareEnforcementStateRow {
	t.Helper()

	row, ok := maybeReadFirmwareEnforcementState(t, db, orgID, deviceIdentifier)
	require.True(t, ok, "missing firmware enforcement state for %q", deviceIdentifier)
	return row
}

func maybeReadFirmwareEnforcementState(t *testing.T, db *sql.DB, orgID int64, deviceIdentifier string) (firmwareEnforcementStateRow, bool) {
	t.Helper()

	var row firmwareEnforcementStateRow
	err := db.QueryRowContext(t.Context(), `
		SELECT
			state,
			desired_firmware_file_id,
			desired_firmware_version,
			retry_count,
			last_batch_uuid,
			last_dispatched_at,
			confirmed_at,
			observed_at,
			last_error
		FROM device_enforcement_state
		WHERE org_id = $1
		  AND device_identifier = $2
		  AND dimension = 'firmware'
	`, orgID, deviceIdentifier).Scan(
		&row.state,
		&row.desiredFileID,
		&row.desiredVersion,
		&row.retryCount,
		&row.lastBatchUUID,
		&row.lastDispatchedAt,
		&row.confirmedAt,
		&row.observedAt,
		&row.lastError,
	)
	if err == sql.ErrNoRows {
		return firmwareEnforcementStateRow{}, false
	}
	require.NoError(t, err)
	return row, true
}

func requireNoFirmwareEnforcementState(t *testing.T, db *sql.DB, orgID int64, deviceIdentifier string) {
	t.Helper()

	_, ok := maybeReadFirmwareEnforcementState(t, db, orgID, deviceIdentifier)
	require.False(t, ok, "expected no firmware enforcement state for %q", deviceIdentifier)
}

func requireFirmwareDispatchClaim(t *testing.T, store *sqlstores.SQLCohortStore, orgID int64, deviceIdentifier string, firmwareFileID string, firmwareVersion string) {
	t.Helper()

	claimed, err := store.ClaimFirmwareDispatch(t.Context(), models.ClaimFirmwareDispatchParams{
		OrgID:                  orgID,
		DeviceIdentifier:       deviceIdentifier,
		DesiredFirmwareFileID:  firmwareFileID,
		DesiredFirmwareVersion: firmwareVersion,
		DispatchingBefore:      time.Now().Add(-time.Hour),
	})
	require.NoError(t, err)
	require.True(t, claimed)
}

func cohortIDs(cohorts []*models.Cohort) []int64 {
	ids := make([]int64, 0, len(cohorts))
	for _, cohort := range cohorts {
		ids = append(ids, cohort.ID)
	}
	return ids
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
	ctx := t.Context()

	match := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	otherProduct := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	otherModel := tc.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	setDiscoveredDeviceShape(t, tc, user.OrganizationID, otherProduct.ID, "OtherCorp", "TestMiner")
	setDiscoveredDeviceShape(t, tc, user.OrganizationID, otherModel.ID, "TestCorp", "OtherMiner")

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
		},
	})
	require.NoError(t, err)
	require.Len(t, created.Members, 1)
	assert.Equal(t, match.ID, created.Members[0].DeviceIdentifier)

	_, err = store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:           user.OrganizationID,
		Label:           "selected again",
		Purpose:         "selector allocation",
		SourceActorType: models.SourceActorUser,
		DeviceSelector: &models.CohortDeviceSelector{
			Count:   1,
			Product: &product,
			Model:   &model,
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

func requireDefaultCohort(t *testing.T, cohorts []*models.Cohort) *models.Cohort {
	t.Helper()

	for _, c := range cohorts {
		if c.IsDefault {
			return c
		}
	}
	require.Fail(t, "missing default cohort")
	return nil
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

func requireCohortDevice(t *testing.T, devices []models.CohortDevice, deviceIdentifier string) models.CohortDevice {
	t.Helper()

	for _, device := range devices {
		if device.DeviceIdentifier == deviceIdentifier {
			return device
		}
	}
	require.Failf(t, "missing cohort device", "device identifier %q not found", deviceIdentifier)
	return models.CohortDevice{}
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

func upsertDeviceStatus(t *testing.T, tc *testutil.TestContext, orgID int64, deviceIdentifier string, status string) {
	t.Helper()

	result, err := tc.DatabaseService.DB.ExecContext(t.Context(), `
		INSERT INTO device_status (device_id, status, status_timestamp)
		SELECT d.id, $3, CURRENT_TIMESTAMP
		FROM device d
		WHERE d.org_id = $1
		  AND d.device_identifier = $2
		  AND d.deleted_at IS NULL
		ON CONFLICT (device_id)
		DO UPDATE SET
		    status = EXCLUDED.status,
		    status_timestamp = EXCLUDED.status_timestamp
	`, orgID, deviceIdentifier, status)
	require.NoError(t, err)
	affected, err := result.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)
}

func pairCohortTestDevice(t *testing.T, tc *testutil.TestContext, orgID int64, deviceIdentifier string) {
	t.Helper()

	result, err := tc.DatabaseService.DB.ExecContext(t.Context(), `
		INSERT INTO device_pairing (device_id, pairing_status, paired_at)
		SELECT d.id, 'PAIRED', CURRENT_TIMESTAMP
		FROM device d
		WHERE d.org_id = $1
		  AND d.device_identifier = $2
		  AND d.deleted_at IS NULL
		ON CONFLICT (device_id)
		DO UPDATE SET
		    pairing_status = EXCLUDED.pairing_status,
		    paired_at = EXCLUDED.paired_at
	`, orgID, deviceIdentifier)
	require.NoError(t, err)
	affected, err := result.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)
}

func upsertObservedFirmware(t *testing.T, tc *testutil.TestContext, orgID int64, deviceIdentifier string, firmwareVersion string) {
	t.Helper()
	upsertObservedFirmwareAt(t, tc, orgID, deviceIdentifier, firmwareVersion, time.Now().UTC())
}

func upsertObservedFirmwareAt(t *testing.T, tc *testutil.TestContext, orgID int64, deviceIdentifier string, firmwareVersion string, observedAt time.Time) {
	t.Helper()

	result, err := tc.DatabaseService.DB.ExecContext(t.Context(), `
		INSERT INTO device_firmware_state (
		    org_id,
		    device_identifier,
		    firmware_version,
		    observed_at
		) VALUES (
		    $1,
		    $2,
		    $3,
		    $4
		)
		ON CONFLICT (org_id, device_identifier)
		DO UPDATE SET
		    firmware_version = EXCLUDED.firmware_version,
		    observed_at = EXCLUDED.observed_at
	`, orgID, deviceIdentifier, firmwareVersion, observedAt)
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
