package ha

import (
	"context"
	"errors"
	"sync"
)

var ErrNotActive = errors.New("Fleet is not active")

// Gate admits work only during a healthy active runtime lifetime.
type Gate struct {
	mu     sync.RWMutex
	active *gateActivation
}

type gateActivation struct {
	ctx        context.Context //nolint:containedctx // The context is the lifetime this activation represents.
	cancel     context.CancelFunc
	admitting  bool
	admissions int
	drained    chan struct{}
}

func newGate() *Gate {
	return &Gate{}
}

func (g *Gate) Active() bool {
	g.mu.RLock()
	active := g.active
	isActive := active != nil && active.admitting && active.ctx.Err() == nil
	g.mu.RUnlock()
	return isActive
}

// Admit binds a request to the current active lifetime. The returned release
// function must be called when the request finishes.
func (g *Gate) Admit(ctx context.Context) (context.Context, func(), error) {
	g.mu.Lock()
	active := g.active
	if active == nil || !active.admitting || active.ctx.Err() != nil {
		g.mu.Unlock()
		return nil, nil, ErrNotActive
	}
	active.admissions++
	g.mu.Unlock()

	requestCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(active.ctx, cancel)
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			stop()
			cancel()
			g.release(active)
		})
	}
	if active.ctx.Err() != nil {
		release()
		return nil, nil, ErrNotActive
	}
	return requestCtx, release, nil
}

func (g *Gate) activate(ctx context.Context) {
	activeCtx, cancelActive := context.WithCancel(ctx)
	active := &gateActivation{
		ctx:       activeCtx,
		cancel:    cancelActive,
		admitting: true,
		drained:   make(chan struct{}),
	}

	g.mu.Lock()
	if g.active != nil {
		g.active.admitting = false
		g.active.cancel()
		g.closeIfDrained(g.active)
	}
	g.active = active
	g.mu.Unlock()
}

// deactivate closes admission for the current activation and cancels all
// admitted contexts. The returned channel closes after every admitted caller
// has released its activation.
func (g *Gate) deactivate() <-chan struct{} {
	g.mu.Lock()
	active := g.active
	if active == nil {
		g.mu.Unlock()
		drained := make(chan struct{})
		close(drained)
		return drained
	}

	g.active = nil
	active.admitting = false
	active.cancel()
	g.closeIfDrained(active)
	g.mu.Unlock()
	return active.drained
}

func (g *Gate) release(active *gateActivation) {
	g.mu.Lock()
	active.admissions--
	g.closeIfDrained(active)
	g.mu.Unlock()
}

// closeIfDrained must be called while g.mu is held.
func (g *Gate) closeIfDrained(active *gateActivation) {
	if !active.admitting && active.admissions == 0 {
		select {
		case <-active.drained:
		default:
			close(active.drained)
		}
	}
}
