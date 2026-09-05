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

func TestToUpdateParamsPreservesRMAEtaClearSignal(t *testing.T) {
	params, err := toUpdateParams(&pb.UpdateRepairTicketRequest{Id: 1, ClearRmaEta: true}, 42)
	require.NoError(t, err)
	assert.True(t, params.ClearRMAEta)
}

func TestToUpdateParamsAcceptsNoActionNeededResolution(t *testing.T) {
	params, err := toUpdateParams(&pb.UpdateRepairTicketRequest{
		Id:         1,
		Resolution: pb.TicketResolution_TICKET_RESOLUTION_NO_ACTION_NEEDED.Enum(),
	}, 42)
	require.NoError(t, err)
	require.NotNil(t, params.Resolution)
	assert.Equal(t, models.TicketResolutionNoActionNeeded, *params.Resolution)
}

func TestToListFilterRangeChecksSortAndIncludesOverdue(t *testing.T) {
	filter, err := toListFilter(&pb.ListRepairTicketsRequest{Filter: &pb.TicketFilter{OverdueOnly: true}, SortField: pb.TicketSortField_TICKET_SORT_FIELD_CREATED_AT, SortDirection: pb.SortDirection_SORT_DIRECTION_DESC}, 42)
	require.NoError(t, err)
	assert.True(t, filter.OverdueOnly)
	assert.Equal(t, models.SortDirectionDescending, filter.SortDirection)

	_, err = toListFilter(&pb.ListRepairTicketsRequest{SortField: pb.TicketSortField(99)}, 42)
	assert.Error(t, err)
}

func TestCompletedAtSortIsLimitedToCompletedTickets(t *testing.T) {
	_, err := toListFilter(&pb.ListRepairTicketsRequest{SortField: pb.TicketSortField_TICKET_SORT_FIELD_COMPLETED_AT}, 42)
	assert.Error(t, err)

	filter, err := toCompletedFilter(&pb.ListCompletedTicketsRequest{SortField: pb.TicketSortField_TICKET_SORT_FIELD_COMPLETED_AT}, 42)
	require.NoError(t, err)
	assert.Equal(t, models.TicketSortFieldCompletedAt, filter.SortField)
}

func TestToListFilterAcceptsOmittedFilter(t *testing.T) {
	filter, err := toListFilter(&pb.ListRepairTicketsRequest{}, 42)
	require.NoError(t, err)
	assert.Equal(t, int64(42), filter.OrgID)
	assert.Nil(t, filter.AssigneeUserID)
}

func TestListResponsesOnlyEmitCursorWhenAnotherPageExists(t *testing.T) {
	cursor := models.TicketCursor{
		SortField: models.TicketSortFieldCreatedAt, SortDirection: models.SortDirectionDescending,
		Value: "2026-09-04T00:00:00Z", ID: 7,
	}
	tickets := []models.RepairTicketSummary{{RepairTicket: models.RepairTicket{ID: 7}, Cursor: cursor}}

	finalPage := toListRepairTicketsResponse(tickets, 1, false)
	assert.Empty(t, finalPage.NextPageToken)

	pageWithMore := toListCompletedTicketsResponse(tickets, 2, true, []models.Assignee{{UserID: 9, Username: "former-tech"}})
	assert.NotEmpty(t, pageWithMore.NextPageToken)
	require.Len(t, pageWithMore.AssigneeFacets, 1)
	assert.Equal(t, "former-tech", pageWithMore.AssigneeFacets[0].Username)
}

func TestToProtoAssigneesMapsHydratedLabels(t *testing.T) {
	assignees := toProtoAssignees([]models.Assignee{{UserID: 7, Username: "tech", RoleName: "Field Tech"}})
	require.Len(t, assignees, 1)
	assert.Equal(t, int64(7), assignees[0].UserId)
	assert.Equal(t, "tech", assignees[0].Username)
}
