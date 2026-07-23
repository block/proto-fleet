// Package minerchannel is the domain layer for miner channel CRUD.
package minerchannel

import (
	"context"
	"fmt"
	"strings"
	"time"

	collectionpb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	poolpb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/minerchannel/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

// Service is the domain entry point for miner channel CRUD.
type Service struct {
	store                   interfaces.MinerChannelStore
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

// WithSourceDeviceSetResolver wires the group-to-miner channel bridge.
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

// NewService returns a miner channel service.
func NewService(store interfaces.MinerChannelStore, opts ...Option) *Service {
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

// CreateMinerChannel validates and inserts a miner channel plus explicit members.
func (s *Service) CreateMinerChannel(ctx context.Context, params models.CreateMinerChannelParams) (*models.MinerChannel, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("miner channel store is not configured")
	}
	params.Label = strings.TrimSpace(params.Label)
	params.Purpose = strings.TrimSpace(params.Purpose)
	if params.Label == "" {
		return nil, fleeterror.NewInvalidArgumentError("Enter a miner channel label.")
	}
	if params.Purpose == "" {
		return nil, fleeterror.NewInvalidArgumentError("Enter a miner channel purpose.")
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
			return nil, fleeterror.NewInternalError("miner channel source device-set resolver is not configured")
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
		return nil, fleeterror.NewInvalidArgumentError("Add at least one initial member to create a miner channel.")
	}
	if err := validateUniqueDeviceIdentifiers(params.DeviceIdentifiers); err != nil {
		return nil, err
	}

	created, err := s.store.CreateMinerChannel(ctx, params)
	if err != nil {
		return nil, err
	}
	if err := validateMinerChannelSingleMinerType(created); err != nil {
		return nil, err
	}
	s.hydrateMinerChannelFirmware(ctx, created)
	s.auditMinerChannelCreated(ctx, created)
	return created, nil
}

// GetMinerChannel returns a miner channel with explicit members.
func (s *Service) GetMinerChannel(ctx context.Context, orgID, minerChannelID int64) (*models.MinerChannel, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("miner channel store is not configured")
	}
	minerChannel, err := s.store.GetMinerChannel(ctx, orgID, minerChannelID)
	if err != nil {
		return nil, err
	}
	s.hydrateMinerChannelFirmware(ctx, minerChannel)
	return minerChannel, nil
}

// ListMinerChannels returns miner channels for an org.
func (s *Service) ListMinerChannels(ctx context.Context, params models.ListMinerChannelsParams) (models.PagedMinerChannels, error) {
	if s.store == nil {
		return models.PagedMinerChannels{}, fleeterror.NewInternalError("miner channel store is not configured")
	}
	result, err := s.store.ListMinerChannels(ctx, params)
	if err != nil {
		return models.PagedMinerChannels{}, err
	}
	s.hydrateMinerChannelsFirmware(ctx, result.MinerChannels)
	return result, nil
}

// ListMinerChannelsByOwner returns miner channels owned by a user.
func (s *Service) ListMinerChannelsByOwner(ctx context.Context, params models.ListMinerChannelsByOwnerParams) (models.PagedMinerChannels, error) {
	if s.store == nil {
		return models.PagedMinerChannels{}, fleeterror.NewInternalError("miner channel store is not configured")
	}
	result, err := s.store.ListMinerChannelsByOwner(ctx, params)
	if err != nil {
		return models.PagedMinerChannels{}, err
	}
	s.hydrateMinerChannelsFirmware(ctx, result.MinerChannels)
	return result, nil
}

// UpdateMinerChannel changes mutable miner channel metadata and desired state.
func (s *Service) UpdateMinerChannel(ctx context.Context, params models.UpdateMinerChannelParams) (*models.MinerChannel, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("miner channel store is not configured")
	}
	if params.Label != nil {
		trimmed := strings.TrimSpace(*params.Label)
		if trimmed == "" {
			return nil, fleeterror.NewInvalidArgumentError("Enter a miner channel label.")
		}
		params.Label = &trimmed
	}
	if params.Purpose != nil {
		trimmed := strings.TrimSpace(*params.Purpose)
		if trimmed == "" {
			return nil, fleeterror.NewInvalidArgumentError("Enter a miner channel purpose.")
		}
		params.Purpose = &trimmed
	}
	target, err := s.store.GetMinerChannel(ctx, params.OrgID, params.MinerChannelID)
	if err != nil {
		return nil, err
	}
	if target.IsDefault && !isDefaultConfigOnlyUpdate(params) {
		return nil, fleeterror.NewInvalidArgumentError("Set default miner channel firmware per manufacturer and model.")
	}
	if params.DesiredConfigJSONSet {
		if err := s.validateDesiredConfig(ctx, params.OrgID, params.DesiredConfig, params.DesiredConfigJSON); err != nil {
			return nil, err
		}
	}
	var updated *models.MinerChannel
	if target.IsDefault {
		updated, err = s.store.UpdateDefaultMinerChannelConfig(ctx, params)
	} else {
		updated, err = s.store.UpdateMinerChannel(ctx, params)
	}
	if err != nil {
		return nil, err
	}
	result := updated
	s.auditMinerChannelFieldsUpdated(ctx, target, result, params)
	s.hydrateMinerChannelFirmware(ctx, result)
	return result, nil
}

// SetMinerChannelFirmwareTarget sets or clears firmware for a miner channel manufacturer/model.
func (s *Service) SetMinerChannelFirmwareTarget(ctx context.Context, params models.SetMinerChannelFirmwareTargetParams) (*models.MinerChannel, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("miner channel store is not configured")
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
				formatMinerChannelMinerType(metadata.TargetManufacturer, metadata.TargetModel),
				formatMinerChannelMinerType(*params.Manufacturer, *params.Model),
			)
		}
	}
	if params.Manufacturer == nil {
		return nil, fleeterror.NewInvalidArgumentError("Select a product.")
	}
	if params.Model == nil {
		return nil, fleeterror.NewInvalidArgumentError("Select a model.")
	}
	target, err := s.store.GetMinerChannel(ctx, params.OrgID, params.MinerChannelID)
	if err != nil {
		return nil, err
	}
	if target.State != models.MinerChannelStateActive {
		return nil, fleeterror.NewInvalidArgumentError("This miner channel is not active.")
	}
	if target.IsDefault {
		if !isSuperAdminRole(params.ActorRole) {
			return nil, fleeterror.NewForbiddenError("Only super admins can update default miner channel firmware.")
		}
	} else if err := authorizeMinerChannelOwnerMutation(target, params.ActorUserID, params.ActorRole); err != nil {
		return nil, err
	}
	if !target.IsDefault {
		manufacturer, model, err := minerChannelSingleMinerType(target)
		if err != nil {
			return nil, err
		}
		if manufacturer == "" || model == "" {
			return nil, fleeterror.NewInvalidArgumentError("Add miner channel members before setting firmware.")
		}
		if !sameMinerType(manufacturer, *params.Manufacturer) || !sameMinerType(model, *params.Model) {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"Firmware target %s does not match miner channel miner type %s.",
				formatMinerChannelMinerType(*params.Manufacturer, *params.Model),
				formatMinerChannelMinerType(manufacturer, model),
			)
		}
	}
	updated, err := s.store.SetMinerChannelFirmwareTarget(ctx, params)
	if err != nil {
		return nil, err
	}
	s.auditMinerChannelFirmwareTargetUpdated(ctx, target, updated, params)
	s.hydrateMinerChannelFirmware(ctx, updated)
	return updated, nil
}

// AddDevicesToMinerChannel moves devices into the target miner channel after ownership checks.
func (s *Service) AddDevicesToMinerChannel(ctx context.Context, params models.MembershipMutationParams) (*models.MinerChannel, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("miner channel store is not configured")
	}
	if err := validateUniqueDeviceIdentifiers(params.DeviceIdentifiers); err != nil {
		return nil, err
	}
	target, err := s.store.GetMinerChannel(ctx, params.OrgID, params.MinerChannelID)
	if err != nil {
		return nil, err
	}
	if err := authorizeMinerChannelOwnerMutation(target, params.ActorUserID, params.ActorRole); err != nil {
		return nil, err
	}
	if err := s.authorizeDeviceMoves(ctx, params); err != nil {
		return nil, err
	}
	updated, err := s.store.MoveDevicesToMinerChannel(ctx, params)
	if err != nil {
		return nil, err
	}
	if err := validateMinerChannelSingleMinerType(updated); err != nil {
		return nil, err
	}
	s.auditMinerChannelMembersChanged(ctx, updated, "members_added", len(params.DeviceIdentifiers))
	s.hydrateMinerChannelFirmware(ctx, updated)
	return updated, nil
}

// RemoveDevicesFromMinerChannel releases explicit members from one miner channel back to default.
func (s *Service) RemoveDevicesFromMinerChannel(ctx context.Context, params models.MembershipMutationParams) (*models.MinerChannel, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("miner channel store is not configured")
	}
	if err := validateUniqueDeviceIdentifiers(params.DeviceIdentifiers); err != nil {
		return nil, err
	}
	minerChannel, err := s.store.GetMinerChannel(ctx, params.OrgID, params.MinerChannelID)
	if err != nil {
		return nil, err
	}
	if err := authorizeMinerChannelOwnerMutation(minerChannel, params.ActorUserID, params.ActorRole); err != nil {
		return nil, err
	}
	updated, err := s.store.RemoveDevicesAndGetMinerChannel(ctx, params)
	if err != nil {
		return nil, err
	}
	s.auditMinerChannelMembersChanged(ctx, updated, "members_removed", -len(params.DeviceIdentifiers))
	s.hydrateMinerChannelFirmware(ctx, updated)
	return updated, nil
}

// ReleaseMinerChannel releases all members back to default and marks the miner channel released.
func (s *Service) ReleaseMinerChannel(ctx context.Context, params models.MembershipMutationParams) (*models.MinerChannel, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("miner channel store is not configured")
	}
	minerChannel, err := s.store.GetMinerChannel(ctx, params.OrgID, params.MinerChannelID)
	if err != nil {
		return nil, err
	}
	if err := authorizeMinerChannelOwnerMutation(minerChannel, params.ActorUserID, params.ActorRole); err != nil {
		return nil, err
	}
	released, err := s.store.ReleaseMinerChannel(ctx, params.OrgID, params.MinerChannelID)
	if err != nil {
		return nil, err
	}
	s.auditMinerChannelReleased(ctx, released)
	s.hydrateMinerChannelFirmware(ctx, released)
	return released, nil
}

// SweepExpired releases expired active miner channels.
func (s *Service) SweepExpired(ctx context.Context) ([]*models.MinerChannel, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("miner channel store is not configured")
	}
	released, err := s.store.SweepExpiredMinerChannels(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range released {
		s.hydrateMinerChannelFirmware(ctx, c)
		s.auditMinerChannelExpired(ctx, c)
	}
	return released, nil
}

// ListDevices returns devices decorated with their effective miner channel.
func (s *Service) ListDevices(ctx context.Context, params models.ListDevicesParams) (models.PagedMinerChannelDevices, error) {
	if s.store == nil {
		return models.PagedMinerChannelDevices{}, fleeterror.NewInternalError("miner channel store is not configured")
	}
	result, err := s.store.ListDevices(ctx, params)
	if err != nil {
		return models.PagedMinerChannelDevices{}, err
	}
	for i := range result.Devices {
		s.hydrateFirmwareStatus(ctx, result.Devices[i].FirmwareStatus)
	}
	return result, nil
}

func (s *Service) hydrateMinerChannelsFirmware(ctx context.Context, minerChannels []*models.MinerChannel) {
	for _, minerChannel := range minerChannels {
		s.hydrateMinerChannelFirmware(ctx, minerChannel)
	}
}

func (s *Service) hydrateMinerChannelFirmware(ctx context.Context, minerChannel *models.MinerChannel) {
	if minerChannel == nil {
		return
	}
	for i := range minerChannel.FirmwareStatuses {
		s.hydrateFirmwareStatus(ctx, &minerChannel.FirmwareStatuses[i])
	}
	for i := range minerChannel.Members {
		s.hydrateFirmwareStatus(ctx, minerChannel.Members[i].FirmwareStatus)
	}
	minerChannel.FirmwareProgress = minerChannelFirmwareProgress(minerChannel)
}

func (s *Service) hydrateFirmwareStatus(ctx context.Context, status *models.MinerChannelFirmwareStatus) {
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

func minerChannelFirmwareProgress(minerChannel *models.MinerChannel) models.MinerChannelFirmwareProgress {
	statuses := minerChannel.FirmwareStatuses
	if len(statuses) == 0 && len(minerChannel.Members) > 0 {
		statuses = make([]models.MinerChannelFirmwareStatus, 0, len(minerChannel.Members))
		for _, member := range minerChannel.Members {
			if member.FirmwareStatus != nil {
				statuses = append(statuses, *member.FirmwareStatus)
			}
		}
	}
	var progress models.MinerChannelFirmwareProgress
	for _, status := range statuses {
		if status.TargetFirmwareFileID == "" || status.State == models.MinerChannelFirmwareRolloutStateNoTarget {
			continue
		}
		progress.TargetedCount++
		switch status.State {
		case models.MinerChannelFirmwareRolloutStateNoTarget:
			continue
		case models.MinerChannelFirmwareRolloutStateComplete:
			progress.CompleteCount++
		case models.MinerChannelFirmwareRolloutStateQueued:
			progress.QueuedCount++
		case models.MinerChannelFirmwareRolloutStateUpdating:
			progress.UpdatingCount++
		case models.MinerChannelFirmwareRolloutStateVerifying:
			progress.VerifyingCount++
		case models.MinerChannelFirmwareRolloutStateNeedsAttention:
			progress.NeedsAttentionCount++
		case models.MinerChannelFirmwareRolloutStateUnknown:
			progress.UnknownCount++
		}
	}
	return progress
}

func deriveFirmwareRolloutState(status *models.MinerChannelFirmwareStatus) models.MinerChannelFirmwareRolloutState {
	if status == nil || status.TargetFirmwareFileID == "" {
		return models.MinerChannelFirmwareRolloutStateNoTarget
	}
	if status.EnforcementState != nil && *status.EnforcementState == models.EnforcementStateDispatching {
		return models.MinerChannelFirmwareRolloutStateUpdating
	}
	switch strings.ToUpper(status.DeviceStatus) {
	case "UPDATING", "REBOOT_REQUIRED":
		return models.MinerChannelFirmwareRolloutStateUpdating
	}
	if status.CurrentFirmwareVersion != "" && status.CurrentFirmwareVersion == status.TargetFirmwareVersion {
		return models.MinerChannelFirmwareRolloutStateComplete
	}
	if status.EnforcementState != nil {
		switch *status.EnforcementState {
		case models.EnforcementStateDispatched:
			return models.MinerChannelFirmwareRolloutStateVerifying
		case models.EnforcementStateDispatching:
			return models.MinerChannelFirmwareRolloutStateUpdating
		case models.EnforcementStateFailed, models.EnforcementStateHeld:
			return models.MinerChannelFirmwareRolloutStateNeedsAttention
		case models.EnforcementStatePending, models.EnforcementStateDrifted:
			if status.RetryCount > 0 || strings.TrimSpace(ptrStringValue(status.LastError)) != "" {
				return models.MinerChannelFirmwareRolloutStateNeedsAttention
			}
		case models.EnforcementStateConfirmed:
			return models.MinerChannelFirmwareRolloutStateQueued
		}
	}
	if status.TargetFirmwareVersion == "" {
		return models.MinerChannelFirmwareRolloutStateUnknown
	}
	if status.CurrentFirmwareVersion != "" && status.CurrentFirmwareVersion != status.TargetFirmwareVersion {
		return models.MinerChannelFirmwareRolloutStateQueued
	}
	return models.MinerChannelFirmwareRolloutStateQueued
}

func ptrStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// DeleteMinerChannel soft-deletes a miner channel by releasing it and clearing memberships.
func (s *Service) DeleteMinerChannel(ctx context.Context, orgID, minerChannelID int64) (*models.MinerChannel, error) {
	if s.store == nil {
		return nil, fleeterror.NewInternalError("miner channel store is not configured")
	}
	minerChannel, err := s.store.ReleaseMinerChannel(ctx, orgID, minerChannelID)
	if err != nil {
		return nil, err
	}
	s.auditMinerChannelDeleted(ctx, minerChannel)
	s.hydrateMinerChannelFirmware(ctx, minerChannel)
	return minerChannel, nil
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

func validateMinerChannelSingleMinerType(minerChannel *models.MinerChannel) error {
	_, _, err := minerChannelSingleMinerType(minerChannel)
	return err
}

func minerChannelSingleMinerType(minerChannel *models.MinerChannel) (string, string, error) {
	if minerChannel == nil || len(minerChannel.Members) == 0 {
		return "", "", nil
	}
	var manufacturer string
	var model string
	for _, member := range minerChannel.Members {
		nextManufacturer := strings.TrimSpace(member.Display.Manufacturer)
		nextModel := strings.TrimSpace(member.Display.Model)
		if nextManufacturer == "" || nextModel == "" {
			return "", "", fleeterror.NewInvalidArgumentErrorf("MinerChannel member %q is missing manufacturer or model information.", member.DeviceIdentifier)
		}
		if manufacturer == "" && model == "" {
			manufacturer = nextManufacturer
			model = nextModel
			continue
		}
		if !sameMinerType(nextManufacturer, manufacturer) || !sameMinerType(nextModel, model) {
			return "", "", fleeterror.NewInvalidArgumentError("MinerChannel members must have a single manufacturer and model.")
		}
	}
	return manufacturer, model, nil
}

func formatMinerChannelMinerType(manufacturer, model string) string {
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
	ownership, err := s.store.ListMinerChannelDeviceOwnership(ctx, params.OrgID, params.DeviceIdentifiers)
	if err != nil {
		return err
	}
	for _, row := range ownership {
		if row.MinerChannelID == params.MinerChannelID {
			continue
		}
		if isSuperAdminRole(params.ActorRole) {
			continue
		}
		if row.OwnerUserID == nil {
			return fleeterror.NewForbiddenErrorf("Device %q is leased by an ownerless miner channel; admin access is required.", row.DeviceIdentifier)
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

func authorizeMinerChannelOwnerMutation(minerChannel *models.MinerChannel, actorUserID int64, actorRole string) error {
	if minerChannel == nil {
		return nil
	}
	if minerChannel.IsDefault {
		return fleeterror.NewInvalidArgumentError("The default miner channel cannot be changed through this action.")
	}
	if isSuperAdminRole(actorRole) {
		return nil
	}
	if minerChannel.OwnerUserID == nil {
		return fleeterror.NewForbiddenErrorf("MinerChannel %d is ownerless; admin access is required.", minerChannel.ID)
	}
	if *minerChannel.OwnerUserID == actorUserID {
		return nil
	}
	owner := "another user"
	if minerChannel.OwnerUsername != nil && *minerChannel.OwnerUsername != "" {
		owner = *minerChannel.OwnerUsername
	}
	return fleeterror.NewForbiddenErrorf("MinerChannel %d is leased by %s.", minerChannel.ID, owner)
}

func isSuperAdminRole(role string) bool {
	switch strings.ToUpper(strings.TrimSpace(role)) {
	case "SUPER_ADMIN":
		return true
	default:
		return false
	}
}

func (s *Service) auditMinerChannelCreated(ctx context.Context, minerChannel *models.MinerChannel) {
	if minerChannel == nil {
		return
	}
	orgID := minerChannel.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           activityTypeCreated,
		OrganizationID: &orgID,
		Description:    fmt.Sprintf("Created miner channel %q (id=%d)", minerChannel.Label, minerChannel.ID),
		Metadata: map[string]any{
			"miner_channel_id": minerChannel.ID,
			"label":            minerChannel.Label,
		},
	}
	activity.StampActor(ctx, &event)
	s.audit.Log(ctx, event)
}

func (s *Service) auditMinerChannelDeleted(ctx context.Context, minerChannel *models.MinerChannel) {
	if minerChannel == nil {
		return
	}
	orgID := minerChannel.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           activityTypeDeleted,
		OrganizationID: &orgID,
		Description:    fmt.Sprintf("Deleted miner channel %q (id=%d)", minerChannel.Label, minerChannel.ID),
		Metadata: map[string]any{
			"miner_channel_id": minerChannel.ID,
			"label":            minerChannel.Label,
		},
	}
	activity.StampActor(ctx, &event)
	s.audit.Log(ctx, event)
}

func (s *Service) auditMinerChannelReleased(ctx context.Context, minerChannel *models.MinerChannel) {
	if minerChannel == nil {
		return
	}
	orgID := minerChannel.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           activityTypeReleased,
		OrganizationID: &orgID,
		Description:    fmt.Sprintf("Released miner channel %q (id=%d)", minerChannel.Label, minerChannel.ID),
		Metadata: map[string]any{
			"miner_channel_id": minerChannel.ID,
			"label":            minerChannel.Label,
		},
	}
	activity.StampActor(ctx, &event)
	s.audit.Log(ctx, event)
}

func (s *Service) auditMinerChannelExpired(ctx context.Context, minerChannel *models.MinerChannel) {
	if minerChannel == nil {
		return
	}
	orgID := minerChannel.OrgID
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           activityTypeExpired,
		OrganizationID: &orgID,
		Description:    fmt.Sprintf("Expired miner channel %q (id=%d)", minerChannel.Label, minerChannel.ID),
		Metadata: map[string]any{
			"miner_channel_id": minerChannel.ID,
			"label":            minerChannel.Label,
		},
	}
	activity.StampActor(ctx, &event)
	s.audit.Log(ctx, event)
}

func (s *Service) auditMinerChannelFieldsUpdated(ctx context.Context, before, after *models.MinerChannel, params models.UpdateMinerChannelParams) {
	minerChannel := after
	if minerChannel == nil {
		minerChannel = before
	}
	if minerChannel == nil {
		return
	}

	metadata := minerChannelUpdateMetadata(minerChannel, "miner_channel_fields_updated")
	changedFields := make([]string, 0, 5)
	if params.Label != nil {
		changedFields = append(changedFields, "label")
	}
	if params.Purpose != nil {
		changedFields = append(changedFields, "purpose")
	}
	if params.ExpiresAt != nil || params.ClearExpiresAt {
		changedFields = append(changedFields, "expires_at")
		metadata["old_expires_at"] = timeOrNil(minerChannelExpiresAt(before))
		metadata["new_expires_at"] = timeOrNil(minerChannelExpiresAt(after))
	}
	if params.DesiredConfigJSONSet || params.ClearDesiredConfig {
		changedFields = append(changedFields, "desired_config")
		metadata["desired_config_changed"] = params.DesiredConfigJSONSet
		metadata["desired_config_cleared"] = params.ClearDesiredConfig
		metadata["old_desired_config_present"] = minerChannelHasDesiredConfig(before)
		metadata["new_desired_config_present"] = minerChannelHasDesiredConfig(after)
	}
	metadata["changed_fields"] = changedFields
	s.auditMinerChannelUpdated(ctx, minerChannel, nil, fmt.Sprintf("Updated miner channel %q (id=%d)", minerChannel.Label, minerChannel.ID), metadata)
}

func (s *Service) auditMinerChannelFirmwareTargetUpdated(
	ctx context.Context,
	before *models.MinerChannel,
	after *models.MinerChannel,
	params models.SetMinerChannelFirmwareTargetParams,
) {
	minerChannel := after
	if minerChannel == nil {
		minerChannel = before
	}
	if minerChannel == nil {
		return
	}
	metadata := minerChannelUpdateMetadata(minerChannel, "firmware_target_updated")
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
	metadata["old_firmware_file_id"] = stringOrNil(minerChannelFirmwareFileIDForTarget(before, manufacturer, model))
	metadata["new_firmware_file_id"] = stringOrNil(params.FirmwareFileID)
	s.auditMinerChannelUpdated(ctx, minerChannel, nil, fmt.Sprintf("Updated miner channel firmware target for %q (id=%d)", minerChannel.Label, minerChannel.ID), metadata)
}

func (s *Service) auditMinerChannelMembersChanged(ctx context.Context, minerChannel *models.MinerChannel, updateKind string, memberCountDelta int) {
	if minerChannel == nil {
		return
	}
	affectedCount := memberCountDelta
	if affectedCount < 0 {
		affectedCount = -affectedCount
	}
	metadata := minerChannelUpdateMetadata(minerChannel, updateKind)
	metadata["affected_member_count"] = affectedCount
	metadata["member_count_delta"] = memberCountDelta
	s.auditMinerChannelUpdated(ctx, minerChannel, &affectedCount, fmt.Sprintf("Updated miner channel members for %q (id=%d)", minerChannel.Label, minerChannel.ID), metadata)
}

func (s *Service) auditMinerChannelUpdated(ctx context.Context, minerChannel *models.MinerChannel, scopeCount *int, description string, metadata map[string]any) {
	if minerChannel == nil {
		return
	}
	orgID := minerChannel.OrgID
	scopeType := "miner_channel"
	scopeLabel := minerChannel.Label
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

func minerChannelUpdateMetadata(minerChannel *models.MinerChannel, updateKind string) map[string]any {
	return map[string]any{
		"miner_channel_id": minerChannel.ID,
		"label":            minerChannel.Label,
		"is_default":       minerChannel.IsDefault,
		"update_kind":      updateKind,
	}
}

func minerChannelExpiresAt(minerChannel *models.MinerChannel) *time.Time {
	if minerChannel == nil {
		return nil
	}
	return minerChannel.ExpiresAt
}

func minerChannelHasDesiredConfig(minerChannel *models.MinerChannel) bool {
	return minerChannel != nil && len(minerChannel.DesiredConfigJSON) > 0
}

func isDefaultConfigOnlyUpdate(params models.UpdateMinerChannelParams) bool {
	return (params.DesiredConfigJSONSet || params.ClearDesiredConfig) &&
		params.Label == nil && params.Purpose == nil && params.ExpiresAt == nil && !params.ClearExpiresAt
}

func (s *Service) validateDesiredConfig(ctx context.Context, orgID int64, config *models.MinerChannelDesiredConfig, raw []byte) error {
	if config == nil && len(raw) > 0 {
		parsed, err := models.ParseMinerChannelDesiredConfig(raw)
		if err != nil {
			return fleeterror.NewInvalidArgumentErrorf("Desired configuration is not valid: %v", err)
		}
		config = parsed
	}
	if config == nil || config.Pools == nil {
		return nil
	}
	if s.poolReferences == nil {
		return fleeterror.NewInternalError("miner channel pool reference provider is not configured")
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

func minerChannelFirmwareFileIDForTarget(minerChannel *models.MinerChannel, manufacturer, model string) *string {
	if minerChannel == nil {
		return nil
	}
	for _, target := range minerChannel.FirmwareTargets {
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
