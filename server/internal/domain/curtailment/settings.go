package curtailment

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const MaxPostEventCooldownSec int32 = 24 * 60 * 60

type UpdateSettingsRequest struct {
	OrgID                int64
	PostEventCooldownSec int32
}

func (s *Service) GetSettings(ctx context.Context, orgID int64) (*models.OrgConfig, error) {
	if s == nil || s.store == nil {
		return nil, fleeterror.NewUnimplementedError("curtailment service is not configured")
	}
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	return s.store.GetOrgConfig(ctx, orgID)
}

func (s *Service) UpdateSettings(ctx context.Context, req UpdateSettingsRequest) (*models.OrgConfig, error) {
	if s == nil || s.store == nil {
		return nil, fleeterror.NewUnimplementedError("curtailment service is not configured")
	}
	if req.OrgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if req.PostEventCooldownSec < 0 {
		return nil, fleeterror.NewInvalidArgumentError("post_event_cooldown_sec must be >= 0")
	}
	if req.PostEventCooldownSec > MaxPostEventCooldownSec {
		return nil, fleeterror.NewInvalidArgumentErrorf(
			"post_event_cooldown_sec must be <= %d, got %d",
			MaxPostEventCooldownSec,
			req.PostEventCooldownSec,
		)
	}
	return s.store.UpdateOrgConfigPostEventCooldown(ctx, req.OrgID, req.PostEventCooldownSec)
}
