package mininggroups

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestActivationWaitingMessage(t *testing.T) {
	for _, tc := range []struct{ reason, message string }{
		{"stop self mining before activating groups", "Waiting for mining to stop. Activation will retry automatically."},
		{"wait for 3 active boards to stop before activating groups", "Waiting for mining to stop. Activation will retry automatically."},
		{"wait for 2 factory run-in reservations to be released before activating groups", "Finish or cancel factory tests and release their board reservations. Activation will retry automatically."},
		{"HA owner controller-a has no fresh hosted-capable session", "A controller is offline or does not support mining groups. Reconnect it or update its firmware; activation will retry automatically."},
		{"HA owner IDs are unavailable", "Check which controllers are configured to manage this container. Activation will retry automatically."},
		{"unexpected live owner controller-b; include every eligible owner", "Check which controllers are configured to manage this container. Activation will retry automatically."},
		{"the container power controller must be enabled before activating mining groups", "Enable the container's power controller. Activation will retry automatically."},
		{"release isolated boards before activation", "End isolated board maintenance on this container. Activation will retry automatically."},
		{"connection failure; private-password", "The controller has not confirmed activation. Fleet will retry automatically."},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			err := status.Error(codes.Unavailable, tc.reason)
			require.Equal(t, tc.message, activationWaitingMessage(err))
			require.Equal(t, tc.message, activationWaitingMessage(fmt.Errorf("fleet node reported command failure: execute command: %w", err)))
		})
	}
}

func TestActivationReportsBlockerAndCompletesAfterItClears(t *testing.T) {
	ctx, f := newRetryFixture(t)
	f.prepareActivation(t, ctx)
	prepared := f.state(t)
	require.True(t, prepared.Step.Valid)
	for _, reason := range []string{"stop self mining before activating groups", "wait for 2 factory run-in reservations to be released before activating groups", "HA owner controller-a has no fresh hosted-capable session"} {
		f.miner.activationErr = status.Error(codes.Unavailable, reason)
		require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
		blocked := f.state(t)
		require.Equal(t, "blocked", blocked.Status)
		require.Equal(t, activationWaitingMessage(f.miner.activationErr), blocked.Message)
		require.Equal(t, prepared.StepKey, blocked.StepKey, "keep retrying the accepted request")
	}
	f.miner.activationErr = nil
	require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
	require.Equal(t, statusApplying, f.state(t).Status)
	require.NoError(t, f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier))
	require.Equal(t, statusComplete, f.state(t).Status)
	require.Equal(t, "Configuration applied", f.state(t).Message)
}

func TestActivationLeavesDefaultStoppedUntilOperatorStartsIt(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(fmt.Sprint(running), func(t *testing.T) {
			ctx, f := newRetryFixture(t)
			f.miner.mining = running
			f.prepareActivation(t, ctx)
			require.Equal(t, running, f.state(t).ActivationResumeMining.Bool)
			require.False(t, f.miner.mining)
			f.reconcile(t, ctx) // activate
			require.True(t, f.miner.enabled)
			require.NotEqual(t, statusComplete, f.state(t).Status)
			f.reconcile(t, ctx) // confirm activation
			require.Equal(t, statusComplete, f.state(t).Status)
			require.False(t, f.miner.mining)
			if running {
				require.Equal(t, "Mining groups enabled. Start mining in ProtoOS when ready.", f.state(t).Message)
			}
			for _, call := range f.miner.calls {
				require.NotEqual(t, "start", call.Action, "activation must not start Default")
				require.NotEqual(t, "replace_pools", call.Action, "activation preserves existing pools")
				require.NotEqual(t, "transfer", call.Action, "activation does not reassign boards")
			}
		})
	}
}

func TestActivationWaitsForShutdownAndPreservesStateAfterLostStopReply(t *testing.T) {
	ctx, f := newRetryFixture(t)
	f.miner.mining = true
	f.reconcile(t, ctx) // save original state
	f.miner.loseStop = true
	f.reconcile(t, ctx) // controller stops, reply lost
	require.True(t, f.state(t).ActivationResumeMining.Bool)
	require.False(t, f.state(t).ActivationStopRequested)
	require.False(t, f.miner.mining)
	f.service = NewService(f.conn, f.miner, f.service.pools, f.service.secrets)
	f.miner.holdStop = true
	f.miner.activeBoards = 3
	f.reconcile(t, ctx) // replay stop with same identity
	require.True(t, f.state(t).ActivationStopRequested)
	f.reconcile(t, ctx) // wait for board shutdown despite mining=false
	require.False(t, f.state(t).Step.Valid)
	require.False(t, f.miner.enabled)
	require.Equal(t, "Waiting for mining to stop", f.state(t).Message)
	var stopKeys []string
	for _, call := range f.miner.calls {
		if call.Action == "stop_legacy" {
			stopKeys = append(stopKeys, call.Key)
		}
	}
	require.Len(t, stopKeys, 2)
	require.NotEmpty(t, stopKeys[0])
	require.Equal(t, stopKeys[0], stopKeys[1])
	f.miner.activeBoards = 0
	for range 5 {
		f.reconcile(t, ctx)
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	require.False(t, f.miner.mining)
	require.Equal(t, "Mining groups enabled. Start mining in ProtoOS when ready.", f.state(t).Message)
}

func TestActivationRequiresInitialStatusBeforeStopping(t *testing.T) {
	ctx, f := newRetryFixture(t)
	f.miner.statusErr = true
	f.reconcile(t, ctx)
	require.False(t, f.state(t).ActivationResumeMining.Valid)
	for _, call := range f.miner.calls {
		require.True(t, call.ReadOnly())
	}
	f.miner.statusErr = false
	f.miner.enabled = true // activated externally before Fleet recorded or stopped anything
	f.reconcile(t, ctx)
	require.Equal(t, statusComplete, f.state(t).Status)
	for _, call := range f.miner.calls {
		require.True(t, call.ReadOnly())
	}
}

func TestActivationReplaysLostActivationReceiptWithoutStartingDefault(t *testing.T) {
	ctx, f := newRetryFixture(t)
	f.miner.mining = true
	f.prepareActivation(t, ctx)
	activationKey := f.state(t).StepKey
	f.miner.loseActivate = true
	f.reconcile(t, ctx)
	require.True(t, f.miner.enabled)
	require.Equal(t, activationKey, f.state(t).StepKey)
	f.service = NewService(f.conn, f.miner, f.service.pools, f.service.secrets)
	f.reconcile(t, ctx) // replay activation despite enabled observation
	require.False(t, f.state(t).Step.Valid)
	f.reconcile(t, ctx) // verify activation
	require.Equal(t, statusComplete, f.state(t).Status)
	require.False(t, f.miner.mining)
	var activationKeys []string
	for _, call := range f.miner.calls {
		require.NotEqual(t, "start", call.Action)
		if call.Action == "activate" {
			activationKeys = append(activationKeys, call.Key)
		}
	}
	require.Equal(t, []string{activationKey.UUID.String(), activationKey.UUID.String()}, activationKeys)
}
