package mqttingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
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

// observationChannelBuffer absorbs transient dispatch slowness. Once full, the
// broker callback backpressures instead of accepting and losing a state signal.
const observationChannelBuffer = 256
const initialBrokerRetryMax = 30 * time.Second

func (w *sourceWorker) run(ctx context.Context) {
	w.lastObs = make(map[BrokerRole]*Observation)

	state, ok := w.waitForInitialState(ctx)
	if !ok {
		return
	}

	messages := make(chan observation, observationChannelBuffer)
	subscriptions := make(chan struct{}, 2)
	startedAt := w.cfg.Clock()
	deferStartupPending := state.PendingEdge != nil
	startupPendingReadyAt := time.Time{}
	startupPendingSubscribed := false
	if deferStartupPending {
		startupPendingReadyAt = startedAt.Add(w.startupRetryEvery())
	}
	deferStartupWatchdog := shouldDeferStartupWatchdog(state, startedAt, w.source.StalenessThreshold)
	startupWatchdogReadyAt := time.Time{}
	startupWatchdogSubscribed := false
	if deferStartupWatchdog {
		startupWatchdogReadyAt = startedAt.Add(w.source.StalenessThreshold)
	}

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
			w.connectAndSubscribe(ctx, client, host, messages, subscriptions)
		}(bc.client, bc.host)
	}
	defer connectWG.Wait()

	watchdog := time.NewTicker(w.cfg.WatchdogTickEvery)
	defer watchdog.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-subscriptions:
			if deferStartupPending && !startupPendingSubscribed {
				startupPendingSubscribed = true
				// Give retained/live payloads a chance to supersede durable
				// retry state before replaying a possibly stale startup edge.
				// The startup default above still bounds replay time if no
				// broker ever subscribes.
				startupPendingReadyAt = w.cfg.Clock().Add(w.startupRetryEvery())
			}
			if deferStartupWatchdog && !startupWatchdogSubscribed {
				startupWatchdogSubscribed = true
				// Give retained/live payloads one dispatch tick to arrive after
				// subscription. If no broker ever subscribes, the staleness
				// threshold bound above still fails safe.
				startupWatchdogReadyAt = w.cfg.Clock().Add(w.startupRetryEvery())
			}
		case obs := <-messages:
			state = w.handleMessage(ctx, state, obs)
			if state.PendingEdge == nil {
				deferStartupPending = false
				startupPendingReadyAt = time.Time{}
				startupPendingSubscribed = false
			}
			if deferStartupWatchdog && startupWatchdogSatisfied(state, startedAt) {
				deferStartupWatchdog = false
				startupWatchdogReadyAt = time.Time{}
				startupWatchdogSubscribed = false
			}
		case <-watchdog.C:
			now := w.cfg.Clock()
			if deferStartupPending && now.Before(startupPendingReadyAt) {
				continue
			}
			if deferStartupWatchdog && now.Before(startupWatchdogReadyAt) {
				continue
			}
			state = w.handleWatchdog(ctx, state)
			if state.PendingEdge == nil {
				deferStartupPending = false
				startupPendingReadyAt = time.Time{}
				startupPendingSubscribed = false
			}
			if deferStartupWatchdog && startupWatchdogSatisfied(state, startedAt) {
				deferStartupWatchdog = false
				startupWatchdogReadyAt = time.Time{}
				startupWatchdogSubscribed = false
			}
		}
	}
}

func shouldDeferStartupWatchdog(state SourceState, now time.Time, threshold time.Duration) bool {
	if state.PendingEdge != nil || state.LastTarget.IsOff() {
		return false
	}
	return EvaluateWatchdog(state.LastReceivedAt, state.LastTarget, now, threshold) == WatchdogFire
}

func startupWatchdogSatisfied(state SourceState, startedAt time.Time) bool {
	return state.LastTarget.IsOff() || state.LastReceivedAt.After(startedAt)
}

func (w *sourceWorker) waitForInitialState(ctx context.Context) (SourceState, bool) {
	retryEvery := w.startupRetryEvery()
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
// when this source already has an active event. Pending edges are rehydrated
// without replay because a newer retained/live payload may supersede them after
// subscription.
func (w *sourceWorker) loadInitialState(ctx context.Context) (SourceState, bool) {
	state, err := w.cfg.Store.GetSourceState(ctx, w.source.ID)
	if err != nil {
		if !errors.Is(err, ErrSourceStateNotFound) {
			w.cfg.Logger.Warn("mqttingest: get source state failed, retrying",
				slog.String("source", w.source.SourceName),
				slog.Any("error", err))
			return SourceState{}, false
		}
		// LastTarget must be the Unknown sentinel, not the TargetOff zero
		// value, or the first OFF reads as a repeat and the curtail is skipped.
		state = SourceState{SourceConfigID: w.source.ID, LastTarget: TargetUnknown}
	}

	if state.PendingEdge != nil {
		return state, true
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

func (w *sourceWorker) startupRetryEvery() time.Duration {
	if w.cfg.WatchdogTickEvery > 0 && w.cfg.WatchdogTickEvery < time.Second {
		return w.cfg.WatchdogTickEvery
	}
	return time.Second
}

func mqttClientIdentity(src SourceConfig, host string) string {
	return fmt.Sprintf("%d|%s|%s|%d|%s", src.ID, src.SourceName, host, src.BrokerPort, src.Topic)
}

func nextInitialBrokerRetry(current time.Duration) time.Duration {
	next := current * 2
	if next <= 0 {
		return time.Second
	}
	if next > initialBrokerRetryMax {
		return initialBrokerRetryMax
	}
	return next
}

func jitterRetryDelay(base time.Duration, rng *rand.Rand) time.Duration {
	if base <= 0 || rng == nil {
		return base
	}
	jitterMax := int64(base / 5)
	if jitterMax <= 0 {
		return base
	}
	return base + time.Duration(rng.Int63n(jitterMax+1))
}

func (w *sourceWorker) connectAndSubscribe(ctx context.Context, client MQTTClient, host string, messages chan<- observation, subscriptions chan<- struct{}) {
	retryEvery := w.startupRetryEvery()
	rng := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // jitter only; not security-sensitive
	for {
		if err := w.connectAndSubscribeOnce(ctx, client, host, messages); err != nil {
			if ctx.Err() != nil {
				return
			}
			client.Disconnect(w.cfg.ShutdownDeadline)
			retryAfter := jitterRetryDelay(retryEvery, rng)
			w.cfg.Logger.Warn("mqttingest: broker connect failed, retrying",
				slog.String("source", w.source.SourceName),
				slog.String("broker", host),
				slog.Duration("retry_after", retryAfter),
				slog.Any("error", err))
			timer := time.NewTimer(retryAfter)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			retryEvery = nextInitialBrokerRetry(retryEvery)
			continue
		}
		select {
		case subscriptions <- struct{}{}:
		case <-ctx.Done():
		}
		return
	}
}

func (w *sourceWorker) connectAndSubscribeOnce(ctx context.Context, client MQTTClient, host string, messages chan<- observation) error {
	if err := client.Subscribe(ctx, w.source.Topic, func(payload []byte, receivedAt time.Time) {
		select {
		case messages <- observation{broker: host, payload: payload, receivedAt: receivedAt}:
		case <-ctx.Done():
		}
	}); err != nil {
		return err
	}
	return client.Connect(ctx, host, w.source.BrokerPort, w.source.BrokerTransport, w.source.MQTTUsername, w.password, mqttClientIdentity(w.source, host))
}

// handleMessage resolves the canonical signal, dispatches owed edges, and
// persists only state that safely settled.
func (w *sourceWorker) handleMessage(ctx context.Context, prior SourceState, obs observation) SourceState {
	original := prior
	pendingSuperseded := false

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
	liveness := w.latestFreshObservation(prior, primaryObs, secondaryObs)
	alreadyProcessed := w.alreadyProcessedTarget(prior, canonical)

	if pendingEdgeSupersededBy(prior.PendingEdge, canonical, alreadyProcessed) {
		w.cfg.Logger.Info("mqttingest: pending edge superseded by newer payload",
			slog.String("source", w.source.SourceName),
			slog.String("pending_direction", prior.PendingEdge.Direction.String()),
			slog.String("pending_target", prior.PendingEdge.Target.String()),
			slog.String("canonical_target", canonical.Target.String()))
		if prior.PendingEdge.Target == TargetOff && canonical.Target == TargetOn {
			return w.applySupersedingPendingOff(ctx, prior, original, canonical, liveness)
		}
		prior.PendingEdge = nil
		pendingSuperseded = true
	}

	priorTarget := prior.LastTarget
	priorEdgeAt := prior.LastEdgeAt
	direction := Decide(PriorState{LastTarget: priorTarget, LastEdgeAt: priorEdgeAt}, canonical)

	// Each target value may be processed once per seconds-precision publisher
	// timestamp. This keeps a real same-second flip, but suppresses a later QoS
	// redelivery of an older target at that same stamp.
	if alreadyProcessed {
		direction = EdgeNone
	}

	state, dispatched := w.applyEdge(ctx, prior, canonical, direction)

	// Freshness advances even when dispatch fails because the publisher was live.
	state = w.advanceLiveness(state, canonical, liveness)

	if dispatched && direction == EdgeNone {
		recordProcessedTarget(&state, canonical)

		// Failed dispatches and debounced flips must not settle the source target.
		debouncedFlip := canonical.Target != prior.LastTarget &&
			prior.LastTarget != TargetUnknown
		if !debouncedFlip {
			state.LastTarget = canonical.Target
		}
		state.LastEmptyFullFleetWatchdogRef = ""
	}

	if !w.persistState(ctx, state) && pendingSuperseded && original.PendingEdge != nil && state.PendingEdge == nil {
		return original
	}
	return state
}

func (w *sourceWorker) applySupersedingPendingOff(
	ctx context.Context,
	prior SourceState,
	original SourceState,
	canonical CanonicalState,
	liveness *Observation,
) SourceState {
	prior.PendingEdge = nil
	pendingState := prior
	pendingState.PendingEdge = &PendingEdge{
		Direction:      EdgeOffToOn,
		Target:         canonical.Target,
		TargetAt:       canonical.PublishedAt,
		ReceivedAt:     canonical.ReceivedAt,
		ReceivedBroker: canonical.Broker,
		PriorEdgeAt:    prior.LastEdgeAt,
	}
	if !w.persistState(ctx, pendingState) {
		return original
	}

	state, _ := w.dispatchPendingEdge(ctx, pendingState)
	state = w.advanceLiveness(state, canonical, liveness)
	w.persistState(ctx, state)
	return state
}

func (w *sourceWorker) advanceLiveness(state SourceState, canonical CanonicalState, latest *Observation) SourceState {
	state.LastReceivedAt = canonical.ReceivedAt
	state.LastReceivedBroker = canonical.Broker
	if latest != nil && latest.ReceivedAt.After(state.LastReceivedAt) {
		state.LastReceivedAt = latest.ReceivedAt
		state.LastReceivedBroker = latest.Broker
	}
	return state
}

// handleWatchdog enforces fail-safe OFF on stale or externally restored sources.
func (w *sourceWorker) handleWatchdog(ctx context.Context, prior SourceState) SourceState {
	if prior.PendingEdge != nil {
		state, ok := w.retryPendingEdge(ctx, prior)
		if ok {
			return state
		}
		return prior
	}

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
				if !w.ensureCanDispatch(ctx) {
					return prior
				}
				if err := w.cfg.Driver.ResumeSourceEvent(ctx, active); err != nil {
					w.cfg.Logger.Warn("mqttingest: watchdog re-curtail failed",
						slog.String("source", w.source.SourceName),
						slog.Any("error", err))
				}
			}
			return prior
		}
		watchdogRef := startExternalReference(w.source.SourceName, EdgeWatchdogOff, now, time.Time{}, w.source.StalenessThreshold)
		if prior.LastEmptyFullFleetWatchdogRef == watchdogRef {
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
	w.persistState(ctx, state)
	return state
}

// applyEdge dispatches the implied edge and reports whether it settled.
func (w *sourceWorker) applyEdge(ctx context.Context, prior SourceState, canonical CanonicalState, direction EdgeDirection) (SourceState, bool) {
	if direction == EdgeNone {
		return prior, true
	}

	pendingState := prior
	pendingState.PendingEdge = &PendingEdge{
		Direction:      direction,
		Target:         canonical.Target,
		TargetAt:       canonical.PublishedAt,
		ReceivedAt:     canonical.ReceivedAt,
		ReceivedBroker: canonical.Broker,
		PriorEdgeAt:    prior.LastEdgeAt,
	}
	if !w.persistState(ctx, pendingState) {
		return pendingState, false
	}
	if !w.ensureCanDispatch(ctx) {
		return pendingState, false
	}
	return w.dispatchPendingEdge(ctx, pendingState)
}

func (w *sourceWorker) retryPendingEdge(ctx context.Context, prior SourceState) (SourceState, bool) {
	state, dispatched := w.dispatchPendingEdge(ctx, prior)
	if !dispatched {
		return prior, false
	}
	if !w.persistState(ctx, state) {
		return state, true
	}
	return state, true
}

func (w *sourceWorker) dispatchPendingEdge(ctx context.Context, prior SourceState) (SourceState, bool) {
	pending := prior.PendingEdge
	if pending == nil {
		return prior, true
	}
	if !w.ensureCanDispatch(ctx) {
		return prior, false
	}

	// Message-driven OFF references use publisher time; watchdog OFF falls back
	// to receive time. prior.LastEdgeAt disambiguates same-second OFF bursts.
	dispatchAt := pending.ReceivedAt
	if !pending.TargetAt.IsZero() {
		dispatchAt = pending.TargetAt
	}
	if pending.Direction == EdgeOffToOn {
		active, err := w.cfg.Driver.ActiveSourceEvent(ctx, w.source)
		if err != nil {
			w.cfg.Logger.Error("mqttingest: pending ON active-event check failed",
				slog.String("source", w.source.SourceName),
				slog.Any("error", err))
			return prior, false
		}
		if active == nil || eventIsRestoring(active) {
			eventUUID := uuid.Nil
			if active != nil {
				eventUUID = active.EventUUID
			}
			return w.settlePendingEdge(prior, pending, TargetOn, eventUUID, false, dispatchAt), true
		}
	}
	outcome, err := w.cfg.Driver.Dispatch(ctx, w.source, pending.Direction, dispatchAt, pending.PriorEdgeAt)
	if err != nil {
		if errors.Is(err, ErrNoActiveEvent) {
			return w.settlePendingEdge(prior, pending, pending.Target, uuid.Nil, false, dispatchAt), true
		}
		w.cfg.Logger.Error("mqttingest: edge dispatch failed",
			slog.String("source", w.source.SourceName),
			slog.String("direction", pending.Direction.String()),
			slog.Any("error", err))
		return prior, false
	}

	state := w.settlePendingEdge(prior, pending, pending.Target, outcome.EventUUID, outcome.EmptyFullFleetNoop, dispatchAt)
	w.cfg.Logger.Info("mqttingest: edge dispatched",
		slog.String("source", w.source.SourceName),
		slog.String("direction", pending.Direction.String()),
		slog.String("event_uuid", state.LastEdgeEventUUID))
	return state, true
}

func (w *sourceWorker) settlePendingEdge(
	prior SourceState,
	pending *PendingEdge,
	target Target,
	eventUUID uuid.UUID,
	emptyFullFleetNoop bool,
	dispatchAt time.Time,
) SourceState {
	state := prior
	state.PendingEdge = nil
	state.LastEdgeAt = pending.ReceivedAt
	state.LastReceivedAt = pending.ReceivedAt
	state.LastReceivedBroker = pending.ReceivedBroker
	state.LastTarget = target
	if eventUUID != uuid.Nil {
		state.LastEdgeEventUUID = eventUUID.String()
	}
	if emptyFullFleetNoop && pending.Direction == EdgeWatchdogOff {
		state.LastEmptyFullFleetWatchdogRef = startExternalReference(
			w.source.SourceName,
			EdgeWatchdogOff,
			dispatchAt,
			time.Time{},
			w.source.StalenessThreshold,
		)
	} else {
		state.LastEmptyFullFleetWatchdogRef = ""
	}
	recordProcessedTarget(&state, pending.canonical())
	return state
}

func (w *sourceWorker) persistState(ctx context.Context, s SourceState) bool {
	update := StateUpdate{
		SourceConfigID:                w.source.ID,
		LastTarget:                    s.LastTarget,
		LastTargetAt:                  s.LastTargetAt,
		LastProcessedTarget:           s.LastProcessedTarget,
		LastProcessedTargets:          s.LastProcessedTargets,
		LastReceivedAt:                s.LastReceivedAt,
		LastReceivedBroker:            s.LastReceivedBroker,
		LastEdgeAt:                    s.LastEdgeAt,
		LastEdgeEventUUID:             s.LastEdgeEventUUID,
		PendingEdge:                   s.PendingEdge,
		LastEmptyFullFleetWatchdogRef: s.LastEmptyFullFleetWatchdogRef,
	}
	if err := w.cfg.Store.UpsertSourceState(ctx, update); err != nil {
		w.cfg.Logger.Error("mqttingest: persist source state failed",
			slog.String("source", w.source.SourceName),
			slog.Any("error", err))
		return false
	}
	return true
}

func (w *sourceWorker) ensureCanDispatch(ctx context.Context) bool {
	canIngest, err := w.cfg.Store.UserCanIngestCurtailment(ctx, w.source.ServiceUserID, w.source.OrganizationID)
	if err != nil {
		w.cfg.Logger.Error("mqttingest: service-user authorization check failed",
			slog.String("source", w.source.SourceName),
			slog.Int64("service_user_id", w.source.ServiceUserID),
			slog.Int64("org_id", w.source.OrganizationID),
			slog.Any("error", err))
		return false
	}
	if !canIngest {
		w.cfg.Logger.Warn("mqttingest: service user lacks curtailment ingest permission",
			slog.String("source", w.source.SourceName),
			slog.Int64("service_user_id", w.source.ServiceUserID),
			slog.Int64("org_id", w.source.OrganizationID))
		return false
	}
	return true
}

// isStalePayload rejects out-of-order and retained/backlog observations.
func (w *sourceWorker) isStalePayload(prior SourceState, c CanonicalState) bool {
	cutoff := prior.LastTargetAt
	if !prior.LastReceivedAt.IsZero() && prior.LastReceivedAt.Before(cutoff) {
		// Payload timestamps are Unix seconds. Match that precision when
		// capping a future publisher stamp to receive-time ordering.
		cutoff = prior.LastReceivedAt.Truncate(time.Second)
	}
	if !prior.LastTargetAt.IsZero() && c.PublishedAt.Before(cutoff) {
		return true
	}
	return c.ReceivedAt.Sub(c.PublishedAt) >= w.source.StalenessThreshold
}

func (w *sourceWorker) latestFreshObservation(prior SourceState, observations ...*Observation) *Observation {
	var latest *Observation
	for _, obs := range observations {
		if obs == nil || w.isStalePayload(prior, canonical(*obs)) {
			continue
		}
		if latest == nil || obs.ReceivedAt.After(latest.ReceivedAt) {
			latest = obs
		}
	}
	return latest
}

func (w *sourceWorker) brokerRole(host string) BrokerRole {
	if host == w.primaryHost {
		return BrokerPrimary
	}
	return BrokerSecondary
}

func (w *sourceWorker) alreadyProcessedTarget(prior SourceState, c CanonicalState) bool {
	if prior.LastTargetAt.IsZero() || !c.PublishedAt.Equal(prior.LastTargetAt) {
		return false
	}
	if c.Target != prior.LastTarget {
		if c.Target == TargetOff {
			return false
		}
		for _, target := range prior.LastProcessedTargets {
			if target == c.Target {
				return true
			}
		}
		return prior.LastTarget != TargetUnknown &&
			prior.LastProcessedTarget == c.Target &&
			prior.LastProcessedTarget != prior.LastTarget
	}
	for _, target := range prior.LastProcessedTargets {
		if target == c.Target {
			return true
		}
	}
	return c.Target == prior.LastProcessedTarget
}

func recordProcessedTarget(state *SourceState, c CanonicalState) {
	if c.PublishedAt.IsZero() {
		return
	}
	state.LastProcessedTarget = c.Target
	if state.LastTargetAt.IsZero() || !c.PublishedAt.Equal(state.LastTargetAt) {
		state.LastTargetAt = c.PublishedAt
		state.LastProcessedTargets = []Target{c.Target}
		return
	}
	for _, target := range state.LastProcessedTargets {
		if target == c.Target {
			return
		}
	}
	state.LastProcessedTargets = append(state.LastProcessedTargets, c.Target)
}

func pendingEdgeSupersededBy(edge *PendingEdge, c CanonicalState, alreadyProcessed bool) bool {
	if edge == nil || edge.Target == c.Target {
		return false
	}
	if edge.Target == TargetOff && c.Target == TargetOn {
		if alreadyProcessed {
			return false
		}
		if !edge.TargetAt.IsZero() && !c.PublishedAt.IsZero() && c.PublishedAt.Equal(edge.TargetAt) {
			return false
		}
	}
	if !edge.TargetAt.IsZero() && !c.PublishedAt.IsZero() {
		switch {
		case c.PublishedAt.After(edge.TargetAt):
			return true
		case c.PublishedAt.Before(edge.TargetAt):
			if edge.TargetAt.After(edge.ReceivedAt) {
				return c.ReceivedAt.After(edge.ReceivedAt)
			}
			return false
		}
	}
	return c.ReceivedAt.After(edge.ReceivedAt)
}

func (p PendingEdge) canonical() CanonicalState {
	return CanonicalState{
		Target:      p.Target,
		PublishedAt: p.TargetAt,
		ReceivedAt:  p.ReceivedAt,
		Broker:      p.ReceivedBroker,
	}
}
