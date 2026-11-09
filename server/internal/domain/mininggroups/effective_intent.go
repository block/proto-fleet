package mininggroups

import (
	"context"
	"database/sql"

	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// Retain each established effect before changing operation status or planning
// another write. History can fail as a whole without forgetting applied boards.
func retainObservedIntents(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, snapshot Snapshot) error {
	if snapshot.Pending {
		return nil
	}
	for _, group := range snapshot.Groups {
		if !group.Managed || !group.Configured || poolconfigurations.IsClearedPoolIntent(group.ConfigRevision) {
			continue
		}
		if err := q.RetainAppliedPoolGroupIntents(ctx, sqlc.RetainAppliedPoolGroupIntentsParams{OrganizationID: row.OrganizationID, PhysicalAttemptID: row.AttemptID, GroupSnapshotID: sql.NullString{String: group.ConfigRevision, Valid: true}, MacAddresses: group.Members}); err != nil {
			return err
		}
	}
	return nil
}

func hasDetachedIntents(ctx context.Context, q sqlc.Querier, orgID, deviceID int64) (bool, error) {
	detached, err := q.HasDetachedPoolGroupIntents(ctx, sqlc.HasDetachedPoolGroupIntentsParams{OrganizationID: orgID, DeviceID: deviceID})
	if err != nil || detached {
		return detached, err
	}
	cutover, err := q.HasPoolConfigurationCutover(ctx, orgID)
	if err != nil || cutover {
		return false, err
	}
	return q.HasDetachedHashboardPoolIntents(ctx, sqlc.HasDetachedHashboardPoolIntentsParams{OrganizationID: orgID, DeviceID: deviceID})
}

func pruneDetachedIntents(ctx context.Context, q sqlc.Querier, orgID, deviceID int64) error {
	if err := q.PruneDetachedPoolGroupIntents(ctx, sqlc.PruneDetachedPoolGroupIntentsParams{OrganizationID: orgID, DeviceID: deviceID}); err != nil {
		return err
	}
	cutover, err := q.HasPoolConfigurationCutover(ctx, orgID)
	if err != nil || cutover {
		return err
	}
	return q.PruneDetachedHashboardPoolIntents(ctx, sqlc.PruneDetachedHashboardPoolIntentsParams{OrganizationID: orgID, DeviceID: deviceID})
}
