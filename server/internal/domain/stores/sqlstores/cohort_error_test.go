package sqlstores

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

func TestMapCohortInsertError_ActiveLabelUniqueViolation(t *testing.T) {
	t.Parallel()

	err := mapCohortInsertError(&pgconn.PgError{
		Code:           db.PGUniqueViolation,
		ConstraintName: cohortActiveLabelUniqueIndex,
	})

	assert.True(t, fleeterror.IsAlreadyExistsError(err))
	assert.Contains(t, err.Error(), "active cohort with this label")
}

func TestMapCohortUpdateError_ActiveLabelUniqueViolation(t *testing.T) {
	t.Parallel()

	err := mapCohortUpdateError(&pgconn.PgError{
		Code:           db.PGUniqueViolation,
		ConstraintName: cohortActiveLabelUniqueIndex,
	})

	assert.True(t, fleeterror.IsAlreadyExistsError(err))
	assert.Contains(t, err.Error(), "active cohort with this label")
}

func TestDefaultCohortAvailabilityErrorIsUserFacing(t *testing.T) {
	t.Parallel()

	product := "Proto"
	model := "Rig"
	err := newDefaultCohortAvailabilityError(0, &models.CohortDeviceSelector{
		Count:   5,
		Product: &product,
		Model:   &model,
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err))
	assert.Contains(t, err.Error(), "Only 0 miners are available in the default cohort for Proto Rig. Requested 5 miners.")
	assert.NotContains(t, err.Error(), "default-cohort")
	assert.NotContains(t, err.Error(), "product")
}

func TestCohortPageCursorRoundTrip(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	token, err := encodeCohortPageCursor(cohortPageCursor{IsDefault: true, UpdatedAt: updatedAt, ID: 42})
	require.NoError(t, err)

	cursor, err := decodeCohortPageCursor(token)
	require.NoError(t, err)
	require.NotNil(t, cursor)
	assert.True(t, cursor.IsDefault)
	assert.Equal(t, updatedAt, cursor.UpdatedAt)
	assert.Equal(t, int64(42), cursor.ID)
}

func TestCohortDevicePageCursorRoundTrip(t *testing.T) {
	t.Parallel()

	token, err := encodeCohortDevicePageCursor(cohortDevicePageCursor{
		DisplayName:      "Proto Rig 7",
		DeviceIdentifier: "rig-7",
	})
	require.NoError(t, err)

	cursor, err := decodeCohortDevicePageCursor(token)
	require.NoError(t, err)
	require.NotNil(t, cursor)
	assert.Equal(t, "Proto Rig 7", cursor.DisplayName)
	assert.Equal(t, "rig-7", cursor.DeviceIdentifier)
}

func TestCohortPageCursorRejectsInvalidToken(t *testing.T) {
	t.Parallel()

	_, err := decodeCohortPageCursor("not-valid-base64")
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}
