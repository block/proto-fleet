package sqlstores

import (
	"context"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

// UpsertDeviceFirmwareState records the latest observed firmware version used
// by miner channel enforcement without coupling it to the general device list query.
func (s *SQLDeviceStore) UpsertDeviceFirmwareState(ctx context.Context, orgID int64, deviceIdentifier models.DeviceIdentifier, firmwareVersion string, observedAt time.Time) error {
	if firmwareVersion == "" || orgID <= 0 {
		return nil
	}
	if err := s.getQueries(ctx).UpsertDeviceFirmwareState(ctx, sqlc.UpsertDeviceFirmwareStateParams{
		OrgID:            orgID,
		DeviceIdentifier: string(deviceIdentifier),
		FirmwareVersion:  firmwareVersion,
		ObservedAt:       observedAt,
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to upsert firmware state for device %s: %v", deviceIdentifier, err)
	}
	return nil
}
