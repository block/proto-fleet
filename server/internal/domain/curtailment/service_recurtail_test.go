package curtailment

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestService_Recurtail_HappyPath(t *testing.T) {
	t.Parallel()
	f := newStopFixture(t, func(ev *models.Event) { ev.State = models.EventStateRestoring })

	got, err := f.svc.Recurtail(t.Context(), RecurtailRequest{OrgID: 1, EventUUID: f.event.EventUUID})
	require.NoError(t, err)
	assert.Equal(t, models.EventStatePending, got.State, "a restoring event resumes through pending dispatch")
	assert.Equal(t, 1, f.store.beginRecurtailCalls)
	assert.Equal(t, f.event.EventUUID, f.store.beginRecurtailLastEventID)
}

func TestService_Recurtail_ValidatesRequest(t *testing.T) {
	t.Parallel()
	f := newStopFixture(t, nil)

	cases := []struct {
		name string
		req  RecurtailRequest
	}{
		{"missing org", RecurtailRequest{EventUUID: f.event.EventUUID}},
		{"missing event", RecurtailRequest{OrgID: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := f.svc.Recurtail(t.Context(), tc.req)
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, "invalid_argument", fleetErr.GRPCCode.String())
			assert.Equal(t, 0, f.store.beginRecurtailCalls, "invalid request must not reach the store")
		})
	}
}

// A terminal/illegal transition surfaces from the store as FailedPrecondition;
// the service passes it through unchanged.
func TestService_Recurtail_PropagatesStoreError(t *testing.T) {
	t.Parallel()
	f := newStopFixture(t, func(ev *models.Event) { ev.State = models.EventStateRestoring })
	f.store.beginRecurtailErr = fleeterror.NewFailedPreconditionError("cannot re-curtail event in terminal state")

	_, err := f.svc.Recurtail(t.Context(), RecurtailRequest{OrgID: 1, EventUUID: f.event.EventUUID})
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, "failed_precondition", fleetErr.GRPCCode.String())
}
