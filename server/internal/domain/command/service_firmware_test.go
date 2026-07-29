package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	queueMocks "github.com/block/proto-fleet/server/internal/infrastructure/queue/mocks"
)

func setupFirmwareTargetValidationService(t *testing.T) (*Service, *storeMocks.MockDeviceStore, *files.Service) {
	t.Helper()
	t.Chdir(t.TempDir())
	filesService, err := files.NewService(files.Config{})
	require.NoError(t, err)
	deviceStore := storeMocks.NewMockDeviceStore(gomock.NewController(t))
	return &Service{filesService: filesService, deviceStore: deviceStore}, deviceStore, filesService
}

func TestValidateFirmwareUpdateTargets_AcceptsMatchingTargetsCaseInsensitively(t *testing.T) {
	svc, deviceStore, filesService := setupFirmwareTargetValidationService(t)
	fileID, err := filesService.SaveFirmwareFile("update.swu", strings.NewReader("firmware"), files.FirmwareMetadata{
		TargetManufacturer: "Proto",
		TargetModel:        "Rig",
		FirmwareVersion:    "2.0.0",
	})
	require.NoError(t, err)
	devices := []resolvedDevice{{id: 1, identifier: "device-1"}, {id: 2, identifier: "device-2"}}
	deviceStore.EXPECT().GetDevicePropertiesForRename(gomock.Any(), int64(7), []string{"device-1", "device-2"}, false).Return(
		[]stores.DeviceRenameProperties{
			{DeviceIdentifier: "device-1", Manufacturer: " proto ", Model: "RIG"},
			{DeviceIdentifier: "device-2", Manufacturer: "Proto", Model: "Rig"},
		}, nil,
	)

	metadata, err := filesService.GetFirmwareMetadata(fileID)
	require.NoError(t, err)
	assert.NoError(t, svc.validateFirmwareUpdateTargets(t.Context(), 7, devices, metadata))
}

func TestValidateFirmwareUpdateTargets_RejectsMismatchedOrUnknownTargets(t *testing.T) {
	svc, deviceStore, filesService := setupFirmwareTargetValidationService(t)
	fileID, err := filesService.SaveFirmwareFile("update.swu", strings.NewReader("firmware"), files.FirmwareMetadata{
		TargetManufacturer: "Proto",
		TargetModel:        "Rig",
		FirmwareVersion:    "2.0.0",
	})
	require.NoError(t, err)
	devices := []resolvedDevice{{id: 1, identifier: "device-1"}, {id: 2, identifier: "device-2"}}
	deviceStore.EXPECT().GetDevicePropertiesForRename(gomock.Any(), int64(7), []string{"device-1", "device-2"}, false).Return(
		[]stores.DeviceRenameProperties{
			{DeviceIdentifier: "device-1", Manufacturer: "Bitmain", Model: "Rig"},
			{DeviceIdentifier: "device-2", Manufacturer: "Proto", Model: ""},
		}, nil,
	)

	metadata, err := filesService.GetFirmwareMetadata(fileID)
	require.NoError(t, err)
	err = svc.validateFirmwareUpdateTargets(t.Context(), 7, devices, metadata)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "2 of 2")
}

func TestValidateFirmwareUpdateTargets_RejectsLegacyFirmware(t *testing.T) {
	svc, _, _ := setupFirmwareTargetValidationService(t)
	const fileID = "11111111-1111-1111-1111-111111111111"
	legacyDir := filepath.Join("firmware", fileID)
	require.NoError(t, os.MkdirAll(legacyDir, 0750))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "legacy.swu"), []byte("legacy"), 0600))

	metadata, err := svc.filesService.GetFirmwareMetadata(fileID)
	require.NoError(t, err)
	err = svc.validateFirmwareUpdateTargets(t.Context(), 7, []resolvedDevice{{id: 1, identifier: "device-1"}}, metadata)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "metadata is unknown")
	assert.Contains(t, err.Error(), "repair its metadata")
}

func TestLeaseFirmwareMetadata_PreservesMetadataLookupErrors(t *testing.T) {
	tests := []struct {
		name       string
		fileID     string
		setup      func(t *testing.T)
		assertCode func(t *testing.T, err error)
	}{
		{
			name:   "invalid file ID",
			fileID: "not-a-uuid",
			assertCode: func(t *testing.T, err error) {
				t.Helper()
				assert.True(t, fleeterror.IsInvalidArgumentError(err))
			},
		},
		{
			name:   "missing firmware",
			fileID: "11111111-1111-1111-1111-111111111111",
			assertCode: func(t *testing.T, err error) {
				t.Helper()
				assert.True(t, fleeterror.IsNotFoundError(err))
			},
		},
		{
			name:   "corrupt metadata",
			fileID: "22222222-2222-2222-2222-222222222222",
			setup: func(t *testing.T) {
				t.Helper()
				dir := filepath.Join("firmware", "22222222-2222-2222-2222-222222222222")
				require.NoError(t, os.MkdirAll(dir, 0750))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "metadata.json"), []byte("{"), 0600))
			},
			assertCode: func(t *testing.T, err error) {
				t.Helper()
				var fleetErr fleeterror.FleetError
				require.ErrorAs(t, err, &fleetErr)
				assert.Equal(t, connect.CodeInternal, fleetErr.GRPCCode)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, filesService := setupFirmwareTargetValidationService(t)
			if tt.setup != nil {
				tt.setup(t)
			}

			_, release, err := filesService.LeaseFirmwareMetadata(tt.fileID)

			require.Error(t, err)
			assert.Nil(t, release)
			tt.assertCode(t, err)
		})
	}
}

func TestProcessCommand_FirmwareUpdateValidatesBeforeDispatch(t *testing.T) {
	tests := []struct {
		name       string
		legacy     bool
		properties []stores.DeviceRenameProperties
		wantError  bool
	}{
		{
			name: "matching targets dispatch",
			properties: []stores.DeviceRenameProperties{
				{DeviceIdentifier: "device-1", Manufacturer: " proto ", Model: "RIG"},
				{DeviceIdentifier: "device-2", Manufacturer: "Proto", Model: "Rig"},
			},
		},
		{
			name: "mixed targets fail",
			properties: []stores.DeviceRenameProperties{
				{DeviceIdentifier: "device-1", Manufacturer: "Proto", Model: "Rig"},
				{DeviceIdentifier: "device-2", Manufacturer: "Bitmain", Model: "S19"},
			},
			wantError: true,
		},
		{
			name: "missing device target fails",
			properties: []stores.DeviceRenameProperties{
				{DeviceIdentifier: "device-1", Manufacturer: "Proto", Model: "Rig"},
				{DeviceIdentifier: "device-2", Manufacturer: "", Model: ""},
			},
			wantError: true,
		},
		{
			name: "missing device row fails",
			properties: []stores.DeviceRenameProperties{
				{DeviceIdentifier: "device-1", Manufacturer: "Proto", Model: "Rig"},
			},
			wantError: true,
		},
		{
			name:      "legacy firmware fails",
			legacy:    true,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			filesService, err := files.NewService(files.Config{})
			require.NoError(t, err)
			var fileID string
			if tt.legacy {
				fileID = "11111111-1111-1111-1111-111111111111"
				legacyDir := filepath.Join("firmware", fileID)
				require.NoError(t, os.MkdirAll(legacyDir, 0750))
				require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "legacy.swu"), []byte("legacy"), 0600))
			} else {
				fileID, err = filesService.SaveFirmwareFile("update.swu", strings.NewReader("firmware"), files.FirmwareMetadata{
					TargetManufacturer: "Proto",
					TargetModel:        "Rig",
					FirmwareVersion:    "2.0.0",
				})
				require.NoError(t, err)
			}

			ctrl := gomock.NewController(t)
			deviceStore := storeMocks.NewMockDeviceStore(ctrl)
			messageQueue := queueMocks.NewMockMessageQueue(ctrl)
			if !tt.legacy {
				deviceStore.EXPECT().GetDevicePropertiesForRename(
					gomock.Any(), int64(7), []string{"device-1", "device-2"}, false,
				).Return(tt.properties, nil)
			}

			batchCreated := false
			svc := &Service{
				config:           &Config{},
				executionService: &ExecutionService{run: newExecutionRun(context.Background())},
				messageQueue:     messageQueue,
				filesService:     filesService,
				deviceStore:      deviceStore,
				resolveDevicesOverride: func(_ context.Context, identifiers []string) ([]resolvedDevice, error) {
					return []resolvedDevice{
						{id: 101, identifier: identifiers[0]},
						{id: 102, identifier: identifiers[1]},
					}, nil
				},
				saveCommandBatchLogOverride: func(context.Context, int64, int64, *Command, []byte, int) (string, error) {
					batchCreated = true
					return "batch-1", nil
				},
			}
			payload := dto.FirmwareUpdatePayload{FirmwareFileID: fileID}
			if !tt.wantError {
				messageQueue.EXPECT().Enqueue(
					gomock.Any(), "batch-1", commandtype.FirmwareUpdate, []int64{101, 102}, payload,
				).Return(nil)
			}

			result, err := svc.processCommand(manualSessionCtx(7), &Command{
				commandType:    commandtype.FirmwareUpdate,
				deviceSelector: includeSelector("device-1", "device-2"),
				payload:        payload,
			})
			if tt.wantError {
				require.Error(t, err)
				assert.True(t, fleeterror.IsFailedPreconditionError(err))
				assert.False(t, batchCreated)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			assert.True(t, batchCreated)
			require.NotNil(t, result)
			assert.Equal(t, "batch-1", result.BatchIdentifier)
			assert.Equal(t, 2, result.DispatchedCount)
		})
	}
}

func TestProcessCommand_FirmwareUpdateHoldsMetadataLeaseThroughDispatch(t *testing.T) {
	t.Chdir(t.TempDir())
	filesService, err := files.NewService(files.Config{})
	require.NoError(t, err)
	fileID, err := filesService.SaveFirmwareFile("update.swu", strings.NewReader("firmware"), files.FirmwareMetadata{
		TargetManufacturer: "Proto",
		TargetModel:        "Rig",
		FirmwareVersion:    "2.0.0",
	})
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	deviceStore := storeMocks.NewMockDeviceStore(ctrl)
	messageQueue := queueMocks.NewMockMessageQueue(ctrl)
	validationStarted := make(chan struct{})
	allowValidation := make(chan struct{})
	deviceStore.EXPECT().GetDevicePropertiesForRename(
		gomock.Any(), int64(7), []string{"device-1"}, false,
	).DoAndReturn(func(context.Context, int64, []string, bool) ([]stores.DeviceRenameProperties, error) {
		close(validationStarted)
		<-allowValidation
		return []stores.DeviceRenameProperties{{
			DeviceIdentifier: "device-1",
			Manufacturer:     "Proto",
			Model:            "Rig",
		}}, nil
	})

	enqueueStarted := make(chan struct{})
	allowEnqueue := make(chan struct{})
	payload := dto.FirmwareUpdatePayload{FirmwareFileID: fileID}
	messageQueue.EXPECT().Enqueue(
		gomock.Any(), "batch-1", commandtype.FirmwareUpdate, []int64{101}, payload,
	).DoAndReturn(func(context.Context, string, commandtype.Type, []int64, any) error {
		close(enqueueStarted)
		<-allowEnqueue
		return nil
	})

	svc := &Service{
		config:           &Config{},
		executionService: &ExecutionService{run: newExecutionRun(context.Background())},
		messageQueue:     messageQueue,
		filesService:     filesService,
		deviceStore:      deviceStore,
		resolveDevicesOverride: func(_ context.Context, identifiers []string) ([]resolvedDevice, error) {
			return []resolvedDevice{{id: 101, identifier: identifiers[0]}}, nil
		},
		saveCommandBatchLogOverride: func(context.Context, int64, int64, *Command, []byte, int) (string, error) {
			return "batch-1", nil
		},
	}

	type commandOutcome struct {
		result *CommandResult
		err    error
	}
	commandDone := make(chan commandOutcome, 1)
	go func() {
		result, err := svc.processCommand(manualSessionCtx(7), &Command{
			commandType:    commandtype.FirmwareUpdate,
			deviceSelector: includeSelector("device-1"),
			payload:        payload,
		})
		commandDone <- commandOutcome{result: result, err: err}
	}()
	<-validationStarted

	updateStarted := make(chan struct{})
	updateDone := make(chan error, 1)
	go func() {
		close(updateStarted)
		_, err := filesService.UpdateFirmwareMetadata(fileID, files.FirmwareMetadata{
			TargetManufacturer: "Bitmain",
			TargetModel:        "S19",
			FirmwareVersion:    "3.0.0",
		})
		updateDone <- err
	}()
	<-updateStarted

	select {
	case err := <-updateDone:
		close(allowValidation)
		t.Fatalf("metadata update completed during target validation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(allowValidation)
	<-enqueueStarted
	select {
	case err := <-updateDone:
		close(allowEnqueue)
		t.Fatalf("metadata update completed before firmware dispatch: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(allowEnqueue)
	outcome := <-commandDone
	require.NoError(t, outcome.err)
	require.NotNil(t, outcome.result)
	assert.Equal(t, "batch-1", outcome.result.BatchIdentifier)
	require.NoError(t, <-updateDone)
}
