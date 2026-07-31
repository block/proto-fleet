package firmware

import (
	"context"
	"fmt"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

const (
	firmwareUploadedEventType        = "firmware_uploaded"
	firmwareMetadataUpdatedEventType = "firmware_metadata_updated"
)

func logFirmwareUploadActivity(
	ctx context.Context,
	activitySvc *activity.Service,
	filename string,
	result files.FirmwareUploadSaveResult,
) {
	if result.Reused {
		return
	}
	event := activitymodels.Event{
		Category:    activitymodels.CategorySystem,
		Type:        firmwareUploadedEventType,
		Description: fmt.Sprintf("Uploaded firmware file: %s", filename),
		Metadata: map[string]any{
			"firmware_file_id":    result.FirmwareFileID,
			"filename":            filename,
			"target_manufacturer": result.Metadata.TargetManufacturer,
			"target_model":        result.Metadata.TargetModel,
			"firmware_version":    result.Metadata.FirmwareVersion,
		},
	}
	activity.StampActor(ctx, &event)
	activitySvc.Log(ctx, event)
}

func logFirmwareMetadataUpdatedActivity(
	ctx context.Context,
	activitySvc *activity.Service,
	fileID string,
	previous *files.FirmwareMetadata,
	current files.FirmwareMetadata,
) {
	metadata := map[string]any{
		"firmware_file_id":            fileID,
		"current_target_manufacturer": current.TargetManufacturer,
		"current_target_model":        current.TargetModel,
		"current_firmware_version":    current.FirmwareVersion,
	}
	if previous != nil {
		metadata["previous_target_manufacturer"] = previous.TargetManufacturer
		metadata["previous_target_model"] = previous.TargetModel
		metadata["previous_firmware_version"] = previous.FirmwareVersion
	}

	event := activitymodels.Event{
		Category:    activitymodels.CategorySystem,
		Type:        firmwareMetadataUpdatedEventType,
		Description: fmt.Sprintf("Updated firmware metadata: %s", fileID),
		Metadata:    metadata,
	}
	activity.StampActor(ctx, &event)
	activitySvc.Log(ctx, event)
}
