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

// Snapshot is an in-memory, non-authoritative view for future runtime/status
// wiring. This PR intentionally does not expose it over HTTP.
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
	snapshot     Snapshot
	ownership    Ownership
	activeCtx    context.Context //nolint:containedctx // The coordinator owns this explicit active-lifetime context.
	cancelActive context.CancelFunc
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
		observer: observer,
		store:    store,
		config:   config,
		holderID: holderID,
		snapshot: Snapshot{
			State:     StatePassive,
			HolderID:  holderID,
			UpdatedAt: now,
		},
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
	return c.snapshot
}

// ActiveLifetime returns the context and token for the current active term.
// The context is canceled before the snapshot transitions back to passive.
func (c *Coordinator) ActiveLifetime() (context.Context, Token, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.snapshot.State != StateActive || c.activeCtx == nil {
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
			if !timer.Stop() {
				<-timer.C
			}
			c.deactivate(ctx.Err())
			return fmt.Errorf("HA coordinator stopped: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (c *Coordinator) step(ctx context.Context) error {
	observed, err := c.observer.Observe(ctx)
	if err != nil {
		c.deactivate(err)
		return err
	}

	c.mu.RLock()
	active := c.snapshot.State == StateActive
	current := c.ownership
	c.mu.RUnlock()

	if !active {
		ownership, err := c.store.Acquire(
			ctx,
			observed,
			c.holderID,
			c.config.LeaseDuration,
		)
		if err != nil {
			c.deactivate(err)
			return err
		}
		c.activate(ctx, ownership)
		return nil
	}

	if observed.DCSClusterID != current.DCSClusterID ||
		observed.WriterGeneration != current.Token.WriterGeneration {
		err := fmt.Errorf(
			"%w: held %s@%d, observed %s@%d",
			ErrWriterChanged,
			current.DCSClusterID,
			current.Token.WriterGeneration,
			observed.DCSClusterID,
			observed.WriterGeneration,
		)
		c.deactivate(err)
		return err
	}
	renewed, err := c.store.Renew(ctx, current, c.config.LeaseDuration)
	if err != nil {
		c.deactivate(err)
		return err
	}
	c.updateActive(renewed)
	return nil
}

func (c *Coordinator) activate(parent context.Context, ownership Ownership) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancelActive != nil {
		c.cancelActive()
	}
	c.activeCtx, c.cancelActive = context.WithCancel(parent)
	c.ownership = ownership
	c.snapshot = Snapshot{
		State:        StateActive,
		HolderID:     c.holderID,
		DCSClusterID: ownership.DCSClusterID,
		Token:        ownership.Token,
		ExpiresAt:    ownership.ExpiresAt,
		UpdatedAt:    time.Now(),
	}
}

func (c *Coordinator) updateActive(ownership Ownership) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ownership = ownership
	c.snapshot.ExpiresAt = ownership.ExpiresAt
	c.snapshot.UpdatedAt = time.Now()
	c.snapshot.LastError = ""
}

func (c *Coordinator) deactivate(cause error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancelActive != nil {
		c.cancelActive()
		c.cancelActive = nil
		c.activeCtx = nil
	}
	c.ownership = Ownership{}
	c.snapshot.State = StatePassive
	c.snapshot.DCSClusterID = ""
	c.snapshot.Token = Token{}
	c.snapshot.ExpiresAt = time.Time{}
	c.snapshot.UpdatedAt = time.Now()
	if cause != nil {
		c.snapshot.LastError = cause.Error()
	} else {
		c.snapshot.LastError = ""
	}
}
