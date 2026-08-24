package reconciler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"connectrpc.com/authn"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	channel "github.com/block/proto-fleet/server/internal/domain/channel"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/telemetry"
	telemetrymodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	modelsv2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

const (
	defaultTickInterval           = 30 * time.Second
	defaultBatchSize              = 100
	defaultVerificationRetryDelay = 30 * time.Second
)

type Config struct {
	TickInterval           time.Duration `help:"Interval between channel firmware enforcement passes." default:"30s" env:"TICK_INTERVAL"`
	BatchSize              int32         `help:"Maximum enforcement rows processed per pass." default:"100" env:"BATCH_SIZE"`
	VerificationRetryDelay time.Duration `help:"Delay before retrying firmware verification." default:"30s" env:"VERIFICATION_RETRY_DELAY"`
}

func (c Config) withDefaults() Config {
	if c.TickInterval <= 0 {
		c.TickInterval = defaultTickInterval
	}
	if c.BatchSize <= 0 {
		c.BatchSize = defaultBatchSize
	}
	if c.VerificationRetryDelay <= 0 {
		c.VerificationRetryDelay = defaultVerificationRetryDelay
	}
	return c
}

type Store = channel.EnforcementStore

type CommandDispatcher interface {
	ChannelFirmwareUpdate(
		ctx context.Context,
		selector *pb.DeviceSelector,
		firmwareFileID string,
		commandBatchIdentifier string,
	) (*command.CommandResult, error)
}

type TelemetrySampler interface {
	SampleDeviceMetrics(
		ctx context.Context,
		requests []telemetry.SampleRequest,
	) []telemetry.SampleResult
}

type Reconciler struct {
	cfg        Config
	store      Store
	dispatcher CommandDispatcher
	sampler    TelemetrySampler
	now        func() time.Time
	newBatchID func() string

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

var _ runtimejobs.Lifecycle = (*Reconciler)(nil)

func New(
	cfg Config,
	store Store,
	dispatcher CommandDispatcher,
	sampler TelemetrySampler,
) *Reconciler {
	return &Reconciler{
		cfg:        cfg.withDefaults(),
		store:      store,
		dispatcher: dispatcher,
		sampler:    sampler,
		now:        time.Now,
		newBatchID: id.GenerateID,
	}
}

func (r *Reconciler) Start(ctx context.Context) error {
	if r.store == nil {
		return errors.New("channel enforcement reconciler: store is required")
	}
	if r.dispatcher == nil {
		return errors.New("channel enforcement reconciler: command dispatcher is required")
	}
	if r.sampler == nil {
		return errors.New("channel enforcement reconciler: telemetry sampler is required")
	}
	if r.cfg.TickInterval < time.Second {
		return fmt.Errorf(
			"channel enforcement reconciler: tick_interval must be at least 1s, got %s",
			r.cfg.TickInterval)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		select {
		case <-r.done:
			return errors.New("channel enforcement reconciler: previous activation is still stopping")
		default:
			return nil
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	r.cancel = cancel
	r.done = done
	go r.loop(runCtx, done)
	return nil
}

func (r *Reconciler) Stop(ctx context.Context) error {
	r.mu.Lock()
	if r.cancel == nil {
		r.mu.Unlock()
		return nil
	}
	cancel := r.cancel
	done := r.done
	r.mu.Unlock()

	cancel()
	select {
	case <-done:
		r.mu.Lock()
		if r.done == done {
			r.cancel = nil
			r.done = nil
		}
		r.mu.Unlock()
		return nil
	case <-ctx.Done():
		return fmt.Errorf("channel enforcement reconciler: stop: %w", ctx.Err())
	}
}

func (r *Reconciler) loop(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	reportProgress := runtimejobs.TrackProgress(ctx, r.cfg.TickInterval)
	r.safeReconcile(ctx)
	reportProgress()

	ticker := time.NewTicker(r.cfg.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.safeReconcile(ctx)
			reportProgress()
		}
	}
}

func (r *Reconciler) safeReconcile(ctx context.Context) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("channel enforcement reconciler recovered panic", "panic", recovered)
		}
	}()
	r.reconcile(ctx)
}

func (r *Reconciler) reconcile(ctx context.Context) {
	rows, err := r.store.ListForReconcile(ctx, r.cfg.BatchSize)
	if err != nil {
		slog.Error("channel enforcement reconciler failed to list rows", "error", err)
		return
	}
	for _, enforcement := range rows {
		if ctx.Err() != nil {
			return
		}
		r.reconcileOne(ctx, enforcement)
	}
}

func (r *Reconciler) reconcileOne(ctx context.Context, enforcement channel.Enforcement) {
	switch enforcement.State {
	case channel.EnforcementStatePending, channel.EnforcementStateHeld:
		r.dispatch(ctx, enforcement)
	case channel.EnforcementStateDispatching:
		r.attention(
			ctx,
			enforcement,
			"firmware dispatch was interrupted after its durable claim")
	case channel.EnforcementStateDispatched:
		r.observeCommand(ctx, enforcement)
	case channel.EnforcementStateVerifying:
		r.verify(ctx, enforcement)
	case channel.EnforcementStateConfirmed,
		channel.EnforcementStateAttentionRequired,
		channel.EnforcementStateCancelled:
		return
	default:
		slog.Error(
			"channel enforcement reconciler found unknown state",
			"enforcement_id", enforcement.ID,
			"state", enforcement.State)
	}
}

func (r *Reconciler) dispatch(ctx context.Context, enforcement channel.Enforcement) {
	if disposition, reason := modelIdentityDisposition(enforcement); disposition != identityReady {
		switch disposition {
		case identityReady:
			// Guarded by the outer condition.
		case identityHold:
			if err := r.store.Hold(ctx, enforcement, reason, r.now()); err != nil &&
				!errors.Is(err, channel.ErrCASConflict) {
				slog.Error(
					"channel enforcement model identity hold failed",
					"enforcement_id", enforcement.ID,
					"error", err)
			}
		case identityMismatch:
			r.attention(ctx, enforcement, reason)
		}
		return
	}

	claimed, err := r.store.Claim(
		ctx,
		enforcement,
		r.newBatchID(),
		r.now(),
	)
	if errors.Is(err, channel.ErrCASConflict) {
		return
	}
	if err != nil {
		slog.Error(
			"channel enforcement claim failed",
			"enforcement_id", enforcement.ID,
			"error", err)
		return
	}

	commandCtx := command.WithCommandActivitySuppressed(
		enforcementContext(ctx, claimed.OrgID, claimed.CreatedByUserID))
	result, err := r.dispatcher.ChannelFirmwareUpdate(
		commandCtx,
		includeDevice(claimed.DeviceIdentifier),
		claimed.DesiredFirmwareFileID,
		claimed.CommandBatchUUID,
	)
	if err != nil {
		if command.IsEnqueueUncertain(err) {
			r.attention(ctx, claimed, err.Error())
			return
		}
		r.returnPending(ctx, claimed, err.Error())
		return
	}
	if result == nil {
		r.returnPending(ctx, claimed, "firmware dispatcher returned no result")
		return
	}
	if skippedBy(result, claimed.DeviceIdentifier, command.CurtailmentActiveFilterName) {
		if err := r.store.Hold(
			ctx,
			claimed,
			"device is held by active curtailment",
			r.now(),
		); err != nil && !errors.Is(err, channel.ErrCASConflict) {
			slog.Error(
				"channel enforcement hold failed",
				"enforcement_id", claimed.ID,
				"error", err)
		}
		return
	}
	if result.BatchIdentifier != claimed.CommandBatchUUID ||
		!contains(result.DispatchedDeviceIdentifiers, claimed.DeviceIdentifier) {
		r.returnPending(ctx, claimed, "firmware command was not durably enqueued")
		return
	}
	if err := r.store.MarkDispatched(ctx, claimed, r.now()); err != nil &&
		!errors.Is(err, channel.ErrCASConflict) {
		slog.Error(
			"channel enforcement dispatch state write failed",
			"enforcement_id", claimed.ID,
			"error", err)
	}
}

type identityDisposition int

const (
	identityReady identityDisposition = iota
	identityHold
	identityMismatch
)

func modelIdentityDisposition(enforcement channel.Enforcement) (identityDisposition, string) {
	if enforcement.ExpectedModelIdentityKey == "" {
		return identityReady, ""
	}
	if enforcement.ModelIdentityValidatedAt == nil {
		return identityHold, "model identity validation boundary is unavailable"
	}
	if enforcement.ModelIdentityObservedAt == nil ||
		!enforcement.ModelIdentityObservedAt.After(*enforcement.ModelIdentityValidatedAt) {
		return identityHold, "model identity observation is stale"
	}
	if enforcement.ObservedModelIdentityKey == "" {
		return identityHold, "model identity observation is empty"
	}
	if enforcement.ObservedModelIdentityKey != enforcement.ExpectedModelIdentityKey {
		return identityMismatch, "model identity changed after rollout admission"
	}
	return identityReady, ""
}

func (r *Reconciler) returnPending(
	ctx context.Context,
	enforcement channel.Enforcement,
	reason string,
) {
	if err := r.store.ReturnPending(ctx, enforcement, reason); err != nil &&
		!errors.Is(err, channel.ErrCASConflict) {
		slog.Error(
			"channel enforcement pending state write failed",
			"enforcement_id", enforcement.ID,
			"error", err)
	}
}

func (r *Reconciler) observeCommand(ctx context.Context, enforcement channel.Enforcement) {
	outcome, err := r.store.CommandOutcome(ctx, enforcement)
	if err != nil {
		slog.Error(
			"channel enforcement command outcome read failed",
			"enforcement_id", enforcement.ID,
			"error", err)
		return
	}
	switch outcome.Status {
	case channel.CommandOutcomePending, channel.CommandOutcomeProcessing:
		return
	case channel.CommandOutcomeSuccess:
		if outcome.CompletedAt.IsZero() {
			r.attention(ctx, enforcement, "firmware command completed without a durable completion time")
			return
		}
		if err := r.store.MarkVerifying(
			ctx,
			enforcement,
			outcome.CompletedAt,
		); err != nil && !errors.Is(err, channel.ErrCASConflict) {
			slog.Error(
				"channel enforcement verifying state write failed",
				"enforcement_id", enforcement.ID,
				"error", err)
		}
	case channel.CommandOutcomeFailed:
		reason := outcome.Error
		if reason == "" {
			reason = "firmware command failed after enqueue"
		}
		r.attention(ctx, enforcement, reason)
	case channel.CommandOutcomeMissing:
		r.attention(ctx, enforcement, "durable firmware command is missing its queue message")
	default:
		r.attention(ctx, enforcement, "firmware command has an unknown queue outcome")
	}
}

func (r *Reconciler) verify(ctx context.Context, enforcement channel.Enforcement) {
	if enforcement.CommandCompletedAt == nil {
		r.attention(ctx, enforcement, "verification has no command completion boundary")
		return
	}
	results := r.sampler.SampleDeviceMetrics(ctx, []telemetry.SampleRequest{{
		DeviceID:     telemetrymodels.DeviceIdentifier(enforcement.DeviceIdentifier),
		OrgID:        enforcement.OrgID,
		SampledAfter: *enforcement.CommandCompletedAt,
	}})
	if len(results) == 0 {
		r.deferVerificationWithError(ctx, enforcement, "telemetry sampler returned no results")
		return
	}
	if len(results) != 1 {
		r.deferVerificationWithError(
			ctx,
			enforcement,
			fmt.Sprintf("telemetry sampler returned %d results; expected 1", len(results)),
		)
		return
	}
	sample := results[0]
	if sample.Err != nil {
		r.deferVerificationWithError(
			ctx,
			enforcement,
			fmt.Sprintf("telemetry sampler failed: %v", sample.Err),
		)
		return
	}
	if sample.OrgID != enforcement.OrgID {
		r.deferVerificationWithError(
			ctx,
			enforcement,
			fmt.Sprintf(
				"telemetry sample organization mismatch: got %d, want %d",
				sample.OrgID,
				enforcement.OrgID,
			),
		)
		return
	}
	if string(sample.DeviceID) != enforcement.DeviceIdentifier {
		r.deferVerificationWithError(
			ctx,
			enforcement,
			fmt.Sprintf(
				"telemetry sample device mismatch: got %q, want %q",
				sample.DeviceID,
				enforcement.DeviceIdentifier,
			),
		)
		return
	}
	if !sample.FlightStart.After(*enforcement.CommandCompletedAt) {
		r.deferVerificationWithError(
			ctx,
			enforcement,
			fmt.Sprintf(
				"telemetry sample is stale: sampled at %s, command completed at %s",
				sample.FlightStart.Format(time.RFC3339Nano),
				enforcement.CommandCompletedAt.Format(time.RFC3339Nano),
			),
		)
		return
	}

	observation := observationFromSample(sample)
	if confirms(enforcement, sample.Metrics, observation) {
		if err := r.store.Confirm(ctx, enforcement, observation); err != nil &&
			!errors.Is(err, channel.ErrCASConflict) {
			slog.Error(
				"channel enforcement confirmation failed",
				"enforcement_id", enforcement.ID,
				"error", err)
		}
		return
	}
	if err := r.store.RecordObservation(
		ctx,
		enforcement,
		observation,
		r.nextVerificationAttempt(),
	); err != nil &&
		!errors.Is(err, channel.ErrCASConflict) {
		slog.Error(
			"channel enforcement observation write failed",
			"enforcement_id", enforcement.ID,
			"error", err)
	}
}

func (r *Reconciler) deferVerificationWithError(
	ctx context.Context,
	enforcement channel.Enforcement,
	reason string,
) {
	if err := r.store.RecordObservation(
		ctx,
		enforcement,
		channel.Observation{Error: reason},
		r.nextVerificationAttempt(),
	); err != nil && !errors.Is(err, channel.ErrCASConflict) {
		slog.Error(
			"channel enforcement verification error write failed",
			"enforcement_id", enforcement.ID,
			"error", err)
	}
}

func (r *Reconciler) nextVerificationAttempt() time.Time {
	return r.now().Add(r.cfg.VerificationRetryDelay)
}

func observationFromSample(sample telemetry.SampleResult) channel.Observation {
	observation := channel.Observation{
		FirmwareVersion: sample.Metrics.FirmwareVersion,
		ObservedAt:      sample.FlightStart,
	}
	if sample.Metrics.HashrateHS != nil {
		hashrate := sample.Metrics.HashrateHS.Value
		observation.HashrateHS = &hashrate
	}
	return observation
}

func confirms(
	enforcement channel.Enforcement,
	metrics modelsv2.DeviceMetrics,
	observation channel.Observation,
) bool {
	if metrics.FirmwareVersion != enforcement.DesiredFirmwareVersion ||
		metrics.Health != modelsv2.HealthHealthyActive ||
		observation.HashrateHS == nil {
		return false
	}
	hashrate := *observation.HashrateHS
	return hashrate > 0 && !math.IsNaN(hashrate) && !math.IsInf(hashrate, 0)
}

func (r *Reconciler) attention(
	ctx context.Context,
	enforcement channel.Enforcement,
	reason string,
) {
	if err := r.store.MarkAttentionRequired(
		ctx,
		enforcement,
		reason,
		r.now(),
	); err != nil && !errors.Is(err, channel.ErrCASConflict) {
		slog.Error(
			"channel enforcement attention state write failed",
			"enforcement_id", enforcement.ID,
			"error", err)
	}
}

func enforcementContext(parent context.Context, orgID, userID int64) context.Context {
	const actorName = "channel-enforcement-reconciler"
	return authn.SetInfo(parent, &session.Info{
		SessionID:      actorName,
		UserID:         userID,
		OrganizationID: orgID,
		ExternalUserID: actorName,
		Username:       actorName,
		Actor:          session.ActorChannelEnforcement,
	})
}

func includeDevice(deviceIdentifier string) *pb.DeviceSelector {
	return &pb.DeviceSelector{
		SelectionType: &pb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{
				DeviceIdentifiers: []string{deviceIdentifier},
			},
		},
	}
}

func skippedBy(result *command.CommandResult, deviceIdentifier, filterName string) bool {
	for _, skipped := range result.Skipped {
		if skipped.DeviceIdentifier == deviceIdentifier && skipped.FilterName == filterName {
			return true
		}
	}
	return false
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
