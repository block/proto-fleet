package mqttingest

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// sourceWorker owns the runtime for one MQTT source: two broker clients,
// an observation cache for precedence dedup, and the watchdog ticker. One
// goroutine per worker; broker handlers feed a single channel its main
// loop drains.
type sourceWorker struct {
	cfg           Config
	source        SourceConfig
	decoder       PayloadDecoder
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

	state := w.loadInitialState(ctx)

	messages := make(chan observation, observationChannelBuffer)

	primaryClient := w.cfg.NewClient()
	secondaryClient := w.cfg.NewClient()
	defer primaryClient.Disconnect(w.cfg.ShutdownDeadline)
	defer secondaryClient.Disconnect(w.cfg.ShutdownDeadline)

	// Connect to both brokers concurrently. MQTTClient.Connect blocks until
	// connected (it retries with backoff), so a serial connect would let one
	// down broker stall the other broker's subscription and the fail-safe
	// watchdog. Each connect only feeds the shared channel; the loop below
	// stays the sole goroutine that touches source state. Wait for them on
	// exit so a connect finishes before the worker's password is cleared.
	var connectWG sync.WaitGroup
	for _, bc := range []struct {
		client MQTTClient
		host   string
	}{
		{primaryClient, w.primaryHost},
		{secondaryClient, w.secondaryHost},
	} {
		connectWG.Add(1)
		go func(client MQTTClient, host string) {
			defer connectWG.Done()
			if err := w.connectAndSubscribe(ctx, client, host, messages); err != nil {
				w.cfg.Logger.Error("mqttingest: broker connect failed",
					slog.String("source", w.source.SourceName),
					slog.String("broker", host),
					slog.Any("error", err))
			}
		}(bc.client, bc.host)
	}
	defer connectWG.Wait()

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

// loadInitialState reads the persisted state, degrading to cold-start on a
// read error so a transient DB blip doesn't take the fail-safe watchdog down
// with it. If the loaded target is non-OFF but this source already has an
// active curtailment event — a prior OFF started one and the state read or a
// post-Start persist failed — it reconciles to OFF, so a later ON stops that
// event instead of being a cold-start no-op.
func (w *sourceWorker) loadInitialState(ctx context.Context) SourceState {
	state, err := w.cfg.Store.GetSourceState(ctx, w.source.ID)
	if err != nil {
		if !errors.Is(err, ErrSourceStateNotFound) {
			w.cfg.Logger.Warn("mqttingest: get source state failed, starting cold",
				slog.String("source", w.source.SourceName),
				slog.Any("error", err))
		}
		// LastTarget must be the Unknown sentinel, not the TargetOff zero
		// value, or the first OFF reads as a repeat and the curtail is skipped.
		state = SourceState{SourceConfigID: w.source.ID, LastTarget: TargetUnknown}
	}

	if state.LastTarget != TargetOff {
		switch active, aerr := w.cfg.Driver.ActiveSourceEvent(ctx, w.source); {
		case aerr != nil:
			w.cfg.Logger.Warn("mqttingest: active-event reconcile failed",
				slog.String("source", w.source.SourceName),
				slog.Any("error", aerr))
		case active != nil:
			state.LastTarget = TargetOff
			state.LastEdgeEventUUID = active.EventUUID.String()
			// Seed the ordering + debounce anchors from the event so a
			// retained/backlog payload published before this curtailment began
			// can't be processed as a fresh edge and stop it. Without an anchor
			// LastTargetAt/LastEdgeAt stay zero, and a pre-event ON received
			// inside the staleness window would dispatch Stop and un-curtail.
			state.LastTargetAt = active.CreatedAt
			state.LastEdgeAt = active.CreatedAt
			w.cfg.Logger.Info("mqttingest: reconciled to active curtailment",
				slog.String("source", w.source.SourceName),
				slog.String("event_uuid", state.LastEdgeEventUUID))
		}
	}
	return state
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
	payload, err := w.decoder.Decode(obs.payload, obs.receivedAt)
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

	// Drop a stale precedence winner and re-resolve against the other broker so
	// a stale cached observation can't mask the survivor's current data. Stale =
	// published before the last processed stamp (out-of-order redelivery), or
	// older than the staleness threshold at receipt (retained/backlog, which a
	// live broker may outrank — also the cold-start guard, LastTargetAt zero).
	for canonicalOK && w.isStalePayload(prior, canonical) {
		w.cfg.Logger.Warn("mqttingest: evicting stale payload",
			slog.String("source", w.source.SourceName),
			slog.String("broker", canonical.Broker),
			slog.Time("published_at", canonical.PublishedAt),
			slog.Duration("age", canonical.ReceivedAt.Sub(canonical.PublishedAt)))
		w.mu.Lock()
		delete(w.lastObs, w.brokerRole(canonical.Broker))
		primaryObs = w.lastObs[BrokerPrimary]
		secondaryObs = w.lastObs[BrokerSecondary]
		w.mu.Unlock()
		canonical, canonicalOK = CanonicalFromPair(primaryObs, secondaryObs, w.cfg.BrokerFreshness)
	}
	if !canonicalOK {
		return prior
	}

	priorTarget := prior.LastTarget
	priorEdgeAt := prior.LastEdgeAt
	direction := Decide(PriorState{LastTarget: priorTarget, LastEdgeAt: priorEdgeAt}, canonical)

	// Ignore a redelivery of an already-processed payload (a recent QoS-1 or
	// dual-broker copy that survives the eviction loop above): same publisher
	// stamp AND same target. A genuine same-second target change — equal stamp
	// but a differing target, since wire stamps are seconds-precision — is not a
	// duplicate and must still drive its edge.
	if !prior.LastTargetAt.IsZero() && !canonical.PublishedAt.After(prior.LastTargetAt) &&
		canonical.Target == prior.LastProcessedTarget {
		direction = EdgeNone
	}

	state, dispatched := w.applyEdge(ctx, prior, canonical, direction)

	// LastReceivedAt/Broker track liveness for the watchdog — advance on every
	// decoded message, even on a failed dispatch (we did hear the publisher).
	state.LastReceivedAt = canonical.ReceivedAt
	state.LastReceivedBroker = canonical.Broker

	// LastTargetAt is the raw last *processed* publisher stamp. The duplicate
	// guard above compares the incoming stamp against it to recognize a
	// redelivery, so it must be the exact stamp, never clamped. Advance it only
	// when the edge settled; on a failed dispatch leave it so a redelivery
	// retries instead of being suppressed as a duplicate. (isStalePayload caps
	// it at receive-time for *ordering*, so a future-dated stamp still can't pin
	// the out-of-order cutoff ahead of real time.)
	if dispatched {
		state.LastTargetAt = canonical.PublishedAt
		state.LastProcessedTarget = canonical.Target
	}

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

// handleWatchdog enforces OFF when needed. On staleness (no message within the
// threshold while not already OFF) it dispatches WATCHDOG_OFF. When the last
// signal was OFF it re-asserts curtailment if this source's event stopped
// holding: resuming a restoring event in place, or — when the event is gone
// (terminated out-of-band) — dispatching a fresh WATCHDOG_OFF. Records
// LastTarget=OFF after a successful WATCHDOG_OFF dispatch.
func (w *sourceWorker) handleWatchdog(ctx context.Context, prior SourceState) SourceState {
	now := w.cfg.Clock()

	if prior.LastTarget.IsOff() {
		// OFF means this source must stay curtailed. An active/pending event
		// still holds → idle. A restoring event is being undone by an
		// out-of-band Stop, so re-assert curtailment in place. A missing event
		// was terminated out-of-band → fall through to a fresh WATCHDOG_OFF.
		// Another source's event doesn't satisfy this source — each curtails
		// its own scope.
		active, err := w.cfg.Driver.ActiveSourceEvent(ctx, w.source)
		if err != nil {
			w.cfg.Logger.Warn("mqttingest: watchdog active-event check failed",
				slog.String("source", w.source.SourceName),
				slog.Any("error", err))
			return prior
		}
		if active != nil {
			if active.State == models.EventStateRestoring {
				// Flip restoring → active and re-curtail its targets in place,
				// rather than a fresh Start that would just replay this event.
				if err := w.cfg.Driver.ResumeSourceEvent(ctx, active); err != nil {
					w.cfg.Logger.Warn("mqttingest: watchdog re-curtail failed",
						slog.String("source", w.source.SourceName),
						slog.Any("error", err))
				}
			}
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

	// The dispatch timestamp drives the synthetic external_reference: use the
	// publisher's stamp (stable across the dual-broker duplicate and QoS-1
	// redelivery) for message-driven edges; the watchdog edge has no stamp and
	// falls back to receive-time. prior.LastEdgeAt salts that reference so two
	// OFF edges sharing a publisher second (a burst received apart) don't
	// collide; it also stays the debounce anchor tracking local timing.
	dispatchAt := canonical.ReceivedAt
	if !canonical.PublishedAt.IsZero() {
		dispatchAt = canonical.PublishedAt
	}
	outcome, err := w.cfg.Driver.Dispatch(ctx, w.source, direction, dispatchAt, prior.LastEdgeAt)
	if err != nil {
		if errors.Is(err, ErrNoActiveEvent) {
			// OFF→ON with no in-flight event to stop (curtailment already
			// ended elsewhere): the transition still happened, so advance
			// bookkeeping and report success — otherwise every later ON
			// re-dispatches Stop in a loop.
			state := prior
			state.LastEdgeAt = canonical.ReceivedAt
			return state, true
		}
		w.cfg.Logger.Error("mqttingest: edge dispatch failed",
			slog.String("source", w.source.SourceName),
			slog.String("direction", direction.String()),
			slog.Any("error", err))
		return prior, false
	}

	state := prior
	state.LastEdgeAt = canonical.ReceivedAt
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
		SourceConfigID:      w.source.ID,
		LastTarget:          &s.LastTarget,
		LastTargetAt:        &s.LastTargetAt,
		LastProcessedTarget: &s.LastProcessedTarget,
		LastReceivedAt:      &s.LastReceivedAt,
		LastReceivedBroker:  &s.LastReceivedBroker,
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

// isStalePayload reports whether a canonical observation must be dropped from
// edge detection and re-resolved against the other broker: published before the
// last processed stamp (out-of-order redelivery), or older than the staleness
// threshold at receipt (retained / reconnect-backlog data that doesn't prove
// the publisher is live). The latter also covers cold start (LastTargetAt zero).
//
// The out-of-order cutoff caps the last processed stamp at its receive time
// (LastReceivedAt) so a future-dated publisher stamp can't push the cutoff
// ahead of real time and flag later genuine payloads as stale. LastTargetAt
// itself stays the raw stamp — the duplicate guard needs it to match a
// redelivery of that same payload.
func (w *sourceWorker) isStalePayload(prior SourceState, c CanonicalState) bool {
	cutoff := prior.LastTargetAt
	if !prior.LastReceivedAt.IsZero() && prior.LastReceivedAt.Before(cutoff) {
		cutoff = prior.LastReceivedAt
	}
	if !prior.LastTargetAt.IsZero() && c.PublishedAt.Before(cutoff) {
		return true
	}
	return c.ReceivedAt.Sub(c.PublishedAt) >= w.source.StalenessThreshold
}

func (w *sourceWorker) brokerRole(host string) BrokerRole {
	if host == w.primaryHost {
		return BrokerPrimary
	}
	return BrokerSecondary
}
