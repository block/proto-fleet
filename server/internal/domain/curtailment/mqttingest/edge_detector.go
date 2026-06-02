package mqttingest

import "time"

// EdgeDirection identifies the kind of state transition produced by the
// edge detector. The curtailment driver maps each direction to a
// service call (Start, Stop, or watchdog-initiated Start).
type EdgeDirection int

const (
	// EdgeNone means no transition: a repeat state, a debounced flip, or a
	// first observation matching the rehydrated state.
	EdgeNone EdgeDirection = iota
	// EdgeOnToOff fires Service.Start on the source's contracted kW.
	EdgeOnToOff
	// EdgeOffToOn fires Service.Stop on the last edge's curtailment event.
	EdgeOffToOn
	// EdgeWatchdogOff fires Service.Start under staleness — the publisher
	// hasn't been heard from within the source's threshold. Same dispatch
	// shape as EdgeOnToOff but the trigger is local, not message-driven.
	EdgeWatchdogOff
)

// String renders the direction in operator-readable form.
func (d EdgeDirection) String() string {
	switch d {
	case EdgeNone:
		return "none"
	case EdgeOnToOff:
		return "on_to_off"
	case EdgeOffToOn:
		return "off_to_on"
	case EdgeWatchdogOff:
		return "watchdog_off"
	default:
		return "unknown"
	}
}

// PriorState is the persisted state the detector needs to decide whether
// an observation is a transition. LastTarget=Unknown means cold-start.
type PriorState struct {
	// LastTarget is TargetUnknown when no message has been observed yet.
	LastTarget Target
	// LastEdgeAt is the timestamp of the most recent ON↔OFF flip; the
	// detector uses this as the debounce anchor.
	LastEdgeAt time.Time
}

// DebounceWindow is the minimum interval between opposite-direction
// edges; a flip within it is absorbed as transient noise. 5 s stays well
// inside the response SLO.
const DebounceWindow = 5 * time.Second

// Decide returns the edge direction implied by an incoming canonical
// observation against the prior state. Debounce: a transition is
// suppressed if the prior edge fired less than DebounceWindow ago.
func Decide(prior PriorState, canonical CanonicalState) EdgeDirection {
	switch {
	case canonical.Target == TargetOff && prior.LastTarget != TargetOff:
		// Includes cold-start (Unknown→OFF) and ON→OFF.
		if debounced(prior, canonical) {
			return EdgeNone
		}
		return EdgeOnToOff

	case canonical.Target == TargetOn && prior.LastTarget == TargetOff:
		if debounced(prior, canonical) {
			return EdgeNone
		}
		return EdgeOffToOn

	default:
		// Repeat states and cold-start→ON are not edges (cold-start ON has
		// no curtailment to stop).
		return EdgeNone
	}
}

func debounced(prior PriorState, canonical CanonicalState) bool {
	if prior.LastEdgeAt.IsZero() {
		return false
	}
	return canonical.ReceivedAt.Sub(prior.LastEdgeAt) < DebounceWindow
}

// WatchdogDecision is what the watchdog ticker emits for a source on
// each tick. The subscriber consumes this and dispatches accordingly.
type WatchdogDecision int

const (
	// WatchdogIdle means the source's last receive is within threshold
	// or the source is already OFF (no action needed).
	WatchdogIdle WatchdogDecision = iota
	// WatchdogFire means the source has been silent past its threshold
	// and the canonical state is not already OFF — synthesize an OFF
	// edge.
	WatchdogFire
)

// EvaluateWatchdog decides whether staleness warrants synthesizing an OFF
// edge. A zero lastReceivedAt means cold-start (no message ever received).
func EvaluateWatchdog(lastReceivedAt time.Time, lastTarget Target, now time.Time, threshold time.Duration) WatchdogDecision {
	// Already OFF — the curtailment event still holds; nothing to do.
	if lastTarget.IsOff() {
		return WatchdogIdle
	}

	// Cold start: never received a message. Fail-safe — curtail until
	// the publisher comes online.
	if lastReceivedAt.IsZero() {
		return WatchdogFire
	}

	if now.Sub(lastReceivedAt) >= threshold {
		return WatchdogFire
	}
	return WatchdogIdle
}
