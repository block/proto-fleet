package ha

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGateRejectsPassiveRequestsAndCancelsAdmittedRequestsOnDemotion(t *testing.T) {
	gate := newGate()

	_, _, err := gate.Admit(t.Context())
	require.ErrorIs(t, err, ErrNotActive)

	activeCtx, cancelActive := context.WithCancel(t.Context())
	gate.activate(activeCtx)
	admittedCtx, release, err := gate.Admit(t.Context())
	require.NoError(t, err)
	require.True(t, gate.Active())

	cancelActive()
	require.Eventually(t, func() bool {
		return admittedCtx.Err() != nil && !gate.Active()
	}, eventuallyTimeout, eventuallyInterval)

	drained := gate.deactivate()
	require.False(t, channelClosed(drained)())
	_, _, err = gate.Admit(t.Context())
	require.ErrorIs(t, err, ErrNotActive)

	release()
	requireReceive(t, drained)
}

func TestGateReleaseIsScopedToItsActivation(t *testing.T) {
	gate := newGate()

	gate.activate(t.Context())
	_, releaseFirst, err := gate.Admit(t.Context())
	require.NoError(t, err)
	firstDrained := gate.deactivate()

	gate.activate(t.Context())
	_, releaseSecond, err := gate.Admit(t.Context())
	require.NoError(t, err)
	secondDrained := gate.deactivate()

	releaseFirst()
	requireReceive(t, firstDrained)
	require.False(t, channelClosed(secondDrained)())

	releaseSecond()
	requireReceive(t, secondDrained)
}

func channelClosed(ch <-chan struct{}) func() bool {
	return func() bool {
		select {
		case <-ch:
			return true
		default:
			return false
		}
	}
}
