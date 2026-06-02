package mqttingest

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// sourceWorker owns the runtime for one MQTT source: two broker
// clients, an in-memory observation cache for precedence dedup, the
// edge-detector state, and the watchdog ticker. One goroutine per
// worker — message handlers feed into a single channel that the
// worker's main loop drains.
type sourceWorker struct {
	cfg           Config
	source        SourceConfig
	primaryHost   string
	secondaryHost string
	password      string

	mu      sync.Mutex
	lastObs map[BrokerRole]*Observation
}

// observation arrives via the broker handlers into the worker's
// inbound channel. broker carries the host the message came from so
// the worker can tag it with the right BrokerRole.
type observation struct {
	broker     string
	payload    []byte
	receivedAt time.Time
}

// observationChannelBuffer bounds the per-source inbound message queue.
// At the ~30 s publisher cadence across two brokers, dispatch normally
// drains far faster than messages arrive; the buffer only needs to
// absorb a transient slow Service.Start/Stop on an actual edge. When it
// saturates, the just-arrived message is dropped with a Warn — the
// publisher's repeated sends plus the staleness watchdog are the
// backstop, so a dropped edge self-corrects on the next message rather
// than sticking.
const observationChannelBuffer = 256

func (w *sourceWorker) run(ctx context.Context) {
	w.lastObs = make(map[BrokerRole]*Observation)

	state, err := w.cfg.Store.GetSourceState(ctx, w.source.ID)
	if err != nil {
		if !errors.Is(err, ErrSourceStateNotFound) {
			// A transient read error must not silently kill the worker —
			// that would take the fail-safe watchdog down with it. Degrade
			// to cold-start; the watchdog then fires OFF on its first tick
			// (curtail-under-uncertainty) until a live message arrives.
			w.cfg.Logger.Warn("mqttingest: get source state failed, starting cold",
				slog.String("source", w.source.SourceName),
				slog.Any("error", err))
		}
		// Cold-start: no usable state row. Target's zero value collides
		// with TargetOff, so the unknown sentinel must be set explicitly
		// or the edge detector will treat the first OFF observation as a
		// repeat and skip the curtail dispatch.
		state = SourceState{SourceConfigID: w.source.ID, LastTarget: TargetUnknown}
	}

	messages := make(chan observation, observationChannelBuffer)

	primaryClient := w.cfg.NewClient()
	secondaryClient := w.cfg.NewClient()
	defer primaryClient.Disconnect(w.cfg.ShutdownDeadline)
	defer secondaryClient.Disconnect(w.cfg.ShutdownDeadline)

	if err := w.connectAndSubscribe(ctx, primaryClient, w.primaryHost, messages); err != nil {
		w.cfg.Logger.Error("mqttingest: primary broker connect failed",
			slog.String("source", w.source.SourceName),
			slog.String("broker", w.primaryHost),
			slog.Any("error", err))
		// Continue with the secondary — the watchdog will fire OFF
		// if both end up unreachable.
	}
	if err := w.connectAndSubscribe(ctx, secondaryClient, w.secondaryHost, messages); err != nil {
		w.cfg.Logger.Error("mqttingest: secondary broker connect failed",
			slog.String("source", w.source.SourceName),
			slog.String("broker", w.secondaryHost),
			slog.Any("error", err))
	}

	watchdog := time.NewTicker(w.cfg.WatchdogTickEvery)
	defer watchdog.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case obs := <-messages:
			state = w.handleMessage(ctx, state, obs)
		case <-watchdog.C:
			state = w.handleWatchdog(ctx, state)
		}
	}
}

func (w *sourceWorker) connectAndSubscribe(ctx context.Context, client MQTTClient, host string, messages chan<- observation) error {
	if err := client.Connect(ctx, host, w.source.BrokerPort, w.source.MQTTUsername, w.password); err != nil {
		return err
	}
	return client.Subscribe(ctx, w.source.Topic, func(payload []byte, receivedAt time.Time) {
		select {
		case messages <- observation{broker: host, payload: payload, receivedAt: receivedAt}:
		default:
			// Channel full; surface loss as a Warn. Metric counter is a
			// follow-on observability slice.
			w.cfg.Logger.Warn("mqttingest: message channel full, dropping",
				slog.String("source", w.source.SourceName),
				slog.String("broker", host))
		}
	})
}

// handleMessage decodes one inbound observation, updates the
// per-broker cache, resolves canonical state via precedence, asks the
// edge detector whether to dispatch, and persists state. Freshness
// fields advance unconditionally on a successfully-decoded message;
// LastTarget advances when the owed dispatch landed (or none was owed),
// except on a debounced flip, which must leave LastTarget untouched.
func (w *sourceWorker) handleMessage(ctx context.Context, prior SourceState, obs observation) SourceState {
	payload, err := DecodePayload(obs.payload, obs.receivedAt)
	if err != nil {
		w.cfg.Logger.Warn("mqttingest: malformed payload, ignoring",
			slog.String("source", w.source.SourceName),
			slog.String("broker", obs.broker),
			slog.Any("error", err))
		return prior
	}

	role := w.brokerRole(obs.broker)
	w.mu.Lock()
	w.lastObs[role] = &Observation{
		Broker:     obs.broker,
		Role:       role,
		Payload:    payload,
		ReceivedAt: obs.receivedAt,
	}
	primaryObs := w.lastObs[BrokerPrimary]
	secondaryObs := w.lastObs[BrokerSecondary]
	w.mu.Unlock()

	canonical, canonicalOK := CanonicalFromPair(primaryObs, secondaryObs, w.cfg.BrokerFreshness)
	if !canonicalOK {
		return prior
	}

	priorTarget := prior.LastTarget
	priorEdgeAt := prior.LastEdgeAt
	direction := Decide(PriorState{LastTarget: priorTarget, LastEdgeAt: priorEdgeAt}, canonical)

	state, dispatched := w.applyEdge(ctx, prior, canonical, direction)

	// Freshness columns reflect "we heard something" and advance on every
	// successfully-decoded message, even when the dispatch failed.
	state.LastTargetAt = canonical.PublishedAt
	state.LastReceivedAt = canonical.ReceivedAt
	state.LastReceivedBroker = canonical.Broker

	// LastTarget is the settled state derived from the wire. A failed
	// dispatch must not look like a successful transition, or the next
	// identical observation is a no-op repeat and the site silently
	// (un)curtails. A debounced flip (EdgeNone whose canonical target
	// differs from a known prior target) must also leave LastTarget put,
	// so a later genuine opposite edge still fires. Repeats and cold-start
	// (no prior target) still advance — a no-op for repeats, and the
	// first observed target for cold-start.
	debouncedFlip := direction == EdgeNone &&
		canonical.Target != prior.LastTarget &&
		prior.LastTarget != TargetUnknown
	if dispatched && !debouncedFlip {
		state.LastTarget = canonical.Target
	}

	w.persistState(ctx, state)
	return state
}

// handleWatchdog inspects staleness and synthesizes a WATCHDOG_OFF
// dispatch when appropriate. After a successful dispatch the worker
// records LastTarget=TargetOff and persists, so EvaluateWatchdog
// returns Idle on subsequent ticks until a real message clears the
// stale condition. The driver's synthetic external_reference quantizes
// to the source's staleness threshold so a crash mid-window resumes
// against the same idempotency key.
func (w *sourceWorker) handleWatchdog(ctx context.Context, prior SourceState) SourceState {
	now := w.cfg.Clock()
	decision := EvaluateWatchdog(prior.LastReceivedAt, prior.LastTarget, now, w.source.StalenessThreshold)
	if decision == WatchdogIdle {
		return prior
	}
	canonical := CanonicalState{Target: TargetOff, ReceivedAt: now}
	state, dispatched := w.applyEdge(ctx, prior, canonical, EdgeWatchdogOff)
	if !dispatched {
		// Dispatch failed; do not advance LastTarget so the next tick
		// retries against the same staleness window (the driver's
		// quantized external_reference replays idempotently).
		return prior
	}
	state.LastTarget = TargetOff
	w.persistState(ctx, state)
	return state
}

// applyEdge dispatches the implied edge and returns (state, true) on
// success or (prior, false) on dispatch failure. EdgeNone short-circuits
// to (prior, true) since no dispatch was required. The boolean lets the
// caller distinguish "no work owed" and "work owed and confirmed" from
// "work owed but failed".
func (w *sourceWorker) applyEdge(ctx context.Context, prior SourceState, canonical CanonicalState, direction EdgeDirection) (SourceState, bool) {
	if direction == EdgeNone {
		return prior, true
	}

	edgeAt := canonical.ReceivedAt
	outcome, err := w.cfg.Driver.Dispatch(ctx, w.source, direction, edgeAt)
	if err != nil {
		if errors.Is(err, ErrNoActiveEvent) {
			// OFF→ON with no in-flight event to stop: the curtailment
			// already ended by another path (max-duration, admin
			// terminate, restore completion). The transition still
			// happened, so advance the edge bookkeeping and report
			// success — otherwise the caller never moves LastTarget to ON
			// and every later ON message re-dispatches Stop in a loop.
			state := prior
			state.LastEdgeAt = edgeAt
			return state, true
		}
		w.cfg.Logger.Error("mqttingest: edge dispatch failed",
			slog.String("source", w.source.SourceName),
			slog.String("direction", direction.String()),
			slog.Any("error", err))
		return prior, false
	}

	state := prior
	state.LastEdgeAt = edgeAt
	if outcome.EventUUID != uuid.Nil {
		state.LastEdgeEventUUID = outcome.EventUUID.String()
	}
	w.cfg.Logger.Info("mqttingest: edge dispatched",
		slog.String("source", w.source.SourceName),
		slog.String("direction", direction.String()),
		slog.String("event_uuid", state.LastEdgeEventUUID))
	return state, true
}

func (w *sourceWorker) persistState(ctx context.Context, s SourceState) {
	update := StateUpdate{
		SourceConfigID:     w.source.ID,
		LastTarget:         &s.LastTarget,
		LastTargetAt:       &s.LastTargetAt,
		LastReceivedAt:     &s.LastReceivedAt,
		LastReceivedBroker: &s.LastReceivedBroker,
	}
	if !s.LastEdgeAt.IsZero() {
		update.LastEdgeAt = &s.LastEdgeAt
	}
	if s.LastEdgeEventUUID != "" {
		update.LastEdgeEventUUID = &s.LastEdgeEventUUID
	}
	if err := w.cfg.Store.UpsertSourceState(ctx, update); err != nil {
		w.cfg.Logger.Error("mqttingest: persist source state failed",
			slog.String("source", w.source.SourceName),
			slog.Any("error", err))
	}
}

func (w *sourceWorker) brokerRole(host string) BrokerRole {
	if host == w.primaryHost {
		return BrokerPrimary
	}
	return BrokerSecondary
}
