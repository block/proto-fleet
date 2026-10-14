package miner_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type curtailmentSDKDevice struct {
	*sdkMocks.MockDevice
	*sdkMocks.MockDeviceCurtailment
	*sdkMocks.MockDeviceCurtailmentConfigurator
}
type curtailmentPluginManager struct{ *fakePluginManager }

func (m *curtailmentPluginManager) GetCapabilitiesForDriverName(string) sdk.Capabilities {
	return sdk.Capabilities{sdk.CapabilityCurtailFull: true}
}

func TestCachedCurtailmentHandlesCrossOwnershipButRejectChangedBindings(t *testing.T) {
	for _, remote := range []bool{false, true} {
		name := "direct"
		if remote {
			name = "Fleet Node"
		}
		t.Run(name, func(t *testing.T) {
			for _, change := range []string{"endpoint", "credentials", "deleted", "route"} {
				t.Run(change, func(t *testing.T) {
					conn, crypto, files := setupTestDB(t)
					identifier := models.DeviceIdentifier("global-curtailment")
					deviceID := createTestDevice(t, conn, string(identifier))
					sender := &fakeCommandSender{}
					ctrl := gomock.NewController(t)
					driver := sdkMocks.NewMockDriver(ctrl)
					manager := &curtailmentPluginManager{&fakePluginManager{driver: driver, driverName: "antminer"}}
					var nodeID int64
					if remote {
						nodeID = createTestFleetNode(t, conn, "curtailment-node", []byte("curtailment-node-identity"))
						_, err := conn.ExecContext(t.Context(), `INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)`, nodeID, deviceID)
						require.NoError(t, err)
					} else {
						createTestMinerCredentials(t, conn, crypto, deviceID)
						device := &curtailmentSDKDevice{sdkMocks.NewMockDevice(ctrl), sdkMocks.NewMockDeviceCurtailment(ctrl), sdkMocks.NewMockDeviceCurtailmentConfigurator(ctrl)}
						driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil).Times(1)
						device.MockDeviceCurtailment.EXPECT().Curtail(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ sdk.CurtailRequest) error { return devicewrite.RecheckContext(ctx) }).Times(3)
						device.MockDeviceCurtailment.EXPECT().Uncurtail(gomock.Any(), gomock.Any()).Return(nil).Times(3)
						device.MockDeviceCurtailmentConfigurator.EXPECT().ApplyCurtailmentConfig(gomock.Any(), gomock.Any()).Return(nil).Times(3)
					}
					service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, manager).WithCommandSender(sender)
					handle, err := service.GetMinerFromDeviceIdentifier(t.Context(), identifier)
					require.NoError(t, err)
					_, err = devicewrite.NewService(conn).Reserve(t.Context(), 1, deviceID)
					require.NoError(t, err)
					configurer, ok := handle.(interfaces.MinerCurtailmentConfigurator)
					require.True(t, ok)
					payload := dto.ApplyCurtailmentConfigPayload{Config: &sdk.CurtailmentConfig{}}
					if remote {
						payload = dto.ApplyCurtailmentConfigPayload{EncryptedConfig: &dto.NodeEncryptedPayload{Algorithm: "x25519-hkdf-sha256-aes-256-gcm-v1", EphemeralPubkey: make([]byte, 32), Nonce: make([]byte, 12), Ciphertext: make([]byte, 17)}}
					}
					for _, state := range []string{"reserved", "active", "quarantined"} {
						_, err = conn.ExecContext(t.Context(), `UPDATE device_write_fence SET state=$1,epoch=epoch+1 WHERE device_id=$2`, state, deviceID)
						require.NoError(t, err)
						require.NoError(t, handle.Curtail(t.Context(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}))
						if remote {
							require.NotNil(t, sentMinerCommand(t, sender).GetCurtail())
						}
						require.NoError(t, handle.Uncurtail(t.Context(), sdk.UncurtailRequest{}))
						if remote {
							require.NotNil(t, sentMinerCommand(t, sender).GetUncurtail())
						}
						require.NoError(t, configurer.ApplyCurtailmentConfig(t.Context(), payload))
						if remote {
							require.NotNil(t, sentMinerCommand(t, sender).GetApplyCurtailmentConfig())
						}
						sender.called = false
						require.Error(t, handle.Reboot(t.Context()))
						require.False(t, sender.called)
					}
					q := sqlc.New(conn)
					switch change {
					case "endpoint":
						require.NoError(t, q.UpdateDeviceIPAssignment(t.Context(), sqlc.UpdateDeviceIPAssignmentParams{ID: deviceID, IpAddress: "192.0.2.44", Port: "4028", UrlScheme: "https"}))
					case "credentials":
						require.NoError(t, q.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: deviceID, UsernameEnc: "changed", PasswordEnc: "changed"}))
					case "deleted":
						_, err = conn.ExecContext(t.Context(), `UPDATE device SET deleted_at=statement_timestamp() WHERE id=$1`, deviceID)
						require.NoError(t, err)
					case "route":
						if remote {
							_, err = conn.ExecContext(t.Context(), `DELETE FROM fleet_node_device WHERE device_id=$1`, deviceID)
						} else {
							nodeID = createTestFleetNode(t, conn, "replacement-node", []byte("replacement-node-identity"))
							_, err = conn.ExecContext(t.Context(), `INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)`, nodeID, deviceID)
						}
						require.NoError(t, err)
					}
					require.Error(t, handle.Curtail(t.Context(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}))
					require.Error(t, handle.Uncurtail(t.Context(), sdk.UncurtailRequest{}))
					require.Error(t, configurer.ApplyCurtailmentConfig(t.Context(), payload))
					require.False(t, sender.called, "stale transport binding must not reach Fleet Node")
				})
			}
		})
	}
}

func TestCurtailmentCommandResolutionReusesBackendAcrossOwnershipEpochs(t *testing.T) {
	conn, crypto, files := setupTestDB(t)
	identifier := models.DeviceIdentifier("curtailment-command-reuse")
	deviceID := createTestDevice(t, conn, string(identifier))
	createTestMinerCredentials(t, conn, crypto, deviceID)
	ctrl := gomock.NewController(t)
	driver := sdkMocks.NewMockDriver(ctrl)
	device := &curtailmentSDKDevice{sdkMocks.NewMockDevice(ctrl), sdkMocks.NewMockDeviceCurtailment(ctrl), sdkMocks.NewMockDeviceCurtailmentConfigurator(ctrl)}
	driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil).Times(1)
	gomock.InOrder(
		device.MockDeviceCurtailment.EXPECT().Curtail(gomock.Any(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}).Return(nil),
		device.MockDeviceCurtailment.EXPECT().Uncurtail(gomock.Any(), sdk.UncurtailRequest{}).Return(nil),
		device.MockDevice.EXPECT().Reboot(gomock.Any()).Return(nil),
	)
	manager := &curtailmentPluginManager{&fakePluginManager{driver: driver, driverName: "antminer"}}
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, manager)
	before, err := service.GetMinerForCommand(t.Context(), deviceID)
	require.NoError(t, err)
	require.NoError(t, before.Curtail(t.Context(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}))
	fences := devicewrite.NewService(conn)
	epoch, err := fences.Reserve(t.Context(), 1, deviceID)
	require.NoError(t, err)
	after, err := service.GetMinerForCommand(t.Context(), deviceID)
	require.NoError(t, err)
	require.NoError(t, after.Uncurtail(t.Context(), sdk.UncurtailRequest{}))
	require.Error(t, before.Reboot(t.Context()), "ownership still fences ordinary operations")
	require.NoError(t, fences.ReleaseHost(t.Context(), 1, deviceID, epoch))
	require.Error(t, before.Reboot(t.Context()), "old facade must stay fenced after custody returns")
	fresh, err := service.GetMinerForCommand(t.Context(), deviceID)
	require.NoError(t, err)
	require.NoError(t, fresh.Reboot(t.Context()))
}

func invokeCurtailmentControl(t *testing.T, handle interfaces.Miner, ctx context.Context, kind commandtype.Type, payload dto.ApplyCurtailmentConfigPayload) error {
	t.Helper()
	switch {
	case kind == commandtype.Curtail:
		return handle.Curtail(ctx, sdk.CurtailRequest{Level: sdk.CurtailLevelFull})
	case kind == commandtype.Uncurtail:
		return handle.Uncurtail(ctx, sdk.UncurtailRequest{})
	case kind == commandtype.ApplyCurtailmentConfig:
		configurer, ok := handle.(interfaces.MinerCurtailmentConfigurator)
		require.True(t, ok)
		return configurer.ApplyCurtailmentConfig(ctx, payload)
	default:
		t.Fatalf("unexpected control kind %s", kind.String())
		return nil
	}
}

func TestQueuedCurtailmentAuthorityPassesRealAdapterGatesAcrossHandoff(t *testing.T) {
	for _, remote := range []bool{false, true} {
		name := "direct"
		if remote {
			name = "Fleet Node"
		}
		t.Run(name, func(t *testing.T) {
			for _, kind := range []commandtype.Type{commandtype.Curtail, commandtype.Uncurtail, commandtype.ApplyCurtailmentConfig} {
				t.Run(kind.String(), func(t *testing.T) {
					for _, state := range []string{"reserved", "active", "quarantined"} {
						t.Run(state, func(t *testing.T) {
							conn, crypto, files := setupTestDB(t)
							identifier := models.DeviceIdentifier("queued-curtailment-gate")
							deviceID := createTestDevice(t, conn, string(identifier))
							ctrl := gomock.NewController(t)
							driver := sdkMocks.NewMockDriver(ctrl)
							manager := &curtailmentPluginManager{&fakePluginManager{driver: driver, driverName: "antminer"}}
							sender := &fakeCommandSender{}
							payload := dto.ApplyCurtailmentConfigPayload{Config: &sdk.CurtailmentConfig{}}
							if remote {
								nodeID := createTestFleetNode(t, conn, "queued-curtailment-node", []byte("queued-curtailment-identity"))
								_, err := conn.ExecContext(t.Context(), `INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)`, nodeID, deviceID)
								require.NoError(t, err)
								payload = dto.ApplyCurtailmentConfigPayload{EncryptedConfig: &dto.NodeEncryptedPayload{Algorithm: "x25519-hkdf-sha256-aes-256-gcm-v1", EphemeralPubkey: make([]byte, 32), Nonce: make([]byte, 12), Ciphertext: make([]byte, 17)}}
							} else {
								createTestMinerCredentials(t, conn, crypto, deviceID)
								device := &curtailmentSDKDevice{sdkMocks.NewMockDevice(ctrl), sdkMocks.NewMockDeviceCurtailment(ctrl), sdkMocks.NewMockDeviceCurtailmentConfigurator(ctrl)}
								driver.EXPECT().NewDevice(gomock.Any(), string(identifier), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil).Times(1)
								switch {
								case kind == commandtype.Curtail:
									device.MockDeviceCurtailment.EXPECT().Curtail(gomock.Any(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}).Return(nil)
								case kind == commandtype.Uncurtail:
									device.MockDeviceCurtailment.EXPECT().Uncurtail(gomock.Any(), sdk.UncurtailRequest{}).Return(nil)
								case kind == commandtype.ApplyCurtailmentConfig:
									device.MockDeviceCurtailmentConfigurator.EXPECT().ApplyCurtailmentConfig(gomock.Any(), sdk.CurtailmentConfig{}).Return(nil)
								}
							}
							service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, files, manager).WithCommandSender(sender)
							handle, err := service.GetMinerForCommand(t.Context(), deviceID)
							require.NoError(t, err)
							q := sqlc.New(conn)
							actor, err := q.CreateUser(t.Context(), sqlc.CreateUserParams{UserID: id.GenerateID(), Username: "queued-curtailment-actor", PasswordHash: "unused", CreatedAt: time.Now()})
							require.NoError(t, err)
							batch := id.GenerateID()
							_, err = q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{Uuid: batch, Type: kind.String(), CreatedBy: actor, CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPENDING, DevicesCount: 1, OrganizationID: sql.NullInt64{Int64: 1, Valid: true}})
							require.NoError(t, err)
							messages := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 3}, conn)
							require.NoError(t, messages.Enqueue(t.Context(), batch, kind, []int64{deviceID}, nil))
							rows, err := messages.Dequeue(t.Context(), 1)
							require.NoError(t, err)
							require.Len(t, rows, 1)
							fences := devicewrite.NewService(conn)
							attempt, err := fences.BeginQueuedHost(t.Context(), rows[0])
							require.NoError(t, err)
							epoch, err := fences.Reserve(t.Context(), 1, deviceID)
							require.NoError(t, err)
							if state != "reserved" {
								_, err = conn.ExecContext(t.Context(), `UPDATE device_write_fence SET state=$1,epoch=epoch+1 WHERE device_id=$2`, state, deviceID)
								require.NoError(t, err)
							}
							carried := attempt.Context(t.Context())
							other := commandtype.Curtail
							if kind == other {
								other = commandtype.Uncurtail
							}
							require.Error(t, invokeCurtailmentControl(t, handle, carried, other, payload), "another control cannot borrow queued authority")
							require.False(t, sender.called)
							require.NoError(t, invokeCurtailmentControl(t, handle, carried, kind, payload))
							if remote {
								command := sentMinerCommand(t, sender)
								switch {
								case kind == commandtype.Curtail:
									require.NotNil(t, command.GetCurtail())
								case kind == commandtype.Uncurtail:
									require.NotNil(t, command.GetUncurtail())
								case kind == commandtype.ApplyCurtailmentConfig:
									require.NotNil(t, command.GetApplyCurtailmentConfig())
								}
							}
							require.NoError(t, attempt.Finish(t.Context(), nil))
							var attemptState string
							require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT status FROM device_write_attempt WHERE queue_message_id=$1`, rows[0].ID).Scan(&attemptState))
							require.Equal(t, "complete", attemptState)
							require.Greater(t, epoch, rows[0].WriteEpoch)
						})
					}
				})
			}
		})
	}
}
