package ha

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type State string

const (
	StatePassive State = "passive"
	StateActive  State = "active"
)

type CoordinatorConfig struct {
	LeaseDuration time.Duration
	RenewInterval time.Duration
	RetryInterval time.Duration
}

// Snapshot is a non-authoritative view of the coordinator's current state.
type Snapshot struct {
	State        State
	HolderID     uuid.UUID
	DCSClusterID string
	Token        Token
	ExpiresAt    time.Time
	LastError    string
	UpdatedAt    time.Time
}

type Coordinator struct {
	observer writerObserver
	store    ownershipStore
	config   CoordinatorConfig
	holderID uuid.UUID

	mu           sync.RWMutex
	ownership    Ownership
	activeCtx    context.Context //nolint:containedctx // The coordinator owns this explicit active-lifetime context.
	cancelActive context.CancelFunc
	leaseTimer   *time.Timer
	leaseVersion uint64
	lastError    string
	updatedAt    time.Time
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
	now := time.Now()
	return &Coordinator{
		observer:  observer,
		store:     store,
		config:    config,
		holderID:  holderID,
		updatedAt: now,
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
	return c.holderID
}

func (c *Coordinator) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	snapshot := Snapshot{
		State:     StatePassive,
		HolderID:  c.holderID,
		LastError: c.lastError,
		UpdatedAt: c.updatedAt,
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

// Run continuously observes and renews until its parent context ends. Failures
// demote immediately and are retried while remaining passive.
func (c *Coordinator) Run(ctx context.Context) error {
	for {
		err := c.step(ctx)
		delay := c.config.RetryInterval
		if err == nil && c.Snapshot().State == StateActive {
			delay = c.config.RenewInterval
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			c.deactivate(ctx.Err())
			return fmt.Errorf("HA coordinator stopped: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (c *Coordinator) step(ctx context.Context) error {
	c.mu.RLock()
	activeCtx := c.activeCtx
	current := c.ownership
	c.mu.RUnlock()

	stepCtx := activeCtx
	if stepCtx == nil {
		var cancel context.CancelFunc
		stepCtx, cancel = context.WithTimeout(ctx, c.config.LeaseDuration)
		defer cancel()
	}

	if activeCtx == nil {
		var (
			ownership      Ownership
			requestStarted time.Time
		)
		observed, err := c.observer.ObserveAndRun(
			stepCtx,
			func(actionCtx context.Context, observed WriterObservation) error {
				requestStarted = time.Now()
				var acquireErr error
				ownership, acquireErr = c.store.Acquire(
					actionCtx,
					observed,
					c.holderID,
					c.config.LeaseDuration,
				)
				return acquireErr
			},
		)
		if err != nil {
			c.deactivate(err)
			return err
		}
		if err := c.activate(
			ctx,
			ownership,
			requestStarted,
			observed.DCSProofDeadline,
		); err != nil {
			c.deactivate(err)
			return err
		}
		return nil
	}

	var (
		renewed        Ownership
		requestStarted time.Time
	)
	observed, err := c.observer.ObserveAndRun(
		stepCtx,
		func(actionCtx context.Context, observed WriterObservation) error {
			if observed.DCSClusterID != current.DCSClusterID ||
				observed.WriterGeneration != current.Token.WriterGeneration {
				return fmt.Errorf(
					"%w: held %s@%d, observed %s@%d",
					ErrWriterChanged,
					current.DCSClusterID,
					current.Token.WriterGeneration,
					observed.DCSClusterID,
					observed.WriterGeneration,
				)
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
	defer c.mu.Unlock()
	if err := parent.Err(); err != nil {
		return fmt.Errorf("activate HA coordinator: %w", err)
	}
	if c.cancelActive != nil {
		c.cancelActive()
	}
	c.activeCtx, c.cancelActive = context.WithCancel(parent)
	c.ownership = ownership
	c.lastError = ""
	c.updatedAt = time.Now()
	c.resetLeaseTimerLocked(deadline)
	return nil
}

func (c *Coordinator) updateActive(
	ownership Ownership,
	expected Ownership,
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
	defer c.mu.Unlock()
	if c.activeCtx == nil ||
		c.ownership.DCSClusterID != expected.DCSClusterID ||
		c.ownership.Token != expected.Token ||
		c.ownership.HolderID != expected.HolderID {
		return ErrOwnershipLost
	}
	c.ownership = ownership
	c.updatedAt = time.Now()
	c.lastError = ""
	c.resetLeaseTimerLocked(deadline)
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
			defer c.mu.Unlock()
			if c.activeCtx == nil || c.leaseVersion != version {
				return
			}
			c.deactivateLocked(ErrOwnershipExpired)
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
	defer c.mu.Unlock()
	c.deactivateLocked(cause)
}

func (c *Coordinator) deactivateLocked(cause error) {
	c.stopLeaseTimerLocked()
	if c.cancelActive != nil {
		c.cancelActive()
		c.cancelActive = nil
		c.activeCtx = nil
	}
	c.ownership = Ownership{}
	c.updatedAt = time.Now()
	if cause != nil {
		c.lastError = cause.Error()
	} else {
		c.lastError = ""
	}
}
