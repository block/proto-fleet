package remotenode

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPerNodeLimiter_BoundsConcurrencyPerNode(t *testing.T) {
	// Arrange: cap 2 per node.
	lim := NewPerNodeLimiter(2)
	ctx := context.Background()
	r1, err := lim.Acquire(ctx, 1)
	require.NoError(t, err)
	r2, err := lim.Acquire(ctx, 1)
	require.NoError(t, err)

	// Act: a third acquire on node 1 blocks while it is at capacity.
	proceeded := make(chan struct{})
	go func() {
		release, err := lim.Acquire(ctx, 1)
		if err == nil {
			release()
		}
		close(proceeded)
	}()

	// Assert: blocked for node 1...
	select {
	case <-proceeded:
		t.Fatal("third acquire should block while node 1 is at capacity")
	case <-time.After(50 * time.Millisecond):
	}

	// ...but a different node is unaffected.
	rOther, err := lim.Acquire(ctx, 2)
	require.NoError(t, err)
	rOther()

	// Releasing a node-1 slot unblocks the third acquire.
	r1()
	select {
	case <-proceeded:
	case <-time.After(time.Second):
		t.Fatal("third acquire should proceed once a node-1 slot frees")
	}
	r2()
}

func TestPerNodeLimiter_AcquireRespectsCtx(t *testing.T) {
	// Arrange: cap 1, the only slot is taken.
	lim := NewPerNodeLimiter(1)
	release, err := lim.Acquire(context.Background(), 1)
	require.NoError(t, err)
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Act: a cancelled context makes Acquire fail instead of blocking forever.
	_, err = lim.Acquire(ctx, 1)

	// Assert
	require.Error(t, err)
}

func TestPerNodeLimiter_DefaultLimitMatchesFleetNodeCapacity(t *testing.T) {
	// Arrange: occupy every slot in the default per-node limit.
	require.Equal(t, 512, DefaultPerNodeCommandLimit)
	lim := NewPerNodeLimiter(0)
	releases := make([]func(), 0, DefaultPerNodeCommandLimit)
	for range DefaultPerNodeCommandLimit {
		release, err := lim.Acquire(context.Background(), 1)
		require.NoError(t, err)
		releases = append(releases, release)
	}

	// Act: the next acquire blocks until a slot is released.
	proceeded := make(chan struct{})
	go func() {
		release, err := lim.Acquire(context.Background(), 1)
		if err == nil {
			release()
		}
		close(proceeded)
	}()

	select {
	case <-proceeded:
		t.Fatal("next acquire should block while the default limit is at capacity")
	case <-time.After(50 * time.Millisecond):
	}

	releases[0]()
	select {
	case <-proceeded:
	case <-time.After(time.Second):
		t.Fatal("next acquire should proceed once a slot is released")
	}

	for _, release := range releases[1:] {
		release()
	}
}

func TestNestedGate_RollsBackPartialAcquisition(t *testing.T) {
	restricted := NewPerNodeLimiter(1)
	shared := NewPerNodeLimiter(1)
	gate := NewNestedGate(restricted, shared)

	sharedRelease, err := shared.Acquire(t.Context(), 1)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	_, err = gate.Acquire(ctx, 1)
	require.Error(t, err)

	// The failed shared acquire must have returned the restricted permit.
	restrictedRelease, err := restricted.Acquire(t.Context(), 1)
	require.NoError(t, err)
	restrictedRelease()
	sharedRelease()
}

func TestNestedGate_ReleaseReturnsBothPermitsExactlyOnce(t *testing.T) {
	restricted := NewPerNodeLimiter(1)
	shared := NewPerNodeLimiter(1)
	gate := NewNestedGate(restricted, shared)

	release, err := gate.Acquire(t.Context(), 1)
	require.NoError(t, err)
	release()
	release()

	secondRelease, err := gate.Acquire(t.Context(), 1)
	require.NoError(t, err)
	secondRelease()
}
