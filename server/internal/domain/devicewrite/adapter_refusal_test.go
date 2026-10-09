package devicewrite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	"github.com/stretchr/testify/require"
)

func TestAdapterAdmissionRefusalInsideOuterDispatchDoesNotBlockTransfer(t *testing.T) {
	database, org, device, message, _ := queuedFenceFixture(t)
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginQueuedHost(t.Context(), message)
	require.NoError(t, err)
	sender := &admissionTestSender{}
	denied := errors.New("binding revalidation refused")
	adapter, err := remotenode.New(remotenode.Config{
		Sender: sender, FleetNodeID: 7, OrgID: org, DeviceIdentifier: "adapter-denial",
		DriverName: "proto", IPAddress: "192.0.2.1", Port: "4028", URLScheme: "http",
		WriteGate: func(context.Context, commandtype.Type, func(context.Context) error) error { return denied },
	})
	require.NoError(t, err)
	outcome := devicewrite.Dispatch(attempt.Context(t.Context()), adapter.Reboot)
	require.ErrorIs(t, outcome, denied)
	require.True(t, devicewrite.IsNotDispatched(outcome))
	require.Zero(t, sender.calls)
	require.NoError(t, attempt.Finish(t.Context(), outcome))
	inspections, err := service.InspectRecovery(t.Context(), org, device)
	require.NoError(t, err)
	require.Empty(t, inspections, "a refusal before transport must not leave an unknown attempt")
	_, err = service.Reserve(t.Context(), org, device)
	require.NoError(t, err, "the undispatched refusal must not require operator recovery")
}
