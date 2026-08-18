package ha

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

type State string

const (
	StatePassive State = "passive"
	StateActive  State = "active"

	haEventAttribute     = "ha_event"
	haEventFailover      = "failover"
	haEventStateDegraded = "state_degraded"
)

type CoordinatorConfig struct {
	LeaseDuration time.Duration
	RenewInterval time.Duration
	RetryInterval time.Duration
}

// Snapshot is a non-authoritative view of the coordinator's current state.
type Snapshot struct {
	State                State
	HolderID             uuid.UUID
	DCSClusterID         string
	Token                Token
	ExpiresAt            time.Time
	UpdatedAt            time.Time
	ObservationAvailable bool
	FreshUntil           time.Time
}

type Coordinator struct {
	observer writerObserver
	store    ownershipStore
	config   CoordinatorConfig
	holderID uuid.UUID
	logger   *slog.Logger

	mu            sync.RWMutex
	ownership     Ownership
	activeCtx     context.Context //nolint:containedctx // The coordinator owns this explicit active-lifetime context.
	cancelActive  context.CancelCauseFunc
	leaseTimer    *time.Timer
	leaseVersion  uint64
	stateChanged  chan struct{}
	acquireAfter  time.Time
	observed      bool
	updatedAt     time.Time
	proofDeadline time.Time
	degraded      bool
}

func NewCoordinator(
	observer writerObserver,
	store ownershipStore,
	config CoordinatorConfig,
) (*Coordinator, error) {
	if err := validateCoordinatorConfig(observer, store, config); err != nil {
		return nil, err
	}
	holderID, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("create HA process-incarnation holder ID: %w", err)
	}
	return newCoordinatorWithHolder(observer, store, config, holderID), nil
}

func newCoordinatorWithHolder(
	observer writerObserver,
	store ownershipStore,
	config CoordinatorConfig,
	holderID uuid.UUID,
) *Coordinator {
	return &Coordinator{
		observer:     observer,
		store:        store,
		config:       config,
		holderID:     holderID,
		logger:       slog.Default(),
		stateChanged: make(chan struct{}),
	}
}

func validateCoordinatorConfig(
	observer writerObserver,
	store ownershipStore,
	config CoordinatorConfig,
) error {
	if observer == nil || store == nil {
		return errors.New("HA coordinator requires observer and lease store")
	}
	if config.LeaseDuration <= 0 ||
		config.RenewInterval <= 0 ||
		config.RetryInterval <= 0 {
		return errors.New("HA coordinator intervals must be positive")
	}
	if config.RenewInterval >= config.LeaseDuration {
		return errors.New("HA renewal interval must be shorter than lease duration")
	}
	return nil
}

func (c *Coordinator) HolderID() uuid.UUID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.holderID
}

func (c *Coordinator) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	snapshot := Snapshot{
		State:                StatePassive,
		HolderID:             c.holderID,
		UpdatedAt:            c.updatedAt,
		ObservationAvailable: c.observed,
	}
	if !c.updatedAt.IsZero() {
		snapshot.FreshUntil = c.updatedAt.Add(c.config.LeaseDuration + c.config.RetryInterval)
		if !c.proofDeadline.IsZero() && c.proofDeadline.Before(snapshot.FreshUntil) {
			snapshot.FreshUntil = c.proofDeadline
		}
	}
	if c.activeCtx != nil {
		snapshot.State = StateActive
		snapshot.DCSClusterID = c.ownership.DCSClusterID
		snapshot.Token = c.ownership.Token
		snapshot.ExpiresAt = c.ownership.ExpiresAt
	}
	return snapshot
}

// ActiveLifetime returns the context and token for the current active term.
// The context is canceled before the snapshot transitions back to passive.
func (c *Coordinator) ActiveLifetime() (context.Context, Token, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.activeCtx == nil {
		return nil, Token{}, false
	}
	return c.activeCtx, c.ownership.Token, true
}

// WaitForActive waits for the next owned lifetime.
func (c *Coordinator) WaitForActive(ctx context.Context) (context.Context, Token, error) {
	for {
		c.mu.RLock()
		if c.activeCtx != nil {
			activeCtx := c.activeCtx
			token := c.ownership.Token
			c.mu.RUnlock()
			return activeCtx, token, nil
		}
		changed := c.stateChanged
		c.mu.RUnlock()

		select {
		case <-ctx.Done():
			return nil, Token{}, fmt.Errorf("wait for active ownership: %w", ctx.Err())
		case <-changed:
		}
	}
}

// Run retries while passive. After activation, ownership loss is terminal so
// the process supervisor can restart Fleet in a clean passive state.
func (c *Coordinator) Run(ctx context.Context) error {
	for {
		activated, _ := c.tryAcquire(ctx)
		if ctx.Err() != nil {
			c.deactivate(ctx.Err())
			return fmt.Errorf("HA coordinator stopped: %w", ctx.Err())
		}
		if activated {
			activeCtx, _, active := c.ActiveLifetime()
			if !active {
				return fmt.Errorf("active Fleet ownership ended: %w", ErrOwnershipLost)
			}
			return c.renewUntilStopped(ctx, activeCtx)
		}
		timer := time.NewTimer(c.config.RetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			c.deactivate(ctx.Err())
			return fmt.Errorf("HA coordinator stopped: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (c *Coordinator) renewUntilStopped(ctx, activeCtx context.Context) error {
	ticker := time.NewTicker(c.config.RenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.deactivate(ctx.Err())
			return fmt.Errorf("HA coordinator stopped: %w", ctx.Err())
		case <-activeCtx.Done():
			cause := context.Cause(activeCtx)
			if cause == nil {
				cause = ErrOwnershipLost
			}
			return fmt.Errorf("active Fleet ownership ended: %w", cause)
		case <-ticker.C:
			if err := c.renewActive(ctx, activeCtx); err != nil {
				return fmt.Errorf("active Fleet ownership ended: %w", err)
			}
		}
	}
}

func (c *Coordinator) tryAcquire(ctx context.Context) (bool, error) {
	c.mu.RLock()
	holderID := c.holderID
	acquireAfter := c.acquireAfter
	c.mu.RUnlock()
	if time.Now().Before(acquireAfter) {
		return false, nil
	}

	takeoverCtx, cancelTakeover := context.WithTimeout(ctx, 2*c.config.LeaseDuration)
	defer cancelTakeover()
	var ownership Ownership
	var candidateExpiresAt time.Time
	acquired := false
	contended := false
	observed, err := c.observer.ObserveAndRun(
		takeoverCtx,
		func(actionCtx context.Context, observed WriterObservation) error {
			acquireCtx, cancelAcquire := context.WithTimeout(actionCtx, c.config.LeaseDuration)
			var acquireErr error
			ownership, acquireErr = c.store.Acquire(
				acquireCtx,
				observed,
				holderID,
				c.config.LeaseDuration,
			)
			cancelAcquire()
			if errors.Is(acquireErr, ErrLeaseUnavailable) {
				contended = true
				return nil
			}
			if acquireErr != nil {
				return acquireErr
			}
			acquired = true
			candidateExpiresAt = time.Now().Add(
				ownership.ExpiresAt.Sub(ownership.DatabaseTime),
			)
			return nil
		},
	)
	if err != nil {
		if acquired {
			c.abandonCandidate(candidateExpiresAt, err)
		} else {
			c.deactivate(err)
		}
		return false, err
	}
	if contended {
		c.deactivateObserved(observed.DCSProofDeadline)
		return false, ErrLeaseUnavailable
	}
	activationCtx, cancelActivation := context.WithDeadline(ctx, observed.DCSProofDeadline)
	defer cancelActivation()
	requestStarted := time.Now()
	renewed, err := c.store.Renew(
		activationCtx,
		observed,
		ownership,
		c.config.LeaseDuration,
	)
	if err != nil {
		c.abandonCandidate(candidateExpiresAt, err)
		return false, err
	}
	ownership = renewed
	candidateExpiresAt = time.Now().Add(renewed.ExpiresAt.Sub(renewed.DatabaseTime))
	if err := c.activate(ctx, ownership, requestStarted, observed.DCSProofDeadline); err != nil {
		c.abandonCandidate(candidateExpiresAt, err)
		return false, err
	}
	return true, nil
}

func (c *Coordinator) renewActive(ctx context.Context, expectedCtx context.Context) error {
	c.mu.RLock()
	activeCtx := c.activeCtx
	current := c.ownership
	c.mu.RUnlock()
	if activeCtx != expectedCtx {
		return ErrOwnershipLost
	}

	var (
		renewed        Ownership
		requestStarted time.Time
	)
	observed, err := c.observer.ObserveAndRun(
		activeCtx,
		func(actionCtx context.Context, observed WriterObservation) error {
			if err := validateObservedWriter(current, observed); err != nil {
				return err
			}
			requestStarted = time.Now()
			var renewErr error
			renewed, renewErr = c.store.Renew(
				actionCtx,
				observed,
				current,
				c.config.LeaseDuration,
			)
			return renewErr
		},
	)
	if err != nil {
		if errors.Is(err, ErrTimelineMismatch) {
			if writerErr := validateObservedWriter(current, observed); writerErr != nil {
				c.deactivate(writerErr)
				return writerErr
			}
			return c.markActiveObservationUnavailable(activeCtx)
		}
		c.deactivate(err)
		return err
	}
	if err := c.updateActive(
		renewed,
		current,
		requestStarted,
		observed.DCSProofDeadline,
	); err != nil {
		c.deactivate(err)
		return err
	}
	return nil
}

func validateObservedWriter(current Ownership, observed WriterObservation) error {
	if observed.DCSClusterID == current.DCSClusterID &&
		observed.WriterGeneration == current.Token.WriterGeneration {
		return nil
	}
	return fmt.Errorf(
		"%w: held %s@%d, observed %s@%d",
		ErrWriterChanged,
		current.DCSClusterID,
		current.Token.WriterGeneration,
		observed.DCSClusterID,
		observed.WriterGeneration,
	)
}

func (c *Coordinator) markActiveObservationUnavailable(expectedCtx context.Context) error {
	c.mu.Lock()
	if c.activeCtx != expectedCtx {
		c.mu.Unlock()
		return ErrOwnershipLost
	}
	c.observed = false
	logDegraded := c.setDegradedLocked()
	c.mu.Unlock()
	if logDegraded {
		c.logDegraded()
	}
	return nil
}

func (c *Coordinator) activate(
	parent context.Context,
	ownership Ownership,
	requestStarted time.Time,
	dcsProofDeadline time.Time,
) error {
	deadline, err := localOwnershipDeadline(
		ownership,
		requestStarted,
		dcsProofDeadline,
	)
	if err != nil {
		return err
	}

	c.mu.Lock()
	if err := parent.Err(); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("activate HA coordinator: %w", err)
	}
	if c.cancelActive != nil {
		c.cancelActive(ErrOwnershipLost)
	}
	c.activeCtx, c.cancelActive = context.WithCancelCause(parent)
	c.ownership = ownership
	c.observed = true
	c.updatedAt = time.Now()
	c.proofDeadline = dcsProofDeadline
	c.clearDegradedLocked()
	c.resetLeaseTimerLocked(deadline)
	c.signalStateChangedLocked()
	failover := ownership.Token.LeaseEpoch > 1
	c.mu.Unlock()
	if failover {
		c.logger.Warn(
			"HA failover ownership activated",
			haEventAttribute, haEventFailover,
			"writer_generation", ownership.Token.WriterGeneration,
			"lease_epoch", ownership.Token.LeaseEpoch,
		)
	}
	return nil
}

func (c *Coordinator) updateActive(
	renewed Ownership,
	expected Ownership,
	requestStarted time.Time,
	dcsProofDeadline time.Time,
) error {
	deadline, err := localOwnershipDeadline(
		renewed,
		requestStarted,
		dcsProofDeadline,
	)
	if err != nil {
		return err
	}

	c.mu.Lock()
	if c.activeCtx == nil ||
		c.ownership.DCSClusterID != expected.DCSClusterID ||
		c.ownership.Token != expected.Token ||
		c.ownership.HolderID != expected.HolderID {
		c.mu.Unlock()
		return ErrOwnershipLost
	}
	c.ownership = renewed
	c.updatedAt = time.Now()
	c.observed = true
	c.proofDeadline = dcsProofDeadline
	c.clearDegradedLocked()
	c.resetLeaseTimerLocked(deadline)
	c.mu.Unlock()
	return nil
}

func localOwnershipDeadline(
	ownership Ownership,
	requestStarted time.Time,
	dcsProofDeadline time.Time,
) (time.Time, error) {
	validFor := ownership.ExpiresAt.Sub(ownership.DatabaseTime)
	if requestStarted.IsZero() || validFor <= 0 {
		return time.Time{}, ErrOwnershipExpired
	}
	deadline := requestStarted.Add(validFor)
	if dcsProofDeadline.IsZero() {
		return time.Time{}, ErrLeaderLeaseExpired
	}
	if dcsProofDeadline.Before(deadline) {
		deadline = dcsProofDeadline
	}
	if !deadline.After(time.Now()) {
		return time.Time{}, ErrOwnershipExpired
	}
	return deadline, nil
}

func (c *Coordinator) resetLeaseTimerLocked(deadline time.Time) {
	c.stopLeaseTimerLocked()
	version := c.leaseVersion
	c.leaseTimer = time.AfterFunc(
		time.Until(deadline),
		func() {
			c.mu.Lock()
			if c.activeCtx == nil || c.leaseVersion != version {
				c.mu.Unlock()
				return
			}
			logDegraded := c.deactivateLocked(ErrOwnershipExpired)
			c.mu.Unlock()
			if logDegraded {
				c.logDegraded()
			}
		},
	)
}

func (c *Coordinator) stopLeaseTimerLocked() {
	c.leaseVersion++
	if c.leaseTimer != nil {
		c.leaseTimer.Stop()
		c.leaseTimer = nil
	}
}

func (c *Coordinator) deactivate(cause error) {
	c.mu.Lock()
	logDegraded := c.deactivateLocked(cause)
	c.mu.Unlock()
	if logDegraded {
		c.logDegraded()
	}
}

func (c *Coordinator) deactivateObserved(proofDeadline time.Time) {
	c.mu.Lock()
	_ = c.deactivateLocked(nil)
	c.proofDeadline = proofDeadline
	c.mu.Unlock()
}

func (c *Coordinator) abandonCandidate(leaseExpiresAt time.Time, cause error) {
	c.mu.Lock()
	c.holderID = uuid.New()
	// Give another process one normal retry interval after this lease expires.
	c.acquireAfter = leaseExpiresAt.Add(c.config.RetryInterval)
	logDegraded := c.deactivateLocked(cause)
	c.mu.Unlock()
	if logDegraded {
		c.logDegraded()
	}
}

func (c *Coordinator) deactivateLocked(cause error) bool {
	wasActive := c.activeCtx != nil
	c.stopLeaseTimerLocked()
	if c.cancelActive != nil {
		c.cancelActive(cause)
		c.cancelActive = nil
		c.activeCtx = nil
	}
	if wasActive {
		c.signalStateChangedLocked()
	}
	c.ownership = Ownership{}
	c.updatedAt = time.Now()
	c.observed = cause == nil
	c.proofDeadline = time.Time{}
	if cause == nil {
		c.clearDegradedLocked()
	} else if !errors.Is(cause, context.Canceled) {
		return c.setDegradedLocked()
	}
	return false
}

func (c *Coordinator) setDegradedLocked() bool {
	if c.degraded {
		return false
	}
	c.degraded = true
	return true
}

func (c *Coordinator) logDegraded() {
	c.logger.Warn(
		"HA state is not optimal",
		haEventAttribute, haEventStateDegraded,
		"reason", string(ReasonControlPlaneUnavailable),
	)
}

func (c *Coordinator) clearDegradedLocked() {
	c.degraded = false
}

func (c *Coordinator) signalStateChangedLocked() {
	close(c.stateChanged)
	c.stateChanged = make(chan struct{})
}
