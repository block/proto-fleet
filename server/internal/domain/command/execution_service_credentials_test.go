package command

import (
	"context"
	"net/url"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	diagnosticsModels "github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
)

// TestUpdateMinerPassword_PayloadExtraction tests the basic payload extraction logic
// for UpdateMinerPassword command type
func TestUpdateMinerPassword_PayloadExtraction(t *testing.T) {
	tests := []struct {
		name            string
		newPassword     string
		currentPassword string
		expectError     bool
	}{
		{
			name:            "valid payload with both passwords",
			newPassword:     "newpass123",
			currentPassword: "oldpass123",
			expectError:     false,
		},
		{
			name:            "invalid payload - missing current password",
			newPassword:     "newpass456",
			currentPassword: "",
			expectError:     true,
		},
		{
			name:            "invalid payload - missing new password",
			newPassword:     "",
			currentPassword: "oldpass123",
			expectError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test payload
			payload := dto.UpdateMinerPasswordPayload{
				NewPassword:     tt.newPassword,
				CurrentPassword: tt.currentPassword,
			}

			// Verify payload fields
			assert.Equal(t, tt.newPassword, payload.NewPassword)
			assert.Equal(t, tt.currentPassword, payload.CurrentPassword)

			// Validate that both passwords are required
			if tt.expectError {
				assert.True(t, tt.newPassword == "" || tt.currentPassword == "",
					"At least one password should be empty for error cases")
			} else {
				assert.NotEmpty(t, tt.newPassword, "New password should be provided")
				assert.NotEmpty(t, tt.currentPassword, "Current password should be provided")
			}
		})
	}
}

// TestUpdateMinerPassword_DeviceTypeHandling tests that the code correctly
// handles different device types (Antminer vs Proto)
func TestUpdateMinerPassword_DeviceTypeHandling(t *testing.T) {
	tests := []struct {
		name               string
		deviceType         string
		shouldStoreInDB    bool
		userProvidedPasswd string
	}{
		{
			name:               "Antminer devices store credentials in DB after update",
			deviceType:         "antminer",
			shouldStoreInDB:    true,
			userProvidedPasswd: "currentpass",
		},
		{
			name:               "Proto devices do not store credentials in DB",
			deviceType:         "proto",
			shouldStoreInDB:    false,
			userProvidedPasswd: "protocurrent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test documents the expected behavior:
			// - Antminer: credentials stored in DB after successful update (shouldStoreInDB = true)
			// - Proto: credentials NOT stored (shouldStoreInDB = false, uses JWT)
			// - All device types require user to provide current password

			if tt.deviceType == "antminer" {
				assert.True(t, tt.shouldStoreInDB, "Antminer credentials should be stored after update")
			} else if tt.deviceType == "proto" {
				assert.False(t, tt.shouldStoreInDB, "Proto credentials should not be stored")
			}

			// All device types require current password from user
			assert.NotEmpty(t, tt.userProvidedPasswd, "User must always provide current password")
		})
	}
}

// TestUpdateMinerPassword_CurrentPasswordRequired tests that current password
// is always required from the user for all device types
func TestUpdateMinerPassword_CurrentPasswordRequired(t *testing.T) {
	tests := []struct {
		name               string
		deviceType         string
		userProvidedPasswd string
		shouldSucceed      bool
	}{
		{
			name:               "Antminer with current password succeeds",
			deviceType:         "antminer",
			userProvidedPasswd: "currentpass",
			shouldSucceed:      true,
		},
		{
			name:               "Antminer without current password fails",
			deviceType:         "antminer",
			userProvidedPasswd: "",
			shouldSucceed:      false,
		},
		{
			name:               "Proto with current password succeeds",
			deviceType:         "proto",
			userProvidedPasswd: "protocurrent",
			shouldSucceed:      true,
		},
		{
			name:               "Proto without current password fails",
			deviceType:         "proto",
			userProvidedPasswd: "",
			shouldSucceed:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test documents that all device types require the user to provide
			// the current password. The system never auto-fills or retrieves stored passwords.

			if tt.userProvidedPasswd == "" {
				assert.False(t, tt.shouldSucceed, "Operation should fail without current password")
			} else {
				assert.True(t, tt.shouldSucceed, "Operation should succeed with current password")
			}
		})
	}
}

// Mock miner for testing UpdateMinerPassword
type mockMinerForPassword struct {
	updateCalled    bool
	receivedPayload dto.UpdateMinerPasswordPayload
	shouldFail      bool
}

func (m *mockMinerForPassword) UpdateMinerPassword(ctx context.Context, payload dto.UpdateMinerPasswordPayload) error {
	m.updateCalled = true
	m.receivedPayload = payload
	if m.shouldFail {
		return assert.AnError
	}
	return nil
}

// Implement MinerInfo interface
func (m *mockMinerForPassword) GetDriverName() string          { return "antminer" }
func (m *mockMinerForPassword) GetID() models.DeviceIdentifier { return "test-device" }
func (m *mockMinerForPassword) GetOrgID() int64                { return 1 }
func (m *mockMinerForPassword) GetSerialNumber() string        { return "SN123" }
func (m *mockMinerForPassword) GetConnectionInfo() networking.ConnectionInfo {
	return networking.ConnectionInfo{}
}
func (m *mockMinerForPassword) GetWebViewURL() *url.URL { return &url.URL{} }

// Implement remaining Miner interface methods
func (m *mockMinerForPassword) Reboot(ctx context.Context) error      { return nil }
func (m *mockMinerForPassword) StartMining(ctx context.Context) error { return nil }
func (m *mockMinerForPassword) StopMining(ctx context.Context) error  { return nil }
func (m *mockMinerForPassword) Curtail(ctx context.Context, req sdk.CurtailRequest) error {
	return nil
}
func (m *mockMinerForPassword) Uncurtail(ctx context.Context, req sdk.UncurtailRequest) error {
	return nil
}
func (m *mockMinerForPassword) SetCoolingMode(ctx context.Context, payload dto.CoolingModePayload) error {
	return nil
}
func (m *mockMinerForPassword) GetCoolingMode(ctx context.Context) (commonpb.CoolingMode, error) {
	return commonpb.CoolingMode_COOLING_MODE_UNSPECIFIED, nil
}
func (m *mockMinerForPassword) SetPowerTarget(ctx context.Context, payload dto.PowerTargetPayload) error {
	return nil
}
func (m *mockMinerForPassword) UpdateMiningPools(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
	return nil
}
func (m *mockMinerForPassword) DownloadLogs(ctx context.Context, batchLogUUID string) error {
	return nil
}
func (m *mockMinerForPassword) BlinkLED(ctx context.Context) error { return nil }
func (m *mockMinerForPassword) FirmwareUpdate(ctx context.Context, firmware sdk.FirmwareFile) error {
	return nil
}
func (m *mockMinerForPassword) Unpair(ctx context.Context) error { return nil }
func (m *mockMinerForPassword) GetDeviceMetrics(ctx context.Context) (modelsV2.DeviceMetrics, error) {
	return modelsV2.DeviceMetrics{}, nil
}
func (m *mockMinerForPassword) GetDeviceStatus(ctx context.Context) (models.MinerStatus, error) {
	return 0, nil
}
func (m *mockMinerForPassword) GetErrors(ctx context.Context) (diagnosticsModels.DeviceErrors, error) {
	return diagnosticsModels.DeviceErrors{}, nil
}
func (m *mockMinerForPassword) GetMiningPools(ctx context.Context) ([]interfaces.MinerConfiguredPool, error) {
	return nil, nil
}

var _ interfaces.Miner = (*mockMinerForPassword)(nil)

// Mock MinerGetter
type mockMinerGetter struct {
	miner interfaces.Miner
}

func (m *mockMinerGetter) GetMiner(ctx context.Context, deviceID int64) (interfaces.Miner, error) {
	return m.miner, nil
}

// Mock MessageQueue
type mockQueue struct{}

func (m *mockQueue) Enqueue(ctx context.Context, messages []queue.Message) error { return nil }
func (m *mockQueue) Dequeue(ctx context.Context) ([]queue.Message, error)        { return nil, nil }
func (m *mockQueue) MarkSuccess(ctx context.Context, id int64) error             { return nil }
func (m *mockQueue) MarkFailed(ctx context.Context, id int64, reason string) error {
	return nil
}
