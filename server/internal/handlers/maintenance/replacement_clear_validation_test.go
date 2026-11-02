package maintenance

import (
	"testing"

	"buf.build/go/protovalidate"
	pb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTargetReplacementClearRequestValidation(t *testing.T) {
	replacement, model := int64(11), "BM1368"
	for _, tc := range []struct {
		name    string
		request *pb.UpdateTicketTargetRequest
		valid   bool
	}{
		{"clear module only", &pb.UpdateTicketTargetRequest{Kind: pb.TicketCategory_TICKET_CATEGORY_MODULE, ClearReplacementModule: true}, true},
		{"clear board only", &pb.UpdateTicketTargetRequest{Kind: pb.TicketCategory_TICKET_CATEGORY_HASHBOARD, ClearReplacementHashboard: true}, true},
		{"omitted replacement", &pb.UpdateTicketTargetRequest{Kind: pb.TicketCategory_TICKET_CATEGORY_MODULE, Diagnosis: &model}, true},
		{"module set and clear", &pb.UpdateTicketTargetRequest{Kind: pb.TicketCategory_TICKET_CATEGORY_MODULE, ClearReplacementModule: true, ReplacementModuleId: &replacement}, false},
		{"replacement model and clear", &pb.UpdateTicketTargetRequest{Kind: pb.TicketCategory_TICKET_CATEGORY_MODULE, ClearReplacementModule: true, ReplacementModuleModel: &model}, false},
		{"board set and clear", &pb.UpdateTicketTargetRequest{Kind: pb.TicketCategory_TICKET_CATEGORY_HASHBOARD, ClearReplacementHashboard: true, ReplacementHashboardId: &replacement}, false},
		{"module clear on board", &pb.UpdateTicketTargetRequest{Kind: pb.TicketCategory_TICKET_CATEGORY_HASHBOARD, ClearReplacementModule: true}, false},
		{"board clear on module", &pb.UpdateTicketTargetRequest{Kind: pb.TicketCategory_TICKET_CATEGORY_MODULE, ClearReplacementHashboard: true}, false},
		{"module clear on miner", &pb.UpdateTicketTargetRequest{Kind: pb.TicketCategory_TICKET_CATEGORY_MINER, ClearReplacementModule: true}, false},
		{"board clear on miner", &pb.UpdateTicketTargetRequest{Kind: pb.TicketCategory_TICKET_CATEGORY_MINER, ClearReplacementHashboard: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.request.TicketId, tc.request.AssetId = 3, 10
			tc.request.ExpectedUpdatedAt = timestamppb.Now()
			err := protovalidate.Validate(tc.request)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
