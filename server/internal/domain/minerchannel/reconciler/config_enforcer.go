package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/minerchannel/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	defaultConfigObservationInterval = 5 * time.Minute
	defaultConfigObservationMaxAge   = 15 * time.Minute
	defaultConfigRedispatchCooldown  = 5 * time.Minute
	defaultConfigDeviceTimeout       = 30 * time.Second
	defaultConfigObserverConcurrency = 8
)

type DesiredDimensionState struct {
	ComparableHash string
	RevisionHash   string
	Value          any
}

type ObservedDimensionState struct {
	NormalizedJSON []byte
	ComparableHash string
}

type ConfigDimensionPolicy struct {
	ObservationMaxAge  time.Duration
	RedispatchCooldown time.Duration
	MaxRetries         int32
}

type ConfigDimensionAdapter interface {
	Dimension() models.MinerChannelConfigDimension
	HasDesiredState(config *models.MinerChannelDesiredConfig) bool
	Policy() ConfigDimensionPolicy
	Supported(ctx context.Context, candidate models.ConfigEnforcementCandidate) bool
	Desired(ctx context.Context, candidate models.ConfigEnforcementCandidate) (DesiredDimensionState, error)
	Observe(ctx context.Context, candidate models.ConfigEnforcementCandidate) (ObservedDimensionState, error)
	Dispatch(ctx context.Context, candidate models.ConfigEnforcementCandidate, desired DesiredDimensionState) (*command.CommandResult, error)
}

type ConfigEnforcer struct {
	store          interfaces.MinerChannelConfigEnforcementStore
	adapters       []ConfigDimensionAdapter
	now            func() time.Time
	maxRetries     int32
	maxAge         time.Duration
	cooldown       time.Duration
	deviceTimeout  time.Duration
	observerLimit  int
	dispatchBefore time.Duration
}

func NewConfigEnforcer(store interfaces.MinerChannelConfigEnforcementStore, adapters ...ConfigDimensionAdapter) *ConfigEnforcer {
	return &ConfigEnforcer{
		store: store, adapters: adapters, now: time.Now, maxRetries: defaultMaxRetries,
		maxAge: defaultConfigObservationMaxAge, cooldown: defaultConfigRedispatchCooldown,
		deviceTimeout: defaultConfigDeviceTimeout, observerLimit: defaultConfigObserverConcurrency,
		dispatchBefore: defaultDispatchingTimeout,
	}
}

func (e *ConfigEnforcer) Observe(ctx context.Context) {
	e.forEachCandidate(ctx, func(adapter ConfigDimensionAdapter, candidate models.ConfigEnforcementCandidate) {
		if !adapter.HasDesiredState(candidate.DesiredConfig) {
			return
		}
		deviceCtx, cancel := context.WithTimeout(ctx, e.deviceTimeout)
		defer cancel()
		desired, err := adapter.Desired(deviceCtx, candidate)
		if err != nil || desired.RevisionHash == "" {
			return
		}
		supported := adapter.Supported(deviceCtx, candidate)
		if err := e.store.UpsertConfigSupport(deviceCtx, models.ConfigEnforcementMutationParams{
			OrgID: candidate.OrgID, DeviceIdentifier: candidate.DeviceIdentifier, Dimension: adapter.Dimension(),
			DesiredStateHash: desired.RevisionHash, Supported: supported,
		}); err != nil {
			slog.Error("miner channel config support persistence failed", "dimension", adapter.Dimension(), "device", candidate.DeviceIdentifier, "error", err)
			return
		}
		if !supported {
			return
		}
		observed, err := adapter.Observe(deviceCtx, candidate)
		if err != nil {
			slog.Warn("miner channel config observer failed", "dimension", adapter.Dimension(), "device", candidate.DeviceIdentifier, "error", err)
			return
		}
		if len(observed.NormalizedJSON) == 0 || observed.ComparableHash == "" {
			return
		}
		if err := e.store.UpsertDeviceConfigState(deviceCtx, models.UpsertDeviceConfigStateParams{
			OrgID: candidate.OrgID, DeviceIdentifier: candidate.DeviceIdentifier, Dimension: adapter.Dimension(),
			ObservedStateJSON: observed.NormalizedJSON, ObservedStateHash: observed.ComparableHash, ObservedAt: e.now(),
		}); err != nil {
			slog.Error("miner channel config observer persistence failed", "dimension", adapter.Dimension(), "device", candidate.DeviceIdentifier, "error", err)
		}
	})
}

func (e *ConfigEnforcer) Reconcile(ctx context.Context) {
	e.forEachCandidate(ctx, func(adapter ConfigDimensionAdapter, candidate models.ConfigEnforcementCandidate) {
		e.reconcileCandidate(ctx, adapter, candidate)
	})
}

func (e *ConfigEnforcer) forEachCandidate(ctx context.Context, fn func(ConfigDimensionAdapter, models.ConfigEnforcementCandidate)) {
	if e == nil || e.store == nil || len(e.adapters) == 0 {
		return
	}
	orgIDs, err := e.store.ListOrgsWithDesiredConfig(ctx)
	if err != nil {
		slog.Error("miner channel config enforcement failed to list organizations", "error", err)
		return
	}
	sem := make(chan struct{}, e.observerLimit)
	var wg sync.WaitGroup
	for _, orgID := range orgIDs {
		for _, adapter := range e.adapters {
			candidates, err := e.store.ListConfigEnforcementCandidates(ctx, orgID, adapter.Dimension())
			if err != nil {
				slog.Error("miner channel config enforcement failed to list candidates", "org_id", orgID, "dimension", adapter.Dimension(), "error", err)
				continue
			}
			for _, candidate := range candidates {
				if !adapter.HasDesiredState(candidate.DesiredConfig) {
					continue
				}
				wg.Add(1)
				sem <- struct{}{}
				go func() {
					defer wg.Done()
					defer func() { <-sem }()
					fn(adapter, candidate)
				}()
			}
		}
	}
	wg.Wait()
}

func (e *ConfigEnforcer) reconcileCandidate(ctx context.Context, adapter ConfigDimensionAdapter, c models.ConfigEnforcementCandidate) {
	policy := e.policy(adapter)
	if !adapter.Supported(ctx, c) || c.ConfigObservedAt == nil || c.ObservedStateHash == nil || e.now().Sub(*c.ConfigObservedAt) > policy.ObservationMaxAge {
		return
	}
	desired, err := adapter.Desired(ctx, c)
	if err != nil {
		slog.Error("miner channel config desired state failed", "dimension", adapter.Dimension(), "device", c.DeviceIdentifier, "error", err)
		return
	}
	if desired.ComparableHash == "" || desired.RevisionHash == "" {
		return
	}

	state := configCandidateState(c, desired.RevisionHash)
	revisionChanged := c.DesiredStateHash == nil || *c.DesiredStateHash != desired.RevisionHash
	observedMatches := *c.ObservedStateHash == desired.ComparableHash
	if !revisionChanged && state == models.EnforcementStateDispatched && observedMatches &&
		c.LastDispatchedAt != nil && c.ConfigObservedAt.After(*c.LastDispatchedAt) {
		e.markConfigConfirmed(ctx, c, desired)
		return
	}
	if !revisionChanged && state == models.EnforcementStateConfirmed {
		if observedMatches {
			return
		}
		_, err = e.store.MarkConfigDrifted(ctx, e.mutation(c, desired))
		if err != nil {
			slog.Error("miner channel config drift transition failed", "device", c.DeviceIdentifier, "error", err)
		}
		return
	}
	if !revisionChanged && state == models.EnforcementStateDispatched {
		if c.LastBatchUUID != nil {
			finished, batchErr := e.store.IsCommandBatchFinished(ctx, *c.LastBatchUUID)
			if batchErr != nil || !finished {
				return
			}
		}
		if !e.cooldownElapsed(c, policy.RedispatchCooldown) {
			return
		}
		_, _ = e.store.MarkConfigDrifted(ctx, e.mutation(c, desired))
		return
	}
	if state == models.EnforcementStateFailed && !revisionChanged {
		return
	}
	if state == models.EnforcementStateHeld && !e.cooldownElapsed(c, policy.RedispatchCooldown) {
		return
	}
	if c.LastBatchUUID != nil {
		finished, batchErr := e.store.IsCommandBatchFinished(ctx, *c.LastBatchUUID)
		if batchErr != nil || !finished {
			return
		}
	}
	e.dispatch(ctx, adapter, c, desired)
}

func (e *ConfigEnforcer) dispatch(ctx context.Context, adapter ConfigDimensionAdapter, c models.ConfigEnforcementCandidate, desired DesiredDimensionState) {
	mutation := e.mutation(c, desired)
	mutation.DispatchingBefore = e.now().Add(-e.dispatchBefore)
	claimed, err := e.store.ClaimConfigDispatch(ctx, mutation)
	if err != nil || !claimed {
		return
	}
	result, err := adapter.Dispatch(reconcilerConfigCommandContext(ctx, c), c, desired)
	if err != nil {
		e.recordConfigFailure(ctx, adapter, c, desired, err.Error())
		return
	}
	if result != nil && !containsString(result.DispatchedDeviceIdentifiers, c.DeviceIdentifier) {
		if reason, ok := skippedReason(result.Skipped, c.DeviceIdentifier); ok {
			mutation.LastError = reason
			mutation.LastDispatchedAt = e.now()
			_, _ = e.store.MarkConfigDispatchHeld(ctx, mutation)
			return
		}
	}
	if result == nil || result.BatchIdentifier == "" || !containsString(result.DispatchedDeviceIdentifiers, c.DeviceIdentifier) {
		e.recordConfigFailure(ctx, adapter, c, desired, "configuration command did not enqueue device")
		return
	}
	mutation.LastBatchUUID = result.BatchIdentifier
	mutation.LastDispatchedAt = e.now()
	_, err = e.store.MarkConfigDispatched(ctx, mutation)
	if err != nil {
		slog.Error("miner channel config dispatched transition failed", "device", c.DeviceIdentifier, "error", err)
	}
}

func (e *ConfigEnforcer) recordConfigFailure(ctx context.Context, adapter ConfigDimensionAdapter, c models.ConfigEnforcementCandidate, desired DesiredDimensionState, message string) {
	mutation := e.mutation(c, desired)
	mutation.State = models.EnforcementStatePending
	if configCandidateState(c, desired.RevisionHash) == models.EnforcementStateDrifted {
		mutation.State = models.EnforcementStateDrifted
	}
	mutation.LastError = message
	mutation.MaxRetries = e.policy(adapter).MaxRetries
	_, _ = e.store.MarkConfigDispatchFailure(ctx, mutation)
}

func (e *ConfigEnforcer) markConfigConfirmed(ctx context.Context, c models.ConfigEnforcementCandidate, desired DesiredDimensionState) {
	mutation := e.mutation(c, desired)
	mutation.ConfirmedAt = e.now()
	mutation.ObservedAt = *c.ConfigObservedAt
	_, _ = e.store.MarkConfigConfirmed(ctx, mutation)
}

func (e *ConfigEnforcer) mutation(c models.ConfigEnforcementCandidate, desired DesiredDimensionState) models.ConfigEnforcementMutationParams {
	return models.ConfigEnforcementMutationParams{
		OrgID: c.OrgID, DeviceIdentifier: c.DeviceIdentifier, Dimension: c.Dimension,
		DesiredStateHash: desired.RevisionHash,
	}
}

func (e *ConfigEnforcer) cooldownElapsed(c models.ConfigEnforcementCandidate, cooldown time.Duration) bool {
	return c.LastDispatchedAt == nil || e.now().Sub(*c.LastDispatchedAt) >= cooldown
}

func (e *ConfigEnforcer) policy(adapter ConfigDimensionAdapter) ConfigDimensionPolicy {
	policy := adapter.Policy()
	if policy.ObservationMaxAge <= 0 {
		policy.ObservationMaxAge = e.maxAge
	}
	if policy.RedispatchCooldown <= 0 {
		policy.RedispatchCooldown = e.cooldown
	}
	if policy.MaxRetries <= 0 {
		policy.MaxRetries = e.maxRetries
	}
	return policy
}

func configCandidateState(c models.ConfigEnforcementCandidate, revisionHash string) models.EnforcementState {
	if c.State == nil || c.DesiredStateHash == nil || *c.DesiredStateHash != revisionHash {
		return models.EnforcementStatePending
	}
	return *c.State
}

func reconcilerConfigCommandContext(parent context.Context, c models.ConfigEnforcementCandidate) context.Context {
	firmwareCandidate := models.FirmwareEnforcementCandidate{
		OrgID: c.OrgID, DeviceIdentifier: c.DeviceIdentifier, ActorUserID: c.ActorUserID,
		ActorExternalUserID: c.ActorExternalUserID, ActorUsername: c.ActorUsername,
	}
	return reconcilerCommandContext(parent, firmwareCandidate)
}

func validateAdapter(adapter ConfigDimensionAdapter) error {
	if adapter == nil || adapter.Dimension() == "" {
		return fmt.Errorf("config adapter dimension is required")
	}
	return nil
}
