// Package reconciler continuously enforces cohort desired firmware targets.
package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"connectrpc.com/authn"
	"github.com/google/uuid"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/cohort/models"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

const (
	reconcilerActorName = "cohort-reconciler"

	defaultTickInterval             = 30 * time.Second
	defaultShutdownDeadline         = 10 * time.Second
	defaultObservationMaxAge        = 10 * time.Minute
	defaultDispatchingTimeout       = 2 * time.Minute
	defaultRedispatchCooldown       = 45 * time.Minute
	defaultMaxRetries         int32 = 5
)

type CommandDispatcher interface {
	FirmwareUpdate(ctx context.Context, selector *pb.DeviceSelector, firmwareFileID string) (*command.CommandResult, error)
}

type FirmwareMetadataProvider interface {
	GetFirmwareMetadata(fileID string) (files.FirmwareMetadata, error)
}

type Config struct {
	TickInterval       time.Duration
	ShutdownDeadline   time.Duration
	ObservationMaxAge  time.Duration
	DispatchingTimeout time.Duration
	RedispatchCooldown time.Duration
	MaxRetries         int32
}

func (c Config) withDefaults() Config {
	if c.TickInterval <= 0 {
		c.TickInterval = defaultTickInterval
	}
	if c.ShutdownDeadline <= 0 {
		c.ShutdownDeadline = defaultShutdownDeadline
	}
	if c.ObservationMaxAge <= 0 {
		c.ObservationMaxAge = defaultObservationMaxAge
	}
	if c.DispatchingTimeout <= 0 {
		c.DispatchingTimeout = defaultDispatchingTimeout
	}
	if c.RedispatchCooldown <= 0 {
		c.RedispatchCooldown = defaultRedispatchCooldown
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = defaultMaxRetries
	}
	return c
}

type Reconciler struct {
	cfg              Config
	store            interfaces.CohortFirmwareEnforcementStore
	cmd              CommandDispatcher
	firmwareMetadata FirmwareMetadataProvider
	configEnforcer   *ConfigEnforcer
	now              func() time.Time

	stopCancel context.CancelFunc
	workCancel context.CancelFunc
	wg         sync.WaitGroup

	mu      sync.Mutex
	running bool
}

type Option func(*Reconciler)

func WithConfigEnforcement(store interfaces.CohortConfigEnforcementStore, adapters ...ConfigDimensionAdapter) Option {
	return func(r *Reconciler) {
		r.configEnforcer = NewConfigEnforcer(store, adapters...)
	}
}

func New(cfg Config, store interfaces.CohortFirmwareEnforcementStore, cmd CommandDispatcher, firmwareMetadata FirmwareMetadataProvider, opts ...Option) *Reconciler {
	r := &Reconciler{
		cfg:              cfg.withDefaults(),
		store:            store,
		cmd:              cmd,
		firmwareMetadata: firmwareMetadata,
		now:              time.Now,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *Reconciler) Start(_ context.Context) error {
	if r.store == nil {
		return fmt.Errorf("cohort reconciler: store is required")
	}
	if r.cmd == nil {
		return fmt.Errorf("cohort reconciler: command dispatcher is required")
	}
	if r.firmwareMetadata == nil {
		return fmt.Errorf("cohort reconciler: firmware metadata provider is required")
	}
	if r.cfg.TickInterval < time.Second {
		return fmt.Errorf("cohort reconciler: tick interval must be at least 1s, got %s", r.cfg.TickInterval)
	}
	if r.configEnforcer != nil {
		for _, adapter := range r.configEnforcer.adapters {
			if err := validateAdapter(adapter); err != nil {
				return fmt.Errorf("cohort reconciler: %w", err)
			}
		}
	}

	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return nil
	}
	r.running = true
	stopCtx, stopCancel := context.WithCancel(context.Background())
	workCtx, workCancel := context.WithCancel(context.Background())
	r.stopCancel = stopCancel
	r.workCancel = workCancel
	r.mu.Unlock()

	r.wg.Add(1)
	go r.tickLoop(stopCtx, workCtx)
	if r.configEnforcer != nil {
		r.wg.Add(1)
		go r.configObservationLoop(stopCtx, workCtx)
	}
	slog.Info("cohort reconciler started", "tick_interval", r.cfg.TickInterval)
	return nil
}

func (r *Reconciler) configObservationLoop(stopCtx, workCtx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(defaultConfigObservationInterval)
	defer ticker.Stop()
	r.configEnforcer.Observe(workCtx)
	for {
		select {
		case <-stopCtx.Done():
			return
		case <-ticker.C:
			r.configEnforcer.Observe(workCtx)
		}
	}
}

func (r *Reconciler) Stop() error {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return nil
	}
	r.running = false
	stopCancel := r.stopCancel
	workCancel := r.workCancel
	r.stopCancel = nil
	r.workCancel = nil
	r.mu.Unlock()

	if workCancel != nil {
		watchdog := time.AfterFunc(r.cfg.ShutdownDeadline, workCancel)
		defer watchdog.Stop()
	}
	if stopCancel != nil {
		stopCancel()
	}
	r.wg.Wait()
	if workCancel != nil {
		workCancel()
	}
	slog.Info("cohort reconciler stopped")
	return nil
}

func (r *Reconciler) tickLoop(stopCtx, workCtx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.cfg.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCtx.Done():
			return
		case <-ticker.C:
			r.safeTick(workCtx)
		}
	}
}

func (r *Reconciler) safeTick(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("cohort reconciler: recovered panic in tick", "panic", rec)
		}
	}()
	r.runTick(ctx)
}

func (r *Reconciler) runTick(ctx context.Context) {
	tickStart := r.now()
	tickUUID := uuid.New()
	tickCtx, cancel := context.WithTimeout(ctx, 2*r.cfg.TickInterval)
	defer cancel()

	orgIDs, err := r.store.ListOrgsWithFirmwareTargets(tickCtx)
	if err != nil {
		slog.Error("cohort reconciler: failed to list orgs with firmware targets", "error", err)
		r.upsertHeartbeat(tickStart, tickUUID, 0)
		return
	}

	var activeCount int32
	for _, orgID := range orgIDs {
		if tickCtx.Err() != nil {
			break
		}
		candidates, err := r.store.ListFirmwareEnforcementCandidates(tickCtx, orgID)
		if err != nil {
			slog.Error("cohort reconciler: failed to list firmware candidates", "org_id", orgID, "error", err)
			continue
		}
		activeCount += int32(len(candidates)) //nolint:gosec // per-org device count is bounded well below MaxInt32.
		for _, candidate := range candidates {
			if tickCtx.Err() != nil {
				break
			}
			r.processCandidate(tickCtx, candidate)
		}
	}
	r.upsertHeartbeat(tickStart, tickUUID, activeCount)
	if r.configEnforcer != nil && tickCtx.Err() == nil {
		r.configEnforcer.Reconcile(tickCtx)
	}
}

func (r *Reconciler) upsertHeartbeat(tickStart time.Time, tickUUID uuid.UUID, activeCount int32) {
	durationMS := int32(r.now().Sub(tickStart).Milliseconds()) //nolint:gosec // tick durations fit in int32.
	hbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.store.UpsertCohortReconcilerHeartbeat(hbCtx, tickStart, tickUUID, &durationMS, activeCount); err != nil {
		slog.Error("cohort reconciler: heartbeat upsert failed", "error", err)
	}
}

func (r *Reconciler) processCandidate(ctx context.Context, c models.FirmwareEnforcementCandidate) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("cohort reconciler: recovered panic processing firmware candidate",
				"org_id", c.OrgID, "device", c.DeviceIdentifier, "panic", rec)
		}
	}()
	if c.ActorUserID <= 0 {
		slog.Warn("cohort reconciler: firmware candidate has no actor user", "org_id", c.OrgID, "device", c.DeviceIdentifier)
		return
	}
	if strings.TrimSpace(c.FirmwareFileID) == "" {
		return
	}
	desiredVersion, ok := r.desiredFirmwareVersion(ctx, c)
	if !ok {
		return
	}
	c.DesiredFirmwareVersion = desiredVersion

	observationFresh := c.FirmwareObservedAt != nil &&
		c.ObservedFirmwareVersion != nil &&
		r.now().Sub(*c.FirmwareObservedAt) <= r.cfg.ObservationMaxAge
	if observationFresh && *c.ObservedFirmwareVersion == c.DesiredFirmwareVersion {
		r.markConfirmed(ctx, c)
		return
	}

	state := candidateState(c)
	if state == models.EnforcementStateDispatched {
		if c.LastBatchUUID != nil {
			finished, err := r.store.IsCommandBatchFinished(ctx, *c.LastBatchUUID)
			if err != nil {
				slog.Error("cohort reconciler: failed to check firmware batch", "device", c.DeviceIdentifier, "batch", *c.LastBatchUUID, "error", err)
				return
			}
			if !finished {
				return
			}
		}
		if !r.redispatchCooldownElapsed(c) || c.FirmwareObservedAt == nil {
			return
		}
		_, err := r.store.MarkFirmwareDrifted(ctx, models.MarkFirmwareDriftedParams{
			OrgID:            c.OrgID,
			DeviceIdentifier: c.DeviceIdentifier,
			ObservedAt:       *c.FirmwareObservedAt,
		})
		if err != nil {
			slog.Error("cohort reconciler: failed to age firmware dispatch into drift", "device", c.DeviceIdentifier, "error", err)
		}
		return
	}

	if !observationFresh {
		return
	}

	switch state {
	case models.EnforcementStateDispatched:
		// Dispatched candidates are fully handled above, including batch
		// completion and cooldown checks.
		return
	case models.EnforcementStateConfirmed:
		_, err := r.store.MarkFirmwareDrifted(ctx, models.MarkFirmwareDriftedParams{
			OrgID:            c.OrgID,
			DeviceIdentifier: c.DeviceIdentifier,
			ObservedAt:       *c.FirmwareObservedAt,
		})
		if err != nil {
			slog.Error("cohort reconciler: failed to mark firmware drifted", "device", c.DeviceIdentifier, "error", err)
		}
	case models.EnforcementStateFailed:
		return
	case models.EnforcementStateHeld:
		return
	case models.EnforcementStatePending, models.EnforcementStateDispatching, models.EnforcementStateDrifted:
		if state == models.EnforcementStateDrifted && !r.redispatchCooldownElapsed(c) {
			return
		}
		r.dispatchFirmware(ctx, c)
	}
}

func (r *Reconciler) desiredFirmwareVersion(ctx context.Context, c models.FirmwareEnforcementCandidate) (string, bool) {
	metadata, err := r.firmwareMetadata.GetFirmwareMetadata(c.FirmwareFileID)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			r.clearMissingFirmwareTarget(ctx, c, err)
			return "", false
		}
		slog.Error("cohort reconciler: failed to read firmware metadata", "firmware_file_id", c.FirmwareFileID, "error", err)
		return "", false
	}
	version := strings.TrimSpace(metadata.FirmwareVersion)
	if version == "" {
		slog.Warn("cohort reconciler: firmware file has no version metadata", "firmware_file_id", c.FirmwareFileID)
		return "", false
	}
	return version, true
}

func (r *Reconciler) clearMissingFirmwareTarget(ctx context.Context, c models.FirmwareEnforcementCandidate, cause error) {
	cleared, err := r.store.ClearMissingFirmwareTarget(ctx, c.OrgID, c.FirmwareFileID)
	if err != nil {
		slog.Error("cohort reconciler: failed to clear missing firmware target",
			"org_id", c.OrgID,
			"cohort_id", c.CohortID,
			"firmware_file_id", c.FirmwareFileID,
			"cause", cause,
			"error", err,
		)
		return
	}
	if cleared == 0 {
		slog.Debug("cohort reconciler: missing firmware target was already clear",
			"org_id", c.OrgID,
			"cohort_id", c.CohortID,
			"firmware_file_id", c.FirmwareFileID,
			"cause", cause,
		)
		return
	}
	slog.Warn("cohort reconciler: cleared missing firmware target",
		"org_id", c.OrgID,
		"cohort_id", c.CohortID,
		"firmware_file_id", c.FirmwareFileID,
		"cleared_references", cleared,
		"cause", cause,
	)
}

func (r *Reconciler) markConfirmed(ctx context.Context, c models.FirmwareEnforcementCandidate) {
	observedAt := *c.FirmwareObservedAt
	_, err := r.store.MarkFirmwareConfirmed(ctx, models.MarkFirmwareConfirmedParams{
		OrgID:                  c.OrgID,
		DeviceIdentifier:       c.DeviceIdentifier,
		DesiredFirmwareFileID:  c.FirmwareFileID,
		DesiredFirmwareVersion: c.DesiredFirmwareVersion,
		ConfirmedAt:            r.now(),
		ObservedAt:             observedAt,
	})
	if err != nil {
		slog.Error("cohort reconciler: failed to mark firmware confirmed", "device", c.DeviceIdentifier, "error", err)
	}
}

func (r *Reconciler) dispatchFirmware(ctx context.Context, c models.FirmwareEnforcementCandidate) {
	claimed, err := r.store.ClaimFirmwareDispatch(ctx, models.ClaimFirmwareDispatchParams{
		OrgID:                  c.OrgID,
		DeviceIdentifier:       c.DeviceIdentifier,
		DesiredFirmwareFileID:  c.FirmwareFileID,
		DesiredFirmwareVersion: c.DesiredFirmwareVersion,
		DispatchingBefore:      r.now().Add(-r.cfg.DispatchingTimeout),
	})
	if err != nil {
		slog.Error("cohort reconciler: failed to claim firmware dispatch", "device", c.DeviceIdentifier, "error", err)
		return
	}
	if !claimed {
		return
	}

	selector := &pb.DeviceSelector{
		SelectionType: &pb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{c.DeviceIdentifier}},
		},
	}
	result, dispatchErr := r.cmd.FirmwareUpdate(reconcilerCommandContext(ctx, c), selector, c.FirmwareFileID)
	if dispatchErr != nil {
		r.recordDispatchFailure(ctx, c, dispatchErr.Error())
		return
	}
	if result != nil && !containsString(result.DispatchedDeviceIdentifiers, c.DeviceIdentifier) {
		if reason, ok := skippedReason(result.Skipped, c.DeviceIdentifier); ok {
			r.recordDispatchHeld(ctx, c, reason)
			return
		}
	}
	if result == nil || result.BatchIdentifier == "" {
		r.recordDispatchFailure(ctx, c, "firmware command produced no batch")
		return
	}
	if !containsString(result.DispatchedDeviceIdentifiers, c.DeviceIdentifier) {
		r.recordDispatchFailure(ctx, c, "firmware command did not enqueue device")
		return
	}

	_, err = r.store.MarkFirmwareDispatched(ctx, models.MarkFirmwareDispatchedParams{
		OrgID:                  c.OrgID,
		DeviceIdentifier:       c.DeviceIdentifier,
		DesiredFirmwareFileID:  c.FirmwareFileID,
		DesiredFirmwareVersion: c.DesiredFirmwareVersion,
		LastBatchUUID:          result.BatchIdentifier,
		LastDispatchedAt:       r.now(),
	})
	if err != nil {
		slog.Error("cohort reconciler: failed to mark firmware dispatched", "device", c.DeviceIdentifier, "error", err)
	}
}

func (r *Reconciler) recordDispatchFailure(ctx context.Context, c models.FirmwareEnforcementCandidate, msg string) {
	retryState := models.EnforcementStatePending
	if candidateState(c) == models.EnforcementStateDrifted {
		retryState = models.EnforcementStateDrifted
	}
	_, err := r.store.MarkFirmwareDispatchFailure(ctx, models.MarkFirmwareDispatchFailureParams{
		OrgID:                  c.OrgID,
		DeviceIdentifier:       c.DeviceIdentifier,
		DesiredFirmwareFileID:  c.FirmwareFileID,
		DesiredFirmwareVersion: c.DesiredFirmwareVersion,
		RetryState:             retryState,
		LastError:              msg,
		MaxRetries:             r.cfg.MaxRetries,
	})
	if err != nil {
		slog.Error("cohort reconciler: failed to record firmware dispatch failure", "device", c.DeviceIdentifier, "error", err)
	}
}

func (r *Reconciler) recordDispatchHeld(ctx context.Context, c models.FirmwareEnforcementCandidate, msg string) {
	retryState := models.EnforcementStatePending
	if candidateState(c) == models.EnforcementStateDrifted {
		retryState = models.EnforcementStateDrifted
	}
	_, err := r.store.MarkFirmwareDispatchHeld(ctx, models.MarkFirmwareDispatchHeldParams{
		OrgID:                  c.OrgID,
		DeviceIdentifier:       c.DeviceIdentifier,
		DesiredFirmwareFileID:  c.FirmwareFileID,
		DesiredFirmwareVersion: c.DesiredFirmwareVersion,
		RetryState:             retryState,
		LastError:              msg,
	})
	if err != nil {
		slog.Error("cohort reconciler: failed to hold firmware dispatch", "device", c.DeviceIdentifier, "error", err)
	}
}

func (r *Reconciler) redispatchCooldownElapsed(c models.FirmwareEnforcementCandidate) bool {
	return c.LastDispatchedAt == nil || r.now().Sub(*c.LastDispatchedAt) >= r.cfg.RedispatchCooldown
}

func candidateState(c models.FirmwareEnforcementCandidate) models.EnforcementState {
	if c.State == nil {
		return models.EnforcementStatePending
	}
	if cachedTargetID := strings.TrimSpace(stringValue(c.StateDesiredFirmwareFileID)); cachedTargetID != "" && cachedTargetID != c.FirmwareFileID {
		return models.EnforcementStatePending
	}
	return *c.State
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func reconcilerCommandContext(parent context.Context, c models.FirmwareEnforcementCandidate) context.Context {
	return authn.SetInfo(parent, &session.Info{
		SessionID:      reconcilerActorName,
		UserID:         c.ActorUserID,
		OrganizationID: c.OrgID,
		ExternalUserID: c.ActorExternalUserID,
		Username:       c.ActorUsername,
		Role:           "SUPER_ADMIN",
		Actor:          session.ActorCohort,
	})
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func skippedReason(skipped []command.SkippedDevice, deviceIdentifier string) (string, bool) {
	for _, item := range skipped {
		if item.DeviceIdentifier != deviceIdentifier {
			continue
		}
		switch {
		case item.Reason != "":
			return item.Reason, true
		case item.FilterName != "":
			return "filtered by " + item.FilterName, true
		}
		return "firmware command skipped device", true
	}
	return "", false
}
