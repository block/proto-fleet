package mqttingest

import (
	"net/netip"
	"sort"
	"time"
)

// BrokerRole identifies a source's precedence-ordered brokers.
type BrokerRole int

const (
	BrokerPrimary BrokerRole = iota
	BrokerSecondary
)

// Observation is one decoded broker message.
type Observation struct {
	Broker     string
	Role       BrokerRole
	Payload    Payload
	ReceivedAt time.Time
}

// ResolveBrokerRoles orders brokers by lower IP, falling back to lexical DNS.
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
	Broker      string
}

// CanonicalFromPair picks primary unless it is stale relative to secondary.
func CanonicalFromPair(primary, secondary *Observation, freshnessWindow time.Duration) (CanonicalState, bool) {
	switch {
	case primary == nil && secondary == nil:
		return CanonicalState{}, false
	case secondary == nil:
		return canonical(*primary), true
	case primary == nil:
		return canonical(*secondary), true
	}

	if primary.ReceivedAt.Add(freshnessWindow).Before(secondary.ReceivedAt) {
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
