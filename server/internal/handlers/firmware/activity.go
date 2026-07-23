package firmware

import (
	"context"
	"fmt"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

const firmwareUploadedEventType = "firmware_uploaded"

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
