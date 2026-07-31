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

	mu            sync.RWMutex
	ownership     Ownership
	activeCtx     context.Context //nolint:containedctx // The coordinator owns this explicit active-lifetime context.
	cancelActive  context.CancelFunc
	leaseTimer    *time.Timer
	leaseVersion  uint64
	stateChanged  chan struct{}
	acquirePaused bool
	lastError     string
	updatedAt     time.Time
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
		observer:     observer,
		store:        store,
		config:       config,
		holderID:     holderID,
		stateChanged: make(chan struct{}),
		updatedAt:    now,
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

// RequestDemotion stops renewal, cancels the current active lifetime, and
// pauses acquisition until runtime cleanup succeeds.
func (c *Coordinator) RequestDemotion(cause error) {
	c.deactivate(cause)
}

// ResumeAcquisition allows a new active lifetime after runtime cleanup.
func (c *Coordinator) ResumeAcquisition() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.acquirePaused {
		return
	}
	c.acquirePaused = false
	c.signalStateChangedLocked()
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
		c.mu.RLock()
		var activeDone <-chan struct{}
		if c.activeCtx != nil {
			activeDone = c.activeCtx.Done()
		}
		stateChanged := c.stateChanged
		c.mu.RUnlock()
		select {
		case <-ctx.Done():
			timer.Stop()
			c.deactivate(ctx.Err())
			return fmt.Errorf("HA coordinator stopped: %w", ctx.Err())
		case <-activeDone:
			timer.Stop()
		case <-stateChanged:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (c *Coordinator) step(ctx context.Context) error {
	c.mu.RLock()
	activeCtx := c.activeCtx
	current := c.ownership
	holderID := c.holderID
	acquirePaused := c.acquirePaused
	c.mu.RUnlock()

	if activeCtx == nil && acquirePaused {
		return nil
	}

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
					holderID,
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
	c.acquirePaused = false
	c.lastError = ""
	c.updatedAt = time.Now()
	c.resetLeaseTimerLocked(deadline)
	c.signalStateChangedLocked()
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
	wasActive := c.activeCtx != nil
	c.stopLeaseTimerLocked()
	if c.cancelActive != nil {
		c.cancelActive()
		c.cancelActive = nil
		c.activeCtx = nil
	}
	if wasActive {
		c.holderID = uuid.New()
		c.acquirePaused = true
		c.signalStateChangedLocked()
	}
	c.ownership = Ownership{}
	c.updatedAt = time.Now()
	if cause != nil {
		c.lastError = cause.Error()
	} else {
		c.lastError = ""
	}
}

func (c *Coordinator) signalStateChangedLocked() {
	close(c.stateChanged)
	c.stateChanged = make(chan struct{})
}
