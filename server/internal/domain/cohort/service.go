// Package cohort is the domain layer for cohort CRUD.
package cohort

import (
	"context"
	"fmt"
	"strings"
	"time"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	poolpb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
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
	poolReferences          PoolReferenceProvider
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

// PoolReferenceProvider resolves active, organization-scoped pool references.
type PoolReferenceProvider interface {
	GetPool(ctx context.Context, orgID int64, poolID int64) (*poolpb.Pool, error)
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

// WithPoolReferenceProvider wires desired pool reference validation.
func WithPoolReferenceProvider(provider PoolReferenceProvider) Option {
	return func(s *Service) {
		s.poolReferences = provider
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
		return nil, fleeterror.NewInvalidArgumentError("Enter a cohort label.")
	}
	if params.Purpose == "" {
		return nil, fleeterror.NewInvalidArgumentError("Enter a cohort purpose.")
	}
	if err := s.validateDesiredConfig(ctx, params.OrgID, params.DesiredConfig, params.DesiredConfigJSON); err != nil {
		return nil, err
	}
	if params.SourceActorType == "" {
		params.SourceActorType = models.SourceActorUser
	}
	if params.DeviceSelector != nil {
		if len(params.DeviceIdentifiers) > 0 || params.SourceDeviceSetID != nil {
			return nil, fleeterror.NewInvalidArgumentError("Choose only one way to add initial members.")
		}
		params.DeviceSelector.Product = trimOptionalString(params.DeviceSelector.Product)
		params.DeviceSelector.Model = trimOptionalString(params.DeviceSelector.Model)
		if params.DeviceSelector.Count <= 0 {
			return nil, fleeterror.NewInvalidArgumentError("Count must be greater than zero.")
		}
		if params.DeviceSelector.Count > 10000 {
			return nil, fleeterror.NewInvalidArgumentError("Count must be at most 10,000.")
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
			return nil, fleeterror.NewInvalidArgumentError("Select a group for the initial members.")
		}
		ids, err := s.sourceDeviceSetResolver.GetDeviceIdentifiersByDeviceSetID(ctx, *params.SourceDeviceSetID, params.OrgID)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, fleeterror.NewInvalidArgumentError("The selected group has no miners.")
		}
		params.DeviceIdentifiers = ids
		sourceID := fmt.Sprintf("device_set:%d", *params.SourceDeviceSetID)
		params.SourceActorID = &sourceID
	}
	if params.DeviceSelector == nil && params.SourceDeviceSetID == nil && len(params.DeviceIdentifiers) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("Add at least one initial member to create a cohort.")
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
	s.hydrateCohortFirmware(ctx, created)
	s.auditCohortCreated(ctx, created)
	return created, nil
}

// GetCohort returns a cohort with explicit members.
func (s *Service) GetCohort(ctx context.Context, orgID, cohortID int64) (*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	cohort, err := s.store.GetCohort(ctx, orgID, cohortID)
	if err != nil {
		return nil, err
	}
	s.hydrateCohortFirmware(ctx, cohort)
	return cohort, nil
}

// ListCohorts returns cohorts for an org.
func (s *Service) ListCohorts(ctx context.Context, params models.ListCohortsParams) (models.PagedCohorts, error) {
	if s.store == nil {
		return models.PagedCohorts{}, fleeterror.NewInternalError("cohort store is not configured")
	}
	result, err := s.store.ListCohorts(ctx, params)
	if err != nil {
		return models.PagedCohorts{}, err
	}
	s.hydrateCohortsFirmware(ctx, result.Cohorts)
	return result, nil
}

// ListCohortsByOwner returns cohorts owned by a user.
func (s *Service) ListCohortsByOwner(ctx context.Context, params models.ListCohortsByOwnerParams) (models.PagedCohorts, error) {
	if s.store == nil {
		return models.PagedCohorts{}, fleeterror.NewInternalError("cohort store is not configured")
	}
	result, err := s.store.ListCohortsByOwner(ctx, params)
	if err != nil {
		return models.PagedCohorts{}, err
	}
	s.hydrateCohortsFirmware(ctx, result.Cohorts)
	return result, nil
}

// UpdateCohort changes mutable cohort metadata and desired state.
func (s *Service) UpdateCohort(ctx context.Context, params models.UpdateCohortParams) (*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	if params.Label != nil {
		trimmed := strings.TrimSpace(*params.Label)
		if trimmed == "" {
			return nil, fleeterror.NewInvalidArgumentError("Enter a cohort label.")
		}
		params.Label = &trimmed
	}
	if params.Purpose != nil {
		trimmed := strings.TrimSpace(*params.Purpose)
		if trimmed == "" {
			return nil, fleeterror.NewInvalidArgumentError("Enter a cohort purpose.")
		}
		params.Purpose = &trimmed
	}
	target, err := s.store.GetCohort(ctx, params.OrgID, params.CohortID)
	if err != nil {
		return nil, err
	}
	if target.IsDefault && !isDefaultConfigOnlyUpdate(params) {
		return nil, fleeterror.NewInvalidArgumentError("Set default cohort firmware per manufacturer and model.")
	}
	if params.DesiredConfigJSONSet {
		if err := s.validateDesiredConfig(ctx, params.OrgID, params.DesiredConfig, params.DesiredConfigJSON); err != nil {
			return nil, err
		}
	}
	var updated *models.Cohort
	if target.IsDefault {
		updated, err = s.store.UpdateDefaultCohortConfig(ctx, params)
	} else {
		updated, err = s.store.UpdateCohort(ctx, params)
	}
	if err != nil {
		return nil, err
	}
	result := updated
	s.auditCohortFieldsUpdated(ctx, target, result, params)
	s.hydrateCohortFirmware(ctx, result)
	return result, nil
}

// SetCohortFirmwareTarget sets or clears firmware for a cohort manufacturer/model.
func (s *Service) SetCohortFirmwareTarget(ctx context.Context, params models.SetCohortFirmwareTargetParams) (*models.Cohort, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("cohort store is not configured")
	}
	params.Manufacturer = trimOptionalString(params.Manufacturer)
	params.Model = trimOptionalString(params.Model)
	if params.FirmwareFileID != nil {
		trimmed := strings.TrimSpace(*params.FirmwareFileID)
		if trimmed == "" {
			params.FirmwareFileID = nil
		} else {
			params.FirmwareFileID = &trimmed
		}
	}
	if params.FirmwareFileID == nil {
		if params.Manufacturer == nil {
			return nil, fleeterror.NewInvalidArgumentError("Select a product before clearing the firmware target.")
		}
		if params.Model == nil {
			return nil, fleeterror.NewInvalidArgumentError("Select a model before clearing the firmware target.")
		}
	} else if s.firmwareMetadata != nil {
		metadata, err := s.firmwareMetadata.GetFirmwareMetadata(*params.FirmwareFileID)
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentErrorf("Couldn't read the selected firmware file: %v", err)
		}
		if params.Manufacturer == nil {
			params.Manufacturer = trimOptionalString(&metadata.TargetManufacturer)
		}
		if params.Model == nil {
			params.Model = trimOptionalString(&metadata.TargetModel)
		}
		if params.Manufacturer == nil {
			return nil, fleeterror.NewInvalidArgumentError("The selected firmware file is missing a target product.")
		}
		if params.Model == nil {
			return nil, fleeterror.NewInvalidArgumentError("The selected firmware file is missing a target model.")
		}
		if !sameMinerType(metadata.TargetManufacturer, *params.Manufacturer) || !sameMinerType(metadata.TargetModel, *params.Model) {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"Firmware target %s does not match the requested target %s.",
				formatCohortMinerType(metadata.TargetManufacturer, metadata.TargetModel),
				formatCohortMinerType(*params.Manufacturer, *params.Model),
			)
		}
	}
	if params.Manufacturer == nil {
		return nil, fleeterror.NewInvalidArgumentError("Select a product.")
	}
	if params.Model == nil {
		return nil, fleeterror.NewInvalidArgumentError("Select a model.")
	}
	target, err := s.store.GetCohort(ctx, params.OrgID, params.CohortID)
	if err != nil {
		return nil, err
	}
	if target.State != models.CohortStateActive {
		return nil, fleeterror.NewInvalidArgumentError("This cohort is not active.")
	}
	if target.IsDefault {
		if !isSuperAdminRole(params.ActorRole) {
			return nil, fleeterror.NewForbiddenError("Only super admins can update default cohort firmware.")
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
			return nil, fleeterror.NewInvalidArgumentError("Add cohort members before setting firmware.")
		}
		if !sameMinerType(manufacturer, *params.Manufacturer) || !sameMinerType(model, *params.Model) {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"Firmware target %s does not match cohort miner type %s.",
				formatCohortMinerType(*params.Manufacturer, *params.Model),
				formatCohortMinerType(manufacturer, model),
			)
		}
	}
	updated, err := s.store.SetCohortFirmwareTarget(ctx, params)
	if err != nil {
		return nil, err
	}
	s.auditCohortFirmwareTargetUpdated(ctx, target, updated, params)
	s.hydrateCohortFirmware(ctx, updated)
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
	updated, err := s.store.MoveDevicesToCohort(ctx, params)
	if err != nil {
		return nil, err
	}
	if err := validateCohortSingleMinerType(updated); err != nil {
		return nil, err
	}
	s.auditCohortMembersChanged(ctx, updated, "members_added", len(params.DeviceIdentifiers))
	s.hydrateCohortFirmware(ctx, updated)
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
	s.hydrateCohortFirmware(ctx, updated)
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
	s.hydrateCohortFirmware(ctx, released)
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
		s.hydrateCohortFirmware(ctx, c)
		s.auditCohortExpired(ctx, c)
	}
	return released, nil
}

// ListDevices returns devices decorated with their effective cohort.
func (s *Service) ListDevices(ctx context.Context, params models.ListDevicesParams) (models.PagedCohortDevices, error) {
	if s.store == nil {
		return models.PagedCohortDevices{}, fleeterror.NewInternalError("cohort store is not configured")
	}
	result, err := s.store.ListDevices(ctx, params)
	if err != nil {
		return models.PagedCohortDevices{}, err
	}
	for i := range result.Devices {
		s.hydrateFirmwareStatus(ctx, result.Devices[i].FirmwareStatus)
	}
	return result, nil
}

func (s *Service) hydrateCohortsFirmware(ctx context.Context, cohorts []*models.Cohort) {
	for _, cohort := range cohorts {
		s.hydrateCohortFirmware(ctx, cohort)
	}
}

func (s *Service) hydrateCohortFirmware(ctx context.Context, cohort *models.Cohort) {
	if cohort == nil {
		return
	}
	for i := range cohort.FirmwareStatuses {
		s.hydrateFirmwareStatus(ctx, &cohort.FirmwareStatuses[i])
	}
	for i := range cohort.Members {
		s.hydrateFirmwareStatus(ctx, cohort.Members[i].FirmwareStatus)
	}
	cohort.FirmwareProgress = cohortFirmwareProgress(cohort)
}

func (s *Service) hydrateFirmwareStatus(ctx context.Context, status *models.CohortFirmwareStatus) {
	if status == nil {
		return
	}
	status.TargetFirmwareFileID = strings.TrimSpace(status.TargetFirmwareFileID)
	status.TargetFirmwareVersion = strings.TrimSpace(status.TargetFirmwareVersion)
	status.CurrentFirmwareVersion = strings.TrimSpace(status.CurrentFirmwareVersion)
	status.DeviceStatus = strings.TrimSpace(status.DeviceStatus)

	if status.TargetFirmwareFileID != "" && s.firmwareMetadata != nil {
		if metadata, err := s.firmwareMetadata.GetFirmwareMetadata(status.TargetFirmwareFileID); err == nil {
			if version := strings.TrimSpace(metadata.FirmwareVersion); version != "" {
				status.TargetFirmwareVersion = version
			}
		}
	}
	status.State = deriveFirmwareRolloutState(status)
}

func cohortFirmwareProgress(cohort *models.Cohort) models.CohortFirmwareProgress {
	statuses := cohort.FirmwareStatuses
	if len(statuses) == 0 && len(cohort.Members) > 0 {
		statuses = make([]models.CohortFirmwareStatus, 0, len(cohort.Members))
		for _, member := range cohort.Members {
			if member.FirmwareStatus != nil {
				statuses = append(statuses, *member.FirmwareStatus)
			}
		}
	}
	var progress models.CohortFirmwareProgress
	for _, status := range statuses {
		if status.TargetFirmwareFileID == "" || status.State == models.CohortFirmwareRolloutStateNoTarget {
			continue
		}
		progress.TargetedCount++
		switch status.State {
		case models.CohortFirmwareRolloutStateNoTarget:
			continue
		case models.CohortFirmwareRolloutStateComplete:
			progress.CompleteCount++
		case models.CohortFirmwareRolloutStateQueued:
			progress.QueuedCount++
		case models.CohortFirmwareRolloutStateUpdating:
			progress.UpdatingCount++
		case models.CohortFirmwareRolloutStateVerifying:
			progress.VerifyingCount++
		case models.CohortFirmwareRolloutStateNeedsAttention:
			progress.NeedsAttentionCount++
		case models.CohortFirmwareRolloutStateUnknown:
			progress.UnknownCount++
		}
	}
	return progress
}

func deriveFirmwareRolloutState(status *models.CohortFirmwareStatus) models.CohortFirmwareRolloutState {
	if status == nil || status.TargetFirmwareFileID == "" {
		return models.CohortFirmwareRolloutStateNoTarget
	}
	if status.EnforcementState != nil && *status.EnforcementState == models.EnforcementStateDispatching {
		return models.CohortFirmwareRolloutStateUpdating
	}
	switch strings.ToUpper(status.DeviceStatus) {
	case "UPDATING", "REBOOT_REQUIRED":
		return models.CohortFirmwareRolloutStateUpdating
	}
	if status.CurrentFirmwareVersion != "" && status.CurrentFirmwareVersion == status.TargetFirmwareVersion {
		return models.CohortFirmwareRolloutStateComplete
	}
	if status.EnforcementState != nil {
		switch *status.EnforcementState {
		case models.EnforcementStateDispatched:
			return models.CohortFirmwareRolloutStateVerifying
		case models.EnforcementStateDispatching:
			return models.CohortFirmwareRolloutStateUpdating
		case models.EnforcementStateFailed, models.EnforcementStateHeld:
			return models.CohortFirmwareRolloutStateNeedsAttention
		case models.EnforcementStatePending, models.EnforcementStateDrifted:
			if status.RetryCount > 0 || strings.TrimSpace(ptrStringValue(status.LastError)) != "" {
				return models.CohortFirmwareRolloutStateNeedsAttention
			}
		case models.EnforcementStateConfirmed:
			return models.CohortFirmwareRolloutStateQueued
		}
	}
	if status.TargetFirmwareVersion == "" {
		return models.CohortFirmwareRolloutStateUnknown
	}
	if status.CurrentFirmwareVersion != "" && status.CurrentFirmwareVersion != status.TargetFirmwareVersion {
		return models.CohortFirmwareRolloutStateQueued
	}
	return models.CohortFirmwareRolloutStateQueued
}

func ptrStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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
	s.hydrateCohortFirmware(ctx, cohort)
	return cohort, nil
}

func validateUniqueDeviceIdentifiers(ids []string) error {
	seen := make(map[string]struct{}, len(ids))
	for i, id := range ids {
		id = strings.TrimSpace(id)
		ids[i] = id
		if id == "" {
			return fleeterror.NewInvalidArgumentErrorf("Miner %d is missing a device identifier.", i+1)
		}
		if _, ok := seen[id]; ok {
			return fleeterror.NewInvalidArgumentErrorf("Miner %q was selected more than once.", id)
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
			return "", "", fleeterror.NewInvalidArgumentErrorf("Cohort member %q is missing manufacturer or model information.", member.DeviceIdentifier)
		}
		if manufacturer == "" && model == "" {
			manufacturer = nextManufacturer
			model = nextModel
			continue
		}
		if !sameMinerType(nextManufacturer, manufacturer) || !sameMinerType(nextModel, model) {
			return "", "", fleeterror.NewInvalidArgumentError("Cohort members must have a single manufacturer and model.")
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

func sameMinerType(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
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
			return fleeterror.NewForbiddenErrorf("Device %q is leased by an ownerless cohort; admin access is required.", row.DeviceIdentifier)
		}
		if *row.OwnerUserID == params.ActorUserID {
			continue
		}
		owner := "another user"
		if row.OwnerUsername != nil && *row.OwnerUsername != "" {
			owner = *row.OwnerUsername
		}
		return fleeterror.NewForbiddenErrorf("Device %q is leased by %s.", row.DeviceIdentifier, owner)
	}
	return nil
}

func authorizeCohortOwnerMutation(cohort *models.Cohort, actorUserID int64, actorRole string) error {
	if cohort == nil {
		return nil
	}
	if cohort.IsDefault {
		return fleeterror.NewInvalidArgumentError("The default cohort cannot be changed through this action.")
	}
	if isSuperAdminRole(actorRole) {
		return nil
	}
	if cohort.OwnerUserID == nil {
		return fleeterror.NewForbiddenErrorf("Cohort %d is ownerless; admin access is required.", cohort.ID)
	}
	if *cohort.OwnerUserID == actorUserID {
		return nil
	}
	owner := "another user"
	if cohort.OwnerUsername != nil && *cohort.OwnerUsername != "" {
		owner = *cohort.OwnerUsername
	}
	return fleeterror.NewForbiddenErrorf("Cohort %d is leased by %s.", cohort.ID, owner)
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
	manufacturer := ""
	model := ""
	if params.Manufacturer != nil {
		manufacturer = *params.Manufacturer
	}
	if params.Model != nil {
		model = *params.Model
	}
	metadata["manufacturer"] = manufacturer
	metadata["model"] = model
	metadata["old_firmware_file_id"] = stringOrNil(cohortFirmwareFileIDForTarget(before, manufacturer, model))
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

func cohortHasDesiredConfig(cohort *models.Cohort) bool {
	return cohort != nil && len(cohort.DesiredConfigJSON) > 0
}

func isDefaultConfigOnlyUpdate(params models.UpdateCohortParams) bool {
	return (params.DesiredConfigJSONSet || params.ClearDesiredConfig) &&
		params.Label == nil && params.Purpose == nil && params.ExpiresAt == nil && !params.ClearExpiresAt
}

func (s *Service) validateDesiredConfig(ctx context.Context, orgID int64, config *models.CohortDesiredConfig, raw []byte) error {
	if config == nil && len(raw) > 0 {
		parsed, err := models.ParseCohortDesiredConfig(raw)
		if err != nil {
			return fleeterror.NewInvalidArgumentErrorf("Desired configuration is not valid: %v", err)
		}
		config = parsed
	}
	if config == nil || config.Pools == nil {
		return nil
	}
	if s.poolReferences == nil {
		return fleeterror.NewInternalError("cohort pool reference provider is not configured")
	}
	ids := []int64{config.Pools.PrimaryPoolID}
	if config.Pools.Backup1PoolID != nil {
		ids = append(ids, *config.Pools.Backup1PoolID)
	}
	if config.Pools.Backup2PoolID != nil {
		ids = append(ids, *config.Pools.Backup2PoolID)
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return fleeterror.NewInvalidArgumentError("Select a valid pool for every configured slot.")
		}
		if _, exists := seen[id]; exists {
			return fleeterror.NewInvalidArgumentError("Each pool slot must reference a different pool.")
		}
		seen[id] = struct{}{}
		if _, err := s.poolReferences.GetPool(ctx, orgID, id); err != nil {
			return fleeterror.NewInvalidArgumentErrorf("Pool %d is not an active pool in this organization.", id)
		}
	}
	return nil
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
