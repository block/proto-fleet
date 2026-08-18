// Package readiness periodically emits the existing fleet-ha failover-ready
// result into the alert metrics pipeline.
package readiness

import (
	"context"
	"log/slog"
	"time"

	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

const interval = 30 * time.Second

type Check func(context.Context) (bool, error)

type Emitter interface {
	EmitHAFailoverReady(ctx context.Context, ready bool)
}

type Collector struct {
	check   Check
	emitter Emitter
}

func New(check Check, emitter Emitter) *Collector {
	return &Collector{check: check, emitter: emitter}
}

func (c *Collector) Run(ctx context.Context) {
	reportProgress := runtimejobs.TrackProgress(ctx, interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	c.collectOnce(ctx)
	reportProgress()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collectOnce(ctx)
			reportProgress()
		}
	}
}

func (c *Collector) collectOnce(ctx context.Context) {
	ready, err := c.check(ctx)
	if err != nil {
		slog.Warn("HA readiness check failed", "error", err)
		ready = false
	}
	c.emitter.EmitHAFailoverReady(ctx, ready)
}
