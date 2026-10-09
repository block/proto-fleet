package command

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
)

type writeOutcomeSender struct {
	calls int
	ack   *gatewaypb.ControlAck
	err   error
}

func (s *writeOutcomeSender) SendCommand(context.Context, int64, gatewaypb.CommandProtocolVersion, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	s.calls++
	return s.ack, s.err
}

func TestRemotePoolPreparationFailureDoesNotPoisonDrain(t *testing.T) {
	for _, failure := range []string{"key lookup", "sealing", "payload validation", "transport", "acknowledgement"} {
		t.Run(failure, func(t *testing.T) {
			service, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
			payload := dto.UpdateMiningPoolsPayload{DefaultPool: dto.MiningPool{URL: "stratum+tcp://pool.example.com:3333", Username: "worker"}}
			if failure == "payload validation" {
				payload = dto.UpdateMiningPoolsPayload{}
			}
			kind := commandtype.UpdateMiningPools
			var queuedPayload any = payload
			sender := &writeOutcomeSender{}
			key := func(context.Context) ([]byte, error) { return []byte("invalid key"), nil }
			if failure == "key lookup" {
				key = func(context.Context) ([]byte, error) { return nil, errors.New("key unavailable") }
			}
			// StopMining reaches transport without pool encryption, providing uncertain
			// transport/ack controls on the same durable queued-attempt path.
			if failure == "transport" || failure == "acknowledgement" {
				kind = commandtype.StopMining
				queuedPayload = nil
				if failure == "transport" {
					sender.err = context.DeadlineExceeded
				} else {
					sender.ack = &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_INTERNAL}
				}
			}
			message, _ := enqueueFenceTestCommand(t, conn, deviceID, kind, queuedPayload)
			miner, err := remotenode.New(remotenode.Config{Sender: sender, FleetNodeID: 7, OrgID: 1, DeviceIdentifier: identifier, DriverName: "proto", IPAddress: "192.0.2.10", Port: "443", URLScheme: "https", EncryptionPublicKey: key})
			require.NoError(t, err)
			ctrl := gomock.NewController(t)
			getter := minerMocks.NewMockCachedMinerGetter(ctrl)
			getter.EXPECT().GetMiner(gomock.Any(), deviceID).Return(miner, nil)
			service.minerService = getter
			_, _, err = service.executeQueuedCommand(t.Context(), message)
			require.Error(t, err)
			uncertain := failure == "transport" || failure == "acknowledgement"
			require.Equal(t, uncertain, sender.calls > 0)
			var status string
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT status FROM device_write_attempt WHERE queue_message_id = $1`, message.ID).Scan(&status))
			if uncertain {
				require.Equal(t, "unknown", status)
			} else {
				require.Equal(t, "complete", status)
			}
			writes := devicewrite.NewService(conn)
			epoch, err := writes.Reserve(t.Context(), 1, deviceID)
			require.NoError(t, err)
			drained, err := writes.Drained(t.Context(), 1, deviceID, epoch)
			require.NoError(t, err)
			require.Equal(t, !uncertain, drained)
			if !uncertain {
				require.NoError(t, writes.ReleaseHost(t.Context(), 1, deviceID, epoch))
			}
		})
	}
}

func TestQueuedRemoteBusyRetryAllowsPhysicalDrain(t *testing.T) {
	service, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
	message, messages := enqueueFenceTestCommand(t, conn, deviceID, commandtype.StopMining, nil)
	sender := &writeOutcomeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY}}
	miner, err := remotenode.New(remotenode.Config{Sender: sender, FleetNodeID: 7, OrgID: 1, DeviceIdentifier: identifier, DriverName: "proto", IPAddress: "192.0.2.10", Port: "443", URLScheme: "https"})
	require.NoError(t, err)
	ctrl := gomock.NewController(t)
	getter := minerMocks.NewMockCachedMinerGetter(ctrl)
	getter.EXPECT().GetMiner(gomock.Any(), deviceID).Return(miner, nil).Times(2)
	service.minerService = getter
	service.messageQueue = messages
	service.workerProcessCommand(t.Context(), message)
	var attemptStatus string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT status FROM device_write_attempt WHERE queue_message_id = $1`, message.ID).Scan(&attemptStatus))
	require.Equal(t, "complete", attemptStatus, "BUSY did not admit a physical write")
	retry, err := messages.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, retry, 1, "BUSY retains queue retry behavior")
	require.Equal(t, message.ID, retry[0].ID)
	sender.ack = &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true}
	service.workerProcessCommand(t.Context(), retry[0])
	require.Equal(t, 2, sender.calls)
	var unresolved int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM device_write_attempt WHERE queue_message_id = $1 AND status != 'complete'`, message.ID).Scan(&unresolved))
	require.Zero(t, unresolved)
	var finishedAt sql.NullTime
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT finished_at FROM device_write_attempt WHERE queue_message_id = $1 ORDER BY created_at DESC LIMIT 1`, message.ID).Scan(&finishedAt))
	require.True(t, finishedAt.Valid)
	writes := devicewrite.NewService(conn)
	epoch, err := writes.Reserve(t.Context(), 1, deviceID)
	require.NoError(t, err)
	drained, err := writes.Drained(t.Context(), 1, deviceID, epoch)
	require.NoError(t, err)
	require.True(t, drained, "rejected BUSY followed by successful retry must not strand ownership")
	require.NoError(t, writes.ReleaseHost(t.Context(), 1, deviceID, epoch))
}

func TestQueuedRemoteMissingStreamRetryAllowsPhysicalDrain(t *testing.T) {
	service, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
	message, messages := enqueueFenceTestCommand(t, conn, deviceID, commandtype.StopMining, nil)
	registry := control.NewRegistry()
	miner, err := remotenode.New(remotenode.Config{Sender: registry, FleetNodeID: 7, OrgID: 1, DeviceIdentifier: identifier, DriverName: "proto", IPAddress: "192.0.2.10", Port: "443", URLScheme: "https"})
	require.NoError(t, err)
	ctrl := gomock.NewController(t)
	getter := minerMocks.NewMockCachedMinerGetter(ctrl)
	getter.EXPECT().GetMiner(gomock.Any(), deviceID).Return(miner, nil).Times(2)
	service.minerService = getter
	service.messageQueue = messages
	service.workerProcessCommand(t.Context(), message)
	var attemptStatus string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT status FROM device_write_attempt WHERE queue_message_id = $1`, message.ID).Scan(&attemptStatus))
	require.Equal(t, "complete", attemptStatus, "the registry had no stream and could not enqueue a physical write")
	retry, err := messages.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, retry, 1, "missing stream remains retryable")
	require.Equal(t, message.ID, retry[0].ID)
	stream := registry.Register(7)
	defer stream.Unregister()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	acknowledged := make(chan bool, 1)
	go func() {
		select {
		case command := <-stream.Outgoing:
			stream.PublishAck(&gatewaypb.ControlAck{CommandId: command.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true})
			acknowledged <- true
		case <-ctx.Done():
			acknowledged <- false
		}
	}()
	service.workerProcessCommand(ctx, retry[0])
	require.True(t, <-acknowledged, "the successful retry reached the real registry stream")
	var unresolved int
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT count(*) FROM device_write_attempt WHERE queue_message_id = $1 AND status != 'complete'`, message.ID).Scan(&unresolved))
	require.Zero(t, unresolved)
	writes := devicewrite.NewService(conn)
	epoch, err := writes.Reserve(t.Context(), 1, deviceID)
	require.NoError(t, err)
	drained, err := writes.Drained(t.Context(), 1, deviceID, epoch)
	require.NoError(t, err)
	require.True(t, drained, "a proven pre-enqueue rejection cannot strand ownership after a successful retry")
	require.NoError(t, writes.ReleaseHost(t.Context(), 1, deviceID, epoch))
}
