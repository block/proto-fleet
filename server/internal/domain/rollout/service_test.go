package rollout

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextStateManualGateLifecycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		current      State
		operation    ControlOperation
		withFailures bool
		want         State
		wantErr      error
	}{
		{"admit created", StateCreated, ControlOperationAdmit, false, StateRunning, nil},
		{"continue review", StateReview, ControlOperationContinue, false, StateRunning, nil},
		{"pause running", StateRunning, ControlOperationPause, false, StatePaused, nil},
		{"pause review", StateReview, ControlOperationPause, false, StatePaused, nil},
		{"resume running", StatePaused, ControlOperationResume, false, StateRunning, nil},
		{"abort created", StateCreated, ControlOperationAbort, false, StateAborted, nil},
		{"abort paused", StatePaused, ControlOperationAbort, false, StateAborted, nil},
		{"complete running", StateRunning, ControlOperationComplete, false, StateCompleted, nil},
		{"complete with failures", StateReview, ControlOperationComplete, true, StateCompletedWithFailures, nil},
		{"begin revert", StateCompleted, ControlOperationRevert, false, StateReverting, nil},
		{"finish revert", StateReverting, ControlOperationComplete, false, StateReverted, nil},
		{"cannot resume created", StateCreated, ControlOperationResume, false, "", ErrInvalidTransition},
		{"cannot admit aborted", StateAborted, ControlOperationAdmit, false, "", ErrInvalidTransition},
		{"cannot abort completed", StateCompleted, ControlOperationAbort, false, "", ErrInvalidTransition},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NextState(tc.current, tc.operation, StateRunning, tc.withFailures)
			require.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestStateIsTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state State
		want  bool
	}{
		{StateCreated, false},
		{StateRunning, false},
		{StatePaused, false},
		{StateReview, false},
		{StateAborted, true},
		{StateCompleted, true},
		{StateCompletedWithFailures, true},
		{StateReverting, false},
		{StateReverted, true},
	}

	for _, testCase := range tests {
		t.Run(string(testCase.state), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, testCase.state.IsTerminal())
		})
	}
}

func TestFingerprintCreateRejectsUnmarshalableSnapshots(t *testing.T) {
	t.Parallel()

	_, err := fingerprintCreate(CreateRequest{
		SourceSnapshot: map[string]any{"invalid": func() {}},
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "marshal rollout creation fingerprint")
}

func TestServiceAdmitDuplicateIdempotencyDoesNotCallStrategyTwice(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	controlID := uuid.New()
	batch := Batch{ID: 7, RolloutID: rolloutID, Position: 0, State: BatchStateAdmitted}
	first := ControlResult{
		Rollout: &Rollout{
			ID:          rolloutID,
			OrgID:       42,
			StrategyKey: "fake",
			State:       StateRunning,
			Revision:    2,
			Batches:     []Batch{batch},
		},
		Batch:   &batch,
		Control: Control{ID: controlID, Status: ControlStatusStarted},
	}
	replay := first
	replay.Replayed = true
	replay.Control.Status = ControlStatusSucceeded

	store := &fakeStore{
		getResult: &Rollout{ID: rolloutID, OrgID: 42, StrategyKey: "fake", State: StateCreated, Revision: 1},
		controlResults: []ControlResult{
			first,
			replay,
		},
	}
	strategy := &fakeAdmissionStrategy{key: "fake"}
	svc := NewService(store, strategy)
	req := AdmitRequest{
		OrgID:            42,
		RolloutID:        rolloutID,
		BatchID:          7,
		ExpectedRevision: 1,
		IdempotencyKey:   "admit-batch-7",
		Reason:           "operator approved",
		ActorUserID:      9,
	}

	got, err := svc.Admit(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, int64(2), got.Revision)

	got, err = svc.Admit(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, int64(2), got.Revision)
	assert.Equal(t, 1, strategy.admitCalls)
	assert.Equal(t, 1, store.finishCalls)
}

func TestServiceAdmitReplayResumesStartedStrategyWork(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	batch := Batch{
		ID:        7,
		RolloutID: rolloutID,
		State:     BatchStateAdmitted,
	}
	store := &fakeStore{
		getResult: &Rollout{
			ID:          rolloutID,
			OrgID:       42,
			StrategyKey: "fake",
			State:       StateRunning,
			Revision:    2,
		},
		controlResults: []ControlResult{{
			Rollout: &Rollout{
				ID:          rolloutID,
				OrgID:       42,
				StrategyKey: "fake",
				State:       StateRunning,
				Revision:    2,
				Batches:     []Batch{batch},
			},
			Batch:    &batch,
			Control:  Control{ID: uuid.New(), Status: ControlStatusStarted},
			Replayed: true,
		}},
	}
	strategy := &fakeAdmissionStrategy{key: "fake"}
	svc := NewService(store, strategy)

	_, err := svc.Admit(t.Context(), AdmitRequest{
		OrgID:            42,
		RolloutID:        rolloutID,
		BatchID:          7,
		ExpectedRevision: 1,
		IdempotencyKey:   "resume-started-admit",
		Reason:           "operator approved",
		ActorUserID:      9,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, strategy.admitCalls)
	assert.Equal(t, 1, store.finishCalls)
}

func TestServiceRevertReplayResumesStartedStrategyWork(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	current := &Rollout{
		ID:          rolloutID,
		OrgID:       42,
		StrategyKey: "fake",
		State:       StateReverting,
		Revision:    4,
	}
	store := &fakeStore{
		getResult: current,
		controlResults: []ControlResult{{
			Rollout:  current,
			Control:  Control{ID: uuid.New(), Status: ControlStatusStarted},
			Replayed: true,
		}},
	}
	strategy := &fakeAdmissionStrategy{key: "fake"}
	svc := NewService(store, strategy)

	_, err := svc.Revert(t.Context(), ControlRequest{
		OrgID:            42,
		RolloutID:        rolloutID,
		ExpectedRevision: 3,
		IdempotencyKey:   "resume-started-revert",
		Reason:           "operator approved",
		ActorUserID:      9,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, strategy.revertCalls)
	assert.Equal(t, 1, store.finishCalls)
}

func TestServiceWithoutConcreteStrategyStillSupportsReadsAndFailsAdmissionClosed(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	store := &fakeStore{
		getResult: &Rollout{
			ID:          rolloutID,
			OrgID:       42,
			StrategyKey: "not-registered",
			State:       StateCreated,
			Revision:    1,
		},
	}
	svc := NewService(store)

	got, err := svc.Get(t.Context(), 42, rolloutID)
	require.NoError(t, err)
	assert.Equal(t, rolloutID, got.ID)

	_, err = svc.Admit(t.Context(), AdmitRequest{
		OrgID:            42,
		RolloutID:        rolloutID,
		BatchID:          7,
		ExpectedRevision: 1,
		IdempotencyKey:   "admit-without-strategy",
		Reason:           "operator approved",
		ActorUserID:      9,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrStrategyUnavailable)
	assert.Empty(t, store.controlRequests)
}

func TestServiceControlPropagatesStaleRevisionWithoutRetry(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	store := &fakeStore{controlErr: ErrRevisionConflict}
	svc := NewService(store)

	_, err := svc.Pause(t.Context(), ControlRequest{
		OrgID:            42,
		RolloutID:        rolloutID,
		ExpectedRevision: 4,
		IdempotencyKey:   "pause-stale",
		Reason:           "investigating",
		ActorUserID:      9,
	})
	require.ErrorIs(t, err, ErrRevisionConflict)
	require.Len(t, store.controlRequests, 1)
	assert.Equal(t, ControlOperationPause, store.controlRequests[0].Operation)
}

type fakeStore struct {
	getResult       *Rollout
	getErr          error
	controlResults  []ControlResult
	controlErr      error
	controlRequests []ControlRequest
	finishCalls     int
}

func (s *fakeStore) Create(context.Context, CreateRequest) (CreateResult, error) {
	return CreateResult{}, errors.New("unexpected Create call")
}

func (s *fakeStore) Get(context.Context, int64, uuid.UUID) (*Rollout, error) {
	return s.getResult, s.getErr
}

func (s *fakeStore) List(context.Context, int64, []State) ([]Rollout, error) {
	return nil, errors.New("unexpected List call")
}

func (s *fakeStore) ApplyControl(_ context.Context, req ControlRequest) (ControlResult, error) {
	s.controlRequests = append(s.controlRequests, req)
	if s.controlErr != nil {
		return ControlResult{}, s.controlErr
	}
	result := s.controlResults[0]
	s.controlResults = s.controlResults[1:]
	return result, nil
}

func (s *fakeStore) FinishControl(context.Context, FinishControlRequest) (*Rollout, error) {
	s.finishCalls++
	return s.getResult, nil
}

func (s *fakeStore) UpdateMember(context.Context, MemberUpdateRequest) (Member, error) {
	return Member{}, errors.New("unexpected UpdateMember call")
}

func (s *fakeStore) CaptureEvidence(context.Context, EvidenceRequest) ([]Evidence, error) {
	return nil, errors.New("unexpected CaptureEvidence call")
}

type fakeAdmissionStrategy struct {
	key         string
	admitCalls  int
	revertCalls int
}

func (s *fakeAdmissionStrategy) Key() string {
	return s.key
}

func (s *fakeAdmissionStrategy) Admit(context.Context, AdmissionRequest) error {
	s.admitCalls++
	return nil
}

func (s *fakeAdmissionStrategy) Revert(context.Context, RevertRequest) error {
	s.revertCalls++
	return nil
}
