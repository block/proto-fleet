package command

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	minerDomain "github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
)

const steppedFirmwareImage = "firmware image"

// steppedFirmware is the device side of an update that runs in steps, the way
// the CT1 plugin updates PSU modules: FirmwareUpdate starts the next step, and
// a status poll only reports whether one is ready. Every poll finds the
// running step finished.
type steppedFirmware struct {
	mu    sync.Mutex
	steps int
	// requests holds the request ID of every FirmwareUpdate that reached the device.
	requests []string
	polls    int
	// onPoll runs before each status report with the number of steps started.
	onPoll func(started int)
}

func (f *steppedFirmware) start(requestID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, requestID)
}

func (f *steppedFirmware) status() *sdk.FirmwareUpdateStatus {
	f.mu.Lock()
	f.polls++
	started := len(f.requests)
	f.mu.Unlock()
	if f.onPoll != nil {
		f.onPoll(started)
	}
	progress := started * 100 / f.steps
	state := sdk.FirmwareStateContinue
	if started == f.steps {
		state = "completed"
	}
	return &sdk.FirmwareUpdateStatus{State: state, Progress: &progress}
}

func (f *steppedFirmware) seen() (requests []string, polls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...), f.polls
}

// steppedFirmwareDevice is the device behind a directly managed miner.
type steppedFirmwareDevice struct {
	*sdkMocks.MockDevice
	update *steppedFirmware
}

func (d steppedFirmwareDevice) FirmwareUpdate(_ context.Context, file sdk.FirmwareFile) error {
	data, err := io.ReadAll(file.Reader)
	if err != nil {
		return fmt.Errorf("read firmware: %w", err)
	}
	if string(data) != steppedFirmwareImage {
		return fmt.Errorf("every step must receive the whole firmware file, got %q", data)
	}
	d.update.start(file.RequestID)
	return nil
}

func (d steppedFirmwareDevice) GetFirmwareUpdateStatus(context.Context) (*sdk.FirmwareUpdateStatus, error) {
	return d.update.status(), nil
}

// steppedFirmwareNode is the Fleet Node in front of a remotely managed miner.
type steppedFirmwareNode struct {
	update *steppedFirmware
}

func (n steppedFirmwareNode) SendCommand(ctx context.Context, nodeID int64, version gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	ack, _, err := n.SendCommandWithArtifactResults(ctx, nodeID, version, cmd, nil)
	return ack, err
}

func (n steppedFirmwareNode) SendCommandWithArtifactResults(_ context.Context, _ int64, _ gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand, _ []control.ArtifactExpectation) (*gatewaypb.ControlAck, []*gatewaypb.CommandArtifactRef, error) {
	var envelope gatewaypb.AgentCommand
	if err := proto.Unmarshal(cmd.GetPayload(), &envelope); err != nil {
		return nil, nil, fmt.Errorf("decode command: %w", err)
	}
	ack := &gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Succeeded: true, Code: gatewaypb.AckCode_ACK_CODE_OK}
	switch action := envelope.GetMinerCommand().GetAction().(type) {
	case *gatewaypb.MinerCommand_FirmwareUpdate:
		n.update.start(action.FirmwareUpdate.GetRequestId())
	case *gatewaypb.MinerCommand_GetFirmwareUpdateStatus:
		current := n.update.status()
		payload, err := proto.Marshal(&gatewaypb.FirmwareUpdateStatusResult{State: current.State, Progress: proto.Int32(int32(*current.Progress))}) //nolint:gosec // a percentage
		if err != nil {
			return nil, nil, fmt.Errorf("encode status: %w", err)
		}
		ack.Payload = payload
	default:
		return nil, nil, fmt.Errorf("unexpected command %T", action)
	}
	return ack, nil, nil
}

// A status poll passes no write admission, so a device that updates in steps
// starts each one only when the firmware command is sent again. Each send goes
// through the miner's write gate: once the command's queue message is reaped
// or the device's write fence is reserved, no further step reaches the device.
func TestFirmwareStepsPassWriteAdmission(t *testing.T) {
	interval := firmwareInstallPollInterval
	firmwareInstallPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { firmwareInstallPollInterval = interval })

	for _, route := range []string{"direct", "remote"} {
		for _, lost := range []string{"", "queue message reaped", "write fence reserved"} {
			name := route + "/admitted throughout"
			if lost != "" {
				name = route + "/" + lost
			}
			t.Run(name, func(t *testing.T) {
				_, conn, crypto, deviceID, identifier := setupPasswordCommandDevice(t, "antminer", true)
				t.Chdir(t.TempDir())
				filesService, err := files.NewService(files.Config{})
				require.NoError(t, err)
				fileID, err := filesService.SaveFirmwareFile("update.swu", strings.NewReader(steppedFirmwareImage), files.FirmwareMetadata{
					TargetManufacturer: "TestCorp", TargetModel: "TestMiner", FirmwareVersion: "2.0.0",
				})
				require.NoError(t, err)

				update := &steppedFirmware{steps: 4}
				ctrl := gomock.NewController(t)
				driver := sdkMocks.NewMockDriver(ctrl)
				minerService := minerDomain.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, filesService, &commandTestPluginManager{driverName: "antminer", driver: driver})
				if route == "direct" {
					device := steppedFirmwareDevice{MockDevice: sdkMocks.NewMockDevice(ctrl), update: update}
					driver.EXPECT().NewDevice(gomock.Any(), identifier, gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil).AnyTimes()
				} else {
					var nodeID int64
					require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status)
 VALUES(1,'stepped-firmware-node',$1,$2,'CONFIRMED') RETURNING id`, []byte("stepped-firmware-identity"), []byte("01234567890123456789012345678901")).Scan(&nodeID))
					_, err = conn.ExecContext(t.Context(), "INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)", nodeID, deviceID)
					require.NoError(t, err)
					minerService.WithCommandSender(steppedFirmwareNode{update: update})
				}

				message, messages := enqueueFenceTestCommand(t, conn, deviceID, commandtype.FirmwareUpdate, dto.FirmwareUpdatePayload{FirmwareFileID: fileID})
				// Admission is lost while the second step runs. The poll that
				// finds that step finished must not lead to a third.
				update.onPoll = func(started int) {
					if started != 2 {
						return
					}
					switch lost {
					case "queue message reaped":
						_, err := sqlc.New(conn).UpdateMessageStatus(t.Context(), sqlc.UpdateMessageStatusParams{ID: message.ID, Status: sqlc.QueueStatusEnumFAILED})
						require.NoError(t, err)
					case "write fence reserved":
						_, err := devicewrite.NewService(conn).Reserve(t.Context(), 1, deviceID)
						require.NoError(t, err)
					}
				}
				executor := NewExecutionService(&Config{MaxWorkers: 1}, conn, messages, crypto, nil, minerService, sqlstores.NewSQLDeviceStore(conn), nil, filesService)

				_, _, err = executor.executeQueuedCommand(t.Context(), message)

				requestID := fmt.Sprintf("%s/%d", message.BatchLogUUID, message.ID)
				requests, polls := update.seen()
				if lost == "" {
					require.NoError(t, err)
					require.Equal(t, []string{requestID, requestID, requestID, requestID}, requests, "each step is the same command sent again")
					require.Equal(t, 4, polls, "one send follows each poll that reports a step is ready")
					return
				}
				require.Error(t, err)
				require.True(t, devicewrite.IsNotDispatched(err), "the refused send never reached the device: %v", err)
				require.True(t, fleeterror.IsFailedPreconditionError(err), "a command that lost write admission is not retried: %v", err)
				require.Equal(t, []string{requestID, requestID}, requests, "a command that lost write admission must not start another step")
				require.Equal(t, 2, polls, "the command ends when its send is refused")
			})
		}
	}
}
