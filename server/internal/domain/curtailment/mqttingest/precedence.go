package mqttingest

import (
	"net/netip"
	"sort"
	"time"
)

// BrokerRole identifies which of a source's two brokers an observation
// came from. Precedence: the lower IP wins (per the wire contract); a
// DNS-name host falls back to lexicographic ordering.
type BrokerRole int

const (
	// BrokerPrimary is the precedence-winning broker for the source.
	BrokerPrimary BrokerRole = iota
	// BrokerSecondary is the hot fallback.
	BrokerSecondary
)

// Observation is one decoded message tagged with its broker and the
// fleet receive timestamp.
type Observation struct {
	Broker     string
	Role       BrokerRole
	Payload    Payload
	ReceivedAt time.Time
}

// ResolveBrokerRoles orders the two broker hosts by precedence: the lower
// IP wins (10.0.0.9 beats 10.0.0.10 — a string sort would not), falling
// back to lexicographic order for DNS names. Equal hosts return
// ("", "", false).
func ResolveBrokerRoles(hostA, hostB string) (primary, secondary string, ok bool) {
	if hostA == hostB {
		return "", "", false
	}
	addrA, errA := netip.ParseAddr(hostA)
	addrB, errB := netip.ParseAddr(hostB)
	if errA == nil && errB == nil {
		if addrA.Compare(addrB) <= 0 {
			return hostA, hostB, true
		}
		return hostB, hostA, true
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
// per-broker observations: the primary (lower-IP) broker wins unless its
// data is older than freshnessWindow relative to the secondary, in which
// case the secondary is the live broker. A nil entry means that broker
// has no observation yet; freshnessWindow is typically 2x the publisher
// tick.
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
