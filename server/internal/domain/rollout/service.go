package rollout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/cryptohash"
)

type Service struct {
	store      Store
	strategies map[string]AdmissionStrategy
	activity   ActivityLogger
}

type ActivityLogger interface {
	Log(ctx context.Context, event activitymodels.Event)
}

// NoopActivityLogger is an explicit activity sink for tests and deployments
// that intentionally do not persist rollout activity.
type NoopActivityLogger struct{}

func (NoopActivityLogger) Log(context.Context, activitymodels.Event) {}

type AdmitRequest struct {
	OrgID             int64
	RolloutID         uuid.UUID
	BatchID           int64
	ExpectedRevision  int64
	IdempotencyKey    string
	Reason            string
	ActorUserID       int64
	ActorType         ActorType
	ActorCredentialID *string
}

func NewService(store Store, strategies ...AdmissionStrategy) *Service {
	return NewServiceWithActivity(store, NoopActivityLogger{}, strategies...)
}

func NewServiceWithActivity(
	store Store,
	activity ActivityLogger,
	strategies ...AdmissionStrategy,
) *Service {
	if activity == nil {
		panic("rollout service: activity logger is required")
	}
	service := &Service{
		store:      store,
		activity:   activity,
		strategies: make(map[string]AdmissionStrategy, len(strategies)),
	}
	for _, strategy := range strategies {
		if strategy == nil || strings.TrimSpace(strategy.Key()) == "" {
			continue
		}
		service.strategies[strategy.Key()] = strategy
	}
	return service
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Rollout, error) {
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}
	strategy, ok := s.strategies[req.StrategyKey]
	if !ok {
		return nil, strategyUnavailable(req.StrategyKey)
	}
	if creation, validatesCreate := strategy.(CreationStrategy); validatesCreate {
		if err := creation.ValidateCreate(ctx, req); err != nil {
			return nil, fleeterror.NewFailedPreconditionErrorf(
				"rollout creation failed: %w",
				err,
			)
		}
	}
	if req.ID == uuid.Nil {
		req.ID = uuid.New()
	}
	fingerprint, err := fingerprintCreate(req)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf(
			"create rollout request fingerprint: %w",
			err,
		)
	}
	req.RequestFingerprint = fingerprint
	result, err := s.store.Create(ctx, req)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return result.Rollout, nil
}

func (s *Service) Get(ctx context.Context, orgID int64, rolloutID uuid.UUID) (*Rollout, error) {
	if orgID <= 0 || rolloutID == uuid.Nil {
		return nil, fleeterror.NewInvalidArgumentError("organization and rollout IDs are required")
	}
	result, err := s.store.Get(ctx, orgID, rolloutID)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return result, nil
}

func (s *Service) GetGroup(ctx context.Context, orgID int64, groupID uuid.UUID) (*Group, error) {
	if orgID <= 0 || groupID == uuid.Nil {
		return nil, fleeterror.NewInvalidArgumentError("organization and parent rollout IDs are required")
	}
	result, err := s.store.GetGroup(ctx, orgID, groupID)
	if err != nil {
		return nil, mapStoreError(err)
	}
	projection := DeriveGroupProjection(result.Children)
	result.Lifecycle = projection.Lifecycle
	result.Activity = projection.Activity
	result.NeedsAction = projection.NeedsAction
	result.TerminalOutcome = projection.TerminalOutcome
	result.EvidenceReadiness = projection.EvidenceReadiness
	result.ResultReady = projection.ResultReady
	return result, nil
}

func (s *Service) ListGroups(ctx context.Context, orgID int64) ([]Group, error) {
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("organization ID is required")
	}
	groups, err := s.store.ListGroups(ctx, orgID)
	if err != nil {
		return nil, mapStoreError(err)
	}
	for index := range groups {
		projection := DeriveGroupProjection(groups[index].Children)
		groups[index].Lifecycle = projection.Lifecycle
		groups[index].Activity = projection.Activity
		groups[index].NeedsAction = projection.NeedsAction
		groups[index].TerminalOutcome = projection.TerminalOutcome
		groups[index].EvidenceReadiness = projection.EvidenceReadiness
		groups[index].ResultReady = projection.ResultReady
	}
	return groups, nil
}

// DeriveGroupProjection reduces child state without granting the parent any
// control authority. Each dimension is calculated independently so attention
// never masquerades as lifecycle and uniform unsuccessful outcomes stay
// distinct from genuinely mixed outcomes.
func DeriveGroupProjection(children []Rollout) GroupProjection {
	projection := GroupProjection{
		Lifecycle:         GroupLifecycleActive,
		Activity:          GroupActivityCreated,
		TerminalOutcome:   GroupTerminalOutcomePending,
		EvidenceReadiness: GroupEvidencePending,
	}
	if len(children) == 0 {
		return projection
	}

	allTerminal := true
	hasFailedAdmission := false
	hasAttention := false
	hasReview := false
	hasPaused := false
	hasReverting := false
	hasFinalizing := false
	hasRunning := false
	hasCreated := false
	requiredEvidenceReady := true
	outcomes := make(map[GroupTerminalOutcome]struct{}, len(children))

	for index := range children {
		child := &children[index]
		if !child.State.IsTerminal() {
			allTerminal = false
		}
		if child.FailedAdmission {
			hasFailedAdmission = true
		}
		switch child.State {
		case StateReview:
			hasReview = true
		case StatePaused:
			hasPaused = true
		case StateReverting:
			hasReverting = true
		case StateRunning:
			if childHasOnlyTerminalMembers(*child) {
				hasFinalizing = true
			} else {
				hasRunning = true
			}
		case StateCreated:
			hasCreated = true
		case StateAborted, StateCompleted, StateCompletedWithFailures, StateReverted:
		}
		if childNeedsAttention(*child) {
			hasAttention = true
		}
		if child.HashratePolicy != nil && !childEvidenceReady(*child) {
			requiredEvidenceReady = false
		}
		if child.State.IsTerminal() {
			outcomes[groupOutcomeForState(child.State)] = struct{}{}
		}
	}

	projection.NeedsAction = hasFailedAdmission || hasAttention || hasReview || hasPaused
	switch {
	case hasFailedAdmission:
		projection.Activity = GroupActivityFailedAdmission
	case hasAttention:
		projection.Activity = GroupActivityAttentionRequired
	case hasReview:
		projection.Activity = GroupActivityReview
	case hasPaused:
		projection.Activity = GroupActivityPaused
	case hasReverting:
		projection.Activity = GroupActivityReverting
	case hasFinalizing:
		projection.Activity = GroupActivityFinalizing
	case hasRunning:
		projection.Activity = GroupActivityRunning
	case hasCreated:
		projection.Activity = GroupActivityCreated
	default:
		projection.Activity = GroupActivitySettled
	}

	if !allTerminal {
		return projection
	}
	projection.Lifecycle = GroupLifecycleTerminal
	if len(outcomes) == 1 {
		for outcome := range outcomes {
			projection.TerminalOutcome = outcome
		}
	} else {
		projection.TerminalOutcome = GroupTerminalOutcomeMixed
	}
	if requiredEvidenceReady {
		projection.EvidenceReadiness = GroupEvidenceReady
		projection.ResultReady = true
	}
	return projection
}

func childNeedsAttention(child Rollout) bool {
	for _, member := range child.Members {
		if member.State == MemberStateAttentionRequired || member.State == MemberStateFailed {
			return true
		}
	}
	for _, batch := range child.Batches {
		if batch.EvidenceStatus == EvidenceStatusHeld ||
			batch.EvidenceStatus == EvidenceStatusAutomationError {
			return true
		}
	}
	return false
}

func childHasOnlyTerminalMembers(child Rollout) bool {
	if len(child.Members) == 0 {
		return false
	}
	for _, member := range child.Members {
		switch member.State {
		case MemberStateSucceeded, MemberStateFailed, MemberStateAttentionRequired,
			MemberStateCancelled, MemberStateReverted:
		case MemberStatePending, MemberStateAdmitted, MemberStateReverting:
			return false
		}
	}
	return true
}

func childEvidenceReady(child Rollout) bool {
	if len(child.Batches) == 0 {
		return false
	}
	for _, batch := range child.Batches {
		if !batch.PostWindowFinalized {
			return false
		}
	}
	return true
}

func groupOutcomeForState(state State) GroupTerminalOutcome {
	switch state {
	case StateCompleted:
		return GroupTerminalOutcomeSuccessful
	case StateReverted:
		return GroupTerminalOutcomeReverted
	case StateAborted:
		return GroupTerminalOutcomeAborted
	case StateCompletedWithFailures:
		return GroupTerminalOutcomeCompletedWithFailures
	case StateCreated, StateRunning, StatePaused, StateReview, StateReverting:
		return GroupTerminalOutcomePending
	default:
		return GroupTerminalOutcome(state)
	}
}

func (s *Service) List(ctx context.Context, orgID int64, states []State) ([]Rollout, error) {
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("organization ID is required")
	}
	result, err := s.store.List(ctx, orgID, states)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return result, nil
}

func (s *Service) Admit(ctx context.Context, req AdmitRequest) (*Rollout, error) {
	result, err := s.runAdmission(ctx, req, ControlOperationAdmit)
	s.projectControlActivity(ctx, controlRequestFromAdmit(req), ControlOperationAdmit, result, err)
	return result, err
}

func (s *Service) Continue(ctx context.Context, req AdmitRequest) (*Rollout, error) {
	req.BatchID = 0
	result, err := s.runAdmission(ctx, req, ControlOperationContinue)
	s.projectControlActivity(ctx, controlRequestFromAdmit(req), ControlOperationContinue, result, err)
	return result, err
}

func (s *Service) runAdmission(
	ctx context.Context,
	req AdmitRequest,
	operation ControlOperation,
) (*Rollout, error) {
	if err := validateAdmitRequest(req); err != nil {
		return nil, err
	}
	current, err := s.Get(ctx, req.OrgID, req.RolloutID)
	if err != nil {
		return nil, err
	}
	strategy, ok := s.strategies[current.StrategyKey]
	if !ok {
		return nil, strategyUnavailable(current.StrategyKey)
	}

	control := ControlRequest{
		OrgID:             req.OrgID,
		RolloutID:         req.RolloutID,
		BatchID:           req.BatchID,
		ExpectedRevision:  req.ExpectedRevision,
		Operation:         operation,
		IdempotencyKey:    req.IdempotencyKey,
		Reason:            req.Reason,
		ActorUserID:       req.ActorUserID,
		ActorType:         req.ActorType,
		ActorCredentialID: req.ActorCredentialID,
	}
	control.RequestFingerprint = fingerprintControl(control)
	result, err := s.store.ApplyControl(ctx, control)
	if err != nil {
		return nil, mapStoreError(err)
	}
	if result.Replayed && result.Control.Status != ControlStatusStarted {
		return replayResult(result)
	}
	if result.Batch == nil {
		return nil, fleeterror.NewInternalError("rollout admission did not select a batch")
	}

	admission := strategy.Admit(ctx, AdmissionRequest{
		Rollout:        *result.Rollout,
		Batch:          *result.Batch,
		ControlID:      result.Control.ID,
		IdempotencyKey: req.IdempotencyKey,
	})
	switch admission.Outcome {
	case AdmissionOutcomeCommitted:
		if _, err := s.store.FinishControl(ctx, FinishControlRequest{
			OrgID:     req.OrgID,
			RolloutID: req.RolloutID,
			ControlID: result.Control.ID,
			Success:   true,
		}); err != nil {
			return nil, mapStoreError(err)
		}
		return result.Rollout, nil
	case AdmissionOutcomeDefinitivelyRolledBack:
		admissionErr := admission.Err
		if admissionErr == nil {
			admissionErr = errors.New("admission transaction rolled back")
		}
		_, finishErr := s.store.FinishControl(ctx, FinishControlRequest{
			OrgID:        req.OrgID,
			RolloutID:    req.RolloutID,
			ControlID:    result.Control.ID,
			Success:      false,
			ErrorMessage: admissionErr.Error(),
		})
		if finishErr != nil {
			return nil, fleeterror.NewInternalErrorf(
				"rollout admission failed and could not record its cause: %v; record error: %w",
				admissionErr,
				finishErr,
			)
		}
		return nil, fleeterror.NewFailedPreconditionErrorf(
			"rollout admission failed: %w",
			admissionErr,
		)
	case AdmissionOutcomeUnknown:
		if admission.Err != nil {
			return nil, fleeterror.NewInternalErrorf(
				"rollout admission transaction outcome is unknown; replay the same idempotency key: %w",
				admission.Err,
			)
		}
		return nil, fleeterror.NewInternalError(
			"rollout admission transaction outcome is unknown; replay the same idempotency key",
		)
	default:
		return nil, fleeterror.NewInternalErrorf(
			"rollout admission returned unknown outcome %q",
			admission.Outcome,
		)
	}
}

func (s *Service) Pause(ctx context.Context, req ControlRequest) (*Rollout, error) {
	result, err := s.applySimpleControl(ctx, req, ControlOperationPause)
	s.projectControlActivity(ctx, req, ControlOperationPause, result, err)
	return result, err
}

func (s *Service) Resume(ctx context.Context, req ControlRequest) (*Rollout, error) {
	result, err := s.applySimpleControl(ctx, req, ControlOperationResume)
	s.projectControlActivity(ctx, req, ControlOperationResume, result, err)
	return result, err
}

func (s *Service) Abort(ctx context.Context, req ControlRequest) (*Rollout, error) {
	result, err := s.applySimpleControl(ctx, req, ControlOperationAbort)
	s.projectControlActivity(ctx, req, ControlOperationAbort, result, err)
	return result, err
}

func (s *Service) Complete(ctx context.Context, req ControlRequest) (*Rollout, error) {
	result, err := s.complete(ctx, req)
	s.projectControlActivity(ctx, req, ControlOperationComplete, result, err)
	return result, err
}

func (s *Service) complete(ctx context.Context, req ControlRequest) (*Rollout, error) {
	if err := validateControlRequest(req); err != nil {
		return nil, err
	}
	current, err := s.Get(ctx, req.OrgID, req.RolloutID)
	if err != nil {
		return nil, err
	}
	completion, hasCompletion := s.strategies[current.StrategyKey].(CompletionStrategy)
	if hasCompletion {
		if err := completion.ValidateComplete(ctx, CompletionRequest{
			Rollout:      *current,
			WithFailures: req.WithFailures,
		}); err != nil {
			return nil, fleeterror.NewFailedPreconditionErrorf(
				"rollout cannot complete: %w",
				err,
			)
		}
	}

	req.Operation = ControlOperationComplete
	req.RequestFingerprint = fingerprintControl(req)
	result, err := s.store.ApplyControl(ctx, req)
	if err != nil {
		return nil, mapStoreError(err)
	}
	if hasCompletion {
		if err := completion.Complete(ctx, CompletionRequest{
			Rollout:      *result.Rollout,
			WithFailures: req.WithFailures,
		}); err != nil {
			return nil, fleeterror.NewFailedPreconditionErrorf(
				"rollout completed but lane finalization is pending: %w",
				err,
			)
		}
	}
	if result.Replayed && result.Control.Status != ControlStatusStarted {
		return replayResult(result)
	}
	return result.Rollout, nil
}

func (s *Service) Revert(ctx context.Context, req ControlRequest) (*Rollout, error) {
	result, err := s.revert(ctx, req)
	s.projectControlActivity(ctx, req, ControlOperationRevert, result, err)
	return result, err
}

func (s *Service) revert(ctx context.Context, req ControlRequest) (*Rollout, error) {
	if err := validateControlRequest(req); err != nil {
		return nil, err
	}
	current, err := s.Get(ctx, req.OrgID, req.RolloutID)
	if err != nil {
		return nil, err
	}
	strategy, ok := s.strategies[current.StrategyKey]
	if !ok {
		return nil, strategyUnavailable(current.StrategyKey)
	}
	req.Operation = ControlOperationRevert
	req.RequestFingerprint = fingerprintControl(req)
	replayed, err := s.store.CheckControlReplay(ctx, req)
	if err != nil {
		return nil, mapStoreError(err)
	}
	if validator, validatesRevert := strategy.(RevertStrategy); validatesRevert && !replayed {
		if err := validator.ValidateRevert(ctx, RevertValidationRequest{
			Rollout: *current,
		}); err != nil {
			return nil, fleeterror.NewFailedPreconditionErrorf(
				"rollout cannot revert: %w",
				err,
			)
		}
	}

	result, err := s.store.ApplyControl(ctx, req)
	if err != nil {
		return nil, mapStoreError(err)
	}
	if result.Replayed && result.Control.Status != ControlStatusStarted {
		return replayResult(result)
	}
	err = strategy.Revert(ctx, RevertRequest{
		Rollout:        *result.Rollout,
		ControlID:      result.Control.ID,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		_, finishErr := s.store.FinishControl(ctx, FinishControlRequest{
			OrgID:        req.OrgID,
			RolloutID:    req.RolloutID,
			ControlID:    result.Control.ID,
			Success:      false,
			ErrorMessage: err.Error(),
		})
		if finishErr != nil {
			return nil, fleeterror.NewInternalErrorf(
				"rollout revert failed and could not restore its prior state: %v; record error: %w",
				err,
				finishErr,
			)
		}
		return nil, fleeterror.NewFailedPreconditionErrorf("rollout revert failed: %w", err)
	}
	finished, err := s.store.FinishControl(ctx, FinishControlRequest{
		OrgID:     req.OrgID,
		RolloutID: req.RolloutID,
		ControlID: result.Control.ID,
		Success:   true,
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	return finished, nil
}

func (s *Service) applySimpleControl(
	ctx context.Context,
	req ControlRequest,
	operation ControlOperation,
) (*Rollout, error) {
	if err := validateControlRequest(req); err != nil {
		return nil, err
	}
	req.Operation = operation
	req.RequestFingerprint = fingerprintControl(req)
	result, err := s.store.ApplyControl(ctx, req)
	if err != nil {
		return nil, mapStoreError(err)
	}
	if result.Replayed {
		return replayResult(result)
	}
	return result.Rollout, nil
}

func (s *Service) UpdateMember(ctx context.Context, req MemberUpdateRequest) (Member, error) {
	member, err := s.store.UpdateMember(ctx, req)
	if err != nil {
		return Member{}, mapStoreError(err)
	}
	return member, nil
}

func (s *Service) CaptureEvidence(ctx context.Context, req EvidenceRequest) ([]Evidence, error) {
	if req.OrgID <= 0 || req.RolloutID == uuid.Nil {
		return nil, fleeterror.NewInvalidArgumentError("organization and rollout IDs are required")
	}
	if req.Phase != EvidencePhaseBaseline && req.Phase != EvidencePhasePost {
		return nil, fleeterror.NewInvalidArgumentError("evidence phase must be baseline or post")
	}
	if !req.WindowStart.Before(req.WindowEnd) {
		return nil, fleeterror.NewInvalidArgumentError("evidence window start must precede its end")
	}
	evidence, err := s.store.CaptureEvidence(ctx, req)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return evidence, nil
}

func controlRequestFromAdmit(req AdmitRequest) ControlRequest {
	return ControlRequest{
		OrgID:             req.OrgID,
		RolloutID:         req.RolloutID,
		BatchID:           req.BatchID,
		ExpectedRevision:  req.ExpectedRevision,
		IdempotencyKey:    req.IdempotencyKey,
		Reason:            req.Reason,
		ActorUserID:       req.ActorUserID,
		ActorType:         req.ActorType,
		ActorCredentialID: req.ActorCredentialID,
	}
}

func (s *Service) projectControlActivity(
	ctx context.Context,
	req ControlRequest,
	operation ControlOperation,
	result *Rollout,
	controlErr error,
) {
	child := result
	if child == nil {
		child, _ = s.store.Get(ctx, req.OrgID, req.RolloutID)
	}
	if child == nil || child.GroupID == nil {
		return
	}
	resultType := activitymodels.ResultSuccess
	var errorMessage *string
	if controlErr != nil {
		resultType = activitymodels.ResultFailure
		message := controlErr.Error()
		errorMessage = &message
	}
	eventType := map[ControlOperation]string{
		ControlOperationAdmit:    "rollout_child.admitted",
		ControlOperationContinue: "rollout_child.continued",
		ControlOperationPause:    "rollout_child.paused",
		ControlOperationResume:   "rollout_child.resumed",
		ControlOperationAbort:    "rollout_child.aborted",
		ControlOperationRevert:   "rollout_child.reverting",
		ControlOperationComplete: "rollout_child.completed",
	}[operation]
	if eventType == "" {
		return
	}
	if operation == ControlOperationComplete && child.State == StateCompletedWithFailures {
		eventType = "rollout_child.completed_with_failures"
	}
	scopeType := "rollout_lane"
	scopeLabel := ""
	if child.LaneID != nil {
		scopeLabel = child.LaneID.String()
	}
	s.activity.Log(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventType,
		Description:    "Updated model rollout",
		Result:         resultType,
		ErrorMessage:   errorMessage,
		ScopeType:      &scopeType,
		ScopeLabel:     &scopeLabel,
		ActorType:      ActivityActorType(req.ActorType),
		OrganizationID: &req.OrgID,
		Metadata: map[string]any{
			"parent_id":          child.GroupID.String(),
			"child_id":           child.ID.String(),
			"model_identity_key": child.ModelIdentityKey,
			"manufacturer":       child.Manufacturer,
			"model":              child.Model,
			"operation":          string(operation),
		},
		IdempotencyKey: fmt.Sprintf(
			"rollout-child-control:%s:%s:%s",
			child.ID,
			operation,
			req.IdempotencyKey,
		),
	})
}

func ActivityActorType(actor ActorType) activitymodels.ActorType {
	if actor == ActorTypeSystem {
		return activitymodels.ActorSystem
	}
	return activitymodels.ActorUser
}

func NextState(
	current State,
	operation ControlOperation,
	resumeState State,
	withFailures bool,
) (State, error) {
	switch operation {
	case ControlOperationCreate:
		// Creation is not a control transition.
	case ControlOperationAdmit:
		if current == StateCreated || current == StateRunning || current == StateReview {
			return StateRunning, nil
		}
	case ControlOperationContinue:
		if current == StateReview {
			return StateRunning, nil
		}
	case ControlOperationPause:
		if current == StateRunning || current == StateReview {
			return StatePaused, nil
		}
	case ControlOperationResume:
		if current == StatePaused {
			if resumeState == StateReview {
				return StateReview, nil
			}
			return StateRunning, nil
		}
	case ControlOperationAbort:
		if current == StateCreated || current == StateRunning ||
			current == StatePaused || current == StateReview {
			return StateAborted, nil
		}
	case ControlOperationRevert:
		if current == StateAborted || current == StateCompleted ||
			current == StateCompletedWithFailures {
			return StateReverting, nil
		}
	case ControlOperationComplete:
		if current == StateReverting {
			return StateReverted, nil
		}
		if current == StateRunning || current == StateReview {
			if withFailures {
				return StateCompletedWithFailures, nil
			}
			return StateCompleted, nil
		}
	}
	return "", fmt.Errorf("%w: cannot %s a %s rollout", ErrInvalidTransition, operation, current)
}

func validateCreateRequest(req CreateRequest) error {
	if req.OrgID <= 0 || strings.TrimSpace(req.Name) == "" ||
		strings.TrimSpace(req.StrategyKey) == "" || req.ActorUserID <= 0 {
		return fleeterror.NewInvalidArgumentError("organization, name, strategy, and actor are required")
	}
	if err := ValidateHashratePolicy(req.HashratePolicy); err != nil {
		return err
	}
	if err := ValidateActorIdentity(req.ActorType, req.ActorCredentialID); err != nil {
		return err
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || strings.TrimSpace(req.Reason) == "" {
		return fleeterror.NewInvalidArgumentError("idempotency key and reason are required")
	}
	if len(req.Batches) == 0 {
		return fleeterror.NewInvalidArgumentError("at least one rollout batch is required")
	}
	seen := make(map[string]struct{})
	for _, batch := range req.Batches {
		if len(batch.Members) == 0 {
			return fleeterror.NewInvalidArgumentError("rollout batches cannot be empty")
		}
		for _, member := range batch.Members {
			identifier := strings.TrimSpace(member.DeviceIdentifier)
			if identifier == "" {
				return fleeterror.NewInvalidArgumentError("member device identifiers are required")
			}
			if _, exists := seen[identifier]; exists {
				return fleeterror.NewInvalidArgumentErrorf("duplicate rollout member %q", identifier)
			}
			seen[identifier] = struct{}{}
		}
	}
	return nil
}

func ValidateHashratePolicy(policy *HashratePolicy) error {
	if policy == nil {
		return nil
	}
	if policy.MaxDropBasisPoints < 0 ||
		policy.MaxDropBasisPoints > 10000 ||
		policy.MaxDropBasisPoints%10 != 0 {
		return fleeterror.NewInvalidArgumentError(
			"maximum hashrate drop must be between 0 and 10000 basis points in increments of 10",
		)
	}
	if policy.HealthyDurationSeconds < 10 ||
		policy.HealthyDurationSeconds > 1800 ||
		policy.HealthyDurationSeconds%10 != 0 {
		return fleeterror.NewInvalidArgumentError(
			"healthy duration must be between 10 and 1800 seconds in increments of 10",
		)
	}
	return nil
}

func validateAdmitRequest(req AdmitRequest) error {
	if req.OrgID <= 0 || req.RolloutID == uuid.Nil || req.ActorUserID <= 0 {
		return fleeterror.NewInvalidArgumentError("organization, rollout, and actor IDs are required")
	}
	if err := ValidateActorIdentity(req.ActorType, req.ActorCredentialID); err != nil {
		return err
	}
	if req.ExpectedRevision <= 0 || strings.TrimSpace(req.IdempotencyKey) == "" ||
		strings.TrimSpace(req.Reason) == "" {
		return fleeterror.NewInvalidArgumentError("expected revision, idempotency key, and reason are required")
	}
	return nil
}

func validateControlRequest(req ControlRequest) error {
	if req.OrgID <= 0 || req.RolloutID == uuid.Nil || req.ActorUserID <= 0 {
		return fleeterror.NewInvalidArgumentError("organization, rollout, and actor IDs are required")
	}
	if err := ValidateActorIdentity(req.ActorType, req.ActorCredentialID); err != nil {
		return err
	}
	if req.ExpectedRevision <= 0 || strings.TrimSpace(req.IdempotencyKey) == "" ||
		strings.TrimSpace(req.Reason) == "" {
		return fleeterror.NewInvalidArgumentError("expected revision, idempotency key, and reason are required")
	}
	return nil
}

// ValidateActorIdentity checks actor metadata shared by rollout entry points.
func ValidateActorIdentity(actorType ActorType, credentialID *string) error {
	if !actorType.Valid() {
		return fleeterror.NewInvalidArgumentError("actor type is invalid")
	}
	if credentialID != nil && strings.TrimSpace(*credentialID) == "" {
		return fleeterror.NewInvalidArgumentError("actor credential ID cannot be empty")
	}
	return nil
}

func fingerprintControl(req ControlRequest) string {
	payload := fmt.Sprintf(
		"%s\n%d\n%d\n%s\n%d\n%t",
		req.Operation,
		req.ExpectedRevision,
		req.BatchID,
		req.Reason,
		req.ActorUserID,
		req.WithFailures,
	)
	return cryptohash.Sha256Hex(payload)
}

func fingerprintCreate(req CreateRequest) (string, error) {
	fingerprintInput := struct {
		Name               string
		StrategyKey        string
		SourceChannelID    *int64
		TargetChannelID    *int64
		SourceReleaseSetID *int64
		TargetReleaseSetID *int64
		SourceSnapshot     map[string]any
		TargetSnapshot     map[string]any
		RevertSnapshot     map[string]any
		HashratePolicy     *HashratePolicy
		Batches            []CreateBatch
		Reason             string
		ActorUserID        int64
	}{
		Name:               req.Name,
		StrategyKey:        req.StrategyKey,
		SourceChannelID:    req.SourceChannelID,
		TargetChannelID:    req.TargetChannelID,
		SourceReleaseSetID: req.SourceReleaseSetID,
		TargetReleaseSetID: req.TargetReleaseSetID,
		SourceSnapshot:     req.SourceSnapshot,
		TargetSnapshot:     req.TargetSnapshot,
		RevertSnapshot:     req.RevertSnapshot,
		HashratePolicy:     req.HashratePolicy,
		Batches:            req.Batches,
		Reason:             req.Reason,
		ActorUserID:        req.ActorUserID,
	}
	encoded, err := json.Marshal(fingerprintInput)
	if err != nil {
		return "", fmt.Errorf("marshal rollout creation fingerprint: %w", err)
	}
	return cryptohash.Sha256Hex(string(encoded)), nil
}

func replayResult(result ControlResult) (*Rollout, error) {
	switch result.Control.Status {
	case ControlStatusSucceeded:
		return result.Rollout, nil
	case ControlStatusStarted:
		return nil, fleeterror.NewFailedPreconditionError("the idempotent rollout control is still in progress")
	case ControlStatusFailed:
		message := "the idempotent rollout control previously failed"
		if result.Control.ErrorMessage != nil {
			message += ": " + *result.Control.ErrorMessage
		}
		return nil, fleeterror.NewFailedPreconditionError(message)
	default:
		return nil, fleeterror.NewInternalErrorf("unknown rollout control status %q", result.Control.Status)
	}
}

func strategyUnavailable(key string) error {
	return fleeterror.NewUnimplementedErrorf("%w: %s", ErrStrategyUnavailable, key)
}

func mapStoreError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return fleeterror.NewNotFoundErrorf("%w", err)
	case errors.Is(err, ErrRevisionConflict),
		errors.Is(err, ErrInvalidTransition),
		errors.Is(err, ErrParentNotControllable):
		return fleeterror.NewFailedPreconditionErrorf("%w", err)
	case errors.Is(err, ErrIdempotencyConflict), errors.Is(err, ErrOwnershipConflict):
		return fleeterror.NewAlreadyExistsErrorf("%w", err)
	default:
		return err
	}
}
