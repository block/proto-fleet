// Package curtailment: alert_metrics.go periodically translates MQTT
// curtailment-source state into emissions on the metrics contract declared in
// server/internal/infrastructure/metrics, feeding the default Grafana rules
// "Miners Curtailed by Curtailment Source" and "Curtailment Source Disconnected".
package curtailment

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/mqttingest"
	"github.com/block/proto-fleet/server/internal/infrastructure/metrics"
)

const defaultAlertMetricsInterval = 30 * time.Second

// AlertMetricsEmitter is the subset of metrics.Provider the loop depends on.
type AlertMetricsEmitter interface {
	EmitMQTTSourceConnected(ctx context.Context, labels metrics.MQTTSourceLabels, connected bool)
	EmitMQTTCurtailmentActive(ctx context.Context, labels metrics.MQTTSourceLabels, active bool)
}

// SourceRuntimeStatusProvider reports in-memory connection health for one
// source; implemented by mqttingest.Subscriber.
type SourceRuntimeStatusProvider interface {
	SourceRuntimeStatus(sourceID int64) mqttingest.RuntimeStatus
}

// EnabledSourcesLister is the slice of mqttingest.Store the loop needs.
type EnabledSourcesLister interface {
	ListEnabledSources(ctx context.Context) ([]mqttingest.SourceConfig, error)
}

// ActiveCurtailmentLister is the slice of the curtailment store the loop
// needs; implemented by sqlstores.SQLCurtailmentStore.
type ActiveCurtailmentLister interface {
	ListMQTTSourcesWithActiveCurtailment(ctx context.Context) ([]*models.MQTTSourceActiveCurtailment, error)
}

// AlertMetricsConfig bundles the loop's dependencies and tunables.
type AlertMetricsConfig struct {
	Sources           EnabledSourcesLister
	Runtime           SourceRuntimeStatusProvider
	ActiveCurtailment ActiveCurtailmentLister
	Emitter           AlertMetricsEmitter
	// Interval between emissions; zero uses the default.
	Interval time.Duration
	Logger   *slog.Logger
}

// AlertMetricsLoop is a singleton goroutine re-emitting the per-source gauges
// every Interval, so the alert rules' freshness windows stay populated while
// a condition holds and the series vanish once a source is removed.
type AlertMetricsLoop struct {
	cfg AlertMetricsConfig

	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.Mutex
	running bool

	// prevDisabledActive remembers disabled sources whose curtailment gauge
	// was emitted last tick, so a final 0 can clear the alert promptly when
	// their event ends. Touched only by the tick goroutine.
	prevDisabledActive map[int64]metrics.MQTTSourceLabels
}

// NewAlertMetricsLoop validates dependencies and applies defaults.
func NewAlertMetricsLoop(cfg AlertMetricsConfig) (*AlertMetricsLoop, error) {
	if cfg.Sources == nil {
		return nil, errors.New("curtailment alert metrics: Sources lister is required")
	}
	if cfg.Runtime == nil {
		return nil, errors.New("curtailment alert metrics: Runtime status provider is required")
	}
	if cfg.ActiveCurtailment == nil {
		return nil, errors.New("curtailment alert metrics: ActiveCurtailment lister is required")
	}
	if cfg.Emitter == nil {
		return nil, errors.New("curtailment alert metrics: Emitter is required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultAlertMetricsInterval
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &AlertMetricsLoop{cfg: cfg}, nil
}

// Start launches the tick loop; a second Start while running is a no-op.
// The loop runs until Stop.
func (l *AlertMetricsLoop) Start(_ context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running {
		return nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.running = true
	l.wg.Add(1)
	go l.tickLoop(runCtx)
	l.cfg.Logger.Info("curtailment alert metrics loop started", "interval", l.cfg.Interval)
	return nil
}

// Stop cancels the loop and waits for the in-flight tick to drain. The wait
// holds the mutex so a concurrent Start cannot reuse the WaitGroup mid-Wait;
// the tick goroutine never takes the mutex, so this cannot deadlock.
func (l *AlertMetricsLoop) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel == nil {
		return
	}
	l.cancel()
	l.cancel = nil
	l.running = false
	l.wg.Wait()
	l.cfg.Logger.Info("curtailment alert metrics loop stopped")
}

func (l *AlertMetricsLoop) tickLoop(ctx context.Context) {
	defer l.wg.Done()
	ticker := time.NewTicker(l.cfg.Interval)
	defer ticker.Stop()
	l.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.tick(ctx)
		}
	}
}

// tick emits one sample per source; a panic is contained so one bad tick
// cannot kill the loop.
//
// Error handling deliberately fails open: on a lookup error nothing is
// emitted and, if the errors persist past the rules' freshness window, the
// alerts resolve. Re-emitting cached values instead could mask a real
// disconnect or hold a stale curtailed alert with no bound, so the residual
// exposure (a partial DB failure hitting only these reads) is accepted; a
// full-DB outage stops metric ingest too and fires the ingest-stalled alert.
func (l *AlertMetricsLoop) tick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			l.cfg.Logger.Error("curtailment alert metrics tick panicked", "panic", r)
		}
	}()
	sources, err := l.cfg.Sources.ListEnabledSources(ctx)
	if err != nil {
		if ctx.Err() == nil {
			l.cfg.Logger.Error("curtailment alert metrics: list enabled sources failed", "error", err)
		}
		return
	}

	// Sourced from curtailment_event by external reference, so it covers
	// events whose rule or source was disabled after curtailment started.
	active, activeErr := l.cfg.ActiveCurtailment.ListMQTTSourcesWithActiveCurtailment(ctx)
	if activeErr != nil && ctx.Err() == nil {
		l.cfg.Logger.Error("curtailment alert metrics: list active curtailment failed", "error", activeErr)
	}
	activeBySourceID := make(map[int64]*models.MQTTSourceActiveCurtailment, len(active))
	for _, a := range active {
		if a != nil {
			activeBySourceID[a.SourceID] = a
		}
	}

	emittedActive := make(map[int64]struct{}, len(sources))
	for _, src := range sources {
		labels := metrics.MQTTSourceLabels{
			OrganizationID: metrics.OrgIDToLabel(src.OrganizationID),
			SourceName:     src.SourceName,
		}
		status := l.cfg.Runtime.SourceRuntimeStatus(src.ID)
		l.cfg.Emitter.EmitMQTTSourceConnected(ctx, labels, status.State == mqttingest.RuntimeStateRunning)
		if activeErr == nil {
			_, isActive := activeBySourceID[src.ID]
			l.cfg.Emitter.EmitMQTTCurtailmentActive(ctx, labels, isActive)
			emittedActive[src.ID] = struct{}{}
			delete(activeBySourceID, src.ID)
		}
	}

	if activeErr != nil {
		// State unknown: keep prevDisabledActive so the clearing 0 still
		// lands once the lookup recovers.
		return
	}
	// A source disabled mid-curtailment still has a live event; keep its
	// gauge high so the alert cannot resolve while miners stay curtailed.
	curDisabledActive := make(map[int64]metrics.MQTTSourceLabels, len(activeBySourceID))
	for _, a := range activeBySourceID {
		labels := metrics.MQTTSourceLabels{
			OrganizationID: metrics.OrgIDToLabel(a.OrganizationID),
			SourceName:     a.SourceName,
		}
		l.cfg.Emitter.EmitMQTTCurtailmentActive(ctx, labels, true)
		curDisabledActive[a.SourceID] = labels
		emittedActive[a.SourceID] = struct{}{}
	}
	// One clearing 0 for a disabled source whose event just ended, so the
	// alert resolves promptly instead of aging out of the rule window. Best
	// effort: a restart in between falls back to the age-out path.
	for id, labels := range l.prevDisabledActive {
		if _, emitted := emittedActive[id]; !emitted {
			l.cfg.Emitter.EmitMQTTCurtailmentActive(ctx, labels, false)
		}
	}
	l.prevDisabledActive = curDisabledActive
}
