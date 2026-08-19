package betweenchannel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/infrastructure/cryptohash"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Service struct {
	store    LaneStore
	resolver ReleaseTargetResolver
}

type ReleaseTargetResolver interface {
	ResolveReleaseTargets(
		ctx context.Context,
		firmwareFileIDs []string,
	) ([]ReleaseTarget, func(), error)
}

type ReleaseTargetResolverFunc func(
	ctx context.Context,
	firmwareFileIDs []string,
) ([]ReleaseTarget, func(), error)

func (f ReleaseTargetResolverFunc) ResolveReleaseTargets(
	ctx context.Context,
	firmwareFileIDs []string,
) ([]ReleaseTarget, func(), error) {
	return f(ctx, firmwareFileIDs)
}

func NewService(store LaneStore, resolver ReleaseTargetResolver) *Service {
	return &Service{
		store:    store,
		resolver: resolver,
	}
}

func (s *Service) PreviewLane(
	ctx context.Context,
	req PreviewLaneRequest,
) (InitialEnforcementPreview, error) {
	if err := validatePreviewLaneRequest(req); err != nil {
		return InitialEnforcementPreview{}, err
	}
	targets, release, err := s.resolveTargets(ctx, req.FirmwareFileIDs, req.ReleaseTargets)
	if err != nil {
		return InitialEnforcementPreview{}, err
	}
	defer release()
	req.ReleaseTargets = targets
	preview, err := s.store.PreviewLane(ctx, req)
	if err != nil {
		return InitialEnforcementPreview{}, mapStoreError(err)
	}
	preview.Targets = sortedTargets(targets)
	return preview, nil
}

func (s *Service) CreateLane(
	ctx context.Context,
	req CreateLaneRequest,
) (*Lane, error) {
	if err := validateCreateLaneRequest(req); err != nil {
		return nil, err
	}
	targets, release, err := s.resolveTargets(ctx, req.FirmwareFileIDs, req.ReleaseTargets)
	if err != nil {
		return nil, err
	}
	defer release()
	req.ReleaseTargets = targets
	if req.ID == uuid.Nil {
		req.ID = uuid.New()
	}
	req.RequestFingerprint, err = fingerprintLaneCreate(req)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf(
			"rollout lane request cannot be fingerprinted: %v",
			err,
		)
	}
	lane, err := s.store.CreateLane(ctx, req)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return lane, nil
}

func (s *Service) GetLane(
	ctx context.Context,
	orgID int64,
	laneID uuid.UUID,
	includeFirmwareConvergenceMembers bool,
	firmwareConvergenceMembersUpdatedAfter *time.Time,
) (*Lane, error) {
	if orgID <= 0 || laneID == uuid.Nil {
		return nil, fleeterror.NewInvalidArgumentError(
			"organization and rollout lane IDs are required",
		)
	}
	lane, err := s.store.GetLane(
		ctx,
		orgID,
		laneID,
		includeFirmwareConvergenceMembers,
		firmwareConvergenceMembersUpdatedAfter,
	)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return lane, nil
}

func (s *Service) ListLanes(
	ctx context.Context,
	orgID int64,
	activeFirmwareConvergenceOnly bool,
) ([]Lane, error) {
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("organization ID is required")
	}
	lanes, err := s.store.ListLanes(ctx, orgID, activeFirmwareConvergenceOnly)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return lanes, nil
}

func (s *Service) ListMembers(
	ctx context.Context,
	req ListMembersRequest,
) (ListMembersResult, error) {
	if req.OrgID <= 0 || req.LaneID == uuid.Nil {
		return ListMembersResult{}, fleeterror.NewInvalidArgumentError(
			"organization and rollout lane IDs are required",
		)
	}
	if req.Limit == 0 {
		req.Limit = 100
	}
	if req.Limit < 1 || req.Limit > 1000 {
		return ListMembersResult{}, fleeterror.NewInvalidArgumentError(
			"page size must be between 1 and 1000",
		)
	}
	result, err := s.store.ListMembers(ctx, req)
	if err != nil {
		return ListMembersResult{}, mapStoreError(err)
	}
	return result, nil
}

func (s *Service) PreviewMembershipChange(
	ctx context.Context,
	req PreviewMembershipChangeRequest,
) (MembershipChangePreview, error) {
	if err := validateMembershipOperations(
		req.OrgID,
		req.LaneID,
		req.AddIdentifiers,
		req.RemoveIdentifiers,
	); err != nil {
		return MembershipChangePreview{}, err
	}
	req.AddIdentifiers = sortedStrings(req.AddIdentifiers)
	req.RemoveIdentifiers = sortedStrings(req.RemoveIdentifiers)
	result, err := s.store.PreviewMembershipChange(ctx, req)
	if err != nil {
		return MembershipChangePreview{}, mapStoreError(err)
	}
	return result, nil
}

func (s *Service) UpdateMembership(
	ctx context.Context,
	req UpdateMembershipRequest,
) (UpdateMembershipResult, error) {
	if err := validateMembershipOperations(
		req.OrgID,
		req.LaneID,
		req.AddIdentifiers,
		req.RemoveIdentifiers,
	); err != nil {
		return UpdateMembershipResult{}, err
	}
	if req.ExpectedRevision <= 0 || req.ActorUserID <= 0 {
		return UpdateMembershipResult{}, fleeterror.NewInvalidArgumentError(
			"expected revision and actor are required",
		)
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || strings.TrimSpace(req.Reason) == "" {
		return UpdateMembershipResult{}, fleeterror.NewInvalidArgumentError(
			"idempotency key and reason are required",
		)
	}
	if err := validateLaneMutationActor(req.ActorType, req.ActorCredentialID); err != nil {
		return UpdateMembershipResult{}, err
	}
	req.AddIdentifiers = sortedStrings(req.AddIdentifiers)
	req.RemoveIdentifiers = sortedStrings(req.RemoveIdentifiers)
	if req.ChangeID == uuid.Nil {
		req.ChangeID = uuid.New()
	}
	var err error
	req.RequestFingerprint, err = fingerprintMembershipUpdate(req)
	if err != nil {
		return UpdateMembershipResult{}, fleeterror.NewInvalidArgumentErrorf(
			"rollout lane membership request cannot be fingerprinted: %v",
			err,
		)
	}
	result, err := s.store.UpdateMembership(ctx, req)
	if err != nil {
		return UpdateMembershipResult{}, mapStoreError(err)
	}
	return result, nil
}

func (s *Service) DeleteLane(ctx context.Context, req DeleteLaneRequest) error {
	if err := validateDeleteLaneRequest(req); err != nil {
		return err
	}
	req.RequestFingerprint = fingerprintLaneDelete(req)
	if err := s.store.DeleteLane(ctx, req); err != nil {
		return mapStoreError(err)
	}
	return nil
}

func validateMembershipOperations(
	orgID int64,
	laneID uuid.UUID,
	addIdentifiers []string,
	removeIdentifiers []string,
) error {
	if orgID <= 0 || laneID == uuid.Nil {
		return fleeterror.NewInvalidArgumentError(
			"organization and rollout lane IDs are required",
		)
	}
	if len(addIdentifiers) == 0 && len(removeIdentifiers) == 0 {
		return fleeterror.NewInvalidArgumentError(
			"at least one add or remove device identifier is required",
		)
	}
	if len(addIdentifiers) > 0 {
		if err := validateIdentifiers(addIdentifiers); err != nil {
			return err
		}
	}
	if len(removeIdentifiers) > 0 {
		if err := validateIdentifiers(removeIdentifiers); err != nil {
			return err
		}
	}
	added := make(map[string]struct{}, len(addIdentifiers))
	for _, identifier := range addIdentifiers {
		added[strings.TrimSpace(identifier)] = struct{}{}
	}
	for _, identifier := range removeIdentifiers {
		if _, ok := added[strings.TrimSpace(identifier)]; ok {
			return fleeterror.NewInvalidArgumentErrorf(
				"device identifier %q cannot be added and removed in the same request",
				identifier,
			)
		}
	}
	return nil
}

func (s *Service) StartRollout(
	ctx context.Context,
	req StartRolloutRequest,
) (StartRolloutResult, error) {
	if err := validateStartRolloutRequest(req); err != nil {
		return StartRolloutResult{}, err
	}
	targets, release, err := s.resolveTargets(ctx, req.FirmwareFileIDs, req.ReleaseTargets)
	if err != nil {
		return StartRolloutResult{}, err
	}
	defer release()
	req.ReleaseTargets = targets
	if req.ID == uuid.Nil {
		req.ID = uuid.New()
	}
	req.RequestFingerprint, err = fingerprintLaneStart(req)
	if err != nil {
		return StartRolloutResult{}, fleeterror.NewInvalidArgumentErrorf(
			"rollout lane start request cannot be fingerprinted: %v",
			err,
		)
	}
	result, err := s.store.StartRollout(ctx, req)
	if err != nil {
		return StartRolloutResult{}, mapStoreError(err)
	}
	return result, nil
}

func validateCreateLaneRequest(req CreateLaneRequest) error {
	if req.OrgID <= 0 || req.ActorUserID <= 0 || strings.TrimSpace(req.Label) == "" {
		return fleeterror.NewInvalidArgumentError(
			"organization, actor, and rollout lane label are required",
		)
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return fleeterror.NewInvalidArgumentError("idempotency key is required")
	}
	if err := validateReleaseInput(req.FirmwareFileIDs, req.ReleaseTargets); err != nil {
		return err
	}
	if err := validateIdentifiers(req.DeviceIdentifiers); err != nil {
		return err
	}
	return nil
}

func validatePreviewLaneRequest(req PreviewLaneRequest) error {
	if req.OrgID <= 0 {
		return fleeterror.NewInvalidArgumentError("organization ID is required")
	}
	if err := validateReleaseInput(req.FirmwareFileIDs, req.ReleaseTargets); err != nil {
		return err
	}
	return validateIdentifiers(req.DeviceIdentifiers)
}

func validateDeleteLaneRequest(req DeleteLaneRequest) error {
	if req.OrgID <= 0 || req.LaneID == uuid.Nil || req.ActorUserID <= 0 || req.ExpectedRevision <= 0 {
		return fleeterror.NewInvalidArgumentError(
			"organization, rollout lane, actor, and expected revision are required",
		)
	}
	if err := validateLaneMutationActor(req.ActorType, req.ActorCredentialID); err != nil {
		return err
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || strings.TrimSpace(req.Reason) == "" {
		return fleeterror.NewInvalidArgumentError("idempotency key and reason are required")
	}
	return nil
}

func validateLaneMutationActor(actorType rollout.ActorType, credentialID *string) error {
	if err := rollout.ValidateActorIdentity(actorType, credentialID); err != nil {
		return err
	}
	if actorType == rollout.ActorTypeAPIKey && credentialID == nil {
		return fleeterror.NewInvalidArgumentError(
			"API key actor credential ID is required",
		)
	}
	if actorType == rollout.ActorTypeSystem && credentialID != nil {
		return fleeterror.NewInvalidArgumentError(
			"system actor credential ID must be omitted",
		)
	}
	return nil
}

func validateStartRolloutRequest(req StartRolloutRequest) error {
	if req.OrgID <= 0 || req.ActorUserID <= 0 || req.LaneID == uuid.Nil ||
		strings.TrimSpace(req.Name) == "" {
		return fleeterror.NewInvalidArgumentError(
			"organization, actor, lane, and rollout name are required",
		)
	}
	if err := rollout.ValidateActorIdentity(req.ActorType, req.ActorCredentialID); err != nil {
		return err
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || strings.TrimSpace(req.Reason) == "" {
		return fleeterror.NewInvalidArgumentError("idempotency key and reason are required")
	}
	if err := validateReleaseInput(req.FirmwareFileIDs, req.ReleaseTargets); err != nil {
		return err
	}
	if len(req.Batches) == 0 {
		return fleeterror.NewInvalidArgumentError("at least one rollout batch is required")
	}
	identifiers := make([]string, 0)
	for _, batch := range req.Batches {
		if len(batch.Members) == 0 {
			return fleeterror.NewInvalidArgumentError("rollout batches cannot be empty")
		}
		for _, member := range batch.Members {
			identifiers = append(identifiers, member.DeviceIdentifier)
		}
	}
	return validateIdentifiers(identifiers)
}

func validateReleaseInput(
	firmwareFileIDs []string,
	targets []ReleaseTarget,
) error {
	switch {
	case len(firmwareFileIDs) > 0 && len(targets) > 0:
		return fleeterror.NewInvalidArgumentError(
			"firmware file IDs and resolved release targets are mutually exclusive",
		)
	case len(firmwareFileIDs) > 0:
		return validateIdentifiers(firmwareFileIDs)
	default:
		return validateReleaseTargets(targets)
	}
}

func (s *Service) resolveTargets(
	ctx context.Context,
	firmwareFileIDs []string,
	targets []ReleaseTarget,
) ([]ReleaseTarget, func(), error) {
	if len(firmwareFileIDs) == 0 {
		return targets, func() {}, nil
	}
	if s.resolver == nil {
		return nil, func() {}, fleeterror.NewFailedPreconditionError(
			"firmware release resolver is not registered",
		)
	}
	resolved, release, err := s.resolver.ResolveReleaseTargets(ctx, firmwareFileIDs)
	if release == nil {
		release = func() {}
	}
	if err != nil {
		release()
		return nil, func() {}, err
	}
	if err := validateReleaseTargets(resolved); err != nil {
		release()
		return nil, func() {}, err
	}
	return resolved, release, nil
}

func validateIdentifiers(identifiers []string) error {
	if len(identifiers) == 0 {
		return fleeterror.NewInvalidArgumentError("device identifiers are required")
	}
	seen := make(map[string]struct{}, len(identifiers))
	for _, value := range identifiers {
		identifier := strings.TrimSpace(value)
		if identifier == "" {
			return fleeterror.NewInvalidArgumentError("device identifiers cannot be empty")
		}
		if _, ok := seen[identifier]; ok {
			return fleeterror.NewInvalidArgumentErrorf(
				"duplicate device identifier %q",
				identifier,
			)
		}
		seen[identifier] = struct{}{}
	}
	return nil
}

func validateReleaseTargets(targets []ReleaseTarget) error {
	if len(targets) == 0 {
		return fleeterror.NewInvalidArgumentError("at least one release target is required")
	}
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if strings.TrimSpace(target.FirmwareFileID) == "" ||
			strings.TrimSpace(target.Manufacturer) == "" ||
			strings.TrimSpace(target.Model) == "" ||
			strings.TrimSpace(target.FirmwareVersion) == "" ||
			!sha256Pattern.MatchString(target.SHA256) {
			return fleeterror.NewInvalidArgumentError(
				"release targets require file, manufacturer, model, version, and lowercase SHA-256",
			)
		}
		key := ModelKey(target.Manufacturer, target.Model)
		if _, ok := seen[key]; ok {
			return fleeterror.NewInvalidArgumentErrorf(
				"duplicate release target for %s %s",
				target.Manufacturer,
				target.Model,
			)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateTransitionTargets(
	source []DeviceTransition,
	targets []ReleaseTarget,
) error {
	if err := validateReleaseTargets(targets); err != nil {
		return err
	}
	targetByModel := make(map[string]ReleaseTarget, len(targets))
	for _, target := range targets {
		targetByModel[ModelKey(target.Manufacturer, target.Model)] = target
	}
	for _, transition := range source {
		if strings.TrimSpace(transition.Manufacturer) == "" ||
			strings.TrimSpace(transition.Model) == "" ||
			transition.SourceReleaseTargetID <= 0 {
			return fmt.Errorf(
				"%w: source release is missing model support for device %s",
				ErrCompatibility,
				transition.DeviceIdentifier,
			)
		}
		target, ok := targetByModel[ModelKey(transition.Manufacturer, transition.Model)]
		if !ok {
			return fmt.Errorf(
				"%w: target release is missing %s %s",
				ErrCompatibility,
				transition.Manufacturer,
				transition.Model,
			)
		}
		if target.FirmwareVersion == transition.SourceFirmwareVersion ||
			target.SHA256 == transition.SourceSHA256 {
			return fmt.Errorf(
				"%w: %s %s already targets source release",
				ErrCompatibility,
				transition.Manufacturer,
				transition.Model,
			)
		}
	}
	return nil
}

func ValidateTransitionTargetsForStore(
	source []DeviceTransition,
	targets []ReleaseTarget,
) error {
	return validateTransitionTargets(source, targets)
}

func ModelKey(manufacturer, model string) string {
	return strings.ToLower(strings.TrimSpace(manufacturer)) +
		"\x00" +
		strings.ToLower(strings.TrimSpace(model))
}

func fingerprintLaneCreate(req CreateLaneRequest) (string, error) {
	// Confirmation acknowledges the initial updates but does not change the
	// desired lane, so confirmed retries share the original idempotency identity.
	payload := struct {
		Label             string
		Description       string
		FirmwareFileIDs   []string
		ReleaseTargets    []ReleaseTarget
		DeviceIdentifiers []string
		ActorUserID       int64
	}{
		Label:             req.Label,
		Description:       req.Description,
		FirmwareFileIDs:   sortedStrings(req.FirmwareFileIDs),
		ReleaseTargets:    sortedTargets(req.ReleaseTargets),
		DeviceIdentifiers: sortedStrings(req.DeviceIdentifiers),
		ActorUserID:       req.ActorUserID,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal lane create fingerprint: %w", err)
	}
	return cryptohash.Sha256Hex(string(encoded)), nil
}

func fingerprintLaneStart(req StartRolloutRequest) (string, error) {
	payload := struct {
		LaneID          uuid.UUID
		Name            string
		FirmwareFileIDs []string
		ReleaseTargets  []ReleaseTarget
		Batches         []rollout.CreateBatch
		Reason          string
		ActorUserID     int64
	}{
		LaneID:          req.LaneID,
		Name:            req.Name,
		FirmwareFileIDs: sortedStrings(req.FirmwareFileIDs),
		ReleaseTargets:  sortedTargets(req.ReleaseTargets),
		Batches:         req.Batches,
		Reason:          req.Reason,
		ActorUserID:     req.ActorUserID,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal lane start fingerprint: %w", err)
	}
	return cryptohash.Sha256Hex(string(encoded)), nil
}

func fingerprintLaneDelete(req DeleteLaneRequest) string {
	actorType := req.ActorType
	if actorType == "" {
		actorType = rollout.ActorTypeUser
	}
	actorCredentialID := ""
	if req.ActorCredentialID != nil {
		actorCredentialID = *req.ActorCredentialID
	}
	payload := fmt.Sprintf(
		"%s\n%d\n%s\n%d\n%s\n%s",
		req.LaneID,
		req.ExpectedRevision,
		req.Reason,
		req.ActorUserID,
		actorType,
		actorCredentialID,
	)
	return cryptohash.Sha256Hex(payload)
}

func fingerprintMembershipUpdate(req UpdateMembershipRequest) (string, error) {
	actorType := req.ActorType
	if actorType == "" {
		actorType = rollout.ActorTypeUser
	}
	actorCredentialID := ""
	if req.ActorCredentialID != nil {
		actorCredentialID = *req.ActorCredentialID
	}
	payload := struct {
		LaneID            uuid.UUID
		ExpectedRevision  int64
		AddIdentifiers    []string
		RemoveIdentifiers []string
		ConfirmFirmware   bool
		ConfirmReassign   bool
		Reason            string
		ActorUserID       int64
		ActorType         rollout.ActorType
		ActorCredentialID string
	}{
		LaneID:            req.LaneID,
		ExpectedRevision:  req.ExpectedRevision,
		AddIdentifiers:    sortedStrings(req.AddIdentifiers),
		RemoveIdentifiers: sortedStrings(req.RemoveIdentifiers),
		ConfirmFirmware:   req.ConfirmFirmware,
		ConfirmReassign:   req.ConfirmReassign,
		Reason:            req.Reason,
		ActorUserID:       req.ActorUserID,
		ActorType:         actorType,
		ActorCredentialID: actorCredentialID,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal membership update fingerprint: %w", err)
	}
	return cryptohash.Sha256Hex(string(encoded)), nil
}

func sortedTargets(values []ReleaseTarget) []ReleaseTarget {
	result := append([]ReleaseTarget(nil), values...)
	sort.Slice(result, func(i, j int) bool {
		return ModelKey(result[i].Manufacturer, result[i].Model) <
			ModelKey(result[j].Manufacturer, result[j].Model)
	})
	return result
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func mapStoreError(err error) error {
	switch {
	case errors.Is(err, ErrLaneNotFound):
		return fleeterror.NewNotFoundErrorf("%w", err)
	case errors.Is(err, ErrLaneConflict),
		errors.Is(err, ErrMembershipConflict),
		errors.Is(err, ErrCompatibility),
		errors.Is(err, ErrInitialEnforcementConfirmationRequired),
		errors.Is(err, ErrFirmwareConfirmationRequired),
		errors.Is(err, ErrReassignmentConfirmationRequired),
		errors.Is(err, ErrFirmwareConvergenceActive),
		errors.Is(err, ErrLaneWorkActive):
		return fleeterror.NewFailedPreconditionErrorf("%w", err)
	case errors.Is(err, ErrIdempotencyConflict):
		return fleeterror.NewAlreadyExistsErrorf("%w", err)
	default:
		return err
	}
}
