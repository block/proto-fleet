package mqttingest

import "time"

// EdgeDirection is the transition the driver must dispatch.
type EdgeDirection int

const (
	// EdgeNone is a repeat, debounced flip, or cold-start ON.
	EdgeNone EdgeDirection = iota
	// EdgeOnToOff curtails the source.
	EdgeOnToOff
	// EdgeOffToOn restores the source.
	EdgeOffToOn
	// EdgeWatchdogOff curtails because the publisher is stale.
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

// PriorState is the persisted state needed for edge detection.
type PriorState struct {
	LastTarget Target
	// LastEdgeAt anchors the debounce window.
	LastEdgeAt time.Time
}

// DebounceWindow absorbs transient OFF->ON restore flips.
const DebounceWindow = 5 * time.Second

// Decide returns the edge implied by an incoming canonical observation.
func Decide(prior PriorState, canonical CanonicalState) EdgeDirection {
	switch {
	case canonical.Target == TargetOff && prior.LastTarget != TargetOff:
		return EdgeOnToOff

	case canonical.Target == TargetOn && prior.LastTarget == TargetOff:
		if debounced(prior, canonical) {
			return EdgeNone
		}
		return EdgeOffToOn

	default:
		return EdgeNone
	}
}

func debounced(prior PriorState, canonical CanonicalState) bool {
	if prior.LastEdgeAt.IsZero() {
		return false
	}
	return canonical.ReceivedAt.Sub(prior.LastEdgeAt) < DebounceWindow
}

// WatchdogDecision is what the watchdog emits for a source.
type WatchdogDecision int

const (
	// WatchdogIdle means no synthetic OFF is owed.
	WatchdogIdle WatchdogDecision = iota
	// WatchdogFire means synthesize OFF because the source is stale.
	WatchdogFire
)

// EvaluateWatchdog decides whether staleness warrants a synthetic OFF.
func EvaluateWatchdog(lastReceivedAt time.Time, lastTarget Target, now time.Time, threshold time.Duration) WatchdogDecision {
	if lastTarget.IsOff() {
		return WatchdogIdle
	}

	if lastReceivedAt.IsZero() {
		return WatchdogFire
	}

	if now.Sub(lastReceivedAt) >= threshold {
		return WatchdogFire
	}
	return WatchdogIdle
}
