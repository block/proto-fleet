package sqlstores

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

type countingRolloutLaneChannelQuerier struct {
	sqlc.Querier
	calls int
	rows  []sqlc.ListRolloutLaneChannelsByLaneIDsRow
	arg   sqlc.ListRolloutLaneChannelsByLaneIDsParams
}

func (q *countingRolloutLaneChannelQuerier) ListRolloutLaneChannelsByLaneIDs(
	_ context.Context,
	arg sqlc.ListRolloutLaneChannelsByLaneIDsParams,
) ([]sqlc.ListRolloutLaneChannelsByLaneIDsRow, error) {
	q.calls++
	q.arg = arg
	return q.rows, nil
}

func TestRolloutLaneChannelIDsBatchesManySourceLanes(t *testing.T) {
	t.Parallel()

	const sourceLaneCount = 1000
	laneIDs := make([]uuid.UUID, 0, sourceLaneCount)
	rows := make([]sqlc.ListRolloutLaneChannelsByLaneIDsRow, 0, sourceLaneCount*2)
	wantChannelIDs := make([]int64, 0, sourceLaneCount*2)
	for index := range sourceLaneCount {
		laneID := uuid.New()
		laneIDs = append(laneIDs, laneID)
		for position := range 2 {
			channelID := int64(index*2 + position + 1)
			rows = append(rows, sqlc.ListRolloutLaneChannelsByLaneIDsRow{
				LaneID:    laneID,
				ChannelID: channelID,
			})
			wantChannelIDs = append(wantChannelIDs, channelID)
		}
	}
	querier := &countingRolloutLaneChannelQuerier{rows: rows}

	channelIDs, err := rolloutLaneChannelIDs(t.Context(), querier, 42, laneIDs)

	require.NoError(t, err)
	assert.Equal(t, 1, querier.calls)
	assert.Equal(t, int64(42), querier.arg.OrgID)
	assert.Equal(t, laneIDs, querier.arg.LaneIds)
	assert.Equal(t, wantChannelIDs, channelIDs)
}
