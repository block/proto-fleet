package mininggroups

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewPolicyRestrictionEndsSettledOperation(t *testing.T) {
	ctx, f, m, _, _ := newTransferFailureFixture(t)
	// The transfer was accepted before the controller acquired custom policy.
	m.groups[0].CustomPolicy = true
	f.reconcile(t, ctx)
	require.Equal(t, "failed", f.state(t).Status)
	require.False(t, f.state(t).Step.Valid)
	require.Empty(t, m.receipts)
	_, err := f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
	require.NoError(t, err)
	intents, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Empty(t, intents)
}

func TestPolicyRestrictionCannotDiscardUnresolvedTransfer(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "lost_response"}[lost], func(t *testing.T) {
			ctx, f, m, _, _ := newTransferFailureFixture(t)
			m.transferPhase = pendingTransferPhase
			m.loseReply = lost
			f.reconcile(t, ctx)
			f.reconcile(t, ctx)
			original := f.state(t)
			m.groups[0].CustomPolicy = true
			for range 3 {
				f.reconcile(t, ctx)
			}
			require.Equal(t, original.Step, f.state(t).Step)
			require.Equal(t, original.StepKey, f.state(t).StepKey)
			require.True(t, active(f.state(t).Status))
			require.Len(t, groupActivities(t, f), 1, "unresolved work must retain only its acceptance event")
			_, err := f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
			require.Error(t, err)
		})
	}
}

func TestPolicyChangeAfterConfigurationPreservesCompletedSetup(t *testing.T) {
	ctx, f, m, config, boards := newIdlePoolAssignmentFixture(t)
	m.groups[1].Pools = nil
	m.groups[1].Mining = false
	controller := &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 289, Groups: m.groups}, receipts: map[string]sdk.MiningGroupResult{}}
	f.service.miners = controller
	_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	_, err = f.service.submitLegacyRequest(ctx, f.target.DeviceIdentifier, uuid.NewString(), config, boards, f.state(t).Revision)
	require.NoError(t, err)
	f.reconcile(t, ctx) // Prepare pool configuration.
	f.reconcile(t, ctx) // Apply it and retain the successful receipt.
	applied := f.state(t)
	require.False(t, applied.Step.Valid)
	require.NotEmpty(t, controller.snapshot.Groups[1].Pools)
	controller.snapshot.Groups[0].CustomPolicy = true
	controller.calls = nil
	f.reconcile(t, ctx)
	require.Equal(t, "failed", f.state(t).Status)
	require.Equal(t, applied.Bindings, f.state(t).Bindings)
	require.False(t, controller.snapshot.Groups[1].Mining, "newly unsupported work must stop before starting the destination")
	for _, call := range controller.calls {
		require.True(t, call.ReadOnly())
	}
	_, err = f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
	require.NoError(t, err)
}
