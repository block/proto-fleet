package mqttingest

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
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

func (w *sourceWorker) run(ctx context.Context) {
	w.lastObs = make(map[BrokerRole]*Observation)

	state, err := w.cfg.Store.GetSourceState(ctx, w.source.ID)
	if err != nil {
		if !errors.Is(err, ErrSourceStateNotFound) {
			w.cfg.Logger.Error("mqttingest: get source state failed",
				slog.String("source", w.source.SourceName),
				slog.Any("error", err))
			return
		}
		// Cold-start: no state row yet. Target's zero value collides
		// with TargetOff, so the unknown sentinel must be set
		// explicitly or the edge detector will treat the first OFF
		// observation as a repeat and skip the curtail dispatch.
		state = SourceState{SourceConfigID: w.source.ID, LastTarget: TargetUnknown}
	}

	messages := make(chan observation, 16)

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
		case <-ctx.Done():
		default:
			// Channel full; drop oldest by skipping. Surfacing this
			// loss in metrics is BE-V2-3 work.
			w.cfg.Logger.Warn("mqttingest: message channel full, dropping",
				slog.String("source", w.source.SourceName),
				slog.String("broker", host))
		}
	})
}

// handleMessage decodes one inbound observation, updates the
// per-broker cache, resolves canonical state via precedence, asks the
// edge detector whether to dispatch, and persists state.
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

	canonical, ok := CanonicalFromPair(primaryObs, secondaryObs, w.cfg.BrokerFreshness)
	if !ok {
		return prior
	}

	priorTarget := prior.LastTarget
	priorEdgeAt := prior.LastEdgeAt
	direction := Decide(PriorState{LastTarget: priorTarget, LastEdgeAt: priorEdgeAt}, canonical)

	state := w.applyEdge(ctx, prior, canonical, direction)
	state.LastTarget = canonical.Target
	state.LastTargetAt = canonical.PublishedAt
	state.LastReceivedAt = canonical.ReceivedAt
	state.LastReceivedBroker = canonical.Broker

	w.persistState(ctx, state)
	return state
}

// handleWatchdog inspects staleness and synthesizes a WATCHDOG_OFF
// dispatch when appropriate. Repeated firings are deduped by the v1
// partial unique index — the synthetic external_reference includes
// the second-resolution timestamp so back-to-back ticks produce the
// same reference and the second hits the index.
func (w *sourceWorker) handleWatchdog(ctx context.Context, prior SourceState) SourceState {
	now := w.cfg.Clock()
	decision := EvaluateWatchdog(prior.LastReceivedAt, prior.LastTarget, now, w.source.StalenessThreshold)
	if decision == WatchdogIdle {
		return prior
	}
	// Synthesize a canonical state for the dispatch — the existing
	// dispatch shape only needs the target and edge timestamp.
	canonical := CanonicalState{Target: TargetOff, ReceivedAt: now}
	return w.applyEdge(ctx, prior, canonical, EdgeWatchdogOff)
}

func (w *sourceWorker) applyEdge(ctx context.Context, prior SourceState, canonical CanonicalState, direction EdgeDirection) SourceState {
	if direction == EdgeNone {
		return prior
	}

	edgeAt := canonical.ReceivedAt
	outcome, err := w.cfg.Driver.Dispatch(ctx, w.source, direction, edgeAt)
	if err != nil {
		w.cfg.Logger.Error("mqttingest: edge dispatch failed",
			slog.String("source", w.source.SourceName),
			slog.String("direction", direction.String()),
			slog.Any("error", err))
		// Do not advance prior on dispatch failure — the next tick
		// will retry. WatchdogOff specifically is the case where the
		// dispatch matters most.
		return prior
	}

	state := prior
	state.LastEdgeAt = edgeAt
	if outcome.EventUUID.String() != "00000000-0000-0000-0000-000000000000" {
		state.LastEdgeEventUUID = outcome.EventUUID.String()
	}
	w.cfg.Logger.Info("mqttingest: edge dispatched",
		slog.String("source", w.source.SourceName),
		slog.String("direction", direction.String()),
		slog.String("event_uuid", state.LastEdgeEventUUID))
	return state
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
