package maintenance

import (
	"testing"
	"time"

	"buf.build/go/protovalidate"
	pb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestToCreateParamsMapsIdempotencyKey(t *testing.T) {
	params, err := toCreateParams(&pb.CreateRepairTicketRequest{
		Category:       pb.TicketCategory_TICKET_CATEGORY_MINER,
		Description:    "Miner fan stopped",
		Component:      "Fan",
		IdempotencyKey: "create-ticket-1",
	}, 42)
	require.NoError(t, err)
	assert.Equal(t, "create-ticket-1", params.IdempotencyKey)
}

func TestToUpdateParamsPreservesPartsSelectionPresence(t *testing.T) {
	without, err := toUpdateParams(&pb.UpdateRepairTicketRequest{ExpectedUpdatedAt: timestamppb.New(time.Unix(1700000000, 123456000)), Id: 1}, 42)
	require.NoError(t, err)
	assert.Nil(t, without.PartsSelection)

	withEmpty, err := toUpdateParams(&pb.UpdateRepairTicketRequest{ExpectedUpdatedAt: timestamppb.New(time.Unix(1700000000, 123456000)),
		Id: 1, PartsSelection: &pb.TicketPartsSelection{},
	}, 42)
	require.NoError(t, err)
	require.NotNil(t, withEmpty.PartsSelection)
	assert.Empty(t, *withEmpty.PartsSelection)
}

func TestToUpdateParamsPreservesRMAEtaClearSignal(t *testing.T) {
	params, err := toUpdateParams(&pb.UpdateRepairTicketRequest{ExpectedUpdatedAt: timestamppb.New(time.Unix(1700000000, 123456000)), Id: 1, ClearRmaEta: true}, 42)
	require.NoError(t, err)
	assert.True(t, params.ClearRMAEta)
}

func TestToUpdateParamsAcceptsNoActionNeededResolution(t *testing.T) {
	params, err := toUpdateParams(&pb.UpdateRepairTicketRequest{ExpectedUpdatedAt: timestamppb.New(time.Unix(1700000000, 123456000)),
		Id:         1,
		Resolution: pb.TicketResolution_TICKET_RESOLUTION_NO_ACTION_NEEDED.Enum(),
	}, 42)
	require.NoError(t, err)
	require.NotNil(t, params.Resolution)
	assert.Equal(t, models.TicketResolutionNoActionNeeded, *params.Resolution)
}

func TestToUpdateParamsKeepsResolutionNoteSeparateFromOperationalNotes(t *testing.T) {
	notes, resolutionNote := "Internal diagnostic details", "Repair verified"
	params, err := toUpdateParams(&pb.UpdateRepairTicketRequest{
		Id:                1,
		ExpectedUpdatedAt: timestamppb.New(time.Unix(1700000000, 123456000)),
		Notes:             &notes,
		ResolutionNote:    &resolutionNote,
		Status:            pb.TicketStatus_TICKET_STATUS_COMPLETED.Enum(),
	}, 42)
	require.NoError(t, err)
	require.NotNil(t, params.Notes)
	require.NotNil(t, params.ResolutionNote)
	assert.Equal(t, notes, *params.Notes)
	assert.Equal(t, resolutionNote, *params.ResolutionNote)

	params, err = toUpdateParams(&pb.UpdateRepairTicketRequest{
		Id:                1,
		ExpectedUpdatedAt: timestamppb.New(time.Unix(1700000000, 123456000)),
		ResolutionNote:    &resolutionNote,
		Status:            pb.TicketStatus_TICKET_STATUS_COMPLETED.Enum(),
	}, 42)
	require.NoError(t, err)
	assert.Nil(t, params.Notes, "closing a ticket must not overwrite operational notes")
}

func TestToBulkCloseParamsMapsExplicitResolutionNote(t *testing.T) {
	params, err := toBulkCloseParams(&pb.BulkCloseParams{
		Resolution:     pb.TicketResolution_TICKET_RESOLUTION_DEFERRED,
		ResolutionNote: "Waiting for replacement",
	}, 42, []int64{7, 9})
	require.NoError(t, err)
	require.NotNil(t, params.ResolutionNote)
	assert.Equal(t, "Waiting for replacement", *params.ResolutionNote)
}

func TestToProtoTicketKeepsResolutionNoteSeparateFromOperationalNotes(t *testing.T) {
	notes := "Internal diagnostic details"
	ticket := toProtoTicket(&models.RepairTicket{Notes: &notes, ResolutionNote: "Repair verified"})
	assert.Equal(t, notes, ticket.Notes)
	assert.Equal(t, "Repair verified", ticket.ResolutionNote)
}

func TestToProtoTicketAssetPreservesMinerIdentifier(t *testing.T) {
	asset := toProtoTicketAsset(models.TicketAsset{
		Kind:            models.TicketCategoryMiner,
		ID:              7,
		MinerIdentifier: "01a0fdc7-5872-7db7-bd0e-78c982fb09c6",
	})
	assert.Equal(t, int64(7), asset.Id)
	assert.Equal(t, "01a0fdc7-5872-7db7-bd0e-78c982fb09c6", asset.MinerIdentifier)
}

func TestUpdateResolutionNoteOnlyAppliesToCompletion(t *testing.T) {
	resolutionNote := "Repair verified"
	for _, tc := range []struct {
		name   string
		status *pb.TicketStatus
		valid  bool
	}{
		{"completion", pb.TicketStatus_TICKET_STATUS_COMPLETED.Enum(), true},
		{"ordinary status update", pb.TicketStatus_TICKET_STATUS_IN_PROGRESS.Enum(), false},
		{"no status update", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := protovalidate.Validate(&pb.UpdateRepairTicketRequest{
				Id:                1,
				ExpectedUpdatedAt: timestamppb.New(time.Unix(1700000000, 123456000)),
				Status:            tc.status,
				ResolutionNote:    &resolutionNote,
			})
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "resolution_note is only accepted when completing a ticket")
			}
		})
	}
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

func TestPartsResponseDistinguishesConsumedHistoryFromActiveReservations(t *testing.T) {
	consumed := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	parts := toProtoPartsUsed([]models.PartUsage{{InventoryPartID: 7, PartName: "Fan", Quantity: 1, ConsumedAt: &consumed}, {InventoryPartID: 7, PartName: "Fan", Quantity: 2}})
	field := parts[0].ProtoReflect().Descriptor().Fields().ByName("consumed_at")
	require.NotNil(t, field, "the response must distinguish historical consumption from active reservations")
	require.True(t, parts[0].ProtoReflect().Has(field))
	require.False(t, parts[1].ProtoReflect().Has(field))
}

func TestCreateTicketRequiresDescriptionButNotDiagnosis(t *testing.T) {
	request := &pb.CreateRepairTicketRequest{Category: pb.TicketCategory_TICKET_CATEGORY_INFRASTRUCTURE, Component: "Power", IdempotencyKey: "description-only", Description: "Power is out"}
	require.NoError(t, protovalidate.Validate(request))
	params, err := toCreateParams(request, 42)
	require.NoError(t, err)
	require.Equal(t, "Power is out", params.Description)
	require.Nil(t, params.Diagnosis)
	request.Description = ""
	require.Error(t, protovalidate.Validate(request))
	request.Description = "   "
	_, err = toCreateParams(request, 42)
	require.ErrorContains(t, err, "description")
}
