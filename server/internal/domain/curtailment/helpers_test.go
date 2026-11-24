package curtailment

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/stretchr/testify/assert"
)

func TestMaxDurationElapsed_PendingTimeSurvivesLifecycleTransitions(t *testing.T) {
	created := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	started := created.Add(50 * time.Second)
	capSeconds := int32(60)
	event := &models.Event{State: models.EventStatePending, CreatedAt: created, MaxDurationSeconds: &capSeconds}

	elapsed, expired := MaxDurationElapsed(event, started)
	assert.Equal(t, 50*time.Second, elapsed)
	assert.False(t, expired)
	event.State = models.EventStateActive
	event.StartedAt = &started
	for _, state := range []models.EventState{models.EventStateActive, models.EventStateRestoring, models.EventStatePending} {
		t.Run(string(state), func(t *testing.T) {
			event.State = state
			elapsed, expired := MaxDurationElapsed(event, created.Add(time.Minute))
			assert.Equal(t, time.Minute, elapsed)
			assert.True(t, expired, "activation and restore reversal must not grant more time")
		})
	}
	// A second confirmation may overwrite started_at after a restore reversal.
	restarted := created.Add(2 * time.Minute)
	event.StartedAt = &restarted
	_, expired = MaxDurationElapsed(event, restarted)
	assert.True(t, expired)
}

func TestMaxDurationElapsed_UsesEarlierStartedAt(t *testing.T) {
	started := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	capSeconds := int32(60)
	event := &models.Event{CreatedAt: started.Add(time.Second), StartedAt: &started, MaxDurationSeconds: &capSeconds}
	elapsed, expired := MaxDurationElapsed(event, started.Add(time.Minute))
	assert.Equal(t, time.Minute, elapsed)
	assert.True(t, expired, "an initial start timestamp before insertion is the earliest event time")
}

func TestMaxDurationElapsed_NoFiniteCap(t *testing.T) {
	capSeconds, zero, negative := int32(60), int32(0), int32(-1)
	for _, event := range []*models.Event{
		nil,
		{},
		{MaxDurationSeconds: &zero},
		{MaxDurationSeconds: &negative},
		{MaxDurationSeconds: &capSeconds, AllowUnbounded: true},
	} {
		elapsed, expired := MaxDurationElapsed(event, time.Now())
		assert.Zero(t, elapsed)
		assert.False(t, expired)
	}
}
