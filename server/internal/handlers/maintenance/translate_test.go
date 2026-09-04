package maintenance

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToUpdateParamsPreservesPartsSelectionPresence(t *testing.T) {
	without, err := toUpdateParams(&pb.UpdateRepairTicketRequest{Id: 1}, 42)
	require.NoError(t, err)
	assert.Nil(t, without.PartsSelection)

	withEmpty, err := toUpdateParams(&pb.UpdateRepairTicketRequest{Id: 1, PartsSelection: &pb.TicketPartsSelection{}}, 42)
	require.NoError(t, err)
	require.NotNil(t, withEmpty.PartsSelection)
	assert.Empty(t, *withEmpty.PartsSelection)
}

func TestToListFilterRangeChecksSortAndIncludesOverdue(t *testing.T) {
	filter, err := toListFilter(&pb.ListRepairTicketsRequest{Filter: &pb.TicketFilter{OverdueOnly: true}, SortField: pb.TicketSortField_TICKET_SORT_FIELD_CREATED_AT, SortDirection: pb.SortDirection_SORT_DIRECTION_DESC}, 42)
	require.NoError(t, err)
	assert.True(t, filter.OverdueOnly)
	assert.Equal(t, models.SortDirectionDescending, filter.SortDirection)

	_, err = toListFilter(&pb.ListRepairTicketsRequest{SortField: pb.TicketSortField(99)}, 42)
	assert.Error(t, err)
}

func TestToListFilterAcceptsOmittedFilter(t *testing.T) {
	filter, err := toListFilter(&pb.ListRepairTicketsRequest{}, 42)
	require.NoError(t, err)
	assert.Equal(t, int64(42), filter.OrgID)
	assert.Nil(t, filter.AssigneeUserID)
}

func TestToProtoAssigneesMapsHydratedLabels(t *testing.T) {
	assignees := toProtoAssignees([]models.Assignee{{UserID: 7, Username: "tech", RoleName: "Field Tech"}})
	require.Len(t, assignees, 1)
	assert.Equal(t, int64(7), assignees[0].UserId)
	assert.Equal(t, "tech", assignees[0].Username)
}
