package betweenchannel

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
)

type recordingActivityLogger struct {
	events []activitymodels.Event
}

func (l *recordingActivityLogger) Log(_ context.Context, event activitymodels.Event) {
	l.events = append(l.events, event)
}

type recordingFinalizationStore struct {
	rows    []Finalization
	results map[int64]FinalizationResult
	errs    map[int64]error
	calls   []int64
}

func (s *recordingFinalizationStore) ListFinalizations(
	context.Context,
	int32,
) ([]Finalization, error) {
	return append([]Finalization(nil), s.rows...), nil
}

func (s *recordingFinalizationStore) Finalize(
	_ context.Context,
	finalization Finalization,
) (FinalizationResult, error) {
	s.calls = append(s.calls, finalization.MemberID)
	if err := s.errs[finalization.MemberID]; err != nil {
		return FinalizationResult{}, err
	}
	return s.results[finalization.MemberID], nil
}

func TestFinalizerRunOnceFinalizesAndProjectsSuccess(t *testing.T) {
	t.Parallel()

	row := testFinalization(1)
	store := &recordingFinalizationStore{
		rows: []Finalization{row},
		results: map[int64]FinalizationResult{
			row.MemberID: {
				Finalization:    row,
				Outcome:         FinalizationOutcomeMoved,
				ProjectActivity: true,
			},
		},
	}
	logger := &recordingActivityLogger{}
	finalizer := NewFinalizer(FinalizerConfig{BatchSize: 10}, store, logger)

	finalizer.RunOnce(t.Context())

	require.Equal(t, []int64{row.MemberID}, store.calls)
	require.Len(t, logger.events, 1)
}

func TestFinalizerRunOnceContinuesAfterRowFailure(t *testing.T) {
	t.Parallel()

	first := testFinalization(1)
	second := testFinalization(2)
	store := &recordingFinalizationStore{
		rows: []Finalization{first, second},
		results: map[int64]FinalizationResult{
			second.MemberID: {
				Finalization:    second,
				Outcome:         FinalizationOutcomeMoved,
				ProjectActivity: true,
			},
		},
		errs: map[int64]error{first.MemberID: errors.New("row conflict")},
	}
	logger := &recordingActivityLogger{}
	finalizer := NewFinalizer(FinalizerConfig{BatchSize: 10}, store, logger)

	finalizer.RunOnce(t.Context())

	require.Equal(t, []int64{first.MemberID, second.MemberID}, store.calls)
	require.Len(t, logger.events, 1)
}

func TestFinalizerRunOnceReplayDoesNotProjectActivity(t *testing.T) {
	t.Parallel()

	row := testFinalization(1)
	store := &recordingFinalizationStore{
		rows: []Finalization{row},
		results: map[int64]FinalizationResult{
			row.MemberID: {
				Finalization: row,
				Outcome:      FinalizationOutcomeMoved,
			},
		},
	}
	logger := &recordingActivityLogger{}
	finalizer := NewFinalizer(FinalizerConfig{BatchSize: 10}, store, logger)

	finalizer.RunOnce(t.Context())

	require.Equal(t, []int64{row.MemberID}, store.calls)
	require.Empty(t, logger.events)
}

func TestFinalizerRunOnceProjectsOnlyFlaggedActivity(t *testing.T) {
	t.Parallel()

	replay := testFinalization(1)
	newResult := testFinalization(2)
	store := &recordingFinalizationStore{
		rows: []Finalization{replay, newResult},
		results: map[int64]FinalizationResult{
			replay.MemberID: {
				Finalization: replay,
				Outcome:      FinalizationOutcomeMoved,
			},
			newResult.MemberID: {
				Finalization:    newResult,
				Outcome:         FinalizationOutcomeAttention,
				ProjectActivity: true,
			},
		},
	}
	logger := &recordingActivityLogger{}
	finalizer := NewFinalizer(FinalizerConfig{BatchSize: 10}, store, logger)

	finalizer.RunOnce(t.Context())

	require.Len(t, logger.events, 1)
	require.Equal(
		t,
		"between_channel_rollout_member.attention_required",
		logger.events[0].Type,
	)
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

func testFinalization(memberID int64) Finalization {
	return Finalization{
		MemberID:         memberID,
		RolloutID:        uuid.New(),
		OrgID:            1,
		DeviceIdentifier: "miner-a",
		SourceChannelID:  10,
		TargetChannelID:  20,
		LaneID:           uuid.New(),
	}
}
