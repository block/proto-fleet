package betweenchannel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
)

type recordingActivityLogger struct {
	events []activitymodels.Event
}

func (l *recordingActivityLogger) Log(_ context.Context, event activitymodels.Event) {
	l.events = append(l.events, event)
}

func TestFinalizerProjectsOnlyNewFinalizations(t *testing.T) {
	t.Parallel()

	logger := &recordingActivityLogger{}
	finalizer := NewFinalizer(FinalizerConfig{}, nil, logger)
	result := FinalizationResult{
		Finalization: Finalization{
			OrgID:            1,
			DeviceIdentifier: "miner-a",
		},
		Outcome: FinalizationOutcomeMoved,
	}

	finalizer.projectActivity(t.Context(), result)
	require.Empty(t, logger.events)

	result.ProjectActivity = true
	finalizer.projectActivity(t.Context(), result)
	require.Len(t, logger.events, 1)
}
