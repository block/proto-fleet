package sites

import (
	"context"
	"fmt"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// AssignContainersToSite moves whole controllers. Authorization runs against
// locked placements so a concurrent move cannot change the source permission.
func (s *Service) AssignContainersToSite(ctx context.Context, orgID int64, target *int64, identifiers []string, authorize func(context.Context, *int64) error) (int64, error) {
	identifiers = dedupeStrings(identifiers)
	if len(identifiers) == 0 || len(identifiers) > 1000 || (target != nil && *target <= 0) {
		return 0, fleeterror.NewInvalidArgumentError("provide 1–1000 containers and a positive target site, or omit the site to unassign")
	}
	for _, identifier := range identifiers {
		if strings.TrimSpace(identifier) == "" || len(identifier) > 256 {
			return 0, fleeterror.NewInvalidArgumentError("invalid container identifier")
		}
	}
	if authorize == nil {
		return 0, fleeterror.NewForbiddenError("site assignment authorization required")
	}
	result, err := s.transactor.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
		if err := authorize(txCtx, target); err != nil {
			return nil, err
		}
		// Site → devices, in the same order as DeleteSite and equipment writes.
		if target != nil {
			if err := s.store.LockSiteForWrite(txCtx, orgID, *target); err != nil {
				return nil, err
			}
		}
		placements, err := s.store.LockContainersForSiteAssignment(txCtx, orgID, identifiers)
		if err != nil {
			return nil, err
		}
		if len(placements) != len(identifiers) {
			return nil, fleeterror.NewNotFoundError("one or more containers not found")
		}
		for _, placement := range placements {
			if err := authorize(txCtx, placement.SiteID); err != nil {
				return nil, err
			}
		}
		count, err := s.store.AssignContainersToSite(txCtx, orgID, target, identifiers)
		if err != nil {
			return nil, err
		}
		if count != int64(len(identifiers)) {
			return nil, fleeterror.NewNotFoundError("one or more containers not found")
		}
		return count, nil
	})
	if err != nil {
		return 0, err
	}
	count, ok := result.(int64)
	if !ok {
		return 0, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}
	logged := identifiers
	if len(logged) > maxDeviceIdentifiersInMetadata {
		logged = logged[:maxDeviceIdentifiersInMetadata]
	}
	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           "container.assigned_to_site",
		OrganizationID: &orgID,
		SiteID:         target,
		Description:    fmt.Sprintf("Assigned %d container(s) to site %s", count, formatSiteIDForDescription(target)),
		Metadata:       map[string]any{"container_identifiers": logged, "container_count": count, "target_site_id": target},
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)
	return count, nil
}
