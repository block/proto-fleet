package maintenance

import (
	"testing"

	"buf.build/go/protovalidate"
	pb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTicketTargetsRejectConflictingPlacementFields(t *testing.T) {
	id := int64(1)
	identifier := "miner-1"
	for _, tc := range []struct {
		name string
		set  func(*pb.CreateRepairTicketRequest)
	}{
		{"site", func(r *pb.CreateRepairTicketRequest) { r.SiteId = &id }},
		{"building", func(r *pb.CreateRepairTicketRequest) { r.BuildingId = &id }},
		{"rack", func(r *pb.CreateRepairTicketRequest) { r.RackId = &id }},
		{"miner", func(r *pb.CreateRepairTicketRequest) { r.MinerIdentifier = &identifier }},
		{"zone", func(r *pb.CreateRepairTicketRequest) { r.Zone = "Zone A" }},
		{"rack label", func(r *pb.CreateRepairTicketRequest) { r.RackLabel = "Rack A" }},
		{"group label", func(r *pb.CreateRepairTicketRequest) { r.GroupLabel = "Group A" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := &pb.CreateRepairTicketRequest{Category: pb.TicketCategory_TICKET_CATEGORY_MODULE, Component: "Power", Description: "Power issue", IdempotencyKey: "create-key", Targets: []*pb.TicketAssetSelection{{Kind: pb.TicketCategory_TICKET_CATEGORY_MODULE, Id: 1}}}
			require.NoError(t, protovalidate.Validate(request))
			tc.set(request)
			require.ErrorContains(t, protovalidate.Validate(request), "location and miner fields cannot be supplied with targets")
		})
	}
}

func TestTicketCompletionRequiresExplicitResolutionNote(t *testing.T) {
	status := pb.TicketStatus_TICKET_STATUS_COMPLETED
	request := &pb.UpdateRepairTicketRequest{Id: 1, Status: &status, ExpectedUpdatedAt: timestamppb.Now()}
	require.ErrorContains(t, protovalidate.Validate(request), "completing a ticket requires a resolution note")
	note := "Repair verified"
	request.ResolutionNote = &note
	require.NoError(t, protovalidate.Validate(request))
}

func TestHostTicketCreationRejectsAccountCategoryInContract(t *testing.T) {
	request := &pb.CreateRepairTicketRequest{Category: pb.TicketCategory_TICKET_CATEGORY_ACCOUNT, Component: "Account", Description: "Account issue", IdempotencyKey: "account-category"}
	require.Error(t, protovalidate.Validate(request))
}

func TestHostOtherTicketAllowsOmittedComponent(t *testing.T) {
	site := int64(1)
	request := &pb.CreateRepairTicketRequest{Category: pb.TicketCategory(6), Description: "General site issue", SiteId: &site, IdempotencyKey: "other"}
	require.NoError(t, protovalidate.Validate(request))
	params, err := toCreateParams(request, 2)
	require.NoError(t, err)
	require.Equal(t, "Unknown", params.Component)
	require.False(t, params.ShareWithClients)
}

func TestHostOtherTicketCategoryCanBeFiltered(t *testing.T) {
	filter, err := toListFilter(&pb.ListRepairTicketsRequest{Filter: &pb.TicketFilter{Categories: []pb.TicketCategory{pb.TicketCategory(6)}}}, 2)
	require.NoError(t, err)
	require.Equal(t, []int16{6}, filter.Categories)
}
