package rollout

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sqlc-dev/pqtype"

	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
)

// successfulFixtureDispatcher models commands that execute successfully before
// the next enforcement tick. It persists their exact batch identity and results
// so tests must still supply matching telemetry before provenance can advance.
// The underlying fakeDispatcher only records transport calls; controlled command
// tests can replace this wrapper without inheriting its successful outcomes.
type successfulFixtureDispatcher struct {
	f *fixture
}

func (d *successfulFixtureDispatcher) FirmwareUpdateArtifact(ctx context.Context, selector *commandpb.DeviceSelector, checksum string, metadata files.FirmwareMetadata) (*command.CommandResult, error) {
	result, err := d.f.dispatcher.FirmwareUpdateArtifact(ctx, selector, checksum, metadata)
	if err != nil {
		return nil, err
	}
	if err := d.f.recordDispatchedFirmwareBatch(ctx, result, checksum, sqlc.BatchStatusEnumFINISHED); err != nil {
		return nil, err
	}
	for _, identifier := range result.DispatchedDeviceIdentifiers {
		if err := d.f.svc.store.GetQueries(ctx).UpsertCommandOnDeviceLog(ctx, sqlc.UpsertCommandOnDeviceLogParams{
			Uuid: result.BatchIdentifier, DeviceID: d.f.deviceIDs[identifier],
			Status: sqlc.DeviceCommandStatusEnumSUCCESS, UpdatedAt: time.Now(),
		}); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (f *fixture) recordDispatchedFirmwareBatch(ctx context.Context, result *command.CommandResult, checksum string, status sqlc.BatchStatusEnum) error {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"firmware_checksum": checksum})
	if err != nil {
		return fmt.Errorf("marshal dispatched firmware payload: %w", err)
	}
	result.BatchIdentifier = id.GenerateID()
	_, err = f.svc.store.GetQueries(ctx).CreateCommandBatchLog(ctx, sqlc.CreateCommandBatchLogParams{
		Uuid: result.BatchIdentifier, Type: "FirmwareUpdate", CreatedBy: info.UserID,
		CreatedAt: time.Now(), Status: status,
		DevicesCount:   int32(result.DispatchedCount), // #nosec G115 -- bounded test fleet
		Payload:        pqtype.NullRawMessage{RawMessage: payload, Valid: true},
		OrganizationID: sql.NullInt64{Int64: info.OrganizationID, Valid: true},
	})
	return err
}
