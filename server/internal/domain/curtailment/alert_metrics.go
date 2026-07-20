// Package curtailment: alert_metrics.go periodically translates MQTT
// curtailment-source state into emissions on the metrics contract declared in
// server/internal/infrastructure/metrics, feeding the default Grafana rules
// "Curtailment Active" and "Curtailment Source Unreachable".
package curtailment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/mqttingest"
	"github.com/block/proto-fleet/server/internal/infrastructure/metrics"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
)

// 10s keeps alert latency close to the source signal: curtailment engages
// immediately, and the Grafana curtailment rule group also evaluates at 10s.
const defaultAlertMetricsInterval = 10 * time.Second

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

	cancel  context.CancelFunc
	runDone <-chan struct{}

	lifecycleMu sync.Mutex
	mu          sync.Mutex
	stopping    bool

	// prevConnected / prevActive remember the labels each gauge was emitted
	// with last tick (touched only by the tick goroutine), so a renamed or
	// retired series gets one clearing sample instead of keeping its alert
	// firing until the last sample ages out of the rule window.
	prevConnected map[int64]metrics.MQTTSourceLabels
	prevActive    map[int64]metrics.MQTTSourceLabels
}

var _ runtimejobs.Lifecycle = (*AlertMetricsLoop)(nil)

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
func (l *AlertMetricsLoop) Start(ctx context.Context) error {
	l.lifecycleMu.Lock()
	defer l.lifecycleMu.Unlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stopping {
		return errors.New("curtailment alert metrics: previous activation is still stopping")
	}
	if l.cancel != nil {
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	runDone := make(chan struct{})
	l.cancel = cancel
	l.runDone = runDone
	go l.tickLoop(runCtx, runDone)
	l.cfg.Logger.Info("curtailment alert metrics loop started", "interval", l.cfg.Interval)
	return nil
}

// Stop cancels the loop and waits for the in-flight tick within ctx.
// A timed-out activation remains owned, preventing a replacement loop from
// overlapping work that ignored cancellation.
func (l *AlertMetricsLoop) Stop(ctx context.Context) error {
	l.lifecycleMu.Lock()
	defer l.lifecycleMu.Unlock()
	l.mu.Lock()
	if l.cancel == nil {
		l.mu.Unlock()
		return nil
	}
	l.stopping = true
	cancel := l.cancel
	runDone := l.runDone
	l.mu.Unlock()
	cancel()
	select {
	case <-runDone:
		l.cfg.Logger.Info("curtailment alert metrics loop stopped")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("curtailment alert metrics: stop: %w", ctx.Err())
	}
}

func (l *AlertMetricsLoop) finishActivation() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cancel = nil
	l.runDone = nil
	l.stopping = false
}

func (l *AlertMetricsLoop) tickLoop(ctx context.Context, runDone chan<- struct{}) {
	defer close(runDone)
	defer l.finishActivation()
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
func (l *AlertMetricsLoop) tick(parent context.Context) {
	defer func() {
		if r := recover(); r != nil {
			l.cfg.Logger.Error("curtailment alert metrics tick panicked", "panic", r)
		}
	}()
	// Bound the tick (mirroring reconciler.runTick) so one hung query cannot
	// stall every future tick and silently resolve the alerts via age-out.
	ctx, cancel := context.WithTimeout(parent, 2*l.cfg.Interval)
	defer cancel()
	sources, err := l.cfg.Sources.ListEnabledSources(ctx)
	if err != nil {
		// parent, not ctx: a tick timeout must be logged; only shutdown is quiet.
		if parent.Err() == nil {
			l.cfg.Logger.Error("curtailment alert metrics: list enabled sources failed", "error", err)
		}
		return
	}

	// Sourced from curtailment_event by external reference, so it covers
	// events whose rule or source was disabled after curtailment started.
	active, activeErr := l.cfg.ActiveCurtailment.ListMQTTSourcesWithActiveCurtailment(ctx)
	if activeErr != nil && parent.Err() == nil {
		l.cfg.Logger.Error("curtailment alert metrics: list active curtailment failed", "error", activeErr)
	}
	activeBySourceID := make(map[int64]*models.MQTTSourceActiveCurtailment, len(active))
	for _, a := range active {
		if a != nil {
			activeBySourceID[a.SourceID] = a
		}
	}

	curConnected := make(map[int64]metrics.MQTTSourceLabels, len(sources))
	curActive := make(map[int64]metrics.MQTTSourceLabels, len(sources)+len(activeBySourceID))
	for _, src := range sources {
		labels := metrics.MQTTSourceLabels{
			OrganizationID: metrics.OrgIDToLabel(src.OrganizationID),
			SourceName:     src.SourceName,
		}
		status := l.cfg.Runtime.SourceRuntimeStatus(src.ID)
		l.cfg.Emitter.EmitMQTTSourceConnected(ctx, labels, status.State == mqttingest.RuntimeStateRunning)
		curConnected[src.ID] = labels
		if activeErr == nil {
			_, isActive := activeBySourceID[src.ID]
			l.cfg.Emitter.EmitMQTTCurtailmentActive(ctx, labels, isActive)
			curActive[src.ID] = labels
			delete(activeBySourceID, src.ID)
		}
	}
	// A renamed source changes the series identity (kind label) and a
	// disabled/removed source stops being monitored: either way, retire the
	// old series with one non-alerting sample so its alert resolves now
	// instead of firing for a dead series until the window ages it out.
	for id, old := range l.prevConnected {
		if cur, ok := curConnected[id]; !ok || cur != old {
			l.cfg.Emitter.EmitMQTTSourceConnected(ctx, old, true)
		}
	}
	l.prevConnected = curConnected

	if activeErr != nil {
		// State unknown: keep prevActive so pending clearing samples still
		// land once the lookup recovers.
		return
	}
	// A source disabled mid-curtailment still has a live event; keep its
	// gauge high so the alert cannot resolve while miners stay curtailed.
	for _, a := range activeBySourceID {
		labels := metrics.MQTTSourceLabels{
			OrganizationID: metrics.OrgIDToLabel(a.OrganizationID),
			SourceName:     a.SourceName,
		}
		l.cfg.Emitter.EmitMQTTCurtailmentActive(ctx, labels, true)
		curActive[a.SourceID] = labels
	}
	// One clearing 0 when a series is renamed or retires (a disabled source's
	// event ended), so the alert resolves promptly instead of aging out. Best
	// effort: a restart in between falls back to the age-out path.
	for id, old := range l.prevActive {
		if cur, ok := curActive[id]; !ok || cur != old {
			l.cfg.Emitter.EmitMQTTCurtailmentActive(ctx, old, false)
		}
	}
	l.prevActive = curActive
}
