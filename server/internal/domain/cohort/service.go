// Package cohort is the domain layer for cohort CRUD.
package cohort

import (
	"context"
	"fmt"
	"strings"
	"time"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

// Service is the domain entry point for cohort CRUD.
type Service struct {
	store                   interfaces.CohortStore
	sourceDeviceSetResolver SourceDeviceSetResolver
	audit                   AuditLogger
	metrics                 Metrics
	firmwareMetadata        FirmwareMetadataProvider
}

// SourceDeviceSetResolver resolves group membership for create-from-group.
type SourceDeviceSetResolver interface {
	GetCollectionType(ctx context.Context, orgID int64, collectionID int64) (collectionpb.CollectionType, error)
	GetDeviceIdentifiersByDeviceSetID(ctx context.Context, deviceSetID, orgID int64) ([]string, error)
}

// FirmwareMetadataProvider resolves uploaded firmware file target metadata.
type FirmwareMetadataProvider interface {
	GetFirmwareMetadata(fileID string) (files.FirmwareMetadata, error)
}

// Option configures a Service.
type Option func(*Service)

// WithAuditLogger wires activity logging.
func WithAuditLogger(logger AuditLogger) Option {
	return func(s *Service) {
		if logger != nil {
			s.audit = logger
		}
	}
}

// WithMetrics wires operational metrics.
func WithMetrics(metrics Metrics) Option {
	return func(s *Service) {
		if metrics != nil {
			s.metrics = metrics
		}
	}
}

// WithSourceDeviceSetResolver wires the group-to-cohort bridge.
func WithSourceDeviceSetResolver(resolver SourceDeviceSetResolver) Option {
	return func(s *Service) {
		s.sourceDeviceSetResolver = resolver
	}
}

// WithFirmwareMetadataProvider wires firmware target validation.
func WithFirmwareMetadataProvider(provider FirmwareMetadataProvider) Option {
	return func(s *Service) {
		s.firmwareMetadata = provider
	}
}

// SetFirmwareMetadataProvider wires firmware target validation after service construction.
func (s *Service) SetFirmwareMetadataProvider(provider FirmwareMetadataProvider) {
	s.firmwareMetadata = provider
}

// NewService returns a cohort service.
func NewService(store interfaces.CohortStore, opts ...Option) *Service {
	s := &Service{
		store:   store,
		audit:   NoOpAuditLogger{},
		metrics: NoOpMetrics{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// CreateCohort validates and inserts a cohort plus explicit members.
func (s *Service) CreateCohort(ctx context.Context, params models.CreateCohortParams) (*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	params.Label = strings.TrimSpace(params.Label)
	params.Purpose = strings.TrimSpace(params.Purpose)
	if params.Label == "" {
		return nil, fleeterror.NewInvalidArgumentError("cohort label is required")
	}
	if params.Purpose == "" {
		return nil, fleeterror.NewInvalidArgumentError("cohort purpose is required")
	}
	if params.DesiredFirmwareFileID != nil && strings.TrimSpace(*params.DesiredFirmwareFileID) == "" {
		params.DesiredFirmwareFileID = nil
	}
	if params.DesiredFirmwareFileID != nil && s.firmwareMetadata != nil {
		metadata, err := s.firmwareMetadata.GetFirmwareMetadata(*params.DesiredFirmwareFileID)
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentErrorf("invalid desired_firmware_file_id: %v", err)
		}
		params.DesiredFirmwareTargetManufacturer = metadata.TargetManufacturer
		params.DesiredFirmwareTargetModel = metadata.TargetModel
	}
	if params.SourceActorType == "" {
		params.SourceActorType = models.SourceActorUser
	}
	if params.DeviceSelector != nil {
		if len(params.DeviceIdentifiers) > 0 || params.SourceDeviceSetID != nil {
			return nil, fleeterror.NewInvalidArgumentError("select cannot be combined with explicit devices or source_device_set_id")
		}
		params.DeviceSelector.Product = trimOptionalString(params.DeviceSelector.Product)
		params.DeviceSelector.Model = trimOptionalString(params.DeviceSelector.Model)
		if params.DeviceSelector.Count <= 0 {
			return nil, fleeterror.NewInvalidArgumentError("select.count must be greater than zero")
		}
		if params.DeviceSelector.Count > 10000 {
			return nil, fleeterror.NewInvalidArgumentError("select.count must be at most 10000")
		}
	}
	if params.SourceDeviceSetID != nil {
		if s.sourceDeviceSetResolver == nil {
			return nil, fleeterror.NewInternalError("cohort source device-set resolver is not configured")
		}
		collectionType, err := s.sourceDeviceSetResolver.GetCollectionType(ctx, params.OrgID, *params.SourceDeviceSetID)
		if err != nil {
			return nil, err
		}
		if collectionType != collectionpb.CollectionType_COLLECTION_TYPE_GROUP {
			return nil, fleeterror.NewInvalidArgumentError("source_device_set_id must reference a group")
		}
		ids, err := s.sourceDeviceSetResolver.GetDeviceIdentifiersByDeviceSetID(ctx, *params.SourceDeviceSetID, params.OrgID)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, fleeterror.NewInvalidArgumentError("source group has no devices")
		}
		params.DeviceIdentifiers = ids
		sourceID := fmt.Sprintf("device_set:%d", *params.SourceDeviceSetID)
		params.SourceActorID = &sourceID
	}
	if params.DeviceSelector == nil && params.SourceDeviceSetID == nil && len(params.DeviceIdentifiers) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("cohort requires initial members")
	}
	if err := validateUniqueDeviceIdentifiers(params.DeviceIdentifiers); err != nil {
		return nil, err
	}

	created, err := s.store.CreateCohort(ctx, params)
	if err != nil {
		return nil, err
	}
	if err := validateCohortSingleMinerType(created); err != nil {
		return nil, err
	}
	if err := s.validateDesiredFirmwareTarget(models.UpdateCohortParams{
		DesiredFirmwareFileID:    params.DesiredFirmwareFileID,
		DesiredFirmwareFileIDSet: params.DesiredFirmwareFileID != nil,
	}, created); err != nil {
		return nil, err
	}
	s.auditCohortCreated(ctx, created)
	return created, nil
}

// GetCohort returns a cohort with explicit members.
func (s *Service) GetCohort(ctx context.Context, orgID, cohortID int64) (*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	return s.store.GetCohort(ctx, orgID, cohortID)
}

// ListCohorts returns cohorts for an org.
func (s *Service) ListCohorts(ctx context.Context, params models.ListCohortsParams) (models.PagedCohorts, error) {
	if s.store == nil {
		return models.PagedCohorts{}, fleeterror.NewInternalError("cohort store is not configured")
	}
	return s.store.ListCohorts(ctx, params)
}

// ListCohortsByOwner returns cohorts owned by a user.
func (s *Service) ListCohortsByOwner(ctx context.Context, params models.ListCohortsByOwnerParams) (models.PagedCohorts, error) {
	if s.store == nil {
		return models.PagedCohorts{}, fleeterror.NewInternalError("cohort store is not configured")
	}
	return s.store.ListCohortsByOwner(ctx, params)
}

// UpdateCohort changes mutable cohort metadata and desired state.
func (s *Service) UpdateCohort(ctx context.Context, params models.UpdateCohortParams) (*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	if params.Label != nil {
		trimmed := strings.TrimSpace(*params.Label)
		if trimmed == "" {
			return nil, fleeterror.NewInvalidArgumentError("cohort label is required")
		}
		params.Label = &trimmed
	}
	if params.Purpose != nil {
		trimmed := strings.TrimSpace(*params.Purpose)
		if trimmed == "" {
			return nil, fleeterror.NewInvalidArgumentError("cohort purpose is required")
		}
		params.Purpose = &trimmed
	}
	if params.DesiredFirmwareFileIDSet && params.DesiredFirmwareFileID != nil {
		trimmed := strings.TrimSpace(*params.DesiredFirmwareFileID)
		if trimmed == "" {
			params.DesiredFirmwareFileID = nil
		} else {
			params.DesiredFirmwareFileID = &trimmed
		}
	}
	target, err := s.store.GetCohort(ctx, params.OrgID, params.CohortID)
	if err != nil {
		return nil, err
	}
	if target.IsDefault {
		return nil, fleeterror.NewInvalidArgumentError("default cohort firmware must be set per manufacturer/model")
	}
	if err := s.validateDesiredFirmwareTarget(params, target); err != nil {
		return nil, err
	}
	updated, err := s.store.UpdateCohort(ctx, params)
	if err != nil {
		return nil, err
	}
	result := updated
	if params.DesiredFirmwareFileIDSet {
		manufacturer, model, err := cohortSingleMinerType(target)
		if err != nil {
			return nil, err
		}
		if manufacturer != "" && model != "" {
			result, err = s.store.SetCohortFirmwareTarget(ctx, models.SetCohortFirmwareTargetParams{
				OrgID:          params.OrgID,
				CohortID:       params.CohortID,
				Manufacturer:   manufacturer,
				Model:          model,
				FirmwareFileID: params.DesiredFirmwareFileID,
			})
			if err != nil {
				return nil, err
			}
		}
	}
	s.auditCohortFieldsUpdated(ctx, target, result, params)
	return result, nil
}

// SetCohortFirmwareTarget sets or clears firmware for a cohort manufacturer/model.
func (s *Service) SetCohortFirmwareTarget(ctx context.Context, params models.SetCohortFirmwareTargetParams) (*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	params.Manufacturer = strings.TrimSpace(params.Manufacturer)
	params.Model = strings.TrimSpace(params.Model)
	if params.Manufacturer == "" {
		return nil, fleeterror.NewInvalidArgumentError("manufacturer is required")
	}
	if params.Model == "" {
		return nil, fleeterror.NewInvalidArgumentError("model is required")
	}
	if params.FirmwareFileID != nil {
		trimmed := strings.TrimSpace(*params.FirmwareFileID)
		if trimmed == "" {
			params.FirmwareFileID = nil
		} else {
			params.FirmwareFileID = &trimmed
		}
	}

	target, err := s.store.GetCohort(ctx, params.OrgID, params.CohortID)
	if err != nil {
		return nil, err
	}
	if target.State != models.CohortStateActive {
		return nil, fleeterror.NewInvalidArgumentError("cohort is not active")
	}
	if target.IsDefault {
		if !isSuperAdminRole(params.ActorRole) {
			return nil, fleeterror.NewForbiddenError("default cohort firmware requires super admin")
		}
	} else if err := authorizeCohortOwnerMutation(target, params.ActorUserID, params.ActorRole); err != nil {
		return nil, err
	}
	if !target.IsDefault {
		manufacturer, model, err := cohortSingleMinerType(target)
		if err != nil {
			return nil, err
		}
		if manufacturer == "" || model == "" {
			return nil, fleeterror.NewInvalidArgumentError("non-default cohort firmware target requires cohort members")
		}
		if manufacturer != params.Manufacturer || model != params.Model {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"firmware target %s does not match cohort miner type %s",
				formatCohortMinerType(params.Manufacturer, params.Model),
				formatCohortMinerType(manufacturer, model),
			)
		}
	}
	if params.FirmwareFileID != nil && s.firmwareMetadata != nil {
		metadata, err := s.firmwareMetadata.GetFirmwareMetadata(*params.FirmwareFileID)
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentErrorf("invalid firmware_file_id: %v", err)
		}
		if metadata.TargetManufacturer != params.Manufacturer || metadata.TargetModel != params.Model {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"firmware target %s does not match requested target %s",
				formatCohortMinerType(metadata.TargetManufacturer, metadata.TargetModel),
				formatCohortMinerType(params.Manufacturer, params.Model),
			)
		}
	}
	updated, err := s.store.SetCohortFirmwareTarget(ctx, params)
	if err != nil {
		return nil, err
	}
	s.auditCohortFirmwareTargetUpdated(ctx, target, updated, params)
	return updated, nil
}

// AddDevicesToCohort moves devices into the target cohort after ownership checks.
func (s *Service) AddDevicesToCohort(ctx context.Context, params models.MembershipMutationParams) (*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	if err := validateUniqueDeviceIdentifiers(params.DeviceIdentifiers); err != nil {
		return nil, err
	}
	target, err := s.store.GetCohort(ctx, params.OrgID, params.CohortID)
	if err != nil {
		return nil, err
	}
	if err := authorizeCohortOwnerMutation(target, params.ActorUserID, params.ActorRole); err != nil {
		return nil, err
	}
	if err := s.authorizeDeviceMoves(ctx, params); err != nil {
		return nil, err
	}
	if target.DesiredFirmwareFileID != nil && s.firmwareMetadata != nil {
		metadata, err := s.firmwareMetadata.GetFirmwareMetadata(*target.DesiredFirmwareFileID)
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentErrorf("invalid desired_firmware_file_id: %v", err)
		}
		params.DesiredFirmwareTargetManufacturer = metadata.TargetManufacturer
		params.DesiredFirmwareTargetModel = metadata.TargetModel
	}
	updated, err := s.store.MoveDevicesToCohort(ctx, params)
	if err != nil {
		return nil, err
	}
	if err := validateCohortSingleMinerType(updated); err != nil {
		return nil, err
	}
	if err := s.validateDesiredFirmwareTarget(models.UpdateCohortParams{
		DesiredFirmwareFileID:    target.DesiredFirmwareFileID,
		DesiredFirmwareFileIDSet: target.DesiredFirmwareFileID != nil,
	}, updated); err != nil {
		return nil, err
	}
	s.auditCohortMembersChanged(ctx, updated, "members_added", len(params.DeviceIdentifiers))
	return updated, nil
}

// RemoveDevicesFromCohort releases explicit members from one cohort back to default.
func (s *Service) RemoveDevicesFromCohort(ctx context.Context, params models.MembershipMutationParams) (*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	if err := validateUniqueDeviceIdentifiers(params.DeviceIdentifiers); err != nil {
		return nil, err
	}
	cohort, err := s.store.GetCohort(ctx, params.OrgID, params.CohortID)
	if err != nil {
		return nil, err
	}
	if err := authorizeCohortOwnerMutation(cohort, params.ActorUserID, params.ActorRole); err != nil {
		return nil, err
	}
	updated, err := s.store.RemoveDevicesAndGetCohort(ctx, params)
	if err != nil {
		return nil, err
	}
	s.auditCohortMembersChanged(ctx, updated, "members_removed", -len(params.DeviceIdentifiers))
	return updated, nil
}

// ReleaseCohort releases all members back to default and marks the cohort released.
func (s *Service) ReleaseCohort(ctx context.Context, params models.MembershipMutationParams) (*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	cohort, err := s.store.GetCohort(ctx, params.OrgID, params.CohortID)
	if err != nil {
		return nil, err
	}
	if err := authorizeCohortOwnerMutation(cohort, params.ActorUserID, params.ActorRole); err != nil {
		return nil, err
	}
	released, err := s.store.ReleaseCohort(ctx, params.OrgID, params.CohortID)
	if err != nil {
		return nil, err
	}
	s.auditCohortReleased(ctx, released)
	return released, nil
}

// SweepExpired releases expired active cohorts.
func (s *Service) SweepExpired(ctx context.Context) ([]*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	released, err := s.store.SweepExpiredCohorts(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range released {
		s.auditCohortExpired(ctx, c)
	}
	return released, nil
}

// ListDevices returns devices decorated with their effective cohort.
func (s *Service) ListDevices(ctx context.Context, params models.ListDevicesParams) (models.PagedCohortDevices, error) {
	if s.store == nil {
		return models.PagedCohortDevices{}, fleeterror.NewInternalError("cohort store is not configured")
	}
	return s.store.ListDevices(ctx, params)
}

// DeleteCohort soft-deletes a cohort by releasing it and clearing memberships.
func (s *Service) DeleteCohort(ctx context.Context, orgID, cohortID int64) (*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	cohort, err := s.store.ReleaseCohort(ctx, orgID, cohortID)
	if err != nil {
		return nil, err
	}
	s.auditCohortDeleted(ctx, cohort)
	return cohort, nil
}

func validateUniqueDeviceIdentifiers(ids []string) error {
	seen := make(map[string]struct{}, len(ids))
	for i, id := range ids {
		id = strings.TrimSpace(id)
		ids[i] = id
		if id == "" {
			return fleeterror.NewInvalidArgumentErrorf("device_identifiers[%d] is empty", i)
		}
		if _, ok := seen[id]; ok {
			return fleeterror.NewInvalidArgumentErrorf("duplicate device identifier %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (s *Service) validateDesiredFirmwareTarget(params models.UpdateCohortParams, cohort *models.Cohort) error {
	if !params.DesiredFirmwareFileIDSet || params.DesiredFirmwareFileID == nil {
		return nil
	}
	if s.firmwareMetadata == nil {
		return nil
	}

	metadata, err := s.firmwareMetadata.GetFirmwareMetadata(*params.DesiredFirmwareFileID)
	if err != nil {
		return fleeterror.NewInvalidArgumentErrorf("invalid desired_firmware_file_id: %v", err)
	}

	manufacturer, model, err := cohortSingleMinerType(cohort)
	if err != nil {
		return err
	}
	if manufacturer == "" && model == "" {
		return nil
	}
	if manufacturer != metadata.TargetManufacturer || model != metadata.TargetModel {
		return fleeterror.NewInvalidArgumentErrorf(
			"firmware target %s does not match cohort miner type %s",
			formatCohortMinerType(metadata.TargetManufacturer, metadata.TargetModel),
			formatCohortMinerType(manufacturer, model),
		)
	}
	return nil
}

func validateCohortSingleMinerType(cohort *models.Cohort) error {
	_, _, err := cohortSingleMinerType(cohort)
	return err
}

func cohortSingleMinerType(cohort *models.Cohort) (string, string, error) {
	if cohort == nil || len(cohort.Members) == 0 {
		return "", "", nil
	}
	var manufacturer string
	var model string
	for _, member := range cohort.Members {
		nextManufacturer := strings.TrimSpace(member.Display.Manufacturer)
		nextModel := strings.TrimSpace(member.Display.Model)
		if nextManufacturer == "" || nextModel == "" {
			return "", "", fleeterror.NewInvalidArgumentErrorf("cohort member %q is missing manufacturer or model", member.DeviceIdentifier)
		}
		if manufacturer == "" && model == "" {
			manufacturer = nextManufacturer
			model = nextModel
			continue
		}
		if nextManufacturer != manufacturer || nextModel != model {
			return "", "", fleeterror.NewInvalidArgumentError("cohort members must have a single manufacturer and model")
		}
	}
	return manufacturer, model, nil
}

func formatCohortMinerType(manufacturer, model string) string {
	manufacturer = strings.TrimSpace(manufacturer)
	model = strings.TrimSpace(model)
	switch {
	case manufacturer != "" && model != "":
		return manufacturer + " " + model
	case manufacturer != "":
		return manufacturer
	case model != "":
		return model
	default:
		return "unknown"
	}
}

func (s *Service) authorizeDeviceMoves(ctx context.Context, params models.MembershipMutationParams) error {
	ownership, err := s.store.ListCohortDeviceOwnership(ctx, params.OrgID, params.DeviceIdentifiers)
	if err != nil {
		return err
	}
	for _, row := range ownership {
		if row.CohortID == params.CohortID {
			continue
		}
		if isSuperAdminRole(params.ActorRole) {
			continue
		}
		if row.OwnerUserID == nil {
			return fleeterror.NewForbiddenErrorf("device %q is leased by an ownerless cohort; admin required", row.DeviceIdentifier)
		}
		if *row.OwnerUserID == params.ActorUserID {
			continue
		}
		owner := "another user"
		if row.OwnerUsername != nil && *row.OwnerUsername != "" {
			owner = *row.OwnerUsername
		}
		return fleeterror.NewForbiddenErrorf("device %q is leased by %s", row.DeviceIdentifier, owner)
	}
	return nil
}

func authorizeCohortOwnerMutation(cohort *models.Cohort, actorUserID int64, actorRole string) error {
	if cohort == nil {
		return nil
	}
	if cohort.IsDefault {
		return fleeterror.NewInvalidArgumentError("default cohort cannot be mutated through this operation")
	}
	if isSuperAdminRole(actorRole) {
		return nil
	}
	if cohort.OwnerUserID == nil {
		return fleeterror.NewForbiddenErrorf("cohort %d is ownerless; admin required", cohort.ID)
	}
	if *cohort.OwnerUserID == actorUserID {
		return nil
	}
	owner := "another user"
	if cohort.OwnerUsername != nil && *cohort.OwnerUsername != "" {
		owner = *cohort.OwnerUsername
	}
	return fleeterror.NewForbiddenErrorf("cohort %d is leased by %s", cohort.ID, owner)
}

func isSuperAdminRole(role string) bool {
	switch strings.ToUpper(strings.TrimSpace(role)) {
	case "SUPER_ADMIN":
		return true
	default:
		return false
	}
}

func (s *Service) auditCohortCreated(ctx context.Context, cohort *models.Cohort) {
	if cohort == nil {
		return
	}
	orgID := cohort.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           activityTypeCreated,
		OrganizationID: &orgID,
		Description:    fmt.Sprintf("Created cohort %q (id=%d)", cohort.Label, cohort.ID),
		Metadata: map[string]any{
			"cohort_id": cohort.ID,
			"label":     cohort.Label,
		},
	}
	activity.StampActor(ctx, &event)
	s.audit.Log(ctx, event)
}

func (s *Service) auditCohortDeleted(ctx context.Context, cohort *models.Cohort) {
	if cohort == nil {
		return
	}
	orgID := cohort.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           activityTypeDeleted,
		OrganizationID: &orgID,
		Description:    fmt.Sprintf("Deleted cohort %q (id=%d)", cohort.Label, cohort.ID),
		Metadata: map[string]any{
			"cohort_id": cohort.ID,
			"label":     cohort.Label,
		},
	}
	activity.StampActor(ctx, &event)
	s.audit.Log(ctx, event)
}

func (s *Service) auditCohortReleased(ctx context.Context, cohort *models.Cohort) {
	if cohort == nil {
		return
	}
	orgID := cohort.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           activityTypeReleased,
		OrganizationID: &orgID,
		Description:    fmt.Sprintf("Released cohort %q (id=%d)", cohort.Label, cohort.ID),
		Metadata: map[string]any{
			"cohort_id": cohort.ID,
			"label":     cohort.Label,
		},
	}
	activity.StampActor(ctx, &event)
	s.audit.Log(ctx, event)
}

func (s *Service) auditCohortExpired(ctx context.Context, cohort *models.Cohort) {
	if cohort == nil {
		return
	}
	orgID := cohort.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           activityTypeExpired,
		OrganizationID: &orgID,
		Description:    fmt.Sprintf("Expired cohort %q (id=%d)", cohort.Label, cohort.ID),
		Metadata: map[string]any{
			"cohort_id": cohort.ID,
			"label":     cohort.Label,
		},
	}
	activity.StampActor(ctx, &event)
	s.audit.Log(ctx, event)
}

func (s *Service) auditCohortFieldsUpdated(ctx context.Context, before, after *models.Cohort, params models.UpdateCohortParams) {
	cohort := after
	if cohort == nil {
		cohort = before
	}
	if cohort == nil {
		return
	}

	metadata := cohortUpdateMetadata(cohort, "cohort_fields_updated")
	changedFields := make([]string, 0, 5)
	if params.Label != nil {
		changedFields = append(changedFields, "label")
	}
	if params.Purpose != nil {
		changedFields = append(changedFields, "purpose")
	}
	if params.ExpiresAt != nil || params.ClearExpiresAt {
		changedFields = append(changedFields, "expires_at")
		metadata["old_expires_at"] = timeOrNil(cohortExpiresAt(before))
		metadata["new_expires_at"] = timeOrNil(cohortExpiresAt(after))
	}
	if params.DesiredFirmwareFileIDSet {
		changedFields = append(changedFields, "desired_firmware_file_id")
		metadata["old_firmware_file_id"] = stringOrNil(cohortDesiredFirmwareFileID(before))
		metadata["new_firmware_file_id"] = stringOrNil(cohortDesiredFirmwareFileID(after))
	}
	if params.DesiredConfigJSONSet || params.ClearDesiredConfig {
		changedFields = append(changedFields, "desired_config")
		metadata["desired_config_changed"] = params.DesiredConfigJSONSet
		metadata["desired_config_cleared"] = params.ClearDesiredConfig
		metadata["old_desired_config_present"] = cohortHasDesiredConfig(before)
		metadata["new_desired_config_present"] = cohortHasDesiredConfig(after)
	}
	metadata["changed_fields"] = changedFields
	s.auditCohortUpdated(ctx, cohort, nil, fmt.Sprintf("Updated cohort %q (id=%d)", cohort.Label, cohort.ID), metadata)
}

func (s *Service) auditCohortFirmwareTargetUpdated(
	ctx context.Context,
	before *models.Cohort,
	after *models.Cohort,
	params models.SetCohortFirmwareTargetParams,
) {
	cohort := after
	if cohort == nil {
		cohort = before
	}
	if cohort == nil {
		return
	}
	metadata := cohortUpdateMetadata(cohort, "firmware_target_updated")
	metadata["manufacturer"] = params.Manufacturer
	metadata["model"] = params.Model
	metadata["old_firmware_file_id"] = stringOrNil(cohortFirmwareFileIDForTarget(before, params.Manufacturer, params.Model))
	metadata["new_firmware_file_id"] = stringOrNil(params.FirmwareFileID)
	s.auditCohortUpdated(ctx, cohort, nil, fmt.Sprintf("Updated cohort firmware target for %q (id=%d)", cohort.Label, cohort.ID), metadata)
}

func (s *Service) auditCohortMembersChanged(ctx context.Context, cohort *models.Cohort, updateKind string, memberCountDelta int) {
	if cohort == nil {
		return
	}
	affectedCount := memberCountDelta
	if affectedCount < 0 {
		affectedCount = -affectedCount
	}
	metadata := cohortUpdateMetadata(cohort, updateKind)
	metadata["affected_member_count"] = affectedCount
	metadata["member_count_delta"] = memberCountDelta
	s.auditCohortUpdated(ctx, cohort, &affectedCount, fmt.Sprintf("Updated cohort members for %q (id=%d)", cohort.Label, cohort.ID), metadata)
}

func (s *Service) auditCohortUpdated(ctx context.Context, cohort *models.Cohort, scopeCount *int, description string, metadata map[string]any) {
	if cohort == nil {
		return
	}
	orgID := cohort.OrgID
	scopeType := "cohort"
	scopeLabel := cohort.Label
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           activityTypeUpdated,
		OrganizationID: &orgID,
		Description:    description,
		ScopeType:      &scopeType,
		ScopeLabel:     &scopeLabel,
		ScopeCount:     scopeCount,
		Metadata:       metadata,
	}
	activity.StampActor(ctx, &event)
	s.audit.Log(ctx, event)
}

func cohortUpdateMetadata(cohort *models.Cohort, updateKind string) map[string]any {
	return map[string]any{
		"cohort_id":   cohort.ID,
		"label":       cohort.Label,
		"is_default":  cohort.IsDefault,
		"update_kind": updateKind,
	}
}

func cohortExpiresAt(cohort *models.Cohort) *time.Time {
	if cohort == nil {
		return nil
	}
	return cohort.ExpiresAt
}

func cohortDesiredFirmwareFileID(cohort *models.Cohort) *string {
	if cohort == nil {
		return nil
	}
	return cohort.DesiredFirmwareFileID
}

func cohortHasDesiredConfig(cohort *models.Cohort) bool {
	return cohort != nil && len(cohort.DesiredConfigJSON) > 0
}

func cohortFirmwareFileIDForTarget(cohort *models.Cohort, manufacturer, model string) *string {
	if cohort == nil {
		return nil
	}
	for _, target := range cohort.FirmwareTargets {
		if target.Manufacturer == manufacturer && target.Model == model {
			return target.FirmwareFileID
		}
	}
	if !cohort.IsDefault {
		cohortManufacturer, cohortModel, err := cohortSingleMinerType(cohort)
		if err == nil && cohortManufacturer == manufacturer && cohortModel == model {
			return cohort.DesiredFirmwareFileID
		}
	}
	return nil
}

func timeOrNil(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func stringOrNil(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
