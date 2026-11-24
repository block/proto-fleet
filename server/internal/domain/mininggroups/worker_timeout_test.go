package mininggroups

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReconciliationBudgetAllowsDelayedNodeAcknowledgementAndCommit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), reconciliationTimeout)
		defer cancel()
		// Admission and transport consume part of the outer budget before the
		// node begins its independently bounded 25-second command.
		time.Sleep(2 * time.Second)
		nodeCtx, stopNode := context.WithTimeout(ctx, 25*time.Second)
		defer stopNode()
		select {
		case <-time.After(24 * time.Second):
		case <-nodeCtx.Done():
			t.Fatal("reconciliation canceled before the node's acknowledgement")
		}
		// Returning the receipt and committing the journal also need headroom.
		time.Sleep(2 * time.Second)
		require.NoError(t, ctx.Err(), "the receipt must be persisted before the outer deadline")
	})
}
