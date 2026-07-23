package interfaces

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/minerchannel/models"
)

//go:generate go run go.uber.org/mock/mockgen -source=miner_channel.go -destination=mocks/mock_miner_channel_store.go -package=mocks MinerChannelStore

// MinerChannelStore is the persistence boundary for the miner channels domain.
//
//nolint:interfacebloat // MinerChannel membership, lifecycle, and ownership queries form one transactional domain boundary.
type MinerChannelStore interface {
	CreateMinerChannel(ctx context.Context, params models.CreateMinerChannelParams) (*models.MinerChannel, error)
	GetMinerChannel(ctx context.Context, orgID, minerChannelID int64) (*models.MinerChannel, error)
	ListMinerChannels(ctx context.Context, params models.ListMinerChannelsParams) (models.PagedMinerChannels, error)
	ListMinerChannelsByOwner(ctx context.Context, params models.ListMinerChannelsByOwnerParams) (models.PagedMinerChannels, error)
	UpdateMinerChannel(ctx context.Context, params models.UpdateMinerChannelParams) (*models.MinerChannel, error)
	UpdateDefaultMinerChannelConfig(ctx context.Context, params models.UpdateMinerChannelParams) (*models.MinerChannel, error)
	SetMinerChannelFirmwareTarget(ctx context.Context, params models.SetMinerChannelFirmwareTargetParams) (*models.MinerChannel, error)
	MoveDevicesToMinerChannel(ctx context.Context, params models.MembershipMutationParams) (*models.MinerChannel, error)
	RemoveDevicesAndGetMinerChannel(ctx context.Context, params models.MembershipMutationParams) (*models.MinerChannel, error)
	ReleaseMinerChannel(ctx context.Context, orgID, minerChannelID int64) (*models.MinerChannel, error)
	SweepExpiredMinerChannels(ctx context.Context) ([]*models.MinerChannel, error)

	InsertMinerChannelMember(ctx context.Context, params models.InsertMinerChannelMemberParams) error
	DeleteMinerChannelMemberships(ctx context.Context, orgID, minerChannelID int64, deviceIdentifiers []string) (int64, error)
	ListMinerChannelMembers(ctx context.Context, orgID, minerChannelID int64) ([]models.MinerChannelMember, error)
	ResolveEffectiveMinerChannelForDevice(ctx context.Context, orgID int64, deviceIdentifier string) (*models.MinerChannel, error)
	ListDefaultMinerChannelDevices(ctx context.Context, orgID int64) ([]models.DefaultMinerChannelDevice, error)
	ListMinerChannelDeviceOwnership(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]models.MinerChannelDeviceOwnership, error)
	ListActiveOwnedMinerChannelMemberships(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]models.MinerChannelDeviceOwnership, error)
	ListDevices(ctx context.Context, params models.ListDevicesParams) (models.PagedMinerChannelDevices, error)
}

//nolint:interfacebloat // Firmware lifecycle transitions form one persistence boundary.
type MinerChannelFirmwareEnforcementStore interface {
	ListOrgsWithFirmwareTargets(ctx context.Context) ([]int64, error)
	ListFirmwareEnforcementCandidates(ctx context.Context, orgID int64) ([]models.FirmwareEnforcementCandidate, error)
	ClearMissingFirmwareTarget(ctx context.Context, orgID int64, firmwareFileID string) (int64, error)
	ClaimFirmwareDispatch(ctx context.Context, params models.ClaimFirmwareDispatchParams) (bool, error)
	MarkFirmwareDispatched(ctx context.Context, params models.MarkFirmwareDispatchedParams) (bool, error)
	MarkFirmwareConfirmed(ctx context.Context, params models.MarkFirmwareConfirmedParams) (bool, error)
	MarkFirmwareDrifted(ctx context.Context, params models.MarkFirmwareDriftedParams) (bool, error)
	MarkFirmwareDispatchFailure(ctx context.Context, params models.MarkFirmwareDispatchFailureParams) (bool, error)
	MarkFirmwareDispatchHeld(ctx context.Context, params models.MarkFirmwareDispatchHeldParams) (bool, error)
	IsCommandBatchFinished(ctx context.Context, batchUUID string) (bool, error)
	UpsertMinerChannelReconcilerHeartbeat(ctx context.Context, lastTickAt time.Time, lastTickUUID uuid.UUID, durationMS *int32, activeDeviceCount int32) error
}

// MinerChannelConfigEnforcementStore is the dimension-agnostic persistence boundary
// shared by ordinary miner channel configuration adapters.
//
//nolint:interfacebloat // Generic config lifecycle transitions form one persistence boundary.
type MinerChannelConfigEnforcementStore interface {
	ListOrgsWithDesiredConfig(ctx context.Context) ([]int64, error)
	ListConfigEnforcementCandidates(ctx context.Context, orgID int64, dimension models.MinerChannelConfigDimension) ([]models.ConfigEnforcementCandidate, error)
	UpsertDeviceConfigState(ctx context.Context, params models.UpsertDeviceConfigStateParams) error
	UpsertConfigSupport(ctx context.Context, params models.ConfigEnforcementMutationParams) error
	ClaimConfigDispatch(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error)
	MarkConfigDispatched(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error)
	MarkConfigConfirmed(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error)
	MarkConfigDrifted(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error)
	MarkConfigDispatchFailure(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error)
	MarkConfigDispatchHeld(ctx context.Context, params models.ConfigEnforcementMutationParams) (bool, error)
	IsCommandBatchFinished(ctx context.Context, batchUUID string) (bool, error)
}
