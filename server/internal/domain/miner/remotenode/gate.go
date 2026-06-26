package remotenode

import (
	"context"
	"fmt"
	"sync"

	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
)

// DefaultPerNodeCommandLimit caps in-flight commands to one fleet node, held below the
// node's worker-pool ceiling so a large batch is paced here (the DB queue holds the
// backlog) rather than oversubscribing the node and being rejected BUSY.
const DefaultPerNodeCommandLimit = 8

// DefaultPerNodeLogDownloadLimit matches the gateway's per-node command artifact
// upload capacity so same-node log batches wait server-side instead of overrunning
// UploadCommandArtifact stream admission.
const DefaultPerNodeLogDownloadLimit = control.MaxConcurrentCommandArtifactUploadsPerFleetNode

// Gate bounds concurrent commands to a single fleet node.
type Gate interface {
	Acquire(ctx context.Context, fleetNodeID int64) (release func(), err error)
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
