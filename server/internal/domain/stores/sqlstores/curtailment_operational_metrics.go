package sqlstores

import (
	"context"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func (s *SQLCurtailmentStore) GetPendingWorkAges(ctx context.Context) (time.Duration, time.Duration, error) {
	row, err := s.GetQueries(ctx).GetCurtailmentPendingWorkAges(ctx)
	if err != nil {
		return 0, 0, fleeterror.NewInternalErrorf("failed to read curtailment pending work ages: %v", err)
	}
	return time.Duration(row.PendingEventAgeSeconds * float64(time.Second)), time.Duration(row.PendingMqttEdgeAgeSeconds * float64(time.Second)), nil
}
