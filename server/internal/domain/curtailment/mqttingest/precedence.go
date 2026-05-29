package mqttingest

import (
	"sort"
	"time"
)

// BrokerRole identifies which of a source's two brokers an observation
// came from. Precedence is resolved by host string ordering — the
// lexicographically lower host wins, which matches the wire contract's
// "lower IP wins" rule when broker hosts are dotted-quad IPs. Hostname
// configurations get a stable but operator-visible ordering.
type BrokerRole int

const (
	// BrokerPrimary is the precedence-winning broker for the source.
	BrokerPrimary BrokerRole = iota
	// BrokerSecondary is the hot fallback.
	BrokerSecondary
)

// Observation is one decoded message tagged with its broker and the
// fleet receive timestamp. Precedence dedup operates on Observations.
type Observation struct {
	Broker     string
	Role       BrokerRole
	Payload    Payload
	ReceivedAt time.Time
}

// ResolveBrokerRoles orders the two configured broker hosts by
// precedence. The lower-string-comparison host wins; the other is the
// hot fallback. Equal strings (operator misconfig caught by the DB
// CHECK) return ("", "", false).
func ResolveBrokerRoles(hostA, hostB string) (primary, secondary string, ok bool) {
	if hostA == hostB {
		return "", "", false
	}
	hosts := []string{hostA, hostB}
	sort.Strings(hosts)
	return hosts[0], hosts[1], true
}

// CanonicalState is the deduped state the edge detector consumes.
type CanonicalState struct {
	Target      Target
	PublishedAt time.Time
	ReceivedAt  time.Time
	// Broker is the host whose observation won precedence. Surfaces in
	// state metadata + audit rows.
	Broker string
}

// CanonicalFromPair picks the canonical observation from up to two
// per-broker latest-observations. The rules:
//
//   - If only one broker has data, that broker wins.
//   - If both brokers have data and the secondary's last receive is
//     within freshnessWindow of the primary's, primary wins.
//   - If the primary's data is older than freshnessWindow relative to
//     the secondary's, secondary is the live broker and wins.
//
// freshnessWindow is the threshold the caller picks (typically 2x the
// publisher's expected tick). nil entries mean that broker has not
// produced an observation yet.
func CanonicalFromPair(primary, secondary *Observation, freshnessWindow time.Duration) (CanonicalState, bool) {
	switch {
	case primary == nil && secondary == nil:
		return CanonicalState{}, false
	case secondary == nil:
		return canonical(*primary), true
	case primary == nil:
		return canonical(*secondary), true
	}

	// Both present; compare freshness against the other side.
	if primary.ReceivedAt.Add(freshnessWindow).Before(secondary.ReceivedAt) {
		// Primary is stale compared to secondary; secondary drives state.
		return canonical(*secondary), true
	}
	return canonical(*primary), true
}

func canonical(o Observation) CanonicalState {
	return CanonicalState{
		Target:      o.Payload.Target,
		PublishedAt: o.Payload.PublishedAt,
		ReceivedAt:  o.ReceivedAt,
		Broker:      o.Broker,
	}
}
