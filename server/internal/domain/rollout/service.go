package rollout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/cryptohash"
)

type Service struct {
	store      Store
	strategies map[string]AdmissionStrategy
}

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
	service := &Service{
		store:      store,
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
	req.RequestFingerprint = fingerprintCreate(req)
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
	return s.runAdmission(ctx, req, ControlOperationAdmit)
}

func (s *Service) Continue(ctx context.Context, req AdmitRequest) (*Rollout, error) {
	req.BatchID = 0
	return s.runAdmission(ctx, req, ControlOperationContinue)
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

	err = strategy.Admit(ctx, AdmissionRequest{
		Rollout:        *result.Rollout,
		Batch:          *result.Batch,
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
				"rollout admission failed and could not record its cause: %v; record error: %w",
				err,
				finishErr,
			)
		}
		return nil, fleeterror.NewFailedPreconditionErrorf("rollout admission failed: %w", err)
	}
	if _, err := s.store.FinishControl(ctx, FinishControlRequest{
		OrgID:     req.OrgID,
		RolloutID: req.RolloutID,
		ControlID: result.Control.ID,
		Success:   true,
	}); err != nil {
		return nil, mapStoreError(err)
	}
	return result.Rollout, nil
}

func (s *Service) Pause(ctx context.Context, req ControlRequest) (*Rollout, error) {
	return s.applySimpleControl(ctx, req, ControlOperationPause)
}

func (s *Service) Resume(ctx context.Context, req ControlRequest) (*Rollout, error) {
	return s.applySimpleControl(ctx, req, ControlOperationResume)
}

func (s *Service) Abort(ctx context.Context, req ControlRequest) (*Rollout, error) {
	return s.applySimpleControl(ctx, req, ControlOperationAbort)
}

func (s *Service) Complete(ctx context.Context, req ControlRequest) (*Rollout, error) {
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
	if validator, validatesRevert := strategy.(RevertStrategy); validatesRevert {
		if err := validator.ValidateRevert(ctx, RevertValidationRequest{
			Rollout: *current,
		}); err != nil {
			return nil, fleeterror.NewFailedPreconditionErrorf(
				"rollout cannot revert: %w",
				err,
			)
		}
	}

	req.Operation = ControlOperationRevert
	req.RequestFingerprint = fingerprintControl(req)
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

func fingerprintCreate(req CreateRequest) string {
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
		Batches:            req.Batches,
		Reason:             req.Reason,
		ActorUserID:        req.ActorUserID,
	}
	encoded, _ := json.Marshal(fingerprintInput)
	return cryptohash.Sha256Hex(string(encoded))
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
	case errors.Is(err, ErrRevisionConflict), errors.Is(err, ErrInvalidTransition):
		return fleeterror.NewFailedPreconditionErrorf("%w", err)
	case errors.Is(err, ErrIdempotencyConflict), errors.Is(err, ErrOwnershipConflict):
		return fleeterror.NewAlreadyExistsErrorf("%w", err)
	default:
		return err
	}
}
