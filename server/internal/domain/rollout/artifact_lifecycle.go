package rollout

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

// PrepareFirmwareDeletion reserves a connection before the files lifecycle lock
// is taken. Otherwise deletion could hold that lock while waiting for a pool
// whose transactions all need to pin or resolve firmware before committing.
func PrepareFirmwareDeletion(ctx context.Context, pool *sql.DB) (files.FirmwareDeletionCheck, func(), error) {
	conn, err := pool.Conn(ctx)
	if err != nil {
		return nil, nil, fleeterror.NewInternalErrorf("prepare firmware usage check: %w", err)
	}
	q := sqlc.New(conn)
	return func(fileID, checksum string) error {
		return checkFirmwareDeletion(ctx, q, fileID, checksum)
	}, func() { _ = conn.Close() }, nil
}

// Assignment pins protect through commit; command leases protect through
// enqueue. Once those end, the durable references below prevent deletion.
func checkFirmwareDeletion(ctx context.Context, q sqlc.Querier, fileID, checksum string) error {
	inUse, err := q.CheckFirmwareArtifactInUse(ctx, sqlc.CheckFirmwareArtifactInUseParams{
		FileID: fileID, FirmwareChecksum: checksum,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("check firmware usage before deletion: %w", err)
	}
	if inUse {
		return fleeterror.NewFailedPreconditionError("This firmware is assigned to a release channel or used by an unfinished update. Change or clear the assignment and wait for pending updates to finish before deleting it.")
	}
	return nil
}
