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

func TestService_ListActive_ReturnsAllActiveEvents(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	e1 := &models.Event{ID: 1, EventUUID: uuid.New(), OrgID: 7, State: models.EventStateActive}
	e2 := &models.Event{ID: 2, EventUUID: uuid.New(), OrgID: 7, State: models.EventStatePending}
	store.activeEvents = []*models.Event{e1, e2}
	svc := NewService(store)

	got, err := svc.ListActive(t.Context(), 7)
	require.NoError(t, err)
	assert.Equal(t, []*models.Event{e1, e2}, got)
}

func TestService_ListActive_RejectsMissingOrg(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())

	_, err := svc.ListActive(t.Context(), 0)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestService_ListActive_PropagatesStoreError(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.activeEventErr = errors.New("db down")
	svc := NewService(store)

	_, err := svc.ListActive(t.Context(), 7)
	require.Error(t, err)
	assert.ErrorContains(t, err, "db down")
}
