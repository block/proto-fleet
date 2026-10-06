package command

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	tmodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/gateway"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
)

// Exercise the real remote boundary: execution's open reader is not the reader
// used by a Fleet Node, which downloads the granted ID in a later HTTP request.
func TestExecuteCommandOnDevice_FirmwareDeliveryLease(t *testing.T) {
	for _, identity := range []string{"checksum", "legacy ID"} {
		for _, outcome := range []string{"success", "error ack", "cancellation", "disconnect"} {
			t.Run(identity+"/"+outcome, func(t *testing.T) {
				t.Chdir(t.TempDir())
				ctrl := gomock.NewController(t)
				filesService, err := files.NewService(files.Config{})
				require.NoError(t, err)
				const content = "firmware payload served after command dispatch"
				fileID, err := filesService.SaveFirmwareFile("update.swu", strings.NewReader(content), files.FirmwareMetadata{
					TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "1.2.3",
				})
				require.NoError(t, err)
				artifact, err := filesService.ResolveFirmwareArtifact(fileID)
				require.NoError(t, err)

				registry := control.NewRegistry()
				stream := registry.Register(44)
				t.Cleanup(stream.Unregister)
				remote, err := remotenode.New(remotenode.Config{
					Sender: registry, FleetNodeID: 44, OrgID: 1,
					DeviceIdentifier: "miner-a", DriverName: "virtual",
					IPAddress: "10.0.0.5", Port: "4028", URLScheme: "http",
				})
				require.NoError(t, err)
				minerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
				minerGetter.EXPECT().GetMiner(gomock.Any(), int64(42)).Return(remote, nil)
				deviceStore := storeMocks.NewMockDeviceStore(ctrl)
				if outcome == "success" {
					deviceStore.EXPECT().GetDeviceStatusForDeviceIdentifiers(gomock.Any(), []tmodels.DeviceIdentifier{"miner-a"}).
						Return(map[tmodels.DeviceIdentifier]models.MinerStatus{}, nil)
				}
				svc := NewExecutionService(&Config{}, nil, nil, nil, nil, minerGetter, deviceStore, nil, filesService)
				client := firmwareDeliveryClient(t, registry, filesService)
				payload := dto.FirmwareUpdatePayload{FirmwareFileID: fileID}
				if identity == "checksum" {
					payload.FirmwareChecksum = artifact.Checksum
				}
				payloadBytes, err := json.Marshal(payload)
				require.NoError(t, err)

				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				result := make(chan error, 1)
				finished := make(chan struct{})
				go func() {
					defer close(finished)
					_, _, err := svc.executeCommandOnDevice(ctx, commandtype.FirmwareUpdate, queue.Message{
						ID: 9, DeviceID: 42, CommandType: commandtype.FirmwareUpdate, Payload: payloadBytes,
					})
					result <- err
				}()
				t.Cleanup(func() {
					cancel()
					select {
					case <-finished:
					case <-time.After(3 * time.Second):
						t.Error("firmware execution did not stop after cancellation")
					}
				})

				command, action := receiveFirmwareDeliveryCommand(t, stream)
				ref := action.GetFirmwareUpdate().GetArtifact()
				require.NotNil(t, ref)
				require.Equal(t, fileID, ref.GetArtifactId())
				require.True(t, fleeterror.IsFailedPreconditionError(filesService.DeleteFirmwareFile(fileID)),
					"the exact granted path must remain available until the remote command ends")

				// Another copy with the same checksum cannot use the original
				// download grant. Its existence also must not block unrelated deletion.
				otherID, err := filesService.SaveFirmwareFile("other.swu", strings.NewReader(content), files.FirmwareMetadata{
					TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "2.0.0",
				})
				require.NoError(t, err)
				require.NotEqual(t, fileID, otherID)
				otherRef, ok := proto.Clone(ref).(*pb.CommandArtifactRef)
				require.True(t, ok)
				otherRef.ArtifactId = otherID
				denied, err := client.DownloadCommandArtifact(ctx, connect.NewRequest(&pb.DownloadCommandArtifactRequest{
					CommandId: command.GetCommandId(), Artifact: otherRef, DeviceIdentifier: "miner-a",
				}))
				require.NoError(t, err)
				require.False(t, denied.Receive())
				require.Error(t, denied.Err())
				require.NoError(t, denied.Close())
				require.NoError(t, filesService.DeleteFirmwareFile(otherID))

				download, err := client.DownloadCommandArtifact(ctx, connect.NewRequest(&pb.DownloadCommandArtifactRequest{
					CommandId: command.GetCommandId(), Artifact: ref, DeviceIdentifier: "miner-a",
				}))
				require.NoError(t, err)
				var received bytes.Buffer
				var header *pb.CommandArtifactRef
				for download.Receive() {
					if msg := download.Msg().GetHeader(); msg != nil {
						header = msg.GetArtifact()
					} else {
						_, err := received.Write(download.Msg().GetChunk().GetData())
						require.NoError(t, err)
					}
				}
				require.NoError(t, download.Err())
				require.NoError(t, download.Close())
				require.True(t, proto.Equal(ref, header))
				require.Equal(t, content, received.String())

				switch outcome {
				case "success":
					ackFirmwareDeliveryCommand(stream, command)
					statusCommand, statusAction := receiveFirmwareDeliveryCommand(t, stream)
					require.NotNil(t, statusAction.GetGetFirmwareUpdateStatus())
					// Polling is still waiting for its reply, but payload delivery
					// finished and must no longer keep this file pinned.
					require.NoError(t, filesService.DeleteFirmwareFile(fileID))
					ackFirmwareDeliveryCommand(stream, statusCommand)
					rebootCommand, rebootAction := receiveFirmwareDeliveryCommand(t, stream)
					require.NotNil(t, rebootAction.GetReboot())
					ackFirmwareDeliveryCommand(stream, rebootCommand)
				case "error ack":
					stream.PublishAck(&pb.ControlAck{
						CommandId: command.GetCommandId(), Code: pb.AckCode_ACK_CODE_BAD_REQUEST, ErrorMessage: "firmware rejected",
					})
				case "cancellation":
					cancel()
				case "disconnect":
					stream.Unregister()
				}
				select {
				case err := <-result:
					if outcome == "success" {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
						require.NoError(t, filesService.DeleteFirmwareFile(fileID))
					}
				case <-time.After(3 * time.Second):
					t.Fatal("firmware command did not finish after its terminal outcome")
				}
			})
		}
	}
}

func firmwareDeliveryClient(t *testing.T, registry *control.Registry, filesService *files.Service) fleetnodegatewayv1connect.FleetNodeGatewayServiceClient {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(
		gateway.NewHandler(nil, nil, nil, registry, filesService),
		connect.WithInterceptors(interceptors.NewErrorMappingInterceptor()),
	))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := authn.SetInfo(r.Context(), &auth.Subject{FleetNodeID: 44, OrgID: 1, Name: "firmware-delivery-test"})
		mux.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(server.Close)
	return fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(server.Client(), server.URL)
}

func receiveFirmwareDeliveryCommand(t *testing.T, stream *control.Stream) (*pb.ControlCommand, *pb.MinerCommand) {
	t.Helper()
	select {
	case command := <-stream.Outgoing:
		var envelope pb.AgentCommand
		require.NoError(t, proto.Unmarshal(command.GetPayload(), &envelope))
		require.NotNil(t, envelope.GetMinerCommand())
		return command, envelope.GetMinerCommand()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for remote firmware command")
		return nil, nil
	}
}

func ackFirmwareDeliveryCommand(stream *control.Stream, command *pb.ControlCommand) {
	stream.PublishAck(&pb.ControlAck{CommandId: command.GetCommandId(), Succeeded: true, Code: pb.AckCode_ACK_CODE_OK})
}
