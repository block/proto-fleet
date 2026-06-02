package mqttingest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestResolveBrokerRoles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		hostA, hostB  string
		wantPrimary   string
		wantSecondary string
		wantOK        bool
	}{
		{"IP-ordered ascending", "10.155.0.3", "10.155.0.4", "10.155.0.3", "10.155.0.4", true},
		{"IP-ordered reverse", "10.155.0.4", "10.155.0.3", "10.155.0.3", "10.155.0.4", true},
		{"IP ordered by value not lexicographically", "10.155.0.10", "10.155.0.9", "10.155.0.9", "10.155.0.10", true},
		{"IP reverse multi-digit octet", "10.155.0.9", "10.155.0.10", "10.155.0.9", "10.155.0.10", true},
		{"hostname ordered", "broker-a.example", "broker-b.example", "broker-a.example", "broker-b.example", true},
		{"IP and hostname falls back to string order", "10.0.0.1", "zzz.example", "10.0.0.1", "zzz.example", true},
		{"equal hosts rejected", "10.155.0.3", "10.155.0.3", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pri, sec, ok := ResolveBrokerRoles(tc.hostA, tc.hostB)

			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantPrimary, pri)
			assert.Equal(t, tc.wantSecondary, sec)
		})
	}
}

func TestCanonicalFromPair(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	freshness := 60 * time.Second

	mkObs := func(broker string, role BrokerRole, target Target, receivedAt time.Time) *Observation {
		return &Observation{
			Broker:     broker,
			Role:       role,
			Payload:    Payload{Target: target, PublishedAt: receivedAt},
			ReceivedAt: receivedAt,
		}
	}

	t.Run("both nil yields no canonical state", func(t *testing.T) {
		t.Parallel()

		_, ok := CanonicalFromPair(nil, nil, freshness)
		assert.False(t, ok)
	})

	t.Run("primary only", func(t *testing.T) {
		t.Parallel()

		obs := mkObs("10.0.0.1", BrokerPrimary, TargetOn, now)
		canonical, ok := CanonicalFromPair(obs, nil, freshness)

		assert.True(t, ok)
		assert.Equal(t, TargetOn, canonical.Target)
		assert.Equal(t, "10.0.0.1", canonical.Broker)
	})

	t.Run("secondary only", func(t *testing.T) {
		t.Parallel()

		obs := mkObs("10.0.0.2", BrokerSecondary, TargetOff, now)
		canonical, ok := CanonicalFromPair(nil, obs, freshness)

		assert.True(t, ok)
		assert.Equal(t, TargetOff, canonical.Target)
		assert.Equal(t, "10.0.0.2", canonical.Broker)
	})

	t.Run("both fresh — primary wins regardless of payload disagreement", func(t *testing.T) {
		t.Parallel()

		pri := mkObs("10.0.0.1", BrokerPrimary, TargetOn, now)
		sec := mkObs("10.0.0.2", BrokerSecondary, TargetOff, now)

		canonical, ok := CanonicalFromPair(pri, sec, freshness)

		assert.True(t, ok)
		assert.Equal(t, TargetOn, canonical.Target)
		assert.Equal(t, "10.0.0.1", canonical.Broker)
	})

	t.Run("primary stale beyond freshness — secondary takes over", func(t *testing.T) {
		t.Parallel()

		// Primary's receive was ~120s ago; secondary is current.
		pri := mkObs("10.0.0.1", BrokerPrimary, TargetOn, now.Add(-120*time.Second))
		sec := mkObs("10.0.0.2", BrokerSecondary, TargetOff, now)

		canonical, ok := CanonicalFromPair(pri, sec, freshness)

		assert.True(t, ok)
		assert.Equal(t, TargetOff, canonical.Target)
		assert.Equal(t, "10.0.0.2", canonical.Broker)
	})

	t.Run("primary slightly older but within freshness — primary still wins", func(t *testing.T) {
		t.Parallel()

		// Primary is 30s older; freshness is 60s so primary still wins.
		pri := mkObs("10.0.0.1", BrokerPrimary, TargetOn, now.Add(-30*time.Second))
		sec := mkObs("10.0.0.2", BrokerSecondary, TargetOff, now)

		canonical, ok := CanonicalFromPair(pri, sec, freshness)

		assert.True(t, ok)
		assert.Equal(t, TargetOn, canonical.Target)
		assert.Equal(t, "10.0.0.1", canonical.Broker)
	})
}
