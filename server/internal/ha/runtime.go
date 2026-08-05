package ha

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

const (
	defaultHealthCheckInterval = 100 * time.Millisecond
	defaultCleanupTimeout      = 10 * time.Second
)

// ErrRuntimeAborted marks an HA exit that completed its bounded hard-abort
// cleanup attempt for the active runtime job group.
var ErrRuntimeAborted = errors.New("HA Fleet runtime aborted")

var errCriticalRuntimeUnhealthy = errors.New("critical Fleet runtime is unhealthy")

type RuntimeConfig struct {
	HealthCheckInterval time.Duration
	CleanupTimeout      time.Duration
}

type runtimeOwner interface {
	Run(ctx context.Context) error
	WaitForActive(ctx context.Context) (context.Context, Token, error)
}

type runtimeGroup interface {
	Start(ctx context.Context) error
	Abort(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Runtime serializes lease ownership, the existing runtime-job group, and
// request admission. It deliberately does not supervise jobs individually.
type Runtime struct {
	owner       runtimeOwner
	group       runtimeGroup
	healthCheck func() bool
	config      RuntimeConfig
	gate        *Gate
}

func NewRuntime(
	owner *Coordinator,
	group *runtimejobs.Group,
	healthy func() bool,
) (*Runtime, error) {
	if owner == nil {
		return nil, errors.New("HA runtime requires an ownership coordinator")
	}
	if group == nil {
		return nil, errors.New("HA runtime requires a runtime job group")
	}
	if healthy == nil {
		return nil, errors.New("HA runtime requires a critical health check")
	}
	return newRuntime(owner, group, healthy, RuntimeConfig{}), nil
}

func NewStandaloneRuntime(
	group *runtimejobs.Group,
	healthy func() bool,
) (*Runtime, error) {
	if group == nil {
		return nil, errors.New("standalone runtime requires a runtime job group")
	}
	if healthy == nil {
		return nil, errors.New("standalone runtime requires a critical health check")
	}
	return newRuntime(nil, group, healthy, RuntimeConfig{}), nil
}

func newRuntime(
	owner runtimeOwner,
	group runtimeGroup,
	healthy func() bool,
	config RuntimeConfig,
) *Runtime {
	return &Runtime{
		owner:       owner,
		group:       group,
		healthCheck: healthy,
		config:      withRuntimeDefaults(config),
		gate:        newGate(),
	}
}

func withRuntimeDefaults(config RuntimeConfig) RuntimeConfig {
	if config.HealthCheckInterval <= 0 {
		config.HealthCheckInterval = defaultHealthCheckInterval
	}
	if config.CleanupTimeout <= 0 {
		config.CleanupTimeout = defaultCleanupTimeout
	}
	return config
}

func (r *Runtime) Active() bool {
	return r.gate.Active()
}

func (r *Runtime) Admit(ctx context.Context) (context.Context, func(), error) {
	return r.gate.Admit(ctx)
}

func (r *Runtime) Run(ctx context.Context) error {
	if r.owner == nil {
		return r.runStandalone(ctx)
	}
	return r.runHA(ctx)
}

func (r *Runtime) runStandalone(ctx context.Context) error {
	if err := r.group.Start(ctx); err != nil {
		return fmt.Errorf("start standalone Fleet runtime: %w", err)
	}
	if !r.healthCheck() {
		stopErr := r.stopGroup()
		if ctx.Err() != nil {
			return stopErr
		}
		return errors.Join(errCriticalRuntimeUnhealthy, stopErr)
	}
	r.gate.activate(ctx)
	activeErr := r.waitWhileHealthy(ctx, ctx)
	_ = r.gate.deactivate()
	stopErr := r.stopGroup()
	if ctx.Err() != nil {
		return stopErr
	}
	return errors.Join(activeErr, stopErr)
}

func (r *Runtime) runHA(ctx context.Context) error {
	coordinatorCtx, cancelCoordinator := context.WithCancel(ctx)
	defer cancelCoordinator()

	coordinatorResult := make(chan error, 1)
	go func() {
		coordinatorResult <- r.owner.Run(coordinatorCtx)
	}()

	type activationResult struct {
		ctx context.Context //nolint:containedctx // Carries the owned lifetime returned by WaitForActive.
		err error
	}
	activation := make(chan activationResult, 1)
	go func() {
		activeCtx, _, err := r.owner.WaitForActive(coordinatorCtx)
		activation <- activationResult{ctx: activeCtx, err: err}
	}()

	var activeCtx context.Context
	select {
	case result := <-activation:
		activeCtx = result.ctx
		if result.err != nil {
			if ctx.Err() != nil {
				return nil
			}
			select {
			case coordinatorErr := <-coordinatorResult:
				return fmt.Errorf("run Fleet ownership coordinator: %w", coordinatorErr)
			default:
				return fmt.Errorf("wait for Fleet ownership: %w", result.err)
			}
		}
	case coordinatorErr := <-coordinatorResult:
		return fmt.Errorf("run Fleet ownership coordinator: %w", coordinatorErr)
	case <-ctx.Done():
		return nil
	}

	if err := r.group.Start(activeCtx); err != nil {
		return fmt.Errorf("start active Fleet runtime: %w", err)
	}
	if !r.healthCheck() {
		return r.abortGroup(errCriticalRuntimeUnhealthy)
	}

	r.gate.activate(activeCtx)
	activeErr := r.waitWhileHealthy(ctx, activeCtx)
	admissionDrained := r.gate.deactivate()
	if ctx.Err() != nil {
		return r.stopGroupAndDrainAdmissions(admissionDrained)
	}
	abortErr := r.abortGroup(activeErr)

	select {
	case coordinatorErr := <-coordinatorResult:
		return errors.Join(abortErr, coordinatorErr)
	default:
		return abortErr
	}
}

func (r *Runtime) abortGroup(cause error) error {
	abortCtx, cancel := context.WithTimeout(context.Background(), r.config.CleanupTimeout)
	defer cancel()
	cleanupErr := r.group.Abort(abortCtx)
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("abort Fleet runtime: %w", cleanupErr)
	}
	return errors.Join(ErrRuntimeAborted, cause, cleanupErr)
}

func (r *Runtime) waitWhileHealthy(parent, activeCtx context.Context) error {
	ticker := time.NewTicker(r.config.HealthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-parent.Done():
			return fmt.Errorf("Fleet runtime stopped: %w", parent.Err())
		case <-activeCtx.Done():
			return fmt.Errorf("active lifetime ended: %w", context.Cause(activeCtx))
		case <-ticker.C:
			if !r.healthCheck() {
				return errCriticalRuntimeUnhealthy
			}
		}
	}
}

func (r *Runtime) stopGroup() error {
	stopCtx, cancel := context.WithTimeout(context.Background(), r.config.CleanupTimeout)
	defer cancel()
	return r.stopGroupWithContext(stopCtx)
}

func (r *Runtime) stopGroupAndDrainAdmissions(admissionDrained <-chan struct{}) error {
	stopCtx, cancel := context.WithTimeout(context.Background(), r.config.CleanupTimeout)
	defer cancel()
	if err := r.stopGroupWithContext(stopCtx); err != nil {
		return err
	}
	select {
	case <-admissionDrained:
		if err := stopCtx.Err(); err != nil {
			return fmt.Errorf("drain active Fleet requests: %w", err)
		}
		return nil
	case <-stopCtx.Done():
		return fmt.Errorf("drain active Fleet requests: %w", stopCtx.Err())
	}
}

func (r *Runtime) stopGroupWithContext(stopCtx context.Context) error {
	if err := r.group.Stop(stopCtx); err != nil {
		return fmt.Errorf("stop Fleet runtime: %w", err)
	}
	return nil
}
