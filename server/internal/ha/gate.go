package ha

import (
	"context"
	"errors"
	"sync"
)

var ErrNotActive = errors.New("Fleet is not active")

// Gate admits work only during a healthy active runtime lifetime.
type Gate struct {
	mu           sync.RWMutex
	activeCtx    context.Context //nolint:containedctx // The context is the lifetime this gate represents.
	cancelActive context.CancelFunc
}

func newGate() *Gate {
	return &Gate{}
}

func (g *Gate) Active() bool {
	g.mu.RLock()
	activeCtx := g.activeCtx
	g.mu.RUnlock()
	return activeCtx != nil && activeCtx.Err() == nil
}

// Admit binds a request to the current active lifetime. The returned release
// function must be called when the request finishes.
func (g *Gate) Admit(ctx context.Context) (context.Context, func(), error) {
	g.mu.RLock()
	activeCtx := g.activeCtx
	g.mu.RUnlock()
	if activeCtx == nil || activeCtx.Err() != nil {
		return nil, nil, ErrNotActive
	}

	requestCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(activeCtx, cancel)
	if activeCtx.Err() != nil {
		stop()
		cancel()
		return nil, nil, ErrNotActive
	}
	return requestCtx, func() {
		stop()
		cancel()
	}, nil
}

func (g *Gate) activate(ctx context.Context) {
	g.mu.Lock()
	if g.cancelActive != nil {
		g.cancelActive()
	}
	g.activeCtx, g.cancelActive = context.WithCancel(ctx)
	g.mu.Unlock()
}

func (g *Gate) deactivate() {
	g.mu.Lock()
	if g.cancelActive != nil {
		g.cancelActive()
		g.cancelActive = nil
	}
	g.activeCtx = nil
	g.mu.Unlock()
}
