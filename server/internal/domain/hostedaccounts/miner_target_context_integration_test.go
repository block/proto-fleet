package hostedaccounts_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	mpb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMinerTargetTicketsPreserveOpeningPlacementAndLinks(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.DefaultCapabilities)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES($1,'Site','ticket-context') RETURNING id", f.org).Scan(&site))
	var buildings, racks, miners [2]int64
	var identifiers [2]string
	for i := range miners {
		miners[i] = f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
		require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO building(org_id,site_id,name) VALUES($1,$2,$3) RETURNING id", f.org, site, uuid.NewString()).Scan(&buildings[i]))
		require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO device_set(org_id,type,label) VALUES($1,'rack',$2) RETURNING id", f.org, uuid.NewString()).Scan(&racks[i]))
		_, err := f.db.ExecContext(t.Context(), "INSERT INTO device_set_rack(device_set_id,org_id,site_id,building_id,rows,columns) VALUES($1,$2,$3,$4,1,1)", racks[i], f.org, site, buildings[i])
		require.NoError(t, err)
		_, err = f.db.ExecContext(t.Context(), "UPDATE device SET site_id=$1,building_id=$2 WHERE id=$3", site, buildings[i], miners[i])
		require.NoError(t, err)
		_, err = f.db.ExecContext(t.Context(), "INSERT INTO device_set_membership(org_id,device_set_id,device_set_type,device_id,device_identifier) SELECT $1,$2,'rack',id,device_identifier FROM device WHERE id=$3", f.org, racks[i], miners[i])
		require.NoError(t, err)
		require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT device_identifier FROM device WHERE id=$1", miners[i]).Scan(&identifiers[i]))
		require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miners[i], 0)))
	}
	single, err := f.svc.CreateAccountTicket(t.Context(), f.user, f.org, &pb.CreateAccountTicketRequest{AccountId: account.AccountID, Description: "One miner", IdempotencyKey: uuid.NewString(), Targets: []*mpb.TicketAssetSelection{{Kind: mpb.TicketCategory_TICKET_CATEGORY_MINER, Id: miners[0]}}})
	require.NoError(t, err)
	service := ticketServiceForTest(f.db)
	detail, err := service.GetRepairTicket(t.Context(), f.org, single.Ticket.Id)
	require.NoError(t, err)
	require.Equal(t, &identifiers[0], detail.Ticket.MinerIdentifier)
	require.Equal(t, &buildings[0], detail.Ticket.BuildingID)
	require.Equal(t, &racks[0], detail.Ticket.RackID)
	multi := seedLegacyClientTicket(t, f, account, models.CreateParams{Category: models.TicketCategoryMiner, SiteID: &site, Description: "Two miners", Component: "Unknown", Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryMiner, ID: miners[0]}, {Kind: models.TicketCategoryMiner, ID: miners[1]}}})
	detail, err = service.GetRepairTicket(t.Context(), f.org, multi.Ticket.Id)
	require.NoError(t, err)
	require.Nil(t, detail.Ticket.MinerIdentifier, "a multi-miner incident has no arbitrary primary miner")
	require.Len(t, detail.Ticket.Targets, 2)
	for i, target := range detail.Ticket.Targets {
		require.Equal(t, identifiers[i], target.MinerIdentifier)
	}
	for _, moved := range []bool{false, true} {
		if moved {
			_, err = f.db.ExecContext(t.Context(), "DELETE FROM device_set_membership WHERE org_id=$1 AND device_set_type='rack'", f.org)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), "UPDATE device SET building_id=NULL WHERE org_id=$1", f.org)
			require.NoError(t, err)
		}
		for i := range miners {
			for _, filter := range []models.ListFilter{{OrgID: f.org, BuildingIDs: []int64{buildings[i]}}, {OrgID: f.org, RackIDs: []int64{racks[i]}}} {
				tickets, total, _, err := service.ListRepairTickets(t.Context(), filter)
				require.NoError(t, err)
				expected := []int64{multi.Ticket.Id}
				if i == 0 {
					expected = append(expected, single.Ticket.Id)
				}
				actual := make([]int64, len(tickets))
				for j, ticket := range tickets {
					actual[j] = ticket.ID
				}
				require.ElementsMatch(t, expected, actual, "filters use the opening placement even after moves")
				require.EqualValues(t, len(expected), total)
			}
		}
	}
}
