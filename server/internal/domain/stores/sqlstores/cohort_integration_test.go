package sqlstores_test

import (
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
		DeviceIdentifiers:     []string{"miner-a", "miner-b"},
	})
	require.NoError(t, err)
	assert.False(t, created.IsDefault)
	assert.Equal(t, models.CohortStateActive, created.State)
	assert.Equal(t, int64(2), created.ExplicitMemberCount)
	require.Len(t, created.Members, 2)

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

	listed, err := store.ListCohorts(ctx, models.ListCohortsParams{OrgID: user.OrganizationID})
	require.NoError(t, err)
	require.Len(t, listed, 2) // the org default cohort plus the created cohort
	userCohorts := nonDefaultCohorts(listed)
	require.Len(t, userCohorts, 1)
	assert.Equal(t, created.ID, userCohorts[0].ID)
	assert.Equal(t, int64(2), userCohorts[0].ExplicitMemberCount)

	owned, err := store.ListCohortsByOwner(ctx, models.ListCohortsByOwnerParams{
		OrgID:       user.OrganizationID,
		OwnerUserID: user.DatabaseID,
	})
	require.NoError(t, err)
	require.Len(t, owned, 1)
	assert.Equal(t, created.ID, owned[0].ID)

	released, err := store.ReleaseCohort(ctx, user.OrganizationID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, models.CohortStateReleased, released.State)
	assert.Equal(t, int64(0), released.ExplicitMemberCount)
	assert.Empty(t, released.Members)

	active, err := store.ListCohorts(ctx, models.ListCohortsParams{OrgID: user.OrganizationID})
	require.NoError(t, err)
	assert.Empty(t, nonDefaultCohorts(active)) // only the org default cohort remains active

	withReleased, err := store.ListCohorts(ctx, models.ListCohortsParams{
		OrgID:           user.OrganizationID,
		IncludeReleased: true,
	})
	require.NoError(t, err)
	releasedUserCohorts := nonDefaultCohorts(withReleased)
	require.Len(t, releasedUserCohorts, 1)
	assert.Equal(t, created.ID, releasedUserCohorts[0].ID)
	assert.Equal(t, models.CohortStateReleased, releasedUserCohorts[0].State)
}

func TestCohortStore_RejectsDuplicateDeviceMembership(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	user := tc.DatabaseService.CreateSuperAdminUser()
	store := sqlstores.NewSQLCohortStore(tc.DatabaseService.DB)
	ctx := t.Context()

	_, err := store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "first",
		Purpose:           "first reservation",
		SourceActorType:   models.SourceActorUser,
		DeviceIdentifiers: []string{"miner-one"},
	})
	require.NoError(t, err)

	_, err = store.CreateCohort(ctx, models.CreateCohortParams{
		OrgID:             user.OrganizationID,
		Label:             "second",
		Purpose:           "second reservation",
		SourceActorType:   models.SourceActorUser,
		DeviceIdentifiers: []string{"miner-one"},
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "expected AlreadyExists, got %v", err)
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
