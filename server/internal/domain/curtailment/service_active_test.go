package curtailment

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestService_GetActive_ReturnsActiveEvent(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	event := &models.Event{ID: 42, EventUUID: uuid.New(), OrgID: 1, State: models.EventStateActive}
	store.activeEvent = event
	svc := NewService(store)

	got, err := svc.GetActive(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, event, got)
}

func TestService_GetActive_ReturnsNilWhenNoneActive(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())

	got, err := svc.GetActive(t.Context(), 1)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestService_GetActive_RejectsMissingOrg(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())

	_, err := svc.GetActive(t.Context(), 0)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestService_GetActive_PropagatesStoreError(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.activeEventErr = errors.New("db down")
	svc := NewService(store)

	_, err := svc.GetActive(t.Context(), 1)
	require.Error(t, err)
	assert.ErrorContains(t, err, "db down")
}
