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

// The queued payload carries the canonical file id whatever form the caller
// used, so release channels can compare it with the ids the files service
// reports.
func TestFirmwareUpdate_QueuesCanonicalFileID(t *testing.T) {
	t.Chdir(t.TempDir())
	filesService, err := files.NewService(files.Config{})
	require.NoError(t, err)
	fileID, err := filesService.SaveFirmwareFile("update.swu", strings.NewReader("firmware"), files.FirmwareMetadata{
		TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "2.0.0",
	})
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	deviceStore := storeMocks.NewMockDeviceStore(ctrl)
	deviceStore.EXPECT().GetDevicePropertiesForRename(gomock.Any(), int64(7), []string{"device-1"}, false).
		Return([]stores.DeviceRenameProperties{{DeviceIdentifier: "device-1", Manufacturer: "Proto", Model: "Rig"}}, nil)
	messageQueue := queueMocks.NewMockMessageQueue(ctrl)
	messageQueue.EXPECT().Enqueue(
		gomock.Any(), "batch-1", commandtype.FirmwareUpdate, []int64{101}, dto.FirmwareUpdatePayload{FirmwareFileID: fileID},
	).Return(nil)
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
		startStatusUpdateRoutineOverride: func(string, onFinishedCallbackFunc) {},
	}

	result, err := svc.FirmwareUpdate(manualSessionCtx(7), includeSelector("device-1"), "urn:uuid:"+strings.ToUpper(fileID))
	require.NoError(t, err)
	assert.Equal(t, 1, result.DispatchedCount)

	_, err = svc.FirmwareUpdate(manualSessionCtx(7), includeSelector("device-1"), "not-a-uuid")
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestFirmwareUpdateArtifact_UsesAssignmentSnapshot(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		wantCode connect.Code
	}{
		{name: "edited target and version"},
		{name: "reuploaded checksum with other metadata"},
		{name: "missing sidecar"},
		{name: "corrupt sidecar"},
		{name: "missing payload", wantCode: connect.CodeNotFound},
		{name: "mismatched miner", wantCode: connect.CodeFailedPrecondition},
		{name: "incomplete snapshot", wantCode: connect.CodeFailedPrecondition},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			filesService, err := files.NewService(files.Config{})
			require.NoError(t, err)
			metadata := files.FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "2.0.0"}
			fileID, err := filesService.SaveFirmwareFile("assigned.swu", strings.NewReader("assigned firmware"), metadata)
			require.NoError(t, err)
			assignment, err := filesService.ResolveFirmwareArtifact(fileID)
			require.NoError(t, err)
			other := files.FirmwareMetadata{TargetManufacturer: "Bitmain", TargetModel: "S19", FirmwareVersion: "9.0.0"}
			sidecar := filepath.Join("firmware", fileID, "metadata.json")
			switch scenario.name {
			case "edited target and version":
				_, err = filesService.UpdateFirmwareMetadata(fileID, other)
				require.NoError(t, err)
			case "reuploaded checksum with other metadata":
				require.NoError(t, filesService.DeleteFirmwareFile(fileID))
				fileID, err = filesService.SaveFirmwareFile("new-name.swu", strings.NewReader("assigned firmware"), other)
				require.NoError(t, err)
				assert.NotEqual(t, assignment.FileID, fileID)
			case "missing sidecar":
				require.NoError(t, os.Remove(sidecar))
			case "corrupt sidecar":
				require.NoError(t, os.WriteFile(sidecar, []byte(`not json`), 0600))
			case "missing payload":
				require.NoError(t, filesService.DeleteFirmwareFile(fileID))
			case "incomplete snapshot":
				assignment.Metadata.FirmwareVersion = ""
			}

			ctrl := gomock.NewController(t)
			deviceStore := storeMocks.NewMockDeviceStore(ctrl)
			if scenario.name != "missing payload" && scenario.name != "incomplete snapshot" {
				manufacturer := " proto "
				if scenario.name == "mismatched miner" {
					manufacturer = "Bitmain"
				}
				deviceStore.EXPECT().GetDevicePropertiesForRename(gomock.Any(), int64(7), []string{"device-1"}, false).
					Return([]stores.DeviceRenameProperties{{DeviceIdentifier: "device-1", Manufacturer: manufacturer, Model: "RIG"}}, nil)
			}
			messageQueue := queueMocks.NewMockMessageQueue(ctrl)
			if scenario.wantCode == 0 {
				messageQueue.EXPECT().Enqueue(gomock.Any(), "batch-1", commandtype.FirmwareUpdate, []int64{101},
					dto.FirmwareUpdatePayload{FirmwareFileID: fileID, FirmwareChecksum: assignment.Checksum}).Return(nil)
			}
			svc := &Service{
				config:           &Config{},
				executionService: &ExecutionService{run: newExecutionRun(context.Background())},
				messageQueue:     messageQueue, filesService: filesService, deviceStore: deviceStore,
				resolveDevicesOverride: func(_ context.Context, identifiers []string) ([]resolvedDevice, error) {
					return []resolvedDevice{{id: 101, identifier: identifiers[0]}}, nil
				},
				saveCommandBatchLogOverride: func(context.Context, int64, int64, *Command, []byte, int) (string, error) {
					return "batch-1", nil
				},
				startStatusUpdateRoutineOverride: func(string, onFinishedCallbackFunc) {},
			}
			result, err := svc.FirmwareUpdateArtifact(manualSessionCtx(7), includeSelector("device-1"), assignment.Checksum, assignment.Metadata)
			if scenario.wantCode == 0 {
				require.NoError(t, err)
				assert.Equal(t, 1, result.DispatchedCount)
			} else {
				var fleetErr fleeterror.FleetError
				require.ErrorAs(t, err, &fleetErr)
				assert.Equal(t, scenario.wantCode, fleetErr.GRPCCode)
				assert.Nil(t, result)
			}
			if scenario.name != "missing payload" {
				// Both success and preflight failure must release the artifact lease.
				done := make(chan error, 1)
				go func() { done <- filesService.DeleteFirmwareFile(fileID) }()
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("artifact lease was not released")
				}
			}
		})
	}
}

func TestFirmwareUpdateArtifact_HoldsPayloadLeaseThroughEnqueue(t *testing.T) {
	svc, deviceStore, filesService := setupFirmwareTargetValidationService(t)
	metadata := files.FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "2.0.0"}
	fileID, err := filesService.SaveFirmwareFile("assigned.swu", strings.NewReader("firmware"), metadata)
	require.NoError(t, err)
	assignment, err := filesService.ResolveFirmwareArtifact(fileID)
	require.NoError(t, err)
	deviceStore.EXPECT().GetDevicePropertiesForRename(gomock.Any(), int64(7), []string{"device-1"}, false).
		Return([]stores.DeviceRenameProperties{{DeviceIdentifier: "device-1", Manufacturer: "Proto", Model: "Rig"}}, nil)
	messageQueue := queueMocks.NewMockMessageQueue(gomock.NewController(t))
	deleteStarted := make(chan struct{})
	deleteDone := make(chan error, 1)
	messageQueue.EXPECT().Enqueue(gomock.Any(), "batch-1", commandtype.FirmwareUpdate, []int64{101}, gomock.Any()).
		DoAndReturn(func(context.Context, string, commandtype.Type, []int64, any) error {
			go func() {
				close(deleteStarted)
				deleteDone <- filesService.DeleteFirmwareFile(fileID)
			}()
			<-deleteStarted
			select {
			case err := <-deleteDone:
				t.Errorf("payload deleted before enqueue completed: %v", err)
				deleteDone <- err
			case <-time.After(100 * time.Millisecond):
			}
			return nil
		})
	svc.config = &Config{}
	svc.executionService = &ExecutionService{run: newExecutionRun(context.Background())}
	svc.messageQueue = messageQueue
	svc.resolveDevicesOverride = func(context.Context, []string) ([]resolvedDevice, error) {
		return []resolvedDevice{{id: 101, identifier: "device-1"}}, nil
	}
	svc.saveCommandBatchLogOverride = func(context.Context, int64, int64, *Command, []byte, int) (string, error) {
		return "batch-1", nil
	}
	svc.startStatusUpdateRoutineOverride = func(string, onFinishedCallbackFunc) {}
	_, err = svc.FirmwareUpdateArtifact(manualSessionCtx(7), includeSelector("device-1"), assignment.Checksum, assignment.Metadata)
	require.NoError(t, err)
	select {
	case err := <-deleteDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("payload deletion blocked after enqueue completed")
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
