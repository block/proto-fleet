package sqlstores

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/minerchannel/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

func TestMapMinerChannelInsertError_ActiveLabelUniqueViolation(t *testing.T) {
	t.Parallel()

	err := mapMinerChannelInsertError(&pgconn.PgError{
		Code:           db.PGUniqueViolation,
		ConstraintName: minerChannelActiveLabelUniqueIndex,
	})

	assert.True(t, fleeterror.IsAlreadyExistsError(err))
	assert.Contains(t, err.Error(), "active miner channel with this label")
}

func TestMapMinerChannelUpdateError_ActiveLabelUniqueViolation(t *testing.T) {
	t.Parallel()

	err := mapMinerChannelUpdateError(&pgconn.PgError{
		Code:           db.PGUniqueViolation,
		ConstraintName: minerChannelActiveLabelUniqueIndex,
	})

	assert.True(t, fleeterror.IsAlreadyExistsError(err))
	assert.Contains(t, err.Error(), "active miner channel with this label")
}

func TestDefaultMinerChannelAvailabilityErrorIsUserFacing(t *testing.T) {
	t.Parallel()

	product := "Proto"
	model := "Rig"
	err := newDefaultMinerChannelAvailabilityError(0, &models.MinerChannelDeviceSelector{
		Count:   5,
		Product: &product,
		Model:   &model,
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err))
	assert.Contains(t, err.Error(), "Only 0 miners are available in the default miner channel for Proto Rig. Requested 5 miners.")
	assert.NotContains(t, err.Error(), "default-miner channel")
	assert.NotContains(t, err.Error(), "product")
}

func TestMinerChannelPageCursorRoundTrip(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	token, err := encodeMinerChannelPageCursor(minerChannelPageCursor{IsDefault: true, UpdatedAt: updatedAt, ID: 42})
	require.NoError(t, err)

	cursor, err := decodeMinerChannelPageCursor(token)
	require.NoError(t, err)
	require.NotNil(t, cursor)
	assert.True(t, cursor.IsDefault)
	assert.Equal(t, updatedAt, cursor.UpdatedAt)
	assert.Equal(t, int64(42), cursor.ID)
}

func TestMinerChannelDevicePageCursorRoundTrip(t *testing.T) {
	t.Parallel()

	token, err := encodeMinerChannelDevicePageCursor(minerChannelDevicePageCursor{
		DisplayName:      "Proto Rig 7",
		DeviceIdentifier: "rig-7",
	})
	require.NoError(t, err)

	cursor, err := decodeMinerChannelDevicePageCursor(token)
	require.NoError(t, err)
	require.NotNil(t, cursor)
	assert.Equal(t, "Proto Rig 7", cursor.DisplayName)
	assert.Equal(t, "rig-7", cursor.DeviceIdentifier)
}

func TestMinerChannelPageCursorRejectsInvalidToken(t *testing.T) {
	t.Parallel()

	_, err := decodeMinerChannelPageCursor("not-valid-base64")
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}
