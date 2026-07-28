package ha

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

const (
	defaultActivationTimeout   = 4 * time.Second
	defaultHealthCheckInterval = 100 * time.Millisecond
	defaultCleanupTimeout      = 10 * time.Second
)

var errCriticalRuntimeUnhealthy = errors.New("critical Fleet runtime is unhealthy")

// HealthCheck reports one explicitly control-critical condition. Generic
// runtime-job freshness remains observational and must not demote Fleet.
type HealthCheck func() bool

type RuntimeConfig struct {
	ActivationTimeout   time.Duration
	HealthCheckInterval time.Duration
	CleanupTimeout      time.Duration
}

type runtimeOwner interface {
	Run(ctx context.Context) error
	WaitForActive(ctx context.Context) (context.Context, Token, error)
	RequestDemotion(cause error)
	ResumeAcquisition()
}

type runtimeGroup interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Status() runtimejobs.GroupStatus
	Err() error
}

// Runtime serializes lease ownership, the existing runtime-job group, and
// request admission. It deliberately does not supervise jobs individually.
type Runtime struct {
	owner        runtimeOwner
	group        runtimeGroup
	healthChecks []HealthCheck
	config       RuntimeConfig
	gate         *Gate
}

func NewRuntime(
	owner *Coordinator,
	group *runtimejobs.Group,
	healthChecks ...HealthCheck,
) (*Runtime, error) {
	if owner == nil {
		return nil, errors.New("HA runtime requires an ownership coordinator")
	}
	if group == nil {
		return nil, errors.New("HA runtime requires a runtime job group")
	}
	if len(healthChecks) == 0 {
		return nil, errors.New("HA runtime requires at least one critical health check")
	}
	for _, check := range healthChecks {
		if check == nil {
			return nil, errors.New("HA runtime critical health checks must not be nil")
		}
	}
	return newRuntime(owner, group, healthChecks, RuntimeConfig{}), nil
}

func NewStandaloneRuntime(group *runtimejobs.Group) (*Runtime, error) {
	if group == nil {
		return nil, errors.New("standalone runtime requires a runtime job group")
	}
	return newRuntime(nil, group, nil, RuntimeConfig{}), nil
}

func newRuntime(
	owner runtimeOwner,
	group runtimeGroup,
	healthChecks []HealthCheck,
	config RuntimeConfig,
) *Runtime {
	return &Runtime{
		owner:        owner,
		group:        group,
		healthChecks: healthChecks,
		config:       withRuntimeDefaults(config),
		gate:         newGate(),
	}
}

func newStandaloneRuntime(group runtimeGroup, config RuntimeConfig) *Runtime {
	return newRuntime(nil, group, nil, config)
}

func withRuntimeDefaults(config RuntimeConfig) RuntimeConfig {
	if config.ActivationTimeout <= 0 {
		config.ActivationTimeout = defaultActivationTimeout
	}
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
	if err := r.waitUntilHealthy(ctx); err != nil {
		stopErr := r.stopGroup()
		if ctx.Err() != nil {
			return stopErr
		}
		return errors.Join(err, stopErr)
	}
	r.gate.activate(ctx)
	activeErr := r.waitWhileHealthy(ctx, ctx)
	r.gate.deactivate()
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

	for {
		activeCtx, _, err := r.owner.WaitForActive(coordinatorCtx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			select {
			case coordinatorErr := <-coordinatorResult:
				return fmt.Errorf("run Fleet ownership coordinator: %w", coordinatorErr)
			default:
				return fmt.Errorf("wait for Fleet ownership: %w", err)
			}
		}

		if err := r.group.Start(activeCtx); err != nil {
			r.owner.RequestDemotion(fmt.Errorf("start Fleet runtime: %w", err))
			if terminalErr := r.group.Err(); terminalErr != nil {
				return fmt.Errorf("start Fleet runtime left terminal cleanup failure: %w", terminalErr)
			}
			r.owner.ResumeAcquisition()
			continue
		}

		if err := r.waitUntilHealthy(activeCtx); err != nil {
			if activeCtx.Err() == nil {
				r.owner.RequestDemotion(err)
			}
			if stopErr := r.stopGroup(); stopErr != nil {
				return stopErr
			}
			if ctx.Err() == nil {
				r.owner.ResumeAcquisition()
			}
			continue
		}

		r.gate.activate(activeCtx)
		activeErr := r.waitWhileHealthy(ctx, activeCtx)
		r.gate.deactivate()
		if errors.Is(activeErr, errCriticalRuntimeUnhealthy) {
			r.owner.RequestDemotion(activeErr)
		}
		if err := r.stopGroup(); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		r.owner.ResumeAcquisition()
	}
}

func (r *Runtime) waitUntilHealthy(activeCtx context.Context) error {
	timeout := time.NewTimer(r.config.ActivationTimeout)
	defer timeout.Stop()
	ticker := time.NewTicker(r.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		if r.healthy() {
			return nil
		}
		select {
		case <-activeCtx.Done():
			return fmt.Errorf("active lifetime ended during startup: %w", activeCtx.Err())
		case <-timeout.C:
			return fmt.Errorf(
				"%w after %s activation timeout",
				errCriticalRuntimeUnhealthy,
				r.config.ActivationTimeout,
			)
		case <-ticker.C:
		}
	}
}

func (r *Runtime) waitWhileHealthy(parent, activeCtx context.Context) error {
	ticker := time.NewTicker(r.config.HealthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-parent.Done():
			return fmt.Errorf("Fleet runtime stopped: %w", parent.Err())
		case <-activeCtx.Done():
			return fmt.Errorf("active lifetime ended: %w", activeCtx.Err())
		case <-ticker.C:
			if !r.healthy() {
				return errCriticalRuntimeUnhealthy
			}
		}
	}
}

func (r *Runtime) healthy() bool {
	status := r.group.Status()
	if status.State != runtimejobs.StateRunning || status.TerminalError != nil {
		return false
	}
	for _, check := range r.healthChecks {
		if !check() {
			return false
		}
	}
	return true
}

func (r *Runtime) stopGroup() error {
	stopCtx, cancel := context.WithTimeout(context.Background(), r.config.CleanupTimeout)
	defer cancel()
	if err := r.group.Stop(stopCtx); err != nil {
		return fmt.Errorf("stop Fleet runtime: %w", err)
	}
	return nil
}
