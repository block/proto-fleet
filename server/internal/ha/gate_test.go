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
	defer release()
	require.True(t, gate.Active())

	cancelActive()
	require.Eventually(t, func() bool {
		return admittedCtx.Err() != nil && !gate.Active()
	}, eventuallyTimeout, eventuallyInterval)
}
