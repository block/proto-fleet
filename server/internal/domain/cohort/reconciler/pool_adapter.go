package reconciler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/command"
	minerinterfaces "github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/workername"
	"github.com/block/proto-fleet/server/sdk/v1"
)

type CohortPoolReferenceProvider interface {
	GetCohortPoolReference(ctx context.Context, orgID, poolID int64) (models.CohortPoolReference, error)
}

type PoolMinerProvider interface {
	GetMinerFromDeviceIdentifier(ctx context.Context, deviceID minermodels.DeviceIdentifier) (minerinterfaces.Miner, error)
}

type PoolCapabilitiesProvider interface {
	GetRawCapabilitiesForDevice(ctx context.Context, driverName, manufacturer, model string) sdk.Capabilities
}

type PoolCommandDispatcher interface {
	UpdateMiningPoolsAsCohort(ctx context.Context, selector *commandpb.DeviceSelector, defaultPool, backup1Pool, backup2Pool *commandpb.PoolSlotConfig) (*command.CommandResult, error)
}

type PoolAdapter struct {
	pools    CohortPoolReferenceProvider
	miners   PoolMinerProvider
	caps     PoolCapabilitiesProvider
	commands PoolCommandDispatcher
}

func NewPoolAdapter(pools CohortPoolReferenceProvider, miners PoolMinerProvider, caps PoolCapabilitiesProvider, commands PoolCommandDispatcher) *PoolAdapter {
	return &PoolAdapter{pools: pools, miners: miners, caps: caps, commands: commands}
}

func (*PoolAdapter) Dimension() models.CohortConfigDimension {
	return models.CohortConfigDimensionPools
}

func (*PoolAdapter) Policy() ConfigDimensionPolicy {
	return ConfigDimensionPolicy{
		ObservationMaxAge:  defaultConfigObservationMaxAge,
		RedispatchCooldown: defaultConfigRedispatchCooldown,
		MaxRetries:         defaultMaxRetries,
	}
}

func (*PoolAdapter) HasDesiredState(config *models.CohortDesiredConfig) bool {
	return config != nil && config.Pools != nil
}

func (a *PoolAdapter) Supported(ctx context.Context, c models.ConfigEnforcementCandidate) bool {
	if a.caps == nil {
		return false
	}
	caps := a.caps.GetRawCapabilitiesForDevice(ctx, c.DriverName, c.Manufacturer, c.Model)
	return caps[sdk.CapabilityGetMiningPools] && caps[sdk.CapabilityPoolConfig]
}

type normalizedPool struct {
	Priority int32  `json:"priority"`
	URL      string `json:"url"`
	Username string `json:"username"`
}

type poolDesiredValue struct {
	PrimaryPoolID int64
	Backup1PoolID *int64
	Backup2PoolID *int64
}

type desiredPoolSlot struct {
	ID       int64
	Priority int32
}

func (a *PoolAdapter) Desired(ctx context.Context, c models.ConfigEnforcementCandidate) (DesiredDimensionState, error) {
	if !a.HasDesiredState(c.DesiredConfig) || a.pools == nil {
		return DesiredDimensionState{}, nil
	}
	slots := desiredPoolSlots(c.DesiredConfig.Pools)
	normalized := make([]normalizedPool, 0, len(slots))
	revisionParts := make([]any, 0, len(slots))
	for _, slot := range slots {
		pool, err := a.pools.GetCohortPoolReference(ctx, c.OrgID, slot.ID)
		if err != nil {
			return DesiredDimensionState{}, err
		}
		username := workername.EffectivePoolUsername(pool.Username, c.WorkerName, !strings.Contains(strings.TrimSpace(pool.Username), "."))
		normalized = append(normalized, normalizedPool{Priority: slot.Priority, URL: strings.TrimSpace(pool.URL), Username: username})
		revisionParts = append(revisionParts, struct {
			ID        int64          `json:"id"`
			UpdatedAt int64          `json:"updated_at_unix_nano"`
			Pool      normalizedPool `json:"pool"`
		}{ID: pool.ID, UpdatedAt: pool.UpdatedAt.UnixNano(), Pool: normalized[len(normalized)-1]})
	}
	desiredPools := c.DesiredConfig.Pools
	return DesiredDimensionState{
		ComparableHash: hashJSON(normalized),
		RevisionHash: hashJSON(struct {
			Pools      []any  `json:"pools"`
			WorkerName string `json:"worker_name"`
		}{revisionParts, c.WorkerName}),
		Value: poolDesiredValue{
			PrimaryPoolID: desiredPools.PrimaryPoolID,
			Backup1PoolID: desiredPools.Backup1PoolID,
			Backup2PoolID: desiredPools.Backup2PoolID,
		},
	}, nil
}

func (a *PoolAdapter) Observe(ctx context.Context, c models.ConfigEnforcementCandidate) (ObservedDimensionState, error) {
	if a.miners == nil {
		return ObservedDimensionState{}, fmt.Errorf("pool miner provider is not configured")
	}
	miner, err := a.miners.GetMinerFromDeviceIdentifier(ctx, minermodels.DeviceIdentifier(c.DeviceIdentifier))
	if err != nil {
		return ObservedDimensionState{}, err
	}
	pools, err := miner.GetMiningPools(ctx)
	if err != nil {
		return ObservedDimensionState{}, err
	}
	normalized := normalizeObservedPools(pools)
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return ObservedDimensionState{}, fmt.Errorf("marshal observed pools: %w", err)
	}
	return ObservedDimensionState{NormalizedJSON: encoded, ComparableHash: hashJSON(normalized)}, nil
}

func (a *PoolAdapter) Dispatch(ctx context.Context, c models.ConfigEnforcementCandidate, desired DesiredDimensionState) (*command.CommandResult, error) {
	if a.commands == nil {
		return nil, fmt.Errorf("pool command dispatcher is not configured")
	}
	value, ok := desired.Value.(poolDesiredValue)
	if !ok || value.PrimaryPoolID <= 0 {
		return nil, fmt.Errorf("pool desired state has no primary pool")
	}
	selector := &commandpb.DeviceSelector{SelectionType: &commandpb.DeviceSelector_IncludeDevices{
		IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{c.DeviceIdentifier}},
	}}
	return a.commands.UpdateMiningPoolsAsCohort(
		ctx,
		selector,
		&commandpb.PoolSlotConfig{PoolSource: &commandpb.PoolSlotConfig_PoolId{PoolId: value.PrimaryPoolID}},
		optionalPoolSlot(value.Backup1PoolID),
		optionalPoolSlot(value.Backup2PoolID),
	)
}

func desiredPoolSlots(config *models.CohortPoolDesiredConfig) []desiredPoolSlot {
	if config == nil {
		return nil
	}
	slots := []desiredPoolSlot{{ID: config.PrimaryPoolID, Priority: 0}}
	if config.Backup1PoolID != nil {
		slots = append(slots, desiredPoolSlot{ID: *config.Backup1PoolID, Priority: 1})
	}
	if config.Backup2PoolID != nil {
		slots = append(slots, desiredPoolSlot{ID: *config.Backup2PoolID, Priority: 2})
	}
	return slots
}

func normalizeObservedPools(pools []minerinterfaces.MinerConfiguredPool) []normalizedPool {
	normalized := make([]normalizedPool, 0, len(pools))
	for _, pool := range pools {
		normalized = append(normalized, normalizedPool{Priority: pool.Priority, URL: strings.TrimSpace(pool.URL), Username: strings.TrimSpace(pool.Username)})
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Priority < normalized[j].Priority })
	return normalized
}

func optionalPoolSlot(id *int64) *commandpb.PoolSlotConfig {
	if id == nil {
		return nil
	}
	return &commandpb.PoolSlotConfig{PoolSource: &commandpb.PoolSlotConfig_PoolId{PoolId: *id}}
}

func hashJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
