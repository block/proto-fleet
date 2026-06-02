package mqttingest

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// sourceWorker owns the runtime for one MQTT source: two broker clients,
// an observation cache for precedence dedup, and the watchdog ticker. One
// goroutine per worker; broker handlers feed a single channel its main
// loop drains.
type sourceWorker struct {
	cfg           Config
	source        SourceConfig
	primaryHost   string
	secondaryHost string
	password      string

	mu      sync.Mutex
	lastObs map[BrokerRole]*Observation
}

// observation arrives from a broker handler into the worker's inbound
// channel; broker is the source host, used to tag the BrokerRole.
type observation struct {
	broker     string
	payload    []byte
	receivedAt time.Time
}

// observationChannelBuffer bounds the per-source inbound queue. Dispatch
// normally drains faster than the ~30 s publisher cadence; the buffer
// absorbs a transient slow dispatch. On saturation the newest message is
// dropped (Warn) — repeated publisher sends and the watchdog are the
// backstop.
const observationChannelBuffer = 256

func (w *sourceWorker) run(ctx context.Context) {
	w.lastObs = make(map[BrokerRole]*Observation)

	state, err := w.cfg.Store.GetSourceState(ctx, w.source.ID)
	if err != nil {
		if !errors.Is(err, ErrSourceStateNotFound) {
			// A transient read error must not kill the worker — that takes
			// the fail-safe watchdog down with it. Degrade to cold-start
			// (the watchdog then fires OFF until a live message arrives).
			w.cfg.Logger.Warn("mqttingest: get source state failed, starting cold",
				slog.String("source", w.source.SourceName),
				slog.Any("error", err))
		}
		// Cold-start. LastTarget must be the Unknown sentinel, not the
		// TargetOff zero value, or the first OFF reads as a repeat and the
		// curtail dispatch is skipped.
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
			// Channel full; drop with a Warn (metric counter is follow-on).
			w.cfg.Logger.Warn("mqttingest: message channel full, dropping",
				slog.String("source", w.source.SourceName),
				slog.String("broker", host))
		}
	})
}

// handleMessage decodes an observation, resolves canonical state via
// precedence, dispatches any implied edge, and persists state. Freshness
// fields always advance on a decoded message; LastTarget advances only
// when the owed dispatch landed — never on a debounced flip.
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

	// Freshness columns advance on every decoded message, even if dispatch failed.
	state.LastTargetAt = canonical.PublishedAt
	state.LastReceivedAt = canonical.ReceivedAt
	state.LastReceivedBroker = canonical.Broker

	// LastTarget advances only when the owed dispatch landed: a failed
	// dispatch must not read as a settled transition (the next identical
	// observation would be a no-op repeat). A debounced flip (EdgeNone
	// against a known, differing prior target) likewise must not advance,
	// so a later genuine edge still fires; repeats and cold-start do.
	debouncedFlip := direction == EdgeNone &&
		canonical.Target != prior.LastTarget &&
		prior.LastTarget != TargetUnknown
	if dispatched && !debouncedFlip {
		state.LastTarget = canonical.Target
	}

	w.persistState(ctx, state)
	return state
}

// handleWatchdog dispatches a WATCHDOG_OFF curtail when needed: on
// staleness (no message within the threshold while not already OFF), or —
// when the last signal was OFF — if the event was terminated out-of-band
// and the source must be re-curtailed. After a successful dispatch it
// records LastTarget=OFF.
func (w *sourceWorker) handleWatchdog(ctx context.Context, prior SourceState) SourceState {
	now := w.cfg.Clock()

	if prior.LastTarget.IsOff() {
		// OFF means the source must stay curtailed; re-curtail only if the
		// event is gone (e.g. an admin terminate), not while it still holds.
		active, err := w.cfg.Driver.HasActiveEvent(ctx, w.source.OrganizationID)
		if err != nil {
			w.cfg.Logger.Warn("mqttingest: watchdog active-event check failed",
				slog.String("source", w.source.SourceName),
				slog.Any("error", err))
			return prior
		}
		if active {
			return prior
		}
	} else if EvaluateWatchdog(prior.LastReceivedAt, prior.LastTarget, now, w.source.StalenessThreshold) == WatchdogIdle {
		return prior
	}

	canonical := CanonicalState{Target: TargetOff, ReceivedAt: now}
	state, dispatched := w.applyEdge(ctx, prior, canonical, EdgeWatchdogOff)
	if !dispatched {
		// Dispatch failed; leave LastTarget so the next tick retries.
		return prior
	}
	state.LastTarget = TargetOff
	w.persistState(ctx, state)
	return state
}

// applyEdge dispatches the implied edge, returning (state, true) on
// success (EdgeNone short-circuits to (prior, true) — no work owed) or
// (prior, false) on dispatch failure.
func (w *sourceWorker) applyEdge(ctx context.Context, prior SourceState, canonical CanonicalState, direction EdgeDirection) (SourceState, bool) {
	if direction == EdgeNone {
		return prior, true
	}

	edgeAt := canonical.ReceivedAt
	outcome, err := w.cfg.Driver.Dispatch(ctx, w.source, direction, edgeAt)
	if err != nil {
		if errors.Is(err, ErrNoActiveEvent) {
			// OFF→ON with no in-flight event to stop (curtailment already
			// ended elsewhere): the transition still happened, so advance
			// bookkeeping and report success — otherwise every later ON
			// re-dispatches Stop in a loop.
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
