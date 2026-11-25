package fleetinventory

import (
	"context"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Resolve and validate before fetching rows, including when no rows would remain
// after the cursor. Pinning an expired tier could silently lose retained history.
func (s *Service) historyPageResolution(ctx context.Context, token string, cursor pageCursor, start, end time.Time) (int32, error) {
	resolution, err := s.store.GetHistoryResolution(ctx, sqlc.GetHistoryResolutionParams{StartTime: start, EndTime: end})
	if err != nil {
		return 0, err
	}
	if token != "" {
		if cursor.ResolutionSeconds == nil || *cursor.ResolutionSeconds != resolution {
			return 0, fleeterror.NewFailedPreconditionError("history resolution changed or page token expired; restart pagination")
		}
		// Summaries include the whole source bucket intersecting the requested start.
		earliest := start
		if resolution > 0 {
			earliest = start.UTC().Truncate(time.Duration(resolution) * time.Second)
		}
		if cursor.Time.Before(earliest) || !cursor.Time.Before(end) {
			return 0, fleeterror.NewInvalidArgumentError("invalid history cursor")
		}
	}
	return resolution, nil
}
