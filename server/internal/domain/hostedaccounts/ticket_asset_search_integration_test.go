package hostedaccounts_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTicketAssetSearchTreatsWildcardsLiterally(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.DefaultCapabilities)
	for _, tc := range []struct{ literal, decoy string }{{"rack_A", "rackXA"}, {"unit%100", "unitX100"}, {`row\one`, "rowone"}} {
		expected := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
		decoy := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
		for _, entry := range []struct {
			id    int64
			label string
		}{{expected, tc.literal}, {decoy, tc.decoy}} {
			_, err := f.db.ExecContext(t.Context(), "UPDATE device SET custom_name=$1 WHERE id=$2", entry.label, entry.id)
			require.NoError(t, err)
			require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, entry.id, 0)))
		}
		host, err := ticketServiceForTest(f.db).ListTicketAssetOptions(t.Context(), f.org, nil, nil, models.TicketCategoryMiner, tc.literal)
		require.NoError(t, err)
		require.Len(t, host, 1)
		require.Equal(t, expected, host[0].ID)
		client, err := f.svc.ListAccountTicketAssets(t.Context(), f.user, f.org, &pb.ListAccountTicketAssetsRequest{AccountId: account.AccountID, Search: tc.literal})
		require.NoError(t, err)
		require.Len(t, client.Assets, 1)
		require.Equal(t, expected, client.Assets[0].Id)
	}
	module, _ := f.miningModule(t)
	var expected int64
	for _, serial := range []string{"board_A%", "boardXA100"} {
		var board int64
		require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id", f.org).Scan(&board))
		_, err := f.db.ExecContext(t.Context(), "INSERT INTO hashboard_identity(org_id,hashboard_id,kind,value) VALUES($1,$2,'serial',$3)", f.org, board, serial)
		require.NoError(t, err)
		_, err = f.db.ExecContext(t.Context(), "INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,$3,now(),now())", board, module, 1+int(board%2))
		require.NoError(t, err)
		if serial == "board_A%" {
			expected = board
		}
	}
	ticket, _, err := ticketServiceForTest(f.db).CreateRepairTicket(t.Context(), models.CreateParams{OrgID: f.org, Category: models.TicketCategoryModule, Component: "Board", IdempotencyKey: uuid.NewString(), Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: module}}})
	require.NoError(t, err)
	options, err := ticketServiceForTest(f.db).ListTicketBoardOptions(t.Context(), f.org, ticket.ID, "board_A%")
	require.NoError(t, err)
	require.Len(t, options, 1)
	require.Equal(t, expected, options[0].ID)
}
