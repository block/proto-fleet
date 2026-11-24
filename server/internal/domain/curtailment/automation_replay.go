package curtailment

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// lookupAutomationReplay uses the same handle precedence as automation starts,
// without needing a current response profile to restore an existing event.
func (s *Service) lookupAutomationReplay(ctx context.Context, orgID int64, idempotencyKey, externalReference string) (*models.Event, error) {
	externalSource := automationExternalSource
	return s.lookupIdempotentReplay(ctx, StartRequest{
		PreviewRequest:    PreviewRequest{OrgID: orgID},
		IdempotencyKey:    &idempotencyKey,
		ExternalSource:    &externalSource,
		ExternalReference: &externalReference,
	})
}
