package mqttingest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDecide_Transitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	canonical := func(target Target) CanonicalState {
		return CanonicalState{Target: target, ReceivedAt: now}
	}

	cases := []struct {
		name       string
		prior      PriorState
		observed   CanonicalState
		wantResult EdgeDirection
	}{
		{
			name:       "cold start OFF",
			prior:      PriorState{LastTarget: TargetUnknown},
			observed:   canonical(TargetOff),
			wantResult: EdgeOnToOff,
		},
		{
			name:       "cold start ON",
			prior:      PriorState{LastTarget: TargetUnknown},
			observed:   canonical(TargetOn),
			wantResult: EdgeNone,
		},
		{
			name:       "ON to OFF",
			prior:      PriorState{LastTarget: TargetOn},
			observed:   canonical(TargetOff),
			wantResult: EdgeOnToOff,
		},
		{
			name:       "OFF to ON",
			prior:      PriorState{LastTarget: TargetOff},
			observed:   canonical(TargetOn),
			wantResult: EdgeOffToOn,
		},
		{
			name:       "ON repeat",
			prior:      PriorState{LastTarget: TargetOn},
			observed:   canonical(TargetOn),
			wantResult: EdgeNone,
		},
		{
			name:       "OFF repeat",
			prior:      PriorState{LastTarget: TargetOff},
			observed:   canonical(TargetOff),
			wantResult: EdgeNone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Decide(tc.prior, tc.observed)

			assert.Equal(t, tc.wantResult, got)
		})
	}
}

func TestDecide_Debounce(t *testing.T) {
	t.Parallel()

	t.Run("OFF to ON within debounce window is absorbed", func(t *testing.T) {
		t.Parallel()

		edgeAt := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
		within := edgeAt.Add(2 * time.Second)

		prior := PriorState{LastTarget: TargetOff, LastEdgeAt: edgeAt}
		observed := CanonicalState{Target: TargetOn, ReceivedAt: within}

		got := Decide(prior, observed)

		assert.Equal(t, EdgeNone, got)
	})

	t.Run("ON to OFF within debounce window still curtails", func(t *testing.T) {
		t.Parallel()

		edgeAt := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
		within := edgeAt.Add(2 * time.Second)

		prior := PriorState{LastTarget: TargetOn, LastEdgeAt: edgeAt}
		observed := CanonicalState{Target: TargetOff, ReceivedAt: within}

		got := Decide(prior, observed)

		assert.Equal(t, EdgeOnToOff, got)
	})

	t.Run("flip after debounce window fires the edge", func(t *testing.T) {
		t.Parallel()

		edgeAt := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
		after := edgeAt.Add(DebounceWindow + time.Millisecond)

		prior := PriorState{LastTarget: TargetOff, LastEdgeAt: edgeAt}
		observed := CanonicalState{Target: TargetOn, ReceivedAt: after}

		got := Decide(prior, observed)

		assert.Equal(t, EdgeOffToOn, got)
	})

	t.Run("debounce only applies on transitions", func(t *testing.T) {
		t.Parallel()

		// A repeat-state observation never produces an edge regardless of
		// recency to the prior flip.
		edgeAt := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
		within := edgeAt.Add(2 * time.Second)

		prior := PriorState{LastTarget: TargetOn, LastEdgeAt: edgeAt}
		observed := CanonicalState{Target: TargetOn, ReceivedAt: within}

		assert.Equal(t, EdgeNone, Decide(prior, observed))
	})
}

func TestEvaluateWatchdog(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	threshold := 240 * time.Second

	cases := []struct {
		name         string
		lastReceived time.Time
		lastTarget   Target
		wantDecision WatchdogDecision
	}{
		{
			name:         "fresh receive, ON state — idle",
			lastReceived: now.Add(-60 * time.Second),
			lastTarget:   TargetOn,
			wantDecision: WatchdogIdle,
		},
		{
			name:         "stale receive, ON state — fire",
			lastReceived: now.Add(-300 * time.Second),
			lastTarget:   TargetOn,
			wantDecision: WatchdogFire,
		},
		{
			name:         "stale receive, already OFF — idle (curtailment holds)",
			lastReceived: now.Add(-300 * time.Second),
			lastTarget:   TargetOff,
			wantDecision: WatchdogIdle,
		},
		{
			name:         "exactly at threshold — fire (>= boundary)",
			lastReceived: now.Add(-threshold),
			lastTarget:   TargetOn,
			wantDecision: WatchdogFire,
		},
		{
			name:         "cold start (no receive ever) and ON state — fail-safe fire",
			lastReceived: time.Time{},
			lastTarget:   TargetUnknown,
			wantDecision: WatchdogFire,
		},
		{
			name:         "cold start (no receive ever) but already OFF — idle",
			lastReceived: time.Time{},
			lastTarget:   TargetOff,
			wantDecision: WatchdogIdle,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := EvaluateWatchdog(tc.lastReceived, tc.lastTarget, now, threshold)

			assert.Equal(t, tc.wantDecision, got)
		})
	}
}

func TestEdgeDirection_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "none", EdgeNone.String())
	assert.Equal(t, "on_to_off", EdgeOnToOff.String())
	assert.Equal(t, "off_to_on", EdgeOffToOn.String())
	assert.Equal(t, "watchdog_off", EdgeWatchdogOff.String())
}
