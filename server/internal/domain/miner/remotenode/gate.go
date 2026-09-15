package remotenode

import (
	"context"
	"fmt"
	"sync"

	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
)

// DefaultPerNodeCommandLimit matches the Fleet Node's ordinary worker-pool ceiling.
const DefaultPerNodeCommandLimit = control.MaxConcurrentCommandsPerFleetNode

// DefaultPerNodeDeferrableReadLimit mirrors the node's lower ceiling for
// retryable reads so Fleet paces those calls before they reach ControlStream.
const DefaultPerNodeDeferrableReadLimit = control.MaxConcurrentDeferrableReadsPerFleetNode

// DefaultPerNodeLogDownloadLimit matches the gateway's per-node command artifact
// upload capacity so same-node log batches wait server-side instead of overrunning
// UploadCommandArtifact stream admission.
const DefaultPerNodeLogDownloadLimit = control.MaxConcurrentCommandArtifactUploadsPerFleetNode

// Gate bounds concurrent commands to a single fleet node.
type Gate interface {
	Acquire(ctx context.Context, fleetNodeID int64) (release func(), err error)
}

// NestedGate acquires two per-node gates in order and rolls back a partial
// acquisition. It lets deferrable reads consume both their lane and the shared
// command capacity with one idempotent release closure.
type NestedGate struct {
	first  Gate
	second Gate
}

func NewNestedGate(first, second Gate) *NestedGate {
	return &NestedGate{first: first, second: second}
}

func (g *NestedGate) Acquire(ctx context.Context, fleetNodeID int64) (func(), error) {
	firstRelease, err := g.first.Acquire(ctx, fleetNodeID)
	if err != nil {
		return nil, err
	}
	secondRelease, err := g.second.Acquire(ctx, fleetNodeID)
	if err != nil {
		firstRelease()
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			secondRelease()
			firstRelease()
		})
	}, nil
}

// PerNodeLimiter is a keyed counting semaphore (up to limit per fleet_node id). Safe for
// concurrent use; the per-node map is bounded by the fleet-node count and not reclaimed.
type PerNodeLimiter struct {
	limit int
	mu    sync.Mutex
	sems  map[int64]chan struct{}
}

func NewPerNodeLimiter(limit int) *PerNodeLimiter {
	if limit <= 0 {
		limit = DefaultPerNodeCommandLimit
	}
	return &PerNodeLimiter{limit: limit, sems: make(map[int64]chan struct{})}
}

func (l *PerNodeLimiter) semFor(fleetNodeID int64) chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.sems[fleetNodeID]
	if s == nil {
		s = make(chan struct{}, l.limit)
		l.sems[fleetNodeID] = s
	}
	return s
}

func (l *PerNodeLimiter) Acquire(ctx context.Context, fleetNodeID int64) (func(), error) {
	s := l.semFor(fleetNodeID)
	select {
	case s <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-s }) }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for fleet node command slot: %w", ctx.Err())
	}
}
