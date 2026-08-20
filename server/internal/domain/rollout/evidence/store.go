package evidence

import (
	"context"
	"time"
)

type Store interface {
	ListCandidates(ctx context.Context, limit int32) ([]Candidate, error)
	Refresh(ctx context.Context, candidate Candidate, windowEnd time.Time) (Snapshot, error)
	UpdateSummary(ctx context.Context, summary Summary) (bool, error)
	MarkAutomationError(ctx context.Context, summary Summary) error
}
