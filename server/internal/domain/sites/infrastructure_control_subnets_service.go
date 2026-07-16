package sites

import (
	"context"
	"fmt"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const (
	eventControlSubnetsCommissioned   = "site.infrastructure_control_subnets_commissioned"
	eventControlSubnetsDecommissioned = "site.infrastructure_control_subnets_decommissioned"
)

// GetInfrastructureControlSubnets returns the dedicated, sensitive per-site
// OT allowlist without exposing it through the generic Site model.
func (s *Service) GetInfrastructureControlSubnets(ctx context.Context, orgID, siteID int64) ([]string, error) {
	canonical, err := s.store.GetInfrastructureControlSubnets(ctx, orgID, siteID)
	if err != nil {
		return nil, err
	}
	return infrastructureControlSubnetsFromStorage(canonical), nil
}

// SetInfrastructureControlSubnets validates and explicitly replaces the
// per-site OT allowlist. The replacement and its audit row commit atomically;
// empty input decommissions the site.
func (s *Service) SetInfrastructureControlSubnets(ctx context.Context, orgID, siteID int64, entries []string) ([]string, error) {
	if s.transactor == nil || s.activitySvc == nil {
		return nil, fleeterror.NewInternalError("site infrastructure control-subnet commissioning is not configured")
	}

	canonical, err := CanonicalizeInfrastructureControlSubnets(entries)
	if err != nil {
		return nil, err
	}

	var subnets []string
	err = s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		persisted, err := s.store.SetInfrastructureControlSubnets(
			txCtx,
			orgID,
			siteID,
			canonical.Canonical,
		)
		if err != nil {
			return err
		}
		subnets = infrastructureControlSubnetsFromStorage(persisted)

		event := infrastructureControlSubnetsEvent(txCtx, orgID, siteID, len(subnets))
		if err := s.activitySvc.LogStrict(txCtx, event); err != nil {
			return fleeterror.NewInternalErrorf("failed to audit infrastructure control-subnet commissioning: %v", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return subnets, nil
}

func infrastructureControlSubnetsEvent(ctx context.Context, orgID, siteID int64, prefixCount int) activitymodels.Event {
	eventType := eventControlSubnetsCommissioned
	description := fmt.Sprintf(
		"Commissioned infrastructure control subnets for site %d (%d prefixes)",
		siteID,
		prefixCount,
	)
	if prefixCount == 0 {
		eventType = eventControlSubnetsDecommissioned
		description = fmt.Sprintf("Decommissioned infrastructure control subnets for site %d", siteID)
	}

	event := activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           eventType,
		OrganizationID: &orgID,
		SiteID:         &siteID,
		Description:    description,
		Metadata: map[string]any{
			"site_id":      siteID,
			"prefix_count": prefixCount,
		},
	}
	activity.StampActor(ctx, &event)
	return event
}

func infrastructureControlSubnetsFromStorage(canonical string) []string {
	if strings.TrimSpace(canonical) == "" {
		return nil
	}
	lines := strings.Split(canonical, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if subnet := strings.TrimSpace(line); subnet != "" {
			out = append(out, subnet)
		}
	}
	return out
}
