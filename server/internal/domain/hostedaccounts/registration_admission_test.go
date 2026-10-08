package hostedaccounts

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestRegistrationAdmissionBudgets(t *testing.T) {
	now := time.Now()
	gate := newRegistrationAdmission(registrationLimits{window: time.Minute, perInvitation: 2, perClient: 2, aggregate: 3, concurrent: 2, maxKeys: 4}, func() time.Time { return now })
	ctx := t.Context()
	// Equivalent trusted IPv4 forms cannot reset a client's budget.
	require.NoError(t, gate.checkClient(ctx, "192.0.2.1"))
	require.NoError(t, gate.checkClient(ctx, "::ffff:192.0.2.1"))
	require.True(t, fleeterror.IsResourceExhaustedError(gate.checkClient(ctx, "192.0.2.1")))
	for range 20 {
		require.NoError(t, gate.checkClient(ctx, ""), "internal callers do not share an artificial client bucket")
	}
	release, err := gate.acquireHash(ctx, "link-one-digest")
	require.NoError(t, err)
	release()
	release() // Safe deferred release after early release.
	release, err = gate.acquireHash(ctx, "link-one-digest")
	require.NoError(t, err)
	release()
	_, err = gate.acquireHash(ctx, "link-one-digest")
	require.True(t, fleeterror.IsResourceExhaustedError(err))
	release, err = gate.acquireHash(ctx, "link-two-digest")
	require.NoError(t, err)
	release()
	_, err = gate.acquireHash(ctx, "link-three-digest")
	require.True(t, fleeterror.IsResourceExhaustedError(err), "rotating valid links cannot evade the aggregate allowance")
	require.Zero(t, gate.active)
	now = now.Add(time.Minute)
	require.NoError(t, gate.checkClient(ctx, "192.0.2.1"))
	release, err = gate.acquireHash(ctx, "link-one-digest")
	require.NoError(t, err)
	release()
}

func TestRegistrationAdmissionCapacityAndCancellation(t *testing.T) {
	now := time.Now()
	gate := newRegistrationAdmission(registrationLimits{window: time.Minute, perInvitation: 10, perClient: 10, aggregate: 10, concurrent: 1, maxKeys: 2}, func() time.Time { return now })
	ctx := t.Context()
	for i := range 2 {
		require.NoError(t, gate.checkClient(ctx, fmt.Sprintf("192.0.2.%d", i+1)))
		release, err := gate.acquireHash(ctx, fmt.Sprintf("digest-%d", i))
		require.NoError(t, err)
		release()
	}
	require.True(t, fleeterror.IsResourceExhaustedError(gate.checkClient(ctx, "192.0.2.3")))
	_, err := gate.acquireHash(ctx, "digest-2")
	require.True(t, fleeterror.IsResourceExhaustedError(err))
	require.Len(t, gate.clients, 2)
	require.Len(t, gate.invitations, 2)
	// A rotating identity cannot evict either still-live budget.
	release, err := gate.acquireHash(ctx, "digest-0")
	require.NoError(t, err)
	_, err = gate.acquireHash(ctx, "digest-1")
	require.True(t, fleeterror.IsResourceExhaustedError(err), "no queued work behind an occupied hash slot")
	release()
	before := gate.total.count
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = gate.acquireHash(canceled, "digest-0")
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, gate.checkClient(canceled, "192.0.2.1"), context.Canceled)
	require.Equal(t, before, gate.total.count)
	now = now.Add(time.Minute)
	require.NoError(t, gate.checkClient(ctx, "192.0.2.3"))
	release, err = gate.acquireHash(ctx, "digest-2")
	require.NoError(t, err)
	release()
	require.Len(t, gate.clients, 1)
	require.Len(t, gate.invitations, 1)
}

func TestRegistrationServicesShareProductionAdmissionAndDefaultBcryptCost(t *testing.T) {
	first := NewInvitationService(nil, nil)
	second := NewInvitationService(nil, nil)
	require.Same(t, first.admission, second.admission)
	require.Same(t, sharedRegistrationAdmission, first.admission)
	hash, err := first.hashPassword([]byte("password123"))
	require.NoError(t, err)
	cost, err := bcrypt.Cost(hash)
	require.NoError(t, err)
	require.Equal(t, bcrypt.DefaultCost, cost)
}

func TestRegistrationAdmissionReclaimsExpiredCapacityBetweenScheduledPrunes(t *testing.T) {
	now := time.Now()
	gate := newRegistrationAdmission(registrationLimits{window: time.Minute, perInvitation: 20, perClient: 20, aggregate: 20, concurrent: 2, maxKeys: 2}, func() time.Time { return now })
	add := func(ip, digest string) {
		require.NoError(t, gate.checkClient(t.Context(), ip))
		release, err := gate.acquireHash(t.Context(), digest)
		require.NoError(t, err)
		release()
	}
	add("192.0.2.1", "one")
	now = now.Add(30 * time.Second)
	add("192.0.2.2", "two")
	now = now.Add(30 * time.Second)
	// The periodic prune removes the first entry, while the second survives.
	add("192.0.2.3", "three")
	now = now.Add(30 * time.Second)
	// Entry two expires before the next scheduled prune. Capacity is reusable.
	add("192.0.2.4", "four")
	require.Len(t, gate.clients, 2)
	require.Len(t, gate.invitations, 2)
}
