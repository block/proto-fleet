package curtailment

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// listTestCursorFixture is the opaque pagination cursor value used across
// the service-list tests. Hoisted out of the inline struct literal so
// gosec's hardcoded-credentials heuristic doesn't conflate a PageToken
// string field with a real credential.
const listTestCursorFixture = "opaque-cursor"

// TestService_ListEvents_HappyPathForwardsCursorAndStateFilter pins the
// service → store hand-off: org gets attached, state filter forwards
// verbatim, and the next-page token round-trips back to the caller when
// rows exceed page_size.
func TestService_ListEvents_HappyPathForwardsCursorAndStateFilter(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	store := newFakeStore()
	store.eventsHistory = []*models.Event{
		{ID: 4, EventUUID: uuid.New(), OrgID: orgID, State: models.EventStateCompleted},
		{ID: 3, EventUUID: uuid.New(), OrgID: orgID, State: models.EventStateCompleted},
		{ID: 2, EventUUID: uuid.New(), OrgID: orgID, State: models.EventStateCompleted},
		{ID: 1, EventUUID: uuid.New(), OrgID: orgID, State: models.EventStateActive},
	}
	svc := NewService(store)

	events, next, err := svc.ListEvents(t.Context(), ListEventsRequest{
		OrgID:       orgID,
		PageSize:    2,
		StateFilter: models.EventStateCompleted,
	})
	require.NoError(t, err)
	assert.Len(t, events, 2, "page_size honored")
	assert.NotEmpty(t, next, "next-page token returned when more rows remain")
	assert.Equal(t, orgID, store.lastListEventsParams.OrgID)
	assert.Equal(t, models.EventStateCompleted, store.lastListEventsParams.StateFilter)
}

// TestService_ListEvents_RejectsMissingOrg pins the orgID guard. Cross-
// tenant exposure is one query away; the validation belongs at the
// service boundary regardless of upstream interceptor coverage.
func TestService_ListEvents_RejectsMissingOrg(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())

	_, _, err := svc.ListEvents(t.Context(), ListEventsRequest{OrgID: 0})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

// TestService_ListEvents_RejectsNegativePageSize pins the page-size
// pre-check so the store never sees nonsense values.
func TestService_ListEvents_RejectsNegativePageSize(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())

	_, _, err := svc.ListEvents(t.Context(), ListEventsRequest{OrgID: 1, PageSize: -5})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "page_size")
}

// TestService_ListEvents_PropagatesStoreError pins the bare-error path.
func TestService_ListEvents_PropagatesStoreError(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.listEventsErr = errors.New("db down")
	svc := NewService(store)

	_, _, err := svc.ListEvents(t.Context(), ListEventsRequest{OrgID: 1})
	require.Error(t, err)
	assert.ErrorContains(t, err, "db down")
}

// TestService_ListEvents_StoreReceivesParams pins what the store sees;
// callers below the service can rely on the params shape.
func TestService_ListEvents_StoreReceivesParams(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	svc := NewService(store)

	_, _, err := svc.ListEvents(t.Context(), ListEventsRequest{
		OrgID:       42,
		PageSize:    20,
		PageToken:   listTestCursorFixture,
		StateFilter: models.EventStateRestoring,
	})
	require.NoError(t, err)
	assert.Equal(t, interfaces.ListEventsParams{
		OrgID:       42,
		PageSize:    20,
		PageToken:   listTestCursorFixture,
		StateFilter: models.EventStateRestoring,
	}, store.lastListEventsParams)
}
