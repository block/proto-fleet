package sqlstores

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func rejectContainerPlacement(ctx context.Context, q sqlc.Querier, orgID int64, identifiers []string) error {
	hasContainers, err := q.ContainsContainerIdentifiers(ctx, sqlc.ContainsContainerIdentifiersParams{OrgID: orgID, Identifiers: identifiers})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to validate container placement: %w", err)
	}
	if hasContainers {
		return fleeterror.NewInvalidArgumentError("containers are facility peers; use AssignContainersToSite to change their site")
	}
	return nil
}
