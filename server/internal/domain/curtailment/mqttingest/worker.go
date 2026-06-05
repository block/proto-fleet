package mqttingest

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// sourceWorker owns one source's broker clients, observation cache, and watchdog.
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

// observation is the raw broker callback payload queued into the worker loop.
type observation struct {
	broker     string
	payload    []byte
	receivedAt time.Time
}

// observationChannelBuffer absorbs transient dispatch slowness; publisher
// retries and the watchdog backstop dropped messages.
const observationChannelBuffer = 256

func (w *sourceWorker) run(ctx context.Context) {
	w.lastObs = make(map[BrokerRole]*Observation)

	state, ok := w.waitForInitialState(ctx)
	if !ok {
		return
	}

	messages := make(chan observation, observationChannelBuffer)

	primaryClient := w.cfg.NewClient()
	secondaryClient := w.cfg.NewClient()
	defer primaryClient.Disconnect(w.cfg.ShutdownDeadline)
	defer secondaryClient.Disconnect(w.cfg.ShutdownDeadline)

	// Connect concurrently so one down broker cannot stall the other broker or
	// the fail-safe watchdog.
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

func (w *sourceWorker) waitForInitialState(ctx context.Context) (SourceState, bool) {
	retryEvery := w.initialStateRetryEvery()
	for {
		state, ok := w.loadInitialState(ctx)
		if ok {
			return state, true
		}

		timer := time.NewTimer(retryEvery)
		select {
		case <-ctx.Done():
			timer.Stop()
			return SourceState{}, false
		case <-timer.C:
		}
	}
}

// loadInitialState recovers persisted source state, then reconciles to OFF only
// when this source already has a pending/active event. A restoring event means
// ON was accepted and restore is in progress.
func (w *sourceWorker) loadInitialState(ctx context.Context) (SourceState, bool) {
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
			return state, false
		case eventHoldsCurtailment(active):
			state.LastTarget = TargetOff
			state.LastEdgeEventUUID = active.EventUUID.String()
			// Anchor ordering/debounce to the event so retained pre-event ON
			// payloads cannot stop the recovered curtailment.
			state.LastTargetAt = active.CreatedAt
			state.LastEdgeAt = active.CreatedAt
			w.cfg.Logger.Info("mqttingest: reconciled to active curtailment",
				slog.String("source", w.source.SourceName),
				slog.String("event_uuid", state.LastEdgeEventUUID))
		}
	}
	return state, true
}

func (w *sourceWorker) initialStateRetryEvery() time.Duration {
	if w.cfg.WatchdogTickEvery > 0 && w.cfg.WatchdogTickEvery < time.Second {
		return w.cfg.WatchdogTickEvery
	}
	return time.Second
}

func (w *sourceWorker) connectAndSubscribe(ctx context.Context, client MQTTClient, host string, messages chan<- observation) error {
	if err := client.Connect(ctx, host, w.source.BrokerPort, w.source.MQTTUsername, w.password); err != nil {
		return err
	}
	return client.Subscribe(ctx, w.source.Topic, func(payload []byte, receivedAt time.Time) {
		select {
		case messages <- observation{broker: host, payload: payload, receivedAt: receivedAt}:
		default:
			w.cfg.Logger.Warn("mqttingest: message channel full, dropping",
				slog.String("source", w.source.SourceName),
				slog.String("broker", host))
		}
	})
}

// handleMessage resolves the canonical signal, dispatches owed edges, and
// persists only state that safely settled.
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

	// Evict stale winners so retained/backlog payloads cannot mask a live broker.
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

	// Same stamp + same target is a duplicate; same stamp + different target is
	// a real flip because wire timestamps are seconds-precision.
	if !prior.LastTargetAt.IsZero() && !canonical.PublishedAt.After(prior.LastTargetAt) &&
		canonical.Target == prior.LastProcessedTarget {
		direction = EdgeNone
	}

	state, dispatched := w.applyEdge(ctx, prior, canonical, direction)

	// Freshness advances even when dispatch fails because the publisher was live.
	state.LastReceivedAt = canonical.ReceivedAt
	state.LastReceivedBroker = canonical.Broker

	// Advance the duplicate-suppression anchor only after the edge settles.
	if dispatched {
		state.LastTargetAt = canonical.PublishedAt
		state.LastProcessedTarget = canonical.Target
	}

	// Failed dispatches and debounced flips must not settle the source target.
	debouncedFlip := direction == EdgeNone &&
		canonical.Target != prior.LastTarget &&
		prior.LastTarget != TargetUnknown
	if dispatched && !debouncedFlip {
		state.LastTarget = canonical.Target
	}

	w.persistState(ctx, state)
	return state
}

// handleWatchdog enforces fail-safe OFF on stale or externally restored sources.
func (w *sourceWorker) handleWatchdog(ctx context.Context, prior SourceState) SourceState {
	now := w.cfg.Clock()

	if prior.LastTarget.IsOff() {
		active, err := w.cfg.Driver.ActiveSourceEvent(ctx, w.source)
		if err != nil {
			w.cfg.Logger.Warn("mqttingest: watchdog active-event check failed",
				slog.String("source", w.source.SourceName),
				slog.Any("error", err))
			return prior
		}
		if active != nil {
			if eventIsRestoring(active) {
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
		return prior
	}
	state.LastTarget = TargetOff
	w.persistState(ctx, state)
	return state
}

// applyEdge dispatches the implied edge and reports whether it settled.
func (w *sourceWorker) applyEdge(ctx context.Context, prior SourceState, canonical CanonicalState, direction EdgeDirection) (SourceState, bool) {
	if direction == EdgeNone {
		return prior, true
	}

	// Message-driven OFF references use publisher time; watchdog OFF falls back
	// to receive time. prior.LastEdgeAt disambiguates same-second OFF bursts.
	dispatchAt := canonical.ReceivedAt
	if !canonical.PublishedAt.IsZero() {
		dispatchAt = canonical.PublishedAt
	}
	outcome, err := w.cfg.Driver.Dispatch(ctx, w.source, direction, dispatchAt, prior.LastEdgeAt)
	if err != nil {
		if errors.Is(err, ErrNoActiveEvent) {
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

// isStalePayload rejects out-of-order and retained/backlog observations.
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
