package miner_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
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
