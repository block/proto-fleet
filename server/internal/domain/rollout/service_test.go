package rollout

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
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

func TestDeriveGroupProjectionKeepsDimensionsIndependent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		children    []Rollout
		activity    GroupActivity
		action      bool
		lifecycle   GroupLifecycle
		outcome     GroupTerminalOutcome
		evidence    GroupEvidenceReadiness
		resultReady bool
	}{
		{
			name: "review outranks running and needs action is orthogonal",
			children: []Rollout{
				{State: StateRunning},
				{State: StateReview},
			},
			activity: GroupActivityReview, action: true, lifecycle: GroupLifecycleActive,
			outcome: GroupTerminalOutcomePending, evidence: GroupEvidencePending,
		},
		{
			name: "failed admission has highest activity priority",
			children: []Rollout{
				{State: StateCreated, FailedAdmission: true},
				{State: StateReview},
			},
			activity: GroupActivityFailedAdmission, action: true, lifecycle: GroupLifecycleActive,
			outcome: GroupTerminalOutcomePending, evidence: GroupEvidencePending,
		},
		{
			name: "paused outranks reverting",
			children: []Rollout{
				{State: StateReverting},
				{State: StatePaused},
			},
			activity: GroupActivityPaused, action: true, lifecycle: GroupLifecycleActive,
			outcome: GroupTerminalOutcomePending, evidence: GroupEvidencePending,
		},
		{
			name: "uniform unsuccessful outcome is not mixed",
			children: []Rollout{
				{State: StateAborted},
				{State: StateAborted},
			},
			activity: GroupActivitySettled, lifecycle: GroupLifecycleTerminal,
			outcome: GroupTerminalOutcomeAborted, evidence: GroupEvidenceReady, resultReady: true,
		},
		{
			name: "uniform successful outcome",
			children: []Rollout{
				{State: StateCompleted},
				{State: StateCompleted},
			},
			activity: GroupActivitySettled, lifecycle: GroupLifecycleTerminal,
			outcome: GroupTerminalOutcomeSuccessful, evidence: GroupEvidenceReady, resultReady: true,
		},
		{
			name: "uniform reverted outcome",
			children: []Rollout{
				{State: StateReverted},
				{State: StateReverted},
			},
			activity: GroupActivitySettled, lifecycle: GroupLifecycleTerminal,
			outcome: GroupTerminalOutcomeReverted, evidence: GroupEvidenceReady, resultReady: true,
		},
		{
			name: "uniform completed with failures outcome",
			children: []Rollout{
				{State: StateCompletedWithFailures},
				{State: StateCompletedWithFailures},
			},
			activity: GroupActivitySettled, lifecycle: GroupLifecycleTerminal,
			outcome:  GroupTerminalOutcomeCompletedWithFailures,
			evidence: GroupEvidenceReady, resultReady: true,
		},
		{
			name:     "single unsuccessful outcome is not mixed",
			children: []Rollout{{State: StateCompletedWithFailures}},
			activity: GroupActivitySettled, lifecycle: GroupLifecycleTerminal,
			outcome:  GroupTerminalOutcomeCompletedWithFailures,
			evidence: GroupEvidenceReady, resultReady: true,
		},
		{
			name: "different terminal outcomes are mixed",
			children: []Rollout{
				{State: StateCompleted},
				{State: StateReverted},
			},
			activity: GroupActivitySettled, lifecycle: GroupLifecycleTerminal,
			outcome: GroupTerminalOutcomeMixed, evidence: GroupEvidenceReady, resultReady: true,
		},
		{
			name: "required evidence remains pending after terminal child",
			children: []Rollout{{
				State:          StateCompleted,
				HashratePolicy: &HashratePolicy{MaxDropBasisPoints: 100, HealthyDurationSeconds: 30},
				Batches: []Batch{{
					State: BatchStateCompleted, EvidenceStatus: EvidenceStatusCollecting,
				}},
			}},
			activity: GroupActivitySettled, lifecycle: GroupLifecycleTerminal,
			outcome: GroupTerminalOutcomeSuccessful, evidence: GroupEvidencePending,
		},
		{
			name: "required persisted evidence makes result ready",
			children: []Rollout{{
				State:          StateCompleted,
				HashratePolicy: &HashratePolicy{MaxDropBasisPoints: 100, HealthyDurationSeconds: 30},
				Batches: []Batch{{
					State: BatchStateCompleted, EvidenceStatus: EvidenceStatusHealthy,
					PostWindowFinalized: true,
				}},
			}},
			activity: GroupActivitySettled, lifecycle: GroupLifecycleTerminal,
			outcome: GroupTerminalOutcomeSuccessful, evidence: GroupEvidenceReady, resultReady: true,
		},
		{
			name:     "lone post-terminal revert keeps parent terminal",
			children: []Rollout{{State: StateReverting}},
			activity: GroupActivityReverting, lifecycle: GroupLifecycleTerminal,
			outcome: GroupTerminalOutcomePending, evidence: GroupEvidencePending,
		},
		{
			name: "mixed post-terminal revert keeps parent terminal",
			children: []Rollout{
				{State: StateReverting},
				{State: StateCompleted},
			},
			activity: GroupActivityReverting, lifecycle: GroupLifecycleTerminal,
			outcome: GroupTerminalOutcomePending, evidence: GroupEvidencePending,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := DeriveGroupProjection(test.children)
			assert.Equal(t, test.activity, got.Activity)
			assert.Equal(t, test.action, got.NeedsAction)
			assert.Equal(t, test.lifecycle, got.Lifecycle)
			assert.Equal(t, test.outcome, got.TerminalOutcome)
			assert.Equal(t, test.evidence, got.EvidenceReadiness)
			assert.Equal(t, test.resultReady, got.ResultReady)
		})
	}
}

func TestServiceGroupReadsPreservePersistedSettlementAuthority(t *testing.T) {
	t.Parallel()

	parent := Group{
		ID:              uuid.New(),
		TerminalOutcome: GroupTerminalOutcomeSuccessful,
		ResultReady:     false,
		Children: []Rollout{{
			State: StateCompleted,
		}},
	}
	store := &fakeStore{
		groupResult:  &parent,
		groupResults: []Group{parent},
	}
	service := NewService(store)

	got, err := service.GetGroup(t.Context(), 42, parent.ID)
	require.NoError(t, err)
	assert.Equal(t, GroupLifecycleTerminal, got.Lifecycle)
	assert.Equal(t, GroupTerminalOutcomeSuccessful, got.TerminalOutcome)
	assert.Equal(t, GroupEvidencePending, got.EvidenceReadiness)
	assert.False(t, got.ResultReady)

	listed, err := service.ListGroups(t.Context(), 42)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, GroupLifecycleTerminal, listed[0].Lifecycle)
	assert.Equal(t, GroupTerminalOutcomeSuccessful, listed[0].TerminalOutcome)
	assert.Equal(t, GroupEvidencePending, listed[0].EvidenceReadiness)
	assert.False(t, listed[0].ResultReady)
}

func TestFingerprintCreateRejectsUnmarshalableSnapshots(t *testing.T) {
	t.Parallel()

	_, err := fingerprintCreate(CreateRequest{
		SourceSnapshot: map[string]any{"invalid": func() {}},
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "marshal rollout creation fingerprint")
}

func TestValidateCreateRequestHashratePolicy(t *testing.T) {
	t.Parallel()

	validRequest := func() CreateRequest {
		return CreateRequest{
			OrgID:          42,
			Name:           "policy rollout",
			StrategyKey:    "fake",
			Batches:        []CreateBatch{{Members: []CreateMember{{DeviceIdentifier: "miner-a"}}}},
			IdempotencyKey: "policy-rollout",
			Reason:         "test policy validation",
			ActorUserID:    9,
		}
	}
	tests := []struct {
		name    string
		policy  *HashratePolicy
		wantErr string
	}{
		{name: "manual mode"},
		{
			name:   "minimum values",
			policy: &HashratePolicy{MaxDropBasisPoints: 0, HealthyDurationSeconds: 10},
		},
		{
			name:   "default UI values",
			policy: &HashratePolicy{MaxDropBasisPoints: 10, HealthyDurationSeconds: 30},
		},
		{
			name:   "maximum values",
			policy: &HashratePolicy{MaxDropBasisPoints: 10000, HealthyDurationSeconds: 1800},
		},
		{
			name:    "drop above maximum",
			policy:  &HashratePolicy{MaxDropBasisPoints: 10010, HealthyDurationSeconds: 30},
			wantErr: "maximum hashrate drop",
		},
		{
			name:    "drop precision",
			policy:  &HashratePolicy{MaxDropBasisPoints: 1, HealthyDurationSeconds: 30},
			wantErr: "maximum hashrate drop",
		},
		{
			name:    "duration below minimum",
			policy:  &HashratePolicy{MaxDropBasisPoints: 10, HealthyDurationSeconds: 0},
			wantErr: "healthy duration",
		},
		{
			name:    "duration above maximum",
			policy:  &HashratePolicy{MaxDropBasisPoints: 10, HealthyDurationSeconds: 1810},
			wantErr: "healthy duration",
		},
		{
			name:    "duration precision",
			policy:  &HashratePolicy{MaxDropBasisPoints: 10, HealthyDurationSeconds: 11},
			wantErr: "healthy duration",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			req := validRequest()
			req.HashratePolicy = test.policy
			err := validateCreateRequest(req)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestFingerprintCreateIncludesHashratePolicy(t *testing.T) {
	t.Parallel()

	base := CreateRequest{Name: "rollout"}
	manual, err := fingerprintCreate(base)
	require.NoError(t, err)

	withPolicy := base
	withPolicy.HashratePolicy = &HashratePolicy{
		MaxDropBasisPoints:     10,
		HealthyDurationSeconds: 30,
	}
	automatic, err := fingerprintCreate(withPolicy)
	require.NoError(t, err)

	assert.NotEqual(t, manual, automatic)
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

func TestServiceAdmitDefinitiveRollbackFinishesFailedControl(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	batch := Batch{ID: 7, RolloutID: rolloutID, State: BatchStateAdmitted}
	store := &fakeStore{
		getResult: &Rollout{ID: rolloutID, OrgID: 42, StrategyKey: "fake", State: StateCreated, Revision: 1},
		controlResults: []ControlResult{{
			Rollout: &Rollout{
				ID: rolloutID, OrgID: 42, StrategyKey: "fake", State: StateRunning,
				Revision: 2, Batches: []Batch{batch},
			},
			Batch: &batch, Control: Control{ID: uuid.New(), Status: ControlStatusStarted},
		}},
	}
	strategy := &fakeAdmissionStrategy{
		key: "fake",
		admitResult: AdmissionResult{
			Outcome: AdmissionOutcomeDefinitivelyRolledBack,
			Err:     errors.New("transaction rolled back"),
		},
	}

	_, err := NewService(store, strategy).Admit(t.Context(), AdmitRequest{
		OrgID: 42, RolloutID: rolloutID, BatchID: 7, ExpectedRevision: 1,
		IdempotencyKey: "admit-attempt-0", Reason: "operator approved", ActorUserID: 9,
	})

	require.ErrorContains(t, err, "transaction rolled back")
	require.Len(t, store.finishRequests, 1)
	assert.False(t, store.finishRequests[0].Success)
}

func TestServiceAdmitUnknownOutcomePreservesStartedControl(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	batch := Batch{ID: 7, RolloutID: rolloutID, State: BatchStateAdmitted}
	store := &fakeStore{
		getResult: &Rollout{ID: rolloutID, OrgID: 42, StrategyKey: "fake", State: StateCreated, Revision: 1},
		controlResults: []ControlResult{{
			Rollout: &Rollout{
				ID: rolloutID, OrgID: 42, StrategyKey: "fake", State: StateRunning,
				Revision: 2, Batches: []Batch{batch},
			},
			Batch: &batch, Control: Control{ID: uuid.New(), Status: ControlStatusStarted},
		}},
	}
	strategy := &fakeAdmissionStrategy{
		key: "fake",
		admitResult: AdmissionResult{
			Outcome: AdmissionOutcomeUnknown,
			Err:     errors.New("commit response lost"),
		},
	}

	_, err := NewService(store, strategy).Admit(t.Context(), AdmitRequest{
		OrgID: 42, RolloutID: rolloutID, BatchID: 7, ExpectedRevision: 1,
		IdempotencyKey: "admit-attempt-0", Reason: "operator approved", ActorUserID: 9,
	})

	require.ErrorContains(t, err, "replay the same idempotency key")
	assert.Empty(t, store.finishRequests)
}

func TestServiceAdmitUnknownOutcomeReconcilesOnSameKeyReplay(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	batch := Batch{ID: 7, RolloutID: rolloutID, State: BatchStateAdmitted}
	started := ControlResult{
		Rollout: &Rollout{
			ID: rolloutID, OrgID: 42, StrategyKey: "fake", State: StateRunning,
			Revision: 2, Batches: []Batch{batch},
		},
		Batch: &batch, Control: Control{ID: uuid.New(), Status: ControlStatusStarted},
	}
	replay := started
	replay.Replayed = true
	store := &fakeStore{
		getResult:      &Rollout{ID: rolloutID, OrgID: 42, StrategyKey: "fake", State: StateRunning, Revision: 2},
		controlResults: []ControlResult{started, replay},
	}
	strategy := &fakeAdmissionStrategy{
		key: "fake",
		admitResults: []AdmissionResult{
			{Outcome: AdmissionOutcomeUnknown, Err: errors.New("commit response lost")},
			{Outcome: AdmissionOutcomeCommitted},
		},
	}
	service := NewService(store, strategy)
	request := AdmitRequest{
		OrgID: 42, RolloutID: rolloutID, BatchID: 7, ExpectedRevision: 1,
		IdempotencyKey: "admit-attempt-0", Reason: "operator approved", ActorUserID: 9,
	}

	_, err := service.Admit(t.Context(), request)
	require.ErrorContains(t, err, "replay the same idempotency key")
	_, err = service.Admit(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, 2, strategy.admitCalls)
	require.Len(t, store.finishRequests, 1)
	assert.True(t, store.finishRequests[0].Success)
}

func TestServiceAdmitProjectsActivityOnlyAfterUnknownOutcomeSettles(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	parentID := uuid.New()
	batch := Batch{ID: 7, RolloutID: rolloutID, State: BatchStateAdmitted}
	child := &Rollout{
		ID: rolloutID, OrgID: 42, StrategyKey: "fake", State: StateRunning,
		Revision: 2, Batches: []Batch{batch}, GroupID: &parentID,
	}
	started := ControlResult{
		Rollout: child,
		Batch:   &batch,
		Control: Control{ID: uuid.New(), Status: ControlStatusStarted},
	}
	replay := started
	replay.Replayed = true
	store := &fakeStore{
		getResult:      child,
		controlResults: []ControlResult{started, replay},
	}
	strategy := &fakeAdmissionStrategy{
		key: "fake",
		admitResults: []AdmissionResult{
			{Outcome: AdmissionOutcomeUnknown, Err: errors.New("commit response lost")},
			{Outcome: AdmissionOutcomeCommitted},
		},
	}
	logger := &recordingActivityLogger{}
	service := NewServiceWithActivity(store, logger, strategy)
	request := AdmitRequest{
		OrgID: 42, RolloutID: rolloutID, BatchID: 7, ExpectedRevision: 1,
		IdempotencyKey: "admit-attempt-0", Reason: "operator approved", ActorUserID: 9,
	}

	_, err := service.Admit(t.Context(), request)
	require.ErrorContains(t, err, "replay the same idempotency key")
	assert.Empty(t, logger.events)

	_, err = service.Admit(t.Context(), request)
	require.NoError(t, err)
	require.Len(t, logger.events, 1)
	assert.Equal(t, activitymodels.ResultSuccess, logger.events[0].Result)
	assert.Nil(t, logger.events[0].ErrorMessage)
}

func TestServiceRevertReplayResumesStartedStrategyWork(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	revertAuthorityID := uuid.New()
	current := &Rollout{
		ID:                rolloutID,
		OrgID:             42,
		StrategyKey:       "fake",
		State:             StateReverting,
		Revision:          4,
		RevertAuthorityID: &revertAuthorityID,
	}
	store := &fakeStore{
		getResult:     current,
		controlReplay: true,
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
	assert.Equal(t, 0, strategy.validateRevertCalls)
	assert.Equal(t, 1, strategy.revertCalls)
	assert.Equal(t, 1, store.finishCalls)
}

func TestServiceRevertDefinitiveRollbackFinishesFailedControl(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	current := &Rollout{
		ID: rolloutID, OrgID: 42, StrategyKey: "fake",
		State: StateCompleted, Revision: 3,
	}
	store := &fakeStore{
		getResult: current,
		controlResults: []ControlResult{{
			Rollout: &Rollout{
				ID: rolloutID, OrgID: 42, StrategyKey: "fake",
				State: StateReverting, Revision: 4,
			},
			Control: Control{ID: uuid.New(), Status: ControlStatusStarted},
		}},
	}
	strategy := &fakeAdmissionStrategy{
		key: "fake",
		revertResult: RevertResult{
			Outcome: RevertOutcomeDefinitivelyRolledBack,
			Err:     errors.New("revert transaction rolled back"),
		},
	}

	_, err := NewService(store, strategy).Revert(t.Context(), ControlRequest{
		OrgID: 42, RolloutID: rolloutID, ExpectedRevision: 3,
		IdempotencyKey: "revert-definitive-rollback", Reason: "operator approved", ActorUserID: 9,
	})

	require.ErrorContains(t, err, "revert transaction rolled back")
	require.Len(t, store.finishRequests, 1)
	assert.False(t, store.finishRequests[0].Success)
}

func TestServiceRevertUnknownOutcomePreservesStartedControl(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	current := &Rollout{
		ID: rolloutID, OrgID: 42, StrategyKey: "fake",
		State: StateCompleted, Revision: 3,
	}
	store := &fakeStore{
		getResult: current,
		controlResults: []ControlResult{{
			Rollout: &Rollout{
				ID: rolloutID, OrgID: 42, StrategyKey: "fake",
				State: StateReverting, Revision: 4,
			},
			Control: Control{ID: uuid.New(), Status: ControlStatusStarted},
		}},
	}
	strategy := &fakeAdmissionStrategy{
		key: "fake",
		revertResult: RevertResult{
			Outcome: RevertOutcomeUnknown,
			Err:     errors.New("commit response lost"),
		},
	}

	_, err := NewService(store, strategy).Revert(t.Context(), ControlRequest{
		OrgID: 42, RolloutID: rolloutID, ExpectedRevision: 3,
		IdempotencyKey: "revert-outcome-unknown", Reason: "operator approved", ActorUserID: 9,
	})

	require.ErrorContains(t, err, "replay the same idempotency key")
	assert.Empty(t, store.finishRequests)
}

func TestServiceRevertUnknownOutcomeReconcilesOnSameKeyReplay(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	current := &Rollout{
		ID: rolloutID, OrgID: 42, StrategyKey: "fake",
		State: StateCompleted, Revision: 3,
	}
	started := ControlResult{
		Rollout: &Rollout{
			ID: rolloutID, OrgID: 42, StrategyKey: "fake",
			State: StateReverting, Revision: 4,
		},
		Control: Control{ID: uuid.New(), Status: ControlStatusStarted},
	}
	replay := started
	replay.Replayed = true
	store := &fakeStore{
		getResult:      current,
		controlResults: []ControlResult{started, replay},
	}
	strategy := &fakeAdmissionStrategy{
		key: "fake",
		revertResults: []RevertResult{
			{Outcome: RevertOutcomeUnknown, Err: errors.New("commit response lost")},
			{Outcome: RevertOutcomeCommitted},
		},
	}
	service := NewService(store, strategy)
	request := ControlRequest{
		OrgID: 42, RolloutID: rolloutID, ExpectedRevision: 3,
		IdempotencyKey: "revert-replay-after-unknown", Reason: "operator approved", ActorUserID: 9,
	}

	_, err := service.Revert(t.Context(), request)
	require.ErrorContains(t, err, "replay the same idempotency key")
	_, err = service.Revert(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, 2, strategy.revertCalls)
	require.Len(t, store.finishRequests, 1)
	assert.True(t, store.finishRequests[0].Success)
}

func TestServiceRevertWithPriorAuthorityStillValidatesNewControl(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	revertAuthorityID := uuid.New()
	store := &fakeStore{
		getResult: &Rollout{
			ID:                rolloutID,
			OrgID:             42,
			StrategyKey:       "fake",
			State:             StateCompletedWithFailures,
			Revision:          5,
			RevertAuthorityID: &revertAuthorityID,
		},
	}
	validationErr := errors.New("no succeeded members")
	strategy := &fakeAdmissionStrategy{
		key:               "fake",
		validateRevertErr: validationErr,
	}
	svc := NewService(store, strategy)

	_, err := svc.Revert(t.Context(), ControlRequest{
		OrgID:            42,
		RolloutID:        rolloutID,
		ExpectedRevision: 5,
		IdempotencyKey:   "new-revert-after-failure",
		Reason:           "operator retry",
		ActorUserID:      9,
	})
	require.ErrorIs(t, err, validationErr)
	assert.Equal(t, 1, strategy.validateRevertCalls)
	assert.Empty(t, store.controlRequests)
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

func TestNewServiceWithActivityRequiresLogger(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(
		t,
		"rollout service: activity logger is required",
		func() { NewServiceWithActivity(&fakeStore{}, nil) },
	)
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

func TestServiceManualContinueRemainsAvailableAfterAutomationError(t *testing.T) {
	t.Parallel()

	rolloutID := uuid.New()
	pending := Batch{
		ID:        8,
		RolloutID: rolloutID,
		State:     BatchStatePending,
	}
	admitted := pending
	admitted.State = BatchStateAdmitted
	store := &fakeStore{
		getResult: &Rollout{
			ID:              rolloutID,
			OrgID:           42,
			StrategyKey:     "fake",
			State:           StateReview,
			Revision:        7,
			CreatedByUserID: 9,
			Batches: []Batch{
				{ID: 7, EvidenceStatus: EvidenceStatusAutomationError, State: BatchStateCompleted},
				pending,
			},
		},
		controlResults: []ControlResult{{
			Rollout: &Rollout{
				ID:          rolloutID,
				OrgID:       42,
				StrategyKey: "fake",
				State:       StateRunning,
				Revision:    8,
				Batches:     []Batch{admitted},
			},
			Batch:   &admitted,
			Control: Control{ID: uuid.New(), Status: ControlStatusStarted},
		}},
	}
	strategy := &fakeAdmissionStrategy{key: "fake"}
	svc := NewService(store, strategy)

	_, err := svc.Continue(t.Context(), AdmitRequest{
		OrgID:            42,
		RolloutID:        rolloutID,
		ExpectedRevision: 7,
		IdempotencyKey:   "operator-continue-after-automation-error",
		Reason:           "operator reviewed automation failure",
		ActorUserID:      9,
		ActorType:        ActorTypeUser,
	})

	require.NoError(t, err)
	require.Len(t, store.controlRequests, 1)
	assert.Equal(t, "operator-continue-after-automation-error", store.controlRequests[0].IdempotencyKey)
	assert.Equal(t, ActorTypeUser, store.controlRequests[0].ActorType)
	assert.Equal(t, 1, strategy.admitCalls)
}

type fakeStore struct {
	getResult        *Rollout
	getErr           error
	groupResult      *Group
	groupResults     []Group
	controlResults   []ControlResult
	controlErr       error
	controlRequests  []ControlRequest
	controlReplay    bool
	controlReplayErr error
	finishCalls      int
	finishRequests   []FinishControlRequest
}

func (s *fakeStore) Create(context.Context, CreateRequest) (CreateResult, error) {
	return CreateResult{}, errors.New("unexpected Create call")
}

func (s *fakeStore) Get(context.Context, int64, uuid.UUID) (*Rollout, error) {
	return s.getResult, s.getErr
}

func (s *fakeStore) GetGroup(context.Context, int64, uuid.UUID) (*Group, error) {
	if s.groupResult == nil {
		return nil, errors.New("unexpected GetGroup call")
	}
	return s.groupResult, nil
}

func (s *fakeStore) List(context.Context, int64, []State) ([]Rollout, error) {
	return nil, errors.New("unexpected List call")
}

func (s *fakeStore) ListGroups(context.Context, int64) ([]Group, error) {
	if s.groupResults == nil {
		return nil, errors.New("unexpected ListGroups call")
	}
	return s.groupResults, nil
}

func (s *fakeStore) CheckControlReplay(context.Context, ControlRequest) (bool, error) {
	return s.controlReplay, s.controlReplayErr
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

func (s *fakeStore) FinishControl(_ context.Context, req FinishControlRequest) (*Rollout, error) {
	s.finishCalls++
	s.finishRequests = append(s.finishRequests, req)
	return s.getResult, nil
}

func (s *fakeStore) UpdateMember(context.Context, MemberUpdateRequest) (Member, error) {
	return Member{}, errors.New("unexpected UpdateMember call")
}

func (s *fakeStore) CaptureEvidence(context.Context, EvidenceRequest) ([]Evidence, error) {
	return nil, errors.New("unexpected CaptureEvidence call")
}

type recordingActivityLogger struct {
	events []activitymodels.Event
}

func (l *recordingActivityLogger) Log(_ context.Context, event activitymodels.Event) {
	l.events = append(l.events, event)
}

type fakeAdmissionStrategy struct {
	key                 string
	admitCalls          int
	admitResult         AdmissionResult
	admitResults        []AdmissionResult
	validateRevertCalls int
	validateRevertErr   error
	revertCalls         int
	revertResult        RevertResult
	revertResults       []RevertResult
}

func (s *fakeAdmissionStrategy) Key() string {
	return s.key
}

func (s *fakeAdmissionStrategy) Admit(context.Context, AdmissionRequest) AdmissionResult {
	s.admitCalls++
	if len(s.admitResults) > 0 {
		result := s.admitResults[0]
		s.admitResults = s.admitResults[1:]
		return result
	}
	if s.admitResult.Outcome == "" {
		return AdmissionResult{Outcome: AdmissionOutcomeCommitted}
	}
	return s.admitResult
}

func (s *fakeAdmissionStrategy) Revert(context.Context, RevertRequest) RevertResult {
	s.revertCalls++
	if len(s.revertResults) > 0 {
		result := s.revertResults[0]
		s.revertResults = s.revertResults[1:]
		return result
	}
	if s.revertResult.Outcome == "" {
		return RevertResult{Outcome: RevertOutcomeCommitted}
	}
	return s.revertResult
}

func (s *fakeAdmissionStrategy) ValidateRevert(context.Context, RevertValidationRequest) error {
	s.validateRevertCalls++
	return s.validateRevertErr
}
