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

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/infrastructure/cryptohash"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type NoopActivityLogger = rollout.NoopActivityLogger

type Service struct {
	store    LaneStore
	resolver ReleaseTargetResolver
	activity rollout.ActivityLogger
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

func NewService(
	store LaneStore,
	resolver ReleaseTargetResolver,
) *Service {
	return NewServiceWithActivity(store, resolver, NoopActivityLogger{})
}

func NewServiceWithActivity(
	store LaneStore,
	resolver ReleaseTargetResolver,
	activity rollout.ActivityLogger,
) *Service {
	if activity == nil {
		panic("between-channel rollout service: activity logger is required")
	}
	return &Service{
		store:    store,
		resolver: resolver,
		activity: activity,
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
	preview.ReassignmentConfirmationToken, err = ReassignmentConfirmationToken(req, preview)
	if err != nil {
		return InitialEnforcementPreview{}, fleeterror.NewInvalidArgumentErrorf(
			"rollout lane reassignment confirmation cannot be generated: %v",
			err,
		)
	}
	return preview, nil
}

func (s *Service) CreateLane(
	ctx context.Context,
	req CreateLaneRequest,
) (*Lane, error) {
	if req.ActorType == "" {
		req.ActorType = rollout.ActorTypeUser
	}
	if err := validateCreateLaneRequest(req); err != nil {
		return nil, err
	}
	targets, release, err := s.resolveTargets(ctx, req.FirmwareFileIDs, req.ReleaseTargets)
	if err != nil {
		return nil, err
	}
	defer release()
	req.ReleaseTargets = targets
	req.DeviceIdentifiers = sortedStrings(req.DeviceIdentifiers)
	if req.ID == uuid.Nil {
		req.ID = uuid.New()
	}
	if req.ChangeID == uuid.Nil {
		req.ChangeID = uuid.New()
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

func (s *Service) GetLaneForRollout(
	ctx context.Context,
	orgID int64,
	rolloutID uuid.UUID,
) (*Lane, error) {
	if orgID <= 0 || rolloutID == uuid.Nil {
		return nil, fleeterror.NewInvalidArgumentError(
			"organization and rollout IDs are required",
		)
	}
	lane, err := s.store.GetLaneForRollout(ctx, orgID, rolloutID)
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

func (s *Service) GetAssignments(
	ctx context.Context,
	orgID int64,
	deviceIdentifiers []string,
) ([]LaneAssignment, error) {
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("organization ID is required")
	}
	if len(deviceIdentifiers) > 50 {
		return nil, fleeterror.NewInvalidArgumentError("at most 50 device identifiers are allowed")
	}
	if err := validateIdentifiers(deviceIdentifiers); err != nil {
		return nil, err
	}
	assignments, err := s.store.GetAssignments(ctx, orgID, sortedStrings(deviceIdentifiers))
	if err != nil {
		return nil, mapStoreError(err)
	}
	return assignments, nil
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

func (s *Service) CreateModelDeclaration(
	ctx context.Context,
	req CreateModelDeclarationRequest,
) (*Lane, error) {
	if req.ActorType == "" {
		req.ActorType = rollout.ActorTypeUser
	}
	if req.OrgID <= 0 || req.LaneID == uuid.Nil || req.ExpectedRevision != 0 ||
		req.ActorUserID <= 0 || strings.TrimSpace(req.IdempotencyKey) == "" ||
		strings.TrimSpace(req.Reason) == "" {
		return nil, fleeterror.NewInvalidArgumentError(
			"organization, lane, zero declaration revision, idempotency key, reason, and actor are required",
		)
	}
	if err := validateLaneMutationActor(req.ActorType, req.ActorCredentialID); err != nil {
		return nil, err
	}
	targets, release, err := s.resolveTargets(ctx, req.FirmwareFileIDs, req.ReleaseTargets)
	if err != nil {
		return nil, err
	}
	defer release()
	if len(targets) != 1 {
		return nil, fleeterror.NewInvalidArgumentError(
			"exactly one firmware target is required for a model declaration",
		)
	}
	if len(req.DeviceIdentifiers) > 0 {
		if err = validateIdentifiers(req.DeviceIdentifiers); err != nil {
			return nil, err
		}
	}
	req.ReleaseTargets = targets
	req.DeviceIdentifiers = sortedStrings(req.DeviceIdentifiers)
	if req.OperationID == uuid.Nil {
		req.OperationID = uuid.New()
	}
	if req.LaneModelID == uuid.Nil {
		req.LaneModelID = uuid.New()
	}
	req.RequestFingerprint, err = fingerprintModelMutation(modelMutationFingerprintRequest{
		Operation: modelMutationCreateDeclaration,
		LaneID:    req.LaneID,
		Selector: ModelDeclarationSelector{ModelIdentityKey: CanonicalModelIdentityKey(
			targets[0].Manufacturer,
			targets[0].Model,
		)},
		ExpectedRevision:  req.ExpectedRevision,
		ReleaseTargets:    targets,
		AddIdentifiers:    req.DeviceIdentifiers,
		ConfirmFirmware:   req.ConfirmInitialEnforcement,
		ConfirmReassign:   req.ConfirmReassignment,
		ReassignmentToken: req.ReassignmentConfirmationToken,
		Reason:            req.Reason,
		ActorUserID:       req.ActorUserID,
		ActorType:         req.ActorType,
		ActorCredentialID: req.ActorCredentialID,
	})
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf(
			"rollout lane model declaration request cannot be fingerprinted: %v",
			err,
		)
	}
	lane, err := s.store.CreateModelDeclaration(ctx, req)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return lane, nil
}

func (s *Service) PublishModelTarget(
	ctx context.Context,
	req PublishModelTargetRequest,
) (*Lane, error) {
	if req.ActorType == "" {
		req.ActorType = rollout.ActorTypeUser
	}
	if req.OrgID <= 0 || req.LaneID == uuid.Nil || !req.Selector().IsValid() ||
		req.ExpectedRevision <= 0 || req.ActorUserID <= 0 ||
		strings.TrimSpace(req.IdempotencyKey) == "" || strings.TrimSpace(req.Reason) == "" {
		return nil, fleeterror.NewInvalidArgumentError(
			"organization, lane, declaration, revision, idempotency key, reason, and actor are required",
		)
	}
	if err := validateLaneMutationActor(req.ActorType, req.ActorCredentialID); err != nil {
		return nil, err
	}
	targets, release, err := s.resolveTargets(ctx, req.FirmwareFileIDs, req.ReleaseTargets)
	if err != nil {
		return nil, err
	}
	defer release()
	if len(targets) != 1 {
		return nil, fleeterror.NewInvalidArgumentError(
			"exactly one firmware target is required for publication",
		)
	}
	req.ReleaseTargets = targets
	if req.OperationID == uuid.Nil {
		req.OperationID = uuid.New()
	}
	req.RequestFingerprint, err = fingerprintModelMutation(modelMutationFingerprintRequest{
		Operation:         modelMutationPublishTarget,
		LaneID:            req.LaneID,
		Selector:          req.Selector(),
		ExpectedRevision:  req.ExpectedRevision,
		ReleaseTargets:    targets,
		Reason:            req.Reason,
		ActorUserID:       req.ActorUserID,
		ActorType:         req.ActorType,
		ActorCredentialID: req.ActorCredentialID,
	})
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf(
			"rollout lane model publication request cannot be fingerprinted: %v",
			err,
		)
	}
	lane, err := s.store.PublishModelTarget(ctx, req)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return lane, nil
}

func (s *Service) PreviewModelMembershipChange(
	ctx context.Context,
	req PreviewModelMembershipChangeRequest,
) (MembershipChangePreview, error) {
	if !req.Selector().IsValid() {
		return MembershipChangePreview{}, fleeterror.NewInvalidArgumentError(
			"exactly one rollout lane model selector is required",
		)
	}
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
	result, err := s.store.PreviewModelMembershipChange(ctx, req)
	if err != nil {
		return MembershipChangePreview{}, mapStoreError(err)
	}
	return result, nil
}

func (s *Service) UpdateModelMembership(
	ctx context.Context,
	req UpdateModelMembershipRequest,
) (UpdateMembershipResult, error) {
	if req.ActorType == "" {
		req.ActorType = rollout.ActorTypeUser
	}
	if !req.Selector().IsValid() || req.ExpectedRevision <= 0 || req.ActorUserID <= 0 ||
		strings.TrimSpace(req.IdempotencyKey) == "" || strings.TrimSpace(req.Reason) == "" {
		return UpdateMembershipResult{}, fleeterror.NewInvalidArgumentError(
			"declaration, revision, idempotency key, reason, and actor are required",
		)
	}
	if err := validateMembershipOperations(
		req.OrgID,
		req.LaneID,
		req.AddIdentifiers,
		req.RemoveIdentifiers,
	); err != nil {
		return UpdateMembershipResult{}, err
	}
	if err := validateLaneMutationActor(req.ActorType, req.ActorCredentialID); err != nil {
		return UpdateMembershipResult{}, err
	}
	req.AddIdentifiers = sortedStrings(req.AddIdentifiers)
	req.RemoveIdentifiers = sortedStrings(req.RemoveIdentifiers)
	if req.OperationID == uuid.Nil {
		req.OperationID = uuid.New()
	}
	var err error
	req.RequestFingerprint, err = fingerprintModelMutation(modelMutationFingerprintRequest{
		Operation:         modelMutationUpdateMembership,
		LaneID:            req.LaneID,
		Selector:          req.Selector(),
		ExpectedRevision:  req.ExpectedRevision,
		AddIdentifiers:    req.AddIdentifiers,
		RemoveIdentifiers: req.RemoveIdentifiers,
		ConfirmFirmware:   req.ConfirmFirmware,
		ConfirmReassign:   req.ConfirmReassign,
		Reason:            req.Reason,
		ActorUserID:       req.ActorUserID,
		ActorType:         req.ActorType,
		ActorCredentialID: req.ActorCredentialID,
	})
	if err != nil {
		return UpdateMembershipResult{}, fleeterror.NewInvalidArgumentErrorf(
			"rollout lane model membership request cannot be fingerprinted: %v",
			err,
		)
	}
	result, err := s.store.UpdateModelMembership(ctx, req)
	if err != nil {
		return UpdateMembershipResult{}, mapStoreError(err)
	}
	return result, nil
}

func (s *Service) GetTopologyReadiness(
	ctx context.Context,
	orgID int64,
) (TopologyReadiness, error) {
	if orgID <= 0 {
		return TopologyReadiness{}, fleeterror.NewInvalidArgumentError(
			"organization ID is required",
		)
	}
	readiness, err := s.store.GetTopologyReadiness(ctx, orgID)
	if err != nil {
		return TopologyReadiness{}, mapStoreError(err)
	}
	return readiness, nil
}

func (s *Service) RepairModelBinding(
	ctx context.Context,
	req RepairModelBindingRequest,
) (RepairModelBindingResult, error) {
	if req.ActorType == "" {
		req.ActorType = rollout.ActorTypeUser
	}
	if req.OrgID <= 0 ||
		req.LaneID == uuid.Nil ||
		req.LaneModelID == uuid.Nil ||
		req.ExpectedRevision <= 0 ||
		req.ActorUserID <= 0 ||
		strings.TrimSpace(req.DeviceIdentifier) == "" ||
		strings.TrimSpace(req.IdempotencyKey) == "" ||
		strings.TrimSpace(req.Reason) == "" {
		return RepairModelBindingResult{}, fleeterror.NewInvalidArgumentError(
			"organization, lane, declaration, device, revision, idempotency key, reason, and actor are required",
		)
	}
	if err := validateLaneMutationActor(req.ActorType, req.ActorCredentialID); err != nil {
		return RepairModelBindingResult{}, err
	}
	if req.OperationID == uuid.Nil {
		req.OperationID = uuid.New()
	}
	if req.BindingID == uuid.Nil {
		req.BindingID = uuid.New()
	}
	req.DeviceIdentifier = strings.TrimSpace(req.DeviceIdentifier)
	req.RequestFingerprint = fingerprintTopologyRepair(req)
	result, err := s.store.RepairModelBinding(ctx, req)
	if err != nil {
		return RepairModelBindingResult{}, mapStoreError(err)
	}
	return result, nil
}

func (s *Service) EnableTopology(
	ctx context.Context,
	req EnableTopologyRequest,
) (EnableTopologyResult, error) {
	if req.ActorType == "" {
		req.ActorType = rollout.ActorTypeUser
	}
	if req.OrgID <= 0 ||
		req.ExpectedRevision <= 0 ||
		req.ActorUserID <= 0 ||
		strings.TrimSpace(req.IdempotencyKey) == "" ||
		strings.TrimSpace(req.Reason) == "" {
		return EnableTopologyResult{}, fleeterror.NewInvalidArgumentError(
			"organization, revision, idempotency key, reason, and actor are required",
		)
	}
	if err := validateLaneMutationActor(req.ActorType, req.ActorCredentialID); err != nil {
		return EnableTopologyResult{}, err
	}
	if req.OperationID == uuid.Nil {
		req.OperationID = uuid.New()
	}
	req.RequestFingerprint = fingerprintTopologyEnable(req)
	result, err := s.store.EnableTopology(ctx, req)
	if err != nil {
		return EnableTopologyResult{}, mapStoreError(err)
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
	releases := make([]func(), 0, len(req.ModelPlans)+1)
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	var err error
	if len(req.ModelPlans) > 0 {
		for index := range req.ModelPlans {
			if req.ModelPlans[index].ReleaseTarget.FirmwareFileID != "" {
				if validateErr := validateReleaseTargets([]ReleaseTarget{
					req.ModelPlans[index].ReleaseTarget,
				}); validateErr != nil {
					return StartRolloutResult{}, validateErr
				}
				continue
			}
			targets, release, resolveErr := s.resolveTargets(
				ctx,
				[]string{req.ModelPlans[index].FirmwareFileID},
				nil,
			)
			releases = append(releases, release)
			if resolveErr != nil {
				return StartRolloutResult{}, resolveErr
			}
			if len(targets) != 1 {
				return StartRolloutResult{}, fleeterror.NewFailedPreconditionError(
					"selected model firmware must resolve to exactly one release target",
				)
			}
			req.ModelPlans[index].ReleaseTarget = targets[0]
		}
		SortStartRolloutModelPlans(req.ModelPlans)
	} else {
		var targets []ReleaseTarget
		var release func()
		targets, release, err = s.resolveTargets(ctx, req.FirmwareFileIDs, req.ReleaseTargets)
		releases = append(releases, release)
		if err != nil {
			return StartRolloutResult{}, err
		}
		req.ReleaseTargets = targets
	}
	if req.ParentID == uuid.Nil && len(req.ModelPlans) > 0 {
		req.ParentID = uuid.New()
	}
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
	s.projectStartActivity(ctx, req, result)
	return result, nil
}

func (s *Service) projectStartActivity(
	ctx context.Context,
	req StartRolloutRequest,
	result StartRolloutResult,
) {
	if result.Parent == nil {
		return
	}
	orgID := req.OrgID
	scopeType := "rollout_lane"
	scopeLabel := req.LaneID.String()
	s.activity.Log(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           "rollout_group.started",
		Description:    "Started aggregate rollout",
		Result:         activitymodels.ResultSuccess,
		ScopeType:      &scopeType,
		ScopeLabel:     &scopeLabel,
		ActorType:      rollout.ActivityActorType(req.ActorType),
		OrganizationID: &orgID,
		Metadata: map[string]any{
			"parent_id":   result.Parent.ID.String(),
			"lane_id":     req.LaneID.String(),
			"model_count": len(result.Children),
		},
		IdempotencyKey: "rollout-group-start:" + result.Parent.ID.String(),
	})
	for _, started := range result.Children {
		if started.Child == nil {
			continue
		}
		child := started.Child
		s.activity.Log(ctx, activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           "rollout_child.started",
			Description:    "Created model rollout",
			Result:         activitymodels.ResultSuccess,
			ScopeType:      &scopeType,
			ScopeLabel:     &scopeLabel,
			ActorType:      rollout.ActivityActorType(req.ActorType),
			OrganizationID: &orgID,
			Metadata: map[string]any{
				"parent_id":          result.Parent.ID.String(),
				"child_id":           child.ID.String(),
				"model_identity_key": child.ModelIdentityKey,
				"manufacturer":       child.Manufacturer,
				"model":              child.Model,
				"member_count":       len(child.Members),
			},
			IdempotencyKey: "rollout-child-start:" + child.ID.String(),
		})
	}
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
	if err := validateLaneMutationActor(req.ActorType, req.ActorCredentialID); err != nil {
		return err
	}
	if err := validateReleaseInput(req.FirmwareFileIDs, req.ReleaseTargets); err != nil {
		return err
	}
	if len(req.DeviceIdentifiers) > 0 {
		if err := validateIdentifiers(req.DeviceIdentifiers); err != nil {
			return err
		}
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
	if len(req.DeviceIdentifiers) == 0 {
		return nil
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
	if err := rollout.ValidateHashratePolicy(req.HashratePolicy); err != nil {
		return err
	}
	if err := rollout.ValidateActorIdentity(req.ActorType, req.ActorCredentialID); err != nil {
		return err
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || strings.TrimSpace(req.Reason) == "" {
		return fleeterror.NewInvalidArgumentError("idempotency key and reason are required")
	}
	if len(req.ModelPlans) > 0 {
		seenDeclarations := make(map[uuid.UUID]struct{}, len(req.ModelPlans))
		seenStartKeys := make(map[string]struct{}, len(req.ModelPlans))
		for _, plan := range req.ModelPlans {
			if plan.LaneModelID == uuid.Nil || plan.ExpectedModelRevision <= 0 ||
				strings.TrimSpace(plan.FirmwareFileID) == "" ||
				strings.TrimSpace(plan.ModelStartKey) == "" {
				return fleeterror.NewInvalidArgumentError(
					"model declaration, revision, firmware file, and model start key are required",
				)
			}
			if _, duplicate := seenDeclarations[plan.LaneModelID]; duplicate {
				return fleeterror.NewInvalidArgumentError("model declarations cannot be selected more than once")
			}
			if _, duplicate := seenStartKeys[plan.ModelStartKey]; duplicate {
				return fleeterror.NewInvalidArgumentError("model start keys must be unique")
			}
			seenDeclarations[plan.LaneModelID] = struct{}{}
			seenStartKeys[plan.ModelStartKey] = struct{}{}
			if err := rollout.ValidateHashratePolicy(plan.HashratePolicy); err != nil {
				return err
			}
			if len(plan.Batches) == 0 {
				return fleeterror.NewInvalidArgumentError("at least one model rollout batch is required")
			}
			if err := validateStartBatches(plan.Batches); err != nil {
				return err
			}
		}
		return nil
	}
	if err := validateReleaseInput(req.FirmwareFileIDs, req.ReleaseTargets); err != nil {
		return err
	}
	if len(req.Batches) == 0 {
		return fleeterror.NewInvalidArgumentError("at least one rollout batch is required")
	}
	return validateStartBatches(req.Batches)
}

func validateStartBatches(batches []rollout.CreateBatch) error {
	identifiers := make([]string, 0)
	for _, batch := range batches {
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

func CanonicalModelIdentityKey(manufacturer, model string) string {
	normalizedManufacturer := strings.ToLower(strings.TrimSpace(manufacturer))
	normalizedModel := strings.ToLower(strings.TrimSpace(model))
	if normalizedManufacturer == "" || normalizedModel == "" {
		return ""
	}
	return fmt.Sprintf(
		"v1:%d:%s:%d:%s",
		len([]byte(normalizedManufacturer)),
		normalizedManufacturer,
		len([]byte(normalizedModel)),
		normalizedModel,
	)
}

type modelMutationOperation string

const (
	modelMutationCreateDeclaration modelMutationOperation = "create_declaration"
	modelMutationPublishTarget     modelMutationOperation = "publish_target"
	modelMutationUpdateMembership  modelMutationOperation = "update_membership"
)

type modelMutationFingerprintRequest struct {
	Operation         modelMutationOperation
	LaneID            uuid.UUID
	Selector          ModelDeclarationSelector
	ExpectedRevision  int64
	ReleaseTargets    []ReleaseTarget
	AddIdentifiers    []string
	RemoveIdentifiers []string
	ConfirmFirmware   bool
	ConfirmReassign   bool
	ReassignmentToken string
	Reason            string
	ActorUserID       int64
	ActorType         rollout.ActorType
	ActorCredentialID *string
}

type modelMutationFingerprintPayload struct {
	Operation         string
	LaneID            uuid.UUID
	LaneModelID       uuid.UUID
	ModelIdentityKey  string
	ExpectedRevision  int64
	ReleaseTargets    []ReleaseTarget
	AddIdentifiers    []string
	RemoveIdentifiers []string
	ConfirmFirmware   bool
	ConfirmReassign   bool
	ReassignmentToken string
	Reason            string
	ActorUserID       int64
	ActorType         rollout.ActorType
	ActorCredentialID string
}

func fingerprintModelMutation(req modelMutationFingerprintRequest) (string, error) {
	payload := modelMutationFingerprintPayload{
		Operation:         string(req.Operation),
		LaneID:            req.LaneID,
		LaneModelID:       req.Selector.LaneModelID,
		ModelIdentityKey:  req.Selector.ModelIdentityKey,
		ExpectedRevision:  req.ExpectedRevision,
		ReleaseTargets:    sortedTargets(req.ReleaseTargets),
		AddIdentifiers:    sortedStrings(req.AddIdentifiers),
		RemoveIdentifiers: sortedStrings(req.RemoveIdentifiers),
		ConfirmFirmware:   req.ConfirmFirmware,
		ConfirmReassign:   req.ConfirmReassign,
		ReassignmentToken: req.ReassignmentToken,
		Reason:            req.Reason,
		ActorUserID:       req.ActorUserID,
		ActorType:         req.ActorType,
		ActorCredentialID: actorCredentialValue(req.ActorCredentialID),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal model mutation fingerprint: %w", err)
	}
	return cryptohash.Sha256Hex(string(encoded)), nil
}

func fingerprintLaneCreate(req CreateLaneRequest) (string, error) {
	// Confirmation booleans acknowledge side effects but do not change the
	// desired lane. The preview token does identify the exact source state that
	// was acknowledged, so it participates in idempotency.
	payload := struct {
		Label                         string
		Description                   string
		FirmwareFileIDs               []string
		ReleaseTargets                []ReleaseTarget
		DeviceIdentifiers             []string
		ReassignmentConfirmationToken string
		ActorUserID                   int64
		ActorType                     rollout.ActorType
		ActorCredentialID             string
	}{
		Label:                         req.Label,
		Description:                   req.Description,
		FirmwareFileIDs:               sortedStrings(req.FirmwareFileIDs),
		ReleaseTargets:                sortedTargets(req.ReleaseTargets),
		DeviceIdentifiers:             sortedStrings(req.DeviceIdentifiers),
		ReassignmentConfirmationToken: req.ReassignmentConfirmationToken,
		ActorUserID:                   req.ActorUserID,
		ActorType:                     req.ActorType,
		ActorCredentialID:             actorCredentialValue(req.ActorCredentialID),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal lane create fingerprint: %w", err)
	}
	return cryptohash.Sha256Hex(string(encoded)), nil
}

func ReassignmentConfirmationToken(
	req PreviewLaneRequest,
	preview InitialEnforcementPreview,
) (string, error) {
	if !preview.RequiresReassignConfirmation {
		return "", nil
	}
	reassignments := append([]MembershipReassignment(nil), preview.Reassignments...)
	sort.Slice(reassignments, func(i, j int) bool {
		return reassignments[i].DeviceIdentifier < reassignments[j].DeviceIdentifier
	})
	type sourceState struct {
		DeviceIdentifier   string
		SourceLaneID       uuid.UUID
		SourceChannelID    int64
		SourceLaneRevision int64
	}
	sources := make([]sourceState, 0, len(reassignments))
	for _, reassignment := range reassignments {
		sources = append(sources, sourceState{
			DeviceIdentifier:   reassignment.DeviceIdentifier,
			SourceLaneID:       reassignment.SourceLaneID,
			SourceChannelID:    reassignment.SourceChannelID,
			SourceLaneRevision: reassignment.SourceLaneRevision,
		})
	}
	payload := struct {
		OrgID             int64
		DeviceIdentifiers []string
		ReleaseTargets    []ReleaseTarget
		Sources           []sourceState
	}{
		OrgID:             req.OrgID,
		DeviceIdentifiers: sortedStrings(req.DeviceIdentifiers),
		ReleaseTargets:    sortedTargets(req.ReleaseTargets),
		Sources:           sources,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal reassignment confirmation token: %w", err)
	}
	return cryptohash.Sha256Hex(string(encoded)), nil
}

func actorCredentialValue(credentialID *string) string {
	if credentialID == nil {
		return ""
	}
	return *credentialID
}

func fingerprintLaneStart(req StartRolloutRequest) (string, error) {
	payload := struct {
		LaneID          uuid.UUID
		Name            string
		FirmwareFileIDs []string
		ReleaseTargets  []ReleaseTarget
		Batches         []rollout.CreateBatch
		HashratePolicy  *rollout.HashratePolicy
		Reason          string
		ActorUserID     int64
		ModelPlans      []StartRolloutModelPlan
	}{
		LaneID:          req.LaneID,
		Name:            req.Name,
		FirmwareFileIDs: sortedStrings(req.FirmwareFileIDs),
		ReleaseTargets:  sortedTargets(req.ReleaseTargets),
		Batches:         req.Batches,
		HashratePolicy:  req.HashratePolicy,
		Reason:          req.Reason,
		ActorUserID:     req.ActorUserID,
		ModelPlans:      req.ModelPlans,
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

func fingerprintTopologyRepair(req RepairModelBindingRequest) string {
	return cryptohash.Sha256Hex(fmt.Sprintf(
		"%d\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%d\x00%s\x00%s",
		req.OrgID,
		req.LaneID,
		req.LaneModelID,
		req.DeviceIdentifier,
		req.ExpectedRevision,
		req.IdempotencyKey,
		req.Reason,
		req.ActorUserID,
		req.ActorType,
		actorCredentialValue(req.ActorCredentialID),
	))
}

func fingerprintTopologyEnable(req EnableTopologyRequest) string {
	return cryptohash.Sha256Hex(fmt.Sprintf(
		"%d\x00%d\x00%s\x00%s\x00%d\x00%s\x00%s",
		req.OrgID,
		req.ExpectedRevision,
		req.IdempotencyKey,
		req.Reason,
		req.ActorUserID,
		req.ActorType,
		actorCredentialValue(req.ActorCredentialID),
	))
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
		errors.Is(err, ErrDeclarationConflict),
		errors.Is(err, ErrCompatibility),
		errors.Is(err, ErrInitialEnforcementConfirmationRequired),
		errors.Is(err, ErrFirmwareConfirmationRequired),
		errors.Is(err, ErrReassignmentConfirmationRequired),
		errors.Is(err, ErrFirmwareConvergenceActive),
		errors.Is(err, ErrLaneWorkActive),
		errors.Is(err, ErrLaneEmpty),
		errors.Is(err, ErrTopologyNotReady),
		errors.Is(err, ErrTopologyAlreadyEnabled),
		errors.Is(err, ErrTopologyRepairConflict),
		errors.Is(err, ErrModelWorkActive),
		errors.Is(err, ErrScalarProjectionUnavailable):
		return fleeterror.NewFailedPreconditionErrorf("%w", err)
	case errors.Is(err, ErrIdempotencyConflict):
		return fleeterror.NewAlreadyExistsErrorf("%w", err)
	default:
		return err
	}
}
